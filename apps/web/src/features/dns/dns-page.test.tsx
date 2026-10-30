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
    "GET /api/v1/dns/zones/z-1/records": () =>
      jsonResponse({ items: [{ id: "r-1", zoneId: "z-1", type: "A", name: "www", content: "203.0.113.10", ttl: 120, proxied: false }] }),
    "GET /api/v1/dns/zones/z-2/records": () => jsonResponse({ items: [] }),
    "POST /api/v1/dns/zones/z-1/records": (_init) =>
      jsonResponse({ id: "r-2", zoneId: "z-1", type: "A", name: "api", content: "203.0.113.11" }, 201),
    "POST /api/v1/dns/accounts": (_init) =>
      jsonResponse({ id: "a-3", providerCode: "cloudflare", displayName: "New", enabled: true, isDefault: false, credentialStatus: "pending_verification" }, 201),
    "POST /api/v1/dns/accounts/a-1/test": () => jsonResponse({ status: "success", zoneRead: true, recordWrite: true }),
  };
}

describe("dns page", () => {
  it("selects accounts/zones independently and creates a record with a scoped zone", async () => {
    const { calls } = await renderAuthenticated("/dns", dnsRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "DNS 管理" })).toBeVisible();
    expect(await screen.findByText("example.com")).toBeVisible();
    expect(await screen.findByText("203.0.113.10")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: /Second/ }));
    expect(await screen.findByText("other.com")).toBeVisible();
    await waitFor(() => {
      expect(calls.some(call => call.url.startsWith("/api/v1/dns/accounts/a-2/zones"))).toBe(true);
    });

    fireEvent.click(screen.getByRole("button", { name: /Main/ }));
    expect(await screen.findByText("example.com")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "添加记录" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "api" } });
    fireEvent.change(within(dialog).getByLabelText("内容"), { target: { value: "203.0.113.11" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存记录" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/zones/z-1/records");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ type: "A", name: "api", content: "203.0.113.11" });
    });
    expect(await screen.findByText("DNS 记录已保存（服务端结果）。")).toBeVisible();
  });

  it("writes an API token once and reports the scoped connection test", async () => {
    const { calls } = await renderAuthenticated("/dns", dnsRoutes());
    fireEvent.click(await screen.findByRole("button", { name: "添加账号" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("显示名称"), { target: { value: "New" } });
    fireEvent.change(within(dialog).getByLabelText("Cloudflare API Token"), { target: { value: "cf-token-secret" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存账号" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/accounts");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ providerCode: "cloudflare", displayName: "New", apiToken: "cf-token-secret" });
    });
    expect(document.body.textContent).not.toContain("cf-token-secret");

    const checkButtons = await screen.findAllByRole("button", { name: "检查" });
    fireEvent.click(checkButtons[0]);
    expect(await screen.findByText(/Zone 读取 通过 · 记录写入 通过/)).toBeVisible();
  });
});
