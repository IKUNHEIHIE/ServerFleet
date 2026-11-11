import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse, type FetchRoutes } from "@/test/http";
afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
const path = "/api/v1/certificates/public-renewals/policy-one/delivery";
const policy = { id: "policy-one", revision: 3, enabled: true, orderTaskId: "old-order", installTaskId: "old-install", csrTaskId: "csr", nodeId: "node-one", domain: "example.com", keyRef: "key", zoneId: "zone", profileSha256: "a".repeat(64), notAfter: "2026-12-31T00:00:00Z", renewBeforeDays: 30, updatedAt: "2026-11-11T00:00:00Z", state: "scheduled", automaticInstallation: false };
function routes(blocked = false): FetchRoutes {
 return {
  "GET /api/v1/certificates/public-renewals": () => jsonResponse({ items: [{ policy, state: "scheduled", automaticInstallation: false }] }),
  ["GET " + path]: () => jsonResponse({ delivery: { policyId: policy.id, revision: 4, enabled: true, publicationIds: [], cycle: blocked ? { id: "cycle-one", policyRevision: 3, issueTaskId: "unknown-issue", state: "blocked", errorCode: "ISSUANCE_FAILED_OR_UNKNOWN", publications: [], updatedAt: policy.updatedAt } : undefined }, tasks: [], history: [] }),
  ["POST " + path]: () => jsonResponse({ policyId: policy.id, revision: 5, enabled: true, publicationIds: ["pub-one"] }),
  ["POST " + path + "/recover"]: () => jsonResponse({ policyId: policy.id, revision: 5, enabled: false, publicationIds: [] }),
 };
}
async function open() {
 fireEvent.click(await screen.findByRole("tab", { name: "公网 HTTPS 证书" }));
 fireEvent.click(await screen.findByRole("button", { name: "安装发布编排" }));
 await screen.findByLabelText("需要更新的现有发布计划 ID");
}
describe("renewal delivery", () => {
 it("uses dual CAS and requires unique plan identifiers without approving tasks", async () => {
  const { calls } = await renderAuthenticated("/certificates", routes()); await open();
  fireEvent.change(screen.getByLabelText("需要更新的现有发布计划 ID"), { target: { value: "pub-one\npub-one" } }); expect(screen.getByRole("button", { name: "启用或保存安装发布编排" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("需要更新的现有发布计划 ID"), { target: { value: "pub-one" } }); fireEvent.click(screen.getByRole("button", { name: "启用或保存安装发布编排" }));
  await waitFor(() => expect(calls.some(c => c.method === "POST" && c.url === path)).toBe(true));
  const call = calls.find(c => c.method === "POST" && c.url === path); expect(JSON.parse(String(call?.init?.body))).toEqual({ expectedRevision: 4, policyRevision: 3, enabled: true, publicationIds: ["pub-one"] }); expect(calls.some(c => c.url.includes("/approval"))).toBe(false);
 });
 it("adopts explicit repair proofs with future automation disabled by default", async () => {
  const { calls } = await renderAuthenticated("/certificates", routes(true)); await open();
  fireEvent.click(screen.getByText("人工修复后采用成功结果"));
  fireEvent.change(screen.getByLabelText("修复后成功签发任务 ID"), { target: { value: "new-order" } }); fireEvent.change(screen.getByLabelText("修复后成功安装任务 ID"), { target: { value: "new-install" } }); fireEvent.change(screen.getByLabelText("恢复依据"), { target: { value: "actual successful repair" } }); fireEvent.change(screen.getByLabelText("已独立复核结案的 UNKNOWN 处置单 ID"), { target: { value: "case-one" } });
  expect(screen.getByRole("button", { name: "确认采用修复结果" })).toBeDisabled();
  fireEvent.click(screen.getByLabelText("我已核对新签发、安装与站点状态，确认采用这些成功结果")); fireEvent.click(screen.getByRole("button", { name: "确认采用修复结果" }));
  await waitFor(() => expect(calls.some(c => c.method === "POST" && c.url === path + "/recover")).toBe(true));
  const call = calls.find(c => c.method === "POST" && c.url === path + "/recover"); expect(JSON.parse(String(call?.init?.body))).toEqual({ expectedRevision: 4, policyRevision: 3, orderTaskId: "new-order", installTaskId: "new-install", publicationIds: [], incidentIds: ["case-one"], note: "actual successful repair", confirmed: true, enabled: false });
 });
});
