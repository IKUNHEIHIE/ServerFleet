import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IDENTITY, jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function enrollRoutes(identity = IDENTITY): FetchRoutes {
  return {
    "GET /api/v1/node-enrollments": () =>
      jsonResponse({ items: [{ id: "e-1", name: "new-node", status: "pending", region: "ap-east" }] }),
    "POST /api/v1/node-enrollment-tokens": () =>
      jsonResponse({ id: "tok-1", token: "tok-secret-value", expiresAt: "2026-10-30T12:00:00Z" }),
    "POST /api/v1/node-enrollments/e-1/decision": () =>
      jsonResponse({ id: "e-1", name: "new-node", status: "approved" }),
    "POST /api/v1/auth/login": () =>
      jsonResponse({
        identity,
        sessionExpiresAt: "2026-12-31T23:59:59Z",
        csrfToken: "csrf-test",
      }),
  };
}

describe("add-node page", () => {
  it("creates a one-time token and approves an enrollment through real endpoints", async () => {
    const { calls } = await renderAuthenticated("/nodes/new", enrollRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "添加节点" })).toBeVisible();
    expect(await screen.findByText("new-node")).toBeVisible();

    expect((screen.getByLabelText("节点名称") as HTMLInputElement).value).toMatch(/^node-[a-f0-9]{8}$/);
    fireEvent.change(screen.getByLabelText("节点名称"), { target: { value: "  edge-hk-01  " } });
    fireEvent.change(screen.getByLabelText("有效期（分钟）"), { target: { value: "10" } });
    fireEvent.click(screen.getByRole("button", { name: "生成一次性凭证" }));

    expect(await screen.findByText("tok-secret-value")).toBeVisible();
    expect(screen.getByText("接入令牌（仅显示这一次）")).toBeVisible();
    const create = calls.find(call => call.method === "POST" && call.url === "/api/v1/node-enrollment-tokens");
    expect(JSON.parse(String(create?.init?.body))).toEqual({ name: "edge-hk-01", expiryMinutes: 10, maxUses: 1 });

    expect(screen.getByLabelText("节点名称")).toHaveValue("edge-hk-01");
    expect(screen.getByLabelText("节点名称")).toHaveAttribute("readonly");
    expect(screen.queryByText("2. 安装与审批")).not.toBeInTheDocument();
    expect(screen.queryByText("契约缺口：接入历史与安装命令")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "1. 命名与接入凭证" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "2. 可信安装" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "3. 接入审批" })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "批准" }));
    expect(await screen.findByRole("heading", { level: 2, name: "批准节点接入" })).toBeVisible();
    fireEvent.change(screen.getByLabelText("原因（可选）"), { target: { value: "已验证安装来源" } });
    fireEvent.click(screen.getByRole("button", { name: "确认批准" }));

    await waitFor(() => {
      const decision = calls.find(
        call => call.method === "POST" && call.url === "/api/v1/node-enrollments/e-1/decision",
      );
      expect(decision).toBeDefined();
      expect(JSON.parse(String(decision?.init?.body))).toEqual({ decision: "approve", reason: "已验证安装来源", grantRead: true });
    });
    expect(await screen.findByText("已批准该接入请求，并授予当前账户该节点的读取权限。")).toBeVisible();
    await waitFor(() => {
      expect(calls.filter(call => call.url === "/api/v1/node-enrollments").length).toBeGreaterThanOrEqual(2);
    });
  });

  it("locks writes when the server identity has no enrollment permission", async () => {
    await renderAuthenticated(
      "/nodes/new",
      enrollRoutes({ ...IDENTITY, permissions: [], isBootstrapAdmin: false }),
      { ...IDENTITY, permissions: [], isBootstrapAdmin: false },
    );
    expect(await screen.findByText("操作入口已锁定")).toBeVisible();
    expect(screen.getByRole("button", { name: "生成一次性凭证" })).toBeDisabled();
    // 批准按钮在“待审批接入请求”面板内，随接入列表异步加载；等待其出现后再断言禁用。
    expect(await screen.findByRole("button", { name: "批准" })).toBeDisabled();
  });

  it("allows removing the one-time token from the page", async () => {
    await renderAuthenticated("/nodes/new", enrollRoutes());
    fireEvent.click(await screen.findByRole("button", { name: "生成一次性凭证" }));
    expect(await screen.findByText("tok-secret-value")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "从页面移除" }));
    expect(screen.queryByText("tok-secret-value")).not.toBeInTheDocument();
  });

  it("refreshes automatic names for a new token and keeps the current token name fixed", async () => {
    const { calls } = await renderAuthenticated("/nodes/new", enrollRoutes());
    const name = screen.getByLabelText("节点名称") as HTMLInputElement;
    const initial = name.value;
    fireEvent.click(screen.getByRole("button", { name: "生成一次性凭证" }));
    await screen.findByText("tok-secret-value");
    expect(name).toHaveValue(initial);
    expect(name).toHaveAttribute("readonly");
    fireEvent.click(screen.getByRole("button", { name: "生成一次性凭证" }));
    await waitFor(() => expect(name.value).not.toBe(initial));
    const current = name.value;
    expect(current).toMatch(/^node-[a-f0-9]{8}$/);
    const bodies = calls.filter(call => call.url === "/api/v1/node-enrollment-tokens")
      .map(call => JSON.parse(String(call.init?.body)));
    expect(bodies.map(body => body.name)).toEqual([initial, current]);
    fireEvent.click(screen.getByRole("button", { name: "从页面移除" }));
    expect(name.value).not.toBe(current);
    expect(name).not.toHaveAttribute("readonly");
  });

  it("keeps a custom name across token creation and removal, and requires a valid name", async () => {
    const { calls } = await renderAuthenticated("/nodes/new", enrollRoutes());
    const name = screen.getByLabelText("节点名称");
    fireEvent.change(name, { target: { value: "   " } });
    expect(screen.getByRole("button", { name: "生成一次性凭证" })).toBeDisabled();
    expect(calls.some(call => call.url === "/api/v1/node-enrollment-tokens")).toBe(false);
    fireEvent.change(name, { target: { value: "custom-node" } });
    fireEvent.click(screen.getByRole("button", { name: "生成一次性凭证" }));
    await screen.findByText("tok-secret-value");
    fireEvent.click(screen.getByRole("button", { name: "从页面移除" }));
    expect(name).toHaveValue("custom-node");
    fireEvent.click(screen.getByRole("button", { name: "生成一次性凭证" }));
    await screen.findByText("tok-secret-value");
    expect(calls.filter(call => call.url === "/api/v1/node-enrollment-tokens")
      .map(call => JSON.parse(String(call.init?.body)).name)).toEqual(["custom-node", "custom-node"]);
  });
});
