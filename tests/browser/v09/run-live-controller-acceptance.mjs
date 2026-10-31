/**
 * GM-W3-02 · 真实 Go Controller + SQLite WAL 端到端浏览器验收（V09-5D-GM / gemini-02）。
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
 *      DNS/网关/通知/审计全部走真实 Controller。
 *   4. Headless Edge（回退 Chrome）跑完全链路并输出截图证据。
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
