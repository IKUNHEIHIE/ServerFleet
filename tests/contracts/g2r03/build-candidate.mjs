import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseDocument, stringify } from "yaml";

export const root = fileURLToPath(new URL("../../../", import.meta.url));
export const candidate = "api/openapi/revisions/g2-r03/nodes-read-candidate.yaml";
export const docs = "docs/contracts/revisions/g2-r03";
export const listPath = "/api/v1/nodes";
export const detailPath = "/api/v1/nodes/{nodeId}";
export const sources = {
  merged: "api/openapi/g2-merged-candidate.yaml",
  core: "api/openapi/g2-core-candidate.yaml",
  r02: "api/openapi/revisions/g2-r02/merged-candidate.yaml",
  originalMerged: "docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml",
  originalLegacy: "docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-API-Contract-v0.9.yaml",
  webAdapter: "apps/web/src/features/nodes/candidate-adapter.ts",
  webTypes: "apps/web/src/features/nodes/types.ts",
  node: "internal/nodes/node.go",
  visibility: "internal/nodes/visibility.go",
  auth: "internal/auth/node_policy.go",
};
export const read = file => readFileSync(path.join(root, file), "utf8");
export const hash = value => createHash("sha256").update(value).digest("hex");
export function document(file) {
  const parsed = parseDocument(read(file), { uniqueKeys: true });
  if (parsed.errors.length) throw new Error(`${file}: ${parsed.errors[0].message}`);
  return parsed.toJS({ maxAliasCount: 10000 });
}

export const fields = [
  ["id", "id (必需)", "nodeId (必需)", "nodeId → id", "id", "nodeId", "仅别名映射；不 trim、补造或从名称构造 ID"],
  ["name", "name (必需)", "displayName (必需)", "displayName → name", "name", "displayName", "保留原显示值；Web 空名回退只属显示，不回写 DTO"],
  ["online", "online:boolean (必需)", "无", "仅 status=online 显示指标", "online", null, "布尔字段保留；false 不能反推出 offline/suspended/revoked"],
  ["status", "缺失", "五状态 (必需)", "五状态 (必需)", "status", "status", "列表可选新增；有权威状态时 online 必须等于 status=online；无状态不猜"],
  ["revision", "缺失", "integer >=1 (必需)", "number (必需)", "revision", "revision", "列表可选新增；不得填 1/0；缺失不能直接构造现有 Web DTO"],
  ["platform", "缺失", "可选 string", "可选 string/null", "platform", "platform", "列表可选新增；无证据省略；Web 归一到 null"],
  ["agentVersion", "缺失", "可选 string", "可选 string/null", "agentVersion", "agentVersion", "列表可选新增；不得拿 Controller 版本充当 Agent 版本"],
  ["lastSeenAt", "缺失", "可选 date-time string", "可选 string/null", "lastSeenAt", "lastSeenAt", "列表可选新增；沿用详情不接受 null；不是指标采样时间"],
  ["cpuPercent", "可选 number/null, 0..100", "缺失", "局部 CandidateNodeMetrics → cpu", "cpuPercent", "cpuPercent", "保留列表原字段；详情可选新增；0 是真实值，null/缺失不是 0"],
  ["memoryPercent", "可选 number/null, 0..100", "缺失", "局部 CandidateNodeMetrics → memory", "memoryPercent", "memoryPercent", "保留列表原字段；详情可选新增；越界不夹取"],
  ["sampledAt", "缺失", "缺失", "局部指标可选 string/null", "sampledAt", "sampledAt", "两者可选新增 date-time/null；仅同一真实样本共用；缺失不能称实时"],
  ["region/provider/countryCode/ip/tags", "可选；countryCode 两位大写", "缺失", "当前列表 VM 未声明", "region", null, "全部保留原字段；无权字段不得输出；不扩进详情"],
];

export function buildCandidate() {
  const baseline = document(sources.merged);
  const summary = structuredClone(baseline.components.schemas.NodeSummary);
  const detail = structuredClone(baseline.components.schemas.NodeDetail);
  // Only optional additions: old required fields and property definitions stay intact.
  for (const name of ["status", "revision", "platform", "agentVersion", "lastSeenAt"]) {
    summary.properties[name] = structuredClone(detail.properties[name]);
  }
  const sampledAt = {
    type: ["string", "null"], format: "date-time",
    description: "提案：CPU/内存同一真实样本的时间；无共同采样证据时省略或 null。不得用 lastSeenAt、响应时间或 now 补齐。新鲜度由可信调用方显式时间/阈值判断。",
  };
  summary.properties.sampledAt = structuredClone(sampledAt);
  for (const name of ["cpuPercent", "memoryPercent"]) detail.properties[name] = structuredClone(summary.properties[name]);
  detail.properties.sampledAt = structuredClone(sampledAt);
  summary.description = "提案：兼容旧 NodeSummary 的可选扩展。id/name/online 与所有旧字段保留；只有可信来源提供时输出 status/revision 等新字段。无采样时间的旧指标仍可读取但不能宣称新鲜。";
  summary.oneOf = [
    { not: { required: ["status"] } },
    { required: ["status"], properties: { status: { const: "online" }, online: { const: true } } },
    { required: ["status"], properties: { status: { enum: ["offline", "suspended", "revoked", "unknown"] }, online: { const: false } } },
  ];
  detail.description = "沿用原 NodeDetail 的必填和字段类型，仅提案增加可选 CPU/内存及共同 sampledAt。revision 是节点元数据并发版本，不是指标样本序号，不是授权证明。";
  const list = structuredClone(baseline.paths[listPath].get);
  const get = structuredClone(baseline.paths[detailPath].get);
  list.description = "既有列表操作的范围内修订提案。先绑定可信身份并确认当前策略可用，再过滤授权可见集合，最后应用 onlineOnly；不返回全量计数、无权字段或部分成功。空库存也必须先检查身份/策略。无分页/total 新增。";
  list.responses["200"].content["application/json"].schema = { $ref: "#/components/schemas/NodeList" };
  list.responses["400"] = { $ref: "#/components/responses/ListBadRequest" };
  list.responses["403"] = { $ref: "#/components/responses/ListForbidden" };
  list.responses["503"] = { $ref: "#/components/responses/ListUnavailable" };
  get.description = "沿用已有详情操作。身份验证与当前 nodes.read 授权先于资源/版本观察；缺失或不可见统一 404 是待批准的防枚举提案。原声明的通用错误码保留，不据此增加写入或 If-Match 要求。";
  const headers = { "Cache-Control": { description: "提案：所有成功及错误响应禁止共享/跨主体复用。", schema: { type: "string", const: "no-store" }, example: "no-store" } };
  const response = (description, media, schema, example) => ({ description, headers: structuredClone(headers), content: { [media]: { schema, examples: { valid: { value: example } } } } });
  const responses = {
    Unauthorized: response(baseline.components.responses.Unauthorized.description, "application/json", { $ref: "#/components/schemas/Error" }, { code: "UNAUTHENTICATED", message: "需要有效身份凭证" }),
    ListBadRequest: response("提案：查询参数无效；不输出原始内部异常。", "application/json", { $ref: "#/components/schemas/Error" }, { code: "INVALID_REQUEST", message: "请求参数无效" }),
    ListForbidden: response("提案：已验证主体被拒绝访问列表操作；逐节点的纯拒绝只过滤对应节点。", "application/json", { $ref: "#/components/schemas/Error" }, { code: "PERMISSION_DENIED", message: "请求未获授权" }),
    ListUnavailable: response("提案：策略读取失败、不可用或未知授权故障；绝不能转为 200 空/部分列表。", "application/json", { $ref: "#/components/schemas/Error" }, { code: "POLICY_UNAVAILABLE", message: "服务暂不可用" }),
  };
  for (const status of [400, 401, 403, 404, 409, 412, 428, 429, 503]) {
    const original = baseline.components.responses[`Error${status}`];
    responses[`Error${status}`] = response(original.description, "application/problem+json", {
      allOf: [{ $ref: "#/components/schemas/Problem" }, { properties: { status: { const: status } } }],
    }, { type: "about:blank", title: "请求未完成", status, code: status === 503 ? "POLICY_UNAVAILABLE" : `HTTP_${status}`, requestId: "request-example-01" });
  }
  list.responses["200"].headers = structuredClone(headers);
  get.responses["200"].headers = structuredClone(headers);
  const legacy = { id: "node-example-01", name: "示例节点", online: true, cpuPercent: 23, memoryPercent: null };
  list.responses["200"].content["application/json"].examples = {
    legacyUntimed: { summary: "合法旧响应；没有状态/revision/采样时间，不能直接当作新 Web DTO", value: { items: [legacy] } },
    empty: { summary: "仅身份和策略已成功检查后允许的空可见集合", value: { items: [] } },
    enriched: { summary: "仅可信数据源确有字段时添加；五状态及指标边界", value: { items: ["online", "offline", "suspended", "revoked", "unknown"].map((status, i) => ({
      id: `node-example-${i + 1}`, name: `示例节点 ${i + 1}`, online: status === "online", status, revision: i + 1,
      ...(i === 0 ? { cpuPercent: 0, memoryPercent: 100, sampledAt: "2026-10-29T00:00:00Z", platform: "linux/amd64", agentVersion: "example-version", lastSeenAt: "2026-10-29T00:00:10Z" } : { cpuPercent: null, memoryPercent: null, sampledAt: null }),
    })) } },
  };
  get.responses["200"].content["application/json"].examples = Object.fromEntries(["online", "offline", "suspended", "revoked", "unknown"].map((status, i) => [status, { value: {
    nodeId: `node-example-${i + 1}`, displayName: `示例节点 ${i + 1}`, status, revision: i + 1,
    ...(i === 0 ? { cpuPercent: 100, memoryPercent: 0, sampledAt: "2026-10-29T00:00:00Z" } : {}),
  } }]));
  return {
    openapi: "3.1.0",
    info: { title: "Server Fleet 节点只读 g2-r03 兼容候选（未批准/未冻结）", version: "0.9.0-g2-r03-candidate", description: "G2-R02A/codex-01：仅两个既有 GET 的隔离评审候选；不是运行时 profile、完整 API 或真实 HTTP 实现。保留列表/详情原认证和错误格式差异，必须经总控/产品/安全批准后才可接线。" },
    servers: structuredClone(baseline.servers),
    security: structuredClone(baseline.security),
    tags: [{ name: "Nodes", description: "既有授权节点列表" }, { name: "NodeLifecycle", description: "既有节点详情" }],
    "x-review-status": "PROPOSED-NOT-FROZEN",
    paths: { [listPath]: { get: list }, [detailPath]: { get } },
    components: { securitySchemes: structuredClone(baseline.components.securitySchemes), schemas: {
      NodeList: { type: "object", required: ["items"], properties: { items: { type: "array", items: { $ref: "#/components/schemas/NodeSummary" } } } },
      NodeSummary: summary, NodeDetail: detail,
      Error: structuredClone(baseline.components.schemas.Error), Problem: structuredClone(baseline.components.schemas.Problem),
    }, responses },
  };
}

export function provenance() {
  return {
    task: "G2-R02A", session: "codex-01", base: "0fef8ba9d42bc50d442721b4ab382af961604e72", status: "PROPOSED-NOT-FROZEN",
    sources: Object.fromEntries(Object.entries(sources).map(([name, file]) => [name, { file, sha256: hash(readFileSync(path.join(root, file))) }])),
    operations: [
      { method: "get", path: listPath, operationId: "listNodesForTransfer", schema: "NodeSummary", originalErrors: ["401"], candidateErrors: ["400", "401", "403", "503"], errorMedia: "application/json", required: ["id", "name", "online"], security: [{ bearerAuth: [] }] },
      { method: "get", path: detailPath, operationId: "getNodeDetailG2", schema: "NodeDetail", originalErrors: ["400", "401", "403", "404", "409", "412", "428", "429", "503"], candidateErrors: ["400", "401", "403", "404", "409", "412", "428", "429", "503"], errorMedia: "application/problem+json", required: ["nodeId", "displayName", "status", "revision"], security: [{ cookieSession: [] }, { bearerAuth: [] }] },
    ],
    fields: fields.map(([id, legacy, detail, web, listProperty, detailProperty, proposal]) => ({ id, legacy, detail, web, listProperty, detailProperty, proposal })),
  };
}

export function matrixMarkdown(data = provenance()) {
  return `# 来源、操作与逐字段矩阵\n\n由 tests/contracts/g2r03/build-candidate.mjs 生成并独立重算；语义与决策见 COMPATIBILITY.md。来源 SHA-256 见 provenance.json。G2 未冻结。\n\n## 操作\n\n| 方法/路径 | operationId（保留） | 原错误 → 候选错误 | 错误媒体（保留） | 原认证（保留） |\n|---|---|---|---|---|\n${data.operations.map(op => `| GET ${op.path} | ${op.operationId} | ${op.originalErrors.join("/")} → ${op.candidateErrors.join("/")} | ${op.errorMedia} | ${JSON.stringify(op.security)} |`).join("\n")}\n\nCore 只有详情；原始 v0.9/Merged 和 r02 均有列表。r02 的必需 profile 头/统一 Problem 不进入本候选。列表仍 {items:[NodeSummary]}，详情仍 NodeDetail；不新增集合路由或复用 operationId。\n\n## 字段\n\n| 字段 | 原 Merged 列表 | 原 Core/Merged 详情 | Web 局部形状 | r03 提案/兼容边界 |\n|---|---|---|---|---|\n${data.fields.map(f => `| ${f.id} | ${f.legacy} | ${f.detail} | ${f.web} | ${f.proposal} |`).join("\n")}\n\nregion/provider/countryCode/ip/tags 的类型逐项由测试与原 NodeSummary 深比较；表中合并仅为阅读。NodeSummary 原来未禁止额外字段，NodeDetail 原来 additionalProperties=false，均保留。\n`;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] !== "--write") throw new Error("Use --write to regenerate only the three g2-r03 review artifacts");
  for (const [file, text] of [
    [candidate, stringify(buildCandidate(), { aliasDuplicateObjects: false, lineWidth: 110 })],
    [`${docs}/provenance.json`, JSON.stringify(provenance(), null, 2) + "\n"],
    [`${docs}/MATRIX.md`, matrixMarkdown()],
  ]) {
    mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    writeFileSync(path.join(root, file), text);
    console.log(`wrote ${file}`);
  }
}
