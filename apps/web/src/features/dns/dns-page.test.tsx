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
    "POST /api/v1/dns/record-tasks": () => jsonResponse({taskId:"dns-record-task",status:"awaiting_approval",planHash:"a".repeat(64)},202),
    "POST /api/v1/dns/inspection-tasks": () => jsonResponse({taskId:"dns-check-task",status:"queued"},202),
    "POST /api/v1/dns/accounts": (_init) =>
      jsonResponse({ id: "a-3", providerCode: "cloudflare", displayName: "New", enabled: true, isDefault: false, credentialStatus: "pending_verification" }, 201),
    "POST /api/v1/dns/accounts/a-1/test": () => jsonResponse({ status: "success", zoneRead: true, recordWrite: true }),
  };
}

describe("dns page", () => {
  it("selects accounts/zones independently and submits an approval task without claiming a record write", async () => {
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
    fireEvent.click(within(dialog).getByRole("button", { name: "登记审批任务" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/record-tasks");
      expect(call).toBeDefined();
      const body=JSON.parse(String(call?.init?.body));
      expect(body).toMatchObject({operation:"create",zoneId:"z-1",idempotencyKey:expect.any(String)});
      expect(JSON.parse(body.recordJson)).toMatchObject({type:"A",name:"api",content:"203.0.113.11"});
    });
    expect(await screen.findByText(/DNS 修改审批任务已登记：dns-record-task/)).toBeVisible();
    expect(calls.some(call=>call.method==="POST"&&call.url==="/api/v1/dns/zones/z-1/records")).toBe(false);
    expect(screen.queryByText("203.0.113.11")).not.toBeInTheDocument();
  });

  it("binds an existing credential reference and reports an inspection task without claiming provider success", async () => {
    const { calls } = await renderAuthenticated("/dns", dnsRoutes());
    fireEvent.click(await screen.findByRole("button", { name: "添加账号" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("显示名称"), { target: { value: "New" } });
    fireEvent.change(within(dialog).getByLabelText("Cloudflare 凭据 ID"), { target: { value: "credential-ref-1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存账号" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/dns/accounts");
      expect(call).toBeDefined();
      // 真实 Controller（internal/dns/http.go）：凭据只以 credentialRef 引用提交，不传输明文。
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ displayName: "New", credentialRef: "credential-ref-1", makeDefault: false });
    });
    expect(document.body.textContent).not.toContain("credential-ref-1");

    const checkButtons = await screen.findAllByRole("button", { name: "检查" });
    fireEvent.click(checkButtons[0]);
    expect(await screen.findByText(/检查任务 dns-check-task · queued/)).toBeVisible();
    expect(screen.queryByText(/Zone 读取 通过 · 记录写入 通过/)).not.toBeInTheDocument();
    expect(calls.some(call=>call.url==="/api/v1/dns/accounts/a-1/test")).toBe(false);
  });
});
