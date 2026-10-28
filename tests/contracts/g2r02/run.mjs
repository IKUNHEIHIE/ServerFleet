import { validateContract } from "../../../scripts/contracts.mjs";
import { run, tool, go } from "../../../scripts/lib.mjs";
import { candidate, hash, read, root, source } from "./build-candidate.mjs";
import path from "node:path";
import { mkdirSync, writeFileSync } from "node:fs";

const result = { executedAt: new Date().toISOString(), node: process.version, candidateSha256: hash(read(candidate)), sourceSha256: hash(read(source)), results: [], runtimeSecurity: "UNTESTED", typeScriptGeneration: "UNTESTED" };
function check(name, fn) {
  try { const detail = fn(); result.results.push({ name, status: "PASS", ...(name === "structure" ? detail : {}) }); }
  catch (error) { result.results.push({ name, status: "FAIL", error: error.message }); throw error; }
}
try {
  console.log("Explicit revision check (the root check:contracts does NOT discover this file)");
  check("structure", () => validateContract(read(candidate), candidate));
  check("lint", () => tool("redocly", ["lint", "--config", "redocly.yaml", candidate, "--format=summary"], { name: "g2r02-openapi-lint" }));
  check("contract-model-tests", () => run(process.execPath, ["--test", "tests/contracts/g2r02/candidate.test.mjs", "tests/contracts/g2r02/safety-fixtures.test.mjs"], { name: "g2r02-contract-tests" }));
  check("original-proto-build", () => tool("buf", ["build", "-o", path.join(root, "reports/g2r02-original.binpb")], { cwd: path.join(root, "docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2"), name: "g2r02-original-build" }));
  // Config is an inline invocation only; no source or shared config is edited.
  check("original-wire-compatibility", () => tool("buf", ["breaking", "--against", "reports/g2r02-original.binpb", "--config", JSON.stringify({ version: "v2", modules: [{ path: "proto" }], breaking: { use: ["WIRE"] } })], { name: "g2r02-original-wire-compatibility" }));
  check("go-proto-tests", () => run(go, ["test", "-count=1", "-v", "./tests/contracts/g2r02"], { name: "g2r02-proto-tests" }));
  console.log("PASS: explicit g2-r02 static/model + P1 wire checks; runtime security/transport NOT tested");
} catch (error) {
  console.error(error.message); process.exitCode = 1;
} finally {
  mkdirSync(path.join(root, "reports"), { recursive: true });
  writeFileSync(path.join(root, "reports/g2r02-results.json"), JSON.stringify(result, null, 2) + "\n");
}
