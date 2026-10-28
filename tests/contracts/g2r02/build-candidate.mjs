// Review artifact builder only. Does not generate SDKs or change existing contracts.
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { parseDocument, stringify } from "yaml";

export const root = fileURLToPath(new URL("../../../", import.meta.url));
export const source = "api/openapi/g2-merged-candidate.yaml";
export const candidate = "api/openapi/revisions/g2-r02/merged-candidate.yaml";
export const evidence = "docs/contracts/revisions/g2-r02";
export const read = name => readFileSync(path.join(root, name), "utf8");
export function load(name) {
  const doc = parseDocument(read(name), { uniqueKeys: true });
  if (doc.errors.length) throw new Error(doc.errors.map(e => e.message).join("; "));
  return doc.toJS({ maxAliasCount: 10000 });
}
export const hash = value => createHash("sha256").update(typeof value === "string" ? value : JSON.stringify(value) ?? "undefined").digest("hex");
export const taskStates = ["awaiting_approval", "queued", "waiting_node", "running", "paused", "completed", "partially_failed", "failed", "canceling", "canceled", "unknown", "expired"];
export const executionStates = ["queued", "waiting_node", "offered", "acknowledged", "running", "succeeded", "failed", "cancel_requested", "canceled", "timed_out", "unknown", "expired"];
export const revisionOps = {
  updateNodeGroup: ["node-group", "body-required"],
  replaceNodeGroupMembers: ["node-group", "body-required"],
  deleteNodeGroup: ["node-group", "header-required"],
  patchServiceGroup: ["service-group", "header-required"],
  replaceServiceGroupMembers: ["service-group", "body-required"],
  setServiceGroupMemberScheduling: ["service-group", "header-required"],
  deleteServiceGroup: ["service-group", "header-required"],
  patchUser: ["user", "header-required"],
  patchNodeG2: ["node", "header-required"],
  suspendNodeG2: ["node", "header-required"],
  revokeNodeIdentityG2: ["node", "header-required"],
  approveTaskG2: ["task", "header-required"],
  cancelTaskG2: ["task", "header-required"],
  resumeTaskG2: ["task", "header-required"],
  advanceSafeTask08: ["task", "header-required"],
  stopSafeTask08: ["task", "header-required"],
};
export function operations(doc) {
  return Object.entries(doc.paths).flatMap(([path, item]) => Object.entries(item)
    .filter(([method, op]) => ["get", "post", "put", "patch", "delete", "head", "options", "trace"].includes(method) && op.operationId)
    .map(([method, op]) => ({ path, method, op })));
}
const ref = (section, name) => ({ $ref: `#/components/${section}/${name}` });
const parameter = (name, required, schema, description) => ({ name, in: "header", required, schema, description });
export function resolve(doc, value) {
  if (!value?.$ref) return value;
  return value.$ref.slice(2).split("/").reduce((obj, key) => obj[key.replaceAll("~1", "/").replaceAll("~0", "~")], doc);
}
function addParam(doc, op, name, required) {
  const p = doc.components.parameters[name];
  op.parameters ??= [];
  op.parameters = op.parameters.filter(v => {
    const old = resolve(doc, v);
    return !(old.in === p.in && old.name.toLowerCase() === p.name.toLowerCase());
  });
  op.parameters.push(required === undefined ? ref("parameters", name) : { ...structuredClone(p), required });
}
export function build(base) {
  // Break input YAML anchor identity before changing shared response objects.
  const d = JSON.parse(JSON.stringify(base));
  d.info.version = "0.9.0-g2-r02-candidate";
  d.info.description += "\nG2-R01 / codex-01 独立评审候选，未冻结。仅显式 g2-r02 profile；无运行实现。";
  d["x-contract-profile"] = {
    name: "g2-r02", selector: "X-ServerFleet-Contract", default: "unchanged-source-contract",
    approval: "pending", runtimeImplemented: false,
    rules: "Explicit opt-in; unsupported profile fails before side effects. Never infer profile from credentials or Accept. Probe support before writes. No second queue.",
  };
  const c = d.components;
  delete c.securitySchemes.bearerAuth.bearerFormat;
  c.securitySchemes.bearerAuth.description = "受限非浏览器凭据；不承诺 JWT；签发/撤销/TTL 待审查，当前不开放签发端点。";
  c.securitySchemes.csrfToken = { type: "apiKey", in: "header", name: "X-CSRF-Token", description: "与 Cookie 会话绑定的 CSRF；仅 Cookie 写操作要求，且须校验 Origin/Host。" };
  c.parameters.ContractProfile = parameter("X-ServerFleet-Contract", true, { type: "string", const: "g2-r02" }, "仅显式请求本修订；未知值须在副作用前拒绝，省略时保留原候选行为。SSE 使用可带请求头的 fetch 客户端。");
  c.parameters.IfMatch.schema = { type: "string", pattern: '^"sf-r(0|[1-9][0-9]*)-[a-f0-9]{64}"$' };
  c.parameters.IfMatch.description = '强ETag: "sf-rN-<64hex>"；N是原资源revision，摘要绑定profile+资源URI+授权后的规范表示。完整标签须相等，不仅比较N；不接受*、弱标签或列表。';
  c.parameters.IdempotencyKey.description = "按主体+HTTP方法+规范化路径（含目标ID）+Key 查记录，规范化请求摘要单独比较；同 Key 换参数409。TTL拟24h，需持久化/并发原子占位；重放前复核权限。";
  c.schemas.TaskState = { type: "string", enum: taskStates };
  c.schemas.ExecutionState = { type: "string", enum: executionStates };
  c.schemas.TaskSummary.properties.state = ref("schemas", "TaskState");
  c.schemas.TaskExecution.properties.state = ref("schemas", "ExecutionState");
  const job = c.schemas.BatchTaskJob;
  job.properties.state = ref("schemas", "TaskState");
  for (const name of ["taskId", "revision", "planDigest", "expiresAt"]) job.properties[name] = structuredClone(c.schemas.TaskSummary.properties[name]);
  job.required = [...new Set([...job.required, "taskId", "revision", "planDigest"])];
  job.description = "同一主任务的兼容视图；jobId === taskId。state 与 TaskSummary 同源；不建立第二状态表。旧六状态客户端不可静默降级未知状态。";
  job["x-equal-fields"] = ["jobId", "taskId"];
  job.properties.results.items.properties.state = ref("schemas", "ExecutionState");
  c.schemas.Problem.required = [...new Set([...c.schemas.Problem.required, "detail"])];
  c.schemas.Problem.description = "RFC9457；status必须等于HTTP状态，code稳定；detail仅脱敏摘要，不能直接复制异常/旧message。错误扩展字段由服务端白名单控制。";
  c.schemas.Problem.properties.type = { type: "string", format: "uri-reference" };
  c.schemas.LoginResult.required = [...new Set([...c.schemas.LoginResult.required, "csrfToken"])];
  c.schemas.LoginResult.description = "浏览器会话结果；Cookie: Secure; HttpOnly; SameSite=Strict; Path=/，不设置Domain。不得缓存/记录csrfToken。登录/初始化校验Origin/Host，限流且错误不枚举用户名。";
  for (const name of ["NodeActionInput", "TaskAction"]) c.schemas[name].description = "expectedRevision 是同一资源revision的兼容别名；同时提供If-Match时必须相等，不能用scopeRevision替代。";
  const header = (description, schema) => ({ description, schema });
  const commonHeaders = {
    "X-ServerFleet-Contract": header("确认响应采用显式profile", { type: "string", const: "g2-r02" }),
    Vary: header("包含 X-ServerFleet-Contract；业务响应不跨主体缓存", { type: "string", examples: ["X-ServerFleet-Contract"] }),
    "Cache-Control": header("认证/敏感/错误响应禁止缓存", { type: "string", const: "no-store" }),
  };
  for (const { path, method, op } of operations(d)) {
    const publicOp = (op.security ?? d.security)?.length === 0;
    const write = ["post", "put", "patch", "delete"].includes(method);
    op.security = publicOp ? [] : write ? [{ bearerAuth: [] }, { cookieSession: [], csrfToken: [] }] : [{ bearerAuth: [] }, { cookieSession: [] }];
    addParam(d, op, "ContractProfile");
    if (!publicOp && write) {
      addParam(d, op, "CSRF");
      addParam(d, op, "IdempotencyKey");
      op["x-idempotency"] = { ttlSecondsCandidate: 86400, scope: "subject+method+canonicalPath+key", digestComparedSeparately: true, replayRequiresCurrentAuthorization: true, secretResponseReplay: "409 IDEMPOTENT_SECRET_UNAVAILABLE; never reissue or log secret" };
    }
    if (revisionOps[op.operationId]) {
      const [resource, mode] = revisionOps[op.operationId];
      addParam(d, op, "IfMatch", mode === "header-required");
      op["x-revision-precondition"] = { resource, mode, bothMustAgree: true, staleStatus: 412, missingStatus: 428, malformedOrDisagreeStatus: 400 };
    }
    // Keep every old status (including 422/500) but replace only its error body.
    const codes = new Set([...Object.keys(op.responses).filter(s => /^[45][0-9][0-9]$/.test(s)), "400", "401", "403", "404", "406", "409", "429", "503"]);
    if (revisionOps[op.operationId]) { codes.add("412"); codes.add("428"); }
    if (/^stream.*EventsG2$/.test(op.operationId)) codes.add("410");
    for (const status of codes) {
      c.responses[`R02Error${status}`] = {
      description: `HTTP ${status}; RFC9457脱敏错误；非执行成功`,
      headers: { ...structuredClone(commonHeaders), ...(status === "401" ? { "WWW-Authenticate": header("非浏览器Bearer认证挑战；不泄露令牌", { type: "string" }) } : {}) },
      content: { "application/problem+json": { schema: { allOf: [ref("schemas", "Problem"), { properties: { status: { const: Number(status) } } }] } } },
      };
      op.responses[status] = ref("responses", `R02Error${status}`);
    }
    for (const [status, response] of Object.entries(op.responses)) if (/^2/.test(status)) {
      const r = structuredClone(resolve(d, response));
      r.headers = { ...r.headers, ...structuredClone(commonHeaders) };
      const schemaName = r.content?.["application/json"]?.schema?.$ref?.split("/").at(-1);
      if (["NodeGroup", "ServiceGroup", "User", "NodeDetail", "TaskSummary", "BatchTaskJob"].includes(schemaName)) r.headers.ETag = header('该资源revision对应的强ETag；按授权主体/profile区分表示，任何表示变化须增加revision', structuredClone(c.parameters.IfMatch.schema));
      if (["login", "bootstrapInitialAdmin"].includes(op.operationId)) r.headers["Set-Cookie"] = header("sf_session=<opaque>; Secure; HttpOnly; SameSite=Strict; Path=/，会话撤销立即生效", { type: "string" });
      op.responses[status] = r;
    }
    if (/^stream.*EventsG2$/.test(op.operationId)) {
      delete op.responses["204"];
      op["x-sse"] = { expiredCursorStatus: 410, scope: "subject+resource+stream", heartbeatHasId: false, gapAfterHeaders: "event: stream.gap; close; fetch snapshot then reconnect", sequence: "opaque monotonic cursor; never interpret as permission" };
    }
    if (path.includes("/tasks/")) op["x-task-authority"] = "Controller唯一任务记录；jobId/taskId同一ID；advance只推进未执行批次，stop只停后批，不等同取消运行中执行";
  }
  return d;
}
function schemaChanges(base, next, value) {
  const seen = new Set();
  function walk(v) {
    if (!v || typeof v !== "object") return;
    if (v.$ref?.startsWith("#/components/schemas/")) {
      const n = v.$ref.split("/").at(-1);
      if (seen.has(n)) return;
      seen.add(n); walk(base.components.schemas[n]); walk(next.components.schemas[n]);
    }
    Object.values(v).forEach(walk);
  }
  walk(value);
  return [...seen].filter(n => hash(base.components.schemas[n]) !== hash(next.components.schemas[n])).sort();
}
export function inventory(base, next) {
  const oldIds = JSON.parse(read("docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-OpenAPI-ChangeSet.json")).baseOperationIds;
  const newer = new Map(operations(next).map(x => [x.op.operationId, x]));
  return operations(base).map(({ path, method, op }) => {
    const n = newer.get(op.operationId)?.op;
    if (!n) throw new Error(`Missing operation ${op.operationId}`);
    const success = Object.fromEntries(Object.entries(op.responses).filter(([s]) => /^2/.test(s)));
    return {
      operationId: op.operationId, source: oldIds.includes(op.operationId) ? "legacy-151" : "g2-added-34", method, path,
      sourceOperationSha256: hash(op), revisedOperationSha256: hash(n),
      authBefore: op.security ?? base.security, authAfter: n.security,
      successCodesBefore: Object.keys(success), successCodesAfter: Object.keys(n.responses).filter(s => /^2/.test(s)),
      successSchemaChanges: schemaChanges(base, next, success),
      errorsBefore: Object.fromEntries(Object.entries(op.responses).filter(([s]) => /^[45]/.test(s))),
      errorsAfter: Object.keys(n.responses).filter(s => /^[45]/.test(s)),
      revision: n["x-revision-precondition"] ?? "unchanged-domain-revision; no new generic ETag implied",
      idempotency: n["x-idempotency"] ? "required; proposed24h; current-auth-on-replay" : "source/special-flow",
      pagination: "success envelope and cursor parameters preserved; no fabricated page/count",
      review: op.operationId.startsWith("xray") ? "E02/E08 business success/async model deferred; optional plugin remains disabled unless installed+authorized" : "cross-cutting candidate only; runtime/security acceptance pending",
    };
  });
}
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const base = load(source), next = build(base), rows = inventory(base, next);
  mkdirSync(path.join(root, "api/openapi/revisions/g2-r02"), { recursive: true });
  mkdirSync(path.join(root, evidence), { recursive: true });
  writeFileSync(path.join(root, candidate), stringify(next, { aliasDuplicateObjects: false, lineWidth: 120 }));
  writeFileSync(path.join(root, evidence, "operations.json"), JSON.stringify(rows, null, 2) + "\n");
  const table = ["# 逐操作兼容矩阵", "", "自动提取的185项：旧151项+新增34项。不是逐项业务实现批准。完整前后认证、错误、成功状态码、传递schema变化和SHA256见 operations.json。", "", "|来源|operationId|方法/路径|成功码|传递成功schema变化|revision|", "|---|---|---|---|---|---|", ...rows.map(r => `|${r.source}|${r.operationId}|${r.method.toUpperCase()} ${r.path}|${r.successCodesBefore.join(",")} → ${r.successCodesAfter.join(",")}|${r.successSchemaChanges.join(",") || "保留"}|${typeof r.revision === "string" ? "原领域语义" : `${r.revision.resource}/${r.revision.mode}`}|`)];
  writeFileSync(path.join(root, evidence, "OPERATIONS.md"), table.join("\n") + "\n");
  console.log(`Wrote independent candidate + ${rows.length} operation records (${rows.filter(r => r.source === "legacy-151").length} legacy)`);
}
