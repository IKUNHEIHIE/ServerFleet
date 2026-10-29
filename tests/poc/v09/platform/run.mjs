// Isolated dependencies live ONLY in the task's ignored reports directory.
// Source snapshots use .go.txt so root tidy/checks don't acquire a runtime.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
import { root, go, run } from "../../../../scripts/lib.mjs";

const work = path.join(root, "reports/_review/v09-platform/poc");
const shared = ["go.mod", "go.sum", "package-lock.json"];
const digest = file => createHash("sha256").update(readFileSync(path.join(root, file))).digest("hex");
const before = shared.map(digest);
mkdirSync(work, { recursive: true });
writeFileSync(path.join(work, "go.mod"), `module serverfleet.local/serverfleet/poc

go 1.27.3

require (
  serverfleet.local/serverfleet v0.0.0
  github.com/coder/websocket v1.8.15
  github.com/tetratelabs/wazero v1.12.0
  google.golang.org/protobuf v1.36.12
)

require golang.org/x/sys v0.44.0 // indirect

replace serverfleet.local/serverfleet => ${JSON.stringify(root.replaceAll("\\", "/").replace(/\/$/, ""))}
`);
writeFileSync(path.join(work,"go.sum"),readFileSync(new URL("dependency-sums.txt",import.meta.url)));
for (const name of ["wss_test", "wasm_test", "controller_link_test"]) {
  writeFileSync(path.join(work, `${name}.go`), readFileSync(new URL(`${name}.go.txt`, import.meta.url)));
}
try {
  run(go, ["test", "-mod=readonly", "-count=1", "-v", "-timeout=45s", "."], { cwd: work, name: "v09-isolated-platform-poc" });
} finally {
  if (shared.some((file, i) => digest(file) !== before[i])) throw new Error("Root dependency files changed during isolated PoC");
}
console.log("PASS: root dependency hashes unchanged. Windows local PoC only; Linux/1C1G/full Host admission remain unverified.");
