import { test } from "node:test";
import assert from "node:assert/strict";
import { validBranch, validTitle } from "./git-policy.mjs";

test("task branches and conventional titles", () => {
  for (const branch of ["main", "feature/p2-nodes-ds01", "contract/g2-proto-cx01"]) assert.ok(validBranch(branch));
  for (const branch of ["foo", "feature/../../oops", "feature/task name"]) assert.equal(validBranch(branch), false);
  assert.ok(validTitle("chore(repo): establish P1 foundation"));
  assert.equal(validTitle("unfinished"), false);
});
