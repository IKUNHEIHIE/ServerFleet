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
    "GET /api/v1/installations/catalog": () => jsonResponse({ available: false }),
    "GET /api/v1/node-enrollments": () =>
      jsonResponse({ items: [{ id: "e-1", name: "new-node", status: "pending", region: "ap-east" }] }),
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
  it("approves an enrollment through real endpoints without any token", async () => {
    const { calls } = await renderAuthenticated("/nodes/new", enrollRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "添加节点" })).toBeVisible();
    expect(await screen.findByRole("heading", { name: "一键安装节点" })).toBeVisible();
    expect(await screen.findByRole("heading", { name: "接入审批" })).toBeVisible();
    expect(await screen.findByText("new-node")).toBeVisible();
    expect(screen.queryByText("接入令牌（仅显示这一次）")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "生成一次性凭证" })).not.toBeInTheDocument();

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
    expect(await screen.findByRole("button", { name: "批准" })).toBeDisabled();
  });

  it("rejects an enrollment with a reason", async () => {
    const { calls } = await renderAuthenticated("/nodes/new", enrollRoutes());
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    expect(await screen.findByRole("heading", { level: 2, name: "拒绝节点接入" })).toBeVisible();
    fireEvent.change(screen.getByLabelText("原因（可选）"), { target: { value: "来源不可信" } });
    fireEvent.click(screen.getByRole("button", { name: "确认拒绝" }));
    await waitFor(() => {
      const decision = calls.find(
        call => call.method === "POST" && call.url === "/api/v1/node-enrollments/e-1/decision",
      );
      expect(decision).toBeDefined();
      expect(JSON.parse(String(decision?.init?.body))).toEqual({ decision: "reject", reason: "来源不可信" });
    });
    expect(await screen.findByText("已拒绝该接入请求。")).toBeVisible();
  });
});
