// Isolated dependencies live ONLY in the task's ignored reports directory.
// Source snapshots use .go.txt so root tidy/checks don't acquire a runtime.
import { mkdirSync, readFileSync, readdirSync, unlinkSync, writeFileSync } from "node:fs";
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
// Compile the ACTUAL current Agent application sources in an isolated module;
// only this module imports the unadopted WSS dependency. No hand-translated
// executor/session implementation is substituted for the production source.
const agentWork=path.join(work,"agentapp");mkdirSync(agentWork,{recursive:true});
const agentFiles=readdirSync(path.join(root,"apps/agent")).filter(name=>name.endsWith(".go"));
for(const name of readdirSync(agentWork).filter(name=>name.endsWith(".go")&&!agentFiles.includes(name)&&name!=="actual_wss_test.go")){
 const target=path.resolve(agentWork,name);if(path.dirname(target)!==path.resolve(agentWork))throw new Error("Invalid stale source path");unlinkSync(target);
}
const sources={};
for(const name of agentFiles){const source=readFileSync(path.join(root,"apps/agent",name));writeFileSync(path.join(agentWork,name),source);sources[`apps/agent/${name}`]=createHash("sha256").update(source).digest("hex");}
writeFileSync(path.join(agentWork,"actual_wss_test.go"),readFileSync(new URL("agent_session_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"agent-source-hashes.json"),JSON.stringify(sources,null,2)+"\n");
try {
  if(!process.argv.includes("--prepare-only"))run(go, ["test", "-mod=readonly", "-count=1", "-v", "-timeout=45s", ".", "./agentapp"], { cwd: work, name: "v09-isolated-platform-poc" });
} finally {
  if (shared.some((file, i) => digest(file) !== before[i])) throw new Error("Root dependency files changed during isolated PoC");
}
console.log(process.argv.includes("--prepare-only")?"PREPARED isolated source/locks only; NO tests executed.":"PASS: root dependency hashes unchanged. Windows local PoC only; Linux/1C1G/full Host admission remain unverified.");
