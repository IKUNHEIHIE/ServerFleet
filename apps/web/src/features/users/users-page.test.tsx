import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function userRoutes(): FetchRoutes {
  return {
    "GET /api/v1/users": () =>
      jsonResponse({
        items: [{ userId: "u-1", username: "ops", displayName: "运维", roleIds: ["operator"], enabled: true, revision: 1 }],
        page: { nextCursor: null, limit: 100, hasMore: false },
      }),
    "GET /api/v1/roles": () =>
      jsonResponse({ items: [{ roleId: "operator", label: "运维", permissions: ["node.read"], builtIn: true }] }),
    "GET /api/v1/permissions": () =>
      jsonResponse({ items: [{ key: "node.read", description: "读取节点", resourceType: "node" }] }),
    "GET /api/v1/resource-grants": () =>
      jsonResponse({
        items: [{ grantId: "g-1", principalId: "u-1", resourceType: "node", resourceId: "n-1", permissions: ["node.read"] }],
        page: { nextCursor: null, limit: 100, hasMore: false },
      }),
    "GET /api/v1/auth/sessions": () =>
      jsonResponse({
        items: [
          { sessionId: "s-current", expiresAt: "2026-12-31T00:00:00Z", current: true },
          { sessionId: "s-other", expiresAt: "2026-12-31T00:00:00Z", current: false },
        ],
        page: { nextCursor: null, limit: 100, hasMore: false },
      }),
    "GET /api/v1/nodes": () => jsonResponse({ items: [] }),
    "GET /api/v1/groups": () => jsonResponse({ items: [] }),
    "GET /api/v1/service-groups": () => jsonResponse({ items: [] }),
    "POST /api/v1/users": () =>
      jsonResponse({ userId: "u-2", username: "new", roleIds: ["operator"], enabled: true, revision: 1 }, 200),
    "POST /api/v1/resource-grants": () =>
      jsonResponse({ grantId: "g-2", principalId: "u-1", resourceType: "node", resourceId: "n-1", permissions: ["node.read"] }),
    "DELETE /api/v1/resource-grants/g-1": () => new Response(null, { status: 204 }),
    "DELETE /api/v1/auth/sessions/s-other": () => new Response(null, { status: 204 }),
  };
}

describe("users and permissions page", () => {
  it("creates a user with a server-assigned role", async () => {
    const { calls } = await renderAuthenticated("/users", userRoutes());
    expect(await screen.findByText("ops")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "创建用户" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("用户名"), { target: { value: "new" } });
    fireEvent.change(within(dialog).getByLabelText("初始密码"), { target: { value: "very-secret" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /运维/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "保存用户" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/users");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        username: "new",
        password: "very-secret",
        roleIds: ["operator"],
        enabled: true,
      });
    });
    expect(await screen.findByText("用户已创建。")).toBeVisible();
  });

  it("creates and revokes a resource grant", async () => {
    const { calls } = await renderAuthenticated("/users", userRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "资源授权" }));
    expect(await screen.findByText("u-1")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "创建授权" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("主体 ID"), { target: { value: "u-1" } });
    fireEvent.change(within(dialog).getByLabelText("资源 ID"), { target: { value: "n-1" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /node\.read/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "创建授权" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/resource-grants");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        principalId: "u-1",
        resourceType: "node",
        resourceId: "n-1",
        permissions: ["node.read"],
      });
    });
    expect(await screen.findByText("资源授权已创建。")).toBeVisible();

    await screen.findByRole("button", { name: "撤销" });
    fireEvent.click(screen.getByRole("button", { name: "撤销" }));
    const revokeDialog = await screen.findByRole("dialog");
    fireEvent.click(within(revokeDialog).getByRole("button", { name: "确认撤销" }));
    await waitFor(() => {
      expect(calls.some(call => call.method === "DELETE" && call.url === "/api/v1/resource-grants/g-1")).toBe(true);
    });
  });

  it("revokes another session but never the current one", async () => {
    const { calls } = await renderAuthenticated("/users", userRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "我的会话" }));
    await screen.findByText("s-other");
    const revokeButtons = screen.getAllByRole("button", { name: "撤销" });
    expect(revokeButtons[0]).toBeDisabled();
    fireEvent.click(revokeButtons[1]);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "确认撤销" }));
    await waitFor(() => {
      expect(calls.some(call => call.method === "DELETE" && call.url === "/api/v1/auth/sessions/s-other")).toBe(true);
    });
  });
});
