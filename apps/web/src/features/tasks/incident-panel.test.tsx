import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";
import type { OperatorIncident } from "@/lib/api/endpoints-operations";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
it("reuses an unconfirmed human request and never offers self review or rewrites UNKNOWN", async () => {
  let incident: OperatorIncident = { id: "incident-1", revision: 2, sourceDigest: "a".repeat(64), state: "investigating", assigneeId: "u-admin", source: { kind: "task", id: "task-original", ownerId: "u-admin", revision: 3, state: "unknown", taskId: "task-original", nodeIds: [] }, createdAt: "2026-11-10T00:00:00Z", updatedAt: "2026-11-10T00:00:00Z", followupTaskIds: [], events: [] };
  let writes = 0;
  const { calls } = await renderAuthenticated("/tasks", {
    "GET /api/v1/tasks": () => jsonResponse({ items: [] }),
    "GET /api/v1/operations/incidents": () => jsonResponse({ items: [incident] }),
    "GET /api/v1/operations/incidents/incident-1": () => jsonResponse(incident),
    "POST /api/v1/operations/incidents/incident-1/actions": init => {
      writes++;
      if (writes === 1) return jsonResponse({ code: "TEMPORARILY_UNAVAILABLE" }, 503);
      const body = JSON.parse(String(init?.body));
      incident = { ...incident, revision: 3, state: "review_required", proposal: { outcome: body.outcome, evidence: body.note, proposedBy: "u-admin", sourceDigest: incident.sourceDigest, at: "2026-11-10T00:00:00Z" } };
      return jsonResponse(incident);
    },
  });
  fireEvent.click(await screen.findByRole("button", { name: "打开人工处置" }));
  fireEvent.click(await screen.findByRole("button", { name: "查看与处置" }));
  fireEvent.change(await screen.findByLabelText("处置动作"), { target: { value: "propose" } });
  fireEvent.change(screen.getByLabelText("核验证据与处置说明"), { target: { value: "已检查远端副本和实际资源，附核验记录" } });
  fireEvent.click(screen.getByRole("button", { name: "提交人工处置" }));
  await screen.findByText("处置未确认");
  fireEvent.click(screen.getByRole("button", { name: "提交人工处置" }));
  await screen.findByText(/待复核人工结论/);
  const bodies = calls.filter(c => c.method === "POST" && c.url.endsWith("/actions")).map(c => JSON.parse(String(c.init?.body)));
  expect(bodies).toHaveLength(2); expect(bodies[0]).toEqual(bodies[1]);
  expect(bodies[0]).toMatchObject({ expectedRevision: 2, action: "propose" });
  await waitFor(() => expect(screen.queryByRole("option", { name: "复核通过并人工结案" })).not.toBeInTheDocument());
  expect(screen.getByText("原始结果：unknown")).toBeVisible();
  expect(calls.some(c => /tasks\/.*\/(resume|approve)/.test(c.url))).toBe(false);
});
