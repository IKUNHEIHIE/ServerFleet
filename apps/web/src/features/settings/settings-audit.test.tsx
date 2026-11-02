import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { coreRoutes, installFetch, jsonResponse } from "@/test/http";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

async function loginAndOpen(hash: string, routes: Record<string, () => Response>) {
  let meCalls = 0;
  window.location.hash = hash;
  const installed = installFetch({
    ...coreRoutes(),
    "GET /api/v1/auth/me": () => {
      meCalls += 1;
      return meCalls === 1
        ? jsonResponse({ code: "UNAUTHENTICATED", message: "no session" }, 401)
        : jsonResponse({
            userId: "u-admin",
            displayName: "管理员",
            roles: ["admin"],
            permissions: ["audit.read", "credential.manage"],
            isBootstrapAdmin: true,
          });
    },
    ...routes,
  });
  render(<App />);
  fireEvent.change(await screen.findByLabelText("用户名"), { target: { value: "admin" } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "secret" } });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  await screen.findByRole("navigation", { name: "主导航" });
  return installed;
}

describe("audit page", () => {
  it("reads filtered audit events and opens the redacted detail", async () => {
    const { calls } = await loginAndOpen("/audit", {
      "GET /api/v1/audit/events": () =>
        jsonResponse({
          items: [
            {
              eventId: "ev-1",
              occurredAt: "2026-10-30T01:00:00Z",
              actorType: "user",
              actorId: "u-1",
              action: "node.revoke",
              resourceType: "node",
              resourceId: "n-1",
              result: "success",
              requestId: "req-1",
              redactedSummary: "已吊销节点身份",
            },
          ],
          page: { nextCursor: null, limit: 100, hasMore: false },
        }),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "审计日志" })).toBeVisible();
    expect(await screen.findByText("node.revoke")).toBeVisible();
    fireEvent.change(screen.getByLabelText("动作"), { target: { value: "node.revoke" } });
    fireEvent.click(screen.getByRole("button", { name: "应用筛选" }));
    await vi.waitFor(() => {
      expect(calls.some(call => call.url.includes("action=node.revoke"))).toBe(true);
    });
    const detailButton = await screen.findByRole("button", { name: "详情" });
    fireEvent.click(detailButton);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("已吊销节点身份")).toBeVisible();
    expect(within(dialog).getByText("req-1")).toBeVisible();
  });
});

describe("settings credentials", () => {
  it("writes a secret once, never renders it again, and accepts rotate/revoke operations", async () => {
    const { calls } = await loginAndOpen("/settings", {
      "GET /api/v1/credentials": () =>
        jsonResponse([
          {
            id: "cred-1",
            name: "Cloudflare",
            kind: "cloudflare_token",
            status: "valid",
            usageScope: ["dns"],
            createdAt: "2026-10-01T00:00:00Z",
          },
        ]),
      "POST /api/v1/credentials": () =>
        jsonResponse({ id: "cred-2", name: "SMTP", kind: "smtp_password", status: "unverified", usageScope: ["smtp"] }, 201),
      "GET /api/v1/credentials/cred-1/references": () => jsonResponse({ ok: true }),
      "POST /api/v1/credentials/cred-1/rotate": () => jsonResponse({ id: "job-1", state: "accepted" }, 202),
      "POST /api/v1/credentials/cred-1/revoke": () => jsonResponse({ id: "job-2", state: "accepted" }, 202),
    });
    expect(await screen.findByText("Cloudflare")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "添加凭据" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "SMTP" } });
    fireEvent.change(within(dialog).getByLabelText("秘密值"), { target: { value: "smtp-password-value" } });
    fireEvent.change(within(dialog).getByLabelText("允许使用的模块"), { target: { value: "smtp" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存凭据" }));
    await vi.waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/credentials");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body)).secret).toBe("smtp-password-value");
    });
    expect(document.body.textContent).not.toContain("smtp-password-value");

    await waitFor(() => {
      expect(calls.filter(call => call.method === "GET" && call.url === "/api/v1/credentials").length).toBeGreaterThan(1);
    });
    await screen.findByRole("button", { name: "引用" });
    fireEvent.click(screen.getAllByRole("button", { name: "引用" })[0]);
    expect(await screen.findByText(/没有引用清单字段/)).toBeVisible();
    const referencesDialog = await screen.findByRole("dialog", { name: "凭据引用检查" });
    await waitFor(() => expect(referencesDialog.contains(document.activeElement)).toBe(true));
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "凭据引用检查" })).not.toBeInTheDocument());

    fireEvent.click(screen.getAllByRole("button", { name: "轮换" })[0]);
    const rotateDialog = await screen.findByRole("dialog", { name: "轮换凭据" });
    fireEvent.change(within(rotateDialog).getByLabelText("秘密值"), { target: { value: "new-secret" } });
    fireEvent.click(within(rotateDialog).getByRole("button", { name: "提交轮换" }));
    await vi.waitFor(() => {
      expect(calls.some(call => call.url === "/api/v1/credentials/cred-1/rotate")).toBe(true);
    });
    expect(await screen.findByText(/轮换请求已被服务端接受/)).toBeVisible();

    await screen.findByRole("button", { name: "撤销" });
    fireEvent.click(screen.getAllByRole("button", { name: "撤销" })[0]);
    const revokeDialog = await screen.findByRole("dialog");
    fireEvent.click(within(revokeDialog).getByRole("button", { name: "确认撤销" }));
    await vi.waitFor(() => {
      expect(calls.some(call => call.url === "/api/v1/credentials/cred-1/revoke")).toBe(true);
    });
  });
});
