import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const WORKFLOW = {
  id: "wf-1",
  revision: 1,
  enabled: true,
  trigger: "manual",
  match: {},
  steps: [{ actionId: "tasks.command", nodeIds: ["n-1"], parameters: { command: "uptime" }, humanApproval: false }],
};

function workflowRoutes(overrides: FetchRoutes = {}): FetchRoutes {
  return {
    "GET /api/v1/workflows": () => jsonResponse({ items: [WORKFLOW] }),
    "POST /api/v1/workflows": () => jsonResponse({ ...WORKFLOW, id: "wf-2" }, 201),
    "GET /api/v1/workflows/wf-1": () =>
      jsonResponse({
        workflow: WORKFLOW,
        runs: [
          {
            runId: "run-1",
            workflowId: "wf-1",
            workflowRevision: 1,
            eventId: "evt-1",
            state: "running",
            steps: [{ taskId: "task-9", state: "queued" }],
            createdAt: "2026-10-31T01:00:00Z",
          },
        ],
      }),
    "PATCH /api/v1/workflows/wf-1": () => jsonResponse({ ...WORKFLOW, revision: 2, enabled: false }),
    "POST /api/v1/workflows/wf-1/runs": () =>
      jsonResponse({
        matched: true,
        run: {
          runId: "run-2",
          workflowId: "wf-1",
          workflowRevision: 1,
          eventId: "evt-2",
          state: "running",
          steps: [{ taskId: "task-10", state: "queued" }],
          createdAt: "2026-10-31T01:05:00Z",
        },
      }, 202),
    ...overrides,
  };
}

describe("workflows page", () => {
  it("creates a workflow through the real controller contract", async () => {
    const { calls } = await renderAuthenticated("/workflows", workflowRoutes());
    expect(await screen.findByRole("table", { name: "自动化工作流" })).toBeVisible();
    expect(screen.getByText("wf-1")).toBeVisible();

    fireEvent.change(screen.getByLabelText("目标节点 ID"), { target: { value: "n-1" } });
    fireEvent.change(screen.getByLabelText("参数（每行 key=value，可选）"), { target: { value: "command=uptime" } });
    fireEvent.click(screen.getByRole("button", { name: "创建工作流" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/workflows");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({
        trigger: "manual",
        steps: [{ actionId: "tasks.command", nodeIds: ["n-1"], parameters: { command: "uptime" }, humanApproval: false }],
      });
    });
    expect(await screen.findByText(/工作流 wf-2 已创建/)).toBeVisible();
  });

  it("toggles enable state with the expectedRevision lock", async () => {
    const { calls } = await renderAuthenticated("/workflows", workflowRoutes());
    await screen.findByRole("table", { name: "自动化工作流" });
    fireEvent.click(screen.getByRole("button", { name: "停用" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/workflows/wf-1");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ expectedRevision: 1, enabled: false });
    });
    expect(await screen.findByText(/已停用（revision 2）/)).toBeVisible();
  });

  it("triggers a run and surfaces the generated task id", async () => {
    const { calls } = await renderAuthenticated("/workflows", workflowRoutes());
    await screen.findByRole("table", { name: "自动化工作流" });
    fireEvent.click(screen.getByRole("button", { name: "触发运行" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/workflows/wf-1/runs");
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body.kind).toBe("manual");
      expect(String(body.eventId)).toMatch(/^wf-run-/);
    });
    expect(await screen.findByText(/运行 run-2 已触发/)).toBeVisible();
    expect(screen.getByText(/任务 ID：task-10/)).toBeVisible();
  });

  it("reports unmatched trigger events without inventing a run", async () => {
    await renderAuthenticated("/workflows", workflowRoutes({
      "POST /api/v1/workflows/wf-1/runs": () => jsonResponse({ matched: false, run: null }, 202),
    }));
    await screen.findByRole("table", { name: "自动化工作流" });
    fireEvent.click(screen.getByRole("button", { name: "触发运行" }));
    expect(await screen.findByText(/matched=false/)).toBeVisible();
    expect(screen.getByText(/未创建运行/)).toBeVisible();
  });

  it("shows run records with task ids in the detail dialog", async () => {
    const { calls } = await renderAuthenticated("/workflows", workflowRoutes());
    await screen.findByRole("table", { name: "自动化工作流" });
    fireEvent.click(screen.getByRole("button", { name: "详情" }));
    await waitFor(() => {
      expect(calls.some(call => call.method === "GET" && call.url === "/api/v1/workflows/wf-1")).toBe(true);
    });
    expect(await screen.findByText(/任务 ID：task-9/)).toBeVisible();
    expect(screen.getByText(/run-1/)).toBeVisible();
  });

  it("disables write actions for identities without workflows.write", async () => {
    const readOnlyIdentity = {
      userId: "u-viewer",
      displayName: "只读",
      roles: ["viewer"],
      permissions: ["workflows.read"],
      isBootstrapAdmin: false,
    };
    await renderAuthenticated("/workflows", {
      ...workflowRoutes(),
      "POST /api/v1/auth/login": () =>
        jsonResponse({ identity: readOnlyIdentity, sessionExpiresAt: "2026-12-31T23:59:59Z", csrfToken: "csrf-test" }),
    });
    expect(await screen.findByText(/写操作已禁用/)).toBeVisible();
    expect(screen.getByRole("button", { name: "创建工作流" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "触发运行" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "停用" })).toBeDisabled();
  });
});
