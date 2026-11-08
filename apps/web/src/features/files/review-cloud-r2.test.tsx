import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse, type FetchRoutes } from "@/test/http";

/**
 * 主控首审 W1/W2 的独立回归（V09-CLOUD-REVIEW-R2-01）。
 *
 * 这两条曾经在真实 DOM 上失败：
 *   - 缓存 completed + 当前 GET 403 → 面板仍显示绿色“已完成”；
 *   - A 节点读回挂起 → 切到 B → A 的结果回来污染 B 的面板。
 */
beforeEach(() => {
  sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const NODES = [
  { id: "n-1", name: "Alpha", online: true },
  { id: "n-2", name: "Beta", online: true },
];

const base: FetchRoutes = {
  "GET /api/v1/nodes": () => jsonResponse({ items: NODES }),
  "GET /api/v1/nodes/n-1/files": () => jsonResponse({ nodeId: "n-1", path: "/", entries: [], nextCursor: null }),
  "GET /api/v1/nodes/n-2/files": () => jsonResponse({ nodeId: "n-2", path: "/", entries: [], nextCursor: null }),
  "GET /api/v1/transfers": () => jsonResponse({ items: [] }),
};

const PENDING_KEY = "sf.files.pendingUploads.n-1";

function seed(records: readonly unknown[]) {
  sessionStorage.setItem(PENDING_KEY, JSON.stringify(records));
}

describe("review R2 W1: cached state must never impersonate current evidence", () => {
  it("drops a cached completed status when the current read is forbidden (403)", async () => {
    seed([
      { uploadId: "up-old", nodeId: "n-1", name: "old.txt", status: "completed", taskId: "task-old", updatedAt: "2026-11-03T00:00:00Z" },
    ]);
    const { calls } = await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-old": () =>
        jsonResponse({ error: { code: "forbidden", message: "revoked" } }, 403),
    });
    await waitFor(() => expect(calls.some(call => call.url === "/api/v1/nodes/n-1/files/uploads/up-old")).toBe(true));
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    await waitFor(() => expect(within(panel).getByText("无权限读取（未核验）")).toBeVisible());
    expect(within(panel).queryByText("已完成")).toBeNull();
    expect(within(panel).queryByText(/远端落盘完成/)).toBeNull();
  });

  it("does not show completion before the first read-back settles", async () => {
    seed([
      { uploadId: "up-old", nodeId: "n-1", name: "old.txt", status: "completed", taskId: "task-old", updatedAt: "2026-11-03T00:00:00Z" },
    ]);
    let release!: (value: Response) => void;
    const slow = new Promise<Response>(resolve => {
      release = resolve;
    });
    await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-old": () => slow,
    });
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    // 读回尚未返回：只能是“待核验”，不能凭缓存显示完成。
    expect(within(panel).getByText("待核验")).toBeVisible();
    expect(within(panel).queryByText("已完成")).toBeNull();
    release(jsonResponse({ uploadId: "up-old", status: "completed", taskId: "task-old", bytesReceived: 1 }));
    await waitFor(() => expect(within(panel).getByText("已完成")).toBeVisible());
  });

  it("maps a 5xx read failure to an unverified failure instead of a kept completion", async () => {
    seed([
      { uploadId: "up-old", nodeId: "n-1", name: "old.txt", status: "completed", taskId: "task-old", updatedAt: "2026-11-03T00:00:00Z" },
    ]);
    await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-old": () => jsonResponse({ code: "INTERNAL", message: "boom" }, 500),
    });
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    await waitFor(() => expect(within(panel).getByText("读取失败（未核验）")).toBeVisible());
    expect(within(panel).queryByText("已完成")).toBeNull();
  });

  it("shows UNKNOWN when the server has no such upload session", async () => {
    seed([{ uploadId: "up-gone", nodeId: "n-1", name: "gone.txt", status: "completed", taskId: "task-gone" }]);
    await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-gone": () => jsonResponse({ code: "NOT_FOUND", message: "gone" }, 404),
    });
    const panel = await screen.findByRole("region", { name: "待确认上传" });
    await waitFor(() => expect(within(panel).getByText("会话不存在（UNKNOWN）")).toBeVisible());
    expect(within(panel).queryByText("已完成")).toBeNull();
  });
});

describe("review R2 W2: a stale node read-back must not overwrite the new panel", () => {
  it("drops an in-flight response from the previous node after switching nodes", async () => {
    let release!: (value: Response) => void;
    const pendingOld = new Promise<Response>(resolve => {
      release = resolve;
    });
    seed([
      { uploadId: "up-old", nodeId: "n-1", name: "alpha-only.txt", status: "dispatching", taskId: "task-alpha", updatedAt: "" },
    ]);
    const { calls } = await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-old": () => pendingOld,
    });
    await waitFor(() => expect(calls.some(call => call.url === "/api/v1/nodes/n-1/files/uploads/up-old")).toBe(true));
    expect(await screen.findByText("alpha-only.txt")).toBeVisible();

    fireEvent.change(screen.getByLabelText("左侧节点"), { target: { value: "n-2" } });
    release(jsonResponse({ uploadId: "up-old", status: "completed", taskId: "task-alpha", bytesReceived: 1 }));
    await waitFor(() => expect((screen.getByLabelText("左侧节点") as HTMLSelectElement).value).toBe("n-2"));
    await waitFor(() => expect(screen.queryByText("alpha-only.txt")).toBeNull());
    expect(screen.queryByRole("region", { name: "待确认上传" })).toBeNull();
  });

  it("keeps the new node's own records unverified rather than inheriting the old node's evidence", async () => {
    let release!: (value: Response) => void;
    const pendingOld = new Promise<Response>(resolve => {
      release = resolve;
    });
    seed([
      { uploadId: "up-old", nodeId: "n-1", name: "alpha-only.txt", status: "completed", taskId: "task-alpha" },
    ]);
    sessionStorage.setItem(
      "sf.files.pendingUploads.n-2",
      JSON.stringify([{ uploadId: "up-b", nodeId: "n-2", name: "beta.txt", status: "completed", taskId: "task-beta" }]),
    );
    await renderAuthenticated("/files", {
      ...base,
      "GET /api/v1/nodes/n-1/files/uploads/up-old": () => pendingOld,
      "GET /api/v1/nodes/n-2/files/uploads/up-b": () => jsonResponse({ uploadId: "up-b", status: "dispatching", taskId: "task-beta" }),
    });
    await screen.findByText("alpha-only.txt");
    fireEvent.change(screen.getByLabelText("左侧节点"), { target: { value: "n-2" } });
    release(jsonResponse({ uploadId: "up-old", status: "completed" }));
    // 双栏都会切到 n-2，因此可能出现两个面板；逐个面板检查都不含 A 的记录。
    const panels = await screen.findAllByRole("region", { name: "待确认上传" });
    expect(panels.length).toBeGreaterThan(0);
    for (const panel of panels) {
      expect(within(panel).queryByText("alpha-only.txt")).toBeNull();
      // n-2 缓存里写着 completed，但它不是当前证据：面板不得显示“已完成”。
      expect(within(panel).queryByText("已完成")).toBeNull();
    }
    // n-2 自己的读回说 dispatching，就只能显示“已受理（排队）”。
    await waitFor(() => expect(screen.getAllByText("已受理（排队）").length).toBeGreaterThan(0));
    expect(screen.getAllByText("beta.txt").length).toBeGreaterThan(0);
  });
});
