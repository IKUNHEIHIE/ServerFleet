import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function monitorRoutes(): FetchRoutes {
  return {
    "GET /api/v1/availability-monitors": () =>
      jsonResponse([
        { id: "m-1", name: "Web", type: "http", target: "https://example.com", intervalSeconds: 60, timeoutSeconds: 5, failureCount: 3, recoveryCount: 2, state: "healthy", notifyChannels: ["smtp"] },
      ]),
    "GET /api/v1/nodes": () => jsonResponse({ items: [{ id: "n-1", name: "Tokyo", online: true, cpuPercent: 12, memoryPercent: 34 }] }),
    "GET /api/v1/traffic-risks": () => jsonResponse([{ nodeId: "n-1", kind: "quota", severity: "warning", estimated: true }]),
    "GET /api/v1/notifications/channels": () => jsonResponse({ items: [] }),
    "GET /api/v1/notifications/deliveries": () => jsonResponse({ items: [] }),
    "GET /api/v1/availability-monitors/m-1/history": () =>
      jsonResponse({
        monitorId: "m-1",
        limit: 20,
        items: [
          { id: 7, monitorId: "m-1", status: "down", latencyMs: null, statusCode: null, error: "connection refused", checkedAt: "2026-10-31T01:10:00Z" },
          { id: 6, monitorId: "m-1", status: "up", latencyMs: 42.5, statusCode: 200, checkedAt: "2026-10-31T01:09:00Z" },
        ],
      }),
    "POST /api/v1/availability-monitors/m-1/probe": () =>
      jsonResponse({ monitorId: "m-1", status: "up", state: "healthy", latencyMs: 38.2, statusCode: 200, checkedAt: "2026-10-31T01:11:00Z", consecutiveFailures: 0 }),
    "PATCH /api/v1/availability-monitors/m-1": (_init) => jsonResponse({ id: "m-1", name: "Web", type: "http", target: "https://example.com", intervalSeconds: 60, timeoutSeconds: 5, state: "paused" }),
    "POST /api/v1/availability-monitors": (_init) => jsonResponse({ id: "m-2", name: "API", type: "tcp", target: "api:443", intervalSeconds: 60, timeoutSeconds: 5 }, 201),
    "DELETE /api/v1/availability-monitors/m-1": () => jsonResponse({ ok: true }),
  };
}

describe("monitoring page", () => {
  it("shows current resources and traffic risks without inventing history", async () => {
    await renderAuthenticated("/monitoring", monitorRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "监控告警" })).toBeVisible();
    expect(await screen.findByText("Tokyo")).toBeVisible();
    expect(screen.getByText(/quota\(warning，估算\)/)).toBeVisible();
    expect(screen.getByText(/没有历史指标\/告警规则端点/)).toBeVisible();
  });

  it("creates, pauses and shows the real check history for service monitors", async () => {
    const { calls } = await renderAuthenticated("/monitoring", monitorRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "服务监控" }));
    expect(await screen.findByText("Web")).toBeVisible();

    // 检查历史：真实 history 端点；时间、状态、延迟与错误摘要均来自服务端记录。
    fireEvent.click(screen.getByRole("button", { name: "检查历史" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText("up（可达）")).toBeVisible();
    expect(within(dialog).getByText("down（不可达）")).toBeVisible();
    expect(within(dialog).getByText("42.5 ms")).toBeVisible();
    expect(within(dialog).getByText("connection refused")).toBeVisible();
    await waitFor(() => {
      expect(calls.some(call => call.method === "GET" && call.url.includes("/availability-monitors/m-1/history"))).toBe(true);
    });
    fireEvent.click(within(dialog).getAllByRole("button", { name: "关闭" })[0]);

    fireEvent.click(screen.getByRole("button", { name: "暂停" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/availability-monitors/m-1");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ state: "paused" });
    });

    fireEvent.click(screen.getByRole("button", { name: "新建监控" }));
    const editorDialog = await screen.findByRole("dialog");
    fireEvent.change(within(editorDialog).getByLabelText("名称"), { target: { value: "API" } });
    fireEvent.change(within(editorDialog).getByLabelText("探测目标"), { target: { value: "api:443" } });
    fireEvent.click(within(editorDialog).getByRole("button", { name: "保存监控" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/availability-monitors");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ name: "API", target: "api:443", intervalSeconds: 60, timeoutSeconds: 5 });
    });
    expect(await screen.findByText("监控已保存（服务端结果）。")).toBeVisible();
  });

  it("runs an immediate probe and surfaces the server check result", async () => {
    const { calls } = await renderAuthenticated("/monitoring", monitorRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "服务监控" }));
    expect(await screen.findByText("Web")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "立即探测" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/availability-monitors/m-1/probe");
      expect(call).toBeDefined();
    });
    expect(await screen.findByText(/立即探测完成：up（可达） · 延迟 38.2 ms · 状态码 200/)).toBeVisible();
  });

  it("reports a missing prober (503 prober_unavailable) instead of pretending to probe", async () => {
    await renderAuthenticated("/monitoring", {
      ...monitorRoutes(),
      "POST /api/v1/availability-monitors/m-1/probe": () =>
        jsonResponse({ error: { code: "prober_unavailable", message: "monitoring prober is not configured" } }, 503),
    });
    fireEvent.click(await screen.findByRole("tab", { name: "服务监控" }));
    fireEvent.click(await screen.findByRole("button", { name: "立即探测" }));
    expect(await screen.findByText(/HTTP 503 · 错误码 prober_unavailable/)).toBeVisible();
  });

  it("shows an honest empty state when the monitor has no stored checks", async () => {
    await renderAuthenticated("/monitoring", {
      ...monitorRoutes(),
      "GET /api/v1/availability-monitors/m-1/history": () => jsonResponse({ monitorId: "m-1", limit: 20, items: [] }),
    });
    fireEvent.click(await screen.findByRole("tab", { name: "服务监控" }));
    fireEvent.click(await screen.findByRole("button", { name: "检查历史" }));
    expect(await screen.findByText("暂无检查记录")).toBeVisible();
    expect(screen.getByText(/不会伪造历史/)).toBeVisible();
  });
});
