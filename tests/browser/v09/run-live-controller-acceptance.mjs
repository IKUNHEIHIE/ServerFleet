/**
 * GM-W3-02 / GM-W4-01 · 真实 Go Controller + SQLite WAL 端到端浏览器验收（V09-5D-GM）。
 *
 * 与 `run-acceptance.mjs`（候选契约桩）的分工：本脚本启动**真实后端**：
 *   1. `go build ./apps/controller` 并以 `-db <临时 SQLite WAL> -listen 127.0.0.1:<apiPort>
 *      -origin https://127.0.0.1:<webPort> -tls-cert/-tls-key` 运行（后端强制直连 HTTPS；
 *      `internal/auth.BrowserGuard` 要求 Origin 精确为 https 且 Host 一致，因此不接受纯 http Origin）。
 *   2. 本脚本在同一 https://127.0.0.1:<webPort> 上托管 `apps/web/dist` 生产构建，并把
 *      `/api/*`、`/healthz` 原样转发（保留 Origin/Cookie/CSRF/Sec-Fetch 头，SSE 流式透传），
 *      使页面与 API 同源，满足 `__Host-sf_session`（Secure; SameSite=Strict）的浏览器约束。
 *   3. 本地安装器步骤：后端刻意不通过 HTTP/日志/参数暴露 setup token（`PrepareSetup` 注释），
 *      脚本作为“本机安装器”直接以 node:sqlite 向 `cx_auth_identity` 写入 SetupDigest/SetupExpires
 *      （与 internal/auth credentialDigest 相同的 sha256+base64url），并播种两个节点行作为测试夹具
 *      （与 Wave 2 集成测试相同的夹具方式）；之后的 Bootstrap HTTP 流程、登录、授权、审批、
 *      DNS/网关/通知/审计全部走真实 Controller。GM-W4-01 起备份夹具同样以 node:sqlite 的
 *      `VACUUM INTO` 生成真实 SQLite 备份文件，校验/预演仍走真实 HTTP。
 *   4. Headless Edge（回退 Chrome）跑完全链路并输出截图证据：
 *      步骤 01–30 覆盖 Wave 1–3 全链路（bootstrap、授权、双人审批、SSE、DNS/网关/通知/审计），
 *      步骤 31–56 覆盖 Wave 3 新增接线模块（节点详情/流量配额、监控与流量风险、证书、
 *      服务发布向导、AI 会话与一次性审批、插件中心与 Xray 门控、更新中心与备份恢复、
 *      新域审计），步骤 57–67 覆盖 Wave 5 新增接线模块（工作流管理与手动触发、
 *      监控检查历史与立即探测、文件字节流上传与 SHA-256 校验、发布执行与回滚、
 *      Wave 5 审计闭环）。组合运行时未注入的适配器（更新源、插件目录源、nginx Checker、
 *      恢复执行器、监控探测器、文件传输通道、外部发布器）按服务端语义诚实失败，
 *      脚本断言诚实错误而不是伪造成功。
 *
 * 用法（在 `scripts/env.ps1` 环境中）：
 *   node tests/browser/v09/run-live-controller-acceptance.mjs [--out <目录>] [--skip-build] [--keep]
 */
import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import https from "node:https";
import { tmpdir } from "node:os";
import { dirname, extname, join, normalize, resolve, sep } from "node:path";
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
const outDir = resolve(argValue("out", join(repoRoot, "docs", "development", "sprints", "v09-five-day", "progress", "gemini", "evidence", "live")));
const skipBuild = hasFlag("skip-build");
const keepTemp = hasFlag("keep");

const ADMIN_USER = "admin";
const ADMIN_PASSWORD = "live-admin-strong-password-01";
const APPROVER_USER = "approver";
const APPROVER_PASSWORD = "live-approver-strong-password-02";
const NODE_A = "node-alpha";
const NODE_B = "node-beta";
const NODE_A_LABEL = "Alpha Edge";

const results = [];
function record(step, ok, detail = "") {
  results.push({ step, ok, detail });
  console.log(`${ok ? "PASS" : "FAIL"}  ${step}${detail ? ` — ${detail}` : ""}`);
}

let debugSession = null;
let debugSeq = 0;
async function step(name, fn) {
  try {
    const detail = await fn();
    record(name, true, typeof detail === "string" ? detail : "");
    return true;
  } catch (error) {
    record(name, false, String(error.message ?? error));
    if (debugSession) {
      debugSeq += 1;
      await screenshot(debugSession, join(outDir, `debug-${String(debugSeq).padStart(2, "0")}-${name.replace(/^[\d\s]+/, "").replace(/[^a-z0-9-]+/gi, "-").slice(0, 40)}.png`)).catch(() => {});
    }
    return false;
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

/** 与当前 node 同目录的 npm-cli.js（env.ps1 内置工具链）；Windows 下 spawn .cmd 会 EINVAL，
 * 直接用 node 调 npm-cli.js。 */
function npmBuildCommand() {
  const nodeDir = dirname(process.execPath);
  const npmCli = join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js");
  return existsSync(npmCli) ? [process.execPath, [npmCli]] : null;
}

function goCommand() {
  const bundled = resolve(repoRoot, ".tools/go/bin/go.exe");
  return existsSync(bundled) ? bundled : "go";
}

async function findOpenssl() {
  const candidates = [];
  if (process.env.SF_OPENSSL) candidates.push(process.env.SF_OPENSSL);
  try {
    const { stdout } = await runCommand("where.exe", ["openssl"]);
    for (const line of stdout.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (trimmed) candidates.push(trimmed);
    }
  } catch {
    /* where unavailable; fall through to known locations */
  }
  candidates.push(
    "D:\\x86 soft\\Git\\mingw64\\bin\\openssl.exe",
    "D:\\x86 soft\\Git\\usr\\bin\\openssl.exe",
    "C:\\Program Files\\Git\\mingw64\\bin\\openssl.exe",
    "C:\\Program Files\\Git\\usr\\bin\\openssl.exe",
  );
  return candidates.find(path => existsSync(path)) ?? null;
}

function runCommand(command, args, options = {}) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], ...options });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => { stdout += chunk; });
    child.stderr.on("data", chunk => { stderr += chunk; });
    child.on("error", reject);
    child.on("exit", code => (code === 0 ? resolvePromise({ stdout, stderr }) : reject(new Error(`${command} exited ${code}: ${stderr.slice(-2000)}`))));
  });
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

async function generateCertificate(dir) {
  const cert = join(dir, "cert.pem");
  const key = join(dir, "key.pem");
  const openssl = await findOpenssl();
  if (!openssl) throw new Error("openssl not found; set SF_OPENSSL to the openssl.exe path");
  await runCommand(openssl, [
    "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", cert,
    "-days", "2", "-subj", "/CN=127.0.0.1",
    "-addext", "subjectAltName=IP:127.0.0.1,DNS:localhost",
  ]);
  const pem = await readFile(cert, "utf8");
  if (!pem.startsWith("-----BEGIN CERTIFICATE-----")) {
    throw new Error("generated certificate is not a PEM file");
  }
  return { cert, key };
}

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
};

/** 同源托管 dist 并把 /api/*、/healthz 原样转发给真实 Controller（SSE 流式透传）。 */
function startWebHost({ distDir, cert, key, apiPort }) {
  // https.createServer 接收 PEM 内容（不是路径）。
  const tlsOptions = { cert: readFileSync(cert), key: readFileSync(key) };
  const server = https.createServer(tlsOptions, (req, res) => {
    const url = new URL(req.url ?? "/", `https://${req.headers.host ?? "127.0.0.1"}`);
    if (url.pathname === "/healthz" || url.pathname === "/api" || url.pathname.startsWith("/api/")) {
      const headers = { ...req.headers };
      const upstream = https.request(
        { host: "127.0.0.1", port: apiPort, path: req.url, method: req.method, headers, rejectUnauthorized: false },
        upstreamResponse => {
          res.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
          upstreamResponse.pipe(res);
        },
      );
      upstream.on("error", error => {
        if (!res.headersSent) {
          res.writeHead(502, { "Content-Type": "application/json", "Cache-Control": "no-store" });
        }
        res.end(JSON.stringify({ code: "PROXY_UNREACHABLE", message: String(error) }));
      });
      req.pipe(upstream);
      return;
    }
    void serveStatic(req, res, url.pathname, distDir);
  });
  return new Promise(resolveServer => {
    server.listen(0, "127.0.0.1", () => resolveServer({ server, port: server.address().port }));
  });
}

async function serveStatic(req, res, pathname, distDir) {
  const relative = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const target = resolve(join(distDir, normalize(relative).replace(/^(\.\.[/\\])+/, "")));
  if (!target.startsWith(distDir + sep)) {
    res.writeHead(403).end("forbidden");
    return;
  }
  let file = target;
  try {
    const info = await stat(file);
    if (info.isDirectory()) file = join(file, "index.html");
  } catch {
    file = join(distDir, "index.html"); // hash 路由的 SPA 回退
  }
  try {
    const body = await readFile(file);
    res.writeHead(200, {
      "Content-Type": MIME[extname(file)] ?? "application/octet-stream",
      "Content-Length": body.length,
      "Cache-Control": "no-store",
    });
    res.end(req.method === "HEAD" ? undefined : body);
  } catch {
    res.writeHead(404).end("not found");
  }
}

function waitForHttps(url, label, timeoutMs = 40_000) {
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
      setTimeout(attempt, 400);
    }
    attempt();
  });
}

/**
 * 本机安装器：写入 setup token 摘要并播种节点夹具。
 * 摘要算法与 `internal/auth.credentialDigest` 一致（sha256 + base64url）；
 * 控制器随后在真实 bootstrap HTTP 流程中做常数时间校验与过期检查。
 */
async function installSetupTokenAndNodes(dbPath) {
  const { DatabaseSync } = await import("node:sqlite");
  const setupToken = randomBytes(32).toString("base64url");
  const digest = createHash("sha256").update(setupToken).digest("base64url");
  const expires = new Date(Date.now() + 15 * 60_000).toISOString();
  const db = new DatabaseSync(dbPath);
  try {
    db.exec("PRAGMA busy_timeout = 5000");
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
    const updated = db.prepare(
      "UPDATE cx_auth_identity SET payload = ? WHERE singleton = 1 AND ever_initialized = 0",
    ).run(Buffer.from(JSON.stringify(payload)));
    if (updated.changes !== 1) throw new Error("setup token write raced another writer");
    const now = new Date().toISOString();
    const insert = db.prepare(`INSERT INTO nodes
      (id, name, address, region, provider, country_code, tags, status, last_seen_at, event_seq,
       metrics_cpu, metrics_memory, metrics_observed_at, metrics_source, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, 'agent', ?, ?)`);
    const seed = (id, name, region, country) => {
      insert.run(id, name, "", region, "", country, "[]", "online", now, 1, now, now);
    };
    seed(NODE_A, NODE_A_LABEL, "ap-northeast", "JP");
    seed(NODE_B, "Beta Core", "eu-central", "DE");
  } finally {
    db.close();
  }
  return setupToken;
}

/** 页面内同源请求（Cookie 自动附带）；仅用于取证读取，不在脚本里伪造任何界面状态。 */
const PAGE_FETCH = `path => fetch(path, {
  headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
}).then(response => response.json())`;

/** 页面内同源写请求（先经 /auth/me 取回 CSRF，再携带会话 Cookie）；仅驱动真实后端端点，不伪造结果。 */
const PAGE_WRITE = `async (path, method, body) => {
  const me = await fetch("/api/v1/auth/me", {
    headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
  }).then(response => response.json());
  const response = await fetch(path, {
    method,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "ServerFleetWeb",
      "X-CSRF-Token": me.csrfToken ?? "",
    },
    credentials: "include",
    cache: "no-store",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return { status: response.status, body: await response.json().catch(() => null) };
}`;

/**
 * 本机安装器职责扩展（GM-W4-01）：真实备份文件由 SQLite 在线备份 API（VACUUM INTO）生成，
 * 与 `apps/controller` 的 `storage.VerifyFile` 完整性校验兼容；恢复/校验本身全部走真实 HTTP。
 */
async function createLocalBackup(dbPath, backupID) {
  const { DatabaseSync } = await import("node:sqlite");
  const backupDir = join(dirname(dbPath), "backups");
  await mkdir(backupDir, { recursive: true });
  const target = join(backupDir, `${backupID}.db`);
  const db = new DatabaseSync(dbPath);
  try {
    db.exec(`VACUUM INTO '${target.replace(/\\/g, "/")}'`);
  } finally {
    db.close();
  }
  const info = await stat(target);
  if (info.size <= 0) throw new Error("vacuum backup produced an empty file");
  return { backupDir, target };
}

let tempDir = null;
const children = [];
const servers = [];
function track(child) {
  children.push(child);
  return child;
}

async function cleanup() {
  for (const server of servers) {
    try {
      server.close();
    } catch {
      /* ignore */
    }
  }
  for (const child of children) {
    try {
      child.kill();
    } catch {
      /* already gone */
    }
  }
  if (tempDir && !keepTemp) {
    await rm(tempDir, { recursive: true, force: true }).catch(() => {});
  }
}

async function main() {
  if (!skipBuild) {
    console.log("building production bundle (npm run build --workspace @serverfleet/web)...");
    const npm = npmBuildCommand();
    if (!npm) throw new Error("npm-cli.js not found next to the running node; build apps/web manually and pass --skip-build");
    await runCommand(npm[0], [...npm[1], "run", "build", "--workspace", "@serverfleet/web"], { cwd: repoRoot });
  } else if (!existsSync(resolve(repoRoot, "apps/web/dist/index.html"))) {
    throw new Error("--skip-build given but apps/web/dist is missing; run the build first");
  }

  tempDir = await mkdtemp(join(tmpdir(), "gm09-live-"));
  console.log(`live evidence out dir: ${outDir}`);
  console.log(`temp dir: ${tempDir}`);

  console.log("building controller (go build ./apps/controller)...");
  const controllerBin = join(tempDir, "controller.exe");
  await runCommand(goCommand(), ["build", "-o", controllerBin, "./apps/controller"], { cwd: repoRoot });

  const { cert, key } = await generateCertificate(tempDir);
  const dbPath = join(tempDir, "controller-live.db");
  const apiPort = await freePort();
  const web = await startWebHost({ distDir: resolve(repoRoot, "apps/web/dist"), cert, key, apiPort });
  servers.push(web.server);
  const webPort = web.port;
  const webOrigin = `https://127.0.0.1:${webPort}`;
  console.log(`web host: ${webOrigin}; controller api: 127.0.0.1:${apiPort}`);

  const controller = track(spawn(controllerBin, [
    "-db", dbPath,
    "-listen", `127.0.0.1:${apiPort}`,
    "-origin", webOrigin,
    "-tls-cert", cert,
    "-tls-key", key,
  ], { stdio: ["ignore", "pipe", "pipe"] }));
  controller.stdout.on("data", chunk => process.stdout.write(`[controller] ${chunk}`));
  controller.stderr.on("data", chunk => process.stderr.write(`[controller] ${chunk}`));
  controller.on("exit", (code, signal) => console.error(`[controller] exited code=${code} signal=${signal}`));
  await waitForHttps(`https://127.0.0.1:${apiPort}/healthz`, "controller");

  const setupToken = await installSetupTokenAndNodes(dbPath);
  console.log("installer: setup token prepared (never logged), nodes seeded");

  const cdpPort = 52300 + Math.floor(Math.random() * 400);
  const browser = await launchHeadlessBrowser({ cdpPort, profileDir: join(tempDir, "profile") });
  track(browser.child);
  const session = await connectPage(cdpPort);
  debugSession = session;
  const base = webOrigin;

  // ---- 1. 首次 Bootstrap：初始化管理员 -> 自动登录进入 /overview
  await goto(session, `${base}/#/setup`);
  await step("01 bootstrap form shown for uninitialized controller", () => expectText(session, "本机 setup token"));
  await fillField(session, "本机 setup token", setupToken);
  await fillField(session, "管理员用户名", ADMIN_USER);
  await fillField(session, "密码", ADMIN_PASSWORD);
  await fillField(session, "确认密码", ADMIN_PASSWORD);
  await screenshot(session, join(outDir, "live-01-bootstrap.png"));
  await clickText(session, "创建管理员并进入", { exact: true });
  await step("02 bootstrap auto-login lands on overview", () => waitForUrl(session, "#/overview"));
  await step("03 overview backed by real controller (no scaffold disclaimer)", async () => {
    await expectText(session, "概览");
    const scaffold = await evaluate(session, "document.body.innerText.includes('Controller 仍为工程骨架')");
    if (scaffold) throw new Error("scaffold disclaimer rendered against a real controller");
    return "POST /api/v1/auth/bootstrap (204) + login chain completed";
  });
  await screenshot(session, join(outDir, "live-02-overview.png"));

  // ---- 2. 刷新后 GET /auth/me 恢复 CSRF（真实后端在受保护会话恢复时返回 csrfToken）
  await goto(session, `${base}/#/overview`);
  await step("04 reload restores CSRF token (writes not locked)", async () => {
    await expectText(session, "概览");
    await waitForCondition(session, "!document.body.innerText.includes('安全写入已锁定')", {
      timeoutMs: 10_000, label: "no write-lock banner",
    });
    return "GET /api/v1/auth/me returned csrfToken";
  });

  // ---- 3. 零资源授权：管理员在未授权前看不到任何节点
  await goto(session, `${base}/#/nodes`);
  await step("05 zero-scope: no node visible before any grant", async () => {
    await expectText(session, "节点");
    await sleep(800);
    const text = await evaluate(session, "document.body.innerText");
    if (text.includes(NODE_A_LABEL) || text.includes("Beta Core")) throw new Error("nodes visible without explicit grants");
    return "GET /api/v1/nodes returns an empty list under default zero authorization";
  });
  await screenshot(session, join(outDir, "live-03-nodes-zero-scope.png"));

  // ---- 4. 创建第二操作员（operator 角色）
  const adminUserId = await evaluate(session, `(${PAGE_FETCH})('/api/v1/auth/me').then(identity => identity.userId)`);
  if (typeof adminUserId !== "string" || adminUserId.length === 0) throw new Error("could not read admin userId from /auth/me");
  await goto(session, `${base}/#/users`);
  await step("06 users page lists the bootstrap admin", () => expectText(session, "admin"));
  await clickText(session, "创建用户", { exact: true });
  await fillField(session, "用户名", APPROVER_USER);
  await fillField(session, "显示名称（可选）", "Approver Two");
  await fillField(session, "初始密码", APPROVER_PASSWORD);
  await setCheckbox(session, "Operator", true);
  await screenshot(session, join(outDir, "live-04-create-approver.png"));
  await clickText(session, "保存用户", { exact: true });
  await step("07 second operator created via real controller", () => expectText(session, APPROVER_USER));

  // ---- 4b. 审批人授权：tasks.approve/tasks.read（异人审批与任务可见的最小授权）
  const approverUserId = await evaluate(session, `(${PAGE_FETCH})('/api/v1/users').then(data => {
    const found = (data.items ?? []).find(user => user.username === ${JSON.stringify(APPROVER_USER)});
    return found ? found.userId : null;
  })`);
  if (typeof approverUserId !== "string" || approverUserId.length === 0) throw new Error("could not read approver userId from /users");
  await goto(session, `${base}/#/users`);
  await step("08 switch to resource grants tab", async () => {
    await clickText(session, "资源授权", { exact: true });
    await expectText(session, "创建授权");
  });
  async function createGrant(principalId, permissions) {
    await clickText(session, "创建授权", { exact: true });
    await fillField(session, "主体 ID", principalId);
    await fillField(session, "资源 ID", NODE_A);
    for (const permission of permissions) {
      await setCheckbox(session, permission, true);
    }
    // 静置后复核每个勾选是否真正进入 React 受控状态；未进入的重新勾选。
    await sleep(600);
    for (const permission of permissions) {
      for (let attempt = 0; attempt < 3; attempt += 1) {
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
    const grant = grants.find(entry => entry.principalId === principalId && entry.resourceId === NODE_A);
    if (!grant) throw new Error("grant not visible after creation");
    const missing = permissions.filter(permission => !grant.permissions.includes(permission));
    if (missing.length > 0) throw new Error(`grant missing permissions: ${missing.join(", ")}`);
    return `granted ${grant.permissions.length} permissions on ${NODE_A}`;
  }
  await step("09 admin granted explicit node permissions", () => createGrant(adminUserId, [
    "nodes.read", "nodes.write", "groups.write", "servicegroups.read", "servicegroups.write",
    "tasks.submit", "tasks.read", "tasks.approve", "host.root",
    "gateway.read", "gateway.write", "gateway.provision",
    // GM-W4-01：Wave 3 新增域的权限键（monitoring/quota/certificates/publishing 走资源授权，
    // tasks.command/host.patch 同时作为 AI 会话授权动作的权威交集来源）。
    "monitoring.read", "monitoring.write", "quota.read", "quota.write",
    "certificates.read", "publishing.read", "publishing.write",
    "tasks.command", "host.patch",
    // GM-W5-02：Wave 5 工作流管理与文件字节流上传所需的授权键。
    "files.write", "workflows.read", "workflows.write",
  ]));
  await step("09b approver granted approve/read on the node", () => createGrant(approverUserId, [
    "tasks.approve", "tasks.read",
  ]));
  await screenshot(session, join(outDir, "live-05-grants.png"));

  // ---- 6. 授权后 /nodes 可见性与隔离
  await goto(session, `${base}/#/nodes`);
  await step("10 granted node visible, ungranted node isolated", async () => {
    await expectText(session, NODE_A_LABEL, 30_000);
    const text = await evaluate(session, "document.body.innerText");
    if (text.includes("Beta Core")) throw new Error("ungranted node-beta leaked into the list");
    return "only node-alpha visible after grant";
  });
  await screenshot(session, join(outDir, "live-06-nodes-granted.png"));

  // ---- 7. 普通分组：创建 + expectedRevision 成员更新
  await goto(session, `${base}/#/groups`);
  await step("11 create manual group", async () => {
    await clickText(session, "新建自定义分组", { exact: true });
    await fillField(session, "分组名称", "APAC Edge");
    await fillField(session, "备注（可选）", "Tokyo nodes");
    await clickText(session, "创建分组", { exact: true });
    await expectText(session, "APAC Edge");
    return "POST /api/v1/groups -> 201";
  });
  await step("12 group members saved with expectedRevision", async () => {
    await clickText(session, "管理成员", { exact: true });
    await setCheckbox(session, NODE_A_LABEL, true);
    await expectText(session, "保存成员（1）");
    await clickText(session, "保存成员（1）", { exact: true });
    await waitForCondition(session, "!document.body.innerText.includes('管理成员 · APAC Edge')", {
      timeoutMs: 15_000, label: "member modal closed",
    });
    const groups = await evaluate(session, `(${PAGE_FETCH})('/api/v1/groups').then(data => data.items)`);
    const group = groups.find(entry => entry.name === "APAC Edge");
    if (!group || !Array.isArray(group.memberNodeIds) || !group.memberNodeIds.includes(NODE_A)) {
      throw new Error(`group members not persisted: ${JSON.stringify(group)}`);
    }
    return "PUT /api/v1/groups/{id}/members accepted with expectedRevision";
  });
  await screenshot(session, join(outDir, "live-07-group-members.png"));

  // ---- 8. 服务组：创建（带成员节点）+ 成员管理
  await goto(session, `${base}/#/service-groups`);
  await step("13 create service group with member node", async () => {
    await clickText(session, "新建服务组", { exact: true });
    await fillField(session, "服务组名称", "Edge Web");
    await fillField(session, "服务端口", "8080");
    // 真实 Controller 按成员节点集合授权；创建时必须显式选择成员。
    await setCheckbox(session, NODE_A_LABEL, true);
    await clickText(session, "创建服务组", { exact: true });
    await expectText(session, "Edge Web");
    return "POST /api/v1/service-groups -> 201 (member-scoped authorization)";
  });
  await step("14 service group member update with expectedRevision", async () => {
    // 创建成功后前端已自动进入服务组详情页；直接打开成员编辑并以 expectedRevision 保存。
    await expectText(session, "管理成员", 15_000);
    await clickText(session, "管理成员", { exact: true });
    await setCheckbox(session, NODE_A_LABEL, true);
    await expectText(session, "保存成员（1）");
    await clickText(session, "保存成员（1）", { exact: true });
    await sleep(800);
    const groups = await evaluate(session, `(${PAGE_FETCH})('/api/v1/service-groups').then(data => data.items)`);
    const group = groups.find(entry => entry.name === "Edge Web");
    if (!group || !Array.isArray(group.memberNodeIds) || !group.memberNodeIds.includes(NODE_A)) {
      throw new Error(`service group members not persisted: ${JSON.stringify(group)}`);
    }
    if (group.revision === 1) throw new Error(`expectedRevision did not advance: ${group.revision}`);
    return `member persisted, revision advanced to ${group.revision}`;
  });
  await screenshot(session, join(outDir, "live-08-service-group.png"));

  // ---- 9. 任务：预演 -> 提交 root 任务 -> 自批 409
  await goto(session, `${base}/#/tasks`);
  await step("15 preview requires approval for root task", async () => {
    // 先以同源 fetch 精确复现服务端响应（500 时留证据），再走 UI 表单。
    const probe = await evaluate(session, `(async () => {
      const me = await fetch("/api/v1/auth/me", {
        headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
      }).then(response => response.json());
      const response = await fetch("/api/v1/tasks/preview", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Requested-With": "ServerFleetWeb",
          "X-CSRF-Token": me.csrfToken,
        },
        credentials: "include",
        body: JSON.stringify({
          name: "patch alpha",
          target: { type: "node_ids", nodeIds: ["node-alpha"] },
          command: "host.root", dryRun: true, dispatchMode: "sequential",
        }),
      });
      return { status: response.status, body: (await response.text()).slice(0, 300) };
    })()`);
    if (probe.status !== 200) throw new Error(`direct preview ${probe.status}: ${probe.body}`);
    if (!JSON.parse(probe.body).requiresApproval) throw new Error("server did not require approval for host.root");
    await clickText(session, "创建任务", { exact: true });
    await fillField(session, "任务名称", "patch alpha");
    // 真实 Controller 要求 command 是单一动作 ID；自由文本会被后端判非法。
    await fillField(session, "命令或脚本", "host.root");
    await setCheckbox(session, NODE_A_LABEL, true);
    await clickText(session, "预演目标", { exact: true });
    await expectText(session, "需要审批：是", 20_000);
    return "direct preview 200 requiresApproval=true";
  });
  await step("16 submit root task (dryRun disabled)", async () => {
    await setCheckbox(session, "预演（dryRun）", false);
    await clickText(session, "提交任务", { exact: true });
    await waitForUrl(session, "#/tasks/");
    return "POST /api/v1/tasks -> 202 awaiting_approval";
  });
  await step("17 task detail shows awaiting approval with server planDigest", async () => {
    await expectText(session, "等待审批", 30_000);
    await expectText(session, "planDigest");
  });
  await screenshot(session, join(outDir, "live-09-task-awaiting.png"));
  await step("18 self-approval rejected with 409 conflict", async () => {
    await clickText(session, "批准", { exact: true });
    await fillField(session, "原因（可选）", "self approve attempt");
    await clickText(session, "确认", { exact: true });
    await expectText(session, "资源已被其他操作修改", 20_000);
    return "server rejected the submitter's own approval (ErrSelfApproval -> 409)";
  });
  await screenshot(session, join(outDir, "live-10-self-approve-409.png"));
  await clickText(session, "取消", { exact: true });

  // ---- 10. 异人审批 + SSE
  await clickText(session, "退出", { exact: true });
  await step("19 approver logs in", async () => {
    await expectText(session, "登录", 15_000);
    await fillField(session, "用户名", APPROVER_USER);
    await fillField(session, "密码", APPROVER_PASSWORD);
    await clickText(session, "登录");
    // 登录后按 next 重定向回原页面（不一定是 /overview）；以服务端身份为准确认切换成功。
    await expectText(session, "Approver Two", 20_000);
  });
  await goto(session, `${base}/#/tasks`);
  await step("20 approver opens the submitted task", async () => {
    // 真实 tasks 列表 DTO 不含任务名；以服务端列表取 taskId 后直接进入详情。
    const items = await evaluate(session, `(${PAGE_FETCH})('/api/v1/tasks').then(data => data.items)`);
    const task = items.find(entry => entry.createdBy && entry.state === "awaiting_approval") ?? items[0];
    if (!task) throw new Error("approver sees no tasks through the real list API");
    await goto(session, `${base}/#/tasks/${encodeURIComponent(task.taskId)}`);
    await expectText(session, "任务快照", 20_000);
    return `opened task ${task.taskId} (${task.state})`;
  });
  await step("21 peer approval accepted (different operator)", async () => {
    await clickText(session, "批准", { exact: true });
    await fillField(session, "原因（可选）", "peer reviewed by second operator");
    await clickText(session, "确认", { exact: true });
    await expectText(session, "审批已提交", 20_000);
    return "POST /api/v1/tasks/{id}/approve -> 200 by a different subject";
  });
  await step("22 SSE live events received from real controller", async () => {
    await waitForCondition(session, `(() => {
      const text = document.body.innerText;
      const cursor = /游标：[1-9]/.test(text);
      const events = text.includes("approval_required") || text.includes("approval_granted") || text.includes("created");
      return cursor && events;
    })()`, { timeoutMs: 30_000, label: "SSE cursor and approval events" });
    return "GET /api/v1/tasks/{id}/events streamed server events";
  });
  await step("23 approved task leaves awaiting_approval", async () => {
    await waitForCondition(session, "!document.body.innerText.includes('等待审批')", {
      timeoutMs: 20_000, label: "task left awaiting_approval",
    });
    return "task state advanced per server snapshot";
  });
  await screenshot(session, join(outDir, "live-11-peer-approved-sse.png"));

  // ---- 11. DNS / 网关 / 通知 / 审计（真实读写）
  await step("24 switch back to admin", async () => {
    await clickText(session, "退出", { exact: true });
    await expectText(session, "登录", 15_000);
    await fillField(session, "用户名", ADMIN_USER);
    await fillField(session, "密码", ADMIN_PASSWORD);
    await clickText(session, "登录");
    // 页头身份行显示 "admin · administrator"。
    await expectText(session, "administrator", 20_000);
  });

  await goto(session, `${base}/#/dns`);
  await step("25 dns providers rendered from real controller", () => expectText(session, "Cloudflare（cloudflare）"));
  await step("26 dns account created with credential reference", async () => {
    await clickText(session, "添加账号", { exact: true });
    await fillField(session, "显示名称", "Live CF");
    await fillField(session, "Cloudflare API Token", "live-credential-ref-0001");
    await clickText(session, "保存账号", { exact: true });
    await expectText(session, "Live CF", 20_000);
    const leaked = await evaluate(session, "document.body.innerText.includes('live-credential-ref-0001')");
    if (leaked) throw new Error("credential reference echoed back into the page");
    return "POST /api/v1/dns/accounts (credentialRef, no plaintext token)";
  });
  await screenshot(session, join(outDir, "live-12-dns.png"));

  await goto(session, `${base}/#/gateways`);
  await step("27 gateway installation task accepted", async () => {
    await clickText(session, "新建网关", { exact: true });
    await fillField(session, "节点", NODE_A);
    await setCheckbox(session, "我确认安装 Nginx", true);
    await clickText(session, "创建安装任务", { exact: true });
    await expectText(session, NODE_A_LABEL, 20_000);
    return "POST /api/v1/gateways -> 202 provision job";
  });
  await step("28 gateway route create honors server dependency state", async () => {
    const gateways = await evaluate(session, `(${PAGE_FETCH})('/api/v1/gateways').then(data => data.items)`);
    if (!gateways[0]?.id) throw new Error("no gateway visible for route binding");
    await clickText(session, "服务路由", { exact: true });
    await clickText(session, "新建路由", { exact: true });
    // 真实 Controller 的路由创建按网关归属校验，必须显式选择网关。
    await fillField(session, "所属网关", gateways[0].id);
    await fillField(session, "访问域名", "app.example.live");
    await fillField(session, "上游地址", "node-alpha.internal:8080");
    await clickText(session, "保存路由", { exact: true });
    // 组合运行时未提供 nginx Checker（main.go 不注入 GatewayChecker）：真实后端会诚实返回
    // 503 gateway_unavailable。验收接受两种服务端结果：路由创建成功，或依赖缺口的诚实提示；
    // 绝不接受伪造的路由数据。
    const outcome = await waitForCondition(session, `(() => {
      const text = document.body.innerText;
      if (text.includes("app.example.live")) return "created";
      if (text.includes("Controller 暂时不可用")) return "unavailable-503";
      return null;
    })()`, { timeoutMs: 20_000, label: "route save outcome" });
    if (outcome === "unavailable-503") {
      return "POST /api/v1/gateway-routes -> 503 gateway_unavailable (Checker not wired in composed runtime; honest error surfaced)";
    }
    return "POST /api/v1/gateway-routes -> 201";
  });
  await screenshot(session, join(outDir, "live-13-gateways.png"));

  await goto(session, `${base}/#/monitoring`);
  await step("29 notification channels tab shows honest empty state", async () => {
    // 真实 Controller 在未配置渠道时返回空列表；界面必须如实显示空态，不得伪造渠道。
    await clickText(session, "通知与投递", { exact: true });
    await expectText(session, "未返回通知渠道", 25_000);
    return "GET /api/v1/notifications/channels -> empty (unconfigured controller)";
  });
  await step("29b notification settings saved through real controller", async () => {
    // 渠道从零创建的 UI 缺口登记在 SHARED-CHANGES；这里用通知策略的真实 PUT 验证写路径。
    await goto(session, `${base}/#/settings`);
    await clickText(session, "通知渠道", { exact: true });
    // fillField 自带轮询重试；找不到字段会以明确错误失败，不做一次性探测。
    await fillField(session, "最大重试次数（0–5）", "3");
    await clickText(session, "保存策略", { exact: true });
    await expectText(session, "通知策略已保存", 20_000);
    return "PUT /api/v1/notifications/settings accepted";
  });
  await screenshot(session, join(outDir, "live-14-monitoring.png"));

  await goto(session, `${base}/#/audit`);
  await step("30 audit trail lists real controller events", async () => {
    await expectText(session, "auth.login", 25_000);
    const text = await evaluate(session, "document.body.innerText");
    const expected = ["auth.bootstrap", "auth.user.create", "auth.grant.create", "tasks.submit"];
    const missing = expected.filter(action => !text.includes(action));
    if (missing.length > 0) throw new Error(`audit missing actions: ${missing.join(", ")}`);
    return "GET /api/v1/audit/events returns authoritative records";
  });
  await screenshot(session, join(outDir, "live-15-audit.png"), { width: 1280, height: 1400 });

  // =====================================================================
  // GM-W4-01 · Wave 3 新增接线模块（monitoring/quota/certificates/publishing/
  // ai/plugins/updates/recovery）真后端浏览器实测。组合运行时未注入的适配器
  // （更新源、插件目录源、nginx Checker、恢复执行器）按服务端语义诚实失败，
  // 验收断言诚实错误，绝不伪造成功。
  // =====================================================================

  // ---- 12. 节点详情与流量配额（#/nodes/node-alpha）
  await goto(session, `${base}/#/nodes/${NODE_A}`);
  await step("31 node detail renders identity, capabilities and diagnostics", async () => {
    await expectText(session, "节点元数据", 20_000);
    await expectText(session, "节点身份", 15_000);
    await expectText(session, "协商能力", 10_000);
    await expectText(session, "连接诊断", 10_000);
    await expectText(session, "未吊销", 20_000);
    await expectText(session, "协议版本", 15_000);
    return "GET /api/v1/nodes/{id}/{identity,capabilities,diagnostics} all rendered from the real controller";
  });
  await screenshot(session, join(outDir, "live-16-node-detail.png"));

  await step("32 unconfigured traffic quota shows the honest not-configured state", async () => {
    await expectText(session, "服务端尚未配置该节点的流量配额", 20_000);
    return "GET /api/v1/nodes/node-alpha/traffic-quota -> 404 quota_not_found surfaced as not-configured (GM-W4-02)";
  });

  await step("33 configure and read the traffic quota through the real controller", async () => {
    await clickText(session, "编辑配额", { exact: true });
    await expectText(session, "编辑流量配额与到期提醒", 10_000);
    await fillField(session, "配额 (GB)", "500");
    await screenshot(session, join(outDir, "live-17-quota-editor.png"));
    await clickText(session, "保存配额", { exact: true });
    await expectText(session, "流量配额已保存（服务端结果）", 20_000);
    await waitForCondition(session, "document.body.innerText.includes('配额 (GB)') && document.body.innerText.includes('500')", {
      timeoutMs: 15_000, label: "quota panel shows 500 GB",
    });
    const quota = await evaluate(session, `(${PAGE_FETCH})('/api/v1/nodes/${NODE_A}/traffic-quota')`);
    if (quota.quotaGb !== 500) throw new Error(`server quota mismatch: ${JSON.stringify(quota)}`);
    return `PUT then GET /api/v1/nodes/${NODE_A}/traffic-quota -> quotaGb=${quota.quotaGb}, direction=${quota.direction}`;
  });
  await screenshot(session, join(outDir, "live-18-node-quota.png"));

  // ---- 13. 监控与流量风险（#/monitoring）
  await goto(session, `${base}/#/monitoring`);
  await step("34 resources tab lists the granted node with the traffic-risk source", async () => {
    await expectText(session, NODE_A_LABEL, 20_000);
    await expectText(session, "无返回风险", 20_000);
    const risks = await evaluate(session, `(${PAGE_FETCH})('/api/v1/monitoring/traffic-risks')`);
    if (!Array.isArray(risks)) throw new Error("traffic risks response is not a list");
    return `GET /api/v1/monitoring/traffic-risks -> ${risks.length} risks (quota configured, no usage samples yet)`;
  });

  await step("35 create availability monitor through the real controller", async () => {
    await clickText(session, "服务监控", { exact: true });
    await clickText(session, "新建监控", { exact: true });
    await expectText(session, "新建服务监控", 10_000);
    await fillField(session, "名称", "edge-http");
    // SSRF 边界（internal/monitoring/ssrf.go）拒绝 .internal/.local/loopback/metadata 目标；
    // 使用公网样式主机名通过服务端校验（组合运行时未注入 Prober，探测不会真实发起）。
    await fillField(session, "探测目标", "https://status.edge.example.com/healthz");
    await clickText(session, "保存监控", { exact: true });
    await expectText(session, "监控已保存（服务端结果）", 20_000);
    const monitors = await evaluate(session, `(${PAGE_FETCH})('/api/v1/monitoring/availability-monitors')`);
    const created = Array.isArray(monitors) ? monitors.find(entry => entry.name === "edge-http") : null;
    if (!created) throw new Error("created monitor missing from the real list");
    return `POST /api/v1/monitoring/availability-monitors -> 201 id=${created.id}`;
  });

  await step("36 pause the monitor with a whitelist PATCH body", async () => {
    await clickText(session, "暂停", { exact: true });
    await expectText(session, "监控已请求暂停", 20_000);
    await expectText(session, "已暂停", 15_000);
    return "PATCH /api/v1/monitoring/availability-monitors/{id} -> state=paused (whitelist body per GM-W4-02)";
  });
  await screenshot(session, join(outDir, "live-19-monitoring.png"));

  // ---- 14. 证书管理（#/certificates）
  await goto(session, `${base}/#/certificates`);
  await step("37 internal certificate distributions list is honestly empty", async () => {
    await expectText(session, "内部 TLS 与下发", 20_000);
    await expectText(session, "暂无内部证书任务", 20_000);
    const jobs = await evaluate(session, `(${PAGE_FETCH})('/api/v1/certificates/internal/distributions')`);
    if (!Array.isArray(jobs.items)) throw new Error("distributions response missing items");
    return `GET /api/v1/certificates/internal/distributions -> ${jobs.items.length} jobs (honest empty)`;
  });
  await screenshot(session, join(outDir, "live-20-certificates.png"));

  // ---- 15. 服务发布向导（#/publishing）
  await goto(session, `${base}/#/publishing`);
  await step("38 publishing wizard honestly blocks without a scheduling-eligible gateway", async () => {
    await expectText(session, "发布配置", 20_000);
    await waitForCondition(session, "document.body.innerText.includes('不可调度')", {
      timeoutMs: 20_000, label: "gateway listed as not schedulable",
    });
    await waitForCondition(session, `(() => {
      const select = [...document.querySelectorAll("select")].find(node =>
        [...node.options].some(option => option.textContent.includes("Edge Web")));
      if (!select) return false;
      const option = [...select.options].find(entry => entry.textContent.includes("Edge Web"));
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set;
      setter.call(select, option.value);
      select.dispatchEvent(new Event("change", { bubbles: true }));
      return select.value === option.value;
    })()`, { timeoutMs: 15_000, label: "select service group Edge Web" });
    await clickText(session, "预览变更", { exact: true });
    await expectText(session, "至少选择一个调度可用的网关", 15_000);
    return "wizard blocks on the server-declared eligibility (no nginx checker in the composed runtime; honest)";
  });
  await screenshot(session, join(outDir, "live-21-publishing-gate.png"));

  await step("39 publication preview passes for the granted service group", async () => {
    const groups = await evaluate(session, `(${PAGE_FETCH})('/api/v1/service-groups').then(data => data.items)`);
    const group = groups.find(entry => entry.name === "Edge Web");
    if (!group) throw new Error("service group Edge Web not visible");
    const gateways = await evaluate(session, `(${PAGE_FETCH})('/api/v1/gateways').then(data => data.items)`);
    if (!gateways[0]?.id) throw new Error("no gateway id available for the preview draft");
    const result = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publishing/preview', 'POST', {
      serviceGroupId: ${JSON.stringify(group.id)},
      gatewayIds: [${JSON.stringify(gateways[0].id)}],
      domain: "app.example.live",
      https: true,
    })`);
    if (result.status !== 200) throw new Error(`preview ${result.status}: ${JSON.stringify(result.body).slice(0, 300)}`);
    if (result.body?.eligible !== true) throw new Error(`preview not eligible: ${JSON.stringify(result.body)}`);
    if (!Array.isArray(result.body.targetNodeIdsSnapshot) || !result.body.targetNodeIdsSnapshot.includes(NODE_A)) {
      throw new Error(`preview snapshot missing node-alpha: ${JSON.stringify(result.body)}`);
    }
    return `POST /api/v1/publishing/preview -> 200 eligible=true, snapshot=${result.body.targetNodeIdsSnapshot.join(",")}`;
  });

  await step("40 publication created with three staged plan and listed in the wizard", async () => {
    const groups = await evaluate(session, `(${PAGE_FETCH})('/api/v1/service-groups').then(data => data.items)`);
    const group = groups.find(entry => entry.name === "Edge Web");
    const gateways = await evaluate(session, `(${PAGE_FETCH})('/api/v1/gateways').then(data => data.items)`);
    const created = await evaluate(session, `(${PAGE_WRITE})('/api/v1/publishing/publications', 'POST', {
      name: "Edge Web launch",
      serviceGroupId: ${JSON.stringify(group.id)},
      gatewayIds: [${JSON.stringify(gateways[0].id)}],
      domain: "app.example.live",
      https: true,
    })`);
    if (created.status !== 202 && created.status !== 201) {
      throw new Error(`publication create ${created.status}: ${JSON.stringify(created.body).slice(0, 300)}`);
    }
    if (created.body?.status !== "queued" || (created.body?.stages?.length ?? 0) !== 3) {
      throw new Error(`unexpected plan shape: ${JSON.stringify(created.body).slice(0, 300)}`);
    }
    await clickText(session, "刷新", { exact: true });
    await expectText(session, created.body.id, 20_000);
    await expectText(session, "3 个", 15_000);
    return `POST /api/v1/publishing/publications -> ${created.status} ${created.body.id} (dns/certificate/gateway stages queued)`;
  });
  await screenshot(session, join(outDir, "live-22-publishing.png"));

  // ---- 16. AI 助手与安全策略（#/ai）
  await goto(session, `${base}/#/ai`);
  await step("41 AI security policy read from the real controller", async () => {
    await expectText(session, "服务端安全策略声明", 20_000);
    await expectText(session, "默认拒绝：是", 15_000);
    await expectText(session, "服务端强制：是", 10_000);
    await expectText(session, "未授权发现：禁止", 10_000);
    return "GET /api/v1/ai/security-policy -> defaultDeny=true, serverSideEnforced=true";
  });

  await step("42 create AI session with default zero resource authorization", async () => {
    await clickText(session, "新建 AI 会话", { exact: true });
    await waitForUrl(session, "#/ai/");
    await expectText(session, "默认零授权", 20_000);
    await expectText(session, "空（默认无资源）", 15_000);
    return "POST /api/v1/ai/sessions -> 201 (zero grants, allowedNodeIds empty)";
  });
  await screenshot(session, join(outDir, "live-23-ai-zero-scope.png"));

  const aiSessionId = decodeURIComponent(
    ((await evaluate(session, "location.hash")).split("#/ai/")[1] ?? "").replace(/\/$/, ""),
  );
  if (!aiSessionId) throw new Error("could not read the AI session id from the URL");

  await step("43 grant node-alpha to the AI session with a registered tool", async () => {
    await fillField(session, "允许动作（逗号分隔）", "tasks.command");
    await waitForCondition(session, `(() => {
      const select = [...document.querySelectorAll("select")].find(node =>
        [...node.options].some(option => option.textContent.includes(${JSON.stringify(NODE_A_LABEL)})));
      if (!select) return false;
      const option = [...select.options].find(entry => entry.textContent.includes(${JSON.stringify(NODE_A_LABEL)}));
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set;
      setter.call(select, option.value);
      select.dispatchEvent(new Event("change", { bubbles: true }));
      return select.value === option.value;
    })()`, { timeoutMs: 15_000, label: "select node-alpha as the AI grant resource" });
    await clickText(session, "添加授权", { exact: true });
    await expectText(session, "授权数：1", 20_000);
    await expectText(session, `有效节点：${NODE_A}`, 15_000);
    return `POST /api/v1/ai/sessions/${aiSessionId}/grants -> scopeRevision advanced, allowedNodeIds=[${NODE_A}]`;
  });
  await screenshot(session, join(outDir, "live-24-ai-grant.png"));

  await step("44 switch approval policy to automatic and back to per-action", async () => {
    await clickText(session, "审批策略", { exact: true });
    await expectText(session, "生效模式", 20_000);
    // 模式卡片按钮文本含描述段落，不能用 exact 匹配；contains 取最短包含者即该卡片。
    await clickText(session, "自动审批");
    await expectText(session, "生效模式：自动审批", 20_000);
    await clickText(session, "每次审批");
    await expectText(session, "生效模式：每次审批", 20_000);
    return `PATCH /api/v1/ai/sessions/${aiSessionId}/approval-policy accepted both ways`;
  });

  await step("45 create and approve a one-time tool approval", async () => {
    await clickText(session, "工具审批", { exact: true });
    await fillField(session, "工具名称", "tasks.command");
    await fillField(session, "目标节点 ID（逗号分隔）", NODE_A);
    await fillField(session, "工具参数（JSON 对象）", '{"command":"uptime"}');
    await clickText(session, "请求审批", { exact: true });
    await expectText(session, "参数摘要", 20_000);
    await screenshot(session, join(outDir, "live-25-ai-approval-pending.png"));
    await clickText(session, "批准一次", { exact: true });
    await expectText(session, "approved", 20_000);
    return "POST approvals + decision approve; server returned canonicalArgumentsHash before allowing the decision";
  });

  await step("46 execute the controlled tool through the real task pipeline", async () => {
    await clickText(session, "提交受控工具调用", { exact: true });
    await expectText(session, "已接受：是", 20_000);
    return "POST /api/v1/ai/tools/execute -> 202 queued (routed through tasks.Service.Submit, not a bypass)";
  });
  await screenshot(session, join(outDir, "live-26-ai-execution.png"));

  // ---- 17. 插件中心与 Xray 声明式门控（#/plugins, #/plugins/xray）
  await goto(session, `${base}/#/plugins`);
  await step("47 plugin repository read with honestly empty catalog and installs", async () => {
    await expectText(session, "插件来源", 20_000);
    await expectText(session, "当前来源：official", 15_000);
    await expectText(session, "目录为空", 20_000);
    await expectText(session, "未安装可选插件", 15_000);
    return "GET /api/v1/plugins{,/repository,/catalog} all served by the real controller";
  });

  await step("48 catalog refresh honestly reports the missing release source", async () => {
    await clickText(session, "刷新目录", { exact: true });
    await expectText(session, "PLUGIN_CATALOG_SOURCE_UNAVAILABLE", 20_000);
    return "POST /api/v1/plugins/catalog/refresh -> 501 (no release adapter wired; honest error surfaced, GM-W4-02)";
  });
  await screenshot(session, join(outDir, "live-27-plugins.png"));

  await goto(session, `${base}/#/plugins/xray`);
  await step("49 xray console is gated while the plugin is not enabled", async () => {
    await expectText(session, "插件未启用", 20_000);
    await expectText(session, "前往插件中心安装并启用", 10_000);
    const text = await evaluate(session, "document.body.innerText");
    if (text.includes("Xray 节点") && text.includes("节点与实例") && text.includes("创建安装任务")) {
      throw new Error("xray console rendered while the plugin is not enabled");
    }
    return "GET /api/v1/plugins/xray-manager -> 404 PLUGIN_NOT_FOUND; declarative console stays gated";
  });
  await screenshot(session, join(outDir, "live-28-xray-gate.png"));

  // ---- 18. 更新中心与备份恢复（#/updates, #/backups）
  await goto(session, `${base}/#/updates`);
  await step("50 update check honestly reports the unavailable release source", async () => {
    await expectText(session, "检查更新", 20_000);
    await clickText(session, "检查更新", { exact: true });
    await expectText(session, "检查失败", 20_000);
    await expectText(session, "UPDATE_SOURCE_UNAVAILABLE", 10_000);
    return "POST /api/v1/system/updates/check -> 503 UPDATE_SOURCE_UNAVAILABLE (no feed adapter; honest)";
  });

  await step("51 update plan generation honestly reports the unavailable planner", async () => {
    await fillField(session, "目标版本", "9.9.9");
    await clickText(session, "生成计划", { exact: true });
    await expectText(session, "生成计划失败", 20_000);
    await expectText(session, "UPDATE_PLANNER_UNAVAILABLE", 10_000);
    return "POST /api/v1/system/updates/plan -> 503 UPDATE_PLANNER_UNAVAILABLE (honest)";
  });

  await step("52 update job history is honestly empty", async () => {
    await expectText(session, "暂无更新作业", 20_000);
    const jobs = await evaluate(session, `(${PAGE_FETCH})('/api/v1/system/updates/jobs')`);
    if (!Array.isArray(jobs.items)) throw new Error("update jobs response missing items");
    return `GET /api/v1/system/updates/jobs -> ${jobs.items.length} jobs`;
  });
  await screenshot(session, join(outDir, "live-29-updates.png"));

  const backupID = "live-backup-001";
  await createLocalBackup(dbPath, backupID);
  await goto(session, `${base}/#/backups`);
  await step("53 verify the real local backup through the controller", async () => {
    await expectText(session, "校验备份", 20_000);
    await fillField(session, "备份 ID", backupID);
    await clickText(session, "校验备份", { exact: true });
    await expectText(session, "backup_integrity", 20_000);
    await expectText(session, "校验请求已受理", 10_000);
    return `POST /api/v1/backups/${backupID}/verify -> backup_integrity passed (VACUUM INTO fixture, real integrity check)`;
  });
  await screenshot(session, join(outDir, "live-30-backup-verify.png"));

  await step("54 recovery preview with fenced declaration shows honest failed checks", async () => {
    await fillField(session, "目标主控 ID", "controller-live-01");
    // 预演表单在验证面板下方（折叠区外），先滚动到可见再勾选。
    await waitForCondition(session, `(() => {
      const label = [...document.querySelectorAll("label")].find(node => node.textContent.includes("旧主控已隔离"));
      if (!label) return false;
      label.scrollIntoView({ block: "center" });
      return true;
    })()`, { timeoutMs: 15_000, label: "scroll fenced declaration into view" });
    await setCheckbox(session, "旧主控已隔离", true);
    await clickText(session, "执行预演", { exact: true });
    await expectText(session, "target_identity_not_observed", 20_000);
    await expectText(session, "fencing_evidence_unavailable", 10_000);
    return "POST /api/v1/disaster-recovery/preview -> identity/fencing checks fail closed (checkbox is not evidence)";
  });

  await step("55 recovery job creation stays blocked after an ineligible preview", async () => {
    await expectText(session, "预演未通过", 15_000);
    const disabled = await evaluate(session, `(() => {
      const button = [...document.querySelectorAll("button")].find(node => node.textContent.trim() === "创建恢复作业");
      return button ? button.disabled : null;
    })()`);
    if (disabled !== true) throw new Error(`create recovery job button state: ${disabled}`);
    return "POST /api/v1/disaster-recovery/jobs stays gated by the server verdict; UI disables submission";
  });
  await screenshot(session, join(outDir, "live-31-recovery-preview.png"));

  // ---- 19. Wave 3 新域审计动作落盘
  await goto(session, `${base}/#/audit`);
  await step("56 audit records the wave 3 domain actions", async () => {
    await expectText(session, "monitoring.create", 25_000);
    const text = await evaluate(session, "document.body.innerText");
    const expected = ["monitoring.update", "quota.policy", "publishing.plan_create", "ai.sessions.create", "ai.sessions.grant"];
    const missing = expected.filter(action => !text.includes(action));
    if (missing.length > 0) throw new Error(`audit missing wave 3 actions: ${missing.join(", ")}`);
    return "GET /api/v1/audit/events covers monitoring/quota/publishing/ai domains";
  });
  await screenshot(session, join(outDir, "live-32-audit-wave3.png"), { width: 1280, height: 1400 });

  // =====================================================================
  // GM-W5-02 · Wave 5 新增接线模块（工作流管理/监控历史与探测/文件字节流上传/
  // 发布执行与回滚）真后端浏览器实测。组合运行时未注入监控探测器、文件传输
  // 通道与外部发布器；这些路径按服务端语义诚实失败，验收断言诚实错误，绝不
  // 伪造成功。
  // =====================================================================

  // ---- 20. 自动化工作流（#/workflows）
  await goto(session, `${base}/#/workflows`);
  await step("57 workflow console reads the real controller list", async () => {
    await expectText(session, "创建工作流", 20_000);
    const workflows = await evaluate(session, `(${PAGE_FETCH})('/api/v1/workflows')`);
    if (!Array.isArray(workflows.items)) throw new Error("workflows response missing items");
    return `GET /api/v1/workflows -> ${workflows.items.length} items (workflows.read granted)`;
  });
  await screenshot(session, join(outDir, "live-33-workflows-console.png"));

  let workflowId = null;
  let workflowTaskId = null;
  await step("58 create a workflow through the UI (POST -> 201)", async () => {
    await fillField(session, "目标节点 ID", NODE_A);
    await fillField(session, "参数（每行 key=value，可选）", "command=uptime");
    await clickText(session, "创建工作流", { exact: true });
    await expectText(session, "已创建", 20_000);
    const workflows = await evaluate(session, `(${PAGE_FETCH})('/api/v1/workflows')`);
    const created = (workflows.items ?? []).find(entry =>
      entry.trigger === "manual" && Array.isArray(entry.steps) && entry.steps[0]?.actionId === "tasks.command");
    if (!created) throw new Error("created workflow missing from the real list");
    if (created.enabled !== true || created.revision !== 1) {
      throw new Error(`unexpected created state: ${JSON.stringify(created)}`);
    }
    workflowId = created.id;
    return `POST /api/v1/workflows -> 201 ${created.id} (manual trigger, tasks.command on ${NODE_A})`;
  });

  await step("59 toggle enable state with expectedRevision (PATCH -> 200 twice)", async () => {
    await clickText(session, "停用", { exact: true });
    await expectText(session, "已停用（revision 2）", 20_000);
    const paused = await evaluate(session, `(${PAGE_FETCH})('/api/v1/workflows/${workflowId}')`);
    if (paused.workflow?.enabled !== false) throw new Error("workflow still enabled after pause");
    await clickText(session, "启用", { exact: true });
    await expectText(session, "已启用（revision 3）", 20_000);
    return "PATCH /api/v1/workflows/{id} -> 200 (enabled=false then true; revision advanced server-side)";
  });

  await step("60 trigger a manual run (POST runs -> 202) and surface the generated task id", async () => {
    await clickText(session, "触发运行", { exact: true });
    await expectText(session, "任务 ID：tsk_", 25_000);
    const detail = await evaluate(session, `(${PAGE_FETCH})('/api/v1/workflows/${workflowId}')`);
    const run = (detail.runs ?? [])[0];
    if (!run?.steps?.[0]?.taskId) throw new Error(`no persisted run with taskId: ${JSON.stringify(detail.runs)}`);
    if (run.state !== "running" && run.state !== "queued" && run.state !== "pending") {
      throw new Error(`unexpected run state: ${run.state}`);
    }
    workflowTaskId = run.steps[0].taskId;
    return `POST /api/v1/workflows/${workflowId}/runs -> 202 matched=true; task ${workflowTaskId} submitted through the sole task service`;
  });
  await screenshot(session, join(outDir, "live-34-workflow-run.png"));

  await step("61 workflow detail dialog lists the run with its task id", async () => {
    await clickText(session, "详情", { exact: true });
    await expectText(session, "运行记录", 15_000);
    await expectText(session, workflowTaskId, 10_000);
    await clickText(session, "关闭", { exact: true });
    return `GET /api/v1/workflows/${workflowId} -> 200 with runs[0].taskId rendered`;
  });

  await step("62 workflow task is visible in the unified task center", async () => {
    await goto(session, `${base}/#/tasks/${encodeURIComponent(workflowTaskId)}`);
    await expectText(session, "任务快照", 20_000);
    return `workflow-submitted task ${workflowTaskId} opened in the task center (no bypass channel)`;
  });

  // ---- 21. 监控检查历史与立即探测（#/monitoring）
  await goto(session, `${base}/#/monitoring`);
  await step("63 check history is real and honestly empty before any probe", async () => {
    await clickText(session, "服务监控", { exact: true });
    await expectText(session, "edge-http", 20_000);
    await clickText(session, "检查历史", { exact: true });
    await expectText(session, "暂无检查记录", 15_000);
    await clickText(session, "关闭", { exact: true });
    const history = await evaluate(session, `(${PAGE_FETCH})('/api/v1/availability-monitors')`);
    const monitor = (Array.isArray(history) ? history : []).find(entry => entry.name === "edge-http");
    if (!monitor?.id) throw new Error("edge-http monitor missing from the real list");
    const samples = await evaluate(session, `(${PAGE_FETCH})('/api/v1/availability-monitors/${monitor.id}/history')`);
    if (samples.monitorId !== monitor.id || !Array.isArray(samples.items)) {
      throw new Error(`unexpected history payload: ${JSON.stringify(samples).slice(0, 200)}`);
    }
    return `GET /api/v1/availability-monitors/${monitor.id}/history -> 200 with ${samples.items.length} persisted samples (honest empty)`;
  });

  await step("64 immediate probe fails closed without a wired prober (503 prober_unavailable)", async () => {
    await clickText(session, "立即探测", { exact: true });
    await expectText(session, "HTTP 503 · 错误码 prober_unavailable", 25_000);
    return "POST /api/v1/availability-monitors/{id}/probe -> 503 prober_unavailable (NetProber not wired in composed runtime; honest error surfaced)";
  });
  await screenshot(session, join(outDir, "live-35-monitoring-history-probe.png"));

  // ---- 22. 文件字节流上传与完成校验（#/files）
  const UPLOAD_CONTENT = "serverfleet wave5 live upload";
  await goto(session, `${base}/#/files`);
  await step("65 byte-stream upload through session content channel with server SHA-256 verification", async () => {
    await expectText(session, "上传文件", 20_000);
    const injected = await evaluate(session, `(() => {
      const input = [...document.querySelectorAll('input[type="file"]')][0];
      if (!input) throw new Error("file input missing");
      const file = new File([${JSON.stringify(UPLOAD_CONTENT)}], "live-wave5.txt", { type: "text/plain" });
      const transfer = new DataTransfer();
      transfer.items.add(file);
      input.files = transfer.files;
      // 页面的 change 处理器会同步执行 event.target.value = ""；长度必须在派发前读取。
      const injectedCount = input.files.length;
      input.dispatchEvent(new Event("change", { bubbles: true }));
      return injectedCount;
    })()`);
    if (injected !== 1) throw new Error(`file input injection returned ${injected}`);
    await expectText(
      session,
      `文件字节流（${UPLOAD_CONTENT.length} B）已上传并通过服务端 SHA-256 校验；当前节点文件传输通道未注入（503 unavailable）`,
      30_000,
    );
    return `POST .../files/uploads -> 201; PUT .../content?offset=0 -> 200 bytesReceived=${UPLOAD_CONTENT.length}; POST .../complete -> 503 unavailable (transport not wired; bytes verified server-side)`;
  });
  await screenshot(session, join(outDir, "live-36-files-byte-upload.png"));

  // ---- 23. 服务发布执行与回滚（#/publishing）
  await goto(session, `${base}/#/publishing`);
  const publicationId = await evaluate(session, `(${PAGE_FETCH})('/api/v1/publishing/publications').then(list => {
    const queued = (Array.isArray(list) ? list : []).find(entry => entry.status === "queued") ?? (list ?? [])[0];
    return queued ? queued.id : null;
  })`);
  if (!publicationId) throw new Error("no publication available for the Wave 5 run/rollback steps");
  await step("66 publication run honestly fails closed without a wired publisher (502 stage_failed)", async () => {
    await expectText(session, publicationId, 20_000);
    await clickText(session, "执行发布", { exact: true });
    await expectText(session, "HTTP 502 · 错误码 stage_failed", 30_000);
    const detail = await evaluate(session, `(${PAGE_FETCH})('/api/v1/publishing/publications/${publicationId}')`);
    if (detail.status !== "partially_completed") throw new Error(`plan status after failed run: ${detail.status}`);
    if (detail.stages?.[0]?.status !== "failed") throw new Error(`first stage after failed run: ${JSON.stringify(detail.stages)}`);
    return `POST /api/v1/publications/${publicationId}/run -> 502 stage_failed; dns stage persisted as failed (publisher not wired; honest)`;
  });

  await step("67 stage rollback honestly fails without a publisher compensation port (502 rollback_failed)", async () => {
    await clickText(session, "阶段详情", { exact: true });
    await expectText(session, "回滚此阶段", 15_000);
    // 三个阶段各有回滚按钮；明确点击第一个（dns —— 唯一已执行并失败、可回滚的阶段）。
    const clicked = await evaluate(session, `(() => {
      const buttons = [...document.querySelectorAll("button")]
        .filter(node => node.textContent.trim() === "回滚此阶段" && node.getBoundingClientRect().height > 0);
      if (buttons.length === 0) return false;
      buttons[0].scrollIntoView({ block: "center" });
      buttons[0].click();
      return true;
    })()`);
    if (!clicked) throw new Error("no rollback button rendered in the stage dialog");
    await expectText(session, "HTTP 502 · 错误码 rollback_failed", 30_000);
    return `POST /api/v1/publications/${publicationId}/rollback {stage:"dns"} -> 502 rollback_failed (no compensation port wired; honest, stages refreshed)`;
  });
  await screenshot(session, join(outDir, "live-37-publishing-run-rollback.png"));

  // ---- 24. Wave 5 审计闭环
  await goto(session, `${base}/#/audit`);
  await step("68 audit records the wave 5 workflow/files/publishing actions", async () => {
    await expectText(session, "workflows.save", 25_000);
    const text = await evaluate(session, "document.body.innerText");
    const expected = ["workflows.trigger", "workflows.task.submit", "files.upload_create", "publishing.stage", "publishing.run", "publishing.rollback"];
    const missing = expected.filter(action => !text.includes(action));
    if (missing.length > 0) throw new Error(`audit missing wave 5 actions: ${missing.join(", ")}`);
    return "GET /api/v1/audit/events covers workflows/files.upload/publishing Wave 5 actions";
  });
  await screenshot(session, join(outDir, "live-38-audit-wave5.png"), { width: 1280, height: 1400 });

  await session.close();
}

try {
  await main();
  const failed = results.filter(entry => !entry.ok);
  console.log(`\nlive controller acceptance: ${results.length - failed.length}/${results.length} steps passed`);
  if (failed.length > 0) {
    console.log("failed steps:");
    for (const entry of failed) console.log(`  - ${entry.step}: ${entry.detail}`);
    process.exitCode = 1;
  }
} catch (error) {
  console.error(`live acceptance aborted: ${error.stack ?? error}`);
  process.exitCode = 1;
} finally {
  await cleanup();
}
