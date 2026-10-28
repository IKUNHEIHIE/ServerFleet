import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { parseDocument } from "yaml";

export function validateContract(text, name = "contract") {
  if (Buffer.byteLength(text) > 2 * 1024 * 1024) throw new Error(`${name}: contract exceeds 2 MiB input limit`);
  const parsed = parseDocument(text, { uniqueKeys: true });
  if (parsed.errors.length) throw new Error(`${name}: ${parsed.errors.map(e => e.message).join("; ")}`);
  // The supplied candidate deliberately reuses many response anchors. Keep a
  // finite expansion limit rather than disabling YAML alias protection.
  const doc = parsed.toJS({ maxAliasCount: 10000 });
  if (!/^3\.1\./.test(doc?.openapi) || !doc.paths) throw new Error(`${name}: expected OpenAPI 3.1 paths`);
  const ids = new Set();
  function deref(value) {
    if (!value?.$ref) return value;
    if (!value.$ref.startsWith("#/")) throw new Error(`${name}: external $ref requires explicit review: ${value.$ref}`);
    let target = doc;
    for (const key of value.$ref.slice(2).split("/").map(k => k.replace(/~1/g, "/").replace(/~0/g, "~"))) {
      if (target == null || !Object.hasOwn(target, key)) throw new Error(`${name}: unresolved $ref ${value.$ref}`);
      target = target[key];
    }
    return target;
  }
  const seen = new Set();
  function walk(value) {
    if (!value || typeof value !== "object") return;
    if (seen.has(value)) return;
    seen.add(value);
    if (value.$ref) deref(value);
    Object.values(value).forEach(walk);
  }
  walk(doc);
  let count = 0;
  for (const [url, item] of Object.entries(doc.paths)) {
    const pathItem = deref(item);
    for (const method of ["get", "put", "post", "delete", "options", "head", "patch", "trace"]) {
      const op = pathItem[method];
      if (!op) continue;
      if (!op.operationId || ids.has(op.operationId)) throw new Error(`${name}: missing/duplicate operationId ${op.operationId}`);
      ids.add(op.operationId);
      const params = new Map();
      for (const param of [...(pathItem.parameters ?? []), ...(op.parameters ?? [])].map(deref)) params.set(`${param.in}:${param.name}`, param);
      const required = [...url.matchAll(/\{([^}]+)\}/g)].map(m => m[1]);
      for (const key of required) if (params.get(`path:${key}`)?.required !== true) throw new Error(`${name}: missing required path parameter ${key} at ${url}`);
      for (const p of params.values()) if (p.in === "path" && (!required.includes(p.name) || p.required !== true)) throw new Error(`${name}: invalid path parameter ${p.name}`);
      count++;
    }
  }
  return { paths: Object.keys(doc.paths).length, operations: count };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  for (const name of ["g2-core", "g2-merged", "xray"]) {
    const file = `api/openapi/${name}-candidate.yaml`;
    console.log(`${file}: ${JSON.stringify(validateContract(readFileSync(file, "utf8"), file))}`);
  }
}
