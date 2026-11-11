import { test } from "node:test";
import assert from "node:assert/strict";
import { annotate, canonicalPath } from "./api-docs.mjs";

test("API index matches parameter aliases without conflating methods or dropping revision history", () => {
  const routes = [{ method: "GET", path: "/jobs/{taskId}", declarations: [] }, { method: "POST", path: "/jobs/{taskId}", declarations: [] }];
  const candidates = ["old", "new"].map(file => ({ file, doc: { paths: { "/jobs/{jobId}": { get: { operationId: file } } } } }));
  const result = annotate(routes, candidates);
  assert.deepEqual(result[0].candidates.map(c => c.file), ["old", "new"]);
  assert.deepEqual(result[1].candidates, []);
  assert.equal(canonicalPath("/jobs/{jobId}/events"), "/jobs/{}/events");
  assert.equal(canonicalPath("/jobs/{jobId}"), "/jobs/{}");
});

test("known compatibility aliases retain an explicit match label", () => {
  const routes = [{ method:"GET",path:"/api/v1/monitoring/availability-monitors/{monitorId}/history",declarations:[] }];
  const candidate={file:"monitoring",doc:{paths:{"/api/v1/availability-monitors/{id}/history":{get:{operationId:"history"}}}}};
  assert.equal(annotate(routes,[candidate])[0].candidateMatch,"compatibility-alias");
  assert.equal(annotate([{method:"POST",path:routes[0].path,declarations:[]}],[candidate])[0].candidateMatch,"none");
});
