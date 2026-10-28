import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:net";
import path from "node:path";
import { env, root, run } from "./lib.mjs";

const suffix = process.platform === "win32" ? ".exe" : "";
run(path.join(root, "reports", `agent${suffix}`), [], { name: "agent-runtime" });
const probe = createServer();
probe.listen(0, "127.0.0.1");
await once(probe, "listening");
const port = probe.address().port;
await new Promise((resolve, reject) => probe.close(error => error ? reject(error) : resolve()));
const controller = spawn(path.join(root, "reports", `controller${suffix}`), ["-listen", `127.0.0.1:${port}`], { cwd: root, env, windowsHide: true, stdio: ["ignore", "ignore", "pipe"] });
const closed = once(controller, "close");
let errorOutput = "";
controller.stderr.on("data", data => { errorOutput += data; });
try {
  let response;
  for (let attempt = 0; attempt < 30; attempt++) {
    try { response = await fetch(`http://127.0.0.1:${port}/healthz`, { signal: AbortSignal.timeout(1000) }); break; }
    catch { await new Promise(resolve => setTimeout(resolve, 100)); }
  }
  assert.ok(response, `Controller did not become live: ${errorOutput}`);
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { status: "scaffold", stage: "M0-P1" });
  const denied = await fetch(`http://127.0.0.1:${port}/api/v1/tasks`, { method: "POST", signal: AbortSignal.timeout(1000) });
  assert.equal(denied.status, 404);
  console.log("PASS: Controller process health endpoint works; management task route is unavailable");
} finally {
  controller.kill();
  await closed;
}
console.log("PASS: all smoke-test processes stopped");
