import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

// GM-W6 · 正向成功现实（V09-R6-GM）：真实 Agent 执行链路在 run-positive-success-acceptance.mjs
// 达成"任务 completed + 执行 succeeded"。本文件把这些服务端事实固定为页面渲染契约：
// 真实 Controller 的 executions DTO 只有 nodeId/state/error（无 exitCode/startedAt 等
// 审计字段，缺失一律渲染"—"），失败执行必须原样显示服务端 error 文本，未知/超时
// 不得渲染为成功。

const COMPLETED_TASK = {
  taskId: "tsk-done-1",
  state: "completed",
  createdAt: "2026-11-01T10:00:00Z",
  createdBy: "u-1",
  targetNodeIdsSnapshot: ["n-real-1"],
  revision: 2,
  planDigest: "sha256:plan-done",
};

function detailRoutes(executions: readonly unknown[], state = "completed"): FetchRoutes {
  return {
    "GET /api/v1/tasks": () =>
      jsonResponse({ items: [{ ...COMPLETED_TASK, state }], page: { nextCursor: null, limit: 200, hasMore: false } }),
    "GET /api/v1/tasks/tsk-done-1": () => jsonResponse({ ...COMPLETED_TASK, state }),
    "GET /api/v1/tasks/tsk-done-1/executions": () =>
      jsonResponse({ items: executions, page: { nextCursor: null, limit: 200, hasMore: false } }),
    "GET /api/v1/nodes": () =>
      jsonResponse({ items: [{ id: "n-real-1", name: "QA Positive Node", online: false }] }),
  };
}

describe("task detail · real execution results (GM-W6)", () => {
  it("renders a completed task with its succeeded execution and omits audit fields the controller does not return", async () => {
    renderAuthenticated("/tasks/tsk-done-1", detailRoutes([
      // 真实组合运行时的 executions 条目形状：只有 nodeId/state/error。
      { nodeId: "n-real-1", state: "succeeded" },
    ]));
    await screen.findByText("已完成", {}, { timeout: 20_000 });
    await waitFor(() => expect(screen.getByText("逐节点执行")).toBeTruthy(), { timeout: 20_000 });
    const table = screen.getByRole("table", { name: "任务执行结果" });
    expect(table.textContent).toContain("n-real-1");
    expect(table.textContent).toContain("succeeded");
    // 真实 DTO 无 exitCode/startedAt/finishedAt/最后序号/输出截断 → 诚实显示"—"。
    expect(table.textContent).toContain("—");
    expect(table.textContent).not.toContain("failed");
    // 终态任务不再订阅事件流（SSE 由服务端状态门控）。
    await waitFor(() => expect(screen.getByText("任务已结束；不再订阅事件流。")).toBeTruthy(), { timeout: 20_000 });
  });

  it("renders a failed execution with the server error text and never as success", async () => {
    renderAuthenticated("/tasks/tsk-done-1", detailRoutes([
      { nodeId: "n-real-1", state: "failed", error: "agent reported failed" },
    ], "failed"));
    await screen.findByText("失败", {}, { timeout: 20_000 });
    await waitFor(() => expect(screen.getByText("逐节点执行")).toBeTruthy(), { timeout: 20_000 });
    const table = screen.getByRole("table", { name: "任务执行结果" });
    expect(table.textContent).toContain("failed");
    expect(table.textContent).toContain("agent reported failed");
    expect(table.textContent).not.toContain("succeeded");
  });

  it("keeps the unknown-result honesty: an unknown target state is not rendered as succeeded", async () => {
    renderAuthenticated("/tasks/tsk-done-1", detailRoutes([
      { nodeId: "n-real-1", state: "unknown" },
    ], "failed"));
    await screen.findByText("失败", {}, { timeout: 20_000 });
    await waitFor(() => expect(screen.getByText("逐节点执行")).toBeTruthy(), { timeout: 20_000 });
    const table = screen.getByRole("table", { name: "任务执行结果" });
    expect(table.textContent).toContain("unknown");
    expect(table.textContent).not.toContain("succeeded");
  });
});
