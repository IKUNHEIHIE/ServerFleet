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
    "PUT /api/v1/nodes/n-1/files/uploads/up-1/content": () =>
      jsonResponse({ uploadId: "up-1", status: "awaiting_data", bytesReceived: 5, expectedSize: 5 }),
    "POST /api/v1/nodes/n-1/files/uploads/up-1/complete": () =>
      jsonResponse({ uploadId: "up-1", status: "completed", files: [] }),
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

  it("uploads real bytes through the session content channel and completes with server verification", async () => {
    const { calls } = await renderAuthenticated("/files", fileRoutes());
    await screen.findByRole("table", { name: "左侧目录内容" });
    const inputs = document.querySelectorAll<HTMLInputElement>('input[type="file"]');
    expect(inputs.length).toBeGreaterThan(0);
    fireEvent.change(inputs[0], { target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] } });
    await waitFor(() => {
      const session = calls.find(call => call.method === "POST" && call.url === "/api/v1/nodes/n-1/files/uploads");
      expect(session).toBeDefined();
      expect(JSON.parse(String(session?.init?.body))).toMatchObject({
        destinationDir: "/",
        files: [{ name: "hello.txt", sizeBytes: 5 }],
      });
      const content = calls.find(call => call.method === "PUT" && call.url.includes("/files/uploads/up-1/content"));
      expect(content).toBeDefined();
      expect(content?.url).toContain("offset=0");
      expect(new Headers(content?.init?.headers).get("Content-Type")).toBe("application/octet-stream");
      expect(new Headers(content?.init?.headers).get("X-CSRF-Token")).toBeTruthy();
      expect(new Headers(content?.init?.headers).get("X-Requested-With")).toBe("ServerFleetWeb");
      const bytes = content?.init?.body as Uint8Array;
      expect(new TextDecoder().decode(bytes)).toBe("hello");
      expect(calls.some(call => call.method === "POST" && call.url.includes("/files/uploads/up-1/complete"))).toBe(true);
    });
    expect(await screen.findByText(/上传完成，服务端校验通过（5 B）/)).toBeVisible();
    expect(screen.queryByText(/字节通道未实现/)).toBeNull();
  });

  it("shows 202 dispatching as queued with the task id, then reads the real terminal state back", async () => {
    const { calls } = await renderAuthenticated("/files", {
      ...fileRoutes(),
      "POST /api/v1/nodes/n-1/files/uploads/up-1/complete": () =>
        jsonResponse({ uploadId: "up-1", status: "dispatching", taskId: "task-9", files: [] }, 202),
      "GET /api/v1/nodes/n-1/files/uploads/up-1": () =>
        jsonResponse({ uploadId: "up-1", status: "completed", bytesReceived: 5, expectedSize: 5, taskId: "task-9" }),
    });
    await screen.findByRole("table", { name: "左侧目录内容" });
    const inputs = document.querySelectorAll<HTMLInputElement>('input[type="file"]');
    fireEvent.change(inputs[0], { target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] } });
    // 202 只表示已受理进队列：界面必须给出任务号，且不能声称上传完成。
    expect(await screen.findByText(/已受理进入统一任务队列（任务 task-9），尚未完成/)).toBeVisible();
    expect(screen.queryByText(/上传完成，服务端校验通过/)).toBeNull();
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    expect(within(panel).getByText("已受理（排队）")).toBeVisible();
    expect(within(panel).getByText("任务 task-9")).toBeVisible();
    expect(calls.some(call => call.method === "POST" && call.url.includes("/files/uploads/up-1/complete"))).toBe(true);

    fireEvent.click(within(panel).getByRole("button", { name: "读回真实状态" }));
    await waitFor(() => expect(calls.some(call => call.method === "GET" && call.url === "/api/v1/nodes/n-1/files/uploads/up-1")).toBe(true));
    expect(await within(panel).findByText("已完成")).toBeVisible();
    expect(within(panel).queryByText("已受理（排队）")).toBeNull();
  });

  it("keeps dispatching honest when the queue task is still running after a reload", async () => {
    await renderAuthenticated("/files", {
      ...fileRoutes(),
      "POST /api/v1/nodes/n-1/files/uploads/up-1/complete": () =>
        jsonResponse({ uploadId: "up-1", status: "dispatching", taskId: "task-9", files: [] }, 202),
      "GET /api/v1/nodes/n-1/files/uploads/up-1": () =>
        jsonResponse({ uploadId: "up-1", status: "dispatching", bytesReceived: 5, expectedSize: 5, taskId: "task-9" }),
    });
    await screen.findByRole("table", { name: "左侧目录内容" });
    const inputs = document.querySelectorAll<HTMLInputElement>('input[type="file"]');
    fireEvent.change(inputs[0], { target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] } });
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    await waitFor(() => expect(within(panel).getByText("已受理（排队）")).toBeVisible());
    expect(within(panel).queryByText("已完成")).toBeNull();
    expect(within(panel).getByText(/只有本节点、本身份下服务端读回 completed 才显示已完成/)).toBeVisible();
  });

  it("honestly reports verified bytes when the node transport is not injected (503 unavailable)", async () => {
    await renderAuthenticated("/files", {
      ...fileRoutes(),
      "POST /api/v1/nodes/n-1/files/uploads/up-1/complete": () =>
        jsonResponse({ error: { code: "unavailable", message: "file transport is unavailable" } }, 503),
    });
    await screen.findByRole("table", { name: "左侧目录内容" });
    const inputs = document.querySelectorAll<HTMLInputElement>('input[type="file"]');
    fireEvent.change(inputs[0], { target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] } });
    expect(
      await screen.findByText(
        /文件字节流（5 B）已上传并通过服务端 SHA-256 校验；当前节点文件传输通道未注入（503 unavailable）/,
      ),
    ).toBeVisible();
    // 校验通过 ≠ 已送达远端节点：不得显示“上传完成”。
    expect(screen.queryByText(/上传完成，服务端校验通过/)).toBeNull();
  });
});
