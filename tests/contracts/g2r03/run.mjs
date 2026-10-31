import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { validateContract } from "../../../scripts/contracts.mjs";
import { go, run, tool } from "../../../scripts/lib.mjs";
import { candidate, hash, read, root } from "./build-candidate.mjs";

const result = { task: "G2-R02A", executedAt: new Date().toISOString(), node: process.version, candidateSha256: hash(read(candidate)), runtimeHTTP: "UNTESTED", runtimeAuthentication: "UNTESTED", results: [] };
function check(name, fn) {
  try {
    const detail = fn();
    result.results.push({ name, status: "PASS", ...(name === "structure" ? detail : {}) });
  } catch (error) {
    result.results.push({ name, status: "FAIL", error: error.message });
    throw error;
  }
}
try {
  console.log("Explicit g2-r03 check: root check:contracts does not discover this directory.");
  check("structure", () => validateContract(read(candidate), candidate));
  check("candidate-lint-and-examples", () => tool("redocly", ["lint", "--config", "tests/contracts/g2r03/redocly.yaml", candidate, "--format=summary"], { name: "g2r03-openapi-lint" }));
  check("source-matrix-and-real-negative-contract-checks", () => run(process.execPath, ["--test", "tests/contracts/g2r03/candidate.test.mjs", "tests/contracts/g2r03/negative.test.mjs"], { name: "g2r03-contract-tests" }));
  check("actual-offline-read-boundaries", () => run(go, ["test", "-count=1", "-v", "./tests/contracts/g2r03"], { name: "g2r03-read-boundaries" }));
  console.log("PASS: r03 static contract + actual offline boundary probes; HTTP/authentication/freshness NOT verified.");
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
} finally {
  mkdirSync(path.join(root, "reports"), { recursive: true });
  writeFileSync(path.join(root, "reports/g2r03-results.json"), JSON.stringify(result, null, 2) + "\n");
}
