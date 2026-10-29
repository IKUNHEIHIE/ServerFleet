# READ-HTTP-DESIGN-01 · A1 兼容只读列表 HTTP 设计

状态：**PROPOSED DESIGN / 待总控、安全、契约与节点审查；不是实现授权，不是 G2/D01–D07 批准**。
基线：`4196b2e8ab9c389ae0830bf56f905d9bafa00d05`（本目录 HEAD；本返修从 `ece5efd` 追加）；代码/资料审查基线见任务单 `docs/development/READ-DESIGN-01-WORK-PACKAGES.md:8`。负责人已确认 A1+B1 方向（`docs/contracts/decisions/node-read-01/OWNER-CHOICES.md:16-32`），但下列具体 HTTP、字段、错误、缓存和解析规则仍是待审提案。已接收的认证设计 `48409bac` 只作为 PROPOSED 输入，不是已批准接口；本文件对齐其 Tauth/Tpolicy/Tgate、受限 Bearer 和迁移差异，但不把任何一项写成已接受。

## 1. 范围与非目标

**A1 首版：只做既有 `GET /api/v1/nodes` 的兼容只读列表。** 保留旧路径、`listNodesForTransfer`、`{items:[...]}`、必填 `id/name/online` 和 `onlineOnly` 默认 false；首版不提供详情、`status/revision/platform/agentVersion/lastSeenAt/sampledAt`、权限编辑、分页或新集合路由。

**B1 方向：浏览器使用服务端会话 Cookie；非浏览器 Bearer 的兼容支持须按 D05/认证设计审查。** 本文件只定义 HTTP 适配层需要从认证设计获得的可信主体、错误阶段和 Tgate 结果，不设计或实现登录、会话签发、CSRF、Token 或数据库。

**非目标：** 不实现 handler、数据源、DTO 代码、OpenAPI 修改、数据库、缓存层或真实网络；不把 A1 设计写成已经接线的 Controller；不关闭 D01–D07/P1-C10，不批准运行时 profile。

## 2. 现有契约与 A1 拟形状

### 2.1 已接收的候选与兼容事实

- r03 候选保留两个既有 GET；列表为 `GET /api/v1/nodes`、`listNodesForTransfer`、`onlineOnly:boolean` 默认 false、`{items:[NodeSummary]}`，列表错误为 `application/json` `Error`。见 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:18-114,217-302`；来源/字段矩阵 `docs/contracts/revisions/g2-r03/MATRIX.md:9-31`；D01–D07 仍 OPEN `docs/contracts/revisions/g2-r03/DECISIONS.md:7-13`。
- 原 Merged 已有同一列表操作和 `onlineOnly`；旧列表全局 Bearer，`Error` 为 `application/json`，详情才使用 `Problem`。见 `api/openapi/g2-merged-candidate.yaml:86-113,6205-6213,6865-6883,10357-10362`；安全来源核验 `docs/contracts/decisions/node-read-security-01/SOURCES.md:42-49`。
- r03 的 400/403/503、`Cache-Control: no-store` 和新增字段都只是提案；静态 schema 检查不等于真实 HTTP。`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:99-113,354-483`；`docs/development/reviews/G2-R02A-REVIEW.md:7-28`。

### 2.2 原候选 → A1 兼容差异

| 元素 | 原 Merged/r03 | A1 提案 | 状态与兼容影响 |
|---|---|---|---|
| 路径/操作/包裹 | `GET /api/v1/nodes`、`listNodesForTransfer`、`{items}` | 完全相同；不加集合路由/profile | 兼容；不复制 operationId |
| `onlineOnly` | boolean，默认 false | 保留参数与默认语义；解析规则见 §3.2 | 兼容计划；坏值/空值/重复值的 400 规则待 D06 |
| 认证 | 列表全局 Bearer；r03 保留 | 浏览器 Cookie 会话 + 受限 Bearer（服务器签发 opaque Token，当前 `nodes.read` ∩ 签发节点上限；双凭据拒绝）；Cookie 加入旧列表属契约变更 | **待 D05/认证设计批准**；Bearer 传输/路径保留不等于旧 JWT 或任意 Bearer 值兼容；旧签发方/客户端须盘点迁移；本文件不改 schema。认证提案 `docs/contracts/decisions/node-read-auth-01/DESIGN.md:44-54,66,132-140` |
| 必填字段 | `id/name/online` | 相同 | 兼容；不可缺失、不可用显示名回退替代 |
| 可选旧字段 | `region/provider/countryCode/ip/tags`、可空 CPU/内存 | 只保留 `region/provider`；`ip/tags/countryCode` 暂不输出；CPU/内存保留为可空旧值但无 `sampledAt` | 可选字段省略对 schema 合法，但旧客户端若依赖需探测（D01）；A1 无新鲜指标 |
| 新富化字段 | r03 提议可选 `status/revision/platform/agentVersion/lastSeenAt/sampledAt` | **不输出** | D02–D04 仍 OPEN；不能填假值；详情也不在 A1 |
| 错误媒体 | 列表 `Error` application/json；详情 `Problem` | 列表所有 A1 错误继续 `Error` application/json | 保持列表媒体；不把列表改成 Problem；详情不实现 |
| 错误状态 | 旧 401；r03 提案 400/403/503 | 401/400/403/503/500/504，按阶段映射（§5） | 具体码/机器码/解析规则待 D06 与契约审查；旧客户端可能不识别新码 |
| 缓存 | 旧无头；r03 提案 no-store | 成功与错误 `Cache-Control: no-store` + `Vary: Cookie, Authorization`；无 ETag | 提案；不改旧缓存语义但会阻止缓存；D06/D07、代理验收 |
| 分页/total | 无 | 不新增 | 兼容；不泄露总数 |

## 3. 请求管线与 onlineOnly

### 3.1 固定顺序

`路由/方法 → 凭据数量/类型/秘密验证（Tauth）→ 可信主体与身份来源当前状态 → onlineOnly 解析 → Tpolicy/N03-A 预检 → 库存读取/同次授权 → 授权可见集合 → onlineOnly 过滤 → 字段投影/序列化缓冲 → Tgate（PROPOSED）→ Tcommit`

- **认证/身份先行。** 缺失、无效、过期、撤销、双凭据或不支持凭据 → 401，不解析业务 query，不调用策略/库存。**有效凭据但身份/会话来源不可用或坏记录 → 独立 503 `IDENTITY_UNAVAILABLE`，不是 401，不提示重新登录，也不读策略/库存**；这与凭据本身失效分开。`Tauth` 名字与认证提案对齐（`docs/contracts/decisions/node-read-auth-01/DESIGN.md:40,44-54,144-151`），但其具体分类仍是 PROPOSED。
- **query 次之。** 身份可用后才解析 `onlineOnly`；坏 query 返回 400，且策略读取和库存调用均为 0。有效凭据 + 坏 query + 身份来源不可用时，身份源故障优先返回 503（因为 Tauth 在 query 之前）；凭据本身无效 + 坏 query 返回 401。这样未认证请求不会获得业务 query 的差异反馈。
- **预检先于库存。** 复用已接收的 N03-A `ReadVisibleInventory`；它在每次调用中执行 `PrepareNodeReadScope`（Tpolicy），成功后才调用库存来源，再用同一范围检查所有节点。见 `internal/nodes/read_usecase.go:27-96`、`internal/auth/node_read_scope.go:23-74`；N03-A 复审 `docs/development/reviews/N03-A-REVIEW.md:7-15,17-21,31-40`。HTTP 层不得先读库存再预检，也不得再调用一次 `PrepareNodeReadScope` 造成策略读取两次。
- **过滤只在授权之后。** 先得到 `ReadVisibleInventory` 的授权可见集合，再按 `onlineOnly` 过滤；隐藏节点从不进入集合，也不进入计数。过滤后为空仍是成功 `items:[]`，不跳过预检/库存。
- **全量构建后提交。** 投影和 `json.Marshal` 完成前不写成功响应；不流式输出部分 items。认证提案建议在提交前加 **Tgate**（检查凭据/epoch/策略授权世代是否仍匹配，不再 prepare 或重读完整策略）；本设计推荐接入，但 Tgate 尚未批准/实现。Tgate 失败时丢弃缓冲并按阶段错误处理，不发送部分字节；若最终不采用 Tgate，只能声明 Tcommit 前的最后检查存在残余窗口，不能宣称即时撤权。提交后再发生取消/写错误不能改写成错误响应，只能记录安全 ID/阶段。

### 3.2 `onlineOnly` 解析提案

| 输入 | 提议行为 |
|---|---|
| 参数省略 | false |
| 恰好一个值 `true` 或 `false`（ASCII 小写） | 对应布尔值 |
| 恰好一个空值 `?onlineOnly=` | **400 `INVALID_QUERY`**（不默认 false；避免两个“缺省”表示） |
| 同一参数出现多次（包括 true/false 混合） | **400 `INVALID_QUERY`**；不做 first/last wins |
| `1`/`0`、`TRUE`/`FALSE`、`yes`、空白或其他 token | **400 `INVALID_QUERY`**；不静默宽松解析 |
| 其他未知 query 参数 | 忽略，保持旧客户端兼容；不引入新参数 |

上述解析、400 和机器码均为提案，待 D06/契约批准；实现先用严格 parser，不采用 `strconv.ParseBool` 的宽松集合。`onlineOnly=true` 只保留授权可见集合中 `online=true`；false/省略保留全部可见节点（含离线）；隐藏在线节点在三个取值下都不出现。过滤后空集为 200 `items:[]`，无 `total`；策略/库存故障不得变成空列表。

## 4. 字段投影与可信来源

### 4.1 字段表

A1 输出显式白名单（不是把内部 Node 反射/序列化）：

| 字段 | 来源 | 规则 | 缺失/非法/矛盾 |
|---|---|---|---|
| `id` | 库存 `Node.ID` | 必填、精确、不 trim、不从名称/IP 生成；保持库存顺序 | 空、控制字符、重复或无效 UTF-8 → **500 INTERNAL_ERROR**，整请求失败，无部分 items |
| `name` | 库存 `Node.Name` | 必填、原值；不把空名回退成 ID | 空/控制字符/无效 UTF-8 → 500 |
| `online` | 可信库存的 legacy online 布尔；当前 Node 仅有 `Status` | 内部映射提案：`Status==online` → true，其他合法状态 → false；这不输出五状态、不把 false 宣称为 offline/suspended/revoked。若来源有独立 online 与 status，必须一致 | 缺失/非法/矛盾 → 500；D02 仍是待批提案，映射使用前须确认 |
| `region` | `Node.Region` | 可选；非空、合法 UTF-8/无控制字符才输出 | 空/非法 → 省略该字段；不补造 |
| `provider` | `Node.Provider` | 同上 | 同上 |
| `cpuPercent` | `Node.Metrics.CPUPercent` | 可选数字或 null；仅 online、Metrics 非 nil、有限、0–100、ObservedAt 非零且不晚于请求时间时输出数字 | 缺时间/离线/非有限/越界/未来 → null；不夹取成 0/100；无 `sampledAt`，不得称新鲜 |
| `memoryPercent` | `Node.Metrics.MemoryPercent` | 同上 | 同上 |
| 其他字段 | — | **不输出** `status/revision/platform/agentVersion/lastSeenAt/sampledAt/ip/tags/countryCode` | 不能由内部字段补造；详情不在 A1 |

- `online` 是旧列表必填布尔，但当前内部只有 `Status`；上述映射只是实现提案，不是 D02 已批准事实。若 D02/节点 owner 不批准把 `Status` 用作 legacy online 来源，A1 必须有独立可信 online 字段或阻塞，不能从 UI/名称猜测。
- CPU/内存没有公共 `sampledAt`，即使内部 `Metrics.ObservedAt` 存在也不暴露；A1 的数值只能作为 legacy 无时间值，Web 必须按“未采样/缺失”处理，不能显示成新鲜读数。`ObservedAt` 仅用于拒绝零值/未来样本的基本有效性，不据此定义公共时效（D04 仍 OPEN）。真实 0/100 保留；越界/非有限不夹取，直接 null。
- `region/provider` 为低敏显示字段；`ip/tags` 的字段级授权和敏感信息仍待 D07，A1 不输出；`countryCode` 内部缺失，不臆造。
- 必填字段失败时整请求 500，不跳过坏节点、不返回部分 items；可选字段失败时按上表省略/null，不改变节点身份或计数口径。

## 5. HTTP 错误按阶段与成因

### 5.1 错误表（提案，待 D06）

| 阶段/成因 | HTTP | 提议机器码/媒体 | 固定安全信息 | 策略/库存观察 |
|---|---|---|---|---|
| 凭据缺失/无效/过期/撤销/双凭据/不支持 | 401 | `UNAUTHENTICATED`；`application/json` 旧 `Error` | “Authentication required” | 0/0；身份源未读取或未授权前不产生主体 |
| **有效凭据但身份/会话来源不可用、坏记录、随机源/时钟故障** | **503** | **`IDENTITY_UNAVAILABLE`；`Error`** | “Authentication service temporarily unavailable” | **0/0；不当作未登录、不提示重新登录、不回退旧主体** |
| `onlineOnly` 坏值、空值或重复 | 400 | `INVALID_QUERY`；`Error` | “Invalid query” | 0/0 |
| 已认证主体未知、整体策略拒绝 | 403 | `FORBIDDEN`；`Error` | “Access denied” | 策略1次/库存0次 |
| 策略 reader 失败、nil/坏策略 | 503 | `POLICY_UNAVAILABLE`；`Error` | “Policy service temporarily unavailable” | 策略尝试/库存0次 |
| 库存来源失败、部分数据+错误、混合故障 | 503 | `INVENTORY_UNAVAILABLE`；`Error` | “Node inventory temporarily unavailable” | 策略1次/库存1次；丢弃全部数据 |
| 库存坏/重复 ID、同次 CheckNode 的 InvalidCheck、库存数据不合法 | 500 | `INTERNAL_ERROR`；`Error` | “Internal error” | 库存已读；不归咎客户端、不泄露 ID |
| 投影/序列化前置失败 | 500 | `INTERNAL_ERROR`；`Error` | 同上 | 不提交成功响应 |
| Tgate 观察到凭据/会话失效 | 401 | `UNAUTHENTICATED`；`Error` | 同上 | 丢弃缓冲，0 字节成功响应 |
| Tgate 观察到身份来源不可用/授权世代不可判定 | 503 | `IDENTITY_UNAVAILABLE` 或 `POLICY_UNAVAILABLE`（按 Tgate 分类） | 对应固定安全信息 | 丢弃缓冲；不重读完整策略 |
| Tgate 观察到授权已变/不再允许 | 403 | `FORBIDDEN`；`Error` | “Access denied” | 丢弃缓冲；不返回旧快照 |
| 服务端期限到期且仍可写 | 504 | `DEADLINE_EXCEEDED`；`Error` | “Request deadline exceeded” | 无成功/部分；取消已完成工作 |
| 客户端断开/取消 | 无可用响应/连接关闭 | — | 不写成功/部分 | 无部分 items |
| 成功但 onlineOnly 过滤为空 | 200 | `{items:[]}` | — | 预检/库存均已完成 |

要点：

- **凭据无效与身份来源故障必须分开。** 无效/缺失/歧义凭据是 401；有效凭据之后身份/会话来源不可用是 503 `IDENTITY_UNAVAILABLE`，不能提示客户端重新登录或清会话。两者策略/库存均为 0 次，但观测阶段不同；`identity.unavailable` 是认证提案的内部类别，公共码仍待 D05/D06 批准（`docs/contracts/decisions/node-read-auth-01/DESIGN.md:52,127,144-151`）。
- **不能仅用 sentinel 统一映射。** `auth.ErrInvalidCheck` 可来自预检的 subject/context，也可来自库存坏 ID 的 `CheckNode`；前者是服务端接线/配置故障，后者是后端数据故障，均不是客户端 query 错误。HTTP 适配层必须记录“策略读取已成功、库存来源已调用”等阶段标志：库存已读后的 `InvalidCheck` → 500；query 解析错误由本地 parser 直接产生 400。不得把任何 `ErrInvalidCheck` 一律 400。对应安全缺口 A05/S16/S22/S24（`docs/contracts/decisions/node-read-security-01/SECURITY-ACCEPTANCE.md:18-30`）。
- **`ErrDenied` 也要看阶段。** `PrepareNodeReadScope` 的主体未知/整体拒绝 → 403；`ReadVisibleInventory` 对逐节点纯拒绝会过滤，不会返回该节点错误；非纯拒绝/混合错误返回错误 → 500/503，不返回 200 空列表。
- **错误媒体保持列表原 `Error`**（`application/json`，`code`/`message`，可选 `requestId`）；不把列表改成 Problem，不新增 `status` 字段。详情 Problem 的防枚举/统一 404 只作为未来详情边界，不在 A1 实现。参考 `api/openapi/g2-merged-candidate.yaml:6865-6883,9650-9678,10357-10362`；r03 列表错误 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:354-483`。
- 错误正文只含固定 code/message 和可选服务端生成的 requestId；不回显节点 ID、主体、策略内容、原异常或 unwrap 链；日志/追踪同样不记录 Cookie/Bearer/原始错误。安全证据 A06–A09（`docs/contracts/decisions/node-read-security-01/SECURITY-ACCEPTANCE.md:16-24`）。
- 409/412/428/429 不在 A1 GET 新语义内；可保留旧候选声明但本设计不赋予用途。405/未知路由由路由层处理，不属于 A1 业务错误。
- 成功和所有错误都 `Cache-Control: no-store`；错误响应也不得被代理/客户端缓存。

### 5.2 错误优先级

1. **Tauth**：凭据数量/类型/秘密无效 → 401，身份源不读；有有效凭据但身份/会话来源不可用 → 503 `IDENTITY_UNAVAILABLE`，不读 query/策略/库存。有效凭据 + 坏 query + 身份源故障 → **503**（身份源在 query 前）；凭据本身无效 + 坏 query → **401**。
2. **query**：身份可用后解析 `onlineOnly`；坏 query → 400，策略/库存 0 次。
3. **Tpolicy/库存**：主体未知/整体拒绝 → 403；策略不可用 → 503；库存失败/部分+错误 → 503；库存坏/重复/InvalidCheck → 500；均不返回部分 items。
4. **投影/序列化**：投影/`Marshal` 前置失败 → 500；全量 body 就绪前不写成功响应。
5. **Tgate**（PROPOSED，未批准/未实现）：提交前检查凭据状态/epoch 与策略授权世代/Token 上限；观察到失效/不可用/变更 → 丢弃缓冲，按 401/403/503 固定阶段映射，不重读完整策略、不返回旧快照。
6. **取消/期限**：任何阶段已观察的取消优先于成功/业务错误；服务端期限可写时 504；提交后不能回滚已写字节，只能记录连接中断，不得把截断响应当作成功。

认证设计建议的 Tgate 是**推荐对齐点**，但尚未批准或实现；若最终不采用 Tgate，本设计的保证只到 Tcommit 前最后一次检查，Tpolicy 后撤权/输出窗口仍为未关闭风险，不能宣称即时撤权（`docs/contracts/decisions/node-read-auth-01/DESIGN.md:144-151`）。

## 6. 可信来源、所有权与观察边界

### 6.1 依赖接口（设计需求，不改现有端口）

- **Authenticator（由 READ-AUTH-DESIGN-01 提供，PROPOSED）：** `Authenticate(*http.Request)` → 可信主体结果或固定阶段错误（`credential.missing/invalid/ambiguous/unsupported`、`identity.unavailable`）。只从 Cookie 会话/服务器签发受限 Bearer 验证主体；客户端 subject/角色/NodePolicy 不参与。认证提案还提供 Tauth/Tpolicy/Tgate 语义、会话/Token 生命周期、受限 Token 的当前 `nodes.read` ∩ 签发节点上限求交，以及迁移差异（`docs/contracts/decisions/node-read-auth-01/DESIGN.md:40,44-54,64-68,132-140,144-151`）。这些细节尚未批准；本设计只消费其语义，不签发/解析凭据、不实现受限 Token 适配。
- **Tgate（认证提案建议，尚未批准/实现）：** 提交前可检查凭据/epoch/期限和 Tpolicy 授权世代/Token 上限是否仍匹配；不调用 `PrepareNodeReadScope`、不重读完整策略、不重试库存。HTTP 设计推荐接入；若认证/安全最终不采用，须明确列为差异并保留“Tpolicy 后、Tcommit 前”的撤权/输出窗口，不能声称 auth 已接受仅 Tauth/Tpolicy 边界。
- **PolicyReader：** 现有 `auth.PolicyReader`（`internal/auth/node_policy.go:69-78`）；每请求一次，返回当前、不可变策略；不缓存旧策略、不从客户端构造授权。
- **InventorySource：** 现有 `nodes.InventorySource`（`internal/nodes/read_usecase.go:15-19`）；每请求一次，返回当前库存快照；不替换 `Reader.Get`，不引入 mock 默认；未来真实存储适配器由节点/存储 owner 提供。
- **N03-A 组合：** Controller 只调用 `ReadVisibleInventory` 一次；它内部先预检（Tpolicy）、后读库存，用同一 scope 检查全部节点。HTTP 层不再单独调用 `PrepareNodeReadScope`，避免策略读取两次；来源调用计数和阶段由包装器记录用于错误分类。`internal/nodes/read_usecase.go:27-96`；复审 `docs/development/reviews/N03-A-REVIEW.md:7-15,17-21,31-40`。

### 6.2 最小可信来源设计：权威、建立、更新与读取

本节补足 A1 所需来源的权威持有者与生命周期，不要求本轮写 schema/DB/代码，也不是 Agent 注册实现。

| 数据/来源 | 权威持有者 | 受控建立/更新 | A1 读取与默认行为 |
|---|---|---|---|
| 主体稳定 ID、启用状态、credentialEpoch、会话/受限 Token 记录 | auth/身份目录（认证设计）；storage 只提供连接/事务机制 | 初始化/创建用户、登录/轮换/退出/撤销/停用/降权通过受保护认证管理流程提交；不生成匿名管理员/默认 wildcard；旧 SF 会话名与 JWT 标签不自动成为新凭据 | Tauth 读取当前主体/凭据状态；来源不可用 → 503，不签发/不放行；无真实身份来源时不能进入 A1 |
| 显式 `nodes.read` grants、授权世代/policy revision | auth/策略来源（可信后端）；不能由客户端上传 NodePolicy | 由受控管理员/授权流程显式增删；每次提交递增授权世代/版本；初始管理员不等同全节点授权，已知空 grants 合法 | Tpolicy 一次读取并复制；未知主体拒绝，已知零授权返回有效空范围；策略源缺失/坏格式/失败 → 503，不缓存旧策略 |
| 节点库存：稳定 node ID、name、legacy online 来源、region/provider、raw Metrics | nodes/节点库存存储（未来 Agent 注册/可信连接状态与受控导入）；storage 只提供事务 | 节点加入/更新/删除只能通过受信服务端注册/管理路径；禁止客户端或公开安装默认写入假节点；online 来源须有权威映射，D02 未批准前不得用 `Status` 假装五状态 | 每次请求 Tpolicy 成功后读取一次一致快照；N03-A 复制/过滤/检查；库存为空但已初始化 → 授权后 200 `items:[]`；未初始化/无来源 → 启动失败或 503，不能假造空成功 |
| 部署来源配置/版本选择 | Controller 组合根/受控安装配置 | 固定可信来源、策略/库存 owner、时钟/随机源和部署开关；不接受客户端上传 reader/mock | 任一依赖未配置 → fail closed（启动失败或 503）；无公开默认 mock；配置/连接元数据本身不成为节点数据 |

- **初始空库与已知零授权：** 初始化后允许没有任何节点记录；已知主体在有效策略中没有 grants 是有效零授权，请求先预检、再读空库存 → 200 `items:[]`。未初始化的库、缺失来源或空表与“未配置”不可混淆：前者未配置应 503/启动失败；后者已初始化但无节点才可以空列表。空库存不能跳过身份/策略预检。
- **库存首次进入：** 只能由未来 Agent 注册/可信节点管理路径或受控导入产生；A1 设计不包含 Agent/WSS/注册/数据库实现，也不使用安装期假节点。没有真实库存来源时，A1 只能设计/测试，不能宣称闭环完成。
- **快照/并发模型：** PolicyReader 和 InventorySource 在请求作用域各读取一次；返回当前、不可变快照，读取期间源 map/slice 不得被并发写；存储实现应使用读事务/快照或等价复制；无法保证时返回错误，不能返回可变共享数据。N03-A 复制 Node/Tags/Metrics 后再交给 HTTP；HTTP 不缓存 scope/快照/错误。
- **失败与坏数据：** 身份源/策略源/库存源不可用 → 503 固定阶段错误；策略未知主体 → 403；库存空且已初始化 → 200 空；库存坏/重复 ID → 500；部分数据+错误 → 503 并丢弃全部；任何路径都不返回假节点、旧允许或部分 items。
- **仍需真实接入的数据：** 用户/会话/Token 记录、显式 grants 与授权世代、节点库存及 online/status 更新、存储事务/并发控制，均须 auth/Agent/存储任务实现；安装配置和来源注册只是元数据，不是节点/授权数据。当前 Controller 只有 health（`apps/controller/main.go:17-35`、`apps/controller/health/handler.go:1-12`），App 仍是骨架（`apps/web/src/App.tsx:3-17`）。

### 6.3 撤权、读取与输出的观察边界

| 时点 | 可保证 | 不能保证/后续依赖 |
|---|---|---|
| Tauth | 凭据本身无效/缺失/歧义 → 401；有效凭据后身份源不可用 → 503；主体由可信认证绑定 | Tauth 后撤销会话/Token 可能不阻止在途请求；认证设计定义其生效边界 |
| Tpolicy | 读取一次当前策略，之后捕获 scope；Tpolicy 前撤权可见 | Tpolicy 后撤权不改变已捕获 scope；读取器内部缓存旧策略时 Go 类型无法证明当前性 |
| 库存读取 | 一次当前快照；失败/部分数据不返回成功 | 策略与库存不是同一存储事务，没有共同版本；Tpolicy–库存之间成员/策略变化不保证一致 |
| 同次检查 | 同一 scope；纯拒绝过滤，非纯拒绝中止 | 不重新读取策略；输出前撤权/输出提交不原子 |
| 投影/序列化缓冲 | 全量 body 构建后才提交；隐藏字段不出现 | 序列化期间策略/会话变化不回滚；已提交字节不可撤销 |
| Tgate（PROPOSED） | 若采用：提交前检查凭据状态/epoch、期限、Tpolicy 授权世代/Token 上限；失败丢弃缓冲，不发送部分 items | Tgate 尚未批准/实现；不重读完整策略；即使采用，Tgate 后到 Tcommit/发送仍有最后窗口 |
| Tcommit/响应后 | 下一请求重新认证/预检，可观察撤权 | 本响应可能反映 Tpolicy/库存时点的旧可见集合；不能宣称即时撤权或输出原子性 |

- 认证提案用 Tauth/Tpolicy/Tgate/Tcommit/Tr 定义撤销观察边界；本设计对齐这些名称但不把它们当作已批准接口。未采用 Tgate 时只保证到 Tcommit 前最后一次检查，Tpolicy 后撤权/输出窗口保留为 D07 风险。
- 不通过“再读一次策略”或私有 scope 字段宣称消除 TOCTOU；需要策略/库存共同事务、版本或输出绑定时应另立存储/安全任务。当前不实现事务/锁。
- 请求被取消/超时：Tauth、Tpolicy、库存、遍历、投影、Tgate、提交各阶段都要检查 ctx；已观察取消不得变成 200/200 空/部分；客户端已断开时可能没有响应，服务端只记录安全阶段标识。
- `onlineOnly` 过滤发生在同次检查之后、投影之前；它不改变授权范围，只改变返回集合；隐藏节点在过滤前已不存在。

## 7. 缓存与旧客户端兼容

### 7.1 缓存提案

- 成功和所有错误响应：`Cache-Control: no-store`；建议同时 `Vary: Cookie, Authorization` 作为缓存键防御，但不替代 no-store；不发送 ETag/Last-Modified，不支持 304。
- Controller 不缓存策略、库存、scope、列表或错误；Web/代理/Service Worker 不得跨主体复用；会话/身份/来源变化时清理 Web 旧列表、计数、详情（详情不在 A1，但 Web 不得复用旧状态）。r03 的 no-store 只是候选提案（`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:99-103`；`docs/contracts/revisions/g2-r03/COMPATIBILITY.md:57`）；真实头/代理/浏览器均未验收。
- 认证设计负责会话注销/过期的服务端失效；HTTP 层只保证每个请求重新使用可信认证结果，不把旧响应缓存给下一主体。Web 设计负责本地状态清理；W05 的 key 只是来源/作用域标识，不是授权凭证（`apps/web/src/features/nodes/README.md:45-52`）。

### 7.2 旧客户端兼容矩阵

| 维度 | 旧客户端 | A1 影响 | 验收 |
|---|---|---|---|
| 请求 | Bearer 传输；`onlineOnly` 省略/true/false | Bearer 传输/路径保留，但**不保证旧 JWT/任意 Bearer 值兼容**；认证提案使用服务器签发 opaque 受限 Token，且需盘点旧签发方/客户端后迁移；Cookie 是新增支持（待 D05），双凭据 401；严格 parser 可能拒绝旧的 `1/0/TRUE` 等值 | 旧 Bearer 客户端/`onlineOnly` 三态；别名/凭据值兼容探测；旧 Token 发行来源盘点 |
| 成功形状 | `{items}` 必填 id/name/online；可选旧字段 | 必填不变；region/provider 保留；ip/tags/countryCode 省略；CPU/内存可空但无 sampledAt；无 status/revision 等新字段 | 旧 schema 深比较；字段缺失/未知字段客户端 |
| 错误 | 401 `Error` application/json | 保留 401/Error；新增 400/403/503/500/504 和机器码 | 旧客户端错误处理/媒体/状态码迁移探测 |
| 缓存 | 无 no-store | A1 新增 no-store/Vary | 代理/浏览器网络头验收 |
| 路由/profile | 单一 GET | 不新增/不复制；不使用 r02 header/profile | 路径、operationId、无重复路由检查 |

### 7.3 与认证提案的迁移差异（均为 PROPOSED）

| 差异项 | 认证提案 | A1 HTTP 影响/待审 |
|---|---|---|
| 列表认证 | 旧列表 Bearer-only；提案为 Cookie OR 受限 Bearer | 需契约 owner 批准把 Cookie 加入列表；Bearer 传输保留不等于旧值兼容；不允许所有旧写接口 Cookie |
| Cookie 名 | 推荐 `__Host-sf_session`，不双读旧 `sf_session` | 旧浏览器/外部客户端须迁移；HTTP 不自行接受旧名；旧 Cookie 存在须清/重登 |
| Bearer 格式 | 候选 `bearerFormat:JWT` 仅是标注；提案使用服务器 opaque 有状态 Token、当前权限∩签发节点上限 | 旧 JWT/外部签发者/客户端未盘点前不能宣称兼容；受限 Token 求交适配尚无端口，本设计不启用 Bearer 兼容实现 |
| Tgate | 提案在 Tcommit 前检查凭据/epoch/策略授权世代/Token 上限 | 本设计推荐接入；未批准/未实现前只保证到 Tcommit 前最后检查，保留 Tpolicy 后撤权/输出窗口 |
| 初始化/登录/CSRF | 提案调整 bootstrap 返回、登录后普通登录、CSRF 重载交付 | A1 不含这些操作；不另加路由或伪造 LoginResult；Web 设计负责消费 |
| 错误媒体/状态 | 认证提案只给阶段语义，未统一 HTTP 媒体/码 | 列表保持旧 `Error`；机器码/状态仍待 D06；详情 Problem 不在 A1 |

上述均为 PROPOSED/待共审，不是认证设计已接受或本设计已批准。旧 Bearer 客户端只有在批准的支持凭据与迁移完成后才可称兼容。

可选字段省略在 schema 上合法，但不等于所有旧客户端无条件兼容；`ip/tags` 的字段级权限、CPU/内存旧值显示和错误码处理都需真实客户端探测。不能宣称“legacy 兼容”已完成，只能给出上表验收计划。

## 8. 依赖、未定项与停止边界

- **认证设计依赖：** B1 Cookie/会话、Tauth/Tpolicy/Tgate/Tcommit/Tr、受限 Bearer 求交、双凭据规则、会话撤销/停用/降权、可信主体和错误阶段由 READ-AUTH-DESIGN-01 提供；HTTP 设计不自行选择 Cookie 属性/Token/JWT 或实现认证。认证设计当前只是 PROPOSED，不能称其已接受仅 Tauth/Tpolicy 边界。
- **Web 设计依赖：** legacy 列表 VM、无 `sampledAt` 指标显示、来源/身份切换首提交清理、`onlineOnly` 结果与无 total 计数、Cookie 请求和缓存策略由 READ-WEB-DESIGN-01 对齐；本设计只为后端响应/错误/header 提供输入。
- **契约/安全未定：** D01 真实旧客户端兼容；D02 `online`/五状态权威；D04 CPU/内存采样语义；D05 Cookie/Bearer/受限 Token；D06 状态码/机器码/媒体/query parser；D07 空库存/原子输出/撤权/缓存。全部 OPEN。
- **明确不做：** 不实现 handler/DTO/来源/数据库/认证/缓存；不改 OpenAPI、ports、r03/r02 或既有方案；不声称 R1-A 已启动。

### 8.1 与认证/Web 设计的对齐输入

| 对方设计 | 需要对方提供 | 本 HTTP 设计提供 |
|---|---|---|
| READ-AUTH-DESIGN-01 | Tauth 可信主体结果与 credential/identity error 分类、Tpolicy 授权世代、Tgate 当前性检查、受限 Bearer 求交/签发上限、Cookie/Bearer 迁移和会话失效边界 | 认证 middleware 调用点、HTTP 认证/身份源/策略错误优先级、可信主体→N03-A 数据流、Tgate 失败映射、请求阶段/错误分类需求 |
| READ-WEB-DESIGN-01 | legacy 列表 VM、无 sampledAt 指标显示、source key/世代与身份切换清理、错误/重试/loading 语义、Cookie 请求方式 | 响应字段白名单/省略规则、onlineOnly 结果、Error media/status/no-store、撤权/缓存可观察边界 |
| 契约 owner | Cookie 加入旧 Bearer-only 列表、Cookie 名/Bearer 格式迁移、错误码/媒体、parser 规则、可选字段省略的批准或拒绝 | 现有 Merged/r03 兼容差异、旧客户端/旧 Token 探测清单、具体变更范围；不直接修改 OpenAPI |

## 9. 实施拆分（仅规划，不启动）

1. **READ-AUTH-DESIGN-01 结论**：提供 Authenticator/TrustedSubject、Tauth/Tpolicy/Tgate、Cookie/受限 Bearer 支持与迁移、会话失效边界；HTTP 只消费。认证设计未批准前 Tgate/Bearer 适配仍 PENDING。
2. **READ-WEB-DESIGN-01 结论**：legacy VM、source/loader、无时间指标、身份切换清理和错误 UI；HTTP 不依赖其未提交实现。
3. **契约审查**：把 Cookie 加入旧 Bearer-only 列表、Cookie 名/Bearer 格式迁移、`onlineOnly` 解析、错误码/媒体、字段省略列为提案；由契约 owner 在统一修订批准；本设计不改 schema。
4. **Controller HTTP 适配任务**：认证 middleware（含 identity 源故障分支）→ query parser → N03-A → filter → legacy DTO/projection → Tgate（若批准）→ no-store/error mapper；使用伪认证/受控 policy/inventory 做 httptest，不接真实 DB。
5. **来源任务**：auth/policy 存储 reader、nodes/inventory 真实只读适配器、来源权威/建立/更新/快照/初始空库行为、组合根 fail-closed；无默认 mock；依赖真实身份/库存接入。
6. **验收任务**：按 ACCEPTANCE 的 HTTP/源调用/头/浏览器/代理场景执行；真实服务/存储/浏览器未验收前保持 UNTESTED。

依赖：认证/契约/来源先行；N03-A 已接收（离线）但不是 HTTP 完成；W05/W06 已接收的加载/隔离证据不能替代真实 HTTP/认证/缓存。所有实现范围需总控另批。
