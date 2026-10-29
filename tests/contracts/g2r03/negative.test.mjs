import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { stringify } from "yaml";
import { tool } from "../../../scripts/lib.mjs";
import { validateContract } from "../../../scripts/contracts.mjs";
import { candidate, detailPath, document, listPath, root, sources } from "./build-candidate.mjs";

const config = "tests/contracts/g2r03/redocly.yaml";
function lint(doc, name, format = "stylish") {
  const file = `reports/g2r03-negative/${name}.yaml`;
  mkdirSync(path.join(root, "reports/g2r03-negative"), { recursive: true });
  writeFileSync(path.join(root, file), stringify(doc, { aliasDuplicateObjects: false }));
  return tool("redocly", ["lint", "--config", config, file, `--format=${format}`], { name: `g2r03-${name}`, allowFailure: true });
}

for (const [name, mutate, pattern, rule] of [
  ["bad-ref", doc => { doc.components.schemas.NodeSummary.properties.id = { $ref: "#/components/schemas/DoesNotExist" }; }, /unresolved \$ref/, "no-unresolved-refs"],
  ["duplicate-operation", doc => { doc.paths[detailPath].get.operationId = doc.paths[listPath].get.operationId; }, /duplicate operationId/, "operation-operationId-unique"],
  ["missing-path-parameter", doc => { doc.paths[detailPath].get.parameters = []; }, /missing required path parameter/, "path-parameters-defined"],
]) {
  test(`real structural and Redocly checks reject ${name}`, () => {
    const doc = document(candidate);
    mutate(doc);
    assert.throws(() => validateContract(stringify(doc)), pattern);
    const result = lint(doc, name);
    assert.equal(result.error, undefined);
    assert.equal(result.status, 1);
    assert.ok(result.output.includes(rule), result.output);
  });
}

test("real schema example validation rejects malformed list/detail/metric/error payloads", () => {
  const doc = document(candidate);
  const listMedia = doc.paths[listPath].get.responses["200"].content["application/json"];
  const detailMedia = doc.paths[detailPath].get.responses["200"].content["application/json"];
  const row = listMedia.examples.enriched.value.items[0];
  const detail = detailMedia.examples.online.value;
  const named = (name, value) => [name, { value }];
  const listCases = [
    named("negative_missing_id", { items: [{ name: "示例", online: false }] }),
    named("negative_online_type", { items: [{ ...row, online: "true" }] }),
    named("negative_unknown_status", { items: [{ ...row, status: "connected" }] }),
    named("negative_status_disagrees", { items: [{ ...row, online: false }] }),
    named("negative_offline_disagrees", { items: [{ ...row, status: "offline" }] }),
    named("negative_revision_zero", { items: [{ ...row, revision: 0 }] }),
    named("negative_cpu_over", { items: [{ ...row, cpuPercent: 100.1 }] }),
    named("negative_memory_under", { items: [{ ...row, memoryPercent: -1 }] }),
    named("negative_cpu_string", { items: [{ ...row, cpuPercent: "0" }] }),
    named("negative_sampled_time", { items: [{ ...row, sampledAt: "not-a-time" }] }),
  ];
  const { revision: omitted, ...withoutRevision } = detail;
  const detailCases = [
    named("negative_detail_revision_missing", withoutRevision),
    named("negative_detail_status", { ...detail, status: "healthy" }),
    named("negative_detail_last_seen_null", { ...detail, lastSeenAt: null }),
    named("negative_detail_extra_field", { ...detail, arbitrary: true }),
  ];
  listMedia.examples = Object.fromEntries(listCases);
  detailMedia.examples = Object.fromEntries(detailCases);
  const errorMedia = doc.components.responses.Error401.content["application/problem+json"];
  errorMedia.examples = { negative_error_status: { value: { ...errorMedia.examples.valid.value, status: 403 } } };
  doc.components.responses.Unauthorized.content["application/json"].examples = { negative_legacy_error: { value: { message: "需要有效身份凭证" } } };
  const result = lint(doc, "invalid-examples", "json");
  assert.equal(result.error, undefined);
  assert.equal(result.status, 1);
  const diagnostics = JSON.parse(result.stdout);
  const cases = [...listCases, ...detailCases].map(([name]) => name).concat("negative_error_status", "negative_legacy_error");
  for (const name of cases) assert.ok(diagnostics.problems.some(problem => problem.severity === "error" && problem.ruleId === "no-invalid-media-type-examples" && problem.location.some(location => location.pointer.includes(`/examples/${name}/`))), `No actual schema diagnostic for ${name}`);
  writeFileSync(path.join(root, "reports/g2r03-negative-diagnostics.json"), JSON.stringify({ cases, ...diagnostics }, null, 2) + "\n");
});

test("schema accepts genuine legacy/nullable/stored/future-shaped samples but proves no freshness", () => {
  const doc = document(candidate);
  const media = doc.paths[listPath].get.responses["200"].content["application/json"];
  media.examples.allowedShapes = { value: { items: [
    { id: "legacy", name: "legacy", online: false, cpuPercent: 50 },
    { id: "nullable", name: "nullable", online: true, cpuPercent: null, memoryPercent: null, sampledAt: null },
    { id: "stored", name: "stored", online: false, status: "offline", cpuPercent: 30, memoryPercent: 40, sampledAt: "2020-01-01T00:00:00Z" },
    { id: "future", name: "future", online: true, status: "online", cpuPercent: 0, memoryPercent: 100, sampledAt: "9999-01-01T00:00:00Z" },
  ] } };
  // A date-time schema has no trusted 'now' or max age. Acceptance is wire shape only.
  const result = lint(doc, "allowed-shapes");
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.output);
});

test("original open summary accepts conflicting extension names; r03 narrowing needs explicit approval", () => {
  const doc = document(candidate);
  doc.components.schemas.NodeSummary = document(sources.merged).components.schemas.NodeSummary;
  doc.paths[listPath].get.responses["200"].content["application/json"].examples = {
    oldExtensionCollision: { value: { items: [{ id: "example", name: "example", online: true, status: "connected", revision: 0, sampledAt: "unknown" }] } },
  };
  // These names were unconstrained extension properties in the original schema.
  // The r03 negative cases above reject each value; optional additions are not universally lossless.
  const result = lint(doc, "legacy-extension-collision");
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.output);
});
