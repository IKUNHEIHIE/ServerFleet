# READ-PLAN-01 · 首条只读闭环可批准方案

状态：**PROPOSED / 待负责人批准；G2 未冻结**。本文件只提出建议、依据、代价与缺失证据，不修改既有代码、候选、D01–D07 或 P1-C10 状态，也不构成 HTTP/身份/契约冻结授权。

基线：`98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`（本方案原历史基线）。当前主线核对提交：`ddf8c5b`（N03-A 已接收 `7ef7877`/`0ec6221`；W05 已接收 `b44352f`/`ddf8c5b`）。历史引用保留 98a1f6b；标 `[当前主线]` 的事实来自主目录已提交记录，不要求本分支同步 main。

## 1. 结论与推荐

**推荐分两步，不把“列表 + 详情 + 富化字段”一次做成。**

1. **R1-A（建议先批准并实施）：legacy 兼容的只读列表闭环。** 只实现既有 `GET /api/v1/nodes`，保持旧路径、`listNodesForTransfer`、`{items:[...]}`、必填 `id/name/online` 及既有 `onlineOnly:boolean`（默认 false）；可信身份/策略预检通过后，先得到授权可见集合，再按 `onlineOnly` 过滤，只返回可见节点的旧字段子集。列表不新增 `status/revision/platform/agentVersion/lastSeenAt/sampledAt`，不提供详情，不显示未采样的 CPU/内存为新鲜值。Web 使用一个**新的局部 legacy 列表 ViewModel/适配**（当前 `NodeListItemView` 要求 `revision/status`，不能直接承载旧列表行）。
2. **R1-B（随后）：在详情所需的 revision、状态权威与共同采样时间可用后，启用详情和富化列表。** 详情真正需要 `status/revision`；当前 Node 已有 `Status` 但权威来源/优先级未批准（D02），真正缺少 revision（D03）；`platform/agentVersion` 只是公开可选字段，内部缺失时允许省略/local VM 为 null，**不是详情或 R1-B 的非空前置条件**；`Metrics.ObservedAt` 已存在，但共同 `sampledAt` 语义/公开形状仍待 D04。不能用 `1`、当前时间或 `false` 补齐。

不建议直接做“含详情的完整 DTO”作为第一版：它会同时阻塞在 D02（状态权威）与 D03（revision 来源）、D04（共同采样语义）上，且当前 Web 类型无法接收缺少 revision 的旧列表；这并不要求 `platform/agentVersion` 必须有非空值。若负责人选择一次完成，须接受 R1-B 的前置任务先行、首条闭环推迟的代价。

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
5. 当前 `internal/nodes.Node` 有 `ID/Name/Address/Region/Provider/Tags/Status/LastSeenAt/Metrics`；**真正缺少的是 revision/platform/agentVersion**。`Status` 与 `Metrics.ObservedAt` 已存在，但 Status 的权威来源/暂停吊销优先级、ObservedAt 到公共共同采样时间的语义与公开形状仍待确认；`Usage` 只在 online、有限值、有效且未过期时返回可用样本，但不检查 0–100。`internal/nodes/node.go:29-49,53-67`。`Visible` 只把纯拒绝过滤；策略/取消/未知错误会中止并返回 nil，不会成功返回部分列表。`internal/nodes/visibility.go:20-61,125-142`。
6. r03 已记录两个未关闭的旧入口限制：空库存时 `Visible` 不调用 Authorizer；逐节点检查可能读取不同策略快照，列表不是一次原子策略视图。`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:61-63`。
7. **N03-A 已在当前主线接收**（历史 98a1f6b 基线下当时尚未接收）：`ReadVisibleInventory` 每次真实调用 `PrepareNodeReadScope`，成功后才读库存，再用同一范围检查全部节点；空库存也先预检，主体/策略失败不读库存，已知零授权返回非 nil 空集合；只接收离线用例，不代表 Controller/HTTP 接线。`docs/development/reviews/N03-A-REVIEW.md:7-15,17-21,31-40`（[当前主线] `7ef7877`/`0ec6221`）；历史任务单 `docs/development/CODEX-N03A-WORK-PACKAGE.md:15-17,21-26,30-32,36-38`。
8. Web 已有只读列表/详情组件，但没有真实数据源：局部 `NodeListItemView` 要求 `revision/status/platform/agentVersion/lastSeenAt`，指标要求可解析 `sampledAt`；`candidate-adapter.ts` 只映射 Core 详情子集，`CandidateNodesSource` 没有真实实现。`apps/web/src/features/nodes/types.ts:40-50,66-78`；`apps/web/src/features/nodes/candidate-adapter.ts:30-55,81-107`；`apps/web/src/features/nodes/README.md:3-4,8-19`。
9. **W05 已在当前主线接收**（历史 98a1f6b 基线下当时未接收，W05-R01 曾 OPEN）：接收的是可注入局部 `NodeListItemView` 来源的隔离加载容器，W05-R01/R02 已 CLOSED，真实 fetch/App 接线仍未实现。`docs/development/reviews/FOLLOWUP-03-REVIEW.md:14-20,46`（[当前主线] `b44352f`/`ddf8c5b`）。历史未接收叙述保留在 98a1f6b 语境，不据此声称真实 HTTP/浏览器闭环。

### 2.3 字段存在、权威与公开形状（避免把三类问题混为一谈）

| 字段/概念 | 当前内部结构 | 权威/公开状态 | 对首版的影响 |
|---|---|---|---|
| `status` | **已有** `internal/nodes.Status` 五值 | 内部字段存在；五状态权威来源、暂停/吊销与连接在线优先级仍是 **D02 待批提案**，不是已批准事实 | R1-A 不输出 status；R1-B 只有在 D02 批准且来源一致后输出 |
| `revision` | **缺失** | D03 待批；候选详情必填 `>=1`、列表可选；没有来源/存储/精度结论 | 详情阻塞在 R1-B；R1-A 不输出、不填 1/时间/序号 |
| `platform`/`agentVersion` | **缺失** | 公开 DTO 为**可选** string；Web 局部值可为 null | 不是详情/R1-B 的非空前置；公开 DTO 缺失时省略，局部 VM 可用 null，不阻塞 |
| `lastSeenAt` | 已有 `LastSeenAt` | 公开可选 date-time；与采样时间不是同一概念 | 可用于最近在线展示；不得当 sampledAt |
| `Metrics.ObservedAt` | **已有**原始采样时间（可为零值） | 只是一次原始样本时间；**共同 sampledAt 的公共形状/时效语义仍是 D04 待批提案** | R1-A 无公共 sampledAt 时不得显示为新鲜；R1-B 只在同一真实样本时输出 |
| `online` | 可由内部 Status 计算 | 旧列表必填布尔；D02 提案的 `online==status==online` 尚未批准 | R1-A 只保留旧布尔，不从 false 猜具体状态 |
| CPU/内存 | 已有原始 Metrics | 旧列表可空 0–100；详情可选新增仍是提案；无时间不能称新鲜 | R1-A 可传可空旧值，Web 标为缺时间/缺失；不夹取、不补 0 |
| `region/provider/address/tags` | 已有；`countryCode` 内部缺失 | 旧列表可选字段；IP/tags 的字段级敏感性仍待 D07 | R1-A 建议只含低敏 `region/provider`；`ip/tags/countryCode` 不作为首版承诺 |

### 2.4 不能靠补值进入闭环

- 不能把缺失的 `revision` 填成 `1/0`、时间戳或数组序号；不能把 `online=false` 猜成 `offline/suspended/revoked`；不能把响应时间、`lastSeenAt` 或 `now` 当作 `sampledAt`；不能用客户端自报 `subjectId` 或 `NodePolicy` 快照自认证。`docs/contracts/revisions/g2-r03/DECISIONS.md:7-13`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:19-29,41-53`。
- 当前 Web 必填 `revision/status`，因此旧列表 DTO 不能直接强转为 `NodeListItemView`；这是 R1-A 必须做局部类型/适配的原因，不是实现细节可忽略项。`apps/web/src/features/nodes/types.ts:66-78`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:35`。

## 3. R1-A 首版范围（建议）

**纳入：** 只读、只列表、legacy 兼容。

- 路径/操作：仅保留 `GET /api/v1/nodes`、`listNodesForTransfer`、`items` 包裹，以及既有 `onlineOnly:boolean` 查询参数（默认 false；r03 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:24-29`，原 Merged `api/openapi/g2-merged-candidate.yaml:94-98`）；不新增集合路由、不复制 operationId、不采用 r02 运行时 profile/header。
- 执行顺序：**先认证/策略预检（空库存也执行）→ 读库存并得到授权可见集合 → 再对可见集合应用 `onlineOnly` → 投影字段**。`onlineOnly=true` 只保留可见集合中旧 online=true 的节点；false/省略返回全部可见节点（含离线）；隐藏的在线节点在 true/false 下都不得出现，也不得进入任何计数。过滤后为空返回 `items:[]`，不跳过预检、不泄露总数；策略故障不能变成 200 空列表。坏 query 的解析/HTTP 映射待 D06 批准，本方案不自行决定。
- 字段：保留 `id/name/online` 必填；可选输出内部确有来源的旧字段（建议先只含 `region/provider`）；**不输出** `status/revision/platform/agentVersion/lastSeenAt/sampledAt`；`ip/tags/countryCode` 暂不作为首版承诺，待 D07 字段敏感性和来源确认。旧 CPU/内存可作为可空值传递，但无 `sampledAt` 的数值不能在 Web 显示成新鲜读数。
- 详情：**不在 R1-A**。当前 Node 真正缺 revision；Status 虽存在，D02 权威/优先级仍未批准，因此 `NodeDetail` 不能合法生成。
- 身份/授权：服务端从可信会话/Bearer 绑定 subject，不能读取客户端 subject；每请求先 `PrepareNodeReadScope`（空库存也要）并只使用一次范围；后续逐节点 `CheckNode` 使用同一范围。优先复用当前主线已接收的 N03-A `ReadVisibleInventory`（`docs/development/reviews/N03-A-REVIEW.md:7-15,17-21,31-40`，[当前主线]），不让 Controller 自行复制授权顺序。
- 错误：列表整体未认证/策略不可用/非法输入分别映射；已被拒绝的节点不进入 items；不返回部分成功。推荐 401/403/503（400 仅限真实非法 query），成功/错误 `Cache-Control: no-store`；细节待 D06。
- Web：新增 legacy 列表行类型/适配（id/name/online + 可选 region/provider + 无时间指标缺失），复用现有展示质量规则；不显示详情，不使用假 revision。当前主线已接收的 W05 `NodeLoader`（[当前主线] `b44352f`）提供隔离 loading/错误/来源世代，但**不提供真实 fetch 或公开 DTO 适配**，仍需要 R1-A source/legacy VM；不修改已验收的 `NodeListItemView` 语义。
- 分页/排序：首版沿用原列表无分页/total 的行为；保留服务端可见集合的稳定顺序，不新增 cursor/limit 契约。

**R1-A 明确不关闭：** D02/D03/D04、详情、富化列表、字段级敏感性、真实浏览器验收、跨进程撤权原子性。它只把“可信身份 → 可见节点列表（含 onlineOnly）”闭环做实。

## 4. R1-B 范围（建议）

在真实缺失字段/权威语义可用后：

- 列表可选增加 `status/revision/platform/agentVersion/lastSeenAt`；详情增加 `NodeDetail` 的必填 `status/revision` 与可选 `platform/agentVersion/lastSeenAt`；列表和详情可选共同 `sampledAt`。
- 详情真正的前置是 `status`（D02）与 `revision`（D03）；`platform/agentVersion` 仍是可选公开字段，缺失可省略、Web 局部值可为 null，**不要求非空来源，也不阻塞 R1-B**。`Metrics.ObservedAt` 已存在，但公共共同 `sampledAt`/时效策略待 D04；不在本方案虚构来源。
- Web 使用现有五状态/`NodeListItemView`；当前主线已接收 W05 加载容器，但真实 fetch/DTO 适配仍需单独任务；legacy 分支可保留为兼容读取，不作为新数据的第二权威。

**替代路线（不推荐作为首版）：** 直接把 R1-A/R1-B 合并，先补元数据源再实现列表+详情。优点是只维护一套 Web 类型；代价是首条闭环延迟，且需要 D02/D03/D04 与存储/字段来源先行批准。

## 5. D01–D07 收敛建议

| 决定 | 当前事实/约束 | 建议 | 仍需批准/证据 |
|---|---|---|---|
| D01 旧客户端兼容 | 旧列表已有 `listNodesForTransfer`、`items`、`id/name/online`、`onlineOnly:boolean` 默认 false、可选旧字段；r03 保留，旧候选不可改 | R1-A 保持旧形状、旧错误媒体及 `onlineOnly` 参数/默认语义；先预检和授权可见集合，再过滤；不新增第二路由/profile；富化字段只在 R1-B 经批准后追加。`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:24-29`、`api/openapi/g2-merged-candidate.yaml:94-98`、`docs/contracts/revisions/g2-r03/MATRIX.md:9-31`、`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:31-35` | 真实旧客户端对未知字段/扩展名冲突/新增状态码的行为探测；`onlineOnly` 坏值解析与 HTTP 码待 D06；契约 owner 统一修订 |
| D02 状态权威 | `Status` 内部字段已存在；五状态权威、`online==status=online` 与暂停/吊销优先级来自 **r03 待批提案**，不是已批准事实 | R1-A 不输出 status，只保留旧 online 布尔；R1-B 只有在 D02 批准且来源一致后输出 status；矛盾来源拒绝发布该富化结果，不猜状态 | 五状态来源/优先级、来源冲突处理；产品+安全+节点 owner |
| D03 revision | 列表可选、详情必填 >=1；**当前 Node 真正缺 revision** | R1-A 不输出/不构造 revision，详情延后；R1-B 增加权威元数据 revision 与原子读，不填 1、不复用指标时间；明确 JS 大整数精度/ETag 未来表示 | 元数据 revision 来源、递增边界、存储原子性；节点+存储+契约 owner |
| D04 采样/时效 | 列表旧 CPU/内存可空但无公共时间；`Metrics.ObservedAt` **已存在**但只是原始样本时间；Web 需要共同 sampledAt 才显示新鲜；Go Usage 与 Web 0–100/陈旧策略不完全相同（`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:23-27`、`internal/nodes/node.go:53-67`） | R1-A 保留可空旧值但不显示为新鲜；R1-B 只在 CPU/内存来自同一真实样本时输出共同 sampledAt，缺时间即缺失；新鲜度由调用方显式 now/maxAge 注入，不用 lastSeen/now 补齐；明确 0–100 域 | 公共 sampledAt vs 逐指标时间、时效阈值、Go/Web 语义统一；产品+节点+Web |
| D05 身份/凭据 | 当前无认证/会话；列表原 Bearer-only，详情 Cookie OR Bearer；客户端 subject 不是身份 | Web 首版采用服务端会话 Cookie（HttpOnly/SameSite，CSRF 由未来认证设计确认）；非浏览器可用 Bearer，但双凭据如何拒绝、列表是否新增 Cookie 支持须契约批准；绝不信任客户端 subject | 认证/会话/CSRF/Token 撤销 ADR、Cookie 与列表契约变更；安全+总控 |
| D06 错误/防枚举 | 无 HTTP 实现；候选新增列表 400/403/503，详情保留 Problem/通用码，404 统一是提案 | 推荐列表：401 未认证、403 整体拒绝、503 策略不可用、400 仅非法 query；详情缺失/不可见统一 404；其余 409/412/428/429 不赋予 GET 新语义；错误不返回节点 ID/原始异常，所有响应 no-store；`onlineOnly` 坏值只在批准后映射 | 真实错误映射、Rate limit/超时/取消的 408/504/503 选择、旧客户端错误体兼容；安全+契约 |
| D07 空库存/共同策略/撤权/缓存 | N03-A 已在当前主线接收（离线）：`ReadVisibleInventory` 每次预检后再读库存、空库存不跳过、同一范围检查全部节点；旧 `Visible` 的空库存/逐节点限制仍是历史限制；无 HTTP 缓存 | 每请求先预检再读库存；复用 N03-A 用例，一次请求只准备一次范围并复用到所有节点；空库存也预检；新请求重新预检并看见撤权；隐藏节点不进入列表/计数；成功与错误 no-store，身份/来源变化清除旧 VM | Controller 接线与真实 HTTP；策略与库存同一事务/原子输出、撤权与序列化时序、真实缓存测试；安全+节点+存储 |

D08 的工具/注释协调已完成，不在此重复；它不批准 D01–D07 或运行时方案。`docs/contracts/revisions/g2-r03/DECISIONS.md:14`。

## 6. 负责人需选择的问题（最多 3 个，未回复不构成批准）

### Q1 首版范围：R1-A legacy 列表，还是 R1-full 列表+详情？

- **推荐：R1-A。** 先闭环可信身份、可见过滤、`onlineOnly` 兼容和 legacy 列表；范围小、可先验收授权与不泄露，但首版 UI 没有详情、状态/版本和新鲜指标。
- 替代：R1-full。一次完成列表+详情+富化；体验完整，但详情真正需要 `status`（D02）与 `revision`（D03）来源，且 `platform/agentVersion` 缺失时只能是可省略/null 的可选字段；这仍需节点元数据来源和存储原子性，首条闭环明显推迟。选择 R1-full 不等于要求 `platform/agentVersion` 必须有非空值。

### Q2 Web 首版凭证：服务端会话 Cookie，还是 Bearer-only API 先行？

- **推荐：服务端会话 Cookie（HttpOnly/SameSite）绑定可信主体，浏览器不保存 Token 原文；Bearer 仅供非浏览器集成，并明确双凭据拒绝规则。** 需要批准列表操作增加 Cookie 认证（原列表仅 Bearer）及会话/CSRF/撤销设计。
- 替代：Bearer-only 首版，保持列表原认证不变，但需要另行解决 Web 的安全 Token 获取/存储；可能只能先做 API/CLI 闭环，不能算完整 Web 闭环。

### Q3 状态权威：首版继续只用 legacy `online` 布尔，还是现在就批准五状态为权威？

- **推荐：首版只用 legacy `online` 布尔，`status` 保持在 R1-B 前不输出；** 五状态/`online==status=online` 及暂停/吊销优先级继续作为 D02 待批提案，不在首版默认成已批准事实。
- 替代：现在批准五状态为权威来源，并明确暂停/吊销与连接在线的优先级；这样 R1-B 才能输出 `status`（以及详情所需的必填 status）。若 Q1 选择 R1-full，本项必须选此替代；若选 R1-A，可维持推荐选项并把 status 留待 R1-B。

## 7. 实施拆分与依赖（建议）

| 顺序 | 任务 | 依赖/并行 | 完成证据 |
|---|---|---|---|
| 0 | 负责人批准 Q1–Q3 与 D01–D07 中选定的范围；契约 owner 统一修订原操作（含 `onlineOnly` 语义） | 必须先于 HTTP 接线；不能把 r03 候选当冻结 | 批准记录、统一修订/兼容说明、契约静态检查 |
| 1 | N03-A 离线读取用例（当前主线已接收 `7ef7877`/`0ec6221`） | 已接收；本任务不再等待其实现，但 Controller 仍需基于它做接线测试 | `ReadVisibleInventory` 预检/库存顺序、空库存、共同范围/复制隔离的既有复审证据 |
| 2 | 认证/会话/凭据实现与主体绑定 | 依赖 Q2/D05；可与机制设计并行 | 伪造 subject 被忽略、过期/撤销拒绝、双凭据规则测试 |
| 3 | Controller R1-A 列表端点：scope → inventory → check → 可见集合 → `onlineOnly` → legacy DTO/错误/no-store | 依赖 0/1/2；与元数据任务可并行 | httptest 正反用例、空库存、策略失败、隐藏计数、onlineOnly 全部取值、取消 |
| 4 | Web R1-A legacy source/列表 VM | 依赖 3；当前主线 W05 `NodeLoader` 已接收（`b44352f`）可复用 loading/世代隔离，但真实 fetch/DTO 适配仍需实现；不要读取 W05 未提交内容 | 组件/适配测试、源切换首提交、无网络、指标缺失不显示新鲜 |
| 5 | 节点元数据源：revision（详情必需）、status 权威/优先级（D02）、共同采样时间（D04）；`platform/agentVersion` 仅可选 | 依赖 Q3/D02–D04，可与 3/4 并行；阻塞详情/R1-B | 权威来源、原子读、版本/精度/新鲜度测试；platform/agent 公开 DTO 缺失时省略，局部 VM 可用 null，不阻塞 |
| 6 | R1-B 富化列表 + 详情 + 当前 Web ViewModel | 依赖 0/2/5，可能部分依赖 3 | 详情字段不伪造、状态一致性、旧客户端兼容、错误/缓存测试 |
| 7 | 端到端验收（HTTP/浏览器/安全） | 依赖 3/4（及 R1-B 时的 5/6） | 真实验收输出；本轮不运行、不声称已完成 |

可并行：认证设计、元数据源、契约统一、Web source 准备；必须先等：owner 批准、身份绑定、统一修订。N03-A 已接收；W05 已接收但只提供隔离加载接缝，真实 fetch/App 接线仍不在当前实现。

## 8. 缺失证据与不声称事项

- 缺失：认证/会话/CSRF/Token 撤销 ADR、可信主体绑定实现；节点 revision 来源/存储/原子性；五状态权威来源与优先级（D02 仍未批准）；共同采样时间/时效阈值；真实旧客户端及 `onlineOnly` 坏值解析兼容探测；HTTP 错误/防枚举/限流映射；策略+库存事务与撤权原子性；真实 HTTP/浏览器/cache 测试。
- 当前主线已接收 N03-A 离线用例与 W05 隔离加载容器（[当前主线] `7ef7877`/`0ec6221` 与 `b44352f`/`ddf8c5b`），但两者都不等于 Controller/HTTP/真实 fetch/浏览器验收；历史 98a1f6b 下的未接收状态只作为基线语境保留。
- 本文件不批准 D01–D07/P1-C10、不冻结 G2、不创建新 API revision、不实现代码、不声称静态/离线 PASS 等于真实接口或浏览器验收。r03 候选、原 Merged/Core/r02、现有实现和批准状态均保持不变。
