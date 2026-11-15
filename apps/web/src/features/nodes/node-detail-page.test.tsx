import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IDENTITY, jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function detailRoutes(): FetchRoutes {
  return {
    "GET /api/v1/nodes/n-1": () =>
      jsonResponse({
        nodeId: "n-1",
        displayName: "Tokyo",
        address: "203.0.113.7",
        region: "ap-northeast",
        provider: "aws",
        countryCode: "JP",
        tags: ["production"],
        updatedAt: "2026-11-15T00:00:00Z",
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

  it("submits suspend and revoke with reason and CSRF without inventing revision", async () => {
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

  it("keeps a real detail 404 visible and independently loads diagnostic resources", async () => {
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1"] = () => jsonResponse({ error: { code: "not_found" } }, 404);
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    expect(await screen.findByText("节点详情加载失败")).toBeVisible();
    expect(screen.getByRole("button", { name: "编辑节点" })).toBeDisabled();
    expect(await screen.findByText("aa:bb")).toBeVisible();
    expect(screen.getByText("失联（不能据此推断关机）")).toBeVisible();
    expect(calls.some(call => call.url.startsWith("/api/v1/nodes?"))).toBe(false);
  });

  it("reloads node resources after granting access even when identity is unchanged", async () => {
    let granted = false;
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1"] = () => granted
      ? jsonResponse({ nodeId: "n-1", displayName: "Newly visible", status: "online", updatedAt: "2026-11-15T00:00:00Z" })
      : jsonResponse({ code: "FORBIDDEN" }, 403);
    routes["POST /api/v1/resource-grants"] = () => {
      granted = true;
      return jsonResponse({ id: "grant-new" }, 201);
    };
    await renderAuthenticated("/nodes/n-1", routes, { ...IDENTITY, isBootstrapAdmin: false, roles: ["administrator"] });
    expect(await screen.findByText("无权限")).toBeVisible();
    fireEvent.click(screen.getByLabelText("编辑节点资料"));
    fireEvent.click(screen.getByRole("button", { name: "授权当前账户管理此节点" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Newly visible" })).toBeVisible();
    expect(screen.queryByText("无权限")).not.toBeInTheDocument();
  });

  it("saves complete metadata with the original CAS timestamp and CSRF", async () => {
    const routes = detailRoutes();
    routes["PATCH /api/v1/nodes/n-1"] = () => jsonResponse({ nodeId: "n-1" });
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    fireEvent.click(screen.getByRole("button", { name: "编辑节点" }));
    const dialog = screen.getByRole("dialog", { name: "编辑节点" });
    expect(within(dialog).getByLabelText("地址")).toHaveValue("203.0.113.7");
    for (const [label, value] of [["名称", "Osaka"], ["地址", "203.0.113.9"], ["地区", "ap-west"], ["云厂商", "other"], ["国家码", "cn"], ["标签", "web, production, web"]] as const) {
      fireEvent.change(within(dialog).getByLabelText(label), { target: { value } });
    }
    fireEvent.click(within(dialog).getByRole("button", { name: "保存节点" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/nodes/n-1");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({
        name: "Osaka", address: "203.0.113.9", region: "ap-west", provider: "other", countryCode: "CN",
        tags: ["web", "production"], expectedUpdatedAt: "2026-11-15T00:00:00Z",
      });
      expect(new Headers(call?.init?.headers).get("X-CSRF-Token")).toBe("csrf-test");
    });
    expect(await screen.findByText("节点信息已保存。")).toBeVisible();
  });

  it("locks metadata saving without updatedAt and offers a refresh", async () => {
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1"] = () => jsonResponse({ nodeId: "n-1", displayName: "Tokyo", status: "offline" });
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    fireEvent.click(screen.getByRole("button", { name: "编辑节点" }));
    expect(screen.getByText(/当前信息缺少更新时间/)).toBeVisible();
    expect(screen.getByRole("button", { name: "保存节点" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "保存节点" }));
    expect(calls.some(call => call.method === "PATCH")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "刷新节点信息" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(calls.filter(call => call.url === "/api/v1/nodes/n-1").length).toBeGreaterThan(1);
  });

  it("keeps a metadata CAS conflict visible and requires refresh before another write", async () => {
    const routes = detailRoutes();
    routes["PATCH /api/v1/nodes/n-1"] = () => jsonResponse({ code: "NODE_CONFLICT" }, 409);
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    fireEvent.click(screen.getByRole("button", { name: "编辑节点" }));
    fireEvent.click(screen.getByRole("button", { name: "保存节点" }));
    expect(await screen.findByText("保存失败")).toBeVisible();
    expect(screen.getByRole("dialog", { name: "编辑节点" })).toBeVisible();
    expect(screen.getByRole("button", { name: "保存节点" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "刷新节点信息" })).toBeEnabled();
    expect(calls.filter(call => call.method === "PATCH")).toHaveLength(1);
  });

  it("requires reason and explicit removal acknowledgement, then deletes with CSRF", async () => {
    const routes = detailRoutes();
    routes["DELETE /api/v1/nodes/n-1"] = () => new Response(null, { status: 204 });
    routes["GET /api/v1/nodes"] = () => jsonResponse({ items: [] });
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    fireEvent.click(screen.getByRole("button", { name: "删除节点" }));
    const dialog = screen.getByRole("dialog", { name: "删除节点" });
    const confirm = within(dialog).getByRole("button", { name: "确认删除" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("原因"), { target: { value: "   " } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "停止此节点接入并从列表移除，保留历史审计；重新使用须重新接入" }));
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("原因"), { target: { value: "  设备退役  " } });
    expect(confirm).toBeEnabled();
    fireEvent.click(within(dialog).getByRole("checkbox"));
    expect(confirm).toBeDisabled();
    expect(calls.some(call => call.method === "DELETE")).toBe(false);
    fireEvent.click(within(dialog).getByRole("checkbox"));
    fireEvent.click(confirm);
    await waitFor(() => {
      const call = calls.find(call => call.method === "DELETE");
      expect(call?.url).toBe("/api/v1/nodes/n-1");
      expect(JSON.parse(String(call?.init?.body))).toEqual({ reason: "设备退役" });
      expect(new Headers(call?.init?.headers).get("X-CSRF-Token")).toBe("csrf-test");
      expect(window.location.hash).toBe("#/nodes");
    });
    expect(calls.some(call => call.url.endsWith("/revoke"))).toBe(false);
  });

  it("keeps deletion failure in the confirmation dialog without removing the node", async () => {
    const routes = detailRoutes();
    routes["DELETE /api/v1/nodes/n-1"] = () => jsonResponse({ code: "REMOVE_FAILED" }, 500);
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    fireEvent.click(screen.getByRole("button", { name: "删除节点" }));
    fireEvent.change(screen.getByLabelText("原因"), { target: { value: "退役" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "停止此节点接入并从列表移除，保留历史审计；重新使用须重新接入" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    expect(await screen.findByText("操作未完成")).toBeVisible();
    expect(screen.getByRole("dialog", { name: "删除节点" })).toBeVisible();
    expect(screen.getByLabelText("原因")).toHaveValue("退役");
    expect(window.location.hash).toBe("#/nodes/n-1");
    expect(calls.filter(call => call.method === "DELETE")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "删除节点" }));
    expect(screen.getByLabelText("原因")).toHaveValue("");
    expect(within(screen.getByRole("dialog", { name: "删除节点" })).getByRole("checkbox")).not.toBeChecked();
    expect(screen.queryByText("操作未完成")).not.toBeInTheDocument();
  });

  it.each([
    { permissions: [], edit: false, suspend: false, revoke: false },
    { permissions: ["node.manage"], edit: true, suspend: false, revoke: false },
    { permissions: ["node.suspend"], edit: false, suspend: true, revoke: false },
    { permissions: ["node.revoke"], edit: false, suspend: false, revoke: true },
  ])("checks independent write permissions: $permissions", async ({ permissions, edit, suspend, revoke }) => {
    const { calls } = await renderAuthenticated("/nodes/n-1", detailRoutes(), {
      ...IDENTITY, isBootstrapAdmin: false, roles: [], permissions,
    });
    await screen.findByText("aa:bb");
    expect(screen.getByRole("button", { name: "编辑节点" }).hasAttribute("disabled")).toBe(!edit);
    expect(screen.getByRole("button", { name: "暂停派发" }).hasAttribute("disabled")).toBe(!suspend);
    expect(screen.getByRole("button", { name: "吊销身份" }).hasAttribute("disabled")).toBe(!revoke);
    expect(screen.getByRole("button", { name: "删除节点" }).hasAttribute("disabled")).toBe(!revoke);
    if (permissions.length === 0) {
      for (const name of ["编辑节点", "暂停派发", "吊销身份", "删除节点", "编辑配额"]) {
        expect(screen.getByRole("button", { name })).toBeDisabled();
        fireEvent.click(screen.getByRole("button", { name }));
      }
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    }
    expect(calls.some(call => ["PUT", "DELETE"].includes(call.method) || call.url.endsWith("/suspend") || call.url.endsWith("/revoke"))).toBe(false);
  });

  it("locks node writes when the session has no CSRF token", async () => {
    const routes = detailRoutes();
    routes["POST /api/v1/auth/login"] = () => jsonResponse({ identity: IDENTITY, sessionExpiresAt: "2026-12-31T23:59:59Z" });
    await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    for (const name of ["编辑节点", "删除节点", "暂停派发", "吊销身份"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
  });

  it("refreshes diagnostics every five seconds and exposes a silent refresh failure", async () => {
    let failed = false;
    const routes = detailRoutes();
    routes["GET /api/v1/nodes/n-1/diagnostics"] = () => failed
      ? jsonResponse({ code: "UNAVAILABLE" }, 503)
      : jsonResponse({ nodeId: "n-1", connectionState: "online" });
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const { calls } = await renderAuthenticated("/nodes/n-1", routes);
    await screen.findByText("aa:bb");
    const before = calls.filter(call => call.url.endsWith("/diagnostics")).length;
    failed = true;
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(screen.getByText(/最近一次刷新失败/)).toBeVisible();
    expect(calls.filter(call => call.url.endsWith("/diagnostics")).length).toBeGreaterThan(before);
  });
});
