/**
 * 鏈湴娴忚鍣ㄩ獙鏀舵々锛圙M-09锛夈€備粎鐩戝惉 127.0.0.1锛屽彧涓哄湪缂哄皯鐪熷疄绠＄悊 API 鏃堕獙璇佸凡鏋勫缓鐨勫墠绔€? *
 * 杩欎笉鏄?Controller 瀹炵幇锛屼篃姘歌繙涓嶄細琚?`apps/web/src` 寮曠敤鎴栨墦鍖呰繘 dist锛? * 鐢熶骇浠ｇ爜鍦ㄨ姹傚け璐ユ椂鍙樉绀洪敊璇垎绫伙紝缁濅笉鍥為€€鍒拌繖閲岀殑鍋囨暟鎹€? *
 * 鐢ㄦ硶锛? *   node apps/web/acceptance/stub-server.mjs --port 5180            # 濂戠害妗?+ 闈欐€?dist
 *   node apps/web/acceptance/stub-server.mjs --port 5181 --healthz-only  # 鍙毚闇?/healthz锛堝鐜扮湡瀹炲悗绔己澶憋級
 */
import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { extname, join, normalize, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { randomUUID, randomBytes } from "node:crypto";
import { errorBody, unwrap } from "./harness.mjs";

const here = fileURLToPath(new URL(".", import.meta.url));
const distDir = resolve(here, "../../../apps/web/dist");

const args = process.argv.slice(2);
function argValue(name, fallback) {
  const index = args.indexOf(`--${name}`);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
}
const port = Number(argValue("port", "5180"));
const healthzOnly = args.includes("--healthz-only");
const loginPassword = argValue("password", "accept123");

const IDENTITY = {
  userId: "u-admin",
  displayName: "楠屾敹绠＄悊鍛?,
  roles: ["admin"],
  permissions: [
    "node.read", "node.manage", "task.read", "task.submit", "task.approve", "audit.read",
    "user.manage", "group.manage", "servicegroup.manage", "credential.manage", "settings.manage",
    "monitor.read", "monitor.manage", "dns.manage", "certificate.manage", "gateway.manage",
    "publication.manage", "ai.use", "ai.approve", "ai.manage", "plugin.manage", "plugin.install",
    "plugin.repository.manage", "xray.manage", "system.update", "backup.manage", "file.read",
    "file.write", "file.transfer", "transfer.cancel",
  ],
  isBootstrapAdmin: true,
};

const sessions = new Map();
const state = {};
const faults = [];
let requestLog = [];

const routes = new Map();
const patterns = [];

function register(table) {
  for (const [key, handler] of Object.entries(table)) {
    const space = key.indexOf(" ");
    const method = key.slice(0, space);
    const path = key.slice(space + 1);
    if (path.includes("{")) {
      const names = [...path.matchAll(/\{([^}]+)\}/g)].map(match => match[1]);
      const regex = new RegExp(`^${path.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/\\\{([^}]+)\\\}/g, "([^/]+)")}$`);
      patterns.push({ method, regex, names, handler, key });
    } else {
      routes.set(key, handler);
    }
  }
}

async function loadFixtures() {
  try {
    const module = await import(new URL("./fixtures.mjs", import.meta.url).href);
    register(module.routes);
    console.log(`loaded fixtures: ${Object.keys(module.routes).length} routes`);
  } catch (cause) {
    console.log(`skip fixtures: ${cause.stack}`);
  }
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

function sendJson(res, status, body, headers = {}) {
  if (body === undefined) {
    res.writeHead(status, { "Cache-Control": "no-store", ...headers });
    res.end();
    return;
  }
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(payload),
    "Cache-Control": "no-store",
    ...headers,
  });
  res.end(payload);
}

function parseCookies(header) {
  const jar = {};
  for (const part of (header ?? "").split(";")) {
    const index = part.indexOf("=");
    if (index > 0) jar[part.slice(0, index).trim()] = part.slice(index + 1).trim();
  }
  return jar;
}

function matchFault(method, path) {
  for (const [index, fault] of faults.entries()) {
    if (fault.method && fault.method !== method) continue;
    if (!path.startsWith(fault.match)) continue;
    if (typeof fault.times === "number") {
      fault.times -= 1;
      if (fault.times <= 0) faults.splice(index, 1);
    }
    return fault;
  }
  return null;
}

function sseScript(kind, id) {
  if (kind === "task") {
    return [
      { event: "task.state", data: { taskId: id, state: "queued", message: "宸插叆闃?, progressPercent: 0, sequence: 1 } },
      { event: "task.state", data: { taskId: id, state: "running", nodeId: "n-1", message: "姝ｅ湪鎵ц", progressPercent: 35, sequence: 2 } },
      { event: "task.progress", data: { taskId: id, state: "running", nodeId: "n-1", progressPercent: 72, sequence: 3 } },
      { event: "task.state", data: { taskId: id, state: "completed", message: "鎵ц瀹屾垚", progressPercent: 100, sequence: 4 } },
    ];
  }
  return [
    { event: "ai.tool", data: { toolName: "metrics.read", approvalId: "ap-stub-1", state: "running", message: "璇诲彇鑺傜偣鎸囨爣", output: "SECRET-STUB-OUTPUT-MUST-NOT-RENDER" } },
    { event: "ai.tool", data: { toolName: "metrics.read", approvalId: "ap-stub-1", state: "succeeded", message: "宸茶繑鍥炴巿鏉冭寖鍥村唴鐨勬寚鏍?, output: "SECRET-STUB-OUTPUT-MUST-NOT-RENDER" } },
    { event: "ai.scope", data: { state: "scope_changed", message: "scopeRevision 宸查€掑" } },
  ];
}

async function streamSSE(req, res, kind, id) {
  res.writeHead(200, {
    "Content-Type": "text/event-stream; charset=utf-8",
    "Cache-Control": "no-store",
    Connection: "keep-alive",
  });
  const lastEventId = Number(req.headers["last-event-id"] ?? "0");
  const script = sseScript(kind, id);
  let index = 0;
  res.write(": stub stream open\n\n");
  const timer = setInterval(() => {
    if (index >= script.length) {
      res.write(`: keep-alive ${new Date().toISOString()}\n\n`);
      return;
    }
    const entry = script[index];
    index += 1;
    if (index <= lastEventId) return;
    res.write(`id: ${index}\nevent: ${entry.event}\ndata: ${JSON.stringify(entry.data)}\n\n`);
  }, 900);
  req.on("close", () => clearInterval(timer));
}

function findRoute(method, pathname) {
  const direct = routes.get(`${method} ${pathname}`);
  if (direct) return { handler: direct, params: {} };
  for (const pattern of patterns) {
    if (pattern.method !== method) continue;
    const match = pattern.regex.exec(pathname);
    if (!match) continue;
    const params = {};
    pattern.names.forEach((name, position) => {
      params[name] = decodeURIComponent(match[position + 1]);
    });
    return { handler: pattern.handler, params };
  }
  return null;
}

async function serveStatic(req, res, pathname) {
  const relative = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const target = resolve(join(distDir, normalize(relative).replace(/^(\.\.[/\\])+/, "")));
  if (!target.startsWith(distDir + sep) && target !== join(distDir, "index.html")) {
    res.writeHead(403).end("forbidden");
    return;
  }
  let file = target;
  try {
    const info = await stat(file);
    if (info.isDirectory()) file = join(file, "index.html");
  } catch {
    file = join(distDir, "index.html");
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

const MUTATING = new Set(["POST", "PUT", "PATCH", "DELETE"]);
const CSRF_EXEMPT = new Set(["/api/v1/auth/login", "/api/v1/auth/bootstrap"]);

async function handleApi(req, res, url) {
  const method = req.method;
  const pathname = url.pathname;
  const jars = parseCookies(req.headers.cookie);
  const session = sessions.get(jars.sf_session);

  if (pathname === "/healthz") {
    return sendJson(res, 200, { status: "scaffold", stage: "M0-P1" });
  }

  if (pathname.startsWith("/__stub/")) {
    return handleStubControl(req, res, pathname, method);
  }

  const fault = matchFault(method, pathname);
  if (fault) {
    if (fault.delayMs) await new Promise(done => setTimeout(done, fault.delayMs));
    return sendJson(res, fault.status, fault.body ?? errorBody(fault.code ?? `HTTP_${fault.status}`, `楠屾敹妗╂敞鍏ョ殑 ${fault.status} 鍝嶅簲`));
  }

  if (healthzOnly) {
    return sendJson(res, 404, errorBody("NOT_FOUND", "璇?Controller 鍙彁渚?/healthz锛涚鐞?API 灏氭湭瀹炵幇"));
  }

  if (pathname === "/api/v1/auth/bootstrap-status") {
    return sendJson(res, 200, { bootstrapAvailable: false, initialized: true });
  }

  if (pathname === "/api/v1/auth/login" && method === "POST") {
    const body = await readBody(req);
    if (!body.username || body.password !== loginPassword) {
      return sendJson(res, 401, errorBody("UNAUTHENTICATED", "鐢ㄦ埛鍚嶆垨瀵嗙爜涓嶆纭?));
    }
    const id = randomBytes(16).toString("hex");
    const csrf = `csrf-${randomBytes(12).toString("hex")}`;
    sessions.set(id, { csrf, valid: true });
    return sendJson(
      res,
      200,
      { identity: IDENTITY, sessionExpiresAt: new Date(Date.now() + 8 * 3600 * 1000).toISOString(), csrfToken: csrf },
      { "Set-Cookie": `sf_session=${id}; Path=/; HttpOnly; SameSite=Strict` },
    );
  }

  if (pathname === "/api/v1/auth/logout" && method === "POST") {
    if (jars.sf_session) sessions.delete(jars.sf_session);
    return sendJson(res, 204, undefined, { "Set-Cookie": "sf_session=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0" });
  }

  if (!session || !session.valid) {
    return sendJson(res, 401, errorBody("UNAUTHENTICATED", "浼氳瘽涓嶅瓨鍦ㄦ垨宸插け鏁?));
  }

  if (pathname === "/api/v1/auth/me") {
    // 濂戠害缂哄彛 搂1锛?auth/me 涓嶈繑鍥?CSRF锛屽埛鏂板悗鍐欐搷浣滃湪鍓嶇琚攣瀹氥€?    return sendJson(res, 200, IDENTITY);
  }

  if (MUTATING.has(method) && !CSRF_EXEMPT.has(pathname)) {
    const token = req.headers["x-csrf-token"];
    if (!token) return sendJson(res, 403, errorBody("CSRF_MISSING", "缂哄皯 X-CSRF-Token"));
    if (token !== session.csrf) return sendJson(res, 403, errorBody("CSRF_INVALID", "X-CSRF-Token 涓庝細璇濅笉鍖归厤"));
  }

  if (method === "GET" && /^\/api\/v1\/(?:tasks\/[^/]+|ai\/sessions\/[^/]+)\/events$/.test(pathname)) {
    const kind = pathname.includes("/ai/") ? "ai" : "task";
    const id = decodeURIComponent(pathname.split("/").slice(-2, -1)[0] ?? "unknown");
    return streamSSE(req, res, kind, id);
  }

  const found = findRoute(method, pathname);
  if (!found) {
    return sendJson(res, 404, errorBody("NOT_FOUND", `楠屾敹妗╂病鏈夊疄鐜?${method} ${pathname}`));
  }

  const ctx = {
    params: found.params,
    query: url.searchParams,
    body: MUTATING.has(method) ? await readBody(req) : undefined,
    state,
    session: { userId: IDENTITY.userId, permissions: IDENTITY.permissions },
  };
  try {
    const produced = typeof found.handler === "function" ? await found.handler(ctx) : found.handler;
    const { status, body } = unwrap(produced);
    if (status === 204 || body === undefined) {
      res.writeHead(204, { "Cache-Control": "no-store" });
      return res.end();
    }
    return sendJson(res, status, body);
  } catch (cause) {
    console.log(`fixture error ${method} ${pathname}: ${cause.stack}`);
    return sendJson(res, 500, errorBody("STUB_FIXTURE_ERROR", String(cause.message)));
  }
}

async function readBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const text = Buffer.concat(chunks).toString("utf8");
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

async function handleStubControl(req, res, pathname, method) {
  if (pathname === "/__stub/faults" && method === "POST") {
    const body = await readBody(req);
    const fault = { id: randomUUID(), times: undefined, delayMs: 0, method: undefined, ...body };
    faults.push(fault);
    return sendJson(res, 201, fault);
  }
  if (pathname === "/__stub/faults" && method === "GET") return sendJson(res, 200, { items: faults });
  if (pathname === "/__stub/faults" && method === "DELETE") {
    faults.length = 0;
    return sendJson(res, 200, { cleared: true });
  }
  if (pathname === "/__stub/session/invalidate" && method === "POST") {
    for (const session of sessions.values()) session.valid = false;
    return sendJson(res, 200, { invalidated: sessions.size });
  }
  if (pathname === "/__stub/log" && method === "GET") return sendJson(res, 200, { items: requestLog.slice(-200) });
  if (pathname === "/__stub/log" && method === "DELETE") {
    requestLog = [];
    return sendJson(res, 200, { cleared: true });
  }
  if (pathname === "/__stub/state" && method === "GET") return sendJson(res, 200, state);
  return sendJson(res, 404, errorBody("NOT_FOUND", "鏈煡鐨勬々鎺у埗绔偣"));
}

await loadFixtures();

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host ?? "127.0.0.1"}`);
  const started = Date.now();
  res.on("finish", () => {
    const entry = `${req.method} ${url.pathname} -> ${res.statusCode} (${Date.now() - started}ms)`;
    requestLog.push(entry);
    if (requestLog.length > 2000) requestLog = requestLog.slice(-1000);
    console.log(entry);
  });
  try {
    if (url.pathname.startsWith("/api/") || url.pathname === "/healthz" || url.pathname.startsWith("/__stub/")) {
      await handleApi(req, res, url);
      return;
    }
    await serveStatic(req, res, url.pathname);
  } catch (cause) {
    console.log(`handler error: ${cause.stack}`);
    if (!res.headersSent) sendJson(res, 500, errorBody("STUB_ERROR", String(cause.message)));
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log(`acceptance stub listening on http://127.0.0.1:${port} (healthz-only=${healthzOnly}, password=${loginPassword})`);
});
