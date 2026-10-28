import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { taskPath } from "./worktree.mjs";

test("worktree helper contains task paths", () => {
  assert.equal(path.dirname(taskPath(process.cwd(), "feature/p1-smoke")), path.resolve(".worktrees"));
  for (const branch of ["main", "feature/../../escape", "feature/x;rm", "", "feature/C:\\escape"]) assert.throws(() => taskPath(process.cwd(), branch));
});
