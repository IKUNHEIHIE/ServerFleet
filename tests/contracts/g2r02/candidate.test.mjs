import assert from "node:assert/strict";
import test from "node:test";
import { validateContract } from "../../../scripts/contracts.mjs";
import { build, candidate, evidence, executionStates, hash, inventory, load, operations, read, resolve, revisionOps, source, taskStates } from "./build-candidate.mjs";

const base = load(source), revised = load(candidate);
const byId = d => new Map(operations(d).map(({ op }) => [op.operationId, op]));
const ids = byId(revised);
const params = (d, op) => (op.parameters ?? []).map(p => resolve(d, p));
const success = op => Object.fromEntries(Object.entries(op.responses).filter(([s]) => /^2/.test(s)).map(([s, r]) => [s, r.content ?? null]));

// Assert properties of the delivered file, independently of builder output equality.
function validatePolicy(d) {
  validateContract(JSON.stringify(d), candidate);
  const ops = operations(d), original = byId(base);
  assert.equal(ops.length, 185);
  assert.equal(Object.keys(d.paths).length, 147);
  assert.deepEqual(new Set(ops.map(x => x.op.operationId)), new Set(original.keys()));
  for (const { method, path, op } of ops) {
    assert.equal(base.paths[path]?.[method]?.operationId, op.operationId, "method/path/ID retained");
    assert.ok(!/^\/api\/v1\/jobs(?:\/|$)/.test(path), "no second queue");
    const old = original.get(op.operationId), publicOp = (old.security ?? base.security).length === 0;
    const write = ["post", "put", "patch", "delete"].includes(method);
    assert.deepEqual(op.security, publicOp ? [] : write ? [{ bearerAuth: [] }, { cookieSession: [], csrfToken: [] }] : [{ bearerAuth: [] }, { cookieSession: [] }], "Cookie writes require CSRF in the SAME security alternative");
    const p = params(d, op);
    assert.ok(p.some(x => x.name === "X-ServerFleet-Contract" && x.required && x.schema.const === "g2-r02"));
    if (write && !publicOp) assert.ok(p.some(x => x.name === "Idempotency-Key" && x.required));
    if (revisionOps[op.operationId]) {
      assert.ok(p.some(x => x.name === "If-Match" && x.required === (revisionOps[op.operationId][1] === "header-required")));
      assert.equal(op["x-revision-precondition"].staleStatus, 412);
      assert.ok(op.responses["428"] && op.responses["412"]);
    }
    const beforeSuccess = success(old);
    if (/^stream.*EventsG2$/.test(op.operationId)) {
      delete beforeSuccess["204"];
      assert.ok(op.responses["410"]);
      assert.equal(op["x-sse"].expiredCursorStatus, 410);
    }
    assert.deepEqual(success(op), beforeSuccess, "success payload refs/codes retained; transitive schema changes separately recorded");
    for (const [status, response] of Object.entries(op.responses)) if (/^[45][0-9][0-9]$/.test(status)) {
      const r = resolve(d, response);
      assert.deepEqual(Object.keys(r.content), ["application/problem+json"]);
      assert.equal(r.content["application/problem+json"].schema.allOf[1].properties.status.const, Number(status));
    }
  }
  assert.deepEqual(d.components.schemas.TaskState.enum, taskStates);
  assert.deepEqual(d.components.schemas.ExecutionState.enum, executionStates);
  for (const n of ["TaskSummary", "BatchTaskJob"]) assert.equal(d.components.schemas[n].properties.state.$ref, "#/components/schemas/TaskState");
  assert.equal(d.components.schemas.TaskExecution.properties.state.$ref, "#/components/schemas/ExecutionState");
  assert.deepEqual(d.components.schemas.BatchTaskJob["x-equal-fields"], ["jobId", "taskId"]);
  assert.ok(d.components.schemas.BatchTaskJob.required.includes("taskId"));
  assert.equal(d.components.schemas.NodeGroup.properties.revision.minimum, 0);
  assert.equal(d.components.schemas.ServiceGroup.properties.revision.minimum, 1);
  assert.equal(d.components.securitySchemes.bearerAuth.bearerFormat, undefined);
  assert.equal(d.components.schemas.LoginResult.required.includes("csrfToken"), true);
}

test("delivered revision explicitly validates refs, IDs, paths and cross-cutting policy for all 185 operations", () => validatePolicy(revised));
test("reproducible build and operation matrix cover old151 + new34 without stale rows", () => {
  assert.deepEqual(revised, build(base));
  const rows = JSON.parse(read(`${evidence}/operations.json`));
  assert.deepEqual(rows, inventory(base, revised));
  assert.equal(rows.filter(r => r.source === "legacy-151").length, 151);
  assert.equal(rows.filter(r => r.source === "g2-added-34").length, 34);
  const legacy = load("docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-API-Contract-v0.9.yaml");
  for (const { path, method, op } of operations(legacy)) assert.equal(hash(op), hash(base.paths[path][method]), `old op preserved at source: ${op.operationId}`);
});

for (const [name, mutate, expected] of [
  ["dangling ref", d => { d.paths["/api/v1/tasks"].get.responses["200"].content["application/json"].schema.$ref = "#/components/schemas/Absent"; }, /unresolved/],
  ["duplicate ID", d => { d.paths["/api/v1/tasks"].get.operationId = "createBatchTask"; }, /duplicate/],
  ["lost path parameter", d => { d.paths["/api/v1/tasks/{jobId}"].get.parameters = []; }, /path parameter/],
  ["CSRF as OR bypass", d => { d.paths["/api/v1/tasks"].post.security = [{ cookieSession: [] }, { csrfToken: [] }]; }, /Cookie writes/],
  ["anonymous task mutation", d => { d.paths["/api/v1/tasks"].post.security = []; }, /Cookie writes/],
  ["missing operation", d => { delete d.paths["/api/v1/tasks"].post; }, /184/],
  ["unknown task state silently removed", d => { d.components.schemas.TaskState.enum = taskStates.filter(s => s !== "unknown"); }, /unknown/],
  ["HTTP/body status disagreement", d => { d.components.responses.R02Error409.content["application/problem+json"].schema.allOf[1].properties.status.const = 200; }, /200/],
  ["task precondition optional", d => { const op = d.paths["/api/v1/tasks/{jobId}/resume"].post; op.parameters = op.parameters.filter(p => resolve(d, p).name !== "If-Match"); }, /assert.ok/],
  ["success silently rewritten", d => { d.paths["/api/v1/tasks"].post.responses["200"] = d.paths["/api/v1/tasks"].post.responses["202"]; delete d.paths["/api/v1/tasks"].post.responses["202"]; }, /success payload/],
]) test(`negative mutation rejected: ${name}`, () => {
  const bad = structuredClone(revised); mutate(bad);
  assert.throws(() => validatePolicy(bad), expected);
});
test("duplicate YAML keys rejected in revision input", () => assert.throws(() => validateContract("openapi: 3.1.0\npaths: {}\npaths: {}\n"), /unique|same key/i));

// Test-local compatibility oracles, not Controller/auth/scheduler implementations.
// These samples define expected future behavior; real end-to-end tests remain UNTESTED.
function authorize(id, request) {
  if (request.cookie && request.bearer) return 400;
  const op = ids.get(id);
  return op.security.some(alt => Object.keys(alt).every(scheme => {
    if (scheme === "cookieSession") return request.cookie === "valid";
    if (scheme === "bearerAuth") return request.bearer === "valid";
    if (scheme === "csrfToken") return request.csrf === "bound" && request.origin === "same";
    return false;
  })) ? 200 : request.cookie || request.bearer ? 403 : 401;
}
test("auth fixtures: safe Cookie read, bound Cookie write, scoped Bearer write accepted", () => {
  assert.equal(authorize("getBatchTaskJob", { cookie: "valid" }), 200);
  assert.equal(authorize("createBatchTask", { cookie: "valid", csrf: "bound", origin: "same" }), 200);
  assert.equal(authorize("createBatchTask", { bearer: "valid" }), 200);
});
test("auth negative fixtures: absent/revoked identity, missing CSRF, cross-origin, mixed credentials refused", () => {
  for (const [request, status] of [[{}, 401], [{ cookie: "revoked" }, 403], [{ cookie: "valid" }, 403], [{ cookie: "valid", csrf: "bound", origin: "foreign" }, 403], [{ cookie: "valid", bearer: "valid" }, 400]]) assert.equal(authorize("createBatchTask", request), status);
});
const etag = (revision, digest = "a".repeat(64)) => `"sf-r${revision}-${digest}"`;
function precondition({ current, currentTag = etag(current), header, body, bodyRequired = false }) {
  const match = header?.match(/^"sf-r(0|[1-9][0-9]*)-([a-f0-9]{64})"$/);
  if (header !== undefined && !match) return 400;
  if (body !== undefined && (!Number.isSafeInteger(body) || body < 0)) return 400;
  if (bodyRequired ? body === undefined : header === undefined) return 428;
  const h = match ? Number(match[1]) : undefined;
  if (h !== undefined && body !== undefined && h !== body) return 400;
  return (h ?? body) === current && (header === undefined || header === currentTag) ? 200 : 412;
}
test("revision fixtures: group revision0/body alias, identical body+strongETag accepted", () => {
  assert.equal(precondition({ current: 0, body: 0, bodyRequired: true }), 200);
  assert.equal(precondition({ current: 7, header: etag(7), body: 7 }), 200);
});
test("revision negatives: missing428, stale412, disagree400, weak/wildcard/list rejected", () => {
  assert.equal(precondition({ current: 7 }), 428);
  assert.equal(precondition({ current: 7, body: 7 }), 428);
  assert.equal(precondition({ current: 7, header: etag(6) }), 412);
  assert.equal(precondition({ current: 7, header: etag(7), body: 6 }), 400);
  assert.equal(precondition({ current: 7, header: etag(7, "b".repeat(64)) }), 412, "same revision but another resource/representation cannot match");
  for (const header of [`W/${etag(7)}`, '*', `${etag(7)}, ${etag(8)}`]) assert.equal(precondition({ current: 7, header }), 400);
});
test("revision namespaces remain distinct: scopeRevision is not resource If-Match nor Xray config revision", () => {
  assert.equal(precondition({ current: 7, scopeRevision: 7 }), 428);
  assert.equal(revised.components.schemas.XrayRollbackRequest.properties.expectedCurrentRevision.type, "integer");
  assert.equal(revised.components.schemas.AISession.properties.scopeRevision.type, "integer");
});
test("idempotency fixtures: digest is compared after subject/method/path/key lookup, replay rechecks current authority", () => {
  const records = new Map();
  function submit(r) {
    if (!r.authorized) return { status: 403 };
    const key = JSON.stringify([r.subject, r.method, r.path, r.key]);
    const previous = records.get(key);
    if (previous) return previous.digest === r.digest ? { status: 202, taskId: previous.taskId } : { status: 409 };
    const value = { digest: r.digest, taskId: `synthetic-${records.size + 1}` };
    records.set(key, value);
    return { status: 202, taskId: value.taskId };
  }
  const r = { subject: "u1", method: "POST", path: "/api/v1/tasks", key: "synthetic-key", digest: "body-A", authorized: true };
  const first = submit(r);
  assert.deepEqual(submit(r), first);
  assert.equal(submit({ ...r, digest: "body-B" }).status, 409);
  assert.equal(submit({ ...r, authorized: false }).status, 403);
  assert.notEqual(submit({ ...r, subject: "u2" }).taskId, first.taskId);
  assert.notEqual(submit({ ...r, path: "/api/v1/tasks/t1/cancel" }).taskId, first.taskId);
  assert.equal(records.size, 3, "conflicting/revoked requests did not create executions");
});
test("task projection samples preserve unknown/paused/expired instead of false success, jobId/taskId must match", () => {
  function project(j, legacy = false) {
    assert.equal(j.jobId, j.taskId, "alias mismatch");
    assert.ok(taskStates.includes(j.state));
    if (legacy && !base.components.schemas.BatchTaskJob.properties.state.enum.includes(j.state)) throw new Error("CLIENT_UPGRADE_REQUIRED");
    return { taskId: j.taskId, state: j.state };
  }
  for (const state of taskStates) assert.equal(project({ jobId: "t", taskId: "t", state }).state, state);
  for (const state of ["unknown", "paused", "expired", "partially_failed"]) assert.throws(() => project({ jobId: "t", taskId: "t", state }, true), /UPGRADE_REQUIRED/);
  assert.throws(() => project({ jobId: "other", taskId: "t", state: "completed" }), /alias mismatch/);
});
test("SSE gap and Last-Event-ID contract is explicit; cursor never grants authority", () => {
  for (const id of ["streamTaskEventsG2", "streamAISessionEventsG2"]) {
    const op = ids.get(id);
    assert.ok(params(revised, op).some(p => p.name === "Last-Event-ID"));
    assert.equal(op.responses["204"], undefined);
    assert.equal(op["x-sse"].gapAfterHeaders, "event: stream.gap; close; fetch snapshot then reconnect");
    assert.equal(authorize(id, { cursor: "someone-elses:42" }), 401);
  }
});
test("P1 proto revision only adds go_package and formatting; original semantic tokens unchanged", () => {
  const original = read("docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/proto/serverfleet/agent/v1/agent.proto");
  const p1 = read("tests/contracts/proto-baseline/serverfleet/agent/v1/agent.proto");
  const tokens = v => v.replace(/option go_package = "[^"]+";/, "").replace(/\s+/g, "");
  assert.equal(tokens(p1), tokens(original));
});

test("R6 retains every P1 declaration and adds only its four reviewed Envelope fields", () => {
  const p1 = read("tests/contracts/proto-baseline/serverfleet/agent/v1/agent.proto");
  const current = read("proto/serverfleet/agent/v1/agent.proto");
  const sections = current.split("// R6 CANDIDATE, NOT RUNTIME ADMISSION:");
  assert.equal(sections.length, 2, "R6 declarations must be a separate append-only section");
  let prefix = sections[0];
  for (const [type, field, number] of [["FileChunkRequest","file_chunk_request",33],["FileChunkReply","file_chunk_reply",34],["FileOpenRequest","file_open_request",35],["FileOpenReply","file_open_reply",36]]) {
    const line = `    ${type} ${field} = ${number};\n`;
    assert.equal(prefix.split(line).length,2,"exactly one registered R6 field");
    prefix = prefix.replace(line,"");
    assert.ok(sections[1].includes(`message ${type} {`),"candidate body is present");
  }
  const tokens = v => v.replace(/\s+/g, "");
  assert.equal(tokens(prefix), tokens(p1), "legacy declarations, field numbers and comments unchanged");
});
