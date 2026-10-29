import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const NODES = [
  { id: "n-1", name: "Alpha", online: true },
  { id: "n-2", name: "Beta", online: true },
];

const LISTING = {
  nodeId: "n-1",
  path: "/",
  entries: [
    { name: "home", path: "/home", type: "directory", sizeBytes: null },
    { name: "readme.txt", path: "/readme.txt", type: "file", sizeBytes: 1200, version: "v1" },
    { name: "link", path: "/link", type: "symlink", sizeBytes: null },
  ],
  nextCursor: null,
};

function fileRoutes(): FetchRoutes {
  return {
    "GET /api/v1/nodes": () => jsonResponse({ items: NODES }),
    "GET /api/v1/nodes/n-1/files": (_init, url) => {
      const path = url.searchParams.get("path");
      if (path === "/home") {
        return jsonResponse({ nodeId: "n-1", path: "/home", entries: [{ name: "a.txt", path: "/home/a.txt", type: "file", sizeBytes: 10 }], nextCursor: null });
      }
      return jsonResponse(LISTING);
    },
    "GET /api/v1/nodes/n-2/files": (_init, url) =>
      jsonResponse({ nodeId: "n-2", path: url.searchParams.get("path") ?? "/", entries: [], nextCursor: null }),
    "POST /api/v1/nodes/n-1/files/directories": () =>
      jsonResponse({ name: "new", path: "/new", type: "directory" }, 201),
    "POST /api/v1/nodes/n-1/files/uploads": () =>
      jsonResponse({ uploadId: "up-1", status: "awaiting_data", expiresAt: "2026-10-30T03:00:00Z" }, 201),
    "GET /api/v1/transfers": () => jsonResponse({ items: [] }),
    "POST /api/v1/transfers": () =>
      jsonResponse(
        {
          id: "tr-1",
          sourceNodeId: "n-1",
          sourcePaths: ["/readme.txt"],
          destinationNodeId: "n-2",
          destinationDir: "/",
          status: "queued",
          progressPercent: 0,
          createdAt: "2026-10-30T02:00:00Z",
        },
        202,
      ),
  };
}

describe("files page", () => {
  it("browses real directories, marks symlinks, and creates a transfer with an idempotency key", async () => {
    const { calls } = await renderAuthenticated("/files", fileRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "文件互传" })).toBeVisible();
    const table = await screen.findByRole("table", { name: "左侧目录内容" });
    expect(within(table).getByText("readme.txt")).toBeVisible();
    expect(within(table).getByText("符号链接（不跟随）")).toBeVisible();
    expect(within(table).getByLabelText("选择 link")).toBeDisabled();
    expect(screen.getByText(/没有下载端点/)).toBeVisible();

    fireEvent.click(within(table).getByLabelText("选择 readme.txt"));
    fireEvent.click(screen.getByRole("button", { name: "发送到另一侧（1）" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("同名冲突策略"), { target: { value: "overwrite" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认发送" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/transfers");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        sourceNodeId: "n-1",
        sourcePaths: ["/readme.txt"],
        destinationNodeId: "n-2",
        destinationDir: "/",
        conflictPolicy: "overwrite",
        followSymlinks: false,
      });
      expect(new Headers(call?.init?.headers).get("Idempotency-Key")).toBeTruthy();
    });
    expect(await screen.findByText(/传输任务 tr-1 已接受/)).toBeVisible();
    expect(screen.getByText(/不表示文件已复制完成/)).toBeVisible();
  });

  it("navigates into a directory and creates a folder through the server", async () => {
    const { calls } = await renderAuthenticated("/files", fileRoutes());
    const table = await screen.findByRole("table", { name: "左侧目录内容" });
    fireEvent.click(within(table).getByRole("button", { name: "home/" }));
    expect(await screen.findByText("a.txt")).toBeVisible();
    expect(calls.some(call => call.url.includes("/api/v1/nodes/n-1/files?path=%2Fhome"))).toBe(true);

    const createButtons = screen.getAllByRole("button", { name: "新建文件夹" });
    fireEvent.click(createButtons[0]);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("文件夹名称"), { target: { value: "new" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/nodes/n-1/files/directories");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ parentPath: "/home", name: "new" });
    });
  });

  it("declares upload metadata but never calls complete without a byte channel", async () => {
    const { calls } = await renderAuthenticated("/files", fileRoutes());
    await screen.findByRole("table", { name: "左侧目录内容" });
    const inputs = document.querySelectorAll<HTMLInputElement>('input[type="file"]');
    expect(inputs.length).toBeGreaterThan(0);
    fireEvent.change(inputs[0], { target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] } });
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/nodes/n-1/files/uploads");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        destinationDir: "/",
        files: [{ name: "hello.txt", sizeBytes: 5 }],
      });
    });
    expect(await screen.findByText(/上传会话已创建/)).toBeVisible();
    expect(screen.getByText(/不能调用 complete 冒充上传完成/)).toBeVisible();
    expect(calls.some(call => call.url.includes("/files/uploads/up-1/complete"))).toBe(false);
  });
});
