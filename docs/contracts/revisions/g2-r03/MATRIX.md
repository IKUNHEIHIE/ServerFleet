# 来源、操作与逐字段矩阵

由 tests/contracts/g2r03/build-candidate.mjs 生成并独立重算；语义与决策见 COMPATIBILITY.md。来源 SHA-256 见 provenance.json。G2 未冻结。

## 操作

| 方法/路径 | operationId（保留） | 原错误 → 候选错误 | 错误媒体（保留） | 原认证（保留） |
|---|---|---|---|---|
| GET /api/v1/nodes | listNodesForTransfer | 401 → 400/401/403/503 | application/json | [{"bearerAuth":[]}] |
| GET /api/v1/nodes/{nodeId} | getNodeDetailG2 | 400/401/403/404/409/412/428/429/503 → 400/401/403/404/409/412/428/429/503 | application/problem+json | [{"cookieSession":[]},{"bearerAuth":[]}] |

Core 只有详情；原始 v0.9/Merged 和 r02 均有列表。r02 的必需 profile 头/统一 Problem 不进入本候选。列表仍 {items:[NodeSummary]}，详情仍 NodeDetail；不新增集合路由或复用 operationId。

## 字段

| 字段 | 原 Merged 列表 | 原 Core/Merged 详情 | Web 局部形状 | r03 提案/兼容边界 |
|---|---|---|---|---|
| id | id (必需) | nodeId (必需) | nodeId → id | 仅别名映射；不 trim、补造或从名称构造 ID |
| name | name (必需) | displayName (必需) | displayName → name | 保留原显示值；Web 空名回退只属显示，不回写 DTO |
| online | online:boolean (必需) | 无 | 仅 status=online 显示指标 | 布尔字段保留；false 不能反推出 offline/suspended/revoked |
| status | 缺失 | 五状态 (必需) | 五状态 (必需) | 列表可选新增；有权威状态时 online 必须等于 status=online；无状态不猜 |
| revision | 缺失 | integer >=1 (必需) | number (必需) | 列表可选新增；不得填 1/0；缺失不能直接构造现有 Web DTO |
| platform | 缺失 | 可选 string | 可选 string/null | 列表可选新增；无证据省略；Web 归一到 null |
| agentVersion | 缺失 | 可选 string | 可选 string/null | 列表可选新增；不得拿 Controller 版本充当 Agent 版本 |
| lastSeenAt | 缺失 | 可选 date-time string | 可选 string/null | 列表可选新增；沿用详情不接受 null；不是指标采样时间 |
| cpuPercent | 可选 number/null, 0..100 | 缺失 | 局部 CandidateNodeMetrics → cpu | 保留列表原字段；详情可选新增；0 是真实值，null/缺失不是 0 |
| memoryPercent | 可选 number/null, 0..100 | 缺失 | 局部 CandidateNodeMetrics → memory | 保留列表原字段；详情可选新增；越界不夹取 |
| sampledAt | 缺失 | 缺失 | 局部指标可选 string/null | 两者可选新增 date-time/null；仅同一真实样本共用；缺失不能称实时 |
| region/provider/countryCode/ip/tags | 可选；countryCode 两位大写 | 缺失 | 当前列表 VM 未声明 | 全部保留原字段；无权字段不得输出；不扩进详情 |

region/provider/countryCode/ip/tags 的类型逐项由测试与原 NodeSummary 深比较；表中合并仅为阅读。NodeSummary 原来未禁止额外字段，NodeDetail 原来 additionalProperties=false，均保留。
