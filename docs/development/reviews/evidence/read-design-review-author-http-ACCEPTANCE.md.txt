# READ-HTTP-DESIGN-01 · A1 验收计划

状态：**PROPOSED / 待审查**。本文定义 A1 只读列表的可执行验收条件；没有实现 Controller handler、认证、真实 policy/inventory 来源或缓存。除标注的历史离线/静态证据外，HTTP/认证/浏览器/代理/存储均为 UNTESTED。基线 `4196b2e8ab9c389ae0830bf56f905d9bafa00d05`。

## 证据等级

| 标记 | 含义 |
|---|---|
| `OFFLINE-ACCEPTED` | 已有已提交复审的离线函数/模块证据；不是 HTTP 或身份证据 |
| `STATIC-CONTRACT` | 候选 YAML/矩阵/负向 lint 或纯文档核对；不证明服务器行为 |
| `COMPONENT` | Web 组件/fixture 测试或历史浏览器证据；不是真实 HTTP/会话/跨主体证据 |
| `NONE` | 当前无实现/无证据 |
| `BLOCKED` | 依赖未批准决定、未接收接口或未实现来源 |
| `UNTESTED` | 真实链路未执行 |

## 验收矩阵

| ID | 验收项 | 输入/时序与可观察结果 | 当前证据 | 缺失/负责模块 | 状态 |
|---|---|---|---|---|---|
| A1-01 | 可信认证与双凭据 | 无/无效/过期/撤销 Cookie 或 Bearer → 401、无 items、策略和库存调用均为 0；同一请求同时送 Cookie+Bearer → 401 固定错误，不静默选择；body/query/header 自报 subject/角色不改变服务端主体；错误媒体为列表旧 `Error`，no-store。 | 当前无认证；AUTH-01B 只验证可信 subject 字符串与 scope（`docs/development/reviews/AUTH-01B-REVIEW.md:9-19,29-36`）；Controller 只有 health（`apps/controller/main.go:17-35`）。 | READ-AUTH-DESIGN-01、Controller middleware；D05；真实 HTTP | BLOCKED/UNTESTED |
| A1-02 | onlineOnly 解析与优先级 | 省略/false → 全部授权可见节点（含离线）；true → 仅在线；空值、重复、`1/0/TRUE/其他` → 400 `INVALID_QUERY`；坏 query 时策略/库存调用 0；认证失败优先 401；未知其他 query 忽略；不默认宽松 ParseBool。 | 原 Merged/r03 都有 `onlineOnly` 默认 false（`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:24-29`、`api/openapi/g2-merged-candidate.yaml:94-98`）；无 parser/HTTP 测试。 | Controller query parser；D06；旧客户端探测 | STATIC-CONTRACT；HTTP UNTESTED |
| A1-03 | 空库存也必须预检 | 空库存 + 未知主体/策略失败 → 403/503，库存来源调用 0；已知主体零授权 → 200 `{items:[]}`，策略/库存各一次；不能因空库存跳过预检或返回未知主体成功空。 | N03-A 离线已接收：`ReadVisibleInventory` 先预检后库存，空库存不绕过（`docs/development/reviews/N03-A-REVIEW.md:7-15,17-21,31-40`；`internal/nodes/read_usecase.go:27-59`）；旧 `Visible` 空集合限制仍保留（`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:61-63`）。 | Controller 接线；D07；真实 HTTP | OFFLINE-ACCEPTED；HTTP UNTESTED |
| A1-04 | 同次 scope 与新请求撤权 | 每请求策略读取一次、库存一次；预检后策略来源变化不改变本次；新请求重新预检看见撤权；不缓存 scope/列表；策略/库存失败不返回旧允许或部分 items。 | AUTH-01B scope 单次读取/复制/新 Prepare 撤权（`internal/auth/node_read_scope.go:23-74,80-101`；复审 `docs/development/reviews/AUTH-01B-REVIEW.md:9-19`）；N03-A 同范围（`docs/development/reviews/N03-A-REVIEW.md:7-15,17-21`）。 | Controller 组合；策略/库存来源当前性；D07；存储证据 | OFFLINE-ACCEPTED；HTTP/STORAGE UNTESTED |
| A1-05 | 先授权可见集合再 onlineOnly | 库存含可见在线 A、可见离线 B、隐藏在线 H；省略/false 只返回 A/B；true 只返回 A；H 在任何取值下不出现、不计数；过滤后空（全部离线+true）为 200 `{items:[]}`，策略/库存已执行；策略故障不能变成 200 空列表。 | 无 HTTP；`onlineOnly` 仅为候选参数；N03-A 授权后输出用于后续 Filter/Classify（`docs/development/reviews/N03-A-REVIEW.md:9-15,17-21,31-40`）；没有过滤顺序实现。 | Controller handler；D01/D06/D07；真实调用计数 | STATIC-CONTRACT；HTTP UNTESTED |
| A1-06 | 字段白名单与禁止伪造 | 成功 items 只允许 `id/name/online/region/provider/cpuPercent/memoryPercent`；无 `status/revision/platform/agentVersion/lastSeenAt/sampledAt/ip/tags/countryCode`；id/name/online 缺失/空/控制字符/重复 → 500 整请求失败、无部分 items；可选 region/provider 空/非法 → 省略；不补造/不 trim ID/不从名称生成 ID。 | r03 `NodeSummary` 字段/必填/可选形状见 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:217-302`；当前 Node 有 Status/Metrics 但真正缺 revision/platform/agentVersion（`internal/nodes/node.go:29-49`）；无投影实现。 | Controller 投影；D01/D02/D04；契约审查 | BLOCKED；HTTP UNTESTED |
| A1-07 | online 的可信来源与冲突 | `online` 必须来自可信库存来源；当前 Node 仅有 Status，映射 `Status==online→true`、其他合法状态→false 只是待 D02 批准的 legacy 提案；若来源同时有 online/status 且矛盾 → 500；不得用 false 推断 offline/suspended/revoked，也不得输出 status。 | `internal/nodes/node.go:29-49`；D02 待批（`docs/contracts/revisions/g2-r03/DECISIONS.md:8`）；没有来源冲突测试。 | 节点 owner/Controller；D02；真实来源 | BLOCKED D02；UNTESTED |
| A1-08 | 指标无时间不得称新鲜 | online 且 Metrics 有限 0–100、ObservedAt 非零/不晚于请求时间 → CPU/内存可输出数字；离线、缺样本、非有限、越界、未来 → null；真实 0/100 保留；不夹取、不以 0 补缺、不输出 `sampledAt`/不以 lastSeen/now 补时间；Web 必须把无时间值标成缺失/未采样。 | Web 指标质量组件测试 `apps/web/src/features/nodes/candidate-adapter.ts:81-107`；Node.Usage 的 offline/finite/time 规则 `internal/nodes/node.go:53-67`；无 HTTP 映射/真实样本来源。 | Controller 投影；Web legacy VM；D04；真实样本 | COMPONENT/OFFLINE；HTTP UNTESTED |
| A1-09 | 分阶段 HTTP 错误 | auth 缺失/无效 → 401；query 坏 → 400；主体未知/整体拒绝 → 403；策略 reader/坏策略 → 503；库存失败/部分+错误 → 503；库存坏/重复 ID 或同次 CheckNode InvalidCheck → 500（**不是 400**）；投影/序列化前置失败 → 500；所有错误无 items/无原始异常/无节点 ID，列表 `Error` media，no-store。 | 当前无 handler；r03 候选错误/媒体见 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:354-483`；N03-A 证明坏库存 ID 是内部 InvalidCheck、不能推出 HTTP400（`docs/development/reviews/N03-A-REVIEW.md:9-21`；`docs/contracts/decisions/node-read-security-01/SECURITY-ACCEPTANCE.md:18-30`）；错误映射未实现。 | Controller error mapper；D06；真实 HTTP | UNTESTED |
| A1-10 | 取消/期限与提交边界 | 在预检、库存、遍历、投影、序列化各阶段取消/到期：不得返回 200/200 空/部分；客户端断开可能无响应；服务端期限可写时 504；响应提交后不得把截断/写失败当作成功，返回连接关闭/日志安全阶段；不自动重试 GET 业务。 | N02/N03 离线取消/错误分类历史 PASS（`internal/nodes/read_usecase.go:27-95`；N03-A 复审 `:31-40`）；无 HTTP/代理/浏览器测试。 | Controller/代理；D06；真实超时测试 | OFFLINE-ACCEPTED；HTTP UNTESTED |
| A1-11 | 缓存与跨主体隔离 | 成功和所有错误 `Cache-Control: no-store`，建议 `Vary: Cookie, Authorization`；无 ETag/304；Controller/代理/浏览器/Service Worker 不跨主体复用；会话/身份/来源变化时 Web 旧列表/计数/详情首提交清除；A 成功→注销/撤销/降权→B 不显示 A 数据。 | r03 只有 no-store 提案（`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:99-103`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:57`）；W05/W06 只有隔离组件历史证据（`apps/web/src/features/nodes/README.md:22-53`；W06-R1 复审 `docs/development/reviews/W06-R1-REVIEW.md:7-39`）；无 HTTP 头/代理/真实浏览器。 | Controller/Web/代理；D05/D07；真实 cache 验收 | UNTESTED |
| A1-12 | 旧客户端兼容 | 旧 Bearer 客户端仍可调用；路径/operationId/items/必填 id/name/online/onlineOnly 三态不变；可选 ip/tags/countryCode 省略、CPU/内存 null/数字、无 sampledAt；新增 400/403/503/500/504 和 Cookie 支持需探测；无第二路由/r02 profile/分页/total。 | r03 静态矩阵/负向检查与 G2-R02A 复审（`docs/development/reviews/G2-R02A-REVIEW.md:15-20`；`docs/contracts/revisions/g2-r03/MATRIX.md:9-31`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:31-35`）；无真实旧客户端。 | 契约 owner/旧客户端探针；D01/D05/D06 | STATIC-ONLY；CLIENT UNTESTED |
| A1-13 | 无部分列表/隐藏计数 | 库存错误、坏 ID、策略故障、投影失败均无部分 items；隐藏节点名称/IP/标签/region/计数不出现；不返回 total/page；保留授权可见顺序；过滤/投影只在完整成功集合上执行，序列化前完成整个 body。 | N03-A 离线 nil/无部分/深复制/授权后筛选计数（`docs/development/reviews/N03-A-REVIEW.md:9-21,31-40`；`internal/nodes/read_usecase.go:60-95`）；无 HTTP/UI 计数证据。 | Controller/Web；D07；真实 HTTP/浏览器 | OFFLINE-ACCEPTED；HTTP UNTESTED |
| A1-14 | 来源所有权与 fail-closed | Authenticator/PolicyReader/InventorySource 由可信组合根注入；缺失/未配置/失败 → 401/503，不使用默认 mock/空成功；每请求一次策略、一次库存；来源快照在读取期间只读；不跨请求缓存 reader/scope/列表；来源错误无敏感原文。 | 现有 auth/nodes 只有离线函数接缝与测试（`internal/auth/node_read_scope.go:18-74`；`internal/nodes/read_usecase.go:15-27`；N03-A/AUTH-01B 复审）；Controller/App 未接线（`apps/controller/main.go:17-35`；`apps/web/src/App.tsx:3-17`）。 | Controller 组合/来源 owner；D05/D07；存储 | BLOCKED；UNTESTED |

## 观察方法与未来执行层次

- **Controller httptest（未来）**：用伪 Authenticator、可控 PolicyReader、可控 InventorySource 和请求记录器，断言 HTTP status/headers/body、策略/库存调用次数与顺序；注入取消/期限；不接真实网络/DB。覆盖 A1-01–A1-10、A1-13、A1-14。
- **来源/存储测试（未来）**：策略/库存 reader 的当前快照、只读约束、失败/部分数据/并发写检测；存储事务/版本只到批准方案才验收；没有则明确 UNTESTED。覆盖 A1-03/A1-04/A1-14。
- **Web/浏览器（未来）**：用已接收 W05 加载接缝和新的 legacy VM，验证 Cookie 请求、无时间指标、错误/no-store、身份/来源切换、首提交无旧数据；W05/W06 历史证据不自动转移为真实 HTTP/跨主体 PASS。覆盖 A1-01、A1-05、A1-08、A1-11、A1-12。
- **旧客户端**：用固定 Bearer fixture、未知字段/新错误码/`onlineOnly` 别名探测；未测前不能宣称 legacy 兼容完成。
- **错误/敏感信息**：注入节点 ID/策略/包装异常敏感标记，检查响应、Controller/代理日志、追踪和 UI 不出现；只能以批准的错误/媒体方案验收。

## 明确不声称

本任务没有运行真实 HTTP、认证、数据库、浏览器、代理或跨主体 cache 验收；`OFFLINE-ACCEPTED`/`STATIC-CONTRACT`/`COMPONENT` 不等于 A1 已实现或已验收。A1 不含详情，因此详情 404 防枚举只作为未来边界，未测试、未实现。D01–D07/P1-C10 仍 OPEN，G2 未冻结。
