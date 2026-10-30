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
