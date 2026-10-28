// Isolated expectation samples only. No real Authorizer/Task Service is present.
import assert from "node:assert/strict";
import test from "node:test";
import { load } from "./build-candidate.mjs";

function effectiveTargets({ snapshot, currentMembers, userNodes, grantedNodes, revision, currentRevision }) {
  if (revision !== currentRevision) throw new Error("SCOPE_REVISION_STALE");
  return snapshot.filter(id => currentMembers.includes(id) && userNodes.includes(id) && grantedNodes.includes(id));
}
test("RBAC samples: default-zero AI scope; group additions excluded and removals immediately lose scope", () => {
  const r = { snapshot: ["A", "B"], currentMembers: ["A", "C"], userNodes: ["A", "B", "C"], grantedNodes: ["A", "B"], revision: 2, currentRevision: 2 };
  assert.deepEqual(effectiveTargets({ ...r, grantedNodes: [] }), []);
  assert.deepEqual(effectiveTargets(r), ["A"]);
  assert.throws(() => effectiveTargets({ ...r, currentRevision: 3 }), /STALE/);
});
test("AI full-access still refuses arbitrary root Shell without real administrator approval", () => {
  const mayDispatch = r => r.hasRBAC && r.hasScope && r.pluginEnabled && (!r.aiRootShell || r.humanAdminApproved);
  const r = { mode: "full_access", hasRBAC: true, hasScope: true, pluginEnabled: true, aiRootShell: true, humanAdminApproved: false };
  assert.equal(mayDispatch(r), false);
  assert.equal(mayDispatch({ ...r, humanAdminApproved: true }), true);
  assert.equal(mayDispatch({ ...r, humanAdminApproved: true, hasRBAC: false }), false);
  assert.equal(mayDispatch({ ...r, humanAdminApproved: true, pluginEnabled: false }), false);
});
test("dispatch samples: 15min applies only before start; unknown side effects and expired work never auto-run", () => {
  const mayOffer = r => !r.started && r.ageSeconds < 900 && !["unknown", "expired", "canceled"].includes(r.state) && r.authorized && !r.recoveryQuarantine;
  const r = { started: false, ageSeconds: 899, state: "waiting_node", authorized: true, recoveryQuarantine: false };
  assert.equal(mayOffer(r), true);
  for (const change of [{ ageSeconds: 900 }, { state: "unknown" }, { state: "expired" }, { started: true }, { recoveryQuarantine: true }, { authorized: false }]) assert.equal(mayOffer({ ...r, ...change }), false);
});
test("audit view samples: explicit allowed fields; raw secret fields are not a contract extension point", () => {
  const schema = load("api/openapi/revisions/g2-r02/merged-candidate.yaml").components.schemas.AuditEvent;
  assert.equal(schema.additionalProperties, false);
  const view = { eventId: "e", occurredAt: "2026-10-29T00:00:00Z", actorType: "user", action: "task.create", result: "denied", requestId: "r" };
  const shape = v => schema.required.every(k => Object.hasOwn(v, k)) && Object.keys(v).every(k => Object.hasOwn(schema.properties, k));
  assert.equal(shape(view), true);
  for (const key of ["token", "password", "rootCaPrivateKey", "recoveryPrivateKey", "environment"]) assert.equal(shape({ ...view, [key]: "synthetic-not-a-secret" }), false);
  // Allowed string fields can still contain secrets: real allowlist/redaction and log tests are required.
});
