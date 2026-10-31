# DS Wave 3 接线交接（deepseek-02）

- 任务：`V09-5D-DS` Wave 3（`DS-W3-01`—`DS-W3-04`）
- 会话：`deepseek-02`；分支 `feature/v09-services-ds02`；起始基线 `98f39e04e73ee3150b9f6146d92102c59e5358c3`
- 代码检查点：`abcfb30`、`3ce4073`、`d9f5e1e`、`c6715f7`、`83a86de`、`0dcd9f1`、`bca1a70`、`a7e0852`
- 未改动 `apps/controller/**`、`apps/web/**`、公共配置/锁/契约；未新增迁移（保持 Wave 2 的 19 项迁移断言）。

## 1. 需要总控在 `apps/controller/server/server.go` 完成的挂载

| 域 | 注册调用 | 说明 |
|---|---|---|
| monitoring | `monitoring.RegisterHTTP(s.mux, monitoringService, riskSource, authenticator)` | 新增文件 `internal/monitoring/http.go`；`riskSource` 可为 nil，此时 traffic-risks 返回 503 |
| quota | `quota.RegisterHTTP(s.mux, quotaService, authenticator)` | 新增文件 `internal/quota/http.go` |
| certificates | `certificates.RegisterHTTP(s.mux, certificateService, groupMembers, authenticator)` | 新增文件 `internal/certificates/http.go`；`groupMembers` 为 nil 时 POST 返回 503 |
| publishing | `publishing.RegisterHTTP(s.mux, publishingService, authenticator)` | 新增文件 `internal/publishing/http.go` |
| gateway / nodes / tasks | 现有 `RegisterHTTP` / `Register` 调用无需改签名 | 新路由随包内注册自动生效 |

路径同时注册了 Wave 3 规范路径与候选契约/前端已使用的无前缀路径：

- `/api/v1/monitoring/availability-monitors` 与 `/api/v1/availability-monitors`
- `/api/v1/monitoring/traffic-risks` 与 `/api/v1/traffic-risks`
- `/api/v1/publishing/{preview,publications...}` 与 `/api/v1/publications...`
- 证书路径与契约一致：`/api/v1/certificates/internal/distributions`

## 2. 需要总控新增构造的业务服务（Wave 2 尚未构造）

```go
monitoring.NewService(monitoring.Options{
    DB: db, Repo: monitoringRepo, Audit: auditRecorder, Authorizer: authorizer,
    Prober: cfg.MonitoringProber, Notifier: notificationsAdapter,
    AllowedHosts: cfg.MonitoringAllowedHosts, Now: now,
})
quota.NewService(quota.Options{
    DB: db, Repo: quotaRepo, Nodes: nodesRepo, Audit: auditRecorder,
    Authorizer: authorizer, Notifier: notificationsAdapter, Now: now,
})
certificates.NewService(certificates.Options{
    DB: db, Repo: certificateRepo, Audit: auditRecorder, Authorizer: authorizer,
    Issuer: issuerPort, Tasks: certificateTaskAdapter, GroupRevisions: serviceGroupAdapter, Now: now,
})
publishing.NewService(publishing.Options{
    DB: db, Repo: publishingRepo, Groups: publishingGroupAdapter,
    DNS: dnsPublisher, Certificates: certificatePublisher, Gateway: gatewayPublisher,
    Audit: auditRecorder, Authorizer: authorizer, Now: now,
})
```

端口约束：

- `monitoring.Prober` 为 nil 时探测失败关闭（创建/列表仍可用）；`Notifier` 为 nil 时告警不伪造。
- `quota.Notifier` 为 nil 时提醒失败关闭。
- `certificates.Issuer`、`Tasks`、`GroupRevisions` 任一为 nil 时对应写操作失败关闭（503/502）。
- `publishing` 的 `NewService` 要求 DNS/证书/网关三个 publisher 非 nil；未提供真实适配前**不要挂载** publishing，或提供显式失败关闭的适配器。
- 所有 Authorizer 继续使用组合 `composedAuthorizer`；所有写操作需要 Controller 的 CSRF/Origin/Idempotency 中间件，包内只信任注入的 Principal。

## 3. 适配器建议

```go
// monitoring.TrafficRiskSource
type quotaRiskAdapter struct{ svc *quota.Service }
func (a quotaRiskAdapter) ListTrafficRisks(ctx context.Context, subject string) ([]monitoring.TrafficRisk, error) {
    risks, err := a.svc.ListTrafficRisks(ctx, subject)
    if err != nil { return nil, err }
    out := make([]monitoring.TrafficRisk, 0, len(risks))
    for _, r := range risks {
        out = append(out, monitoring.TrafficRisk{NodeID: r.NodeID, Kind: r.Kind, Severity: r.Severity, Estimated: r.Estimated})
    }
    return out, nil
}

// certificates.GroupMembers：使用 servicegroups.Repo.Get 返回成员 nodeId
// certificates 的 GroupRevisions 与 publishing 的 GroupLookup 也可由同一个 servicegroups 适配器实现；
// publishing.GroupRef.NodeIDs 必须是当前成员快照，Revision 必须是当前 revision。
```

证书 `Issuer`/`TaskSubmitter` 与 publishing 的三个 publisher 目前没有生产实现（属外部 provider/transport 接缝）。Wave 3 只在测试中使用受控 fake；真实接线前相关写端点应保持 503/502 失败关闭，不能伪造成功。

## 4. 权限目录（Controller 单写）

Wave 2 的 `ExtraPermissions()` 需要补充以下动作，否则非管理员即使有节点授权也会被目录动作校验拒绝：

- `nodes.suspend`、`nodes.revoke`（resourceType `node`）
- `tasks.resume`（resourceType `node`）
- `monitoring.read` / `monitoring.write`
- `quota.read` / `quota.write`（节点级，`Check.NodeIDs` 已由包内提供）
- `certificates.read` / `certificates.distribution.create` / `certificates.revoke`
- `publishing.read` / `publishing.write` / `publishing.plan_create`

`AuditActions()` 会自动包含新增的 `nodes.suspend`、`nodes.revoke`、`tasks.resume`（通过各域 `ServiceActions()`），无需手写。

## 5. 语义与限制（必须随接线记录）

1. **tasks resume**：仅允许 `queued` 且未开始、未请求取消的任务；重新鉴权并通过仍有效的审批后延长离线等待并 revision+1。终态/运行中/待审批/取消中一律 409；从不重派已派发或未知结果。
2. **nodes suspend/revoke**：只改 Controller 节点库存状态；agent 心跳不会覆盖 suspended；revoke 后 agent 更新被拒绝。活动 WSS 连接关闭与节点证书吊销仍由 transport/CX 接线负责，本包不宣称已完成。
3. **nodes identity/capabilities**：nodes 域没有持久证书/能力记录，响应显式 `recorded:false`，不伪造指纹或协议版本；diagnostics 只由库存状态/最后见到时间派生。
4. **nodes expectedRevision**：节点无持久 revision，suspend/revoke 传非 0 `expectedRevision` 返回 400 `revision_not_supported`（不静默丢弃并发校验）。
5. **gateway checks**：仅返回已持久化的聚合标志（nginxInstalled/checksPassed/nginxVersion/lastError）；`recorded:false` 表示没有记录，逐项检查历史不伪造。
6. **certificates credentialType**：HTTP 适配层用 `<serviceGroupId>.<credentialType>.internal` 生成 CN，并在 GET 时从 CN 后缀重建；未新增数据库列，避免改变 Wave 2 集成测试固定的 19 项迁移。私钥/KMS 引用从不出现在响应、审计或日志中。
7. **publishing preview**：只校验服务组存在、成员非空、revision 匹配与域名；不评估 DNS/证书/网关 provider 健康，也不伪造 configDiff。`gatewayIds` 仅做格式校验，计划表不持久化网关列表（既有 domain 模型限制）。
8. **traffic-risks**：由 `quota.Service.ListTrafficRisks` 生成，按节点读授权过滤；无 usage 时不产生阈值风险，只保留策略到期风险。
9. 未新增 `GET /api/v1/nodes/{nodeId}` 详情端点（不在 Wave 3 DS 清单内）；GM 节点详情仍是候选适配。

## 6. 验证

DS 分支已运行：

- `go test ./internal/... ./tests/...`：全部 PASS（含新增 SQLite WAL 端到端 `TestWave3ServiceHTTPEndToEnd`）。
- `npm run check`：`policy`、`contracts`、`go`、`web`、`git` 全部 PASS。
- `npm run check:security`：`npm audit` 0 漏洞；`govulncheck` 无漏洞。

真实外部环境（Linux/1C1G、真实 CA、DNS、Nginx、QQ/SMTP、Agent WSS）仍 UNTESTED；本包不以 fake 端口通过作为生产验收。
