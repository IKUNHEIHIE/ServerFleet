#!/usr/bin/env node
/**
 * V09-CLOUD-WB-01 · B06 灾难恢复「前置条件探测」
 *
 * 本脚本不是「恢复成功验收」。它做的是把 B06 的阻塞位置精确到接口、状态码与 code，
 * 并证明其中可以被验证的部分确实可用（备份校验真实通过）。
 *
 * 设计约束（与派发一致）：
 * 1. 宿主按生产同构装配：**不注入** server.Config.RecoveryExecute。
 *    注入一个自制的恢复执行器来「跑通」恢复，只会得到自证结论：
 *    验证对象会变成我写的适配器，而不是 AgentMore 的产品实现。
 * 2. 因此本脚本对「实际恢复执行、新权威、旧主控 fence、旧信任代次拒绝」一律不下结论，
 *    只记录产品侧当前返回的确切事实。
 * 3. 备份文件用 SQLite 的 VACUUM INTO 从运行中的真实库生成，是真实一致性备份，
 *    而不是躺在那里的空文件。
 *
 * 观测点：
 *   R05 备份生成（真实 VACUUM INTO + SHA-256）
 *   R06 POST /api/v1/backups/{id}/verify      期望 200 且 verified=true
 *   R07 同上，定点断言 backup_integrity 检查项
 *   R08 POST /api/v1/disaster-recovery/preview 期望 200 + eligible=false + 精确检查项
 *   R09 POST /api/v1/disaster-recovery/jobs    期望 501 RECOVERY_EXECUTOR_UNAVAILABLE
 *   R10 GET  /api/v1/disaster-recovery/jobs/{id} 期望 404 RECOVERY_JOB_NOT_FOUND
 *   R11 未知备份 → 404 BACKUP_NOT_FOUND（负向）
 *   R12 未认证会话 → 403 NOT_AUTHORIZED（负向）
 *   R13 页面实况
 *
 * 分类规则（R08–R10）：
 *   只有「HTTP 状态 + 错误码/problem 结构 + 检查项集合」与下方常量精确匹配时才记 BLOCKED。
 *   预览还必须满足：必需成功项（backup_integrity、recovery_key_reference）全部 passed=true，
 *   且失败项集合恰好是已知两项、reason 逐字相同；少一项、多一项、重复、未知项、结构或
 *   reason 漂移都记 FAIL。任何偏差——包括 500、401、403、其他 code、错误 shape——一律记
 *   FAIL 并以 1 退出。
 *   产品行为若已变更（例如执行器被装配，501 变成 202），同样记 FAIL 并标注「行为已变更」：
 *   那意味着 B06 的结论必须重新评估，不能静默沿用旧的 BLOCKED。
 *
 * 模式：
 *   默认 = 诊断模式（diagnostic）。BLOCKED 不影响退出码，但**不代表完整验收**，输出与
 *   JSON 证据都会显式标注。加 --require-complete 进入严格完整模式：任何 BLOCKED 都使
 *   退出码为 1，只有全部 PASS 才算通过。
 */
import { execFileSync, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  bodyText,
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
const outDir = resolve(value("--out", join(repoRoot, "reports/wb01-recovery-probe")));
const keep = opt("--keep");
const skipBuild = opt("--skip-build");
// 严格完整模式：BLOCKED 使退出码为 1。默认诊断模式保留可读性，但不构成完整验收。
const requireComplete = opt("--require-complete");
const mode = requireComplete ? "complete" : "diagnostic";
const work = join(homedir() || tmpdir(), "sf-wb01-recovery-probe");
const base = () => `https://127.0.0.1:${env.ports.webPort}`;

const env = { ports: {}, ids: {} };
let backupSha = "";
const results = [];
const children = [];

function record(id, title, status, detail, extra = {}) {
  // extra 展开在前：调用方附加的诊断字段绝不能覆盖判定本身（曾因 extra.status 覆盖 status
  // 把 FAIL 记录成 HTTP 状态码，导致汇总与退出码全部失真）。
  const entry = { ...extra, id, title, status, detail };
  results.push(entry);
  const line = `[${status}] ${id} ${title} :: ${detail}`;
  console.log(line);
  if (status === "FAIL") process.exitCode = 1;
}

async function step(id, title, run) {
  try {
    const detail = await run();
    record(id, title, "PASS", detail ?? "ok");
    return detail;
  } catch (error) {
    record(id, title, "FAIL", error?.message ?? String(error));
    throw error;
  }
}

/**
 * 观测到「产品未实现」时用此记录。**只有** status/code/shape/检查项全部精确匹配才允许走到这里：
 * 前版对任何 HTTP 结果都记 BLOCKED，会把 500/401/403 和错误结构一起伪装成「阻塞」，
 * 从而掩盖真实回归。现在唯一入口是 probeBlocked()，不匹配一律 FAIL。
 */
function blocked(id, title, detail, extra = {}) {
  const entry = { ...extra, id, title, status: "BLOCKED", detail };
  results.push(entry);
  console.log(`[BLOCKED] ${id} ${title} :: ${detail}`);
}

/** RFC7807 problem 响应：HTTP 状态、content-type、type/title/status/code 与泄露面全部要核对。 */
function problemMismatches(response, expectedStatus, expectedCode) {
  const problems = [];
  if (response.status !== expectedStatus) problems.push(`HTTP ${response.status} != ${expectedStatus}`);
  const mediaType = String(response.contentType ?? "").split(";")[0].trim().toLowerCase();
  if (mediaType !== "application/problem+json") {
    problems.push(`content-type=${mediaType || "?"} != application/problem+json`);
  }
  const body = response.json;
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    problems.push(`body 不是 JSON 对象: ${String(response.text).slice(0, 160)}`);
    return problems;
  }
  if (body.type !== "about:blank") problems.push(`body.type=${JSON.stringify(body.type)} != "about:blank"`);
  if (body.status !== expectedStatus) problems.push(`body.status=${JSON.stringify(body.status)} != ${expectedStatus}`);
  if (body.code !== expectedCode) {
    problems.push(`body.code=${JSON.stringify(body.code)} != ${JSON.stringify(expectedCode)}`);
  }
  if (typeof body.title !== "string" || body.title === "") problems.push("body.title 缺失");
  for (const field of ["checks", "plan", "job", "backupId", "path"]) {
    if (field in body) problems.push(`错误响应不应包含字段 ${field}`);
  }
  if (/sqlite|\/backups\/|\.db/i.test(String(response.text ?? ""))) {
    problems.push("响应文本泄露路径/SQLite 细节");
  }
  return problems;
}

/**
 * 预览响应必须证明的精确事实。
 *
 * 只有「必需项全部通过」且「失败项集合恰好是已知两项、原因逐字相符」时才允许记 BLOCKED。
 * 只看两个已知项是否存在、原因是否正确是不够的：`recovery_key_reference` 失败或缺失时
 * 旧实现仍返回空 mismatches，会把「恢复密钥损坏」这类新的真实阻塞当成旧的未装配结论，
 * 从而掩盖回归（审查 WB-R2-01 的复现口径）。
 *
 * 因此这里做四层核对：
 *   1. 检查项名字集合与 PREVIEW_CHECK_NAMES 完全相等（少一项、多一项都不行）；
 *   2. 结构合法：name 非空字符串、passed 布尔、reason 只允许字符串、无未知字段、无重复；
 *   3. 必需成功项（backup_integrity / recovery_key_reference）必须 passed=true；
 *   4. 实际失败项集合必须恰好等于 KNOWN_PREVIEW_FAILURES 的两项，且 reason 精确匹配。
 */
const PREVIEW_CHECK_NAMES = [
  "backup_integrity",
  "target_controller_identity",
  "old_controller_fenced",
  "recovery_key_reference",
];
const PREVIEW_REQUIRED_PASSING = ["backup_integrity", "recovery_key_reference"];
const KNOWN_PREVIEW_FAILURES = [
  { name: "target_controller_identity", reason: "target_identity_not_observed" },
  { name: "old_controller_fenced", reason: "fencing_evidence_unavailable" },
];
const CHECK_FIELDS = ["name", "passed", "reason"];

function previewMismatches(response) {
  if (response.status !== 200) return [`HTTP ${response.status} != 200`];
  const body = response.json;
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return [`body 不是 JSON 对象: ${String(response.text).slice(0, 160)}`];
  }
  const problems = [];
  if (body.eligible !== false) problems.push(`eligible=${JSON.stringify(body.eligible)} != false`);
  if (body.verified !== true) problems.push(`verified=${JSON.stringify(body.verified)} != true`);
  if (!Array.isArray(body.checks) || body.checks.length === 0) {
    problems.push("checks 不是非空数组");
    return problems;
  }

  // 结构与重复：非法项不进入名字索引，避免用坏数据继续做集合比较。
  const byName = new Map();
  for (const [index, check] of body.checks.entries()) {
    if (!check || typeof check !== "object" || Array.isArray(check)) {
      problems.push(`checks[${index}] 不是对象: ${JSON.stringify(check).slice(0, 120)}`);
      continue;
    }
    const unknown = Object.keys(check).filter(field => !CHECK_FIELDS.includes(field));
    if (unknown.length > 0) problems.push(`checks[${index}] 含未知字段 ${unknown.join(",")}`);
    if (typeof check.name !== "string" || check.name === "") {
      problems.push(`checks[${index}].name 缺失或非字符串`);
      continue;
    }
    if (typeof check.passed !== "boolean") {
      problems.push(`${check.name}.passed=${JSON.stringify(check.passed)} 不是布尔`);
      continue;
    }
    if (check.reason !== undefined && typeof check.reason !== "string") {
      problems.push(`${check.name}.reason 不是字符串: ${JSON.stringify(check.reason)}`);
      continue;
    }
    const seen = byName.get(check.name) ?? [];
    seen.push(check);
    byName.set(check.name, seen);
  }

  for (const [name, entries] of byName) {
    if (entries.length > 1) problems.push(`检查项重复: ${name}（${entries.length} 次）`);
  }
  const present = [...byName.keys()];
  const missing = PREVIEW_CHECK_NAMES.filter(name => !byName.has(name));
  const unexpected = present.filter(name => !PREVIEW_CHECK_NAMES.includes(name));
  for (const name of missing) problems.push(`缺少必需检查项 ${name}`);
  for (const name of unexpected) problems.push(`出现未知检查项 ${name}`);

  const first = name => byName.get(name)?.[0];

  // 必需成功项：失败或缺失都意味着前置条件已变化，不能沿用旧的「仅两项未装配」结论。
  for (const name of PREVIEW_REQUIRED_PASSING) {
    const got = first(name);
    if (!got) continue;
    if (got.passed !== true) {
      problems.push(`${name} 未通过（必需成功项）：reason=${JSON.stringify(got.reason ?? "")}`);
    } else if ((got.reason ?? "") !== "") {
      problems.push(`${name} 通过却带 reason=${JSON.stringify(got.reason)}`);
    }
  }

  // 已知失败项：必须存在、passed=false、reason 逐字相符。
  for (const known of KNOWN_PREVIEW_FAILURES) {
    const got = first(known.name);
    if (!got) continue;
    if (got.passed !== false) problems.push(`${known.name}.passed=${JSON.stringify(got.passed)} != false`);
    else if ((got.reason ?? "") !== known.reason) {
      problems.push(`${known.name}.reason=${JSON.stringify(got.reason)} != ${JSON.stringify(known.reason)}`);
    }
  }

  // 失败集合必须恰好两项：多一项、少一项（漏报）都要 FAIL。
  const failedNow = [...byName.entries()]
    .filter(([, entries]) => entries.some(entry => entry.passed === false))
    .map(([name]) => name)
    .sort();
  const failedExpected = KNOWN_PREVIEW_FAILURES.map(item => item.name).sort();
  if (JSON.stringify(failedNow) !== JSON.stringify(failedExpected)) {
    problems.push(
      `实际失败项集合=[${failedNow.join(", ")}] != 已知两项 [${failedExpected.join(", ")}]`,
    );
  }
  return problems;
}

/**
 * BLOCKED 的唯一入口。probe() 返回 {label, mismatches, detail, extra}：
 * mismatches 为空才记 BLOCKED；否则记 FAIL（退出码 1）。label 区分两种成因：
 *   「非预期响应」——回归或环境异常；「行为已变更」——产品可能已实现，B06 结论需重评。
 */
async function probeBlocked(id, title, probe) {
  let outcome;
  try {
    outcome = await probe();
  } catch (error) {
    record(id, title, "FAIL", `探针自身失败: ${error?.message ?? error}`);
    return;
  }
  if (outcome.mismatches.length === 0) {
    blocked(id, title, outcome.detail, outcome.extra ?? {});
    return;
  }
  record(id, title, "FAIL", `${outcome.label}: ${outcome.mismatches.join("; ")}`, outcome.extra ?? {});
}

function sleep(ms) {
  return new Promise(resolveSleep => setTimeout(resolveSleep, ms));
}

function run(command, argv, options = {}) {
  const child = spawn(command, argv, { stdio: ["ignore", "pipe", "pipe"], ...options });
  let stdout = "";
  let stderr = "";
  child.stdout?.on("data", chunk => {
    stdout += chunk.toString();
  });
  child.stderr?.on("data", chunk => {
    stderr += chunk.toString();
  });
  const done = new Promise((resolveDone, rejectDone) => {
    child.on("error", rejectDone);
    child.on("close", code => {
      if (code === 0) resolveDone(stdout.trim());
      else rejectDone(new Error(`${command} ${argv.join(" ")} exited ${code}: ${stderr.slice(-600)}`));
    });
  });
  return { child, done, text: () => `${stdout}\n${stderr}` };
}

/** 同步执行并带回 stderr：execFileSync 失败时原始错误会被吞掉，导致只看到 "Command failed"。 */
function execText(command, argv, options = {}) {
  try {
    return execFileSync(command, argv, { stdio: ["ignore", "pipe", "pipe"], ...options }).toString();
  } catch (error) {
    const stderr = error?.stderr?.toString() ?? "";
    const stdout = error?.stdout?.toString() ?? "";
    const detail = (stderr || stdout).trim().slice(-600);
    const shape = `code=${error?.code ?? "?"} signal=${error?.signal ?? "none"}`;
    throw new Error(`${command} 失败 (${shape}): ${detail || error.message}`);
  }
}

function track(child, name) {
  children.push({ child, name });
  return child;
}

async function freePort() {
  const net = await import("node:net");
  return new Promise((resolvePort, rejectPort) => {
    const server = net.createServer();
    server.on("error", rejectPort);
    server.listen(0, "127.0.0.1", () => {
      const port = server.address().port;
      server.close(() => resolvePort(port));
    });
  });
}

function sha256(buffer) {
  return createHash("sha256").update(buffer).digest("hex");
}

async function sha256File(path) {
  return sha256(await readFile(path));
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

/** 用 SQLite 在线一致性备份语义生成真实备份文件，并确认它带迁移账本。 */
const BACKUP_SCRIPT = `
import { DatabaseSync } from "node:sqlite";
const [sourcePath, targetPath] = process.argv.slice(2);
const db = new DatabaseSync(sourcePath);
try {
  db.exec("PRAGMA busy_timeout = 8000");
  db.exec(\`VACUUM INTO '\${targetPath.replaceAll("'", "''")}'\`);
} finally { db.close(); }
const verify = new DatabaseSync(targetPath, { readOnly: true });
try {
  const count = verify.prepare("SELECT COUNT(*) AS n FROM sqlite_master WHERE type='table' AND name='schema_migrations'").get().n;
  const integrity = verify.prepare("PRAGMA integrity_check").get().integrity_check;
  if (count !== 1) throw new Error("backup has no migration ledger");
  if (String(integrity).toLowerCase() !== "ok") throw new Error("integrity_check -> " + integrity);
  process.stdout.write(JSON.stringify({ ledger: count, integrity: String(integrity) }));
} finally { verify.close(); }
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
  const lines = [];
  lines.push(`mkdir -p "${workDir}/pki" "${workDir}/db" "${workDir}/logs" "${workDir}/staging"`);
  lines.push(`chmod 700 "${workDir}/staging"`);
  lines.push(`openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "${workDir}/pki/local-agent-ca-key.pem"`);
  lines.push(`openssl req -x509 -new -key "${workDir}/pki/local-agent-ca-key.pem" -sha256 -subj "/CN=Server Fleet Local Agent CA" -days 30 -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign,digitalSignature" -out "${workDir}/pki/local-agent-ca.pem"`);
  lines.push(`openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "${workDir}/cert-key.pem" -subj "/CN=localhost" -out "${workDir}/cert.csr"`);
  lines.push(`printf "subjectAltName=DNS:localhost,IP:127.0.0.1\\nbasicConstraints=CA:FALSE\\nkeyUsage=digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth,clientAuth\\n" > "${workDir}/cert-ext.cnf"`);
  lines.push(`openssl x509 -req -in "${workDir}/cert.csr" -CA "${workDir}/pki/local-agent-ca.pem" -CAkey "${workDir}/pki/local-agent-ca-key.pem" -days 2 -extfile "${workDir}/cert-ext.cnf" -out "${workDir}/cert.pem"`);
  lines.push(`openssl verify -CAfile "${workDir}/pki/local-agent-ca.pem" "${workDir}/cert.pem"`);
  lines.push(`openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "${workDir}/ai-key.pem" -subj "/CN=127.0.0.1" -out "${workDir}/ai.csr"`);
  lines.push(`printf "subjectAltName=IP:127.0.0.1,DNS:localhost\\nbasicConstraints=CA:FALSE\\nkeyUsage=digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth\\n" > "${workDir}/ai-ext.cnf"`);
  lines.push(`openssl x509 -req -in "${workDir}/ai.csr" -CA "${workDir}/pki/local-agent-ca.pem" -CAkey "${workDir}/pki/local-agent-ca-key.pem" -days 2 -extfile "${workDir}/ai-ext.cnf" -out "${workDir}/ai-cert.pem"`);
  lines.push(`openssl verify -CAfile "${workDir}/pki/local-agent-ca.pem" "${workDir}/ai-cert.pem"`);
  return lines.join("\n");
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

/** 在真实页面上下文内发起同源请求：Cookie、Origin、Sec-Fetch-* 与 CSRF 均由浏览器自身提供。 */
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
    return JSON.stringify({ status: response.status, text, contentType: response.headers.get("content-type") || "" });
  })()`;
  const raw = await evaluate(session, script);
  const envelope = JSON.parse(raw);
  let json = null;
  try {
    json = envelope.text ? JSON.parse(envelope.text) : null;
  } catch {
    json = null;
  }
  return { status: envelope.status, text: envelope.text, json, contentType: envelope.contentType ?? "" };
}

function cleanup() {
  for (const entry of children.reverse()) {
    try {
      entry.child.kill("SIGTERM");
    } catch {
      /* 已经退出 */
    }
  }
  // 浏览器不经过 track()，必须显式终止，否则事件循环不退出、脚本被外层 timeout 杀掉。
  for (const instance of [env.browser, env.anonymousBrowser]) {
    try {
      instance?.kill?.("SIGKILL");
    } catch {
      /* 已经退出 */
    }
  }
}

async function main() {
  console.log(
    `[V09-CLOUD-WB-01] B06 前置条件探测 · mode=${mode}` +
      (requireComplete ? "（严格完整模式：BLOCKED 推出非零）" : "（诊断模式：BLOCKED 不推出非零，不代表完整验收）"),
  );
  await mkdir(outDir, { recursive: true });
  await rm(work, { recursive: true, force: true });
  await mkdir(work, { recursive: true });
  await chmod(work, 0o700);

  process.on("exit", cleanup);
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
      cleanup();
      process.exit(1);
    });
  }

  try {
    await step("R00", "环境与身份", async () => {
      const uid = execText("id", ["-u"]).trim();
      const goBin = join(process.env.GOROOT ?? "", "bin/go");
      const goVersion = execText(goBin, ["version"]).trim();
      const nodeVersion = process.version;
      if (uid === "0") throw new Error("root 不是普通用户安全验收：请以非 root 运行");
      const stat = await import("node:fs").then(fs => fs.statSync(work));
      if ((stat.mode & 0o777) !== 0o700) throw new Error(`工作目录权限 ${(stat.mode & 0o777).toString(8)} != 700`);
      return `uid=${uid} node=${nodeVersion} go=${goVersion} work=${work} mode=700`;
    });

    if (!skipBuild) {
      await step("R01", "构建 fixturehost（生产同构宿主）", async () => {
        execText(join(process.env.GOROOT ?? "", "bin/go"), ["build", "-trimpath", "-o", join(work, "fixturehost"), "./tests/browser/v09/fixturehost"], { cwd: repoRoot });
        return `built ${join(work, "fixturehost")}`;
      });
    } else if (!existsSync(join(work, "fixturehost"))) {
      throw new Error("--skip-build 但工作目录没有 fixturehost");
    }

    await step("R02", "私有 PKI 与目录", async () => {
      const script = pkiScript(work);
      execText("bash", ["-c", script], { cwd: work });
      await writeFile(join(work, "fixture-secret.txt"), `wb01-synthetic-secret-${sha256(work).slice(0, 24)}`, { mode: 0o600 });
      await chmod(join(work, "fixture-secret.txt"), 0o600);
      return "TLS 证书 + AI upstream 证书 + 0600 合成 secret";
    });

    const apiPort = await freePort();
    const webPort = await freePort();
    const aiPort = await freePort();
    const controlPort = await freePort();
    env.ports = { apiPort, webPort, aiPort, controlPort };

    await step("R03", "启动宿主：生产同构（不注入 RecoveryExecute）", async () => {
      const host = track(
        spawn(join(work, "fixturehost"), [
          "-db", join(work, "db/controller.db"),
          "-staging-dir", join(work, "staging"),
          "-agent-pki-dir", join(work, "pki-agent"),
          "-agent-authority", `localhost:${apiPort}`,
          "-agent-server-name", "localhost",
          "-listen", `0.0.0.0:${apiPort}`,
          "-origin", base(),
          "-tls-cert", join(work, "cert.pem"),
          "-tls-key", join(work, "cert-key.pem"),
          "-ai-upstream-origin", `https://127.0.0.1:${aiPort}`,
          "-ai-upstream-ca", join(work, "pki/local-agent-ca.pem"),
          "-ai-upstream-cert", join(work, "ai-cert.pem"),
          "-ai-upstream-key", join(work, "ai-key.pem"),
          "-ai-upstream-listen", `127.0.0.1:${aiPort}`,
          "-ai-secret-file", join(work, "fixture-secret.txt"),
          "-fixture-listen", `127.0.0.1:${controlPort}`,
        ], { stdio: ["ignore", "pipe", "pipe"] }),
        "fixturehost",
      );
      let log = "";
      host.stdout.on("data", chunk => {
        log += chunk.toString();
      });
      host.stderr.on("data", chunk => {
        log += chunk.toString();
      });
      try {
        await waitListening(apiPort);
      } catch (error) {
        throw new Error(`${error.message} | host: ${log.slice(-600)}`);
      }
      return `api=${apiPort} web=${webPort} ai=${aiPort} control=${controlPort}`;
    });

    await writeFile(join(work, "webhost.mjs"), WEBHOST, { mode: 0o600 });
    await step("R04", "Web 同源宿主就绪", async () => {
      track(
        spawn(process.execPath, [
          join(work, "webhost.mjs"),
          join(work, "cert.pem"),
          join(work, "cert-key.pem"),
          join(repoRoot, "apps/web/dist"),
          String(apiPort),
        ], { env: { ...process.env, WEB_PORT: String(webPort) }, stdio: ["ignore", "pipe", "pipe"] }),
        "webhost",
      );
      await waitListening(webPort);
      return `https://127.0.0.1:${webPort} → :${apiPort}`;
    });

    const setupToken = await step("R05", "首次 bootstrap 会话（浏览器内完成）", async () => {
            const setupFile = join(work, "setup-token.mjs");
      await writeFile(setupFile, SETUP_SCRIPT, { mode: 0o600 });
      const token = execText(process.execPath, [setupFile, join(work, "db/controller.db")]).trim();
      if (!token) throw new Error("setup token 生成失败");
      env.setupToken = token;

      const cdp = await freePort();
      const browser = await launchHeadlessBrowser({ cdpPort: cdp, profileDir: join(work, "browser-admin") });
      env.browser = browser;
      const session = await connectPage(cdp);
      env.session = session;
      await goto(session, `${base()}/#/setup`);
      await fillField(session, "本机 setup token", token);
      await fillField(session, "管理员用户名", "admin");
      await fillField(session, "密码", "wb01-dr-admin-synthetic");
      await fillField(session, "确认密码", "wb01-dr-admin-synthetic");
      await clickText(session, "创建管理员并进入");
      await waitForCondition(session, `!location.hash.includes("setup")`, { label: "bootstrap", timeoutMs: 25_000 });
      return "admin";
    });
    void setupToken;

    const me = await browserFetch(env.session, "GET", "/api/v1/auth/me");
    if (me.status !== 200) throw new Error(`管理员会话不可用: ${me.status} ${me.text.slice(0, 200)}`);

    await step("R06", "生成真实一致性备份（SQLite VACUUM INTO）并核对迁移账本", async () => {
      const backupDir = join(work, "db/backups");
      await mkdir(backupDir, { recursive: true, mode: 0o700 });
      const target = join(backupDir, "wb01-dr-backup.db");
      const backupFile = join(work, "make-backup.mjs");
      await writeFile(backupFile, BACKUP_SCRIPT, { mode: 0o600 });
      const raw = execText(process.execPath, [backupFile, join(work, "db/controller.db"), target]);
      const info = JSON.parse(raw);
      backupSha = await sha256File(target);
      env.ids.backupId = "wb01-dr-backup";
      return `backupId=wb01-dr-backup ledger=${info.ledger} integrity=${info.integrity} sha256=${backupSha}`;
    });

    await step("R07", "POST /backups/{id}/verify 真实通过（200 且 verified=true）", async () => {
      const response = await browserFetch(env.session, "POST", `/api/v1/backups/${env.ids.backupId}/verify`);
      if (response.status !== 200) throw new Error(`verify -> ${response.status}: ${response.text.slice(0, 300)}`);
      const integrity = (response.json?.checks ?? []).find(item => item.name === "backup_integrity");
      if (response.json?.verified !== true) throw new Error(`verify verified=${response.json?.verified}: ${response.text.slice(0, 300)}`);
      if (!integrity?.passed) throw new Error(`backup_integrity 未通过: ${response.text.slice(0, 300)}`);
      return `200 verified=true backup_integrity=passed againstSha=${backupSha.slice(0, 16)}…`;
    });

    const previewBody = {
      backupId: env.ids.backupId,
      targetControllerId: "wb01-controller-primary",
      oldControllerFenced: true,
      expectedVersion: "0.9.0",
    };

    await probeBlocked("R08", "POST /disaster-recovery/preview 返回 200 但 eligible 恒为 false", async () => {
      const response = await browserFetch(env.session, "POST", "/api/v1/disaster-recovery/preview", previewBody);
      const mismatches = previewMismatches(response);
      const checks = response.json?.checks ?? [];
      const failed = checks.filter(item => item && item.passed === false);
      const reasons = failed.map(item => `${item.name}/${item.reason ?? ""}`);
      env.ids.previewChecks = checks;
      return {
        mismatches,
        label: `非预期预览响应（预期 200 + eligible=false + ${KNOWN_PREVIEW_FAILURES.length} 项已知不合格）`,
        detail: `200 eligible=false，未通过项 ${failed.length}/${checks.length}: ${reasons.join("; ")}`,
        extra: { httpStatus: response.status, checks },
      };
    });

    await probeBlocked("R09", "POST /disaster-recovery/jobs 返回 501（生产未装配 RecoveryExecute）", async () => {
      const response = await browserFetch(env.session, "POST", "/api/v1/disaster-recovery/jobs", previewBody);
      if (response.status === 200 || response.status === 202) {
        // 执行器已被装配：这不是回归，而是 B06 阻塞解除，旧结论必须重新评估。
        return {
          mismatches: [`HTTP ${response.status}：作业已可创建，B06 阻塞前提已变化，必须重新评估`],
          label: "行为已变更（执行器可能已装配）",
          detail: "",
        };
      }
      const mismatches = problemMismatches(response, 501, "RECOVERY_EXECUTOR_UNAVAILABLE");
      env.ids.blockedJob = "unknown";
      return {
        mismatches,
        label: "非预期 jobs 响应（预期 501 RECOVERY_EXECUTOR_UNAVAILABLE + problem 结构）",
        detail: "501 RECOVERY_EXECUTOR_UNAVAILABLE；作业未创建，未触碰任何主控或数据",
        extra: { httpStatus: response.status },
      };
    });

    await probeBlocked("R10", "GET /disaster-recovery/jobs/{id} 恒为 404（作业状态不可读回）", async () => {
      const response = await browserFetch(env.session, "GET", "/api/v1/disaster-recovery/jobs/wb01-probe-job");
      const mismatches = problemMismatches(response, 404, "RECOVERY_JOB_NOT_FOUND");
      return {
        mismatches,
        label: "非预期作业读回响应（预期 404 RECOVERY_JOB_NOT_FOUND + problem 结构）",
        detail: `404 RECOVERY_JOB_NOT_FOUND；作业状态读回端点无条件 404，即使作业创建成功也无法从服务端读回阶段`,
        extra: { httpStatus: response.status },
      };
    });

    await step("R11", "负向：未知备份 ID 必须 404 且不泄露内容", async () => {
      const response = await browserFetch(env.session, "POST", "/api/v1/backups/wb01-no-such-backup/verify");
      if (response.status !== 404) throw new Error(`verify(unknown) -> ${response.status} want 404`);
      if (response.json?.code !== "BACKUP_NOT_FOUND") throw new Error(`code=${response.json?.code} want BACKUP_NOT_FOUND`);
      const leaked = /sqlite|backups\/|\.db/i.test(response.text);
      if (leaked) throw new Error(`404 响应泄露路径细节: ${response.text.slice(0, 200)}`);
      return "404 BACKUP_NOT_FOUND，响应无路径/SQLite 细节";
    });

    await step("R12", "负向：未认证会话必须 403 且不暴露检查项", async () => {
      const cdp = await freePort();
      const anonymousBrowser = await launchHeadlessBrowser({ cdpPort: cdp, profileDir: join(work, "browser-anon") });
      const anonymous = await connectPage(cdp);
      env.anonymousBrowser = anonymousBrowser;
      try {
        await goto(anonymous, `${base()}/#/login`);
        const response = await browserFetch(anonymous, "POST", "/api/v1/disaster-recovery/preview", previewBody);
        if (response.status !== 403) throw new Error(`preview(anonymous) -> ${response.status} want 403`);
        if (response.json?.code !== "NOT_AUTHORIZED") throw new Error(`code=${response.json?.code} want NOT_AUTHORIZED`);
        if (Array.isArray(response.json?.checks)) throw new Error("403 响应泄露了检查项");
        return "403 NOT_AUTHORIZED，未返回任何检查项或备份信息";
      } finally {
        await screenshot(anonymous, join(outDir, "anonymous-denied.png")).catch(() => {});
      }
    });

    await step("R13", "页面实况：备份与恢复页对当前能力一致呈现", async () => {
      await goto(env.session, `${base()}/#/backups`);
      await waitForCondition(env.session, `document.body.innerText.includes("备份与恢复")`, { label: "backups page", timeoutMs: 20_000 });
      const text = await bodyText(env.session);
      // 「恢复成功」在页面上只允许以「不等于业务恢复」这类限定形式出现；
      // 赤裸的完成陈述才是违规。
      const bareClaim = /恢复成功(?!不等于)|恢复完成|已成功恢复/.exec(text);
      if (bareClaim) {
        const at = Math.max(0, bareClaim.index - 60);
        throw new Error(`页面出现未经验证的完成陈述「${bareClaim[0]}」: …${text.slice(at, at + 160)}…`);
      }
      const mentionsGap = /没有备份记录列表|契约缺口/.test(text);
      if (!mentionsGap) throw new Error("页面未标注备份记录端点缺口，会误导用户");
      await screenshot(env.session, join(outDir, "backups-page.png"));
      return "页面标注契约缺口，未出现「恢复成功」类陈述";
    });
  } catch (error) {
    console.error(`\n运行中断: ${error?.message ?? error}`);
  } finally {
    const counts = {
      pass: results.filter(item => item.status === "PASS").length,
      blocked: results.filter(item => item.status === "BLOCKED").length,
      fail: results.filter(item => item.status === "FAIL").length,
    };
    const complete = counts.fail === 0 && counts.blocked === 0;
    const summary = {
      task: "V09-CLOUD-WB-01",
      scope: "B06 灾难恢复前置条件探测（不是恢复成功验收）",
      mode,
      acceptanceComplete: complete,
      verdict: complete ? "COMPLETE" : counts.fail > 0 ? "REGRESSION" : "INCOMPLETE",
      modeNote:
        mode === "complete"
          ? "严格完整模式：BLOCKED 令退出码为 1，只有全 PASS 才算通过。"
          : "诊断模式：BLOCKED 不改变退出码，本结果**不构成完整验收**，不得作为发布准入证据。",
      command: `node tests/browser/v09/run-recovery-prerequisite-probe.mjs${requireComplete ? " --require-complete" : ""}`,
      generatedAt: new Date().toISOString(),
      ports: env.ports,
      backup: { id: env.ids.backupId ?? null, sha256: backupSha || null },
      counts,
      results,
      disclaimer:
        "R08–R10 的 BLOCKED 只在 HTTP 状态、problem 结构、code 与检查项集合精确匹配时成立；" +
        "任何偏差均记 FAIL。BLOCKED 表示产品侧当前确无该能力（预览恒不合格、执行器未装配、作业状态不可读回），" +
        "不计为本任务通过项；本脚本未注入自制恢复执行器，因此不对实际恢复、新权威、旧主控 fence、旧信任代次拒绝下结论。",
    };
    await writeFile(join(outDir, "recovery-probe-results.json"), JSON.stringify(summary, null, 2));
    console.log(`\n模式: ${mode} (${summary.verdict})`);
    console.log(`证据: ${join(outDir, "recovery-probe-results.json")}`);
    console.log(`汇总: PASS=${counts.pass} BLOCKED=${counts.blocked} FAIL=${counts.fail}`);
    if (!complete && mode === "diagnostic") {
      console.log("提示: 诊断模式不构成完整验收；需要完整门禁请加 --require-complete（BLOCKED 将返回非零）。");
    }
    cleanup();
    if (!keep) {
      await sleep(600);
      await rm(work, { recursive: true, force: true }).catch(() => {});
    }
    process.exit(mode === "complete" ? (complete ? 0 : 1) : counts.fail > 0 ? 1 : 0);
  }
}

await main();
