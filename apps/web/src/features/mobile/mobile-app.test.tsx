import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { App } from "@/App";
import { coreRoutes, IDENTITY, installFetch, jsonResponse, sseResponse, type FetchRoutes } from "@/test/http";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
function open(path: string, routes: FetchRoutes = {}) {
  window.location.hash = path;
  const installed = installFetch({ ...coreRoutes(), "GET /api/v1/auth/me": () => jsonResponse({ ...IDENTITY, csrfToken: "mobile-csrf" }), ...routes });
  render(<App />);
  return installed;
}
const nodes = [
  { id: "n-1", name: "Tokyo", online: true, cpuPercent: 0, memoryPercent: 60, diskPercent: 40, sampledAt: new Date().toISOString() },
  { id: "n-2", name: "Offline", online: false, cpuPercent: 97 },
  { id: "n-3", name: "Stale", online: true, cpuPercent: 98, sampledAt: "2020-01-01T00:00:00Z" },
];

it("restores an authenticated app session and preserves zero, offline and stale metric semantics", async () => {
  open("/app/nodes", { "GET /api/v1/nodes": () => jsonResponse({ items: nodes }) });
  await screen.findByRole("heading", { name: "节点概览" });
  const nav = screen.getByRole("navigation", { name: "App 导航" });
  expect(within(nav).getAllByRole("link")).toHaveLength(2);
  expect(screen.queryByRole("navigation", { name: "主导航" })).not.toBeInTheDocument();
  expect(await screen.findByText("0.0%")).toBeVisible();
  expect(screen.queryByText("97.0%")).not.toBeInTheDocument();
  expect(screen.queryByText("98.0%")).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("textbox", { name: "搜索节点" }), { target: { value: "Tokyo" } });
  expect(screen.queryByRole("heading", { name: "Offline" })).not.toBeInTheDocument();
});
it("shows node hardware using Controller read endpoints", async () => {
  open("/app/nodes/n-1", {
    "GET /api/v1/nodes": () => jsonResponse({ items: nodes }),
    "GET /api/v1/nodes/n-1": () => jsonResponse({ nodeId: "n-1", displayName: "Tokyo", status: "online", platform: "linux", address: "192.0.2.1" }),
    "GET /api/v1/nodes/n-1/capabilities": () => jsonResponse({ nodeId: "n-1", capabilities: [], cpuModel: "Demo CPU", cpuCores: 4, memoryTotalBytes: 4294967296 }),
  });
  expect(await screen.findByText("Demo CPU")).toBeVisible();
  expect(screen.getByText("192.0.2.1")).toBeVisible();
  expect(screen.getByText("4 核")).toBeVisible();
  expect(screen.getByRole("link", { name: "所有节点" })).toHaveAttribute("href", "#/app/nodes");
});
it("keeps AI history, reasoning, touch grants and task execution available in the app", async () => {
  const conversation = { id: "s-1", title: "日常检查", scopeRevision: 2, status: "active", resourceGrants: [], allowedNodeIds: ["n-1"], createdBy: "u-admin" };
  const { calls } = open("/app/ai", {
    "GET /api/v1/nodes": () => jsonResponse({ items: nodes }),
    "GET /api/v1/service-groups": () => jsonResponse({ items: [] }),
    "GET /api/v1/ai/sessions": () => jsonResponse({ items: [conversation] }),
    "GET /api/v1/ai/sessions/s-1": () => jsonResponse(conversation),
    "GET /api/v1/ai/sessions/s-1/approval-policy": () => jsonResponse({ sessionId: "s-1", mode: "per_action", effectiveMode: "per_action", scopeRevision: 2 }),
    "GET /api/v1/ai/sessions/s-1/messages": () => jsonResponse({ items: [{ messageId: "m-1", role: "assistant", createdAt: new Date().toISOString(), redactedContent: "#### 检查结果\\n**一切正常**", reasoningContent: "先读取节点指标", reasoningSummary: "检查概况" }], page: { nextCursor: null, hasMore: false, limit: 100 } }),
    "GET /api/v1/ai/sessions/s-1/events": () => sseResponse([], true),
    "GET /api/v1/ai/sessions/s-1/runs": () => jsonResponse({ items: [] }),
    "GET /api/v1/ai/sessions/s-1/context": () => jsonResponse({ messageCount: 1, estimatedTokens: 30 }),
    "POST /api/v1/ai/sessions/s-1/grants": () => jsonResponse(conversation),
    "POST /api/v1/ai/sessions/s-1/runs": () => jsonResponse({ id: "r-1", state: "thinking", steps: [], scopeRevision: 2 }, 202),
  });
  await waitFor(() => expect(screen.getByRole("combobox", { name: "选择对话" })).toHaveDisplayValue("日常检查"));
  expect(await screen.findByText("一切正常")).toBeVisible();
  fireEvent.click(screen.getByText("思考过程 · 模型输出"));
  expect(screen.getByText("先读取节点指标")).toBeVisible();
  fireEvent.click(screen.getByText("节点与资源授权（0）"));
  fireEvent.click(screen.getByRole("button", { name: "Tokyo" }));
  await waitFor(() => expect(calls.some(call => call.method === "POST" && call.url.endsWith("/grants"))).toBe(true));
  fireEvent.click(screen.getByRole("button", { name: "执行任务" }));
  fireEvent.change(screen.getByRole("textbox", { name: "消息内容" }), { target: { value: "检查磁盘" } });
  fireEvent.click(screen.getByRole("button", { name: "执行" }));
  await waitFor(() => {
    const call = calls.find(call => call.method === "POST" && call.url.endsWith("/runs"));
    expect(JSON.parse(String(call?.init?.body))).toMatchObject({ goal: "检查磁盘", scopeRevision: 2 });
    expect(new Headers(call?.init?.headers).get("X-CSRF-Token")).toBe("mobile-csrf");
  });
});
