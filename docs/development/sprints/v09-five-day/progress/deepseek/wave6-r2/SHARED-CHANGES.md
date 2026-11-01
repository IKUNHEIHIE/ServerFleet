# V09-R6-DS-03 · 共享差异与补丁（deepseek-06）

基线 `c9013f4`；代码 `cde4a21`。本轮 author 未改 `apps/controller/server/server.go`、`apps/agent/connect.go`/`main.go`、根迁移登记、根 OpenAPI 配置或 Proto/gen；以下为总控可应用差异。文件链路按任务要求交 CX，DS 不写 `internal/files`。

## 1. Nginx typed action（Agent 注册 patch）

`apps/agent/gateway_capability.go` 已提供：

```go
gw, err := newGatewayCapability(gatewayCapabilityConfig{
    Prefix: prefix, SiteDir: siteDir, ArtifactDir: artifactDir,
    MainConfig: mainConfig, NginxBinary: "nginx",
}, execGatewayRunner{})
actions := controlledCommandActions()
for id, action := range gw.Actions() { actions[id] = action }
executor, err := newAgentExecutor(ledger, actions)
```

- 新 CLI 参数只接受绝对、operator-owned 路径：`-gateway-prefix`、`-gateway-site-dir`、`-gateway-artifact-dir`、`-gateway-main-config`、`-nginx-binary`；未配置时不注册 gateway action（不是默认放行）。
- `AgentHello.CapabilityIds` 建议增加 `gateway.nginx.v1`；Controller `agentAllowedActions()` 加：
  ```go
  "gateway.nginx.site_apply":  "1",
  "gateway.nginx.site_remove": "1",
  ```
  并仅在显式 gateway 配置且审批/任务准入正常时握手授予。
- Action 参数（tasks 的 `map[string]string`）只允许：
  - apply：`operation=apply, site_path, artifact_path, config_sha256, takeover_approved=true|false, approval_ref`（可选）
  - remove：`operation=remove, site_path, config_sha256`
  不允许 `validate_command`/`reload_command`/任意 shell；`gateway.BuildNginxApplyTask` 已移除这些参数，Agent 固定执行 `nginx -t` 与 `nginx -s reload`。
- 修改类 `gatewayTaskHost.submit` 使用 `tasks.OperationClassModifying`；Controller 的 `RequireApproval` 默认对 modifying 要求审批，不能用 standard 旁路。root 写 `/etc/nginx`/reload 的实际降权/审批仍由 Agent 安装/PRIV 方案决定。

## 2. Controller 发布桥与任务宿主注入（server.go patch）

新增 `apps/controller/server/gateway_tasks.go` 已提供：
- `GatewayArtifactUploader`（由 CX 文件任务组合实现；未注入则网关阶段失败关闭）
- `gatewayTaskHost`（apply 前上传 artifact，再用统一 task 提交 apply/remove）
- `NewProductionPublishingBridge(ProductionPublishingConfig{...})`，内部使用 `publishing.NewProductionBridge` 的 DNS/证书/网关真实端口
- `NewDNSPublisherSecretBorrower` / `NewNotificationSecretBorrower`
- `NewPublicationCleanupCleaner`

建议 `server.Config` 增加：

```go
GatewayArtifacts       GatewayArtifactUploader             // mandatory for gateway stage
CertificateInstallPath map[string]CertificateInstallPaths  // certId -> absolute cert/key paths
ProviderCredentials    ProviderCredentialLookup            // ref -> current owner/revision/module
```

`server.go` 组合（核心顺序）：

```go
pubBridge := publishing.NewLocalBridge(cfg.PublishingBridge)
if cfg.GatewayArtifacts != nil { // explicitly configured production bridge
    bridge, err := NewProductionPublishingBridge(ProductionPublishingConfig{
        Plans: publishingRepo, DNS: dnsService, Certificates: certificatesService,
        Gateways: gatewayService, Nodes: nodesRepo, ServiceGroups: serviceGroupsRepo,
        Tasks: tasksService, Artifacts: cfg.GatewayArtifacts,
        CertificatePath: cfg.CertificateInstallPath,
    })
    if err != nil { return cleanup(err) }
    pubBridge = bridge
}
publishingService, err := publishing.NewService(publishing.Options{
    DB: db, Repo: publishingRepo, Groups: sgLookup,
    DNS: pubBridge.DNS, Certificates: pubBridge.Certificates, Gateway: pubBridge.Gateway,
    Audit: auditRecorder, Authorizer: authorizer, Now: now,
})
```

- **不得**在缺少 `GatewayArtifacts`/发布目标列时回退到旧的只保存 service-group ID；`NewProductionBridge` 对 `TargetsPersisted=false`/目标 revision 不匹配返回 `ErrProductionTarget`。
- 发布 HTTP 的 202/targetsPersisted 不是执行成功；`Run`/`RollbackAll` 才是阶段证据。

### 2.1 Controller 清理 journal 注入

```go
cleaner, err := NewPublicationCleanupCleaner(dnsService)
cleanupJournal, err := certificates.NewCleanupJournal(certificates.CleanupJournalOptions{
    DB: db, Audit: auditRecorder, Authorizer: authorizer,
    Cleaner: cleaner, MaxAttempts: 5, Backoff: time.Minute,
})
s.CertificateCleanup = cleanupJournal
```

- 在 ACME `IssueDNS01` 失败且 `issued.DNS01Records` 非空时调用 `cleanupJournal.RecordFailure(ctx, subject, requestID, planID, domain, records)`；成功后由管理员/系统重试清理；清理失败不能变成证书成功。
- 建议 HTTP 注册（候选路径见 `api/openapi/revisions/v09-r7/services-candidate.yaml`）：`GET /api/v1/certificates/cleanup`、`POST .../{cleanupId}/retry`、`POST .../{cleanupId}/cancel`；主体/request ID 必须来自当前认证，不能用 Web 传入 owner。
- `RetryDue` 使用专用 `cleanup-service` 主体，需组合授予 `certificates.cleanup.retry`；`NewPublicationCleanupCleaner` 通过 dns 服务精确匹配 provider ID/FQDN/value，不删除无关记录。

## 3. 供应商秘密借用组合（server.go patch）

```go
lookup := cfg.ProviderCredentials
if lookup != nil && credentialsService != nil {
    dnsBorrower, err := NewDNSPublisherSecretBorrower(credentialsService, lookup)
    cloudflare, err := dns.NewCloudflareProvider(dns.CloudflareConfig{Secrets: dnsBorrower})
    cfg.DNSProvider = cloudflare
    smtpBorrower, err := NewNotificationSecretBorrower(credentialsService, lookup)
    cfg.SMTPSender = smtp.New... // Config{Secrets: smtpBorrower}
    qqBorrower, err := NewNotificationSecretBorrower(credentialsService, lookup)
    cfg.QQSender = qq.New... // Config{Secrets: qqBorrower}
}
```

- `ProviderCredentialLookup` 必须从当前 DNS account `credential_ref` / notification channel `secretRef` 查出当前 CredentialID、owner、module、revision；不存在/撤权返回 false，不能返回空凭据。
- `server.go` 现有 credential gate 只允许 AI，需按 module/scope 扩展 `dns-provider`、`smtp-password`、`qqbot-secret`；`UseForService` 的 before/after 检查覆盖实际供应商 I/O；不得新增通用 HTTP/tool 解密入口。
- 真实 Cloudflare/SMTP/QQ 账号仍未授权；使用本地 TLS fake 证据，不写真实秘密。

## 4. ACME cleanup DDL（候选，未登记）

`internal/certificates/cleanup_migrations.go`：

```sql
CREATE TABLE certificate_dns_cleanup (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_id TEXT NOT NULL, request_id TEXT NOT NULL, plan_id TEXT NOT NULL DEFAULT '',
  domain TEXT NOT NULL, fqdn TEXT NOT NULL, record_id TEXT NOT NULL, record_value TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('pending','retrying','resolved','canceled','failed')),
  attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error_code TEXT NOT NULL DEFAULT '', next_attempt_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE (fqdn, record_id)
);
CREATE INDEX certificate_dns_cleanup_due ON certificate_dns_cleanup (state, next_attempt_at);
```

- 总控接收后加入集中 `Migrations()`（建议编号 `ds/0062_acme_cleanup`）并更新迁移计数；不改历史 19/21 项。`CleanupMigrations()` 是本包候选构造器，当前未自动注册。
- 测试通过 `certificates.CleanupMigrations()` 验证；生产未登记前 journal 不可用。

## 5. OpenAPI 根登记（patch）

`redocly.yaml` 增加（不修改三份原候选）：

```yaml
apis:
  v09-r7-services:
    root: api/openapi/revisions/v09-r7/services-candidate.yaml
```

候选文件已 `redocly lint` PASS；包含发布目标/运行/rollback-all/cancel、`targetsPersisted`、清理记录与错误/冲突。

## 6. 文件通道黑盒交接（CX 本轮范围，DS 不修改）

`tests/integration/v09/services/file_transport_blackbox_test.go` 使用公共 `transport`/生成类型确认当前仅接受 skip/new-file，并明确失败：
- `overwrite`/`rename`/`createParents=true` → `transport.ErrLinkRejected`；
- read 带 size 但无 digest → 拒绝；write 无 digest/非规范 size/路径逃逸 → 拒绝；
- `FileOffer` 只接受 NORMAL、空插件、schema `"1"`、digest 64hex；
- CX `FileSession.Open` 当前固定发 `resume_offset=0` 且要求 reply relative path 完全等于请求，因此多 item 续传/rename 不得由 Controller 假报成功；需 CX 在自身文件执行模型内补齐或明确拒绝。
- 本机 Windows 下 CX Linux 文件真实用例 SKIP；真实 WSS 双 Agent/字节 SHA/取消/撤权/UNKNOWN 仍由 CX/总控在 Linux/双端验证，DS 不以模块假端口替代。
