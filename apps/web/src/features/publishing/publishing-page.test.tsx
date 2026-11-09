import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";
import type { AsyncOperation } from "@/lib/api/dto";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const GROUP = {
  id: "sg-1",
  revision: 4,
  name: "Web",
  memberNodeIds: ["n-1"],
  service: { port: 443, protocol: "https" },
  loadBalancing: { enabled: true, strategy: "round_robin" },
};

const GATEWAY = {
  id: "gw-1",
  nodeId: "n-1",
  name: "Edge",
  online: true,
  status: "ready",
  nginxInstalled: true,
  checksPassed: true,
  enabled: true,
  schedulingEligible: true,
};

const SEEDED_PUBLICATION: AsyncOperation = {
  id: "pub-seed",
  status: "queued",
  createdAt: "2026-10-30T01:00:00Z",
  stages: [
    { name: "dns", status: "queued" },
    { name: "certificate", status: "queued" },
    { name: "gateway", status: "queued" },
  ],
};

function publishRoutes(): FetchRoutes {
  return {
    "GET /api/v1/service-groups": () => jsonResponse({ items: [GROUP] }),
    "GET /api/v1/gateways": () => jsonResponse({ items: [GATEWAY] }),
    "GET /api/v1/dns/accounts": () => jsonResponse({ items: [{ id: "a-1", providerCode: "cloudflare", displayName: "Main", enabled: true, isDefault: true, credentialStatus: "valid" }] }),
    "GET /api/v1/dns/accounts/a-1/zones": () => jsonResponse({ items: [{ id: "z-1", accountId: "a-1", name: "example.com" }] }),
    "GET /api/v1/publications": () => jsonResponse([SEEDED_PUBLICATION]),
    "POST /api/v1/publications/preview": () =>
      jsonResponse({ eligible: true, targetNodeIdsSnapshot: ["n-1"], checks: [{ name: "gateway", passed: true }], warnings: ["DNS TTL 延迟"] }),
    "POST /api/v1/publications": () =>
      jsonResponse({ id: "pub-1", status: "queued", createdAt: "2026-10-30T02:00:00Z", stages: [{ name: "precheck", status: "queued" }] }, 202),
    "POST /api/v1/publications/pub-seed/run": () =>
      jsonResponse({
        id: "pub-seed",
        status: "partially_completed",
        createdAt: SEEDED_PUBLICATION.createdAt,
        stages: [{ name: "dns", status: "failed", message: "stage_failed" }, { name: "certificate", status: "queued" }, { name: "gateway", status: "queued" }],
      }),
    "POST /api/v1/publications/pub-seed/rollback": (_init, url) => {
      void url;
      return jsonResponse({
        id: "pub-seed",
        status: "partially_completed",
        createdAt: SEEDED_PUBLICATION.createdAt,
        stages: [{ name: "dns", status: "rolled_back" }, { name: "certificate", status: "queued" }, { name: "gateway", status: "queued" }],
      });
    },
    "GET /api/v1/gateways/gw-1": () => jsonResponse(GATEWAY),
    "GET /api/v1/gateways/gw-1/checks": () =>
      jsonResponse({ nginxInstalled: true, serviceRunning: true, configValid: true, healthProbePassed: true }),
    "PATCH /api/v1/gateways/gw-1": (_init) => jsonResponse({ ...GATEWAY, enabled: false, schedulingEligible: false }),
    "GET /api/v1/gateway-routes": () => jsonResponse({ items: [] }),
  };
}

describe("publishing wizard", () => {
  it("requires a server preview and creates a staged publication job", async () => {
    const { calls } = await renderAuthenticated("/publishing", publishRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "服务发布" })).toBeVisible();
    fireEvent.change(await screen.findByLabelText("服务组"), { target: { value: "sg-1" } });
    fireEvent.click(await screen.findByLabelText(/Edge/));
    fireEvent.change(screen.getByLabelText("访问域名"), { target: { value: "app.example.com" } });
    fireEvent.click(screen.getByLabelText(/我确认已查看变更预览/));
    fireEvent.click(screen.getByRole("button", { name: "预览变更" }));
    expect(await screen.findByText("可发布：")).toBeVisible();
    expect(await screen.findByText("DNS TTL 延迟")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "创建发布作业" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/publications");
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body).toMatchObject({ serviceGroupId: "sg-1", gatewayIds: ["gw-1"], domain: "app.example.com", https: true });
    });
    expect(await screen.findByText(/发布作业 pub-1 已创建/)).toBeVisible();
  });
  it("runs and rolls back a staged publication through the real endpoints", async () => {
    const { calls } = await renderAuthenticated("/publishing", publishRoutes());
    expect(await screen.findByText("pub-seed")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "继续发布" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/publications/pub-seed/run");
      expect(call).toBeDefined();
    });
    expect(await screen.findByText(/执行已受理：状态 partially_completed/)).toBeVisible();
    // A returned staged result opens its current details; it is not all-success.
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getAllByRole("button", { name: "回滚此阶段" })[0]);
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/publications/pub-seed/rollback");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ stage: "dns" });
    });
    expect(await screen.findByText(/阶段 dns 回滚已受理/)).toBeVisible();
    expect(within(dialog).getByText(/rolled_back · dns/)).toBeVisible();
  });

  it("surfaces the honest 502 stage failure and refreshes stage state from the server", async () => {
    // 列表路由带可变状态：run 失败后服务端列表返回失败阶段（与真实 Controller 一致）。
    let stored: AsyncOperation = SEEDED_PUBLICATION;
    const { calls } = await renderAuthenticated("/publishing", {
      ...publishRoutes(),
      "GET /api/v1/publications": () => jsonResponse([stored]),
      "POST /api/v1/publications/pub-seed/run": () => {
        stored = {
          ...SEEDED_PUBLICATION,
          status: "partially_completed",
          stages: [{ name: "dns", status: "failed", message: "stage_failed" }, { name: "certificate", status: "queued" }, { name: "gateway", status: "queued" }],
        };
        return jsonResponse(
          { ...stored, error: { code: "stage_failed", message: "publication operation failed" } },
          502,
        );
      },
    });
    await screen.findByText("pub-seed");
    const listCallsBefore = calls.filter(call => call.method === "GET" && call.url === "/api/v1/publications").length;
    fireEvent.click(screen.getByRole("button", { name: "继续发布" }));
    expect(await screen.findByText(/HTTP 502 · 错误码 stage_failed/)).toBeVisible();
    await waitFor(() => {
      expect(calls.filter(call => call.method === "GET" && call.url === "/api/v1/publications").length).toBeGreaterThan(listCallsBefore);
    });
    fireEvent.click(screen.getAllByRole("button", { name: "阶段详情" })[0]);
    expect(await screen.findByText(/failed · dns：stage_failed/)).toBeVisible();
  });
});

describe("gateway management", () => {
  it("shows checks and requests a dispatch state change", async () => {
    const { calls } = await renderAuthenticated("/gateways", publishRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "网关管理" })).toBeVisible();
    expect(await screen.findByText("Edge")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "检查" }));
    expect(await screen.findByText("Nginx 已安装：通过")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "停用调度" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/gateways/gw-1");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ enabled: false });
    });
  });
});
