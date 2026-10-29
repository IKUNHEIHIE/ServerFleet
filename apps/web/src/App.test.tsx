import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { Badge } from "@/components/ui/badge";
import { offlineNode } from "@/test/fixtures";
import { coreRoutes, installFetch } from "@/test/http";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

describe("management console shell", () => {
  it("does not fake a session when the Controller management API is unreachable", async () => {
    vi.stubGlobal("fetch", () => Promise.reject(new Error("network down")));
    render(<App />);
    expect(await screen.findByRole("heading", { level: 1, name: "无法验证管理会话" })).toBeVisible();
    expect(screen.getByText("连接失败")).toBeVisible();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });

  it("shows the real login form for an anonymous session", async () => {
    installFetch(coreRoutes());
    render(<App />);
    expect(await screen.findByRole("heading", { level: 1, name: "登录 Server Fleet" })).toBeVisible();
    expect(screen.getByLabelText("用户名")).toBeVisible();
    expect(screen.getByLabelText("密码")).toBeVisible();
  });

  it("logs in through the real endpoint and enters the dashboard", async () => {
    const { calls } = installFetch(coreRoutes());
    render(<App />);
    fireEvent.change(await screen.findByLabelText("用户名"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByRole("heading", { level: 1, name: "概览仪表盘" })).toBeVisible();
    const login = calls.find(call => call.method === "POST" && call.url === "/api/v1/auth/login");
    expect(login).toBeDefined();
    const body = JSON.parse(String(login?.init?.body));
    expect(body).toEqual({ username: "admin", password: "secret" });
    expect(calls.some(call => call.url.startsWith("/api/v1/nodes"))).toBe(true);
  });

  it("keeps the existing badge semantics", () => {
    render(<Badge aria-label="stage" variant="outline" className="text-lg">候选</Badge>);
    expect(screen.getByLabelText("stage")).toHaveClass("text-lg");
    expect(screen.getByLabelText("stage")).not.toHaveClass("text-xs");
  });

  it("blocks unmocked API calls and preserves missing telemetry", async () => {
    vi.stubGlobal("fetch", () => Promise.reject(new Error("Network disabled in component tests")));
    await expect(fetch("https://controller.example.invalid/api/v1/nodes")).rejects.toThrow("Network disabled");
    expect(offlineNode.cpu).toBeNull();
  });
});

describe("session restore", () => {
  it("locks writes after reload because the current Identity contract omits CSRF", async () => {
    installFetch({
      "GET /api/v1/auth/bootstrap-status": () => new Response(JSON.stringify({ bootstrapAvailable: false, initialized: true }), { headers: { "Content-Type": "application/json" } }),
      "GET /api/v1/auth/me": () => new Response(JSON.stringify({ userId: "u1", displayName: "运维", roles: ["operator"], permissions: ["node.read"] }), { headers: { "Content-Type": "application/json" } }),
      "GET /healthz": () => new Response(JSON.stringify({ status: "ok" }), { headers: { "Content-Type": "application/json" } }),
      "GET /api/v1/nodes": () => new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }),
      "GET /api/v1/tasks": () => new Response(JSON.stringify({ items: [], page: { nextCursor: null, limit: 5, hasMore: false } }), { headers: { "Content-Type": "application/json" } }),
      "GET /api/v1/audit/events": () => new Response(JSON.stringify({ items: [], page: { nextCursor: null, limit: 5, hasMore: false } }), { headers: { "Content-Type": "application/json" } }),
    });
    render(<App />);
    expect(await screen.findByText("安全写入已锁定")).toBeVisible();
    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: "概览仪表盘" })).toBeVisible());
  });
});
