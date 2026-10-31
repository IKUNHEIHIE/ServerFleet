import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { coreRoutes, installFetch, jsonResponse, type FetchRoutes } from "@/test/http";

afterEach(() => {
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
    fireEvent.click(screen.getByRole("tab", { name: "富化视图（候选详情）" }));
    expect(await screen.findByText("候选详情契约")).toBeVisible();
    expect(await screen.findAllByText("Tokyo")).not.toHaveLength(0);
    expect(calls.some(call => call.url === "/api/v1/nodes/n-1")).toBe(true);
    expect(calls.some(call => call.url === "/api/v1/nodes/n-2")).toBe(true);
    fireEvent.click(screen.getAllByText("Tokyo")[0]);
    const detailHeading = await screen.findByRole("heading", { level: 2, name: "节点详情" });
    const detailSection = detailHeading.closest("section");
    expect(detailSection).not.toBeNull();
    if (detailSection) expect(within(detailSection).getByText("linux/amd64")).toBeVisible();
  });

  it("fails the whole compatible response on a bad authoritative field", async () => {
    const routes = {
      ...coreRoutes(),
      "GET /api/v1/nodes": () => jsonResponse({ items: [{ id: "n-1", name: "A", online: "yes" }] }),
    };
    await openNodes(routes);
    expect(await screen.findByText("节点数据格式不符合兼容约定")).toBeVisible();
    expect(screen.queryByRole("table", { name: "兼容旧节点列表" })).not.toBeInTheDocument();
    expect(screen.queryByText("共 1 个节点")).not.toBeInTheDocument();
  });
});
