import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import assert from "node:assert/strict";
import test from "node:test";
import { parseDocument } from "yaml";
import { validateContract } from "../../../../scripts/contracts.mjs";

const sourceURL = new URL("../../../../api/openapi/g2-merged-candidate.yaml", import.meta.url);
const patchURL = new URL("../../../../docs/contracts/revisions/v09-five-day-cx02/auth-session.patch.json", import.meta.url);
const original = readFileSync(sourceURL);
const source = parseDocument(original.toString(), { uniqueKeys: true }).toJS({ maxAliasCount:10000 });
const operations = JSON.parse(readFileSync(patchURL,"utf8"));

// Test-only RFC6902 subset. Applies ONLY the checked-in candidate in memory;
// nothing writes the protected input or a generated OpenAPI artifact.
const keys = pointer => pointer.slice(1).split("/").map(p => p.replaceAll("~1","/").replaceAll("~0","~"));
function resolve(doc,pointer){let value=doc;for(const key of keys(pointer)){assert.ok(Object.hasOwn(value,key),`missing ${pointer}`);value=value[key]}return value}
function apply(doc,patch){
  const out=structuredClone(doc);
  for(const op of patch){
    if(op.op==="test"){assert.deepEqual(resolve(out,op.path),op.value);continue}
    const parts=keys(op.path);const leaf=parts.pop();let parent=out;
    for(const key of parts){assert.ok(Object.hasOwn(parent,key));parent=parent[key]}
    if(op.op==="remove"){assert.ok(Object.hasOwn(parent,leaf));delete parent[leaf];continue}
    if(op.op==="replace")assert.ok(Object.hasOwn(parent,leaf));
    assert.ok(["add","replace","copy"].includes(op.op));
    const value=structuredClone(op.op==="copy"?resolve(out,op.from):op.value);
    if(Array.isArray(parent)&&leaf==="-")parent.push(value);else parent[leaf]=value;
  }
  return out;
}

test("CX02 independent auth candidate preserves protected bytes, operation IDs and legacy node DTO",()=>{
  assert.equal(createHash("sha256").update(original).digest("hex"),"c779a4366d5fe0d3088d1c682b231dfe9d157c3d2ae562e1440cba7ccc3f6726");
  const candidate=apply(source,operations);
  assert.deepEqual(validateContract(JSON.stringify(candidate)),{paths:147,operations:185});
  const old=source.paths["/api/v1/nodes"].get;const next=candidate.paths["/api/v1/nodes"].get;
  assert.equal(next.operationId,old.operationId);assert.deepEqual(next.parameters,old.parameters);assert.deepEqual(next.responses,old.responses);
  assert.deepEqual(next.security,[{cookieSession:[]},{bearerAuth:[]}]);
  assert.equal(candidate.components.securitySchemes.cookieSession.name,"__Host-sf_session");
  assert.ok(!Object.hasOwn(candidate.paths["/api/v1/auth/bootstrap"].post.responses,"200"));
  assert.ok(Object.hasOwn(candidate.paths["/api/v1/auth/bootstrap"].post.responses,"204"));
  assert.ok(candidate.components.schemas.CurrentIdentityResult.required.includes("csrfToken"));
  assert.ok(!Object.hasOwn(source.components.schemas.Identity.properties,"csrfToken"));
  assert.ok(!Object.hasOwn(candidate.components.schemas.Identity.properties,"csrfToken"));
  assert.equal(candidate["x-serverfleet-status"],"PROPOSED-NOT-FROZEN-CX02-AUTH-SESSION");
  assert.deepEqual(readFileSync(sourceURL),original);
});
test("CX02 refuses applying migration to a stale or already migrated input",()=>{
  const stale=structuredClone(source);stale.components.securitySchemes.cookieSession.name="different";
  assert.throws(()=>apply(stale,operations));
  assert.throws(()=>apply(apply(source,operations),operations));
});

test("CX02 enrollment metadata remains an independent candidate without changing protected bytes or states",()=>{
  const patch=JSON.parse(readFileSync(new URL("../../../../docs/contracts/revisions/v09-five-day-cx02/agent-enrollment.patch.json",import.meta.url),"utf8"));
  const candidate=apply(source,patch);
  assert.deepEqual(validateContract(JSON.stringify(candidate)),{paths:147,operations:185});
  const request=candidate.components.schemas.AgentEnrollRequest;
  const result=candidate.components.schemas.AgentEnrollResult;
  assert.ok(request.required.includes("platform"));
  assert.ok(result.required.includes("expiresAt"));
  assert.deepEqual(result.properties.state.enum,["pending","issued","denied"]);
  assert.deepEqual(result.allOf[0].then.required,["nodeId","certificatePem","intermediateChainPem","trustGeneration"]);
  assert.equal(result.additionalProperties,false);
  assert.deepEqual(candidate.paths,source.paths);
  assert.throws(()=>apply(candidate,patch));
  assert.deepEqual(readFileSync(sourceURL),original);
});
