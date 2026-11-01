import assert from "node:assert/strict";
import test from "node:test";
import { expectedUnavailable } from "./probe-status.mjs";

test("a documented unavailable response is an environmental blocker", () => {
  assert.equal(expectedUnavailable({status:503,body:{code:"MESSAGE_SOURCE_UNAVAILABLE"}}, [[503,"MESSAGE_SOURCE_UNAVAILABLE"]]), true);
  assert.equal(expectedUnavailable({status:404,body:{error:{code:"BACKUP_NOT_FOUND"}}}, [[404,"BACKUP_NOT_FOUND"]]), true);
});
test("authentication, invalid fixtures, internal failures and unrelated outages cannot become blockers", () => {
  for (const status of [400,401,403,404,500,502,503]) {
    assert.throws(() => expectedUnavailable({status,body:{code:"unexpected"}}, [[503,"MESSAGE_SOURCE_UNAVAILABLE"]]), /unexpected probe/);
  }
});
test("HTTP acceptance only admits further verification; it is not completion", () => {
  assert.equal(expectedUnavailable({status:202,body:{state:"queued",content:"echo of user prompt"}}, []), false);
  assert.equal(expectedUnavailable({status:200,body:{checks:[]}}, []), false);
});
