import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, sseResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const TASK = {
  taskId: "t-1",
  state: "awaiting_approval",
  createdAt: "2026-10-30T01:00:00Z",
  createdBy: "u-1",
  targetNodeIdsSnapshot: ["n-1"],
  revision: 3,
  planDigest: "sha256:plan-1",
  expiresAt: "2026-10-30T02:00:00Z",
};

function taskRoutes(): FetchRoutes {
  return {
    "GET /api/v1/tasks": () =>
      jsonResponse({ items: [TASK], page: { nextCursor: null, limit: 100, hasMore: false } }),
    "GET /api/v1/tasks/t-1": () => jsonResponse(TASK),
    "GET /api/v1/tasks/t-1/executions": () =>
      jsonResponse({
        items: [{ executionId: "x-1", taskId: "t-1", nodeId: "n-1", state: "queued", lastSequence: 0 }],
        page: { nextCursor: null, limit: 200, hasMore: false },
      }),
    "GET /api/v1/tasks/t-1/events": () =>
      sseResponse([
        'id: 7\nevent: task.state\ndata: {"taskId":"t-1","state":"running","progressPercent":40,"sequence":7,"message":"已开始","output":"SECRET-COMMAND-OUTPUT"}\n\n',
      ], true),
    "POST /api/v1/tasks/t-1/approve": () => jsonResponse({ ...TASK, state: "queued" }),
    "POST /api/v1/tasks/t-1/cancel": () => jsonResponse({ ...TASK, state: "canceling" }),
    "POST /api/v1/tasks/t-1/resume": () => jsonResponse({ ...TASK, state: "queued" }),
    "GET /api/v1/nodes": () => jsonResponse({ items: [{ id: "n-1", name: "Tokyo", online: true }] }),
    "POST /api/v1/tasks/preview": () =>
      jsonResponse({ targetNodeIdsSnapshot: ["n-1"], eligibleNodeIds: ["n-1"], deniedNodeIds: [], requiresApproval: true }),
    "POST /api/v1/tasks": () => jsonResponse({ jobId: "t-2", targetType: "node_ids", targetNodeIdsSnapshot: ["n-1"], state: "awaiting_approval" }, 202),
    "GET /api/v1/tasks/t-2": () => jsonResponse({ ...TASK, taskId: "t-2" }),
    "GET /api/v1/tasks/t-2/executions": () => jsonResponse({ items: [], page: { nextCursor: null, limit: 200, hasMore: false } }),
  };
}

describe("task center", () => {
  it("distinguishes same-name nodes by their exact identity", async () => {
    const { calls } = await renderAuthenticated("/tasks", {
      ...taskRoutes(),
      "GET /api/v1/nodes": () => jsonResponse({ items: [
        { id: "n-2", name: "enrollment-token", online: true },
        { id: "n-1", name: "enrollment-token", online: true },
      ] }),
    });
    await screen.findByText("t-1");
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("任务名称"), { target: { value: "exact node" } });
    fireEvent.change(within(dialog).getByLabelText("命令或脚本"), { target: { value: "host.patch" } });
    fireEvent.click(await within(dialog).findByRole("checkbox", { name: "enrollment-token (n-1)" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "预演目标" }));
    await waitFor(() => {
      const preview = calls.find(call => call.url === "/api/v1/tasks/preview");
      expect(JSON.parse(String(preview?.init?.body)).target.nodeIds).toEqual(["n-1"]);
    });
  });
  it("previews and submits a task through real endpoints", async () => {
    const { calls } = await renderAuthenticated("/tasks", taskRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "批量任务" })).toBeVisible();
    expect(await screen.findByText("t-1")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("任务名称"), { target: { value: "patch" } });
    fireEvent.change(within(dialog).getByLabelText("命令或脚本"), { target: { value: "host.patch" } });
    await within(dialog).findByText("Tokyo");
    fireEvent.click(within(dialog).getByLabelText(/Tokyo/));
    fireEvent.click(within(dialog).getByRole("button", { name: "预演目标" }));
    expect(await within(dialog).findByText(/需要审批：是/)).toBeVisible();
    fireEvent.click(within(dialog).getByRole("button", { name: "提交任务" }));
    await waitFor(() => {
      const create = calls.find(call => call.method === "POST" && call.url === "/api/v1/tasks");
      expect(create).toBeDefined();
      expect(JSON.parse(String(create?.init?.body))).toMatchObject({
        name: "patch",
        command: "host.patch",
        target: { type: "node_ids", nodeIds: ["n-1"] },
        dryRun: true,
      });
    });
    await waitFor(() => expect(window.location.hash).toBe("#/tasks/t-2"));
  });

  it("shows only projected SSE fields, never raw output, and approves with the plan digest", async () => {
    const { calls } = await renderAuthenticated("/tasks/t-1", taskRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "任务 t-1" })).toBeVisible();
    await waitFor(() => expect(screen.getByText(/进度：40%/)).toBeVisible());
    expect(screen.queryByText("SECRET-COMMAND-OUTPUT")).not.toBeInTheDocument();
    expect(screen.getAllByText(/执行中/).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "批准" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("原因（可选）"), { target: { value: "确认" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/tasks/t-1/approve");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ planDigest: "sha256:plan-1", decision: "approve", reason: "确认" });
      expect(new Headers(call?.init?.headers).get("X-CSRF-Token")).toBe("csrf-test");
    });
  });

  it("cancels an active task with revision and documents that cancel is not rollback", async () => {
    const routes = {
      ...taskRoutes(),
      "GET /api/v1/tasks/t-run": () => jsonResponse({ ...TASK, taskId: "t-run", state: "running" }),
      "GET /api/v1/tasks/t-run/executions": () => jsonResponse({ items: [], page: { nextCursor: null, limit: 200, hasMore: false } }),
      "GET /api/v1/tasks/t-run/events": () => sseResponse([], true),
    };
    const { calls } = await renderAuthenticated("/tasks/t-run", routes);
    await screen.findByRole("heading", { level: 1, name: "任务 t-run" });
    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/不会被自动回滚/)).toBeVisible();
    fireEvent.change(within(dialog).getByLabelText("原因（可选）"), { target: { value: "停止" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/tasks/t-run/cancel");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ reason: "停止", expectedRevision: 3 });
    });
  });
});
