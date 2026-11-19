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
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "SMTP 密码/授权码" }));
    const dialog = await screen.findByRole("dialog", { name: "添加安全凭据" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "SMTP" } });
    fireEvent.change(within(dialog).getByLabelText("秘密值"), { target: { value: "smtp-password-value" } });
    fireEvent.change(within(dialog).getByLabelText("允许使用的模块"), { target: { value: "smtp" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存凭据" }));
    await vi.waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/credentials");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body)).secret).toBe("smtp-password-value");
      expect(new Headers(call?.init?.headers).get("Idempotency-Key")).toMatch(/^[a-zA-Z0-9_.-]{16,128}$/);
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

describe("log cleanup settings", () => {
  it("shows the default and submits the saved schedule and manual cleanup", async () => {
    let policy = { revision: 1, retentionDays: 15, scheduled: false };
    let finished = false;
    const path = "/api/v1/settings/log-cleanup";
    const { calls } = await loginAndOpen("/settings", {
      "GET /api/v1/settings/log-cleanup": () => jsonResponse({
        ...policy,
        lastRun: finished ? { finishedAt: "2026-11-19T00:00:00Z", auditEvents: 12, taskLogs: 3 } : undefined,
      }),
      "PUT /api/v1/settings/log-cleanup": () => {
        policy = { revision: 2, retentionDays: 30, scheduled: true };
        return jsonResponse(policy);
      },
      "POST /api/v1/settings/log-cleanup/run": () => {
        finished = true;
        return jsonResponse(policy, 202);
      },
    });
    fireEvent.click(screen.getByRole("tab", { name: "日志清理" }));
    const input = await screen.findByLabelText("保留天数");
    expect(input).toHaveValue(15);
    expect(screen.getByRole("checkbox", { name: "每天自动清理" })).not.toBeChecked();
    fireEvent.change(input, { target: { value: "30" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "每天自动清理" }));
    fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    expect(await screen.findByText("保留最近 30 天的日志。")).toBeVisible();
    expect(JSON.parse(String(calls.find(call => call.method === "PUT" && call.url === path)?.init?.body)))
      .toEqual({ expectedRevision: 1, retentionDays: 30, scheduled: true });
    fireEvent.click(screen.getByRole("button", { name: "一键清理日志" }));
    const dialog = await screen.findByRole("dialog", { name: "清理过期日志" });
    expect(calls.some(call => call.method === "POST" && call.url === path + "/run")).toBe(false);
    fireEvent.click(within(dialog).getByRole("button", { name: "确认清理" }));
    expect(await screen.findByText(/已删除 12 条审计事件和 3 条任务日志/)).toBeVisible();
    expect(JSON.parse(String(calls.find(call => call.method === "POST" && call.url === path + "/run")?.init?.body)))
      .toEqual({ expectedRevision: 2 });
  });
});
