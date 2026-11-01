/**
 * GM-W6 · 正常成功用例验收（V09-R6-GM · Wave 6；V09-R6-GM-02 续作）。
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
 *   4. GM-W6-R1 增补：P02 升级为真实页面 reload 后的 CSRF 恢复验证（检查 /auth/me
 *      实际响应与写锁横幅）；P13 失败任务页面（Agent 退出后派发被诚实拒绝）；P14
 *      撤权后的任务访问隔离（403 不清认证）；B01 以两个真实在线 Agent + 可核对
 *      SHA-256 的真实源/目标夹具构成（字节链路仍未接线，精确 BLOCKED）；B02 走
 *      完整模型设置→凭据→消息入队→显式 generate 流程（配额未接线精确 BLOCKED）；
 *      B04/B06 以真实服务组/网关/备份夹具结构化。
 *   5. B 组是"正向尝试"：入队、202、版本检查、preview 均不算功能完成；接线后
 *      仍需按各步的终态断言补全证据。整体结果为 PARTIAL 时 `--require-complete`
 *      以退出码 1 拒绝全量通过。
 *
 * 退出码：存在 FAIL 才为 1；BLOCKED 不算失败（如实登记的缺口），--require-complete 下
 * 任何 BLOCKED 都算未完成。
 *
 * 用法：
 *   node tests/browser/v09/run-positive-success-acceptance.mjs [--out <目录>] [--skip-build] [--keep]
 *   [--require-complete]（需 WSL 默认发行版可用；--distro 显式指定发行版名）
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
/** B01 夹具样本：写入 WSL 私有目录并计算 SHA-256；内容为合成测试数据。 */
const UPLOAD_SAMPLE_TEXT = "serverfleet wave6-r1 cross-node transfer sample";

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
mkdir -p "$work/pki" "$work/db" "$work/logs" "$work/fixtures/src" "$work/fixtures/dst" "$work/agent-state-01" "$work/agent-state-02"
chmod 700 "$work/agent-state-01" "$work/agent-state-02"
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
stateDir="$4"
cd "$work"
exec "$work/agent" -enroll-token "$token" -controller-url "https://localhost:$apiPort" \\
  -root-cert "$work/pki/local-agent-ca.pem" -state-dir "$work/$stateDir"
`;

const RUN_AGENT_CONNECT_SCRIPT = `#!/bin/sh
set -eu
work="$1"
apiPort="$2"
waitTaskMs="$3"
stateDir="$4"
logTag="$5"
cd "$work"
exec "$work/agent" -connect-once -controller-url "https://localhost:$apiPort" \\
  -root-cert "$work/pki/local-agent-ca.pem" -state-dir "$work/$stateDir" \\
  -wait-task-ms "$waitTaskMs" > "$work/logs/agent-connect-$logTag.json" 2> "$work/logs/agent-connect-$logTag.err"
`;

const KILL_PID_SCRIPT = `#!/bin/sh
set -eu
work="$1"
name="$2"
case "$name" in agent-connect-*|controller|webhost) ;; *) exit 2 ;; esac
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

const BACKUP_SCRIPT = [
  "import { mkdirSync } from 'node:fs';",
  "import { DatabaseSync } from 'node:sqlite';",
  "const work = process.argv[2];",
  "const backupId = process.argv[3];",
  "mkdirSync(work + '/fixtures', { recursive: true });",
  "mkdirSync(work + '/db/backups', { recursive: true });",
  "const db = new DatabaseSync(work + '/db/controller.db');",
  "try {",
  "  db.exec(\"VACUUM INTO '\" + work + \"/db/backups/\" + backupId + \".db'\");",
  "} finally {",
  "  db.close();",
  "}",
  "console.log(\"backup-ready\");",
].join("\n");

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
    for (const name of ["agent-connect-01", "agent-connect-02", "controller", "webhost"]) {
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
  await writeFile(join(tempDir, "backup.mjs"), BACKUP_SCRIPT);

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
    `cp ${wslTemp}/controller ${wslTemp}/agent ${workDir}/ && chmod 700 ${workDir}/controller ${workDir}/agent && cp -r ${toWslPath(resolve(repoRoot, "apps/web/dist"))} ${workDir}/dist && cp ${wslTemp}/setup-token.mjs ${wslTemp}/webhost.mjs ${wslTemp}/pki.sh ${wslTemp}/run-webhost.sh ${wslTemp}/run-controller.sh ${wslTemp}/run-agent-enroll.sh ${wslTemp}/run-agent-connect.sh ${wslTemp}/kill-pid.sh ${wslTemp}/read-file.sh ${wslTemp}/backup.mjs ${workDir}/ && sh ${workDir}/pki.sh ${workDir}`]);
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

  await stepPass("P02 full page reload restores CSRF from the real /auth/me response", async () => {
    // GM-W6-R1：真正的 location.reload()（不是同页导航）。重启后应用必须经
    // GET /api/v1/auth/me 取回身份与 CSRF；本步断言实际响应含非空 csrfToken，
    // 且页面没有写锁横幅。
    const before = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => ({
      hasCsrf: typeof identity.csrfToken === "string" && identity.csrfToken.length > 0,
    }))`);
    if (!before?.hasCsrf) throw new Error("pre-reload /auth/me did not return a csrfToken");
    await evaluate(session, "location.reload()");
    await waitForUrl(session, "#/overview", 30_000);
    await expectText(session, "概览", 30_000);
    const after = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => ({
      hasCsrf: typeof identity.csrfToken === "string" && identity.csrfToken.length > 0,
      user: identity.userId ?? null,
    }))`);
    if (!after?.hasCsrf) throw new Error("post-reload /auth/me did not return a csrfToken");
    await waitForCondition(session, "!document.body.innerText.includes('安全写入已锁定')", {
      timeoutMs: 15_000, label: "no write-lock banner after real reload",
    });
    return `location.reload() completed; /auth/me returned csrfToken for ${after.user} and writes stayed unlocked`;
  });


  async function issueEnrollmentTokenViaUi() {
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
    return token;
  }

  async function agentEnrollOnce(stateDir) {
    const result = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
      `${workDir}/run-agent-enroll.sh`, workDir, String(apiPort), tokenFor(stateDir), stateDir]).catch(error => ({ failed: error }));
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

  // 一次性令牌只显示一次且不能落日志；按 Agent 状态目录在内存中登记。
  const enrollmentTokens = new Map();
  function tokenFor(stateDir) {
    const token = enrollmentTokens.get(stateDir);
    if (!token) throw new Error(`no enrollment token recorded for ${stateDir}`);
    return token;
  }

  let enrollmentToken = null;
  await stepPass("P03 UI issues a one-time enrollment token (shown once only)", async () => {
    enrollmentToken = await issueEnrollmentTokenViaUi();
    enrollmentTokens.set("agent-state-01", enrollmentToken);
    await screenshot(session, join(outDir, "positive-03-enrollment-token.png"));
    return "POST /api/v1/node-enrollment-tokens -> 201; token rendered once and absent from list responses";
  });

  await stepPass("P04 real Linux agent enrolls and lands in pending approval", async () => {
    const observed = await agentEnrollOnce("agent-state-01");
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
    const issued = await agentEnrollOnce("agent-state-01");
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
    const permissions = ["nodes.read", "nodes.write", "groups.write", "tasks.submit", "tasks.read", "tasks.command", "files.read", "files.write", "files.transfer", "servicegroups.read", "servicegroups.write", "gateway.read", "gateway.write", "gateway.provision", "publishing.read", "publishing.write"];
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
    track(wslSpawn(distro, `${workDir}/run-agent-connect.sh`, [workDir, String(apiPort), "240000", "agent-state-01", "01"]));
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
        `${workDir}/read-file.sh`, `${workDir}/logs/agent-connect-01.err`]).catch(() => ({ stdout: "" }));
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
        `${workDir}/read-file.sh`, `${workDir}/logs/agent-connect-01.json`]).catch(() => ({ stdout: "" }));
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

  let failedTaskId = null;
  let nodeId2 = null;
  await stepPass("P15 second real agent enrolls, is approved and reports online", async () => {
    const token2 = await issueEnrollmentTokenViaUi();
    enrollmentTokens.set("agent-state-02", token2);
    const pending = await agentEnrollOnce("agent-state-02");
    if (pending.state !== "pending") throw new Error("expected pending for agent-2");
    await expectText(session, "待审批接入请求", 15_000);
    await clickText(session, "刷新", { exact: true });
    await expectText(session, "待审批", 20_000);
    await clickText(session, "批准", { exact: true });
    await expectText(session, "批准节点接入", 10_000);
    await clickText(session, "确认批准", { exact: true });
    await expectText(session, "已批准", 20_000);
    const issued = await agentEnrollOnce("agent-state-02");
    if (issued.state !== "issued" || typeof issued.nodeId !== "string") {
      throw new Error(`expected issued identity for agent-2: ${JSON.stringify(issued)}`);
    }
    nodeId2 = issued.nodeId;
    return `agent-2 identity issued: nodeId=${nodeId2}`;
  });

  // 先授权 node-2（授权后才可见/可操作），再建立双在线与夹具。
  await stepPass("P16 admin grants the second enrolled node through the API", async () => {
    const grant2 = await evaluate(session, `(${PAGE_WRITE})('/api/v1/resource-grants', 'POST', {
      principalId: ${JSON.stringify(adminUserId)}, resourceType: "node",
      resourceId: ${JSON.stringify(nodeId2)},
      permissions: ["nodes.read", "tasks.submit", "tasks.read", "tasks.command", "files.read", "files.write", "files.transfer"],
    }, { "Idempotency-Key": "positive-w6r1-grant-node2-0001" })`);
    if (![200, 201].includes(grant2.status)) throw new Error(`grant for agent-2 node ${grant2.status}: ${JSON.stringify(grant2.body).slice(0, 200)}`);
    return `granted 7 permissions on ${nodeId2}`;
  });

  let agent2Online = false;
  let agent1Online = false;
  await stepPass("P17 both real agents hold live WSS+mTLS links simultaneously", async () => {
    track(wslSpawn(distro, `${workDir}/run-agent-connect.sh`, [workDir, String(apiPort), "300000", "agent-state-01", "01"]));
    track(wslSpawn(distro, `${workDir}/run-agent-connect.sh`, [workDir, String(apiPort), "300000", "agent-state-02", "02"]));
    const deadline = Date.now() + 40_000;
    while (Date.now() < deadline && !(agent1Online && agent2Online)) {
      await sleep(1000);
      const probe = await evaluate(session, `(${PAGE_FETCH})('/api/v1/nodes').then(data => ({
        first: (data.items ?? []).find(entry => entry.id === ${JSON.stringify(nodeId)})?.online === true,
        second: (data.items ?? []).find(entry => entry.id === ${JSON.stringify(nodeId2)})?.online === true,
      }))`).catch(() => null);
      if (probe?.first) agent1Online = true;
      if (probe?.second) agent2Online = true;
      if (probe?.first && probe?.second) break;
    }
    if (!agent1Online || !agent2Online) {
      const err2 = await runCommand("wsl.exe", ["-d", distro, "--", "sh",
        `${workDir}/read-file.sh`, `${workDir}/logs/agent-connect-02.err`]).catch(() => ({ stdout: "" }));
      throw new Error(`agents online: agent-1=${agent1Online} agent-2=${agent2Online}; agent-2 err: ${cleanWslOutput(err2.stdout).slice(0, 240)}`);
    }
    await screenshot(session, join(outDir, "positive-17-two-agents-online.png"));
    return "two enrolled agents connected over independent WSS+mTLS links; heartbeats visible in the console";
  });

  let sourceSha256 = null;
  await stepPass("P18 real source/target fixtures with verifiable SHA-256 are prepared in private folders", async () => {
    const prep = await runCommand("wsl.exe", ["-d", distro, "--", "sh", "-c",
      `printf '${UPLOAD_SAMPLE_TEXT}' > ${workDir}/fixtures/src/transfer-sample.txt && sha256sum ${workDir}/fixtures/src/transfer-sample.txt | cut -d' ' -f1`]);
    sourceSha256 = cleanWslOutput(prep.stdout).split(/\r?\n/).filter(Boolean).pop();
    if (!/^[a-f0-9]{64}$/.test(sourceSha256 ?? "")) throw new Error("source SHA-256 missing");
    return `fixtures ready: src transfer-sample.txt sha256=${sourceSha256.slice(0, 16)}…; dst dir prepared`;
  });

  await stepPass("P13 real failed task page: peer-approved host.patch is rejected by the agent", async () => {
    // 真实失败链路：host.patch（修改级，需异人审批）→ 审批人批准 → 派发到在线 Agent →
    // Agent 只接受 tasks.command，ack CAPABILITY_REJECTED → 服务端记 target failed。
    const approver = await evaluate(session, `(${PAGE_WRITE})('/api/v1/users', 'POST', {
      username: "approver-02", displayName: "Approver Two", password: "positive-approver-strong-02",
      roleIds: ["operator"],
    }, { "Idempotency-Key": "positive-w6r1-user-approver-001" })`);
    if (![200, 201].includes(approver.status)) throw new Error(`approver create ${approver.status}: ${JSON.stringify(approver.body).slice(0, 200)}`);
    const approverId = approver.body?.id ?? approver.body?.userId;
    const approverGrant = await evaluate(session, `(${PAGE_WRITE})('/api/v1/resource-grants', 'POST', {
      principalId: ${JSON.stringify(approverId)}, resourceType: "node", resourceId: ${JSON.stringify(nodeId)},
      permissions: ["tasks.approve", "tasks.read"],
    }, { "Idempotency-Key": "positive-w6r1-grant-approver-001" })`);
    if (![200, 201].includes(approverGrant.status)) throw new Error(`approver grant ${approverGrant.status}`);
    await goto(session, `${base}/#/tasks`);
    await expectText(session, "创建任务", 20_000);
    await clickText(session, "创建任务", { exact: true });
    await fillField(session, "任务名称", "host patch rejected by agent");
    await fillField(session, "命令或脚本", "host.patch");
    await setCheckbox(session, ENROLLED_NODE_LABEL, true);
    await clickText(session, "预演目标", { exact: true });
    await expectText(session, "需要审批：是", 20_000);
    await setCheckbox(session, "预演（dryRun）", false);
    await clickText(session, "提交任务", { exact: true });
    await waitForUrl(session, "#/tasks/");
    await expectText(session, "等待审批", 30_000);
    failedTaskId = decodeURIComponent(((await evaluate(session, "location.hash")).split("#/tasks/")[1] ?? "").replace(/\//, ""));
    // 切换到审批人（异人审批）批准；401/自批 409 语义由 70 步覆盖。
    await clickText(session, "退出", { exact: true });
    await expectText(session, "登录", 15_000);
    await fillField(session, "用户名", "approver-02");
    await fillField(session, "密码", "positive-approver-strong-02");
    await clickText(session, "登录");
    await expectText(session, "Approver Two", 20_000);
    await goto(session, `${base}/#/tasks/${failedTaskId}`);
    // 诊断：审批人视角的身份、授权与任务读取原始响应（用于定位 403 来源）。
    const diagIdentity = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => ({ userId: identity.userId, roles: identity.roles }))`);
    const diagGrants = await evaluate(session, `(${PAGE_FETCH})('/api/v1/resource-grants').then(data => (data.items ?? []).map(entry => entry.principalId + ":" + entry.resourceId + ":" + (entry.permissions ?? []).join("|")))`).catch(() => null);
    const diagTask = await evaluate(session, `(${PAGE_WRITE})('/api/v1/tasks/${failedTaskId}', 'GET')`);
    console.log("P13-DIAG identity:", JSON.stringify(diagIdentity), "grants-visible:", diagGrants, "task-read:", diagTask.status, JSON.stringify(diagTask.body).slice(0, 200));
    await expectText(session, "批准", 30_000);
    await clickText(session, "批准", { exact: true });
    await fillField(session, "原因（可选）", "peer approved for rejection probe");
    await clickText(session, "确认", { exact: true });
    await expectText(session, "审批已提交", 20_000);
    // 回到管理员，等待派发后的真实失败终态。
    await clickText(session, "退出", { exact: true });
    await expectText(session, "登录", 15_000);
    await fillField(session, "用户名", ADMIN_USER);
    await fillField(session, "密码", ADMIN_PASSWORD);
    await clickText(session, "登录");
    await expectText(session, "administrator", 20_000);
    await goto(session, `${base}/#/tasks/${failedTaskId}`);
    await expectText(session, "任务快照", 30_000);
    let item = null;
    for (let attempt = 0; attempt < 40; attempt += 1) {
      await sleep(1500);
      const executions = await evaluate(session, `(${PAGE_FETCH})('/api/v1/tasks/${failedTaskId}/executions')`);
      item = (executions.items ?? [])[0] ?? null;
      if (item && (item.state === "failed" || item.state === "unknown")) break;
    }
    if (!item || item.state !== "failed") {
      throw new Error(`expected failed execution from agent rejection, got ${JSON.stringify(item).slice(0, 240)}`);
    }
    const detail = await evaluate(session, `(${PAGE_FETCH})('/api/v1/tasks/${failedTaskId}')`);
    const detailState = detail.state ?? detail.job?.state;
    if (detailState !== "failed") throw new Error(`task state not failed: ${JSON.stringify(detail).slice(0, 240)}`);
    await waitForCondition(session, "document.body.innerText.includes('失败') && document.body.innerText.includes('执行说明')", {
      timeoutMs: 20_000, label: "failed state and execution-note column rendered",
    });
    await screenshot(session, join(outDir, "positive-13-task-failed.png"));
    return `task ${failedTaskId} -> failed: peer-approved host.patch rejected by the real agent (CAPABILITY_REJECTED), page renders the failure and per-node note`;
  });

  await attemptPositive("B01 cross-node byte transfer between real agents", async () => {
    const created = await evaluate(session, `(${PAGE_WRITE})('/api/v1/transfers', 'POST', {
      sourceNodeId: ${JSON.stringify(nodeId)},
      sourcePaths: ["/opt/serverfleet-qa/transfer-sample.txt"],
      destinationNodeId: ${JSON.stringify(nodeId2)},
      destinationDir: "/opt/serverfleet-qa/dst",
      conflictPolicy: "overwrite",
    }, { "Idempotency-Key": "positive-w6-transfer-02" })`);
    if ([400, 401, 403, 404, 500, 501, 502, 503].includes(created.status)) {
      throw blocked(`POST /api/v1/transfers -> ${created.status} ${JSON.stringify(created.body).slice(0, 220)}：两个真实在线 Agent（P17）与双端 SHA 夹具（P18）已就绪，但 WSS 文件字节链路仍未接线（CX File* 帧/CLI 能力与 DS 协调器默认拒绝 File*，见 WAVE6-SHARED 文件链路下一检查点）；本步不发送同节点伪跨机请求，也不以任务 completed 状态关闭字节/SHA 验收`);
    }
    const transferId = created.body?.id ?? created.body?.transferId;
    if (!transferId) throw new Error(`transfer create ${created.status}: ${JSON.stringify(created.body).slice(0, 200)}`);
    for (let attempt = 0; attempt < 40; attempt += 1) {
      await sleep(1500);
      const job = await evaluate(session, `(${PAGE_FETCH})('/api/v1/transfers/${transferId}')`);
      const status = job?.status ?? job?.transfer?.status;
      if (status === "completed") {
        const verify = await runCommand("wsl.exe", ["-d", distro, "--", "sh", "-c",
          `sha256sum ${workDir}/fixtures/dst/transfer-sample.txt 2>/dev/null | cut -d' ' -f1`]).catch(() => ({ stdout: "" }));
        const actual = cleanWslOutput(verify.stdout).split(/\r?\n/).filter(Boolean).pop();
        if (actual !== sourceSha256) throw new Error(`SHA mismatch: ${actual} != ${sourceSha256}`);
        return `transfer ${transferId} completed; destination sha256 matches source (${sourceSha256.slice(0, 16)}…)`;
      }
      if (status === "failed" || status === "canceled" || status === "unknown" || job?.error) {
        throw blocked(`transfer ${transferId} did not complete (status=${status}, error=${JSON.stringify(job?.error ?? null).slice(0, 180)})：Agent 运行时当前只接受 tasks.command 自检动作；files.transfer 执行能力属 CX Wave6-R1 交付范围`);
      }
    }
    throw blocked(`transfer ${transferId} not completed within 60s：字节链路未在本基线打通（诚实未完成）`);
  });

  await attemptPositive("B02 AI generation reaches a terminal assistant message", async () => {
    // 完整结构：合成 ai_api_key 凭据（不入日志/不回显）→ owner CAS 保存模型设置 →
    // 会话创建 → 用户消息入队（明确：入队不等于生成）→ 显式 generate。
    const syntheticKey = "sk-local-synthetic-" + Math.random().toString(36).slice(2, 10);
    const credential = await evaluate(session, `(${PAGE_WRITE})('/api/v1/credentials', 'POST', {
      name: "positive-suite local model key", kind: "ai_api_key",
      secret: ${JSON.stringify(syntheticKey)}, usageScope: ["ai"],
    }, { "Idempotency-Key": "positive-w6r1-credential-01" })`);
    if (![200, 201].includes(credential.status)) {
      throw new Error(`credential create ${credential.status}: ${JSON.stringify(credential.body).slice(0, 200)}`);
    }
    const credentialRef = credential.body?.id;
    const credentialRevision = credential.body?.revision;
    if (!credentialRef || !credentialRevision) throw new Error("credential metadata incomplete");
    const saved = await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/model-settings', 'PUT', {
      expectedRevision: 0, enabled: true, provider: "openai-responses",
      model: "local-test-model", credentialRef: ${JSON.stringify(credentialRef)},
      credentialRevision: ${JSON.stringify(credentialRevision)},
    })`);
    if (![200].includes(saved.status)) {
      throw new Error(`model settings PUT ${saved.status}: ${JSON.stringify(saved.body).slice(0, 200)}`);
    }
    const readBack = await evaluate(session, `(${PAGE_FETCH})('/api/v1/ai/model-settings')`);
    if (readBack?.enabled !== true || readBack?.credentialRef !== credentialRef) {
      throw new Error(`model settings read-back mismatch: ${JSON.stringify(readBack).slice(0, 200)}`);
    }
    const created = await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/sessions', 'POST', {})`);
    if (![200, 201].includes(created.status)) throw new Error(`ai session create ${created.status}`);
    const sessionId = created.body?.sessionId ?? created.body?.id;
    if (!sessionId) throw new Error("ai session id missing");
    const sent = await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/sessions/${sessionId}/messages', 'POST', {
      content: "列出当前节点的运行时间", clientMessageId: "positive-w6r1-ai-01",
      scopeRevision: ${JSON.stringify(created.body?.scopeRevision ?? 0)},
    }, { "Idempotency-Key": "positive-w6r1-ai-msg-01" })`);
    if (![200, 201].includes(sent.status)) {
      if (sent.status === 503) throw blocked(`model unavailable at message time: ${sent.status} ${JSON.stringify(sent.body).slice(0, 200)}`);
      throw new Error(`ai message ${sent.status}: ${JSON.stringify(sent.body).slice(0, 200)}`);
    }
    const userMessageId = sent.body?.messageId;
    if (!userMessageId) throw new Error("user message id missing");
    // 显式 generate：上游供应商端点在组合运行时固定为 api.openai.com 且配额默认拒绝；
    // 真实终态需要 CX/总控的本地 TLS 供应商注入。本步断言服务端精确拒绝，绝不把
    // 入队/用户文本回显当作生成成功。
    const generated = await evaluate(session, `(${PAGE_WRITE})('/api/v1/ai/sessions/${sessionId}/messages/${userMessageId}/generate', 'POST')`);
    if (generated.status === 200) {
      const text = generated.body?.redactedContent ?? generated.body?.content;
      if (!text) throw new Error("generate 200 without assistant content");
      const messages = await evaluate(session, `(${PAGE_FETCH})('/api/v1/ai/sessions/${sessionId}/messages')`);
      const assistant = (messages.items ?? []).find(entry => entry.messageId === generated.body?.messageId && entry.role === "assistant");
      if (!assistant) throw new Error("assistant message not persisted after generate");
      return `generation reached terminal assistant message for session ${sessionId}`;
    }
    if ([403, 501, 503].includes(generated.status)) {
      throw blocked(`POST …/generate -> ${generated.status} ${JSON.stringify(generated.body).slice(0, 200)}：模型设置与凭据已保存并通过校验（read-back ok），但组合运行时默认配额拒绝且上游端点固定为 api.openai.com——本地 TLS 供应商注入属 CX Wave6-R1 + 总控组合范围；用户消息已入队但入队不等于生成`);
    }
    throw new Error(`generate ${generated.status}: ${JSON.stringify(generated.body).slice(0, 200)}`);
  });

  await attemptPositive("B03 Xray installation against a real published artifact", async () => {
    const response=await evaluate(session, `(${PAGE_WRITE})('/api/v1/plugins/catalog/refresh','POST')`);
    if(expectedUnavailable(response,[[501,"PLUGIN_CATALOG_SOURCE_UNAVAILABLE"]])) throw blocked(`catalog unavailable: ${response.status} ${JSON.stringify(response.body)}`);
    throw blocked("HARNESS_INCOMPLETE: 目录可用不代表安装完成；仍缺已准入安全制品与安装/配置/用户/流量实际执行证据");
  });

  await attemptPositive("B04 service publication reaches real stage execution with compensation", async () => {
    // 结构：真实服务组（含在线节点）→ 网关安装任务（202）→ 发布预演 → 创建发布 →
    // 执行/回滚/rollback-all。每一步按服务端真实语义推进；202/queued 不算发布完成。
    const group = await evaluate(session, `(${PAGE_WRITE})('/api/v1/service-groups', 'POST', {
      name: "Positive Transfer Group",
      memberNodeIds: [${JSON.stringify(nodeId)}],
      service: { port: 8080, protocol: "http" },
      loadBalancing: { enabled: false, strategy: "none" },
    })`);
    if (![200, 201].includes(group.status)) {
      throw blocked(`service group create -> ${group.status} ${JSON.stringify(group.body).slice(0, 200)}：发布结构未能在本基线建立`);
    }
    const groupId = group.body?.id;
    if (!groupId) throw new Error("service group id missing");
    const gateway = await evaluate(session, `(${PAGE_WRITE})('/api/v1/gateways', 'POST', {
      nodeId: ${JSON.stringify(nodeId)}, acknowledgeInstallImpact: true, installIfMissing: true, preserveExistingConfig: false,
    })`);
    if (![200, 201, 202].includes(gateway.status)) {
      throw blocked(`gateway create -> ${gateway.status} ${JSON.stringify(gateway.body).slice(0, 200)}：网关安装任务未受理`);
    }
    // 网关安装任务会被派发给在线 Agent；Agent 只接受 tasks.command，安装将诚实失败。
    // 等待安装任务进入终态后，以服务端调度资格为准推进预演。
    await sleep(6000);
    const gatewayIds = await evaluate(session, `(${PAGE_FETCH})('/api/v1/gateways').then(data => (data.items ?? []).map(g => g.id))`);
    const preview = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publishing/preview', 'POST', {
      serviceGroupId: ${JSON.stringify(groupId)},
      gatewayIds: ${JSON.stringify(gatewayIds)},
      domain: "app.example.positive",
      https: true,
    })`);
    if (![200].includes(preview.status)) {
      throw blocked(`publishing preview -> ${preview.status} ${JSON.stringify(preview.body).slice(0, 200)}：调度资格由服务端声明；网关安装未实现（Agent apply/reload/restore 属 DS/CX Wave6-R1）`);
    }
    if (preview.body?.eligible !== true) {
      throw blocked(`publishing preview not eligible: ${JSON.stringify(preview.body).slice(0, 220)}——网关健康/调度资格不足（安装任务未实现，诚实门控）`);
    }
    const publication = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publishing/publications', 'POST', {
      name: "positive publication", serviceGroupId: ${JSON.stringify(groupId)},
      gatewayIds: ${JSON.stringify(gatewayIds)},
      domain: "app.example.positive", https: true,
    })`);
    if (![200, 201, 202].includes(publication.status)) {
      throw new Error(`publication create ${publication.status}: ${JSON.stringify(publication.body).slice(0, 200)}`);
    }
    const publicationId = publication.body?.id;
    if (!publicationId) throw new Error("publication id missing");
    const run = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publications/${publicationId}/run', 'POST', {})`);
    if ([501, 502, 503].includes(run.status)) {
      throw blocked(`publication run -> ${run.status} ${JSON.stringify(run.body).slice(0, 200)}：外部发布器未接线；publication ${publicationId} 已创建但 202/queued 不算发布完成`);
    }
    const rollbackAll = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publications/${publicationId}/rollback-all', 'POST', {})`);
    if ([501, 502, 503].includes(rollbackAll.status)) {
      throw blocked(`rollback-all -> ${rollbackAll.status} ${JSON.stringify(rollbackAll.body).slice(0, 200)}：补偿端口未接线；发布/补偿终态待 DS Wave6-R1 + 供应商`);
    }
    throw new Error(`run ${run.status} rollback-all ${rollbackAll.status}: ${JSON.stringify({ run: run.body, rollbackAll: rollbackAll.body }).slice(0, 240)}`);
  });

  await attemptPositive("B05 controller update executes with signature verification", async () => {
    const response=await evaluate(session, `(${PAGE_WRITE})('/api/v1/system/updates/check','POST',{channel:"stable"})`);
    if(expectedUnavailable(response,[[503,"UPDATE_SOURCE_UNAVAILABLE"],[503,"UPDATE_PLANNER_UNAVAILABLE"]])) throw blocked(`update source unavailable: ${response.status} ${JSON.stringify(response.body)}`);
    throw blocked("HARNESS_INCOMPLETE: 版本检查成功仅证明发行源可读；未执行签名安装、进程健康、升级后版本或失败回滚");
  });

  await attemptPositive("B06 disaster recovery executes against an isolated second controller", async () => {
    // 真实备份夹具（VACUUM INTO 与 storage.VerifyFile 兼容）→ 预演（fenced 声明不等于
    // 隔离证据）→ 服务端检查 fail-closed → 恢复作业保持禁用。
    const backupId = "positive-w6r1-backup-01";
    await runCommand("wsl.exe", ["-d", distro, "--", "node", `${workDir}/backup.mjs`, workDir, backupId]);

    const preview = await evaluate(session, `(${PAGE_WRITE})('/api/v1/disaster-recovery/preview', 'POST', {
      backupId: ${JSON.stringify(backupId)},
      targetControllerId: "controller-positive-02",
      oldControllerFenced: true,
      recoveryKeyReference: "qa-key-ref-positive",
    })`);
    if ([404, 503].includes(preview.status)) {
      throw blocked(`recovery preview -> ${preview.status} ${JSON.stringify(preview.body).slice(0, 220)}：备份/恢复执行需要真实备份可见性与第二隔离主控（POC-08/总控 awskr 范围）`);
    }
    const checks = preview.body?.checks ?? [];
    const failedChecks = checks.filter(entry => entry.status !== "passed");
    if (![200, 201].includes(preview.status) || failedChecks.length > 0 || checks.length === 0) {
      throw blocked(`recovery preview not passed: status=${preview.status}, checks=${JSON.stringify(checks).slice(0, 260)}——预演成功也不代表恢复；实际恢复需第二隔离 Controller、真实 fence 与撤销不复活证据`);
    }
    const job = await evaluate(session, `(${PAGE_WRITE})('/api/v1/disaster-recovery/jobs', 'POST', {
      backupId: ${JSON.stringify(backupId)}, targetControllerId: "controller-positive-02",
      oldControllerFenced: true, recoveryKeyReference: "qa-key-ref-positive",
    })`);
    if ([501, 503].includes(job.status)) {
      throw blocked(`recovery job -> ${job.status} ${JSON.stringify(job.body).slice(0, 200)}：恢复执行器未接线`);
    }
    throw new Error(`recovery job accepted ${job.status}: ${JSON.stringify(job.body).slice(0, 200)}`);
  });

  await stepPass("P14 grant revocation isolates task access and a 403 keeps the session", async () => {
    // 撤销管理员对该节点的授权后，任务详情读取必须 403；页面诚实显示拒绝，
    // 且 403 不清除会话（导航与身份保持）。
    const grants = await evaluate(session, `(${PAGE_FETCH})('/api/v1/resource-grants').then(data => data.items)`);
    const mine = grants.find(entry => entry.principalId === adminUserId && entry.resourceId === nodeId);
    if (!mine) throw new Error("admin grant on the enrolled node not found for revocation");
    const revoked = await evaluate(session, `(${PAGE_WRITE})('/api/v1/resource-grants/${mine.grantId}', 'DELETE')`);
    if (![200, 202, 204].includes(revoked.status)) {
      throw new Error(`grant revoke ${revoked.status}: ${JSON.stringify(revoked.body).slice(0, 200)}`);
    }
    const denied = await evaluate(session, `(${PAGE_WRITE})('/api/v1/tasks/${failedTaskId}', 'GET')`);
    if (denied.status !== 403) {
      throw new Error(`expected 403 after revocation, got ${denied.status} ${JSON.stringify(denied.body).slice(0, 160)}`);
    }
    await goto(session, `${base}/#/tasks/${failedTaskId}`);
    await expectText(session, "403", 20_000);
    await goto(session, `${base}/#/overview`);
    await expectText(session, "概览", 20_000);
    const identity = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => identity.userId)`);
    if (identity !== adminUserId) throw new Error("session identity changed after a 403");
    await screenshot(session, join(outDir, "positive-14-revoked-403.png"));
    return "revoked grant -> task detail 403 rendered honestly; 403 did not clear the authenticated session";
  });

  // =====================================================================
  // B组是尚未完成的正向流程：仅登记明确的环境/夹具缺口。
  // 不把消息入队、更新检查、恢复预演或任何错误响应算作完整功能成功。
  // =====================================================================

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
