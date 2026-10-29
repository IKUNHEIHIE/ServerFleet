# READ-PLAN-01 · 首条只读闭环可批准方案

状态：**PROPOSED / 待负责人批准；G2 未冻结**。本文件只提出建议、依据、代价与缺失证据，不修改既有代码、候选、D01–D07 或 P1-C10 状态，也不构成 HTTP/身份/契约冻结授权。

基线：`98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`。引用均为该提交下的仓库路径与行号。

## 1. 结论与推荐

**推荐分两步，不把“列表 + 详情 + 富化字段”一次做成。**

1. **R1-A（建议先批准并实施）：legacy 兼容的只读列表闭环。** 只实现既有 `GET /api/v1/nodes`，保持旧路径、`listNodesForTransfer`、`{items:[...]}`、必填 `id/name/online`；可信身份/策略预检通过后，只返回授权可见节点的旧字段子集。列表不新增 `status/revision/platform/agentVersion/lastSeenAt/sampledAt`，不提供详情，不显示未采样的 CPU/内存为新鲜值。Web 使用一个**新的局部 legacy 列表 ViewModel/适配**（当前 `NodeListItemView` 要求 `revision/status`，不能直接承载旧列表行）。
2. **R1-B（随后）：在权威节点元数据与共同采样时间可用后，启用详情和富化列表。** 需要新增可信的 revision/status/platform/agentVersion 来源、共同 `sampledAt` 语义，以及 Web 现有五状态/`NodeListItemView` 的完整映射；这些是当前内部 `Node` 没有的字段，不能用 `1`、当前时间或 `false` 补齐。

不建议直接做“含详情的完整 DTO”作为第一版：它会同时阻塞在 D02/D03/D04 和节点元数据来源上，且当前 Web 类型无法接收缺少 revision 的旧列表。若负责人选择一次完成，须接受 R1-B 的前置任务先行、首条闭环推迟的代价。

## 2. 已批准约束、现行事实与本次建议（分层）

### 2.1 已批准/不可绕过

- 节点列表必须按登录用户可见范围过滤；分类计数和列表不得泄露无权节点。`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md:56-58,92`；`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-CORE-CONTRACT-RULES.md:9`。
- 离线节点资源指标必须显示缺失或更新时间，不能以 `0%` 假装实时；过滤后列表为空只能是空数组，不泄露总数。`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md:58`；`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-CORE-CONTRACT-RULES.md:22`。
- 任务目标/权限快照只证明提交时的目标集合，操作前和每台执行前仍需当前权限复核；此处只借用“每次请求重新校验”的原则，不把任务授权当成节点读取授权。`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/AGENT-TRANSPORT-AND-STATE-MACHINE.md:27,32`。

### 2.2 当前事实

1. r03 是**已接收的评审工件，不是冻结契约**：两个既有 GET（列表 `listNodesForTransfer`、详情 `getNodeDetailG2`），保留旧路径/字段/认证/错误媒体差异；D01–D07 与 P1-C10 仍 OPEN。`docs/contracts/revisions/g2-r03/README.md:11-13`；`docs/contracts/revisions/g2-r03/DECISIONS.md:3,7-13`；`docs/contracts/P1-CANDIDATE-REVIEW.md:43`。
2. 候选字段和示例已存在：列表 `NodeSummary` 必填 `id/name/online`，可选旧字段与可空 CPU/内存；详情 `NodeDetail` 必填 `nodeId/displayName/status/revision`；r03 另提议可选 `sampledAt`、列表可选 `status/revision/platform/agentVersion/lastSeenAt`。`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:18-114,115-192,217-302,304-352`；逐字段来源见 `docs/contracts/revisions/g2-r03/MATRIX.md:16-31`（本文件不重复抄写矩阵）。
3. Controller 目前只有 health 路由，没有节点 HTTP、登录/会话或管理 API；`apps/controller/main.go:17-35`；`apps/controller/health/handler.go:1-12`。
4. auth 已有离线默认拒绝策略与请求级不可变范围：`PrepareNodeReadScope(ctx, subjectID, PolicyReader)` 在无库存时先校验主体并读取一次当前策略，已知主体零授权可得到有效空范围，未知主体/坏策略失败；`NodeReadScope.CheckNode` 只检查精确节点，不持有策略或读取器，新请求必须重新准备。`internal/auth/node_read_scope.go:8-13,18-23,23-74,80-101`；`internal/auth/node_policy.go:15-22,69-78`；独立复审 `docs/development/reviews/AUTH-01B-REVIEW.md:9-19,29-36`。
5. 当前 `internal/nodes.Node` 有 `ID/Name/Address/Region/Provider/Tags/Status/LastSeenAt/Metrics`，**没有 revision/platform/agentVersion**；`Metrics.ObservedAt` 可以是零值，`Usage` 只在 online、有限值、有效且未过期时返回可用样本，但不检查 0–100。`internal/nodes/node.go:29-49,53-67`。`Visible` 只把纯拒绝过滤；策略/取消/未知错误会中止并返回 nil，不会成功返回部分列表。`internal/nodes/visibility.go:20-61,125-142`。
6. r03 已记录两个未关闭的旧入口限制：空库存时 `Visible` 不调用 Authorizer；逐节点检查可能读取不同策略快照，列表不是一次原子策略视图。`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:61-63`。
7. N03-A 已分配但**尚未交付/接收**：目标是新增“先预检、同一范围、后读库存”的离线节点读取用例，并保留空库存预检/共同策略/复制隔离。`docs/development/CODEX-N03A-WORK-PACKAGE.md:15-17,21-26,30-32,36-38`；`docs/development/TASK-BOARD.md:36`。本方案只能把它列为依赖，不能当作已有实现。
8. Web 已有只读列表/详情组件，但没有真实数据源：局部 `NodeListItemView` 要求 `revision/status/platform/agentVersion/lastSeenAt`，指标要求可解析 `sampledAt`；`candidate-adapter.ts` 只映射 Core 详情子集，`CandidateNodesSource` 没有真实实现。`apps/web/src/features/nodes/types.ts:40-50,66-78`；`apps/web/src/features/nodes/candidate-adapter.ts:30-55,81-107`；`apps/web/src/features/nodes/README.md:3-4,8-19`。
9. W05 加载容器尚未接收；W05-R01 的同 key 无效 load 恢复仍 OPEN，W05 未合入。`docs/development/reviews/FOLLOWUP-02-REVIEW.md:30-38,44-50`；`docs/development/TASK-BOARD.md:3`。不能用未提交的 W05 内容当首条闭环依赖。

### 2.3 不能靠补值进入闭环

- 不能把缺失的 `revision` 填成 `1/0`、时间戳或数组序号；不能把 `online=false` 猜成 `offline/suspended/revoked`；不能把响应时间、`lastSeenAt` 或 `now` 当作 `sampledAt`；不能用客户端自报 `subjectId` 或 `NodePolicy` 快照自认证。`docs/contracts/revisions/g2-r03/DECISIONS.md:7-13`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:19-29,41-53`。
- 当前 Web 必填 `revision/status`，因此旧列表 DTO 不能直接强转为 `NodeListItemView`；这是 R1-A 必须做局部类型/适配的原因，不是实现细节可忽略项。`apps/web/src/features/nodes/types.ts:66-78`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:35`。

## 3. R1-A 首版范围（建议）

**纳入：** 只读、只列表、legacy 兼容。

- 路径/操作：仅保留 `GET /api/v1/nodes`、`listNodesForTransfer`、`items` 包裹；不新增集合路由、不复制 operationId、不采用 r02 运行时 profile/header。
- 字段：保留 `id/name/online` 必填；可选输出内部确有来源的旧字段（建议先只含 `region/provider`）；**不输出** `status/revision/platform/agentVersion/lastSeenAt/sampledAt`；`ip/tags/countryCode` 暂不作为首版承诺，待 D07 字段敏感性和来源确认。旧 CPU/内存可作为可空值传递，但无 `sampledAt` 的数值不能在 Web 显示成新鲜读数。
- 详情：**不在 R1-A**。当前 Node 没有 revision，`NodeDetail` 无法合法生成。
- 身份/授权：服务端从可信会话/Bearer 绑定 subject，不能读取客户端 subject；每请求先 `PrepareNodeReadScope`（空库存也要）并只使用一次范围；后续逐节点 `CheckNode` 使用同一范围。具体组合优先复用通过验收的 N03-A；未接收前不得由 Controller 自行拼一套。
- 错误：列表整体未认证/策略不可用/非法输入分别映射；已被拒绝的节点不进入 items；不返回部分成功。推荐 401/403/503（400 仅限真实非法 query），成功/错误 `Cache-Control: no-store`；细节待 D06。
- Web：新的 legacy 列表行类型/适配（id/name/online + 可选 region/provider + 无时间指标缺失），复用现有展示质量规则；不显示详情，不使用假 revision。需要 Web owner 允许一个局部 legacy ViewModel，而不是修改已验收的 `NodeListItemView` 语义。
- 分页/排序：首版沿用原列表无分页/total 的行为；保留服务端可见集合的稳定顺序，不新增 cursor/limit 契约。

**R1-A 明确不关闭：** D02/D03/D04、详情、富化列表、字段级敏感性、真实浏览器验收、跨进程撤权原子性。它只把“可信身份 → 可见节点列表”闭环做实。

## 4. R1-B 范围（建议）

在权威来源可用后：

- 列表可选增加 `status/revision/platform/agentVersion/lastSeenAt`；详情增加 `NodeDetail` 的必填 `status/revision` 与可选 `platform/agentVersion/lastSeenAt`；列表和详情可选共同 `sampledAt`。
- 需要新增/确认：节点元数据 revision 的存储与原子读、五状态权威来源与优先级、共同采样时间来源、字段级敏感规则；不在本方案虚构这些来源。
- Web 使用现有五状态/`NodeListItemView`，不再保留 legacy 视图分支（或将其限定为兼容读取，不作为新数据的第二权威）。

**替代路线（不推荐作为首版）：** 直接把 R1-A/R1-B 合并，先补元数据源再实现列表+详情。优点是只维护一套 Web 类型；代价是首条闭环延迟，且需要 D02/D03/D04 与存储/字段来源先行批准。

## 5. D01–D07 收敛建议

| 决定 | 当前事实/约束 | 建议 | 仍需批准/证据 |
|---|---|---|---|
| D01 旧客户端兼容 | 旧列表已有 `listNodesForTransfer`、`items`、`id/name/online`、可选旧字段；r03 保留，旧候选不可改 | R1-A 保持旧形状和旧错误媒体；不新增第二路由/profile；富化字段只在 R1-B 经批准后追加。`docs/contracts/revisions/g2-r03/MATRIX.md:9-31`、`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:31-35` | 真实旧客户端对未知字段/扩展名冲突/新增状态码的行为探测；契约 owner 统一修订 |
| D02 状态权威 | 五状态为权威；有 status 时 online 必须等于 status=online；false 不能反推具体状态 | R1-A 不输出 status，只保留旧 online 布尔；R1-B 只从权威来源输出 status；矛盾来源拒绝发布该富化结果，不猜状态 | 暂停/吊销与连接在线的优先级、来源冲突处理；产品+安全+节点 owner |
| D03 revision | 列表可选、详情必填 >=1；当前 Node 无 revision | R1-A 不输出/不构造 revision，详情延后；R1-B 增加权威元数据 revision 与原子读，不填 1、不复用指标时间；明确 JS 大整数精度/ETag 未来表示 | 元数据 revision 来源、递增边界、存储原子性；节点+存储+契约 owner |
| D04 采样/时效 | 列表旧 CPU/内存可空但无时间；Web 需要共同 sampledAt 才显示新鲜；Go Usage 与 Web 0–100/陈旧策略不完全相同（`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:23-27`、`internal/nodes/node.go:53-67`） | R1-A 保留可空旧值但不显示为新鲜；R1-B 只在 CPU/内存来自同一真实样本时输出共同 sampledAt，缺时间即缺失；新鲜度由调用方显式 now/maxAge 注入，不用 lastSeen/now 补齐；明确 0–100 域 | 公共 sampledAt vs 逐指标时间、时效阈值、Go/Web 语义统一；产品+节点+Web |
| D05 身份/凭据 | 当前无认证/会话；列表原 Bearer-only，详情 Cookie OR Bearer；客户端 subject 不是身份 | Web 首版采用服务端会话 Cookie（HttpOnly/SameSite，CSRF 由未来认证设计确认）；非浏览器可用 Bearer，但双凭据如何拒绝、列表是否新增 Cookie 支持须契约批准；绝不信任客户端 subject | 认证/会话/CSRF/Token 撤销 ADR、Cookie 与列表契约变更；安全+总控 |
| D06 错误/防枚举 | 无 HTTP 实现；候选新增列表 400/403/503，详情保留 Problem/通用码，404 统一是提案 | 推荐列表：401 未认证、403 整体拒绝、503 策略不可用、400 仅非法 query；详情缺失/不可见统一 404；其余 409/412/428/429 不赋予 GET 新语义；错误不返回节点 ID/原始异常，所有响应 no-store | 真实错误映射、Rate limit/超时/取消的 408/504/503 选择、旧客户端错误体兼容；安全+契约 |
| D07 空库存/共同策略/撤权/缓存 | `PrepareNodeReadScope` 可预检空库存；旧 `Visible` 空库存不读策略、逐节点读取非原子；N03-A 未接收；无 HTTP 缓存 | 每请求先预检再读库存；一次请求只准备一次范围并复用到所有节点；空库存也预检；新请求重新预检并看见撤权；隐藏节点不进入列表/计数；成功与错误 no-store，身份/来源变化清除旧 VM | N03-A 接收、策略与库存同一事务/原子输出、撤权与序列化时序、真实缓存测试；安全+节点+存储 |

D08 的工具/注释协调已完成，不在此重复；它不批准 D01–D07 或运行时方案。`docs/contracts/revisions/g2-r03/DECISIONS.md:14`。

## 6. 负责人需选择的问题（最多 3 个，未回复不构成批准）

### Q1 首版范围：R1-A list-only，还是 R1-full list+detail？

- **推荐：R1-A。** 先闭环可信身份、可见过滤、空库存预检和 legacy 列表；详情/富化等元数据源。范围小、可先验收授权与不泄露，但也意味着首版 UI 没有详情、状态/版本和新鲜指标。
- 替代：R1-full。一次完成列表+详情+富化；体验完整，但必须先解决节点 revision/status/platform/agentVersion 来源、D02–D04 与存储原子性，首条闭环明显推迟。

### Q2 Web 首版凭证：服务端会话 Cookie，还是 Bearer-only API 先行？

- **推荐：服务端会话 Cookie（HttpOnly/SameSite）绑定可信主体，浏览器不保存 Token 原文；Bearer 仅供非浏览器集成，并明确双凭据拒绝规则。** 需要批准列表操作增加 Cookie 认证（原列表仅 Bearer）及会话/CSRF/撤销设计。
- 替代：Bearer-only 首版，保持列表原认证不变，但需要另行解决 Web 的安全 Token 获取/存储；可能只能先做 API/CLI 闭环，不能算完整 Web 闭环。

### Q3 节点元数据权威：现在新增，还是等 R1-B 再补？

- **推荐：把 revision/status/platform/agentVersion 与共同采样时间作为 R1-B 的显式前置来源任务，先批准来源与存储方向；R1-A 不伪造这些字段。** 代价是多一个节点/存储任务，但避免把 `1`、当前时间或 `false` 写进契约。
- 替代：长期只做 legacy list，不提供详情/富化；实现快，但无法完成产品要求的详情、状态与新鲜指标，且需要维护 legacy Web 分支。

## 7. 实施拆分与依赖（建议）

| 顺序 | 任务 | 依赖/并行 | 完成证据 |
|---|---|---|---|
| 0 | 负责人批准 Q1–Q3 与 D01–D07 中选定的范围；契约 owner 统一修订原操作 | 必须先于 HTTP 接线；不能把 r03 候选当冻结 | 批准记录、统一修订/兼容说明、契约静态检查 |
| 1 | N03-A 验收（预检先行的离线读取用例） | Codex 原任务；可与本方案文档并行 | 真实函数测试、空库存预检、共同范围、复制隔离；未接收前不得由 Controller 复用 |
| 2 | 认证/会话/凭据实现与主体绑定 | 依赖 Q2/D05；可与机制设计并行 | 伪造 subject 被忽略、过期/撤销拒绝、双凭据规则测试 |
| 3 | Controller R1-A 列表端点：scope → inventory → check → legacy DTO/错误/no-store | 依赖 0/1/2；与元数据任务可并行 | httptest 正反用例、空库存、策略失败、隐藏计数、取消 |
| 4 | Web R1-A legacy source/列表 VM | 依赖 3；不要依赖未接收的 W05；与其协调 | 组件/适配测试、源切换首提交、无网络、指标缺失不显示新鲜 |
| 5 | 节点元数据源（revision/status/platform/agentVersion/共同采样） | 依赖 Q3/D02–D04，可与 3/4 并行；阻塞详情 | 权威来源、原子读、版本/精度/新鲜度测试 |
| 6 | R1-B 富化列表 + 详情 + 当前 Web ViewModel | 依赖 0/2/5，可能部分依赖 3 | 详情字段不伪造、状态一致性、旧客户端兼容、错误/缓存测试 |
| 7 | 端到端验收（HTTP/浏览器/安全） | 依赖 3/4（及 R1-B 时的 5/6） | 真实验收输出；本轮不运行、不声称已完成 |

可并行：认证设计、N03-A、元数据源、契约统一；必须先等：owner 批准、N03-A 接收、身份绑定、统一修订。W05 可以并行但不作为首版依赖。

## 8. 缺失证据与不声称事项

- 缺失：认证/会话/CSRF/Token 撤销 ADR、可信主体绑定实现；节点 revision 来源/存储/原子性；五状态权威来源与优先级；共同采样时间与时效阈值；真实旧客户端兼容探测；HTTP 错误/防枚举/限流映射；策略+库存事务与撤权原子性；W05 接收；N03-A 接收；真实 HTTP/浏览器/cache 测试。
- 本文件不批准 D01–D07/P1-C10、不冻结 G2、不创建新 API revision、不实现代码、不声称静态/离线 PASS 等于真实接口或浏览器验收。r03 候选、原 Merged/Core/r02、现有实现和批准状态均保持不变。
