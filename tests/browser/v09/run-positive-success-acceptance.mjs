/**
 * GM-W6 · 正常成功用例验收（V09-R6-GM · Wave 6）。
 *
 * 与 `run-live-controller-acceptance.mjs`（70 步负向/退化回归，保持不动）的分工：
 * 本脚本只覆盖**正常成功路径**——"功能真的做成了"才算 PASS；能力未接线或环境缺失时
 * 记 BLOCKED（引用服务端精确响应），绝不把 501/503/502 的诚实拒绝当作功能完成。
 *
 * 真实执行链路（本机可复现）：
 *   1. 在 WSL（默认发行版，Linux amd64）内运行真实 Controller + 真实 Agent：
 *      `GOOS=linux go build ./apps/controller ./apps/agent`，二者都在 WSL 原生文件系统上
 *      运行（SQLite WAL 语义正确、Agent 平台上报为 linux）。浏览器仍在 Windows 侧通过
 *      https 访问 WSL 内的 Web 宿主（NAT 模式 Windows→WSL localhost 转发，回退 WSL IP）。
 *   2. 本地 PKI：脚手架按 `localpki` 的加载格式预置 local-agent-ca（EC P-256/PKCS8），
 *      并以该 CA 签发覆盖 DNS:localhost 的服务端证书。Agent 用 `-root-cert` 钉扎该 CA，
 *      通过 `https://localhost:<apiPort>` 建立主动 WSS+mTLS（真实 enrollment、真实握手、
 *      真实心跳/指标、真实 TaskOffer 执行）。
 *   3. 正向链路全部由浏览器 UI 驱动：生成接入令牌 → Agent enroll(pending) → UI 审批
 *      → enroll(issued) → UI 创建资源授权 → Agent connect-once 等待任务 → UI 提交
 *      `tasks.command` 任务 → 真实派发/执行 → 任务 completed、执行 succeeded。
 *   4. B 组用例是"正向尝试"：若后端已支持则验证真实成功；当前基线未接线的适配器
 *      （跨节点字节通道、AI 模型、Xray 制品、发布器、更新源、恢复执行器）按服务端
 *      精确错误码记 BLOCKED。B01—B06尚缺完整夹具/执行终态证据，接线后仍需补全测试；
 *      入队、版本检查或恢复预演成功，均不得算完整功能成功。
 *
 * 退出码：存在 FAIL 才为 1；BLOCKED 不算失败（如实登记的缺口）。
 *
 * 用法：
 *   node tests/browser/v09/run-positive-success-acceptance.mjs [--out <目录>] [--skip-build] [--keep]
 *   （需 WSL 默认发行版可用；--distro 显式指定发行版名）
 */
import { spawn } from "node:child_process";
import { expectedUnavailable } from "./probe-status.mjs";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import https from "node:https";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import {
  checkboxState,
  clickText,
  connectPage,
  evaluate,
  fillField,
  goto,
  launchHeadlessBrowser,
  screenshot,
  setCheckbox,
  waitForCondition,
} from "./browser-client.mjs";

const here = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(here, "../../..");

const args = process.argv.slice(2);
function argValue(name, fallback) {
  const index = args.indexOf(`--${name}`);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
}
const hasFlag = name => args.includes(`--${name}`);
const outDir = resolve(argValue("out", join(repoRoot, "docs", "development", "sprints", "v09-five-day", "progress", "gemini", "evidence", "positive")));
const skipBuild = hasFlag("skip-build");
const keepTemp = hasFlag("keep");
const DISTRO = argValue("distro", "");

const ADMIN_USER = "admin";
const ADMIN_PASSWORD = "positive-admin-strong-password-01";
/** UI 不支持自定义令牌名；服务端缺省名即 enrollment-token，审批后成为节点名。 */
const ENROLLED_NODE_LABEL = "enrollment-token";

const results = [];
/** @param {string} step @param {"PASS"|"BLOCKED"|"FAIL"} state @param {string} detail */
function record(step, state, detail = "") {
  results.push({ step, state, detail });
  console.log(`${state}  ${step}${detail ? ` — ${detail}` : ""}`);
}

let debugSession = null;
let debugSeq = 0;
async function stepPass(name, fn) {
  try {
    const detail = await fn();
    record(name, "PASS", typeof detail === "string" ? detail : "");
    return true;
  } catch (error) {
    record(name, "FAIL", String(error.message ?? error));
    if (debugSession) {
      debugSeq += 1;
      await screenshot(debugSession, join(outDir, `debug-${String(debugSeq).padStart(2, "0")}-${name.replace(/^[\d\s]+/, "").replace(/[^a-z0-9-]+/gi, "-").slice(0, 40)}.png`)).catch(() => {});
    }
    return false;
  }
}
/** 正向尝试：fn 成功 → PASS（真实成功）；抛 blocked(reason) → BLOCKED；其他错误 → FAIL。 */
function blocked(reason) {
  const error = new Error(reason);
  error.blocked = true;
  return error;
}
async function attemptPositive(name, fn) {
  try {
    const detail = await fn();
    record(name, "PASS", typeof detail === "string" ? detail : "");
    return "PASS";
  } catch (error) {
    if (error?.blocked) {
      record(name, "BLOCKED", String(error.message));
      return "BLOCKED";
    }
    record(name, "FAIL", String(error.message ?? error));
    if (debugSession) {
      debugSeq += 1;
      await screenshot(debugSession, join(outDir, `debug-${String(debugSeq).padStart(2, "0")}-${name.replace(/^[\d\s]+/, "").replace(/[^a-z0-9-]+/gi, "-").slice(0, 40)}.png`)).catch(() => {});
    }
    return "FAIL";
  }
}

async function expectText(session, needle, timeoutMs = 20_000) {
  await waitForCondition(session, `document.body.innerText.includes(${JSON.stringify(needle)})`, {
    timeoutMs, label: `text "${needle}"`,
  });
}
async function waitForUrl(session, hash, timeoutMs = 20_000) {
  await waitForCondition(session, `location.hash.startsWith(${JSON.stringify(hash)})`, {
    timeoutMs, label: `hash ${hash}`,
  });
}

function runCommand(command, args, options = {}) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], windowsHide: true, ...options });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => { stdout += chunk; });
    child.stderr.on("data", chunk => { stderr += chunk; });
    child.on("error", reject);
    child.on("exit", code => (code === 0 ? resolvePromise({ stdout, stderr }) : reject(new Error(`${command} exited ${code}: ${stderr.slice(-2000)}`))));
  });
}

/**
 * wsl.exe 的列表类输出是 UTF-16LE；去掉 NUL 再按行拆。
 * 注意：wsl.exe 的命令串会先过 Windows argv 解析，嵌入的双引号会被吃掉——
 * 所有 `wsl.exe -- sh -c <cmd>` 命令一律不写双引号/变量赋值，复杂逻辑放脚本文件。
 */
function cleanWslOutput(raw) {
  return raw.replaceAll("\0", "").trim();
}

async function resolveDistro() {
  if (DISTRO) return DISTRO;
  const { stdout } = await runCommand("wsl.exe", ["-l", "-q"]);
  const first = cleanWslOutput(stdout).split(/\r?\n/).map(line => line.trim()).find(Boolean);
  if (!first) throw new Error("no WSL distro found; install one or pass --distro");
  return first;
}

/** C:\Users\...\Temp\x -> /mnt/c/Users/.../Temp/x */
function toWslPath(windowsPath) {
  const normalized = windowsPath.replaceAll("\\", "/");
  const drive = normalized[0].toLowerCase();
  return `/mnt/${drive}${normalized.slice(2)}`;
}

/** 长驻进程统一以脚本文件 + 直接 argv 启动（绕开 wsl.exe 对 `-c` 串的二次解析）。 */
function wslSpawn(distro, scriptPath, args = []) {
  return spawn("wsl.exe", ["-d", distro, "--", "sh", scriptPath, ...args], { stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
}

function goCommand() {
  const bundled = resolve(repoRoot, ".tools/go/bin/go.exe");
  return existsSync(bundled) ? bundled : "go";
}

function npmBuildCommand() {
  const nodeDir = dirname(process.execPath);
  const npmCli = join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js");
  return existsSync(npmCli) ? [process.execPath, [npmCli]] : null;
}

async function freePort() {
  const { createServer } = await import("node:net");
  return new Promise((resolvePort, reject) => {
    const server = createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      server.close(() => resolvePort(port));
    });
  });
}

/** Windows 侧可达性探测：任何 TLS+HTTP 响应（含 502 代理应答）都算宿主已监听。 */
function waitForAnyHttps(url, label, timeoutMs = 45_000) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolveReady, rejectReady) => {
    function attempt() {
      const request = https.request(url, { method: "GET", rejectUnauthorized: false, timeout: 3000 }, response => {
        response.resume();
        resolveReady(response.statusCode ?? 0);
      });
      request.on("timeout", () => {
        request.destroy();
        retry("timeout");
      });
      request.on("error", error => retry(error.message));
      request.end();
    }
    function retry(reason) {
      if (Date.now() > deadline) {
        rejectReady(new Error(`${label} did not become reachable at ${url} (last: ${reason})`));
        return;
      }
      setTimeout(attempt, 500);
    }
    attempt();
  });
}

function waitForHttps(url, label, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolveReady, rejectReady) => {
    function attempt() {
      const request = https.request(url, { method: "GET", rejectUnauthorized: false, timeout: 3000 }, response => {
        response.resume();
        if (response.statusCode === 200) resolveReady();
        else retry(`status ${response.statusCode}`);
      });
      request.on("timeout", () => {
        request.destroy();
        retry("timeout");
      });
      request.on("error", error => retry(error.message));
      request.end();
    }
    function retry(reason) {
      if (Date.now() > deadline) {
        rejectReady(new Error(`${label} did not become ready at ${url} (last: ${reason})`));
        return;
      }
      setTimeout(attempt, 500);
    }
    attempt();
  });
}

const SETUP_SCRIPT = `
import { createHash, randomBytes } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
const dbPath = process.argv[2];
const setupToken = randomBytes(32).toString("base64url");
const digest = createHash("sha256").update(setupToken).digest("base64url");
const expires = new Date(Date.now() + 15 * 60_000).toISOString();
const db = new DatabaseSync(dbPath);
try {
  db.exec("PRAGMA busy_timeout = 8000");
  const row = db.prepare("SELECT payload, ever_initialized FROM cx_auth_identity WHERE singleton = 1").get();
  if (!row) throw new Error("cx_auth_identity row missing; migrations did not run");
  if (row.ever_initialized === 1) throw new Error("identity already initialized; refusing to reuse a live database");
  const payload = {
    ...(row.payload ? JSON.parse(Buffer.from(row.payload).toString("utf8")) : {}),
    Version: 1,
    Initialized: false,
    SetupDigest: digest,
    SetupExpires: expires,
  };
  const updated = db.prepare("UPDATE cx_auth_identity SET payload = ? WHERE singleton = 1 AND ever_initialized = 0").run(Buffer.from(JSON.stringify(payload)));
  if (updated.changes !== 1) throw new Error("setup token write raced another writer");
} finally {
  db.close();
}
process.stdout.write(setupToken);
`;

/** Web 宿主（WSL 内）：同源托管 dist + /api、/healthz 透传，与 70 步脚本的 Windows 版等价。 */
const WEBHOST_SCRIPT = `
import { readFileSync, statSync } from "node:fs";
import https from "node:https";
import { join, normalize, extname, sep, resolve } from "node:path";
const [certPath, keyPath, distDir, apiPort] = process.argv.slice(2);
const MIME = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2" };
const tlsOptions = { cert: readFileSync(certPath), key: readFileSync(keyPath) };
const server = https.createServer(tlsOptions, (req, res) => {
  const url = new URL(req.url ?? "/", \`https://\${req.headers.host ?? "127.0.0.1"}\`);
  if (url.pathname === "/healthz" || url.pathname === "/api" || url.pathname.startsWith("/api/")) {
    const headers = { ...req.headers };
    const upstream = https.request(
      { host: "127.0.0.1", port: Number(apiPort), path: req.url, method: req.method, headers, rejectUnauthorized: false },
      upstreamResponse => {
        res.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
        upstreamResponse.pipe(res);
      },
    );
    upstream.on("error", error => {
      if (!res.headersSent) res.writeHead(502, { "Content-Type": "application/json", "Cache-Control": "no-store" });
      res.end(JSON.stringify({ code: "PROXY_UNREACHABLE", message: String(error) }));
    });
    req.pipe(upstream);
    return;
  }
  void (async () => {
    const relative = url.pathname === "/" ? "index.html" : url.pathname.replace(/^\\/+/, "");
    const target = resolve(join(distDir, normalize(relative).replace(/^(\\.\\.[/\\\\])+/, "")));
    if (!target.startsWith(resolve(distDir) + sep)) { res.writeHead(403).end("forbidden"); return; }
    let file = target;
    try { const info = statSync(file); if (info.isDirectory()) file = join(file, "index.html"); }
    catch { file = join(distDir, "index.html"); }
    try {
      const body = readFileSync(file);
      res.writeHead(200, { "Content-Type": MIME[extname(file)] ?? "application/octet-stream", "Content-Length": body.length, "Cache-Control": "no-store" });
      res.end(req.method === "HEAD" ? undefined : body);
    } catch { res.writeHead(404).end("not found"); }
  })();
});
server.listen(Number(process.env.WEB_PORT), "0.0.0.0", () => console.log("webhost ready"));
`;

const PKI_SCRIPT = `
set -eu
work="$1"
mkdir -p "$work/pki" "$work/db" "$work/logs"
# localpki load() 兼容的本地 Agent CA：EC P-256、PKCS8 私钥、自签 CA（>12h 有效）。
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work/pki/local-agent-ca-key.pem" 2>/dev/null
openssl req -x509 -new -key "$work/pki/local-agent-ca-key.pem" -sha256 \\
  -subj "/CN=Server Fleet Local Agent CA" -days 30 \\
  -addext "basicConstraints=critical,CA:TRUE" \\
  -addext "keyUsage=critical,keyCertSign,cRLSign,digitalSignature" \\
  -out "$work/pki/local-agent-ca.pem" 2>/dev/null
# 服务端证书：链到同一本地 CA，覆盖 Agent 所需的 DNS:localhost 与浏览器 127.0.0.1。
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \\
  -keyout "$work/cert-key.pem" -subj "/CN=localhost" -out "$work/cert.csr" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\\nbasicConstraints=CA:FALSE\\nkeyUsage=digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth,clientAuth\\n" > "$work/cert-ext.cnf"
openssl x509 -req -in "$work/cert.csr" -CA "$work/pki/local-agent-ca.pem" -CAkey "$work/pki/local-agent-ca-key.pem" \\
  -days 2 -extfile "$work/cert-ext.cnf" -out "$work/cert.pem" 2>/dev/null
openssl verify -CAfile "$work/pki/local-agent-ca.pem" "$work/cert.pem" >/dev/null
echo pki-ready
`;

/**
 * 其余 WSL 操作全部走脚本文件：`wsl.exe -- sh -c <串>` 的命令串会被 WSL 默认 shell
 * 二次解析（引号丢失、相对路径落到映射盘 cwd），脚本文件只被 Linux sh 解析一次。
 * 长驻进程一律前台 exec：wsl.exe 客户端在测试期间保持会话存活；WSL 会在会话结束时
 * 终止其进程组——这正是可靠的清理机制（nohup 后台化反而会被会话结束立即杀死）。
 */
const RUN_WEBHOST_SCRIPT = `#!/bin/sh
set -eu
work="$1"
webPort="$2"
apiPort="$3"
cd "$work"
exec env WEB_PORT="$webPort" node "$work/webhost.mjs" "$work/cert.pem" "$work/cert-key.pem" "$work/dist" "$apiPort" > "$work/logs/webhost.log" 2>&1
`;

const RUN_CONTROLLER_SCRIPT = `#!/bin/sh
set -eu
work="$1"
apiPort="$2"
base="$3"
cd "$work"
# -agent-pki-dir 必须显式指向 pki.sh 预置的本地 CA：默认值是 -db 旁边的 db/pki，
# 会让控制器自签另一把 CA，与 Agent 钉扎的 SPKI 不一致，enroll 一律被拒。
exec "$work/controller" -db "$work/db/controller.db" -agent-pki-dir "$work/pki" \\
  -listen "0.0.0.0:$apiPort" -origin "$base" \\
  -tls-cert "$work/cert.pem" -tls-key "$work/cert-key.pem" \\
  -agent-authority "localhost:$apiPort" -agent-server-name localhost \\
  > "$work/logs/controller.log" 2>&1
`;

const RUN_AGENT_ENROLL_SCRIPT = `#!/bin/sh
set -eu
work="$1"
apiPort="$2"
token="$3"
cd "$work"
exec "$work/agent" -enroll-token "$token" -controller-url "https://localhost:$apiPort" \\
  -root-cert "$work/pki/local-agent-ca.pem" -state-dir "$work/agent-state-01"
`;

const RUN_AGENT_CONNECT_SCRIPT = `#!/bin/sh
set -eu
work="$1"
apiPort="$2"
waitTaskMs="$3"
cd "$work"
exec "$work/agent" -connect-once -controller-url "https://localhost:$apiPort" \\
  -root-cert "$work/pki/local-agent-ca.pem" -state-dir "$work/agent-state-01" \\
  -wait-task-ms "$waitTaskMs" > "$work/logs/agent-connect.json" 2> "$work/logs/agent-connect.err"
`;

const KILL_PID_SCRIPT = `#!/bin/sh
set -eu
work="$1"
name="$2"
case "$name" in agent-connect|controller|webhost) ;; *) exit 2 ;; esac
f="$work/pids/$name.pid"
if [ -f "$f" ]; then
  pid=$(cat "$f")
  case "$pid" in ''|*[!0-9]*) exit 2 ;; esac
  if [ "$pid" -gt 1 ] && [ -r "/proc/$pid/cmdline" ] && tr '\\000' ' ' < "/proc/$pid/cmdline" | grep -F -- "$work/" >/dev/null; then
    kill "$pid" 2>/dev/null || true
  fi
  rm -f "$f"
fi
`;

const CLEANUP_SCRIPT = `#!/bin/sh
set -eu
work="$1"
base=$(basename -- "$work")
printf '%s\\n' "$base" | grep -Eq '^sfqa-gm06-w6-[0-9]+-[0-9]+$' || exit 2
[ "$(dirname -- "$work")" = "$HOME" ] || exit 2
[ ! -L "$work" ] || exit 2
[ "$(readlink -f -- "$work")" = "$work" ] || exit 2
rm -rf -- "$work"
`;

const READ_FILE_SCRIPT = `#!/bin/sh
cat "$1" 2>/dev/null || true
`;

const PAGE_FETCH = `path => fetch(path, {
  headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
}).then(response => response.json())`;

const PAGE_WRITE = `async (path, method, body, extraHeaders) => {
  const me = await fetch("/api/v1/auth/me", {
    headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
  }).then(response => response.json());
  const response = await fetch(path, {
    method,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "ServerFleetWeb",
      "X-CSRF-Token": me.csrfToken ?? "",
      ...(extraHeaders ?? {}),
    },
    credentials: "include",
    cache: "no-store",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return { status: response.status, body: await response.json().catch(() => null) };
}`;

let tempDir = null;
let workDir = null;
let distro = "";
const children = [];
function track(child) {
  children.push(child);
  return child;
}

async function killWorkloadPid(name) {
  if (!workDir) return;
  await runCommand("wsl.exe", ["-d", distro, "--", "sh", `${workDir}/kill-pid.sh`, workDir, name]).catch(() => {});
}

async function cleanup() {
  for (const child of children) {
    try { child.kill(); } catch { /* already gone */ }
  }
  if (distro && workDir) {
    for (const name of ["agent-connect", "controller", "webhost"]) {
      await killWorkloadPid(name).catch(() => {});
    }
    if (!keepTemp) {
      await runCommand("wsl.exe", ["-d", distro, "--", "sh", `${toWslPath(tempDir)}/cleanup.sh`, workDir]).catch(() => {});
    }
  }
  if (tempDir && !keepTemp) {
    if (dirname(resolve(tempDir)) !== resolve(tmpdir()) || !basename(tempDir).startsWith("gm-w6-positive-")) {
      throw new Error("refusing cleanup outside this run's temporary directory");
    }
    await rm(tempDir, { recursive: true, force: true }).catch(() => {});
  }
}

async function main() {
  distro = await resolveDistro();
  console.log(`WSL distro: ${distro}`);

  if (!skipBuild) {
    console.log("building production bundle (npm run build --workspace @serverfleet/web)...");
    const npm = npmBuildCommand();
    if (!npm) throw new Error("npm-cli.js not found next to the running node; build apps/web manually and pass --skip-build");
    await runCommand(npm[0], [...npm[1], "run", "build", "--workspace", "@serverfleet/web"], { cwd: repoRoot });
  } else if (!existsSync(resolve(repoRoot, "apps/web/dist/index.html"))) {
    throw new Error("--skip-build given but apps/web/dist is missing; run the build first");
  }

  tempDir = await mkdtemp(join(tmpdir(), "gm-w6-positive-"));
  await mkdir(outDir, { recursive: true });
  console.log(`positive evidence out dir: ${outDir}`);
  console.log(`windows temp dir: ${tempDir}`);

  console.log("cross-building linux/amd64 controller + agent (CGO_ENABLED=0 GOOS=linux)...");
  await runCommand(goCommand(), ["build", "-o", join(tempDir, "controller"), "./apps/controller"], {
    cwd: repoRoot, env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: "amd64" },
  });
  await runCommand(goCommand(), ["build", "-o", join(tempDir, "agent"), "./apps/agent"], {
    cwd: repoRoot, env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: "amd64" },
  });

  await writeFile(join(tempDir, "setup-token.mjs"), SETUP_SCRIPT);
  await writeFile(join(tempDir, "webhost.mjs"), WEBHOST_SCRIPT);
  await writeFile(join(tempDir, "pki.sh"), PKI_SCRIPT.trimStart());
  await writeFile(join(tempDir, "run-webhost.sh"), RUN_WEBHOST_SCRIPT);
  await writeFile(join(tempDir, "run-controller.sh"), RUN_CONTROLLER_SCRIPT);
  await writeFile(join(tempDir, "run-agent-enroll.sh"), RUN_AGENT_ENROLL_SCRIPT);
  await writeFile(join(tempDir, "run-agent-connect.sh"), RUN_AGENT_CONNECT_SCRIPT);
  await writeFile(join(tempDir, "kill-pid.sh"), KILL_PID_SCRIPT);
  await writeFile(join(tempDir, "cleanup.sh"), CLEANUP_SCRIPT);
  await writeFile(join(tempDir, "read-file.sh"), READ_FILE_SCRIPT);

  // WSL 原生工作目录（SQLite WAL 语义正确、二进制可直接执行、Agent 状态目录私有）。
  // 目录名由 Windows 侧生成（纯 ASCII、无空格）；WSL 命令只用无引号 token。
  const workName = `sfqa-gm06-w6-${process.pid}-${Math.floor(Math.random() * 100000)}`;
  const homeOut = await runCommand("wsl.exe", ["-d", distro, "--", "printenv", "HOME"]);
  const linuxHome = cleanWslOutput(homeOut.stdout);
  const uidOut = await runCommand("wsl.exe", ["-d", distro, "--", "id", "-u"]);
  if (!/^\/home\/[A-Za-z0-9_.-]+$/.test(linuxHome) || !/^[1-9][0-9]*$/.test(cleanWslOutput(uidOut.stdout))) {
    throw new Error("an existing ordinary-user WSL home is required");
  }
  const setup = await runCommand("wsl.exe", ["-d", distro, "--", "sh", "-c",
    `mkdir -p $HOME/${workName}/pids && echo $HOME/${workName}`]);
  workDir = cleanWslOutput(setup.stdout).split(/\r?\n/).filter(Boolean).pop();
  if (workDir !== `${linuxHome}/${workName}`) {
    throw new Error(`unexpected WSL work dir: ${workDir}`);
  }
  console.log(`wsl work dir: ${workDir}`);

  const wslTemp = toWslPath(tempDir);
  await runCommand("wsl.exe", ["-d", distro, "--", "sh", "-c",
    `cp ${wslTemp}/controller ${wslTemp}/agent ${workDir}/ && chmod 700 ${workDir}/controller ${workDir}/agent && cp -r ${toWslPath(resolve(repoRoot, "apps/web/dist"))} ${workDir}/dist && cp ${wslTemp}/setup-token.mjs ${wslTemp}/webhost.mjs ${wslTemp}/pki.sh ${wslTemp}/run-webhost.sh ${wslTemp}/run-controller.sh ${wslTemp}/run-agent-enroll.sh ${wslTemp}/run-agent-connect.sh ${wslTemp}/kill-pid.sh ${wslTemp}/read-file.sh ${workDir}/ && sh ${workDir}/pki.sh ${workDir}`]);
  console.log("staged controller/agent/dist and prepared local PKI + localhost server certificate");

  const apiPort = await freePort();
  const webPort = await freePort();

  // Web 宿主先起（WSL 内 0.0.0.0），再从 Windows 探测可达基址：localhost 转发优先，回退 WSL IP。
  track(wslSpawn(distro, `${workDir}/run-webhost.sh`, [workDir, String(webPort), String(apiPort)]));
  await sleep(2000);
  let base = `https://127.0.0.1:${webPort}`;
  try {
    await waitForAnyHttps(`${base}/healthz`, "webhost via localhost", 12_000);
  } catch {
    const ipOut = await runCommand("wsl.exe", ["-d", distro, "--", "sh", "-c", "hostname -I"]);
    const wslIp = cleanWslOutput(ipOut.stdout).split(/\s+/).find(Boolean);
    if (!wslIp) throw new Error("webhost unreachable via localhost and no WSL IP available");
    base = `https://${wslIp}:${webPort}`;
    await waitForAnyHttps(`${base}/healthz`, "webhost via WSL IP", 12_000);
  }
  console.log(`browser base: ${base}; controller api (inside WSL): 127.0.0.1:${apiPort}`);

  track(wslSpawn(distro, `${workDir}/run-controller.sh`, [workDir, String(apiPort), base]));
  await waitForHttps(`${base}/healthz`, "controller through webhost", 60_000);

  // 本机安装器：以 WSL node 写入 setup token 摘要（算法与 internal/auth credentialDigest 一致）。
  const tokenOut = await runCommand("wsl.exe", ["-d", distro, "--", "node", `${workDir}/setup-token.mjs`, `${workDir}/db/controller.db`]);
  const setupToken = cleanWslOutput(tokenOut.stdout).split(/\r?\n/).filter(line => /^[A-Za-z0-9_-]+$/.test(line)).pop();
  if (!setupToken) throw new Error("setup token seeding failed");
  console.log("installer: setup token prepared (never logged), no nodes seeded — the node comes from real enrollment");

  const cdpPort = 52700 + Math.floor(Math.random() * 300);
  const browser = await launchHeadlessBrowser({ cdpPort, profileDir: join(tempDir, "profile") });
  track(browser.child);
  const session = await connectPage(cdpPort);
  debugSession = session;

  // =====================================================================
  // P 组：正常成功链路（真实 Agent + 真实任务执行）
  // =====================================================================

  await goto(session, `${base}/#/setup`);
  await stepPass("P01 bootstrap initializes the real controller admin", async () => {
    await expectText(session, "本机 setup token");
    await fillField(session, "本机 setup token", setupToken);
    await fillField(session, "管理员用户名", ADMIN_USER);
    await fillField(session, "密码", ADMIN_PASSWORD);
    await fillField(session, "确认密码", ADMIN_PASSWORD);
    await screenshot(session, join(outDir, "positive-01-bootstrap.png"));
    await clickText(session, "创建管理员并进入", { exact: true });
    await waitForUrl(session, "#/overview");
    await expectText(session, "概览");
    return "POST /api/v1/auth/bootstrap (204) + auto-login completed against the real controller";
  });

  await stepPass("P02 navigation preserves the authenticated writable session", async () => {
    await goto(session, `${base}/#/overview`);
    await expectText(session, "概览");
    await waitForCondition(session, "!document.body.innerText.includes('安全写入已锁定')", {
      timeoutMs: 10_000, label: "no write-lock banner",
    });
    return "navigation preserves the current writable session; this step does not prove full-reload CSRF recovery";
  });

  let enrollmentToken = null;
  await stepPass("P03 UI issues a one-time enrollment token (shown once only)", async () => {
    await goto(session, `${base}/#/nodes/new`);
    await expectText(session, "生成短期接入凭证", 20_000);
    await clickText(session, "生成一次性凭证", { exact: true });
    await expectText(session, "接入令牌（仅显示这一次）", 20_000);
    const token = await evaluate(session, `(() => {
      const nodes = [...document.querySelectorAll("code")];
      const hit = nodes.find(node => node.textContent.trim().length >= 20);
      return hit ? hit.textContent.trim() : null;
    })()`);
    if (!token) throw new Error("one-time token not rendered in the page");
    const leaked = await evaluate(session, `(${PAGE_FETCH})('/api/v1/node-enrollments').then(data => JSON.stringify(data).includes(${JSON.stringify(token)}))`);
    if (leaked === true) throw new Error("token leaked into the enrollment list response");
    enrollmentToken = token;
    await screenshot(session, join(outDir, "positive-03-enrollment-token.png"));
    return "POST /api/v1/node-enrollment-tokens -> 201; token rendered once and absent from list responses";
  });

  async function agentEnrollOnce() {
    const result = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
      `${workDir}/run-agent-enroll.sh`, workDir, String(apiPort), enrollmentToken]).catch(error => ({ failed: error }));
    if (result.failed) {
      // 服务端会把 agent 侧接入拒绝写入审计（enrollment.rejected + reasonCode）；取证后原样抛出。
      let auditHint = "";
      try {
        const audit = await evaluate(session, `(${PAGE_FETCH})('/api/v1/audit/events?limit=50').then(data =>
          (data.items ?? []).filter(entry => String(entry.action ?? "").startsWith("enrollment."))
            .slice(0, 5).map(entry => entry.action + ":" + (entry.reasonCode ?? entry.result ?? "?")).join(", "))`);
        auditHint = `; server audit: ${audit || "(no enrollment events)"}`;
      } catch (error) {
        auditHint = `; audit read failed: ${String(error.message ?? error).slice(0, 120)}`;
      }
      throw new Error(`agent enroll failed: ${String(result.failed.message ?? result.failed).slice(0, 200)}${auditHint}`);
    }
    return JSON.parse(cleanWslOutput(result.stdout).split(/\r?\n/).filter(Boolean).pop());
  }

  await stepPass("P04 real Linux agent enrolls and lands in pending approval", async () => {
    const observed = await agentEnrollOnce();
    if (observed.state !== "pending") throw new Error(`expected pending, got ${JSON.stringify(observed)}`);
    return "agent CLI (linux/amd64) enrolled over real HTTPS: state=pending";
  });

  await stepPass("P05 administrator approves the pending enrollment through the UI", async () => {
    await expectText(session, "待审批接入请求", 15_000);
    await clickText(session, "刷新", { exact: true });
    await expectText(session, "待审批", 20_000);
    await clickText(session, "批准", { exact: true });
    await expectText(session, "批准节点接入", 10_000);
    await clickText(session, "确认批准", { exact: true });
    await expectText(session, "已批准", 20_000);
    await screenshot(session, join(outDir, "positive-05-enrollment-approved.png"));
    return "POST /api/v1/node-enrollments/{id}/decision approve -> 200; node inventory created server-side";
  });

  let nodeId = null;
  await stepPass("P06 same one-time token fetches the issued identity with a nodeId", async () => {
    const issued = await agentEnrollOnce();
    if (issued.state !== "issued" || typeof issued.nodeId !== "string") {
      throw new Error(`expected issued identity, got ${JSON.stringify(issued)}`);
    }
    nodeId = issued.nodeId;
    return `identity issued server-side: nodeId=${nodeId}`;
  });

  const adminUserId = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => identity.userId)`);
  if (typeof adminUserId !== "string" || adminUserId.length === 0) throw new Error("could not read admin userId");

  await stepPass("P07 admin grants the enrolled node through the UI resource grants", async () => {
    await goto(session, `${base}/#/users`);
    await clickText(session, "资源授权", { exact: true });
    await expectText(session, "创建授权", 15_000);
    await clickText(session, "创建授权", { exact: true });
    await fillField(session, "主体 ID", adminUserId);
    await fillField(session, "资源 ID", nodeId);
    const permissions = ["nodes.read", "tasks.submit", "tasks.read", "tasks.command", "files.read", "files.write", "files.transfer"];
    for (const permission of permissions) {
      await setCheckbox(session, permission, true);
    }
    await sleep(600);
    for (const permission of permissions) {
      for (let retry = 0; retry < 3; retry += 1) {
        if ((await checkboxState(session, permission)) === true) break;
        await setCheckbox(session, permission, true);
        await sleep(400);
      }
      if ((await checkboxState(session, permission)) !== true) {
        throw new Error(`permission "${permission}" checkbox did not stick`);
      }
    }
    await clickText(session, "创建授权", { exact: true });
    await sleep(900);
    const grants = await evaluate(session, `(${PAGE_FETCH})('/api/v1/resource-grants').then(data => data.items)`);
    const grant = grants.find(entry => entry.principalId === adminUserId && entry.resourceId === nodeId);
    if (!grant) throw new Error("grant on the enrolled node not persisted");
    await screenshot(session, join(outDir, "positive-07-node-grant.png"));
    return `granted ${permissions.length} permissions on ${nodeId} through POST /api/v1/resource-grants`;
  });

  await stepPass("P08 real agent opens the controlled WSS+mTLS link and the node reports online", async () => {
    track(wslSpawn(distro, `${workDir}/run-agent-connect.sh`, [workDir, String(apiPort), "240000"]));
    let online = false;
    for (let attempt = 0; attempt < 30 && !online; attempt += 1) {
      await sleep(1000);
      const probe = await evaluate(session, `(${PAGE_FETCH})('/api/v1/nodes').then(data => {
        const found = (data.items ?? []).find(entry => entry.id === ${JSON.stringify(nodeId)});
        return found ? found.online === true : false;
      })`).catch(() => false);
      online = probe === true;
    }
    if (!online) {
      const errLog = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
        `${workDir}/read-file.sh`, `${workDir}/logs/agent-connect.err`]).catch(() => ({ stdout: "" }));
      const ctlLog = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
        `${workDir}/read-file.sh`, `${workDir}/logs/controller.log`]).catch(() => ({ stdout: "" }));
      throw new Error(`agent link did not make the node online (agent err: ${cleanWslOutput(errLog.stdout).slice(0, 200)}; controller: ${cleanWslOutput(ctlLog.stdout).slice(-200)})`);
    }
    await goto(session, `${base}/#/nodes`);
    await expectText(session, ENROLLED_NODE_LABEL, 20_000);
    await screenshot(session, join(outDir, "positive-08-agent-online.png"));
    return `real agent heartbeat/metrics over WSS+mTLS; node ${nodeId} online and listed in the console`;
  });

  let taskId = null;
  await stepPass("P09 UI submits a standard tasks.command task for the enrolled node", async () => {
    await goto(session, `${base}/#/tasks`);
    await expectText(session, "创建任务", 20_000);
    await clickText(session, "创建任务", { exact: true });
    await fillField(session, "任务名称", "agent self-check positive");
    await fillField(session, "命令或脚本", "tasks.command");
    await setCheckbox(session, ENROLLED_NODE_LABEL, true);
    await clickText(session, "预演目标", { exact: true });
    await expectText(session, "需要审批：否", 20_000);
    await setCheckbox(session, "预演（dryRun）", false);
    await screenshot(session, join(outDir, "positive-09-task-submit.png"));
    await clickText(session, "提交任务", { exact: true });
    await waitForUrl(session, "#/tasks/");
    await expectText(session, "任务快照", 30_000);
    taskId = decodeURIComponent(((await evaluate(session, "location.hash")).split("#/tasks/")[1] ?? "").replace(/\/$/, ""));
    if (!taskId.startsWith("tsk_")) throw new Error(`unexpected task id ${taskId}`);
    return `POST /api/v1/tasks -> 202 ${taskId} (standard class, no approval required, real dispatcher)`;
  });

  await stepPass("P10 real agent executes the task; task completes with a succeeded execution", async () => {
    await expectText(session, "已完成", 60_000);
    const detail = await evaluate(session, `(${PAGE_FETCH})('/api/v1/tasks/${taskId}')`);
    if (detail.state !== "completed" && detail.job?.state !== "completed") {
      throw new Error(`task did not complete: ${JSON.stringify(detail).slice(0, 300)}`);
    }
    const executions = await evaluate(session, `(${PAGE_FETCH})('/api/v1/tasks/${taskId}/executions')`);
    const item = (executions.items ?? [])[0];
    if (!item || item.state !== "succeeded") {
      throw new Error(`execution not succeeded: ${JSON.stringify(executions).slice(0, 300)}`);
    }
    // Agent 一次性结果 JSON 在其进程退出时落盘；任务完成后稍等其收尾。
    let agentResult = null;
    for (let attempt = 0; attempt < 15 && !agentResult; attempt += 1) {
      await sleep(1000);
      const agentResultRaw = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
        `${workDir}/read-file.sh`, `${workDir}/logs/agent-connect.json`]).catch(() => ({ stdout: "" }));
      const line = cleanWslOutput(agentResultRaw.stdout).split(/\r?\n/).filter(Boolean).pop();
      if (!line) continue;
      const parsed = JSON.parse(line);
      if (parsed.tasksExecuted === 1) agentResult = parsed;
    }
    if (!agentResult) {
      throw new Error("agent one-shot result JSON never reported tasksExecuted=1");
    }
    await waitForCondition(session, `document.querySelector('h1')?.textContent === ${JSON.stringify(`任务 ${taskId}`)}`, { timeoutMs: 20_000, label: "real task identifier in heading" });
    await screenshot(session, join(outDir, "positive-10-task-completed.png"));
    return `task ${taskId} -> completed; execution state=${item.state}; agent one-shot reports tasksExecuted=1 (real TaskOffer/TaskFinished over the link)`;
  });

  await stepPass("P11 node page reflects the real connection lifecycle honestly", async () => {
    await goto(session, `${base}/#/nodes/${nodeId}`);
    await expectText(session, "节点元数据", 20_000);
    await expectText(session, "连接诊断", 15_000);
    // connect-once Agent 在任务执行后退出；bridge.disconnected 使节点按服务端快照诚实离线。
    // 在线证据已由 P08（心跳/指标 + 列表在线）证明，这里验证页面忠实反映服务端状态。
    const summary = await evaluate(session, `(${PAGE_FETCH})('/api/v1/nodes').then(data => (data.items ?? []).find(entry => entry.id === ${JSON.stringify(nodeId)}))`);
    if (!summary) throw new Error("enrolled node disappeared from the granted node list");
    if (summary.online !== false) throw new Error(`expected honest offline after the one-shot agent exited: ${JSON.stringify(summary)}`);
    await screenshot(session, join(outDir, "positive-11-node-lifecycle.png"));
    return `node ${nodeId} rendered from server truth: online during the link (P08), honestly offline after the one-shot agent exit`;
  });

  await stepPass("P12 audit records the real enrollment and task chain", async () => {
    await goto(session, `${base}/#/audit`);
    await expectText(session, "auth.login", 25_000);
    const text = await evaluate(session, "document.body.innerText");
    const expected = ["enrollment.token", "enrollment.request", "enrollment.approved", "tasks.submit"];
    const missing = expected.filter(action => !text.includes(action));
    if (missing.length > 0) throw new Error(`audit missing actions: ${missing.join(", ")}`);
    await screenshot(session, join(outDir, "positive-12-audit.png"), { width: 1280, height: 1400 });
    return "GET /api/v1/audit/events covers enrollment.token/request/decide + tasks.submit";
  });

  // =====================================================================
  // B组是尚未完成的正向流程：仅登记明确的环境/夹具缺口。
  // 不把消息入队、更新检查、恢复预演或任何错误响应算作完整功能成功。
  // =====================================================================

  await attemptPositive("B01 cross-node byte transfer between real agents", async () => {
    throw blocked("HARNESS_INCOMPLETE: 本套件仅接入一个已退出的单次Agent，未准备两个在线端点和真实源文件；不发送同节点伪跨机请求，不凭completed状态关闭字节/SHA/双端任务验收");
  });

  await attemptPositive("B02 AI message generation with streaming", async () => {
    const created = await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/sessions', 'POST', {})`);
    if (![200,201].includes(created.status)) throw new Error(`ai session create ${created.status}`);
    const sessionId=created.body?.sessionId??created.body?.id;
    if (!sessionId) throw new Error("ai session id missing");
    const sent=await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/sessions/${sessionId}/messages','POST',{
      content:"列出当前节点的运行时间",clientMessageId:"positive-w6-ai-01",scopeRevision:${JSON.stringify(created.body?.scopeRevision??0)}
    },{"Idempotency-Key":"positive-w6-ai-msg-01"})`);
    if(expectedUnavailable(sent,[[503,"MESSAGE_SOURCE_UNAVAILABLE"]])) throw blocked(`model unavailable: ${sent.status} ${JSON.stringify(sent.body)}`);
    throw blocked("HARNESS_INCOMPLETE: 这里只证明用户消息已提交；未配置合成BYOK/本地TLS模型、调用generate并验证assistant终态/SSE，不能把排队或用户文本回显算生成成功");
  });

  await attemptPositive("B03 Xray installation against a real published artifact", async () => {
    const response=await evaluate(session, `(${PAGE_WRITE})('/api/v1/plugins/catalog/refresh','POST')`);
    if(expectedUnavailable(response,[[501,"PLUGIN_CATALOG_SOURCE_UNAVAILABLE"]])) throw blocked(`catalog unavailable: ${response.status} ${JSON.stringify(response.body)}`);
    throw blocked("HARNESS_INCOMPLETE: 目录可用不代表安装完成；仍缺已准入安全制品与安装/配置/用户/流量实际执行证据");
  });

  await attemptPositive("B04 service publication succeeds end to end with compensation", async () => {
    throw blocked("HARNESS_INCOMPLETE: 未创建真实publication及网关/DNS/证书夹具，也未执行和验证补偿；不能把service-group ID当publication ID，或把202接收当发布完成");
  });

  await attemptPositive("B05 controller update executes with signature verification", async () => {
    const response=await evaluate(session, `(${PAGE_WRITE})('/api/v1/system/updates/check','POST',{channel:"stable"})`);
    if(expectedUnavailable(response,[[503,"UPDATE_SOURCE_UNAVAILABLE"],[503,"UPDATE_PLANNER_UNAVAILABLE"]])) throw blocked(`update source unavailable: ${response.status} ${JSON.stringify(response.body)}`);
    throw blocked("HARNESS_INCOMPLETE: 版本检查成功仅证明发行源可读；未执行签名安装、进程健康、升级后版本或失败回滚");
  });

  await attemptPositive("B06 disaster recovery executes against an isolated second controller", async () => {
    const response=await evaluate(session, `(${PAGE_WRITE})('/api/v1/disaster-recovery/preview','POST',{
      backupId:"positive-w6-no-backup",targetControllerId:"controller-positive-02",oldControllerFenced:true,recoveryKeyReference:"qa-key-ref-nonexistent"
    })`);
    if(expectedUnavailable(response,[[404,"BACKUP_NOT_FOUND"]])) throw blocked(`backup unavailable: ${response.status} ${JSON.stringify(response.body)}`);
    throw blocked("HARNESS_INCOMPLETE: preview成功（即使checks为空）也不代表恢复；未构建第二隔离Controller、实际fence、隔离恢复及撤销不复活证明");
  });

  await session.close();
}

try {
  await main();
} catch (error) {
  console.error(`positive acceptance aborted: ${error.stack ?? error}`);
  process.exitCode = 1;
} finally {
  await cleanup();
}

const passed = results.filter(entry => entry.state === "PASS");
const blockedCases = results.filter(entry => entry.state === "BLOCKED");
const failed = results.filter(entry => entry.state === "FAIL");
console.log(`\npositive success acceptance: ${passed.length} PASS, ${blockedCases.length} BLOCKED, ${failed.length} FAIL (${results.length} steps)`);
if (blockedCases.length > 0) {
  console.log("blocked (honest gaps, not feature completions):");
  for (const entry of blockedCases) console.log(`  - ${entry.step}: ${entry.detail}`);
}
if (failed.length > 0) {
  console.log("failed steps:");
  for (const entry of failed) console.log(`  - ${entry.step}: ${entry.detail}`);
  process.exitCode = 1;
}

await mkdir(outDir,{recursive:true});
await writeFile(join(outDir,"results.json"),JSON.stringify({status:failed.length?"FAIL":blockedCases.length?"PARTIAL":"PASS",complete:failed.length===0&&blockedCases.length===0,passed:passed.length,blocked:blockedCases.length,failed:failed.length,results},null,2)+"\n");
if(hasFlag("require-complete")&&blockedCases.length)process.exitCode=1;
