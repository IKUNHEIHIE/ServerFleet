# READ-PLAN-01 · 首条只读闭环验收矩阵

状态：**PROPOSED / 待批准**。本矩阵定义“首条只读闭环”的可执行验收条件；当前没有真实 HTTP、认证、库存来源或浏览器链路，因此除明确标注的离线/静态证据外，所有真实闭环项均为 MISSING/BLOCKED。离线/静态 PASS 不写成真实接口已验收。

基线：`98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`。

## 证据等级

| 标记 | 含义 |
|---|---|
| `OFFLINE-UNIT` | 已直接调用真实 Go 函数/端口的模块测试；不是 HTTP、浏览器或真实身份证据 |
| `STATIC-CONTRACT` | 读取候选 YAML/矩阵/负向 lint 的静态测试；不证明服务器行为 |
| `COMPONENT` | Web 组件的 jsdom 测试；没有真实网络或真实浏览器 Tab/缓存证据 |
| `NONE` | 当前无实现或无可执行证据 |
| `BLOCKED` | 依赖未批准决定、未接收任务或未实现依赖 |

## 验收矩阵

| ID | 验收项 | 可执行判定（未来真实链路） | 当前实际证据 | 缺失证据 / 负责模块 | 状态 |
|---|---|---|---|---|---|
| AUTH-01 | 可信主体绑定与双凭据 | 无/过期/撤销凭证 → 401 且无节点；请求 body/query/header 自报 subject 不改变服务端主体；有效凭据的主体为 A 时，目标/请求只按 A 授权；同一请求同时送 Cookie+Bearer 按已批准规则拒绝，不静默回退。 | `PrepareNodeReadScope` 真实离线测试与复审：只接受 subject 字符串，不认证；AUTH-01B 复审 `docs/development/reviews/AUTH-01B-REVIEW.md:9-19,29-36`；`internal/auth/node_read_scope.go:23-74`。 | 认证/会话/CSRF/撤销实现；Controller HTTP；D05；`NONE` | BLOCKED D05 |
| AUTH-02 | 空库存未知主体/零授权 | 库存来源返回 0 节点时，未知主体或策略失败 → 非 2xx，且库存来源调用数为 0；已知主体零授权 → 200 `{items:[]}`；任何情况下空库存不跳过策略预检。 | `PrepareNodeReadScope` 支持无库存预检（`internal/auth/node_read_scope.go:9-23`）；旧 `Visible` 空库存不调用 Authorizer 的限制见 `docs/contracts/revisions/g2-r03/COMPATIBILITY.md:61-63`；N03-A 已分配未接收 `docs/development/TASK-BOARD.md:36`。 | N03-A 接收；Controller 预检顺序/调用计数测试；D07 | BLOCKED N03-A/D07 |
| AUTH-03 | 策略失败不得成功空/部分列表 | 策略读取失败、`ErrPolicyUnavailable`、`ErrInvalidCheck`、取消/期限或未知错误 → 非 2xx（按 D06 映射），响应不含 items/计数/部分节点；不能退化为 200 `items:[]` 或保留已富化部分。 | N02-R1 离线规则：仅纯 `auth.ErrDenied` 过滤；多原因/错误中止并返回 nil，`internal/nodes/visibility.go:20-61`；N02-R1 复审 `docs/development/reviews/BATCH-05-REVIEW.md:15-19`。 | Controller HTTP 错误映射、策略读取失败真实集成；D06；`NONE` | OFFLINE-UNIT；HTTP MISSING |
| AUTH-04 | 同次共同范围与新请求撤权 | 一次列表请求只准备一次策略/范围（读取次数=1），所有节点使用同一范围；预检后来源策略 map 变化不改变本请求；下一次请求重新预检并看见撤权；不缓存旧范围/列表。 | `NodeReadScope` 复制私有集合、新 Prepare 可见撤权、同次 CheckNode 不读策略：AUTH-01B 复审 `docs/development/reviews/AUTH-01B-REVIEW.md:9-19`；旧 `Visible` 逐节点可能跨快照的限制：`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:61-63`；N03-A 未接收（`docs/development/TASK-BOARD.md:36`）。 | N03-A/Controller 组合；库存与策略同一事务、输出原子性仍无实现；D07 | BLOCKED N03-A/D07 |
| AUTH-05 | 隐藏节点不进列表/计数 | 合成库存含可见节点 A 与隐藏节点 H（独有名称/标签/region）；列表只含 A；Web 计数、筛选、分类/指标聚合均不含 H；H 的名称/标签/ID 不出现在错误或空结果提示中；过滤后空列表为 `items:[]` 且无 total。 | 节点领域离线过滤/计数测试（N01/N02）：`internal/nodes/visibility.go:125-142`、`visibility_test.go`；r03 空数组/不泄露总数要求 `docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-CORE-CONTRACT-RULES.md:22`。 | Controller 输出与 Web 计数集成测试；真实 HTTP/浏览器；D07 | OFFLINE-UNIT；E2E MISSING |
| FIELD-01 | 不伪造状态/版本/平台/版本号 | 来源缺失 `status/revision/platform/agentVersion/lastSeenAt` 时相应字段省略/null（按批准形状）；不填 revision=1/0、不把 online=false 猜成具体状态、不把响应时间/now/lastSeen 当 sampledAt；有 status 时 online 与 status=online 一致；详情没有 revision/status 时不可成功返回。 | r03 候选 schema/示例与静态检查：`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:217-302,304-352`；D02/D03/D04 OPEN `docs/contracts/revisions/g2-r03/DECISIONS.md:7-12`；当前 Node 无 revision/platform/agentVersion `internal/nodes/node.go:39-49`。 | 权威元数据源、状态优先级、D02/D03 批准、详情实现 | BLOCKED D02/D03 |
| FIELD-02 | 指标与采样真实性 | CPU/内存为 0–100 有限数或 null；真实 0/100 保留；无共同 `sampledAt` 的旧值不能显示为新鲜；离线/暂停/吊销/未知不显示当前读数；未来/非法/过期样本按注入的 now/maxAge 判为无效或陈旧；不夹取越界、不以 0 补缺、不以 lastSeen/now 补采样时间。 | Web 局部指标质量测试（组件层）：`apps/web/src/features/nodes/candidate-adapter.ts:81-107`、`apps/web/src/features/nodes/types.ts:40-50`；Go `Node.Usage` 的离线/有限/时间规则 `internal/nodes/node.go:53-67`；两者 0–100/陈旧差异见 `docs/contracts/revisions/g2-r03/COMPATIBILITY.md:23-27`；无真实数据源。 | 公共 sampledAt/逐指标时间、时效阈值、Controller 映射、真实浏览器；D04 | COMPONENT-ONLY；D04 OPEN |
| COMPAT-01 | 旧列表客户端兼容 | 既有 `GET /api/v1/nodes` 路径/`listNodesForTransfer`/`{items}`/必填 `id/name/online` 不变；不得新增第二集合路由或重复 operationId；原 Bearer/错误媒体或经批准的变更须有迁移说明；拒绝未知字段的旧客户端与扩展名占用需实测；新增可选字段只在其批准版本出现。 | r03 静态深比较/负向检查与 G2-R02A 复审：`docs/development/reviews/G2-R02A-REVIEW.md:15-20`；`docs/contracts/revisions/g2-r03/MATRIX.md:9-31`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:31-35`；没有真实旧客户端。 | 真实客户端/fixture 探测、契约统一修订、D01/D05/D06 批准 | STATIC-ONLY |
| WEB-01 | 来源/作用域切换首提交隐藏旧数据 | Web 从“A 已 ready”切到不同来源/作用域、无效来源、loading 或 error 时，首个提交即不渲染 A 的节点/计数/详情；迟到成功/失败/旧错误被丢弃；同 key 有效重渲染不无故重请求；卸载中止/忽略响应。 | W03/W04 组件测试覆盖列表/详情/焦点；W05 加载容器未接收，W05-R01 同 key 无效 load 恢复仍 OPEN，见 `docs/development/reviews/FOLLOWUP-02-REVIEW.md:30-38,44-50`；主分支无真实 source。 | W05-R2 接收、Controller R1-A、真实 browser 首提交/网络检查 | BLOCKED W05/实现 |
| CACHE-01 | 成功/错误缓存与跨主体隔离 | 成功和所有错误响应均 `Cache-Control: no-store`（或经批准的更强策略）；代理/Service Worker/Web 内存不得跨主体复用；身份/来源切换清除旧 subject 的节点、计数、详情；实际浏览器网络面板验证。 | r03 仅提案 no-store，见候选 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:99-103,163-167` 与 `docs/contracts/revisions/g2-r03/COMPATIBILITY.md:57`；没有 HTTP 头或浏览器测试。 | Controller 头、Web 清理、真实浏览器/代理测试；D07 | UNTESTED |
| ERR-01 | 错误映射、防枚举、取消不成功 | 列表未认证 401、整体拒绝 403、策略不可用 503、仅真正非法 query 400；详情不可见与不存在统一 404，不泄露节点存在性；错误不含 node ID/策略/原始异常；取消/期限不产生 200 或部分 items；409/412/428/429 不凭空赋予 GET 语义；错误体保持原媒体或经批准统一。 | 当前无 HTTP 实现；候选错误声明见 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:100-114,115-192,410-482`；映射仍待 D06 `docs/contracts/revisions/g2-r03/DECISIONS.md:12`；N02-R1 仅证明 Go 层错误分类。 | Controller 错误映射/脱敏、防枚举、真实 HTTP 测试；D06 | UNTESTED / D06 |

## 执行与证据规则

- 本矩阵没有在本任务中执行真实 HTTP、认证、数据库、浏览器或缓存验收；现有证据等级已逐行标注，不能把 `OFFLINE-UNIT`/`STATIC-CONTRACT`/`COMPONENT` 写成真实闭环 PASS。
- 未来实现后的最低执行层次：
  1. `go test -count=1 ./internal/auth/... ./internal/nodes/...`：真实离线预检/可见范围；
  2. Controller `httptest` 集成：伪认证适配 + 可控 PolicyReader + 可控库存来源，覆盖空库存、策略失败、共同范围、隐藏计数、取消；
  3. 契约检查：在统一修订上重跑 schema/旧字段深比较/负向 lint；
  4. Web `npm run check:web` + 真实本地 HTTP/fake source 的首提交、source switch、错误/缓存测试；
  5. 真实浏览器网络检查（Cookie/Bearer、no-store、跨主体切换）；若未执行，明确 `UNTESTED`。
- 真实部署环境、跨进程撤权、数据库事务/策略原子性、第三方客户端兼容和 1C1G 性能不在本矩阵的离线证据内。当前只提出验收包，不声称其已通过。

## 与 D01–D07 的对应

| 验收项 | 主要决定 |
|---|---|
| AUTH-01/02/04 | D05、D07 |
| AUTH-03/ERR-01 | D06、D07 |
| AUTH-05 | D07 |
| FIELD-01 | D02、D03 |
| FIELD-02 | D04 |
| COMPAT-01 | D01、D05、D06 |
| WEB-01、CACHE-01 | D01、D05、D07 |

D08 只完成工具/注释协调，不是本矩阵的运行时验收。D01–D07 与 P1-C10 保持 OPEN。
