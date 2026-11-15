import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { renderAuthenticated } from "@/test/app";
import { coreRoutes, IDENTITY, installFetch, jsonResponse, type FetchRoutes } from "@/test/http";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const nodeFixtures = [
  { id: "n-1", name: "Tokyo", online: true, region: "ap-northeast", provider: "aws", cpuPercent: 12, memoryPercent: 34 },
  { id: "n-2", name: "Frankfurt", online: false, region: "eu-central", provider: "hetzner", cpuPercent: 55, memoryPercent: 66 },
];

const detailFixtures = [
  { nodeId: "n-1", displayName: "Tokyo", status: "online", revision: 2, platform: "linux/amd64", agentVersion: "0.9.0", lastSeenAt: "2026-10-30T00:00:00Z" },
  { nodeId: "n-2", displayName: "Frankfurt", status: "offline", revision: 5, platform: "linux/amd64" },
];

function nodesRoutes() {
  return {
    ...coreRoutes(),
    "GET /api/v1/nodes": (_init: RequestInit | undefined, url: URL) => {
      const onlineOnly = url.searchParams.get("onlineOnly") === "true";
      const items = onlineOnly ? nodeFixtures.filter(node => node.online) : nodeFixtures;
      return jsonResponse({ items });
    },
    "GET /api/v1/nodes/n-1": () => jsonResponse(detailFixtures[0]),
    "GET /api/v1/nodes/n-2": () => jsonResponse(detailFixtures[1]),
  };
}

async function openNodes(routes: FetchRoutes) {
  window.location.hash = "#/nodes";
  const { calls } = installFetch(routes);
  render(<App />);
  fireEvent.change(await screen.findByLabelText("用户名"), { target: { value: "admin" } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "secret" } });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  await screen.findByRole("heading", { level: 1, name: "节点列表" });
  return { calls };
}

describe("nodes page", () => {
  it("reads the real list endpoint with onlineOnly and renders the compatible view", async () => {
    const { calls } = await openNodes(nodesRoutes());
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("共 2 个节点"));
    const table = screen.getByRole("table", { name: "兼容旧节点列表" });
    expect(within(table).getAllByRole("row").slice(1)).toHaveLength(2);
    expect(calls.some(call => call.url === "/api/v1/nodes?onlineOnly=false")).toBe(true);
    expect(screen.queryByText("正在读取节点详情…")).not.toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/n-1");
    expect(within(screen.getByRole("main")).getByRole("link", { name: "添加节点" })).toHaveAttribute("href", "#/nodes/new");
  });

  it("opens the real detail page from the default list name", async () => {
    const { calls } = await openNodes(nodesRoutes());
    fireEvent.click(await screen.findByRole("link", { name: "Tokyo" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Tokyo" })).toBeVisible();
    expect(calls.some(call => call.url === "/api/v1/nodes/n-1")).toBe(true);
    expect(screen.getByRole("button", { name: "编辑节点" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "删除节点" })).toBeEnabled();
  });

  it("locks adding nodes for an identity with no write permissions", async () => {
    const routes: FetchRoutes = nodesRoutes();
    delete routes["POST /api/v1/auth/login"];
    delete routes["GET /api/v1/auth/me"];
    const { calls } = await renderAuthenticated("/nodes", routes, {
      ...IDENTITY, isBootstrapAdmin: false, roles: [], permissions: [],
    });
    await screen.findByRole("link", { name: "Tokyo" });
    expect(screen.getByRole("button", { name: "添加节点" })).toBeDisabled();
    expect(within(screen.getByRole("main")).queryByRole("link", { name: "添加节点" })).not.toBeInTheDocument();
    expect(calls.some(call => ["PUT", "DELETE"].includes(call.method))).toBe(false);
  });

  it("refetches with onlineOnly=true and clears the previous rows", async () => {
    const { calls } = await openNodes(nodesRoutes());
    fireEvent.click(screen.getByRole("checkbox", { name: "仅在线节点（服务端过滤）" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("共 1 个节点"));
    expect(calls.some(call => call.url === "/api/v1/nodes?onlineOnly=true")).toBe(true);
    expect(screen.queryByText("Frankfurt")).not.toBeInTheDocument();
  });

  it("does not fetch details until the rich view is explicitly opened, then shows real detail fields", async () => {
    const { calls } = await openNodes(nodesRoutes());
    expect(calls.some(call => call.url.startsWith("/api/v1/nodes/n-1"))).toBe(false);
    fireEvent.click(screen.getByRole("tab", { name: "详细状态" }));
    expect(screen.queryByText("候选详情契约")).not.toBeInTheDocument();
    expect(await screen.findAllByText("Tokyo")).not.toHaveLength(0);
    expect(calls.some(call => call.url === "/api/v1/nodes/n-1")).toBe(true);
    expect(calls.some(call => call.url === "/api/v1/nodes/n-2")).toBe(true);
    expect(screen.getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/n-1");
    expect(screen.getAllByText("linux/amd64").length).toBeGreaterThan(0);
  });

  it("shows real rich details when revision is absent", async () => {
    const routes = nodesRoutes();
    routes["GET /api/v1/nodes/n-1"] = () => jsonResponse({ nodeId: "n-1", displayName: "Tokyo", status: "online", platform: "linux/arm64" });
    await openNodes(routes);
    fireEvent.click(screen.getByRole("tab", { name: "详细状态" }));
    expect(await screen.findByText("linux/arm64")).toBeVisible();
    expect(screen.getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/n-1");
  });

  it("refreshes an offline node to online automatically after one second", async () => {
    let online = false;
    const routes = nodesRoutes();
    routes["GET /api/v1/nodes"] = () => jsonResponse({ items: [{ ...nodeFixtures[0], online }] });
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const { calls } = await openNodes(routes);
    await screen.findByRole("link", { name: "Tokyo" });
    expect(within(screen.getByRole("table")).getByText("离线")).toBeVisible();
    const before = calls.filter(call => call.url === "/api/v1/nodes?onlineOnly=false").length;
    online = true;
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(within(screen.getByRole("table")).getByText("在线", { exact: true })).toBeVisible();
    expect(calls.filter(call => call.url === "/api/v1/nodes?onlineOnly=false").length).toBeGreaterThan(before);
  });

  it("keeps the last successful rows with a visible silent-refresh failure", async () => {
    let failed = false;
    const routes = nodesRoutes();
    routes["GET /api/v1/nodes"] = () => failed
      ? jsonResponse({ code: "UNAVAILABLE" }, 503)
      : jsonResponse({ items: nodeFixtures });
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    await openNodes(routes);
    await screen.findByRole("link", { name: "Tokyo" });
    failed = true;
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText(/最近一次刷新失败/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Tokyo" })).toBeVisible();
  });

  it("refreshes rich details when summary name and online state change", async () => {
    let changed = false;
    const routes = nodesRoutes();
    routes["GET /api/v1/nodes"] = () => jsonResponse({ items: [{ ...nodeFixtures[0], name: changed ? "Osaka" : "Tokyo", online: !changed }] });
    routes["GET /api/v1/nodes/n-1"] = () => jsonResponse({ ...detailFixtures[0], displayName: changed ? "Osaka" : "Tokyo", status: changed ? "offline" : "online" });
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const { calls } = await openNodes(routes);
    fireEvent.click(screen.getByRole("tab", { name: "详细状态" }));
    await screen.findByRole("link", { name: "Tokyo" });
    const before = calls.filter(call => call.url === "/api/v1/nodes/n-1").length;
    changed = true;
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText("Osaka")).toBeVisible();
    expect(calls.filter(call => call.url === "/api/v1/nodes/n-1").length).toBeGreaterThan(before);
  });

  it.each([
    { items: [{ id: "n-1", name: "A", online: true }, { id: "n-2", name: "B", online: "yes" }] },
    { items: [{ id: "n-1", name: "A", online: true }, { id: "n-1", name: "B", online: false }] },
    { items: "invalid" },
  ])("fails the whole compatible response on bad authoritative data: %j", async payload => {
    const routes = {
      ...coreRoutes(),
      "GET /api/v1/nodes": () => jsonResponse(payload),
    };
    await openNodes(routes);
    expect(await screen.findByText("节点数据格式不符合兼容约定")).toBeVisible();
    expect(screen.queryByRole("table", { name: "兼容旧节点列表" })).not.toBeInTheDocument();
    expect(screen.queryByText("共 1 个节点")).not.toBeInTheDocument();
    expect(screen.queryByText("1 个节点", { exact: true })).not.toBeInTheDocument();
  });
});
