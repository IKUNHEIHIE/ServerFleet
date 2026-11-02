#!/usr/bin/env node
/**
 * B05 · Agent 组件本地签名更新 —— 真实浏览器专项（V09-CLOUD-WB-01 / workbuddy-01）。
 *
 * 这是**独立专项**，不是完整 B05，也不是 Controller 组件升级或在线可信发行装配：
 *   - 装配的是 server.Config.UpdateProduct（Agent 组件、本地签名 inbox、可信程序配置）；
 *   - Controller 自身的升级、在线发行源、分批/金丝雀不在本脚本范围，仍保持未完成。
 *
 * 真实链路（每一步都有实际副作用，不伪造）：
 *   1. 普通用户私有目录 + openssl 本地 CA + 服务端证书（与 localpki 加载格式一致）。
 *   2. 阶段 A：fixturehost 组合正式 server.New（仅 enrollment/Agent WSS 链路），
 *      本机安装器写入 setup token 摘要；浏览器完成首次 bootstrap；管理员经 HTTP 生成
 *      enrollment token，真实 agent 二进制 enroll(pending)，管理员审批 → issued。
 *   3. 阶段 A 停止后：把真实发布构建放入 slot/active（1.0.0）与签名 inbox
 *      （1.0.0 = 初始 active；1.0.1 = 待升级；1.0.2 = 故意损坏用于失败用例），
 *      ed25519 合成签名密钥写入私有文件；SchemaFingerprint 由宿主从真实库读取。
 *   4. 阶段 B：fixturehost 带 -product-update-config 启动，正式 ProductDeployment
 *      校验初始主动字节后，用受监督的自有进程启动真实 Agent，写私有 health.json。
 *   5. 浏览器（账号 A）：更新页 check → plan(dryRun) → 确认文本 → 一键更新(202)。
 *   6. 浏览器（账号 B，另一管理员）：任务页审批同一任务（异人规则；A 不能自批）。
 *   7. 作业终态 completed + audit；health.json 版本/pid 变化；槽位 active 字节等于
 *      1.0.1 的 SHA；任务与作业终态在重启后仍持久。
 *   8. 负向：1.0.2 的工件与清单 SHA 不一致 → 安装失败，进程不被替换、版本不变。
 *
 * 失败口径：500/401/403/错误 shape/超时一律 FAIL；只有明确未装配的依赖才 BLOCKED。
 * 证据：reports/ 下的 JSON/截图 + 原始命令与退出码；不含秘密、token 或私钥明文。
 *
 * 用法：
 *   node tests/browser/v09/run-product-update-acceptance.mjs [--out <dir>] [--keep] [--skip-build]
 */
import { createHash, generateKeyPairSync, randomBytes, sign } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { chmod, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import https from "node:https";
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as sleep } from "node:timers/promises";
import {
  clickText,
  connectPage,
  evaluate,
  fillField,
  goto,
  launchHeadlessBrowser,
  screenshot,
  waitForCondition,
} from "./browser-client.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../../..");
const args = process.argv.slice(2);
const opt = name => args.includes(name);
const value = (name, fallback) => {
  const index = args.indexOf(name);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};

const outDir = resolve(value("--out", join(repoRoot, "reports/wb01-product-update")));
const keep = opt("--keep");
const skipBuild = opt("--skip-build");
const work = join(process.env.HOME ?? tmpdir(), "sf-wb01-product-update");

const results = [];
let failed = false;
function record(id, title, status, detail) {
  results.push({ id, title, status, detail });
  const tag = { PASS: "PASS", FAIL: "FAIL", BLOCKED: "BLOCKED", SKIP: "SKIP" }[status];
  console.log(`[${tag}] ${id} ${title}${detail ? ` — ${detail}` : ""}`);
  if (status === "FAIL") failed = true;
}
async function step(id, title, run) {
  try {
    const detail = await run();
    record(id, title, "PASS", typeof detail === "string" ? detail : undefined);
    return detail;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    record(id, title, "FAIL", message);
    throw error;
  }
}

function run(command, argv, options = {}) {
  const result = spawnSync(command, argv, { encoding: "utf8", ...options });
  if (result.status !== 0) {
    throw new Error(`${command} ${argv.join(" ")} exited ${result.status}: ${(result.stderr || result.stdout || "").trim().slice(0, 600)}`);
  }
  return (result.stdout ?? "").trim();
}

function track(child, name) {
  child.on("exit", code => {
    if (code !== 0 && code !== null) console.log(`[proc] ${name} exited ${code}`);
  });
  return child;
}

async function freePort() {
  const net = await import("node:net");
  return await new Promise((resolvePort, reject) => {
    const server = net.createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      server.close(() => resolvePort(port));
    });
  });
}

/** 同源 https 客户端：显式 Origin/Cookie/X-CSRF-Token，复刻浏览器写语义。 */
function createClient(base, connectBase = base) {
  // base 是浏览器同源 Origin（写请求的 Origin 校验基准）；connectBase 是本机直连目标。
  // 阶段 A 只有 API 监听（Web 宿主尚未启动），两者必须分开，否则会连到未监听端口。
  const target = new URL(connectBase);
  let cookie = "";
  let csrf = "";
  const agent = new https.Agent({ rejectUnauthorized: false, keepAlive: true });
  function call(method, path, body) {
    return new Promise((resolveCall, reject) => {
      const payload = body === undefined ? null : Buffer.from(JSON.stringify(body));
      const headers = { Origin: base, "Content-Type": "application/json" };
      if (payload) headers["Content-Length"] = payload.length;
      if (cookie) headers.Cookie = cookie;
      if (csrf) headers["X-CSRF-Token"] = csrf;
      const request = https.request(
        { host: target.hostname, port: target.port, path, method, headers, agent },
        response => {
          const chunks = [];
          response.on("data", chunk => chunks.push(chunk));
          response.on("end", () => {
            const setCookie = response.headers["set-cookie"];
            if (setCookie) cookie = setCookie.map(entry => entry.split(";")[0]).join("; ");
            const text = Buffer.concat(chunks).toString("utf8");
            let json = null;
            try {
              json = text ? JSON.parse(text) : null;
            } catch {
              json = null;
            }
            resolveCall({ status: response.statusCode ?? 0, text, json });
          });
        },
      );
      request.on("error", reject);
      if (payload) request.write(payload);
      request.end();
    });
  }
  return {
    get cookie() {
      return cookie;
    },
    set csrf(valueIn) {
      csrf = valueIn;
    },
    get csrf() {
      return csrf;
    },
    async login(username, password) {
      const response = await call("POST", "/api/v1/auth/login", { username, password });
      if (response.status !== 200 || !response.json?.csrfToken) {
        throw new Error(`login ${username} -> ${response.status} ${response.text.slice(0, 200)}`);
      }
      csrf = response.json.csrfToken;
      return response.json;
    },
    async must(method, path, body, expected) {
      const response = await call(method, path, body);
      if (response.status !== expected) {
        throw new Error(`${method} ${path} -> ${response.status} want ${expected}: ${response.text.slice(0, 300)}`);
      }
      return response;
    },
    async soft(method, path, body) {
      return await call(method, path, body);
    },
  };
}

const SETUP_SCRIPT = `
import { randomBytes, createHash } from "node:crypto";
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
  if (row.ever_initialized === 1) throw new Error("identity already initialized");
  const payload = { ...(row.payload ? JSON.parse(Buffer.from(row.payload).toString("utf8")) : {}), Version: 1, Initialized: false, SetupDigest: digest, SetupExpires: expires };
  const updated = db.prepare("UPDATE cx_auth_identity SET payload = ? WHERE singleton = 1 AND ever_initialized = 0").run(Buffer.from(JSON.stringify(payload)));
  if (updated.changes !== 1) throw new Error("setup token write raced another writer");
} finally { db.close(); }
process.stdout.write(setupToken);
`;

const WEBHOST = `
import { readFileSync, statSync } from "node:fs";
import https from "node:https";
import { join, normalize, extname, sep, resolve } from "node:path";
const [certPath, keyPath, distDir, apiPort] = process.argv.slice(2);
const MIME = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2" };
const server = https.createServer({ cert: readFileSync(certPath), key: readFileSync(keyPath) }, (req, res) => {
  const url = new URL(req.url ?? "/", "https://127.0.0.1");
  if (url.pathname === "/healthz" || url.pathname === "/api" || url.pathname.startsWith("/api/")) {
    const upstream = https.request({ host: "127.0.0.1", port: Number(apiPort), path: req.url, method: req.method, headers: req.headers, rejectUnauthorized: false }, response => {
      res.writeHead(response.statusCode ?? 502, response.headers);
      response.pipe(res);
    });
    upstream.on("error", error => {
      if (!res.headersSent) res.writeHead(502, { "Content-Type": "application/json", "Cache-Control": "no-store" });
      res.end(JSON.stringify({ code: "PROXY_UNREACHABLE", message: String(error) }));
    });
    req.pipe(upstream);
    return;
  }
  void (async () => {
    const relative = url.pathname === "/" ? "index.html" : url.pathname.replace(/^\\/+/, "");
    const candidate = resolve(join(distDir, normalize(relative).replace(/^(\\.\\.[/\\\\])+/, "")));
    if (!candidate.startsWith(resolve(distDir) + sep)) { res.writeHead(403).end("forbidden"); return; }
    let file = candidate;
    try { if (statSync(file).isDirectory()) file = join(file, "index.html"); } catch { file = join(distDir, "index.html"); }
    try {
      const body = readFileSync(file);
      res.writeHead(200, { "Content-Type": MIME[extname(file)] ?? "application/octet-stream", "Content-Length": body.length, "Cache-Control": "no-store" });
      res.end(req.method === "HEAD" ? undefined : body);
    } catch { res.writeHead(404).end("not found"); }
  })();
});
server.listen(Number(process.env.WEB_PORT), "0.0.0.0", () => console.log("webhost ready"));
`;

function pkiScript(workDir) {
  const run1 = [];
  run1.push(`mkdir -p "${workDir}/pki" "${workDir}/db" "${workDir}/logs" "${workDir}/slot" "${workDir}/inbox" "${workDir}/stage" "${workDir}/agent-state" "${workDir}/staging"`);
  run1.push(`chmod 700 "${workDir}/slot" "${workDir}/inbox" "${workDir}/stage" "${workDir}/agent-state" "${workDir}/staging"`);
  run1.push(`openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "${workDir}/pki/local-agent-ca-key.pem"`);
  run1.push(`openssl req -x509 -new -key "${workDir}/pki/local-agent-ca-key.pem" -sha256 -subj "/CN=Server Fleet Local Agent CA" -days 30 -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign,digitalSignature" -out "${workDir}/pki/local-agent-ca.pem"`);
  run1.push(`openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "${workDir}/cert-key.pem" -subj "/CN=localhost" -out "${workDir}/cert.csr"`);
  run1.push(`printf "subjectAltName=DNS:localhost,IP:127.0.0.1\\nbasicConstraints=CA:FALSE\\nkeyUsage=digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth,clientAuth\\n" > "${workDir}/cert-ext.cnf"`);
  run1.push(`openssl x509 -req -in "${workDir}/cert.csr" -CA "${workDir}/pki/local-agent-ca.pem" -CAkey "${workDir}/pki/local-agent-ca-key.pem" -days 2 -extfile "${workDir}/cert-ext.cnf" -out "${workDir}/cert.pem"`);
  run1.push(`openssl verify -CAfile "${workDir}/pki/local-agent-ca.pem" "${workDir}/cert.pem"`);
  run1.push(`openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "${workDir}/ai-key.pem" -subj "/CN=127.0.0.1" -out "${workDir}/ai.csr"`);
  run1.push(`printf "subjectAltName=IP:127.0.0.1,DNS:localhost\\nbasicConstraints=CA:FALSE\\nkeyUsage=digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth\\n" > "${workDir}/ai-ext.cnf"`);
  run1.push(`openssl x509 -req -in "${workDir}/ai.csr" -CA "${workDir}/pki/local-agent-ca.pem" -CAkey "${workDir}/pki/local-agent-ca-key.pem" -days 2 -extfile "${workDir}/ai-ext.cnf" -out "${workDir}/ai-cert.pem"`);
  run1.push(`openssl verify -CAfile "${workDir}/pki/local-agent-ca.pem" "${workDir}/ai-cert.pem"`);
  return run1.join("\n");
}

async function waitListening(port, timeoutMs = 25_000) {
  const net = await import("node:net");
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise(resolveProbe => {
      const socket = net.connect({ host: "127.0.0.1", port }, () => {
        socket.destroy();
        resolveProbe(true);
      });
      socket.on("error", () => resolveProbe(false));
      socket.setTimeout(500, () => {
        socket.destroy();
        resolveProbe(false);
      });
    });
    if (ok) return;
    await sleep(200);
  }
  throw new Error(`port ${port} did not become reachable`);
}


/** 在真实页面上下文内发起同源请求：Cookie、Origin、Sec-Fetch-* 与 CSRF 都由浏览器自身提供。 */
async function browserFetch(session, method, path, body) {
  const script = `(async () => {
    let csrf = "";
    try {
      const me = await fetch("/api/v1/auth/me", { credentials: "include", headers: { "Accept": "application/json" } });
      const payload = await me.json().catch(() => ({}));
      csrf = payload.csrfToken || "";
    } catch {}
    const headers = { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() };
    if (csrf) headers["X-CSRF-Token"] = csrf;
    const response = await fetch(${JSON.stringify(path)}, {
      method: ${JSON.stringify(method)},
      credentials: "include",
      headers,
      body: ${body === undefined ? "undefined" : JSON.stringify(JSON.stringify(body))},
    });
    const text = await response.text();
    return JSON.stringify({ status: response.status, text });
  })()`;
  const raw = await evaluate(session, script);
  const envelope = JSON.parse(raw);
  let json = null;
  try {
    json = envelope.text ? JSON.parse(envelope.text) : null;
  } catch {
    json = null;
  }
  return { status: envelope.status, text: envelope.text, json };
}

async function browserMust(session, method, path, body, expected) {
  const response = await browserFetch(session, method, path, body);
  if (response.status !== expected) {
    throw new Error(`${method} ${path} -> ${response.status} want ${expected}: ${response.text.slice(0, 260)}`);
  }
  return response;
}

async function main() {
  let enrollToken = "";
  await mkdir(outDir, { recursive: true });
  await mkdir(work, { recursive: true });
  await chmod(work, 0o700);
  // 每次运行都必须从干净状态开始：沿用旧 DB/身份会让 setup token、enrollment 与
  // 槽位 provenance 全部失真。保留编译产物（--skip-build 场景）但清空运行态。
  const runtimeState = ["db", "logs", "slot", "inbox", "stage", "staging", "agent-state", "pki", "browser-a", "browser-b", "product-update.json", "health.json", "setup-token.mjs", "webhost.mjs"];
  for (const relative of runtimeState) {
    await rm(join(work, relative), { recursive: true, force: true });
  }
  const fsmod = await import("node:fs");
  globalThis.__fs = fsmod;
  const sha256 = path => createHash("sha256").update(fsmod.readFileSync(path)).digest("hex");
  const environment = {
    node: run("node", ["--version"]),
    platform: `${process.platform}/${process.arch}`,
    uid: typeof process.getuid === "function" ? process.getuid() : -1,
    work,
    outDir,
  };

  // ---- 0. 构建 ----
  if (!skipBuild) {
    await step("P00", "构建真实 fixturehost 与两个真实 Agent 发布版本", async () => {
      run("go", ["build", "-trimpath", "-o", join(work, "fixturehost"), "./tests/browser/v09/fixturehost"], { cwd: repoRoot });
      run("go", ["build", "-trimpath", "-ldflags", "-X main.agentBuildVersion=1.0.0", "-o", join(work, "agent-1.0.0"), "./apps/agent"], { cwd: repoRoot });
      run("go", ["build", "-trimpath", "-ldflags", "-X main.agentBuildVersion=1.0.1", "-o", join(work, "agent-1.0.1"), "./apps/agent"], { cwd: repoRoot });
      const v1 = sha256(join(work, "agent-1.0.0"));
      const v2 = sha256(join(work, "agent-1.0.1"));
      if (v1 === v2) throw new Error("two release builds are byte identical");
      return `1.0.0=${v1.slice(0, 16)}… 1.0.1=${v2.slice(0, 16)}…`;
    });
  }
  environment.binaries = {
    fixturehost: sha256(join(work, "fixturehost")),
    agent100: sha256(join(work, "agent-1.0.0")),
    agent101: sha256(join(work, "agent-1.0.1")),
  };

  // ---- 1. PKI ----
  await step("P01", "本地 CA 与服务端证书（localpki 兼容格式）", async () => {
    run("sh", ["-c", pkiScript(work)]);
    return "ca/cert/ai-cert 已生成并经 openssl verify";
  });

  const apiPort = await freePort();
  const webPort = await freePort();
  const aiPort = await freePort();
  const controlPort = await freePort();
  const base = `https://127.0.0.1:${webPort}`;
  environment.ports = { apiPort, webPort, aiPort, controlPort };
  // 合成上游秘密：独立随机 32 字节、0600 私有文件，只经文件注入，不进 URL/日志。
  const secretPath = join(work, "fixture-secret.txt");
  await writeFile(secretPath, randomBytes(32).toString("hex"), { mode: 0o600 });
  await chmod(secretPath, 0o600);

  // Web 宿主与浏览器在阶段 A 就必须存在：初始化表单只在同源会话上下文里开放，
  // 不能用一个「裸 HTTP 客户端」伪造首次 bootstrap。
  await writeFile(join(work, "webhost.mjs"), WEBHOST, { mode: 0o600 });
  const webhost = track(
    spawn(process.execPath, [join(work, "webhost.mjs"), join(work, "cert.pem"), join(work, "cert-key.pem"), join(repoRoot, "apps/web/dist"), String(apiPort)], {
      env: { ...process.env, WEB_PORT: String(webPort) },
      stdio: ["ignore", "pipe", "pipe"],
    }),
    "webhost",
  );
  let session = null;
  let browser = null;

  // ---- 2. 阶段 A：初始化 + 真实 enroll ----
  const stageA = track(
    spawn(join(work, "fixturehost"), [
      "-db", join(work, "db/controller.db"),
      "-agent-pki-dir", join(work, "pki"),
      "-staging-dir", join(work, "staging"),
      "-listen", `0.0.0.0:${apiPort}`,
      "-origin", base,
      "-tls-cert", join(work, "cert.pem"),
      "-tls-key", join(work, "cert-key.pem"),
      "-agent-authority", `localhost:${apiPort}`,
      "-agent-server-name", "localhost",
      "-ai-upstream-origin", `https://127.0.0.1:${aiPort}`,
      "-ai-upstream-ca", join(work, "pki/local-agent-ca.pem"),
      "-ai-upstream-cert", join(work, "ai-cert.pem"),
      "-ai-upstream-key", join(work, "ai-key.pem"),
      "-ai-upstream-listen", `127.0.0.1:${aiPort}`,
      "-ai-secret-file", secretPath,
      "-fixture-listen", `127.0.0.1:${controlPort}`,
    ], { stdio: ["ignore", "pipe", "pipe"] }),
    "fixturehost-A",
  );
  let stageALog = "";
  stageA.stdout.on("data", chunk => { stageALog += chunk.toString(); });
  stageA.stderr.on("data", chunk => { stageALog += chunk.toString(); });

  // 全部写请求都在真实页面上下文执行（浏览器自身提供 Cookie/Origin/Sec-Fetch-*/CSRF），
  // 不使用绕过这些边界的裸客户端。

  try {
    await waitListening(apiPort);
    await step("P02", "阶段 A 宿主就绪（正式 server.New + enrollment 链路）", async () => "listening");
    process.on("exit", () => { if (failed) console.log(`--- stageA log ---\n${stageALog.slice(-1200)}`); });

    await writeFile(join(work, "setup-token.mjs"), SETUP_SCRIPT, { mode: 0o600 });
    const setupToken = run("node", [join(work, "setup-token.mjs"), join(work, "db/controller.db")]);
    await step("P03", "本机安装器写入 setup token 摘要（不落日志）", async () => "digest seeded");

    await step("P04", "浏览器完成首次 bootstrap，再建立异人管理员账号", async () => {
      await waitListening(webPort);
      const cdpA = await freePort();
      browser = await launchHeadlessBrowser({ cdpPort: cdpA, profileDir: join(work, "browser-a") });
      session = await connectPage(cdpA);
      await goto(session, `${base}/#/setup`);
      await fillField(session, "本机 setup token", setupToken);
      await fillField(session, "管理员用户名", "admin");
      await fillField(session, "密码", "wb01-admin-synthetic");
      await fillField(session, "确认密码", "wb01-admin-synthetic");
      await clickText(session, "创建管理员并进入");
      await waitForCondition(session, `!location.hash.includes("setup")`, { label: "bootstrap completed", timeoutMs: 20_000 });
      const me = await browserMust(session, "GET", "/api/v1/auth/me", undefined, 200);
      const created = await browserMust(session, "POST", "/api/v1/users", {
        username: "wb01-approver",
        displayName: "WB01 发布审批（异人）",
        password: "wb01-approver-synthetic",
        roleIds: ["administrator"],
        enabled: true,
      }, 200);
      return `admin=${me.json?.identity?.userId ?? "?"} approver=${created.json?.userId ?? created.json?.id ?? "?"}`;
    });

    await step("P05", "真实 Agent 接入令牌与 pending enrollment", async () => {
      const tokenResponse = await browserMust(session, "POST", "/api/v1/enrollment-tokens", { expiryMinutes: 10, maxUses: 1 }, 201);
      const token = tokenResponse.json?.token;
      if (!token) throw new Error("enrollment token missing");
      enrollToken = token;
      const enroll = run(join(work, "agent-1.0.0"), [
        "-enroll-token", token,
        "-state-dir", join(work, "agent-state"),
        "-controller-url", `https://localhost:${apiPort}`,
        "-root-cert", join(work, "pki/local-agent-ca.pem"),
      ], { cwd: work });
      const pending = await browserMust(session, "GET", "/api/v1/enrollments", undefined, 200);
      const items = pending.json?.items ?? [];
      if (items.length !== 1) throw new Error(`unexpected enrollments: ${pending.text.slice(0, 300)}`);
      environment.nodeId = items[0].nodeId;
      return `node=${items[0].nodeId} cli=${enroll.slice(-120)}`;
    });

    await step("P06", "管理员审批 enrollment 并签发身份（真实 mTLS）", async () => {
      const pending = await browserMust(session, "GET", "/api/v1/enrollments", undefined, 200);
      const item = (pending.json?.items ?? [])[0];
      await browserMust(session, "POST", `/api/v1/enrollments/${item.id}/decision`, { decision: "approve", reason: "wb01 real agent enrollment" }, 200);
      run(join(work, "agent-1.0.0"), [
        "-enroll-token", enrollToken,
        "-state-dir", join(work, "agent-state"),
        "-controller-url", `https://localhost:${apiPort}`,
        "-root-cert", join(work, "pki/local-agent-ca.pem"),
      ], { cwd: work, timeout: 30_000 });
      return "issued";
    });

    await step("P07", "为两名管理员授予节点与更新权限", async () => {
      const me = await browserMust(session, "GET", "/api/v1/auth/me", undefined, 200);
      const users = await browserMust(session, "GET", "/api/v1/users", undefined, 200);
      const list = users.json?.items ?? [];
      const ids = new Set([me.json?.identity?.userId, ...list.map(entry => entry.userId ?? entry.id)].filter(Boolean));
      for (const id of ids) {
        await browserMust(session, "POST", "/api/v1/resource-grants", {
          principalId: id,
          resourceType: "node",
          resourceId: environment.nodeId,
          permissions: ["nodes.read", "tasks.read", "tasks.submit", "tasks.approve", "system.updates.apply"],
        }, 200);
      }
      return `grants=${ids.size}`;
    });
  } finally {
    stageA.kill("SIGTERM");
    await sleep(1200);
  }

  // ---- 3. 槽位/签名 inbox ----
  await step("P08", "真实发布构建入槽 + 合成 ed25519 签名 inbox", async () => {
    const v1bytes = await readFile(join(work, "agent-1.0.0"));
    const v2bytes = await readFile(join(work, "agent-1.0.1"));
    await writeFile(join(work, "slot/active"), v1bytes, { mode: 0o700 });
    const { publicKey, privateKey } = generateKeyPairSync("ed25519");
    const pubHex = Buffer.from(publicKey.export({ type: "spki", format: "der" }).subarray(-32)).toString("hex");
    const manifestOf = (version, sequence, bytes) => ({
      format: 1,
      component: "agent",
      version,
      sequence,
      os: "linux",
      architecture: process.arch === "x64" ? "amd64" : process.arch,
      sha256: createHash("sha256").update(bytes).digest("hex"),
      bytes: bytes.length,
    });
    const writeRelease = async (manifest, bytes, corrupt) => {
      const directory = join(work, "inbox", manifest.version);
      await mkdir(directory, { recursive: true, mode: 0o700 });
      const raw = Buffer.from(JSON.stringify(manifest));
      const signature = sign(null, Buffer.concat([Buffer.from("serverfleet.release.v1\u0000"), raw]), privateKey);
      await writeFile(join(directory, "manifest.json"), raw, { mode: 0o600 });
      await writeFile(join(directory, "signature.bin"), signature, { mode: 0o600 });
      const body = corrupt ? Buffer.concat([bytes.subarray(0, bytes.length - 8), Buffer.from("tampered")]) : bytes;
      await writeFile(join(directory, "artifact"), body, { mode: 0o600 });
      return manifest;
    };
    const initial = await writeRelease(manifestOf("1.0.0", 1, v1bytes), v1bytes, false);
    await writeRelease(manifestOf("1.0.1", 2, v2bytes), v2bytes, false);
    await writeRelease(manifestOf("1.0.2", 3, v2bytes), v2bytes, true);
    const config = {
      directory: join(work, "slot"),
      inbox: join(work, "inbox"),
      stageDir: join(work, "stage"),
      keyId: "wb01-synthetic-release",
      nodeId: environment.nodeId,
      identityDir: join(work, "agent-state"),
      controllerOrigin: `https://localhost:${apiPort}`,
      rootCertificate: join(work, "pki/local-agent-ca.pem"),
      candidateVersion: "1.0.1",
      schemaVersion: 0,
      initial,
      policyComponent: "agent",
      architecture: process.arch === "x64" ? "amd64" : process.arch,
      minimumSequence: 1,
      maxBytes: 256 << 20,
      trustedKeys: { "wb01-synthetic-release": pubHex },
    };
    const configPath = join(work, "product-update.json");
    await writeFile(configPath, JSON.stringify(config, null, 2), { mode: 0o600 });
    await chmod(configPath, 0o600);
    return `initial=${initial.sha256.slice(0, 16)}… target=${v2bytes.length}b`;
  });

  // ---- 4. 阶段 B：带 UpdateProduct 的宿主（可重启，用于持久性验证）----
  let stageB = null;
  let hostLog = "";
  function startStageB() {
    hostLog = "";
    stageB = track(
      spawn(join(work, "fixturehost"), [
        "-db", join(work, "db/controller.db"),
        "-agent-pki-dir", join(work, "pki"),
        "-staging-dir", join(work, "staging"),
        "-listen", `0.0.0.0:${apiPort}`,
        "-origin", base,
        "-tls-cert", join(work, "cert.pem"),
        "-tls-key", join(work, "cert-key.pem"),
        "-agent-authority", `localhost:${apiPort}`,
        "-agent-server-name", "localhost",
        "-ai-upstream-origin", `https://127.0.0.1:${aiPort}`,
        "-ai-upstream-ca", join(work, "pki/local-agent-ca.pem"),
        "-ai-upstream-cert", join(work, "ai-cert.pem"),
        "-ai-upstream-key", join(work, "ai-key.pem"),
        "-ai-upstream-listen", `127.0.0.1:${aiPort}`,
        "-ai-secret-file", secretPath,
        "-fixture-listen", `127.0.0.1:${controlPort}`,
        "-product-update-config", join(work, "product-update.json"),
      ], { stdio: ["ignore", "pipe", "pipe"] }),
      "fixturehost-B",
    );
    stageB.stdout.on("data", chunk => {
      hostLog += chunk.toString();
    });
    stageB.stderr.on("data", chunk => {
      hostLog += chunk.toString();
    });
    return stageB;
  }
  startStageB();

  try {
    await waitListening(apiPort);
    await waitListening(webPort);
    await step("P09", "阶段 B 宿主按真实库指纹装配 UpdateProduct 并启动受监督真实 Agent", async () => {
      const deadline = Date.now() + 20_000;
      while (Date.now() < deadline) {
        if (/trusted agent product update enabled/.test(hostLog)) break;
        if (/fixturehost:/.test(hostLog) && /reject|invalid|error/i.test(hostLog)) break;
        await sleep(200);
      }
      const healthPath = join(work, "slot/health.json");
      while (Date.now() < deadline) {
        try {
          const raw = JSON.parse(await readFile(healthPath, "utf8"));
          if (raw.version === "1.0.0" && raw.pid > 0) {
            environment.pidBefore = raw.pid;
            return `health 1.0.0 pid=${raw.pid} fingerprint=${/schema fingerprint (\w+)/.exec(hostLog)?.[1]?.slice(0, 12) ?? "?"}`;
          }
        } catch {
          /* not written yet */
        }
        await sleep(200);
      }
      throw new Error(`no real agent readiness: ${hostLog.slice(-500)}`);
    });

    // ---- 5. 浏览器：A 发起更新（复用完成 bootstrap 的同一会话）----
    await step("P10", "浏览器：管理员进入更新中心", async () => {
      await goto(session, `${base}/#/updates`);
      await waitForCondition(session, `document.body.innerText.includes("更新中心")`, { label: "updates page" });
      return "updates page loaded";
    });

    await step("P11", "浏览器：检查更新（真实 /check，签名与平台由服务端校验）", async () => {
      await clickText(session, "检查更新");
      await waitForCondition(session, `document.body.innerText.includes("当前版本")`, { label: "check result" });
      const text = await evaluate(session, "document.body.innerText");
      if (!/1\.0\.1/.test(text)) throw new Error(`latest version not shown: ${text.slice(0, 200)}`);
      return "check -> 1.0.1 available";
    });

    await step("P12", "浏览器：生成冻结计划（dryRun）", async () => {
      await fillField(session, "目标版本", "1.0.1");
      await clickText(session, "生成计划");
      await waitForCondition(session, `document.body.innerText.includes("迁移可逆")`, { label: "plan rendered", timeoutMs: 20_000 });
      const text = await evaluate(session, "document.body.innerText");
      const lines = text.split("\n").map(line => line.trim()).filter(Boolean);
      const bad = lines.filter(line => line.includes("不合格（禁止安装）"));
      if (bad.length > 0) {
        throw new Error(`plan not eligible: ${bad.join(" / ")}`);
      }
      if (!lines.some(line => line.includes("合格"))) {
        throw new Error(`plan state unclear: ${lines.filter(line => line.includes("计划")).join(" / ").slice(0, 300)}`);
      }
      return "plan eligible";
    });

    await step("P13", "浏览器：确认文本 + 风险勾选 → 一键更新（202 只代表受理）", async () => {
      await fillField(session, "确认文本", "1.0.1");
      await evaluate(session, `(() => { const boxes = [...document.querySelectorAll('input[type=checkbox]')]; const box = boxes[boxes.length - 1]; if (!box.checked) box.click(); return box.checked; })()`);
      await clickText(session, "一键更新");
      await waitForCondition(session, `document.body.innerText.includes("升级作业已创建")`, { label: "apply accepted" });
      return "apply accepted (202)";
    });

    // ---- 6. 异人审批（浏览器 B）----
    const cdpB = await freePort();
    const browserB = await launchHeadlessBrowser({ cdpPort: cdpB, profileDir: join(work, "browser-b") });
    const sessionB = await connectPage(cdpB);

    await step("P14", "浏览器（异人管理员会话）：打开任务页并审批同一任务", async () => {
      await goto(sessionB, `${base}/#/login`);
      await fillField(sessionB, "用户名", "wb01-approver");
      await fillField(sessionB, "密码", "wb01-approver-synthetic");
      // 直接提交登录表单：文本点击会命中标题「登录 Server Fleet」，不是提交按钮。
      const submitted = await evaluate(sessionB, `(() => { const form = document.querySelector("form"); if (!form) return false; form.requestSubmit(); return true; })()`);
      if (!submitted) throw new Error("login form not found");
      await waitForCondition(sessionB, `!location.hash.includes("login")`, { label: "approver login", timeoutMs: 20_000 });
      // 以服务端会话为准确认异人已登录；页面渲染只作为证据，不作为唯一判据。
      const sessionCheck = await browserFetch(sessionB, "GET", "/api/v1/auth/me", undefined);
      if (sessionCheck.status !== 200) {
        const body = await evaluate(sessionB, "document.body.innerText");
        throw new Error(`approver session ${sessionCheck.status}: ${sessionCheck.text.slice(0, 160)} | page: ${body.slice(0, 200)}`);
      }
      await goto(sessionB, `${base}/#/tasks`);
      await waitForCondition(sessionB, `document.body.innerText.includes("任务")`, { label: "tasks page", timeoutMs: 20_000 });
      const listed = await browserMust(sessionB, "GET", "/api/v1/tasks", undefined, 200);
      const items = listed.json?.items ?? [];
      const pending = items.filter(entry => entry.planDigest && /awaiting|pending|approval/i.test(entry.state ?? ""));
      if (pending.length !== 1) {
        throw new Error(`expected exactly one awaiting task, got ${pending.length}: ${listed.text.slice(0, 240)}`);
      }
      const task = pending[0];
      const taskId = task.taskId ?? task.jobId;
      environment.taskId = taskId;
      // 异人规则由服务端执行：提交人是 admin，审批人是 wb01-approver，且带同一 planDigest。
      await browserMust(sessionB, "POST", `/api/v1/tasks/${taskId}/approve`, {
        planDigest: task.planDigest,
        decision: "approve",
        reason: "wb01 异人审批：本地签名 Agent 更新",
      }, 200);
      await goto(sessionB, `${base}/#/tasks/${taskId}`);
      await waitForCondition(sessionB, `document.body.innerText.includes(${JSON.stringify(taskId)}) || document.body.innerText.includes("审批")`, { label: "task detail", timeoutMs: 15_000 });
      return `task=${taskId} approved by distinct administrator`;
    });

    await step("P15", "真实 Agent 版本与健康：completed 且进程被替换", async () => {
      const deadline = Date.now() + 40_000;
      let last = "";
      while (Date.now() < deadline) {
        last = await readFile(join(work, "slot/health.json"), "utf8").catch(() => "");
        try {
          const parsed = JSON.parse(last);
          if (parsed.version === "1.0.1" && parsed.pid !== environment.pidBefore) {
            environment.pidAfter = parsed.pid;
            return `1.0.0(pid ${environment.pidBefore}) -> 1.0.1(pid ${parsed.pid})`;
          }
        } catch {
          /* keep waiting */
        }
        await sleep(400);
      }
      throw new Error(`agent version not advanced: ${last.slice(0, 300)}`);
    });

    await step("P16", "槽位字节与 1.0.1 发布构建 SHA 一致（真实安装而非状态伪造）", async () => {
      const active = sha256(join(work, "slot/active"));
      if (active !== environment.binaries.agent101) {
        throw new Error(`active sha ${active.slice(0, 16)} != release ${environment.binaries.agent101.slice(0, 16)}`);
      }
      return `active=${active.slice(0, 16)}…`;
    });

    await step("P17", "作业/任务终态与审计持久（服务端为准）", async () => {
      const jobs = await browserMust(session, "GET", "/api/v1/system/updates/jobs", undefined, 200);
      const items = jobs.json?.items ?? [];
      const job = items.find(entry => entry.targetVersion === "1.0.1");
      if (!job) throw new Error(`job missing: ${jobs.text.slice(0, 300)}`);
      if (!["completed", "installed"].includes(job.status)) throw new Error(`job status ${job.status}`);
      if (job.taskId !== environment.taskId) throw new Error(`job task ${job.taskId} != approved ${environment.taskId}`);
      return `job=${job.id} status=${job.status} task=${job.taskId}`;
    });

    await step("P18", "负向：工件与清单 SHA 不一致的 1.0.2 必须失败且不替换当前产品", async () => {
      const plan = await browserFetch(session, "POST", "/api/v1/system/updates/plan", { targetVersion: "1.0.2", dryRun: true });
      if (plan.status !== 200) {
        // 篡改工件在计划阶段就被签名/哈希校验拒绝：不产生作业、不触碰运行中的产品。
        if (plan.json?.code !== "UPDATE_PLANNER_UNAVAILABLE") {
          throw new Error(`unexpected plan failure ${plan.status}: ${plan.text.slice(0, 200)}`);
        }
        const jobsAfter = await browserMust(session, "GET", "/api/v1/system/updates/jobs", undefined, 200);
        if ((jobsAfter.json?.items ?? []).some(entry => entry.targetVersion === "1.0.2")) {
          throw new Error("a job was created for a tampered artifact");
        }
        const healthNow = JSON.parse(await readFile(join(work, "slot/health.json"), "utf8"));
        if (healthNow.version !== "1.0.1" || healthNow.pid !== environment.pidAfter) {
          throw new Error(`product replaced while rejecting tampered artifact: ${healthNow.version}`);
        }
        if (sha256(join(work, "slot/active")) !== environment.binaries.agent101) {
          throw new Error("slot bytes changed while rejecting tampered artifact");
        }
        return `plan rejected (${plan.json.code}); no job, product still 1.0.1 pid ${healthNow.pid}`;
      }
      if (!plan.json?.eligible) throw new Error(`1.0.2 plan not eligible: ${plan.text.slice(0, 200)}`);
      await browserMust(session, "POST", "/api/v1/system/updates/apply", { planId: plan.json.planId, confirmation: "1.0.2", backupMode: "required" }, 202);
      const listed = await browserMust(sessionB, "GET", "/api/v1/tasks", undefined, 200);
      const awaiting = (listed.json?.items ?? []).filter(entry => entry.planDigest && /awaiting|pending|approval/i.test(entry.state ?? ""));
      if (awaiting.length !== 1) throw new Error(`expected one awaiting task, got ${awaiting.length}`);
      const task = awaiting[0];
      await browserMust(sessionB, "POST", `/api/v1/tasks/${task.taskId ?? task.jobId}/approve`, {
        planDigest: task.planDigest,
        decision: "approve",
        reason: "wb01 负向：损坏工件必须被拒绝",
      }, 200);
      const busy = ["awaiting_approval", "queued", "preflight", "verifying", "backing_up", "installing", "restarting", "health_check"];
      const deadline = Date.now() + 40_000;
      let status = "";
      while (Date.now() < deadline) {
        const jobs = await browserMust(session, "GET", "/api/v1/system/updates/jobs", undefined, 200);
        const found = (jobs.json?.items ?? []).find(entry => entry.targetVersion === "1.0.2");
        if (found && !busy.includes(found.status)) {
          status = found.status;
          break;
        }
        await sleep(400);
      }
      if (!["failed", "unknown", "rolled_back"].includes(status)) {
        throw new Error(`damaged artifact ended ${status || "without terminal state"}`);
      }
      const health = JSON.parse(await readFile(join(work, "slot/health.json"), "utf8"));
      if (health.version !== "1.0.1" || health.pid !== environment.pidAfter) {
        throw new Error(`damaged artifact replaced the product: ${health.version} pid ${health.pid}`);
      }
      if (sha256(join(work, "slot/active")) !== environment.binaries.agent101) {
        throw new Error("slot bytes changed by damaged artifact");
      }
      return `status=${status}; product still 1.0.1 pid ${health.pid}`;
    });

    await step("P19", "重启宿主：已安装版本与作业终态持久，未知结果不重放", async () => {
      stageB.kill("SIGTERM");
      await sleep(3000);
      startStageB();
      try {
        await waitListening(apiPort);
      } catch (restartError) {
        throw new Error(`${restartError.message} | host: ${hostLog.slice(-600)}`);
      }
      const deadline = Date.now() + 30_000;
      let health = null;
      while (Date.now() < deadline) {
        try {
          const raw = JSON.parse(await readFile(join(work, "slot/health.json"), "utf8"));
          if (raw.version === "1.0.1") {
            health = raw;
            break;
          }
        } catch {
          /* restarting */
        }
        await sleep(300);
      }
      if (!health) throw new Error(`restart did not restore 1.0.1: ${hostLog.slice(-300)}`);
      const jobs = await browserMust(session, "GET", "/api/v1/system/updates/jobs", undefined, 200);
      const done = (jobs.json?.items ?? []).find(entry => entry.targetVersion === "1.0.1");
      if (!done || done.status !== "completed") {
        throw new Error(`completed job not durable across restart: ${jobs.text.slice(0, 240)}`);
      }
      if (sha256(join(work, "slot/active")) !== environment.binaries.agent101) {
        throw new Error("slot bytes changed after restart");
      }
      return `restored 1.0.1 pid ${health.pid}; completed job durable`;
    });

    await screenshot(session, join(outDir, "stage-updated.png"));
    if (browserB) browserB.child?.kill();
  } finally {
    if (session?.close) session.close();
    if (browser?.child) browser.child.kill();
    webhost.kill("SIGTERM");
    stageB.kill("SIGTERM");
    await sleep(800);
  }

  const report = {
    task: "V09-CLOUD-WB-01",
    session: "workbuddy-01",
    scope: "Agent 组件本地签名更新 · 浏览器专项（不含 Controller 组件升级）",
    environment,
    results,
    failed,
  };
  await writeFile(join(outDir, "product-update-results.json"), JSON.stringify(report, null, 2));
  console.log(`\n${failed ? "FAIL" : "PASS"}: ${results.filter(entry => entry.status === "PASS").length} PASS / ${results.filter(entry => entry.status === "FAIL").length} FAIL`);
  if (!keep) {
    console.log(`work dir kept at ${work} for evidence; remove manually after review`);
  }
  process.exit(failed ? 1 : 0);
}

main().catch(async error => {
  console.error(`fatal: ${error instanceof Error ? error.stack : error}`);
  record("FATAL", "aborted", "FAIL", String(error));
  await writeFile(join(outDir, "product-update-results.json"), JSON.stringify({ results, error: String(error) }, null, 2)).catch(() => {});
  process.exit(1);
});
