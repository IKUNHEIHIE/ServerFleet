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
    "GET /api/v1/availability-monitors/m-1/checks": () => jsonResponse({ ok: true }),
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

  it("creates, pauses and reports the missing history contract for service monitors", async () => {
    const { calls } = await renderAuthenticated("/monitoring", monitorRoutes());
    fireEvent.click(await screen.findByRole("tab", { name: "服务监控" }));
    expect(await screen.findByText("Web")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "检查历史" }));
    expect(await screen.findByText(/ok=true/)).toBeVisible();
    expect(screen.getByText(/契约没有检查历史\/探测源状态字段/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "暂停" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/availability-monitors/m-1");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ state: "paused" });
    });

    fireEvent.click(screen.getByRole("button", { name: "新建监控" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "API" } });
    fireEvent.change(within(dialog).getByLabelText("探测目标"), { target: { value: "api:443" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存监控" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/availability-monitors");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ name: "API", target: "api:443", intervalSeconds: 60, timeoutSeconds: 5 });
    });
    expect(await screen.findByText("监控已保存（服务端结果）。")).toBeVisible();
  });
});
