# Server Fleet G2 — 基础接口统一规范（候选，待审查）

**状态：G2 设计稿，不是开发验收通过或生产已实现。** 依据 `ServerFleet-Functional-Requirements-v0.9.md`、`architecture/G1-DECISIONS-Accepted.md`、已批准 BOOT/PRIV/TASK/AI/PKI 等 ADR；不推翻产品边界。**本文件优先介绍如何实现一致性，新的接口名称与字段尚待 G2 审核冻结。**

## 1. 统一名称与边界

1. 统一公网 Web API 前缀 `/api/v1`，JSON 请求与响应。Web/AI/插件经同一 Controller 业务服务鉴权；不能访问 Agent 的 WebSocket 或底层数据库。Agent 连接不暴露公网可写管理接口给第三方。
2. **唯一任务资源：`/api/v1/tasks`**。既有 v0.9 已有 `/tasks` 创建/预览、安全计划接口，故 G2 候选以 `/tasks` 为公开标准名。历史 ADR 的 `/jobs` 仅是设计示例/内部领域名，**不创建第二套任务队列，也不同时维护两套可写 `/jobs` API**。历史文档原文保留，通过兼容索引说明。
3. 数据标识：公开路径及任务 ID 统一字符串，不凭是否 UUID 判断权限；系统内部存储可选 UUID/ULID。所有节点必须按登录用户可见范围过滤。自动分类与服务组是不同对象；服务组提交即冻结成员集合。
4. 终端用户连接、注册与任务独立：Web 身份认证不能代替 Agent mTLS；注册 Token 不能用于执行任务；Xray/插件凭据引用不构成主控身份授权。

## 2. 登录、会话、CSRF 和初始化（接口候选）

- 首次初始化：仅管理员在**主控本机控制台**取得的一次性短期 setup token 可以初始化首位管理员；未初始化时其他管理 API 不允许修改任何节点。禁止仅凭远程访问“空数据库”抢占管理员权限。
- 浏览器登录：建议安全 Cookie `Secure + HttpOnly + SameSite`，服务端限时会话；所有 Cookie 会话的 POST/PATCH/PUT/DELETE 必须校验 CSRF Token（以及 Origin/Host）。登录需限制猜测速度，错误消息不得提示“用户名存在”。注销立即撤销服务端 session，变更密码/停用用户应撤销受影响会话。
- 非浏览器受控客户端：可用**限制范围的 Bearer API Token**；第一个版本的 token 签发、撤销与 TTL 仍是待设计点，不能把以前契约中 `bearerFormat: JWT` 理解为已经决定使用 JWT。**生产实现不得因为示例 Bearer 约定而开放匿名访问**。
- 因为旧 v0.9 API 尚未统一 CSRF 约定，G2 组合候选中**老接口维持原有 Bearer 安全声明**；新接口显式允许 Cookie/Bearer，等待后续迁移评审，不能让浏览器 Cookie 绕过 CSRF 安全。
- 用户与权限：起步角色管理员/运维/只读。权限枚举必须由服务器定义，不接受客户端自报角色。`resource-grants` 是资源权限条目，不等于 AI 单次会话授权；AI 会话仍遵守 `scopeRevision`、冻结快照与强制审批。

## 3. 返回值、HTTP 错误与资源并发

- 新增接口成功返回资源对象或 204；异步执行返回 `202 Accepted` + `taskId`（**已有接口成功返回码尚未统一**，组合候选暂保留原成功码；最终冻结时全面审核）。列表统一 `items[] + page {nextCursor,limit,hasMore}`。若过滤后的列表为空，必须是空数组，不泄露总数。
- 时间使用 RFC 3339 UTC（`date-time`），流量使用 `bytes` 的非负整数；CPU 百分比/持续时间必须显式注明单位与采样时间，数据未知时 `null` 或质量状态，不用 0 冒充。
- 错误建议统一 RFC 9457 `application/problem+json`：`type, title, status, detail, code, requestId`，可选字段错误 `fieldErrors`，不可包含内部路径、真实密钥、执行环境变量等。HTTP 状态包括 400/401/403/404/409/412/428/429/503；资源不存在和无权访问时可视泄露风险统一表现为 404。<https://www.rfc-editor.org/rfc/rfc9457.html>
- **幂等**：修改类/异步请求需要 `Idempotency-Key`。存储键 = 身份主体 + 操作种类 + Key + 规范化 Body 哈希；同一 Key/相同请求重放返回原响应和 taskId，同一 Key/不同请求必须 409。有效期及持久化策略待服务端设计，不可承诺 HTTP 层“恰好执行一次”。
- **乐观并发**：对有修订号的资源支持 `ETag`/`If-Match`，版本不匹配返回 412；高风险写请求没有必要的前置条件返回 428。已有 `/groups` 用 `expectedRevision`，不能无提示另造不兼容 revision 体系，必须明确二者如何映射。<https://www.rfc-editor.org/rfc/rfc9110.html>
- **审计**：重要操作和权限拒绝记录用户/插件/AI 来源、请求标识、节点快照、结果、时间；审计 API 只显示必要且脱敏的视图；不可让普通用户删除/修改审计事件。数据库防篡改和保留策略需安全评审，不能宣称 SQLite 已形成外部不可篡改日志。

## 4. 唯一任务引擎与事件流

- 所有 Web、AI、插件、网关、Xray 修改任务统一经 Controller Task Service 进行**创建→规范化计划→资源/权限检查→按需审批→持久化→受控分批派发→逐节点结果**。
- 普通任务最多并发 5 台；root/修改类默认先 1 台再每批最多 5 台；任一批发生失败停止尚未启动的后续批次，已启动的不强制撤回。待上线 15 分钟 TTL，结果 `unknown` 时禁止自动重试有副作用动作。
- `taskId` 对应持久主任务，`executionId` 对应单节点运行实例。切换服务组成员或撤销 AI 会话授权不会悄悄扩大目标；撤销已批准动作阻止后续派发。
- SSE：`GET /api/v1/tasks/{jobId}/events` 使用 `text/event-stream`，消息携带唯一且单调可追溯 `id`、`event`、`data`；支持 `Last-Event-ID` 后继续读取，心跳不含敏感信息。无法续接时返回明确的“日志已截断/游标过期”结果，不得显示为没有事件。<https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events>
- AI 会话消息与工具活动可用独立 SSE 页面，**仍经 AI 会话授权**；禁止日志 SSE 或 AI 响应输出 API 密钥、CA 私钥与未授权节点资产。

## 5. Agent Protobuf 与故障语义

- Agent 主动 WSS+mTLS，一帧一个 `serverfleet.agent.v1.Envelope`；通过 `AgentHello/ControllerHello` 协商主版本与允许能力。重大版本不兼容拒绝连接，不能默默把特权检查关闭。
- `TaskOffer` 必含持久 `taskId` / `executionId` / `planDigest` / `actionId`、权限模式与期限；Agent 在启动进程前落盘去重。重复 offer 不重新执行，接收 ACK 不等于执行成功。
- 意外断开后 Controller 请求 `StateSyncReply`；如果真实结果无法确定，标记 `unknown`，不自动重跑 root/修改类操作。任务超时、输出截断、取消、重试均有独立状态。
- 主控恢复后默认只核对，不重新派发旧任务；Agent 无法自行决定谁可授权 root 操作。

## 6. 与插件、网关和灾备契约的界限

| 系统 | G2 基础层负责 | 业务层必须遵守 |
|---|---|---|
| Xray 插件 | 任务、权限、凭据引用、节点能力、流量样本来源与审计 | 模板版本、Xray 配置检查、独立用户/入站计数器、重启缺口、实例接管边界 |
| Nginx 网关 | 节点身份、安装任务、计划审批/状态、审计 | 先安全检查已有 Nginx，再全新安装或受控接管；健康检查与调度资格独立 |
| 通用插件 | 统一 Host 能力、插件启停后端强制拒绝、代码签名/摘要与审计 | WASM 运行时和 ABI 仍要先 PoC，插件无权直接 SSH/连接 Agent |
| 主控恢复 | 证书状态与任务冻结、审计、恢复资源授权检查 | 恢复使用**离线解密私钥的 CLI**，不将解密私钥提交普通 REST/API |

## 7. 未冻和冲突清单

- 现有主契约中的 `BatchTaskJob.state` 没有 `unknown`、`paused` 等完整状态，需要在下一轮**显式兼容修订**中补足，不能仅把 G2 `TaskSummary` 用作单独数据源而不迁移旧对象。
- 既有 API 使用 bearer/JWT 标注且多数 `Error` 是旧格式；新的 Cookie/CSRF + RFC 9457 约定需要分阶段迁移并形成兼容策略。
- `/api/v1/agent/enroll` 是 G2 新候选；可信根引导、CSR 私钥持有证明、单用 token 与首次签发响应丢失重试仍需完整密码学测试和协议定义。
- Protobuf 帧大小、心跳、缓冲、统计采样周期、版本协商、二进制签名信任与密钥吊销缓存需要 PoC 证据。
- 插件 Host Capability/manifest、文件流、证书公网签发、完整备份 CRUD 等扩展接口未在此基础层冻结；见 `G2-FREEZE-AND-GAP-REGISTER.md`。
