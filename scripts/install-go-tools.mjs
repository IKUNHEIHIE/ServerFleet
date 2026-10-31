import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { env, go, root, run } from "./lib.mjs";

const versions = JSON.parse(readFileSync(path.join(root, "scripts/tool-versions.json"), "utf8"));
mkdirSync(env.GOBIN, { recursive: true });
const requested = process.argv.slice(2);
for (const [name, module] of Object.entries(versions.goTools)) {
  if (requested.length === 0 || requested.includes(name)) run(go, ["install", module], { name: `install-${name}` });
}
for (const name of requested) if (!(name in versions.goTools)) throw new Error(`Unknown tool ${name}`);
