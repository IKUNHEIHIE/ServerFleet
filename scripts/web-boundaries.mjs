import path from "node:path";
import { readFileSync, readdirSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { root } from "./lib.mjs";

const src = path.join(root, "apps/web/src");
export function checkWebSource(text, file) {
  if (/[\\/]test[\\/]|\.test\.[cm]?[jt]sx?$/.test(file)) return;
  function imported(value) {
    if (!value) return;
    const target = value.startsWith("@/") ? path.resolve(src, value.slice(2)) : value.startsWith(".") ? path.resolve(path.dirname(file), value) : null;
    if (!target) return;
    const rel = path.relative(src, target).replaceAll("\\", "/");
    if (rel.startsWith("../") || rel === ".." || /(^|\/)(test|tests|__mocks__|fixtures)(\/|\.)|\.test\./.test(rel)) throw new Error(`${file}: forbidden production import ${value}`);
  }
  // A conservative static guard, independent of the TypeScript compiler API.
  // Computed runtime destinations still require code review and API tests.
  for (const match of text.matchAll(/(?:\bfrom\s*|\bimport\s*\(\s*|\brequire\s*\(\s*|\bimport\s*)["']([^"']+)["']/g)) imported(match[1]);
  if (/\bnew\s+(?:globalThis\.)?WebSocket\s*\(/.test(text)) throw new Error(`${file}: Web must use Controller REST/SSE, not Agent WebSocket`);
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  function walk(dir) {
    for (const item of readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, item.name);
      if (item.isDirectory()) walk(file);
      else if (/\.[jt]sx?$/.test(file)) checkWebSource(readFileSync(file, "utf8"), file);
    }
  }
  walk(src);
  console.log("PASS: Web production import and Agent WebSocket boundaries");
}
