# 当前接口说明

安装目录的 `GET /api/v1/installations/catalog` 仍须管理员会话；未配置时返回 `200 {"available":false}`，不是注册失败或应跳过的安全门。符合条件的已有主控可用[安装向导](../../deployment/QUICK-SETUP.md)生成公开信任材料与私有 catalog 配置；向导不新增业务接口，不代替管理员登录、令牌生成或节点审批。完成后须登录确认 `available:true` 并实际接入节点，不能仅凭 systemd 启动成功判定安装完成。

> v0.9.1 / R37：新增 Agent 身份续期与安全切换、明确接入读取授权、只读升级预检、通用就绪和有界节点诊断。现行迁移共50项（旧49项不变）。行为与接口见 [身份运维说明](../development/IDENTITY-OPERATIONS.md)，升级/反代见仓库 deployment/UPGRADE.md 与 deployment/REVERSE-PROXY.md；旧审计/CI记录仅说明其当时基线。


2026-11-12 / R34。供前端/脚本/联调定位实际接口；不是G2冻结声明。先读安全和状态语义，再从[路由索引](ROUTES.md)进入实际处理器及对应候选。可机读索引：[route-inventory.json](route-inventory.json)。

## 入口与认证

- 正式Web调用同源 `/api/v1`，直接HTTPS。浏览器会话Cookie为 `__Host-sf_session`，HttpOnly/Secure；写请求按当前会话发送 `X-CSRF-Token` 和精确Origin，不信任转发头来伪造TLS。请求ID由Controller产生并随响应/审计关联。
- 全局功能权限与节点资源权限同时生效。管理员没有隐式的所有节点授权。服务组任务使用冻结成员/修订；每次执行、帧和敏感响应还要复核当前授权、隔离门、审批与凭据版本。
- 受限令牌管理使用Cookie会话：`GET/POST /auth/tokens`、`DELETE /auth/tokens/{tokenId}`。目前正式节点/任务HTTP仍使用Cookie认证，**尚无生产Bearer读取适配**；`AuthorizeRestrictedToken`/`BearerCommitGate`属于校验端口，不能将管理成功等同于Bearer可用。令牌仅设计为script-read、nodes.read/tasks.read、1—200显式节点、60—3600秒，不允许写入/AI/插件/注册/执行。
- Agent的 `/api/v1/agent/link` 是WSS+Protobuf协议入口，需实际mTLS身份/能力协商与逐帧授权，不是浏览器REST。`POST /api/v1/agent/enroll` 是严格HTTPS首次注册协议，令牌/CSR/Root信任校验独立于Cookie。两者通过无method的转发挂载进入专用协议处理器，因此不列入AST的method/path声明计数。
- `/healthz`仅表示活性，不能代替部署、节点、权威或业务健康。更新readiness还有独立本地监督进程nonce条件。

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

## 手动监控探测

`POST /api/v1/availability-monitors/{monitorId}/probe`：无查询参数，空体或严格空JSON对象；使用已保存的目标/来源，不从请求体覆盖。写权限与实际点击者的源节点授权必须同时满足。响应包含 `monitorId/status/state/checkedAt/consecutiveFailures`，可选 `latencyMs/statusCode/error/sourceNodeId/taskId`；没有观测值不补零。

`status`是本次观察的up/down/unknown；`state`保留阈值后的业务up/down/unknown；监控列表使用healthy/unhealthy/unknown/paused，并在源未知时明确显示unknown。UNKNOWN不发虚假的宕机/恢复通知，编辑名称/渠道不会抹去未知标记。

节点目标超时1—30秒，执行准入预留5秒、Controller观察预算另预留8秒；Controller源目标超时1—300秒。R34使该HTTP响应期限单独有界到310秒，浏览器请求期限315秒；普通请求的15秒客户端/40秒服务端期限不变。调用方主动取消仍生效，期限不是自动重试授权。

## 候选与版本阅读顺序

1. 原始G2/Xray候选保留字节，不能据其推断全部功能可执行。
2. `api/openapi/revisions/v09-r6`至`v09-r33`是各次独立增量：AI/预算、文件、加密备份、可信发行、PKI/DNS/发布、受控命令、续传、监督更新、灾备、续签、AI循环、异地备份/处置、续签安装、一键安装、持续监控、节点探测。
3. [R34运行接口补充](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json)补令牌/工作流、默认DNS、网关调度、插件状态/拒绝式激活、发布取消和活性等此前缺说明的真实入口。别名由索引链接同一规范操作，不复制相互漂移的schema。
4. [R34兼容与审计记录](../contracts/revisions/v09-r34-audit.md)说明本次变化和剩余不一致。路径参考覆盖不等于字段覆盖、更不等于生产验收。

## 维护索引

```sh
node scripts/api-docs.mjs --write
node scripts/api-docs.mjs --check
```

Go AST扫描正式Controller/internal下的Handle/HandleFunc、有限字面量range及Patterns返回，不扫描测试/参考文档/可选插件业务。包含条件装配和unavailable处理器；无method转发/动态运行态不由静态声明推断。`npm run check:contracts`自动拒绝索引漂移，原始候选、Proto/gen和历史迁移仍保持。
