import assert from "node:assert/strict";
import { readFileSync, unlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { root, run } from "./lib.mjs";
import { taskPath } from "./worktree.mjs";

const branch = "chore/p1-worktree-smoke";
const target = taskPath(root, branch);
run(process.execPath, ["scripts/worktree.mjs", "create", branch, "main"], { name: "worktree-create" });
const sentinel = path.join(target, "p1-worktree-smoke.txt");
const content = "P1 test file created by worktree-smoke; no user data\n";
writeFileSync(sentinel, content, { flag: "wx" });
try {
  const refused = run(process.execPath, ["scripts/worktree.mjs", "remove", branch, "main"], { name: "worktree-dirty-refusal", allowFailure: true });
  assert.notEqual(refused.status, 0);
  assert.match(refused.output, /contains changed, untracked or ignored/);
  assert.equal(readFileSync(sentinel, "utf8"), content, "cleanup must preserve the untracked test file");
} finally {
  // Exact, owned file only; never recursively delete a computed directory.
  assert.equal(readFileSync(sentinel, "utf8"), content);
  unlinkSync(sentinel);
}
run(process.execPath, ["scripts/worktree.mjs", "remove", branch, "main"], { name: "worktree-clean-remove" });
run("git", ["branch", "-d", branch], { name: "worktree-test-branch-remove" });
console.log("PASS: real Worktree create, dirty-file refusal, clean merged cleanup; no Agent started");
