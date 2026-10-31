import test from "node:test";
import assert from "node:assert/strict";
import { validateContract } from "../../../scripts/contracts.mjs";
import { buildCandidate, candidate, detailPath, docs, document, fields, hash, listPath, matrixMarkdown, provenance, read, sources } from "./build-candidate.mjs";

const current = document(candidate);
const merged = document(sources.merged);
const core = document(sources.core);
const r02 = document(sources.r02);
const metadata = JSON.parse(read(`${docs}/provenance.json`));
const deref = value => value?.$ref ? value.$ref.slice(2).split("/").reduce((v, k) => v[k], current) : value;
const errorCodes = op => Object.keys(op.responses).filter(code => Number(code) >= 400).sort();

test("candidate YAML parses, resolves all refs, has exactly the two existing GET operations and is reproducible", () => {
  assert.deepEqual(validateContract(read(candidate), candidate), { paths: 2, operations: 2 });
  assert.deepEqual(current, buildCandidate());
  assert.deepEqual(Object.keys(current.paths), [listPath, detailPath]);
  for (const path of [listPath, detailPath]) {
    assert.deepEqual(Object.keys(current.paths[path]), ["get"]);
    assert.equal(current.paths[path].get.operationId, merged.paths[path].get.operationId);
    assert.equal(current.paths[path].get.operationId, r02.paths[path].get.operationId);
  }
  assert.equal(core.paths[listPath], undefined);
  assert.equal(core.paths[detailPath].get.operationId, current.paths[detailPath].get.operationId);
  assert.equal(document(sources.originalLegacy).paths[listPath].get.operationId, "listNodesForTransfer");
  assert.deepEqual(document(sources.originalMerged).paths[listPath], merged.paths[listPath]);
});

test("provenance, operation matrix and Markdown field matrix are recomputed from real sources", () => {
  assert.deepEqual(metadata, provenance());
  assert.equal(read(`${docs}/MATRIX.md`), matrixMarkdown(metadata));
  for (const source of Object.values(metadata.sources)) assert.equal(source.sha256, hash(read(source.file)));
  for (const row of metadata.operations) {
    const op = current.paths[row.path][row.method];
    assert.equal(op.operationId, row.operationId);
    assert.deepEqual(errorCodes(merged.paths[row.path].get), row.originalErrors);
    assert.deepEqual(errorCodes(op), row.candidateErrors);
    assert.deepEqual(op.security ?? current.security, row.security);
    assert.deepEqual(current.components.schemas[row.schema].required, row.required);
    for (const code of row.candidateErrors) {
      assert.deepEqual(Object.keys(deref(op.responses[code]).content), [row.errorMedia]);
    }
  }
  assert.equal(metadata.fields.length, 12);
  for (const row of metadata.fields) {
    assert.ok(current.components.schemas.NodeSummary.properties[row.listProperty], row.id);
    if (row.detailProperty) assert.ok(current.components.schemas.NodeDetail.properties[row.detailProperty], row.id);
  }
  assert.deepEqual(metadata.fields.map(row => row.id), fields.map(row => row[0]));
});

test("legacy list request, envelope and every original summary field remain unchanged", () => {
  const list = current.paths[listPath].get;
  const old = merged.paths[listPath].get;
  assert.deepEqual(list.parameters, old.parameters);
  assert.deepEqual(list.parameters[0], { in: "query", name: "onlineOnly", schema: { type: "boolean", default: false } });
  const media = list.responses["200"].content["application/json"];
  assert.deepEqual(deref(media.schema), old.responses["200"].content["application/json"].schema);
  const summary = current.components.schemas.NodeSummary;
  const original = merged.components.schemas.NodeSummary;
  assert.deepEqual(summary.required, original.required);
  assert.equal(summary.additionalProperties, original.additionalProperties);
  for (const [name, schema] of Object.entries(original.properties)) assert.deepEqual(summary.properties[name], schema, name);
  assert.deepEqual(media.examples.legacyUntimed.value, { items: [{ id: "node-example-01", name: "示例节点", online: true, cpuPercent: 23, memoryPercent: null }] });
  assert.equal(media.examples.legacyUntimed.value.items[0].revision, undefined);
  assert.equal(media.examples.legacyUntimed.value.items[0].sampledAt, undefined);
  assert.equal(media.examples.legacyUntimed.value.items[0].status, undefined);
  assert.equal(summary.properties.nodeId, undefined);
  assert.equal(summary.properties.displayName, undefined);
});

test("detail keeps original fields/required/closed shape; optional extensions share list metric schemas", () => {
  const detail = current.components.schemas.NodeDetail;
  for (const baseline of [merged, core, r02]) {
    assert.deepEqual(detail.required, baseline.components.schemas.NodeDetail.required);
    for (const [name, schema] of Object.entries(baseline.components.schemas.NodeDetail.properties)) assert.deepEqual(detail.properties[name], schema, name);
  }
  assert.equal(detail.additionalProperties, false);
  assert.deepEqual(current.paths[detailPath].get.parameters, merged.paths[detailPath].get.parameters);
  const summary = current.components.schemas.NodeSummary;
  for (const name of ["cpuPercent", "memoryPercent", "sampledAt"]) assert.deepEqual(detail.properties[name], summary.properties[name]);
  for (const name of ["revision", "status", "platform", "agentVersion", "lastSeenAt"]) assert.deepEqual(summary.properties[name], detail.properties[name]);
  for (const schema of [detail, summary]) {
    for (const name of ["revision", "status", "sampledAt", "lastSeenAt"]) assert.equal(schema.properties[name].default, undefined, name);
    for (const name of ["cpuPercent", "memoryPercent"]) {
      assert.deepEqual(schema.properties[name].type, ["number", "null"]);
      assert.equal(schema.properties[name].minimum, 0);
      assert.equal(schema.properties[name].maximum, 100);
    }
    assert.equal(schema.properties.revision.minimum, 1);
    assert.equal(schema.properties.sampledAt.format, "date-time");
    assert.deepEqual(schema.properties.sampledAt.type, ["string", "null"]);
    assert.equal(schema.properties.lastSeenAt.type, "string");
  }
});

test("matrix source absences are real, including unchanged r02 success schemas and Web's local boundary", () => {
  const oldSummary = merged.components.schemas.NodeSummary;
  const oldDetail = merged.components.schemas.NodeDetail;
  assert.deepEqual(r02.components.schemas.NodeSummary, oldSummary);
  for (const name of ["status", "revision", "platform", "agentVersion", "lastSeenAt", "sampledAt"]) assert.equal(oldSummary.properties[name], undefined, name);
  for (const name of ["cpuPercent", "memoryPercent", "sampledAt"]) assert.equal(oldDetail.properties[name], undefined, name);
  assert.deepEqual(oldDetail.properties.status.enum, ["online", "offline", "suspended", "revoked", "unknown"]);
  const adapter = read(sources.webAdapter);
  assert.ok(adapter.includes("export interface CandidateNodeMetrics"));
  assert.ok(adapter.includes("readonly revision: number"));
  assert.ok(adapter.includes("此形状只用于隔离 Mock 与 ViewModel"));
  assert.ok(read(sources.webTypes).includes("maxSampleAgeMs: number"));
  const internalNode = read(sources.node).split("type Node struct {")[1].split("\n}")[0];
  assert.doesNotMatch(internalNode, /Revision|Platform|AgentVersion/);
});

test("authentication remains explicit; no anonymous branch, profile, fake revision or new list parameters", () => {
  assert.deepEqual(current.security, merged.security);
  assert.deepEqual(current.components.securitySchemes, merged.components.securitySchemes);
  for (const path of [listPath, detailPath]) {
    const op = current.paths[path].get;
    assert.deepEqual(op.security, merged.paths[path].get.security);
    assert.ok((op.security ?? current.security).every(branch => Object.keys(branch).length > 0));
    for (const param of op.parameters ?? []) assert.notEqual(param.name, "X-ServerFleet-Contract");
    for (const response of Object.values(op.responses)) {
      const headers = deref(response).headers;
      assert.equal(headers["Cache-Control"].schema.const, "no-store");
      assert.equal(headers["X-ServerFleet-Contract"], undefined);
    }
  }
  assert.ok(r02.paths[listPath].get.parameters.some(p => p.$ref?.includes("ContractProfile")));
  assert.equal(current["x-review-status"], "PROPOSED-NOT-FROZEN");
});

test("error schemas preserve original formats; detail status is tied to HTTP status by proposal", () => {
  assert.deepEqual(current.components.schemas.Error, merged.components.schemas.Error);
  assert.deepEqual(current.components.schemas.Problem, merged.components.schemas.Problem);
  for (const code of errorCodes(current.paths[detailPath].get)) {
    const response = deref(current.paths[detailPath].get.responses[code]);
    const media = response.content["application/problem+json"];
    assert.equal(media.schema.allOf[1].properties.status.const, Number(code));
    assert.equal(media.examples.valid.value.status, Number(code));
  }
  assert.deepEqual(errorCodes(current.paths[listPath].get), ["400", "401", "403", "503"]);
  assert.equal(current.paths[listPath].get.responses["404"], undefined);
});

test("positive examples cover all five states, zero/100, nullable metrics, absent time/revision and empty inventory", () => {
  const listExamples = current.paths[listPath].get.responses["200"].content["application/json"].examples;
  assert.deepEqual(listExamples.empty.value, { items: [] });
  assert.deepEqual(listExamples.enriched.value.items.map(n => n.status), current.components.schemas.NodeDetail.properties.status.enum);
  for (const row of listExamples.enriched.value.items) assert.equal(row.online, row.status === "online");
  assert.equal(listExamples.enriched.value.items[0].cpuPercent, 0);
  assert.equal(listExamples.enriched.value.items[0].memoryPercent, 100);
  assert.equal(listExamples.enriched.value.items[1].cpuPercent, null);
  const detailExamples = current.paths[detailPath].get.responses["200"].content["application/json"].examples;
  assert.deepEqual(Object.keys(detailExamples), current.components.schemas.NodeDetail.properties.status.enum);
  for (const [name, example] of Object.entries(detailExamples)) assert.equal(example.value.status, name);
});
