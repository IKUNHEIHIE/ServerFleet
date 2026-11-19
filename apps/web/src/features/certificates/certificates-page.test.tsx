import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const GROUP = { id: "sg-1", revision: 4, name: "Web", memberNodeIds: ["n-1", "n-2"], loadBalancing: { enabled: false } };
const JOB = {
  jobId: "cj-1",
  serviceGroupId: "sg-1",
  memberNodeIdsSnapshot: ["n-1", "n-2"],
  credentialType: "trust_bundle",
  state: "partial",
  results: [
    { nodeId: "n-1", state: "verified", identity: "spiffe://n-1" },
    { nodeId: "n-2", state: "waiting_offline", retryable: true },
  ],
  createdAt: "2026-10-30T02:00:00Z",
};

function certRoutes(): FetchRoutes {
  return {
    "GET /api/v1/service-groups": () => jsonResponse({ items: [GROUP] }),
    "GET /api/v1/certificates/internal/distributions": () => jsonResponse({ items: [JOB] }),
    "POST /api/v1/certificates/internal/distributions": () => jsonResponse({ ...JOB, jobId: "cj-2", state: "queued", results: [] }, 202),
    "POST /api/v1/certificates/internal/distributions/cj-1/revoke": () => jsonResponse({ ...JOB, state: "revoking" }, 202),
  };
}

describe("certificates page", () => {
  it("submits a snapshot-based distribution and a revoke request", async () => {
    const { calls } = await renderAuthenticated("/certificates", certRoutes());
    expect(await screen.findByRole("heading", { level: 1, name: "证书管理" })).toBeVisible();
    expect(await screen.findByText(/1 已验证 \/ 2/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "新建下发" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("服务组"), { target: { value: "sg-1" } });
    fireEvent.click(within(dialog).getByLabelText(/我确认该操作会影响所选服务组成员/));
    fireEvent.click(within(dialog).getByRole("button", { name: "提交下发" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/certificates/internal/distributions");
      expect(call).toBeDefined();
      expect(call?.init?.headers && new Headers(call.init.headers).get("Idempotency-Key")).toBeTruthy();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        serviceGroupId: "sg-1",
        credentialType: "trust_bundle",
        confirmServiceImpact: true,
        expectedGroupRevision: 4,
      });
    });

    await screen.findByRole("button", { name: "撤销" });
    fireEvent.click(screen.getByRole("button", { name: "撤销" }));
    const revokeDialog = await screen.findByRole("dialog");
    fireEvent.change(within(revokeDialog).getByLabelText("原因"), { target: { value: "轮换" } });
    fireEvent.click(within(revokeDialog).getByRole("button", { name: "确认撤销" }));
    await waitFor(() => {
      expect(calls.some(call => call.url === "/api/v1/certificates/internal/distributions/cj-1/revoke")).toBe(true);
    });
  });
});

describe("public certificate guide", () => {
  it("matches DNS and reuses the verified task and certificate without asking to copy IDs", async () => {
    const profile = {
      configured: true, choicesAvailable: true, profileSha256: "a".repeat(64),
      nodes: [{ id: "install-node", name: "网站节点" }],
      zones: [{ id: "parent", name: "example.com" }, { id: "more-specific", name: "lab.example.com" }],
    };
    let approved = false;
    const task = (taskId: string) => jsonResponse({ taskId, planHash: "b".repeat(64), status: "awaiting_approval", approvalRequired: true }, 202);
    const done = (id: string) => jsonResponse({ jobId: id, state: "completed", targetNodeIdsSnapshot: ["install-node"] });
    const routes: FetchRoutes = {
      ...certRoutes(),
      "GET /api/v1/certificates/public-profile": () => jsonResponse(profile),
      "POST /api/v1/certificates/csr-tasks": () => task("guide-csr"),
      "GET /api/v1/tasks/guide-csr": () => approved ? done("guide-csr") : jsonResponse({
        jobId: "guide-csr", state: "awaiting_approval", targetNodeIdsSnapshot: ["install-node"],
      }),
      "POST /api/v1/tasks/guide-csr/approve": () => {
        approved = true;
        return jsonResponse({ taskId: "guide-csr", state: "queued" });
      },
      "GET /api/v1/certificates/csr-tasks/guide-csr": () => jsonResponse({
        taskId: "guide-csr", result: { nodeId: "install-node", domain: "site.lab.example.com" },
      }),
      "POST /api/v1/certificates/public-tasks": () => task("guide-issue"),
      "GET /api/v1/tasks/guide-issue": () => done("guide-issue"),
      "GET /api/v1/certificates/public-tasks/guide-issue": () => jsonResponse({
        taskId: "guide-issue", csrTaskId: "guide-csr", nodeId: "install-node", domain: "site.lab.example.com",
        orderStatus: "issued", certificatePemBase64: "Y2hhaW4=", pendingCleanupCount: 0,
      }),
      "POST /api/v1/certificates/chain-tasks": () => task("guide-install"),
      "GET /api/v1/tasks/guide-install": () => done("guide-install"),
      "GET /api/v1/certificates/chain-tasks/guide-install": () => jsonResponse({
        taskId: "guide-install", csrTaskId: "guide-csr",
        result: { nodeId: "install-node", domain: "site.lab.example.com", notAfter: "2027-01-01T10:00:00Z" },
      }),
    };
    const { calls } = await renderAuthenticated("/certificates", routes);
    fireEvent.click(await screen.findByRole("tab", { name: "公网 HTTPS 证书" }));
    expect(await screen.findByLabelText("安装节点")).toHaveValue("install-node");
    const domain = screen.getByLabelText("网站域名");
    fireEvent.change(domain, { target: { value: "not-example.com" } });
    expect(screen.getByRole("button", { name: "开始申请" })).toBeDisabled();
    fireEvent.change(domain, { target: { value: "SITE.lab.example.com" } });
    expect(screen.getByText("DNS 区域 lab.example.com")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "开始申请" }));
    expect(calls.some(call => call.url.endsWith("/approve"))).toBe(false);
    fireEvent.click(await screen.findByRole("button", { name: "批准生成申请" }));
    fireEvent.click(await screen.findByRole("button", { name: "提交签发" }));
    fireEvent.click(await screen.findByRole("button", { name: "提交安装" }));
    expect(await screen.findByText("节点已安装证书")).toBeVisible();
    const posted = (path: string) => JSON.parse(String(calls.find(call => call.method === "POST" && call.url === path)?.init?.body));
    expect(posted("/api/v1/certificates/csr-tasks")).toMatchObject({ nodeId: "install-node", domain: "site.lab.example.com", keyRef: expect.stringMatching(/^tls-/) });
    expect(posted("/api/v1/certificates/public-tasks")).toMatchObject({ csrTaskId: "guide-csr", zoneId: "more-specific" });
    expect(posted("/api/v1/certificates/chain-tasks")).toMatchObject({ csrTaskId: "guide-csr", certificatePemBase64: "Y2hhaW4=" });
    expect(posted("/api/v1/tasks/guide-csr/approve")).toEqual({ planDigest: "b".repeat(64), decision: "approve" });
  });

  it("shows an actionable error before submitting to an unconfigured internal issuer", async () => {
    await renderAuthenticated("/certificates", {
      ...certRoutes(),
      "GET /api/v1/certificates/internal/distributions": () => jsonResponse({ items: [], configured: false }),
    });
    expect(await screen.findByText("内部 TLS 签发尚未配置")).toBeVisible();
    expect(screen.getByRole("button", { name: "新建下发" })).toBeDisabled();
  });
});
