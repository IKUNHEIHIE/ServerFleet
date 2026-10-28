import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { root } from "./lib.mjs";

const base = path.join(root, "docs/reference/ServerFleet-ALL-v0.9-G2");
const manifest = JSON.parse(readFileSync(path.join(base, "SHA256-MANIFEST.json"), "utf8").replace(/^\uFEFF/, ""));
for (const [file, expected] of Object.entries(manifest)) {
  const bytes = readFileSync(path.join(base, file));
  if (bytes.length !== expected.bytes || createHash("sha256").update(bytes).digest("hex") !== expected.sha256) {
    throw new Error(`Original source changed: ${file}`);
  }
}
const index = JSON.parse(readFileSync(path.join(root, "docs/contracts/SOURCE-INDEX.json"), "utf8"));
for (const [copy, source] of Object.entries(index.byteExactCopies)) {
  if (!readFileSync(path.join(root, copy)).equals(readFileSync(path.join(base, source)))) throw new Error(`Candidate copy drifted: ${copy}. Use a separately reviewed revision.`);
}
console.log(`PASS: ${Object.keys(manifest).length} original files and ${Object.keys(index.byteExactCopies).length} byte-exact candidates`);
