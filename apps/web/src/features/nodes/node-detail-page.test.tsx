import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function detailRoutes(): FetchRoutes {
  return {
    "GET /api/v1/nodes/n-1": () =>
      jsonResponse({
        nodeId: "n-1",
        displayName: "Tokyo",
        status: "online",
        revision: 2,
        platform: "linux/amd64",
        agentVersion: "0.9.0",
        lastSeenAt: "2026-10-30T01:00:00Z",
      }),
    "GET /api/v1/nodes/n-1/identity": () =>
      jsonResponse({
        nodeId: "n-1",
        revoked: false,
        certificateFingerprintSha256: "aa:bb",
        serial: "42",
        notAfter: "2027-01-01T00:00:00Z",
        lastVerifiedAt: "2026-10-30T01:00:00Z",
      }),
    "GET /api/v1/nodes/n-1/capabilities": () =>
      jsonResponse({ nodeId: "n-1", protocolMajor: 1, protocolMinor: 2, capabilities: ["exec", "files"] }),
    "GET /api/v1/nodes/n-1/diagnostics": () =>
      jsonResponse({
        nodeId: "n-1",
        connectionState: "unreachable",
        lastHeartbeatAt: "2026-10-30T00:58:00Z",
        identityVerified: true,
        pendingTasks: [{ id: "t-1", expired: false, requiresReapproval: true }],
        diagnosticNotes: ["最近一次心跳超时"],
      }),
    "GET /api/v1/nodes/n-1/traffic-quota": () =>
      jsonResponse({
        nodeId: "n-1",
        quotaGb: 100,
        cycleResetDay: 5,
        timezone: "Asia/Shanghai",
        direction: "combined",
        source: "agent_estimate",
        dataComplete: false,
        thresholdsPct: [70, 85, 95],
        predictionConfidence: "insufficient",
      }),
    "POST /api/v1/nodes/n-1/suspend": () =>
      jsonResponse({ nodeId: "n-1", displayName: "Tokyo", status: "suspended", revision: 3 }),
    "POST /api/v1/nodes/n-1/revoke": () => jsonResponse({ nodeId: "n-1", revoked: true }),
  };
}


function detailRoutesWithoutQuota(): FetchRoutes {
  const routes = detailRoutes();
  routes["GET /api/v1/nodes/n-1/traffic-quota"] = () =>
    jsonResponse({ error: { code: "quota_not_found", message: "no traffic quota is configured for this node" } }, 404);
  routes["PUT /api/v1/nodes/n-1/traffic-quota"] = () =>
    jsonResponse({ nodeId: "n-1", quotaBytes: 200000000000, quotaGb: 200, warningPercent: 70, thresholdsPct: [70], direction: "combined" });
  return routes;
}

describe("node detail page", () => {
  it("shows server detail, diagnostics and quota without inventing missing data", async () => {
    await renderAuthenticated("/nodes/n-1", detailRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "Tokyo" })).toBeVisible();
    expect((await screen.findAllByText("linux/amd64")).length).toBeGreaterThan(0);
    expect(await screen.findByText("失联（不能据此推断关机）")).toBeVisible();
    expect(screen.getByText("危险任务需重新审批")).toBeVisible();
    expect(await screen.findByText("缺失（离线或未采集）")).toBeVisible();
    expect(screen.getByText(/估算值不等同云厂商账单/)).toBeVisible();
    expect(screen.getByText("aa:bb")).toBeVisible();
  });

  it("submits suspend and revoke with reason, revision and CSRF", async () => {
    const { calls } = await renderAuthenticated("/nodes/n-1", detailRoutes());
    await screen.findByText("aa:bb");

    fireEvent.click(screen.getByRole("button", { name: "暂停派发" }));
    expect(await screen.findByRole("heading", { level: 2, name: "暂停节点任务派发" })).toBeVisible();
    const confirm = screen.getByRole("button", { name: "确认执行" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText("原因"), { target: { value: "维护窗口" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/nodes/n-1/suspend");
      expect(call).toBeDefined();
      // 真实 Controller 拒绝 expectedRevision（revision_not_supported）；只提交 reason（GM-W4-02）。
      expect(JSON.parse(String(call?.init?.body))).toEqual({ reason: "维护窗口" });
      expect(new Headers(call?.init?.headers).get("X-CSRF-Token")).toBe("csrf-test");
    });
    expect(await screen.findByText("已提交暂停请求（服务端结果）。")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "吊销身份" }));
    expect(await screen.findByRole("heading", { level: 2, name: "吊销节点身份" })).toBeVisible();
    fireEvent.change(screen.getByLabelText("原因"), { target: { value: "设备丢失" } });
    fireEvent.click(screen.getByLabelText(/我理解吊销身份不可恢复/));
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/nodes/n-1/revoke");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ reason: "设备丢失" });
    });
    expect(await screen.findByText("节点身份已吊销（服务端结果）。")).toBeVisible();
  });

  it("treats a 404 quota_not_found as not-configured and still allows first-time editing (GM-W4-02)", async () => {
    const { calls } = await renderAuthenticated("/nodes/n-1", detailRoutesWithoutQuota());
    await screen.findByText("aa:bb");
    expect(await screen.findByText(/服务端尚未配置该节点的流量配额/)).toBeVisible();
    const edit = screen.getByRole("button", { name: "编辑配额" });
    expect(edit).toBeEnabled();

    fireEvent.click(edit);
    expect(await screen.findByRole("heading", { level: 2, name: "编辑流量配额与到期提醒" })).toBeVisible();
    const save = screen.getByRole("button", { name: "保存配额" });
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("配额 (GB)"), { target: { value: "200" } });
    fireEvent.click(save);
    await waitFor(() => {
      const call = calls.find(call => call.method === "PUT" && call.url === "/api/v1/nodes/n-1/traffic-quota");
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body.quotaGb).toBe(200);
      expect(body.direction).toBe("combined");
    });
    expect(await screen.findByText("流量配额已保存（服务端结果）。")).toBeVisible();
  });

  it("does not offer quota editing while the quota read fails for a non-404 reason", async () => {
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1/traffic-quota"] = () => jsonResponse({ error: { code: "INTERNAL" } }, 500);
    await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    expect(await screen.findByText("流量配额加载失败")).toBeVisible();
    expect(screen.getByRole("button", { name: "编辑配额" })).toBeDisabled();
  });

  it("falls back to the real list summary when the controller has no detail route (GM-W4-02)", async () => {
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1"] = () => jsonResponse({ error: { code: "not_found" } }, 404);
    routes["GET /api/v1/nodes"] = () =>
      jsonResponse({ items: [{ id: "n-1", name: "Tokyo", online: true, region: "ap-northeast", ip: "203.0.113.7" }] });
    await renderAuthenticated("/nodes/n-1", routes);
    expect(await screen.findByText("详情端点未提供")).toBeVisible();
    expect(await screen.findByText("203.0.113.7")).toBeVisible();
    // 子资源不再被详情 404 阻塞：身份/能力/诊断仍来自真实子资源端点。
    expect(await screen.findByText("aa:bb")).toBeVisible();
    expect(screen.getByText("失联（不能据此推断关机）")).toBeVisible();
  });
});
