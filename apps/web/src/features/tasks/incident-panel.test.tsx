import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import type { IncidentChecks, OperatorIncident } from "@/lib/api/endpoints-operations";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
const checks: IncidentChecks = { actualStateChecked: true, executionLogsChecked: true, impactChecked: true, unknownAccepted: false };
function incident(): OperatorIncident {
  const source = { kind: "task", id: "task-original", ownerId: "u-admin", revision: 3, state: "unknown", taskId: "task-original", nodeIds: [] };
  return { id: "incident-1", cycle: 1, revision: 2, observationDigest: "b".repeat(64), sourceDigest: "a".repeat(64), state: "investigating", assigneeId: "u-admin", source, createdAt: "2026-11-11T00:00:00Z", updatedAt: "2026-11-11T00:00:00Z", followupTaskIds: [], events: [], assessment: { current: { source, followups: [], manualEvidenceIds: ["proof-1", "proof-2", "proof-3"], evidenceCategories: ["actual_state", "execution_log", "impact_check"], digest: "facts-1", cycle: 1, restoreCount: 0 }, sourceChanged: false, factsChanged: false, observationDigest: "b".repeat(64), observationFresh: true, needsRecheck: false, blockers: [], observedAt: "2026-11-11T00:00:00Z" } };
}
function routes(get: () => OperatorIncident): FetchRoutes {
  return { "GET /api/v1/tasks": () => jsonResponse({ items: [] }), "GET /api/v1/operations/incidents": () => jsonResponse({ items: [get()] }), "GET /api/v1/operations/incidents/incident-1": () => jsonResponse(get()), "GET /api/v1/operations/incidents/incident-1/evidence": () => jsonResponse({ items: [], nextAfter: 0 }) };
}
async function open() { fireEvent.click(await screen.findByRole("button", { name: "打开人工处置" })); fireEvent.click(await screen.findByRole("button", { name: "查看与处置" })); await screen.findByLabelText("处置动作"); }
function note() { fireEvent.change(screen.getByLabelText("核验证据与处置说明"), { target: { value: "已核对实际资源状态、执行日志与影响范围" } }); }
function confirmChecks() { for (const label of ["已核对实际资源状态", "已检查执行日志与痕迹", "已核对影响范围与补偿结果"]) fireEvent.click(screen.getByLabelText(label)); }
it("reuses an unconfirmed request bound to evidence and never offers self review or rewrites UNKNOWN", async () => {
  let value = incident(); let writes = 0;
  const { calls } = await renderAuthenticated("/tasks", { ...routes(() => value), "POST /api/v1/operations/incidents/incident-1/actions": init => {
    writes++; if (writes === 1) return jsonResponse({ code: "TEMPORARILY_UNAVAILABLE" }, 503);
    const body = JSON.parse(String(init?.body)); value = { ...value, revision: 3, state: "review_required", proposal: { outcome: body.outcome, evidence: body.note, proposedBy: "u-admin", sourceDigest: value.sourceDigest, at: "2026-11-11T00:00:00Z", observationDigest: body.observationDigest, checks: body.checks } }; return jsonResponse(value);
  } });
  await open(); fireEvent.change(screen.getByLabelText("处置动作"), { target: { value: "propose" } }); note(); expect(screen.getByRole("button", { name: "提交人工处置" })).toBeDisabled(); confirmChecks();
  fireEvent.click(screen.getByRole("button", { name: "提交人工处置" })); await screen.findByText("处置未确认"); fireEvent.click(screen.getByRole("button", { name: "提交人工处置" })); await screen.findByText(/待复核人工结论/);
  const bodies = calls.filter(c => c.method === "POST" && c.url.endsWith("/actions")).map(c => JSON.parse(String(c.init?.body))); expect(bodies).toHaveLength(2); expect(bodies[0]).toEqual(bodies[1]); expect(bodies[0]).toMatchObject({ expectedRevision: 2, action: "propose", observationDigest: "b".repeat(64), checks });
  expect(screen.queryByRole("option", { name: "复核通过并人工结案" })).not.toBeInTheDocument(); expect(screen.getByText("原始结果：unknown")).toBeVisible(); expect(calls.some(c => /tasks\/.*\/(resume|approve)/.test(c.url))).toBe(false);
});
it("blocks closure for pending tasks and records a typed manual observation", async () => {
  let value = incident(); value.assessment!.blockers = ["followup_pending_or_unavailable:follow-1"];
  const { calls } = await renderAuthenticated("/tasks", { ...routes(() => value), "POST /api/v1/operations/incidents/incident-1/actions": () => { value = { ...value, revision: 3, observationDigest: "" }; return jsonResponse(value); } });
  await open(); fireEvent.change(screen.getByLabelText("处置动作"), { target: { value: "propose" } }); note(); confirmChecks(); expect(screen.getByRole("button", { name: "提交人工处置" })).toBeDisabled(); expect(screen.getByText(/后续任务尚未结束或不可读取/)).toBeVisible();
  fireEvent.change(screen.getByLabelText("处置动作"), { target: { value: "record_evidence" } }); fireEvent.change(screen.getByLabelText("证据引用"), { target: { value: "journal-reference:10-20" } }); fireEvent.click(screen.getByRole("button", { name: "提交人工处置" }));
  await waitFor(() => expect(calls.some(c => c.method === "POST" && c.url.endsWith("/actions"))).toBe(true)); const body = JSON.parse(String(calls.find(c => c.method === "POST" && c.url.endsWith("/actions"))?.init?.body)); expect(body).toMatchObject({ action: "record_evidence", evidenceCategory: "actual_state", evidenceReference: "journal-reference:10-20" }); expect(Number.isNaN(Date.parse(body.observedAt))).toBe(false); expect(body.checks).toBeUndefined();
});
it("requires a new review acknowledgement when the proposal revision changes", async () => {
  let value = incident(); value.state = "review_required"; value.proposal = { outcome: "confirmed_no_effect", evidence: "independent evidence", proposedBy: "another-admin", sourceDigest: value.sourceDigest, at: "2026-11-11T00:00:00Z", observationDigest: value.observationDigest, checks };
  const { calls } = await renderAuthenticated("/tasks", { ...routes(() => value), "POST /api/v1/operations/incidents/incident-1/actions": () => { value = { ...value, revision: 5, state: "resolved", resolvedBy: "u-admin" }; return jsonResponse(value); } });
  await open(); fireEvent.change(screen.getByLabelText("处置动作"), { target: { value: "approve" } }); note(); const ack = "已独立核对本次提案的证据、快照与后续任务结果"; expect(screen.getByRole("button", { name: "提交人工处置" })).toBeDisabled(); fireEvent.click(screen.getByLabelText(ack));
  value = { ...value, revision: 4, proposal: { ...value.proposal!, observationDigest: "c".repeat(64) } }; fireEvent.click(screen.getByRole("button", { name: "重新读取处置与证据" })); await screen.findByLabelText(ack); expect(screen.getByLabelText(ack)).not.toBeChecked(); expect(screen.getByRole("button", { name: "提交人工处置" })).toBeDisabled(); fireEvent.click(screen.getByLabelText(ack)); fireEvent.click(screen.getByRole("button", { name: "提交人工处置" })); await screen.findByText(/已归档人工结论/);
  const body = JSON.parse(String(calls.find(c => c.method === "POST" && c.url.endsWith("/actions"))?.init?.body)); expect(body).toMatchObject({ expectedRevision: 4, observationDigest: "c".repeat(64), reviewAcknowledged: true }); expect(screen.getByText("原始结果：unknown")).toBeVisible();
});
it("returns an admitted verification task when automatic linking loses a race", async () => {
  const value = incident(); value.source.action = "backups.upload";
  const { calls } = await renderAuthenticated("/tasks", { ...routes(() => value), "POST /api/v1/operations/incidents/incident-1/verify-backup": () => jsonResponse({ taskId: "verification-task", status: "queued", linked: false, originalResultPreserved: true }, 202) });
  await open(); note(); fireEvent.click(screen.getByRole("button", { name: "创建并关联备份只读核验任务" })); await screen.findByText(/关联发生冲突/); expect(screen.getByRole("link", { name: "verification-task" })).toHaveAttribute("href", "#/tasks/verification-task"); expect(calls.some(c => c.url.includes("remote-upload") || /tasks\/.*\/(resume|approve)/.test(c.url))).toBe(false);
});
