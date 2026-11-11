import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { root, go, run, npm, tool, goTool } from "./lib.mjs";

function goFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap(entry => {
    if ([".git", ".worktrees", ".tools", ".cache", "node_modules", "docs", "reports"].includes(entry.name)) return [];
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? goFiles(full) : entry.name.endsWith(".go") ? [path.relative(root, full)] : [];
  });
}

const suites = {
  policy() {
    run(process.execPath, ["scripts/verify-source.mjs"], { name: "source-integrity" });
    run(process.execPath, ["--test", "scripts/contracts.test.mjs", "scripts/api-docs.test.mjs", "scripts/git-policy.test.mjs", "scripts/worktree.test.mjs", "scripts/web-boundaries.test.mjs"], { name: "tooling-tests" });
    run(process.execPath, ["scripts/web-boundaries.mjs"], { name: "web-boundaries" });
    run(process.execPath, ["scripts/git-policy.mjs"], { name: "git-policy" });
    run(go, ["test", "./tests/architecture"], { name: "module-boundaries" });
    goTool("actionlint", ["-shellcheck="]);
  },
  generate() { tool("buf", ["generate"]); },
  contracts() {
    run(process.execPath, ["scripts/verify-source.mjs"], { name: "source-integrity" });
    run(process.execPath, ["scripts/contracts.mjs"], { name: "openapi-structure" });
    run(process.execPath, ["scripts/api-docs.mjs", "--check"], { name: "api-documentation-index" });
    tool("redocly", ["lint", "--format=summary"], { name: "openapi-lint" });
    tool("buf", ["lint"], { name: "protobuf-lint" });
    tool("buf", ["format", "--diff", "--exit-code"], { name: "protobuf-format" });
    tool("buf", ["build", "-o", "reports/agent.binpb"], { name: "protobuf-build" });
    tool("buf", ["breaking", "--against", "tests/contracts/proto-baseline"], { name: "protobuf-compatibility" });
    // On a PR compare to its actual base as well as the initial P1 snapshot.
    if (process.env.GITHUB_BASE_REF) tool("buf", ["breaking", "--against", `.git#branch=origin/${process.env.GITHUB_BASE_REF}`], { name: "protobuf-pr-compatibility" });
    const generated = path.join(root, "gen/go/serverfleet/agent/v1/agent.pb.go");
    if (!existsSync(generated)) throw new Error("Generated Go file is missing; run npm run generate");
    const before = readFileSync(generated);
    tool("buf", ["generate"], { name: "protobuf-generation" });
    if (!before.equals(readFileSync(generated))) throw new Error("Generated Go code was stale. Commit regenerated output.");
    run(go, ["test", "./tests/contracts"], { name: "protobuf-roundtrip" });
    run(process.execPath, ["scripts/proto-negative.mjs"], { name: "protobuf-negative-checks" });
    // Revision candidates are checked explicitly, alongside the original inputs.
    run(process.execPath, ["tests/contracts/g2r02/run.mjs"], { name: "g2r02-explicit-contracts" });
    run(process.execPath, ["tests/contracts/g2r03/run.mjs"], { name: "g2r03-explicit-contracts" });
  },
  go() {
    const fmt = path.join(path.dirname(go), process.platform === "win32" ? "gofmt.exe" : "gofmt");
    const result = run(go === "go" ? "gofmt" : fmt, ["-l", ...goFiles(root)], { name: "go-format" });
    if (result.stdout.trim()) throw new Error("Go formatting differs; run gofmt on the listed files");
    run(go, ["mod", "verify"], { name: "go-mod-verify" });
    run(go, ["mod", "tidy", "-diff"], { name: "go-mod-tidy" });
    run(go, ["build", "-o", "reports/controller" + (process.platform === "win32" ? ".exe" : ""), "./apps/controller"], { name: "controller-build" });
    run(go, ["build", "-o", "reports/agent" + (process.platform === "win32" ? ".exe" : ""), "./apps/agent"], { name: "agent-build" });
    const tests = ["test", "-count=1", "-json", "./..."];
    if (process.env.CI && process.platform === "linux") tests.splice(1, 0, "-race");
    run(go, tests, { name: "go-tests" });
    run(go, ["vet", "./..."], { name: "go-vet" });
    goTool("staticcheck", ["./..."]);
  },
  web() {
    npm(["run", "typecheck", "--workspace", "@serverfleet/web"], { name: "web-typecheck" });
    npm(["test", "--workspace", "@serverfleet/web"], { name: "web-tests" });
    npm(["run", "build", "--workspace", "@serverfleet/web"], { name: "web-build" });
  },
  git() {
    run("git", ["diff", "--check"], { name: "git-diff-check" });
    run("git", ["diff", "--cached", "--check"], { name: "git-staged-check" });
  },
  security() {
    npm(["audit", "--audit-level=high"], { name: "npm-audit" });
    goTool("govulncheck", ["./..."]);
  },
};
const selected = process.argv[2] ?? "all";
const names = selected === "all" ? ["policy", "contracts", "go", "web", "git"] : [selected];
const results = [];
mkdirSync(path.join(root, "reports"), { recursive: true });
for (const name of names) {
  if (!suites[name]) throw new Error(`Unknown suite: ${name}`);
  try { suites[name](); results.push({ name, status: "PASS" }); }
  catch (error) { console.error(error.message); results.push({ name, status: "FAIL", error: error.message }); }
}
writeFileSync(path.join(root, `reports/check-${selected}.json`), JSON.stringify({ executedAt: new Date().toISOString(), results }, null, 2) + "\n");
if (results.some(result => result.status !== "PASS")) process.exitCode = 1;
