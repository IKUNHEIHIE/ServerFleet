import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function dnsRoutes(): FetchRoutes {
  return {
    "GET /api/v1/dns/providers": () =>
      jsonResponse({ items: [{ code: "cloudflare", displayName: "Cloudflare", supported: true, capabilities: { supportsZoneSync: true } }] }),
    "GET /api/v1/dns/accounts": () =>
      jsonResponse({
        items: [
          { id: "a-1", providerCode: "cloudflare", displayName: "Main", enabled: true, isDefault: true, credentialStatus: "valid", zoneCount: 1 },
          { id: "a-2", providerCode: "cloudflare", displayName: "Second", enabled: true, isDefault: false, credentialStatus: "valid", zoneCount: 1 },
        ],
      }),
    "GET /api/v1/dns/accounts/a-1/zones": () => jsonResponse({ items: [{ id: "z-1", accountId: "a-1", name: "example.com" }] }),
    "GET /api/v1/dns/accounts/a-2/zones": () => jsonResponse({ items: [{ id: "z-2", accountId: "a-2", name: "other.com" }] }),
    "GET /api/v1/dns/accounts/a-1/provider-zones": () =>
      jsonResponse({ items: [{ providerZoneId: "pz-1", name: "example.com", status: "active" }] }),
    "POST /api/v1/dns/accounts/a-1/zones/sync": () => jsonResponse({ id: "z-9", accountId: "a-1", name: "example.com", status: "active" }),
    "POST /api/v1/dns/zones/z-9/records/sync": () => jsonResponse({ items: [{ id: "r-9", zoneId: "z-9", type: "A", name: "app", content: "203.0.113.50", proxied: false }] }),
    "POST /api/v1/credentials": () => jsonResponse({ id: "cred-new", name: "DNS New", kind: "cloudflare_token", status: "unverified", usageScope: ["dns"], revision: 1 }, 201),
    "GET /api/v1/dns/zones/z-1/records": () =>
      jsonResponse({ items: [{ id: "r-1", zoneId: "z-1", type: "A", name: "www", content: "203.0.113.10", ttl: 120, proxied: false }] }),
    "GET /api/v1/dns/zones/z-2/records": () => jsonResponse({ items: [] }),
    "POST /api/v1/dns/zones/z-1/records": () => jsonResponse({ id: "r-new", zoneId: "z-1", type: "A", name: "api", content: "203.0.113.11", proxied: false }, 201),
    "POST /api/v1/dns/record-tasks": () => jsonResponse({taskId:"dns-record-task",status:"awaiting_approval",planHash:"a".repeat(64)},202),
    "POST /api/v1/dns/provider-tasks": () => jsonResponse({taskId:"dns-check-task",status:"queued"},202),
    "POST /api/v1/dns/accounts": (_init) =>
      jsonResponse({ id: "a-3", providerCode: "cloudflare", displayName: "New", enabled: true, isDefault: false, credentialStatus: "pending_verification" }, 201),
    "POST /api/v1/dns/accounts/a-1/test": () => jsonResponse({ status: "success", zoneRead: true, recordWrite: true }),
  };
}

describe("dns page", () => {
  it("shows the default account zones and creates a record through the DNS account module", async () => {
    const { calls } = await renderAuthenticated("/dns", dnsRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "DNS 管理" })).toBeVisible();
    expect(await screen.findByText("example.com")).toBeVisible();
    expect(await screen.findByText("203.0.113.10")).toBeVisible();
    expect(calls.some(call => call.url === "/api/v1/dns/accounts/a-1/zones")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "添加记录" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(".example.com")).toBeVisible();
    fireEvent.change(within(dialog).getByLabelText("记录名称"), { target: { value: "api" } });
    fireEvent.change(within(dialog).getByLabelText("内容"), { target: { value: "203.0.113.11" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "添加记录" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/zones/z-1/records");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ type: "A", name: "api", content: "203.0.113.11", proxied: false });
    });
    expect(await screen.findByText(/已保存记录 A api/)).toBeVisible();
    expect(calls.some(call => call.url === "/api/v1/dns/record-tasks")).toBe(false);
  });

  it("creates a DNS credential from a pasted token and binds only its reference", async () => {
    const { calls } = await renderAuthenticated("/settings", dnsRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "DNS 账号" }));
    fireEvent.click(await screen.findByRole("button", { name: "添加账号" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("显示名称"), { target: { value: "New" } });
    fireEvent.change(within(dialog).getByLabelText("Cloudflare API Token"), { target: { value: "cfut-test-token" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存账号" }));
    await waitFor(() => {
      const credential = calls.find(call => call.method === "POST" && call.url === "/api/v1/credentials");
      expect(credential).toBeDefined();
      expect(JSON.parse(String(credential?.init?.body))).toMatchObject({
        name: "DNS New", kind: "cloudflare_token", usageScope: ["dns"], secret: "cfut-test-token",
      });
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/accounts");
      expect(call).toBeDefined();
      // 真实 Controller（internal/dns/http.go）：凭据只以 credentialRef 引用提交，不传输明文。
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ displayName: "New", credentialRef: "cred-new", credentialRevision: 1, makeDefault: false });
    });
    expect(document.body.textContent).not.toContain("cfut-test-token");

    const checkButtons = await screen.findAllByRole("button", { name: "检查" });
    fireEvent.click(checkButtons[0]);
    expect(await screen.findByText(/凭据检查：通过 · Zone 读取 通过 · 记录写入 通过/)).toBeVisible();
    expect(calls.some(call => call.method === "POST" && call.url === "/api/v1/dns/accounts/a-1/test")).toBe(true);
  });

  it("imports a provider zone from the dialog and refreshes its records", async () => {
    const { calls } = await renderAuthenticated("/dns", dnsRoutes());
    await screen.findByText("example.com");
    fireEvent.click(screen.getByRole("button", { name: "导入 Zone" }));
    const dialog = await screen.findByRole("dialog", { name: "从 Cloudflare 导入 Zone" });
    const select = await within(dialog).findByLabelText("可用 Zone");
    expect(await within(select).findByRole("option", { name: "example.com · active" })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "导入并刷新记录" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/accounts/a-1/zones/sync");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ providerZoneId: "pz-1" });
    });
    expect(await screen.findByText(/Zone 已导入：example.com，共 1 条记录/)).toBeVisible();
    expect(calls.some(call => call.url === "/api/v1/dns/accounts/a-1/provider-zones")).toBe(true);
    expect(calls.some(call => call.method === "POST" && call.url === "/api/v1/dns/zones/z-9/records/sync")).toBe(true);
  });
});
