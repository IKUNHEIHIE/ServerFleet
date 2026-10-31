# DS 模块接线清单（候选，供 CX/GM/总控）

日期：2026-10-30；分支 `feature/v09-services-ds02`。所有条目在总控接收前均为候选，不是冻结接口。HTTP 契约以 `api/openapi/g2-merged-candidate.yaml` 为输入；未注册的端点不返回 501/假成功。

## 共同约定

- 身份：HTTP `Authenticator func(*http.Request) (Principal, error)`；Controller 注入可信主体与 request id。DS 不解析 Cookie、Bearer、CSRF。
- 失败：固定 JSON `{"error":{"code","message"}}`；401 未认证、403 权限/来源、404 不存在、409 冲突、422 校验、503 依赖不可用、502 供应商/传输失败；500 不回显内部细节。
- 事务：`storage.DB.InTx(ctx, func(ctx, storage.Tx) error)`；写审计必须与业务同事务。
- 迁移（候选编号）：`ds/0010_audit`、`0020_nodes`、`0021_servicegroups`、`0030_tasks`、`0031_tasks_cx_fields`、`0040_files`、`0050_monitoring`、`0051_notifications`、`0052_quota`、`0060_dns`、`0061_certificates`、`0070_gateway`、`0080_publishing`。
- 备份：`storage.DB.BackupTo`（VACUUM INTO + quick_check）、`VerifyFile`、离线 `RestoreFile`（不覆盖现有库）。13 个迁移集的备份/恢复/损坏拒绝已测。

## 域清单

| 域 | 构造/端口 | HTTP 注册 | 已实现/已测 |
|---|---|---|---|
| storage | `Open`, `InTx`, `Migrator`, `Generations`, `BackupTo/RestoreFile` | — | 重启、坏库、回滚、取消、并发、迁移漂移/未知/失败、备份恢复 |
| audit | `NewSQLRecorder(db, validator, now)` | `audit.Register(mux, rec, auth, ReadGate)` | 追加/查询/单条/保留期/去重触发器/拒绝读取审计 |
| nodes | `NewService(Options{DB,Nodes,Groups,Audit,Authorizer,ReadPolicy,Now})` | `nodes.RegisterHTTP(mux, svc, auth)` | 列表/分类/分组 CRUD+成员；Agent 事件入口 `ApplyAgentUpdate` |
| servicegroups | `NewService(Options{DB,Repo,Nodes,Audit,Authorizer,ReadPolicy,Eligibility,DeleteGuard,Now})` | `servicegroups.RegisterHTTP` | CRUD/成员/暂停/派发预览；`Eligibility` 由 gateway+monitoring 适配，nil 时全部 `gateway_unverified` |
| tasks | `NewService(Options{DB,Repo,Audit,Authorizer,Dispatcher,Approvals,Nodes,...})` | `tasks.Register(mux, svc, auth, GroupResolver, ClassResolver, ReadGate)` | 提交/预览/列表/详情/审批/取消/executions/SSE；外部审批 `ApprovalConsumer`；派发仅 `Dispatcher` |
| files | `NewService(Options{DB,Repo,Audit,Authorizer,Submitter,Canceller,Uploads,Downloads,StagingDir,...})` | `files.RegisterHTTP` | 传输 CRUD/取消/上传会话/完成；字节通道 `UploadTransport/DownloadTransport` |
| dns | `NewService(Options{DB,Repo,Audit,Authorizer,Provider})` | `dns.RegisterHTTP` | 账号/Zone/记录；`Provider` nil 即 provider 操作 503 |
| certificates | `NewService(Options{DB,Repo,Audit,Authorizer,Issuer,Tasks,GroupRevisions,Now})` | 未注册（见下） | 内部分发/续签/吊销/轮换；私钥仅 KeyRef |
| monitoring | 见 `internal/monitoring` | 未注册 | SSRF 校验、状态迁移、维护窗、告警端口 |
| notifications | `NewService(Options{DB,Repo,Audit,Authorizer,SMTPSender,QQSender,Now})` | `notifications.RegisterHTTP(mux, svc, auth, ReadGate)` | 渠道/凭据引用/测试/设置/投递；响应不含 secretRef；去重/备用/重试上限 |
| quota | 见 `internal/quota` | 未注册 | 缺失≠0、超额仅告警、不自动断网 |
| gateway | `NewService(Options{DB,Repo,Nodes,ServiceGroups,Checker,Tasks,Audit,Authorizer,Now})` | `gateway.RegisterHTTP`（部分） | 网关列表/详情/启用、provision、provision 查询、路由 CRUD；路由创建需 `gatewayId` 扩展字段；`/checks` 未注册，避免用推断值冒充 serviceRunning |
| publishing | 见 `internal/publishing` | 未注册 | dns→cert→gateway 分阶段/重试/回滚 |

## 明确差距（不得当作已验收）

- certificates HTTP 未注册：候选要求“节点本地生成私钥+CSR / 信任包”，当前实现是 KeyRef 内部 PKI 分发；需契约 owner 决定后再加端点。
- DNS `/accounts/{id}/credentials` 未实现：DS 不接收原始 apiToken；账号创建/更新使用 `credentialRef`（CX secret store 引用）。UI 需改为引用式写入或由 CX 提供一次性转存端点。
- 文件上传字节通道：候选注明“待技术设计”；DS 只实现 staging/断点/校验与端口，HTTP 仅会话创建/完成；单会话单文件，多文件返回 400。
- 任务 SSE：`Last-Event-ID` 为数值；`tasks.ReadGate` 必须由 CX 实现，nil 时 403。
- 审计 eventId 为数字字符串；`resourceId` 查询参数尚未实现，需契约 owner 确认过滤语义。
- 所有真实外部依赖（Cloudflare/ACME、QQ/SMTP、Nginx、Linux 1C1G、mTLS Agent）均 UNTESTED/BLOCKED，需负责人授权的隔离环境。

## 组合根示例（伪代码）

```go
db, _ := storage.Open(ctx, storage.Config{Path: cfg.DB})
migrator, _ := storage.NewMigrator(audit.Migrations(), nodes.Migrations(), servicegroups.Migrations(),
    tasks.Migrations(), files.Migrations(), monitoring.Migrations(), notifications.Migrations(),
    quota.Migrations(), dns.Migrations(), certificates.Migrations(), gateway.Migrations(),
    publishing.Migrations())
migrator.Apply(ctx, db)

auditRec, _ := audit.NewSQLRecorder(db, validator, time.Now)
authorizer := cxAuthz.New(...) // CX 提供，支持 nodes.read 与各域 action
policy := cxAuthz.PolicyReader(...)

nodeSvc, _ := nodes.NewService(...)
sgSvc, _ := servicegroups.NewService(... Eligibility: gatewayEligibility{}, DeleteGuard: cxDeleteGuard{}, ...)
taskSvc, _ := tasks.NewService(... Dispatcher: cxTransport{}, Approvals: cxApprovalConsumer{}, Nodes: nodeOnlineLookup{}, ...)
taskSvc.Recover(ctx) // 启动一次；在途结果标 UNKNOWN，不自动重跑
go taskSvc.Run(ctx)

mux := http.NewServeMux()
nodes.RegisterHTTP(mux, nodeSvc, cxPrincipal)
servicegroups.RegisterHTTP(mux, sgSvc, cxPrincipal)
tasks.Register(mux, taskSvc, cxPrincipal, cxGroupResolver{}, cxClassResolver{}, cxTaskReadGate{})
files.RegisterHTTP(mux, fileSvc, cxPrincipal)
dns.RegisterHTTP(mux, dnsSvc, cxPrincipal)
audit.Register(mux, auditRec, cxPrincipal, cxAuditGate{})
```
