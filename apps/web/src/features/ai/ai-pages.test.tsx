import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, sseResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const SESSION = {
  id: "s-1",
  scopeRevision: 2,
  status: "active",
  resourceGrants: [{ grantId: "gr-1", resourceType: "node", resourceId: "n-1", allowedActions: ["nodes.read"] }],
  allowedNodeIds: ["n-1"],
  createdBy: "u-1",
};

const POLICY = {
  sessionId: "s-1",
  mode: "per_action",
  effectiveMode: "per_action",
  scopeRevision: 2,
  enforcedRestrictions: ["root shell needs human approval"],
};

function aiRoutes(): FetchRoutes {
  return {
    "GET /api/v1/ai/sessions/s-1": () => jsonResponse(SESSION),
    "GET /api/v1/ai/sessions/s-1/approval-policy": () => jsonResponse(POLICY),
    "PATCH /api/v1/ai/sessions/s-1/approval-policy": (_init) => jsonResponse({ ...POLICY, mode: "automatic", effectiveMode: "automatic" }),
    "GET /api/v1/ai/sessions/s-1/messages": () =>
      jsonResponse({ items: [{ messageId: "m-1", sessionId: "s-1", role: "assistant", createdAt: "2026-10-30T01:00:00Z", redactedContent: "已脱敏回复" }], page: { nextCursor: null, limit: 100, hasMore: false } }),
    "POST /api/v1/ai/sessions/s-1/messages": (_init) =>
      jsonResponse({ messageId: "m-2", sessionId: "s-1", role: "user", createdAt: "2026-10-30T01:00:00Z", redactedContent: "check" }, 200),
    "GET /api/v1/ai/sessions/s-1/approval-events": () => jsonResponse({ items: [], nextCursor: null }),
    "GET /api/v1/ai/sessions/s-1/events": () =>
      sseResponse(['id: 1\nevent: ai.tool\ndata: {"toolName":"metrics.read","state":"running","message":"ok","output":"SECRET-AI-OUTPUT"}\n\n'], true),
    "POST /api/v1/ai/sessions/s-1/grants": (_init) =>
      jsonResponse({ ...SESSION, scopeRevision: 3, resourceGrants: [...SESSION.resourceGrants, { grantId: "gr-2", resourceType: "node", resourceId: "n-2", allowedActions: ["nodes.read"] }] }),
    "POST /api/v1/ai/sessions/s-1/approvals": () =>
      jsonResponse({ id: "ap-1", sessionId: "s-1", state: "pending", toolName: "metrics.read", canonicalArgumentsHash: "hash-1", targetNodeIdsSnapshot: ["n-1"], scopeRevision: 2, riskLevel: "read", expiresAt: "2026-10-30T03:00:00Z" }, 201),
    "POST /api/v1/ai/sessions/s-1/approvals/ap-1/decision": () =>
      jsonResponse({ id: "ap-1", sessionId: "s-1", state: "approved", toolName: "metrics.read", canonicalArgumentsHash: "hash-1", targetNodeIdsSnapshot: ["n-1"], scopeRevision: 2, expiresAt: "2026-10-30T03:00:00Z" }),
    "POST /api/v1/ai/tools/execute": () =>
      jsonResponse({ accepted: true, validatedNodeIds: ["n-1"], state: "queued", scopeRevision: 2 }),
    "GET /api/v1/nodes": () => jsonResponse({ items: [{ id: "n-1", name: "Tokyo", online: true }, { id: "n-2", name: "Osaka", online: true }] }),
    "GET /api/v1/service-groups": () => jsonResponse({ items: [] }),
  };
}

describe("AI session page", () => {
  it("grants a resource, switches policy and approves a one-time tool action", async () => {
    const { calls } = await renderAuthenticated("/ai/s-1", aiRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "AI 会话 s-1" })).toBeVisible();
    expect(await screen.findByText("有效节点：n-1")).toBeVisible();

    // add grant
    const resourceSelect = await screen.findByLabelText("资源");
    fireEvent.change(resourceSelect, { target: { value: "n-2" } });
    fireEvent.click(screen.getByRole("button", { name: "添加授权" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/ai/sessions/s-1/grants");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ resourceType: "node", resourceId: "n-2", allowedActions: ["nodes.read"] });
    });

    // switch policy
    fireEvent.click(screen.getByRole("tab", { name: "审批策略" }));
    fireEvent.click(await screen.findByRole("button", { name: /自动审批/ }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/ai/sessions/s-1/approval-policy");
      expect(JSON.parse(String(call?.init?.body))).toEqual({ mode: "automatic" });
    });

    // create and approve a tool request
    fireEvent.click(screen.getByRole("tab", { name: "工具审批" }));
    fireEvent.change(await screen.findByLabelText("工具名称"), { target: { value: "metrics.read" } });
    fireEvent.change(screen.getByLabelText("目标节点 ID（逗号分隔）"), { target: { value: "n-1" } });
    fireEvent.click(screen.getByRole("button", { name: "请求审批" }));
    expect(await screen.findByText(/hash-1/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "批准一次" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/ai/sessions/s-1/approvals/ap-1/decision");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ decision: "approve" });
    });
  });

  it("sends a message with the current scope and never renders raw SSE payloads", async () => {
    const { calls } = await renderAuthenticated("/ai/s-1", aiRoutes());
    await screen.findByRole("heading", { level: 1, name: "AI 会话 s-1" });
    await waitFor(() => expect(screen.queryByText("SECRET-AI-OUTPUT")).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "消息与审批审计" }));
    fireEvent.change(await screen.findByLabelText("消息内容"), { target: { value: "check" } });
    fireEvent.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/ai/sessions/s-1/messages");
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body).toMatchObject({ content: "check", scopeRevision: 2 });
      expect(body.clientMessageId).toBeTruthy();
    });
    expect(await screen.findByText("已脱敏回复")).toBeVisible();
  });

  it("creates a new session from the AI entry and navigates to it", async () => {
    const { calls } = await renderAuthenticated("/ai", {
      "GET /api/v1/ai/security-policy": () => jsonResponse({ defaultDeny: true, serverSideEnforced: true }),
      "POST /api/v1/ai/sessions": () => jsonResponse({ ...SESSION, id: "s-new" }, 201),
      "GET /api/v1/ai/sessions/s-new": () => jsonResponse({ ...SESSION, id: "s-new" }),
      "GET /api/v1/ai/sessions/s-new/approval-policy": () => jsonResponse({ ...POLICY, sessionId: "s-new" }),
      "GET /api/v1/ai/sessions/s-new/messages": () => jsonResponse({ items: [], page: { nextCursor: null, limit: 100, hasMore: false } }),
      "GET /api/v1/ai/sessions/s-new/approval-events": () => jsonResponse({ items: [], nextCursor: null }),
      "GET /api/v1/ai/sessions/s-new/events": () => sseResponse([], true),
      "GET /api/v1/nodes": () => jsonResponse({ items: [] }),
      "GET /api/v1/service-groups": () => jsonResponse({ items: [] }),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "AI 运维" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "新建 AI 会话" }));
    await waitFor(() => expect(window.location.hash).toBe("#/ai/s-new"));
    expect(calls.some(call => call.method === "POST" && call.url === "/api/v1/ai/sessions")).toBe(true);
  });
});
