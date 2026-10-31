import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { IDENTITY, coreRoutes, installFetch, jsonResponse, type FetchRoutes } from "@/test/http";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const NODE_ITEMS = [{ id: "n-1", name: "Tokyo", online: true, cpuPercent: 10, memoryPercent: 20 }];

function baseRoutes() {
  let meCalls = 0;
  return {
    ...coreRoutes(),
    "GET /api/v1/auth/me": () => {
      meCalls += 1;
      return meCalls === 1
        ? jsonResponse({ code: "UNAUTHENTICATED", message: "no session" }, 401)
        : jsonResponse(IDENTITY);
    },
    "GET /api/v1/nodes": () => jsonResponse({ items: NODE_ITEMS }),
  };
}

async function signIn(routes: FetchRoutes) {
  window.location.hash = "#/nodes";
  const installed = installFetch(routes);
  render(<App />);
  fireEvent.change(await screen.findByLabelText("用户名"), { target: { value: "admin" } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "secret" } });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  return installed;
}

function pageShowPersisted() {
  const event = new Event("pageshow");
  Object.defineProperty(event, "persisted", { value: true });
  window.dispatchEvent(event);
}

describe("session failure semantics", () => {
  it("sends the user to login on 401 and does not retry in a loop", async () => {
    const { calls } = await signIn({
      ...baseRoutes(),
      "GET /api/v1/nodes": () => jsonResponse({ code: "UNAUTHENTICATED", message: "expired" }, 401),
    });
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "登录 Server Fleet" })).toBeVisible(),
    );
    const nodeCalls = () => calls.filter(call => call.url.startsWith("/api/v1/nodes"));
    await waitFor(() => expect(nodeCalls().length).toBeGreaterThan(0));
    await new Promise(resolve => setTimeout(resolve, 50));
    expect(nodeCalls()).toHaveLength(1);
    expect(screen.queryByRole("navigation", { name: "主导航" })).not.toBeInTheDocument();
  });

  it("keeps the session on 403 and clears only the current resource", async () => {
    await signIn({
      ...baseRoutes(),
      "GET /api/v1/nodes": () => jsonResponse({ code: "PERMISSION_DENIED", message: "denied" }, 403),
    });
    expect(await screen.findByText("无权限")).toBeVisible();
    expect(screen.getByRole("heading", { level: 1, name: "节点列表" })).toBeVisible();
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "登录 Server Fleet" })).not.toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "兼容旧节点列表" })).not.toBeInTheDocument();
  });

  it("keeps the session on 503 and shows a retryable service error", async () => {
    await signIn({
      ...baseRoutes(),
      "GET /api/v1/nodes": () => jsonResponse({ code: "UNAVAILABLE", message: "down" }, 503),
    });
    expect(await screen.findByText(/Controller 暂时不可用/)).toBeVisible();
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新加载" })).toBeVisible();
  });

  it("clears a previously loaded list when a manual refresh returns 403", async () => {
    let calls = 0;
    await signIn({
      ...baseRoutes(),
      "GET /api/v1/nodes": () => {
        calls += 1;
        return calls === 1
          ? jsonResponse({ items: NODE_ITEMS })
          : jsonResponse({ code: "PERMISSION_DENIED", message: "denied" }, 403);
      },
    });
    await waitFor(() => expect(screen.getByRole("table", { name: "兼容旧节点列表" })).toBeVisible());
    fireEvent.click(screen.getByRole("button", { name: "刷新" }));
    expect(await screen.findByText("无权限")).toBeVisible();
    expect(screen.queryByRole("table", { name: "兼容旧节点列表" })).not.toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeInTheDocument();
  });

  it("revalidates on BFCache pageshow and refetches the current resource", async () => {
    const { calls } = await signIn(baseRoutes());
    await waitFor(() => expect(screen.getByRole("table", { name: "兼容旧节点列表" })).toBeVisible());
    const before = calls.filter(call => call.url.startsWith("/api/v1/nodes")).length;
    pageShowPersisted();
    await waitFor(() => {
      expect(calls.filter(call => call.url.startsWith("/api/v1/nodes")).length).toBeGreaterThan(before);
    });
    await waitFor(() => expect(screen.getByRole("table", { name: "兼容旧节点列表" })).toBeVisible());
  });

  it("keeps the session when identity revalidation fails with 503", async () => {
    let meCalls = 0;
    await signIn({
      ...baseRoutes(),
      "GET /api/v1/auth/me": () => {
        meCalls += 1;
        return meCalls === 1
          ? jsonResponse({ code: "UNAUTHENTICATED", message: "no session" }, 401)
          : jsonResponse({ code: "UNAVAILABLE", message: "down" }, 503);
      },
    });
    expect(await screen.findByRole("navigation", { name: "主导航" })).toBeInTheDocument();
    pageShowPersisted();
    expect(await screen.findByText(/当前会话已保留/)).toBeVisible();
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "登录 Server Fleet" })).not.toBeInTheDocument();
  });

  it("logs out through the server with the in-memory CSRF token", async () => {
    const { calls } = await signIn(baseRoutes());
    await waitFor(() => expect(screen.getByRole("table", { name: "兼容旧节点列表" })).toBeVisible());
    fireEvent.click(screen.getByRole("button", { name: "退出" }));
    expect(await screen.findByRole("heading", { level: 1, name: "登录 Server Fleet" })).toBeVisible();
    const logout = calls.find(call => call.method === "POST" && call.url === "/api/v1/auth/logout");
    expect(logout).toBeDefined();
    expect(new Headers(logout?.init?.headers).get("X-CSRF-Token")).toBe("csrf-test");
  });
});

const hasBroadcast = typeof BroadcastChannel !== "undefined";

describe("cross-tab session sync", () => {
  it.skipIf(!hasBroadcast)("adopts a logout from another tab without a reload", async () => {
    await signIn(baseRoutes());
    await waitFor(() => expect(screen.getByRole("table", { name: "兼容旧节点列表" })).toBeVisible());
    const channel = new BroadcastChannel("serverfleet.session");
    channel.postMessage({ type: "logout" });
    expect(await screen.findByRole("heading", { level: 1, name: "登录 Server Fleet" })).toBeVisible();
    expect(screen.getByText("已在其他标签页退出登录。")).toBeVisible();
    channel.close();
  });
});
