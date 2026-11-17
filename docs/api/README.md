# 当前接口说明

核对：2026-11-17 / v0.9.2 增量。先读 [开发交接](../development/HANDOFF.md)、[当前状态](../development/CURRENT-STATE.md)。本文及增量修订用于定位实际接口，未宣称统一 OpenAPI 已全字段核验/冻结。

v0.9.2 / [R39](../contracts/revisions/v09-r39-pki-raw.md) 修复 provisioned 完整证书链及有效旧回执的公开 PEM 响应，不新增业务路由或字段；撤销/过期/代次校验不变。安装 catalog 的既有 url/sha256 默认用于固定版本 raw 安装入口，新版命令增加公开 bootstrap CA，令牌仍不进入命令。

安装目录的 `GET /api/v1/installations/catalog` 仍须管理员会话；未配置时返回 `200 {"available":false}`，不是注册失败或应跳过的安全门。符合条件的已有主控可用[安装向导](../../deployment/QUICK-SETUP.md)生成公开信任材料与私有 catalog 配置；向导不新增业务接口，不代替管理员登录、令牌生成或节点审批。完成后须登录确认 `available:true` 并实际接入节点，不能仅凭 systemd 启动成功判定安装完成。

> v0.9.1 / R37：新增 Agent 身份续期与安全切换、明确接入读取授权、只读升级预检、通用就绪和有界节点诊断。现行迁移共50项（旧49项不变）。行为与接口见 [身份运维说明](../development/IDENTITY-OPERATIONS.md)，升级/反代见仓库 deployment/UPGRADE.md 与 deployment/REVERSE-PROXY.md；旧审计/CI记录仅说明其当时基线。


供前端/脚本/联调定位实际接口；先读安全和状态语义，再从[路由索引](ROUTES.md)进入实际处理器及对应候选。可机读索引：[route-inventory.json](route-inventory.json)，数量以生成结果为准，不沿用 R34 旧计数。

## AI 连续上下文与过程显示增量（2026-11-17）

- POST /ai/sessions/{sessionId}/runs/{runId}/continue：正文 expectedRevision、
  additionalSteps（1—24）、timeoutSeconds（60—3600）；202 返回 AgentRun。
  仅当前范围、当前 revision、maximum_model_steps_reached 的 limit_reached 可续接；
  累计最多 64 步/24 小时，UNKNOWN 等拒绝，不重放已确认步骤。
- POST /ai/model-settings/probe：管理员会话与原 Origin/CSRF；正文 model（可空，使用已保存默认）。
  响应 model/toolCalls/reasoning/checkedAt，表示本次合成函数/公开摘要探测结果；
  不返回密钥，不执行节点操作，不是任意供应商 URL 代理。
- 原 POST /ai/sessions/{sessionId}/messages/{messageId}/generate 保持 JSON 响应兼容。
  Accept: text/event-stream 时发送 preview（kind/text 的累计暂存预览）、
  completed（已保存 Message）或 error（固定 code）。只接受 completed 为成功；
  预览经当前权限与内容过滤，原始供应商错误、凭据和隐藏推理不返回。
- Message 新增可选 reasoningSummary，含已过滤的公开简要摘要；
  AgentRun/AgentStep 路径不变，步骤 plan 可包含公开摘要或计划说明。
- 新建运行 maxSteps 上限 64，timeoutSeconds 上限仍 3600。
  /context/compact 不再截断关键原文，64 KiB 以上拒绝而非丢失信息；
  并发源对话变化返回冲突，原摘要与历史消息保持。

所有路径以 /api/v1 为前缀。[完整边界](../development/AI-AGENT-LOOP.md)。


## PostgreSQL 维护兼容增量

本次保持 HTTP 路径、DTO、加密包 format=1 与文件命名不变；
备份验证仍验证可移植 SQLite 恢复快照，不能把在线 PostgreSQL 状态锚点当作备份。
新增离线 CLI recovery restore-postgres、-db-dsn-file 与监督器可选数据库配置，
不构成 HTTP 免审批导入/激活接口。[维护说明](../../deployment/POSTGRES-MAINTENANCE.md)。


## 入口与认证

- 正式Web调用同源 `/api/v1`，直接HTTPS。浏览器会话Cookie为 `__Host-sf_session`，HttpOnly/Secure；写请求按当前会话发送 `X-CSRF-Token` 和精确Origin，不信任转发头来伪造TLS。请求ID由Controller产生并随响应/审计关联。
- 全局功能权限与节点资源权限同时生效。首个 Bootstrap 管理员拥有当前节点的基础管理权限；普通管理员和其他用户没有隐式节点授权。服务组任务使用冻结成员/修订；每次执行、帧和敏感响应还要复核当前授权、隔离门、审批与凭据版本。
- 受限令牌管理使用Cookie会话：`GET/POST /auth/tokens`、`DELETE /auth/tokens/{tokenId}`。目前正式节点/任务HTTP仍使用Cookie认证，**尚无生产Bearer读取适配**；`AuthorizeRestrictedToken`/`BearerCommitGate`属于校验端口，不能将管理成功等同于Bearer可用。令牌仅设计为script-read、nodes.read/tasks.read、1—200显式节点、60—3600秒，不允许写入/AI/插件/注册/执行。
- Agent的 `/api/v1/agent/link` 是WSS+Protobuf协议入口，需实际mTLS身份/能力协商与逐帧授权，不是浏览器REST。`POST /api/v1/agent/enroll` 是严格HTTPS首次注册协议，令牌/CSR/Root信任校验独立于Cookie。两者通过无method的转发挂载进入专用协议处理器，因此不列入AST的method/path声明计数。
- `/healthz`仅表示活性；通用 `/readyz`检查启动及子系统就绪，不能代替业务或节点健康。更新还有独立本地监督进程 nonce 就绪条件，不能相互替代；详见[身份运维](../development/IDENTITY-OPERATIONS.md)。
- `GET /install/agent.sh` 是安装边界入口，无需会话：只返回由 installation catalog 派生的静态脚本（预编译 Agent URL、Root/HTTPS 证书、Root pin、接入审批循环），不含令牌，节点名称作为脚本参数。摘要随 `GET /api/v1/installations/catalog` 的 `agentScript` 字段下发，固定为实际响应字节的 SHA-256；未配置 catalog 时返回 404。该入口只用于首次安装，不替代令牌、mTLS 或审批授权。

## 请求与响应规则

| 项目 | 实际规则 |
|---|---|
| 请求体 | 依据所属处理器的DTO/修订说明，通常JSON且拒绝未知字段，部分空体操作允许无正文；不可把列表响应原样回传。上传/下载/协议流另有边界 |
| 幂等 | 需要幂等的写入显式提供 `Idempotency-Key`；不是每条POST都强制同一规则。相同键不同冻结参数不能复用；网络错误后不得擅自重试副作用 |
| 并发修订 | 按端点使用 `If-Match`、`expectedRevision`、scopeRevision或计划摘要，不可互换。409/412/428均应刷新事实后由用户决定 |
| 列表 | 当前域有 `{items,nextCursor}`、`{items,page}`、`{items}`等已知形状。Web兼容层只接已知形状；缺失字段保持缺失，不伪造分页/统计 |
| 错误 | 当前实现同时存在problem JSON顶层code和`{error:{code,message}}`。客户端识别状态/固定code/requestId，不展示原始错误正文/秘密 |
| 任务返回 | 200/201/202可能表示保存、排队或已接受，不能据HTTP成功认定安装/发布/投递成功；读取实际任务、逐目标执行与typed回执 |
| SSE/下载 | 流期间持续检查授权/隔离门；Last-Event-ID/事件游标按对应域规则，撤权停止流，不继续返回旧权限数据 |
| UNKNOWN | 保留原执行未知结果，不自动重放副作用。修复/核验使用新的授权任务，人工结案不改写原结果 |

所有边界按具体处理器实施。索引的路径匹配仅解决“去哪找接口”，不证明旧候选的每个字段已与当前DTO一致；现行操作/字段以独立修订和对应HTTP处理器交叉核对。

## 主要接口域

| 功能 | 主要路径 | 字段与运行前提 |
|---|---|---|
| 身份/授权/凭据 | `/auth/*`、`/users`、`/roles`、`/permissions`、`/resource-grants`、`/credentials` | 当前会话/CSRF、独立节点授权；凭据只读元数据/引用，秘密仅受控借用 |
| 添加节点 | `/node-enrollment-tokens`、`/node-enrollments` | 管理员签发单次令牌，10分钟内；`/enrollment-tokens`、`/enrollments`是旧别名 |
| 节点/分组/服务组 | `/nodes`、`/groups`、`/service-groups` | 区分分类与业务成员，节点操作按资源授权 |
| 任务/文件 | `/tasks`、`/files`、`/transfers` | 预览/独立审批/终态；文件根映射、摘要、实际路径和显式续传 |
| 网关/发布 | `/gateways`、`/gateway-routes`、`/publications` | 可信部署配置及当前目标权限，发布阶段任务/补偿；`/publishing/publications`兼容同处理器 |
| DNS/证书 | `/dns`、`/certificates` | Cloudflare、ACME、内部PKI配置；CSR/安装/续签/替换是独立任务链 |
| AI/工作流 | `/ai`、`/workflows` | AI资源范围/模型/审批；工作流1—32步、参数string-map、事件触发、步骤经统一任务 |
| 监控/通知/流量 | `/availability-monitors`、`/traffic-risks`、`/notifications`、节点traffic相关路径 | 源节点权限、当前owner、通知凭据、真实计数覆盖；旧`/monitoring/`前缀兼容 |
| 更新/备份/恢复/处置 | `/system`、`/backups`、`/disaster-recovery`、`/operations/incidents` | 可信supervisor、加密/存储凭据、恢复隔离、独立审批和新鲜证据 |
| 插件 | `/plugins`、`/plugins/xray-manager` | 目录/包/声明式元数据可存在；变更/执行需已准入Runtime和Host，当前存在明确501/503拒绝式入口 |

## AI 运维增量（2026-11-17）

AI 侧审批收敛为会话范围的三档：`per_action`（每次询问）、`automatic`（只读/低风险自动）、`full_access`（当前授权内不再逐项确认，root Shell 仍需管理员确认）。`PATCH /api/v1/ai/sessions/{sessionId}/approval-policy` 只接受 `{"mode": ...}`；审批模式变更不迁移资源授权，但会把排队中的 Agent 运行带入新范围，在途步骤无法安全继续时显式标记 unknown。

`PATCH /api/v1/ai/sessions/{sessionId}/preferences` 接受 `model` 与 `reasoningEffort`（`low|medium|high|""`），仅改变本会话模型与 Responses `reasoning.effort`，不扩大资源范围。`GET /api/v1/ai/sessions/{sessionId}/context` 返回消息数、估算 tokens、摘要状态与当前模型；`POST /api/v1/ai/sessions/{sessionId}/context/compact` 接受 `{"keep": 2..20}`，生成并持久化本地抽取式摘要，不调用模型或凭据。`GET /messages` 对 Agent 运行消息保留 `agentRunId`，客户端按运行内嵌展示，不再把占位文本当作执行结果。`POST /api/v1/ai/sessions/{sessionId}/runs` 的 AI 任务不再要求任务层二次审批；`tasks.exec.v1` 与 `tasks.command` 一样使用任务提交权限回退，具体任务仍走统一授权、审批、审计与 UNKNOWN 语义。

## 节点管理补丁（2026-11-15，工作区待提交）

新增 `GET/PATCH/DELETE /api/v1/nodes/{nodeId}`。GET 返回 `nodeId/displayName/status/address/region/provider/countryCode/tags/updatedAt`，可选最近在线与 Agent 版本；不伪造 revision。PATCH 只接受 `name/address/region/provider/countryCode/tags/expectedUpdatedAt`，要求 nodes.write 和读取授权；expectedUpdatedAt 为读取时的元数据版本，冲突返回 409 node_metadata_conflict。心跳不使元数据版本失效。DELETE 接受必填 reason，要求 nodes.revoke，事务内撤销并归档；普通列表隐藏，历史关联和审计记录保留，重复删除幂等。归档不等于远程程序已卸载。

新增 `GET /api/v1/groups/node-results`，支持 mode=manual + groupId 或 mode=automatic + dimension(region/provider/tag) + value，可选 search（别名 q，不能共存）、status(all/online/offline)、cursor、limit(1–200)。返回 `{items,total,nextCursor}`，每次请求按当前 nodes.read 授权计算；游标绑定筛选，不授权资源。自动分类只读，不修改手动分组和权限。

接入令牌创建支持 name；省略或空白时生成可区分的 node-随机名称。页面将同一个名称用于令牌和安装命令。新增迁移 ds/0089_node_archives，工作区迁移共 51 项，历史迁移不改写。

节点能力接口 `GET /api/v1/nodes/{nodeId}/capabilities` 在握手诊断中额外返回 `cpuModel/cpuCores/memoryTotalBytes/diskTotalBytes`（AgentHello 新增字段 9–12，append-only；缺失表示未采集，不按 0 处理）。安装脚本改为创建专用用户与 systemd 服务并带 `-file-root`；文件能力需要非 root 身份，控制器启用文件分发时协议次版本为 1.1。

## DNS 独立可用（2026-11-16，工作区待提交）

- `POST /api/v1/dns/accounts` 与 `PATCH /api/v1/dns/accounts/{accountId}` 新增 `credentialRevision`（与 `credentialRef` 同传，0 拒绝）。账号记住凭据修订号，服务端借用凭据前按该修订校验。
- 新增 `GET /api/v1/dns/accounts/{accountId}/provider-zones`：实时读取 Cloudflare 账号下可导入 Zone（`providerZoneId/name/status`），只读，不写入本地。
- 新增 `POST /api/v1/dns/zones/{zoneId}/records/sync`：读取该 Zone 的 Cloudflare 记录并替换本地缓存；仅保留 A/AAAA/CNAME/TXT/MX，跳过其它类型；不修改远端记录。
- 账号检查改为同步 `POST /api/v1/dns/accounts/{accountId}/test`，返回 `status/zoneRead/recordWrite` 并回填凭据状态；控制台 Token 直填后自动创建 DNS 专用凭据，仅提交引用。
- 独立 DNS 请求不再依赖 ACME 执行配置：服务端为每次请求建立一次性凭据借用窗口（模块 `dns`、绑定修订号），窗口外拒绝；远端写操作仍走统一审批任务。
- 迁移 `ds/0091_dns_credential_revision` 追加 `credential_revision` 列；历史行为 0，需重新绑定凭据（控制台“轮换”）后才能通过供应商校验。

## AI 端点配置（2026-11-16，工作区待提交）

- Controller 新增私有启动参数 `-ai-text-endpoint`：部署级 OpenAI 兼容 Responses URL（必须 `https`、无 userinfo/query/fragment、路径以 `/responses` 结尾，例如 `https://<网关>/compatible-mode/v1/responses`）。未配置时沿用默认 OpenAI 端点。
- 端点只来自部署配置，浏览器/模型档案不能提交或覆盖 base URL；模型标识、凭据引用与 `credentialRevision` 仍由模型档案提交。
- 流式解析会忽略 `response.reasoning_text.*` / `response.reasoning_summary_*` 推理事件并保持序号连续性；只返回正式输出文本，未知事件仍拒绝。
- 新增系统权限 `ai.use` / `ai.manage` / `ai.approve`（管理员），此前未注册导致 AI 页面锁死。
- 修复消息提交缺少 `Idempotency-Key` 导致的 400：现在以客户端消息 ID 作为幂等键。
- 审批事件列表接口尚未实现；控制台对 404 显示“当前 Controller 未提供”，不再当作加载失败。
- 2026-11-17：预算窗口 API 保留但生成链路不再按预算拦截，控制台已移除“用量与预算”界面；模型配置迁入系统设置的 AI 模型页签。
- 新增 `GET /api/v1/ai/sessions`：列出当前用户自己的会话摘要（标题取首条用户消息，最多 50 条）；控制台左侧历史对话使用该接口。发送消息后控制台立即调用生成接口，不再需要手动点“生成回复”。OpenAI 兼容网关要求历史 assistant 消息以 `output_text` 分段提交。

## 手动监控探测

`POST /api/v1/availability-monitors/{monitorId}/probe`：无查询参数，空体或严格空JSON对象；使用已保存的目标/来源，不从请求体覆盖。写权限与实际点击者的源节点授权必须同时满足。响应包含 `monitorId/status/state/checkedAt/consecutiveFailures`，可选 `latencyMs/statusCode/error/sourceNodeId/taskId`；没有观测值不补零。

`status`是本次观察的up/down/unknown；`state`保留阈值后的业务up/down/unknown；监控列表使用healthy/unhealthy/unknown/paused，并在源未知时明确显示unknown。UNKNOWN不发虚假的宕机/恢复通知，编辑名称/渠道不会抹去未知标记。

节点目标超时1—30秒，执行准入预留5秒、Controller观察预算另预留8秒；Controller源目标超时1—300秒。R34使该HTTP响应期限单独有界到310秒，浏览器请求期限315秒；普通请求的15秒客户端/40秒服务端期限不变。调用方主动取消仍生效，期限不是自动重试授权。

## 候选与版本阅读顺序

1. 原始G2/Xray候选保留字节，不能据其推断全部功能可执行。
2. `api/openapi/revisions/v09-r6`至`v09-r33`是各次独立增量：AI/预算、文件、加密备份、可信发行、PKI/DNS/发布、受控命令、续传、监督更新、灾备、续签、AI循环、异地备份/处置、续签安装、一键安装、持续监控、节点探测。
3. [R34运行接口补充](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json)补令牌/工作流、默认DNS、网关调度、插件状态/拒绝式激活、发布取消和活性等此前缺说明的真实入口。别名由索引链接同一规范操作，不复制相互漂移的schema。
4. [R34兼容与审计记录](../contracts/revisions/v09-r34-audit.md)说明本次变化和剩余不一致。路径参考覆盖不等于字段覆盖、更不等于生产验收。
5. v0.9.1 / R37 的身份续期、读取授权、就绪/诊断及升级接口见 [身份运维](../development/IDENTITY-OPERATIONS.md)、[R37 修订](../contracts/revisions/v09-r37-identity-operations.md)与[候选](../../api/openapi/revisions/v09-r37/identity-operations-candidate.json)；不能继续用旧候选推断“无续期/无 readyz”。
6. [R39](../contracts/revisions/v09-r39-pki-raw.md)修复链与安装命令，不新增业务字段。前端重构按当前 DTO/处理器交叉核验候选，现有客户端只是消费方，不反向定义服务端契约。

## 维护索引

```sh
node scripts/api-docs.mjs --write
node scripts/api-docs.mjs --check
```

Go AST扫描正式Controller/internal下的Handle/HandleFunc、有限字面量range及Patterns返回，不扫描测试/参考文档/可选插件业务。包含条件装配和unavailable处理器；无method转发/动态运行态不由静态声明推断。`npm run check:contracts`自动拒绝索引漂移，原始候选、Proto/gen和历史迁移仍保持。
