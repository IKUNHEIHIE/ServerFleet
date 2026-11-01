# V09-R6-DS-03 · Wave6-R2 状态（deepseek-06）

- 任务 `V09-R6-DS-03`；会话 `deepseek-06`；分支 `feature/v09-r6-services-ds06`；基线 `c9013f4231c5f0a586eb0af26386f6e265996c0c`；审查 `codex-review-02`。
- 代码提交 `cde4a21`（发布/清理/借用/黑盒）与 `3c3c4ef`（cleanup HTTP 候选）；文档 HEAD 以 `git log -1` 为准。
- 未改 `internal/files`、apps 既有入口、根锁/CI/Proto/gen；新增 Agent/Controller 文件均按任务单授权，未合并 main、未操作 awskr、未使用真实外部账号。

## 1. 实际发布阶段与补偿

- `internal/publishing/production.go` 新增真实阶段桥：DNS 通过 `DNSRecordWriter` 逐目标地址 ensure/删除并只补偿本次创建的记录；证书阶段只验证已预置的活跃证书（域名/有效期）并读取安装路径，不签发/吊销共享证书；网关阶段要求恰好一个显式 gateway、真实 service upstream 与证书路径，创建/更新 route 并通过 Controller 任务宿主提交 Nginx apply。
- 三个 stage 在任何 provider 调用前重新读取 plan，要求 `TargetsPersisted=true`、`TargetRevision`/`ZoneID`/`GatewayIDs`/`CertificateID` 与冻结计划一致；旧 schema/目标不持久直接 `ErrProductionTarget`，不会退回 service-group-only。
- `Service.Run` 每阶段重新读取服务组 revision 并重新授权；`RollbackAll` 每阶段补偿前重新授权；`Cancel` 新增（无已执行阶段才可取消，不宣称补偿/不打断其他进程 provider 调用）；并发锁/阶段状态/幂等保持。
- 控制器新增 `NewProductionPublishingBridge`、`publicationDNSWriter`、`publicationGatewayAddresses`、`publicationCertificateInspection`、`publicationServiceUpstreams`、`publicationGatewayApplier`、`gatewayTaskHost`（见 `apps/controller/server/gateway_tasks.go`），但 `server.go` 注入仍是总控 patch；未注入前生产阶段失败关闭。
- 测试：`go test ./internal/publishing/ -count=1` PASS（目标持久化/阶段请求、逐阶段 revision/撤权、RollbackAll 阶段撤权、Cancel、生产 bridge DNS 全成功/部分失败清理/仅删除自建记录、证书、网关、旧 schema 拒绝）。未作真实 DNS/证书/网关生产端到端；需外部账号与 Controller 接线。

## 2. 受控 Nginx 动作

- 新增 `apps/agent/gateway_capability.go`：固定 action `gateway.nginx.site_apply/remove`、固定 schema 参数（operation/site_path/artifact_path/config_sha256/takeover_approved/approval_ref），拒绝任意 validate/reload 文本；artifact 摘要/SHA 校验、prefix/site/artifact 根约束、逐段 symlink 拒绝、原子写、固定 argv `nginx -t` / `nginx -s reload`、失败恢复旧文件、remove 按磁盘摘要、幂等 apply。所有执行仍应由 Agent 统一任务/审批触发。
- Controller `gatewayTaskHost` 用统一 task 提交 apply/remove，`OperationClassModifying`（默认审批），ArtifactUploader 由 Controller/CX 注入；未实现任意 Web 命令。
- 真实隔离证据（本机普通用户目录、loopback 高位端口、portable nginx 1.27.5，不使用系统 nginx）：
  `NGINX_BIN=C:/temp/nginx-portable/nginx.exe go test ./apps/agent/ -run TestGatewayCapabilityRealNginxIsolated -count=1 -v` → PASS；覆盖实际 `nginx -t`/reload、HTTP 代理 200、相同 artifact 幂等、无效配置失败后旧文件恢复仍 200、remove 后不再 200。portable nginx 仅测试用，未入仓库；Linux 真实 Agent 注册/审批链仍待总控。

## 3. 供应商秘密借用组合

- `dns.SecretBorrower`/`notifications.SecretBorrower` 已是 callback-scoped；本包新增 Controller `providerSecretBorrower`：
  - `ProviderCredentialLookup` 由组合把 DS 的 account/channel `credential_ref` 映射为当前 CredentialID/owner/module/revision；
  - 每次调用生成含 operation 的唯一 OperationID，使用 `auth.CredentialService.UseForService`，provider I/O 在回调内完成，前后 gate 生效；
  - `NewDNSPublisherSecretBorrower`/`NewNotificationSecretBorrower` 供 server.go 注入；未暴露 HTTP/tool 解密。
- 现有 `server.go` credential gate 仅允许 AI，需总控 patch 扩展 dns-provider/smtp-password/qqbot-secret 的 module/scope；本轮未自行放宽。
- 本地 TLS Cloudflare/SMTP/QQ 证据保留；真实账号 BLOCKED。

## 4. ACME 清理持久化

- 新增 `internal/certificates/cleanup.go`：`CleanupJournal` 持久记录 order/domain/FQDN/provider record ID/value/state/attempts/next retry；`RecordFailure` 幂等、`List`、`Retry`（当前授权、有界 MaxAttempts/backoff）、`Cancel`、`RetryDue`（专用 cleanup-service 主体，需组合授权）；审计 `certificates.cleanup.*`；`DNSRecordCleaner` 必须校验 ID/FQDN/expected value 后才删除。
- Controller 新增 `NewPublicationCleanupCleaner`（通过 dns 服务精确匹配 ID/name/content 后删除，不删除无关记录）。
- DDL 候选 `certificates.CleanupMigrations()`（`ds/0062_acme_cleanup`），未登记历史/集中迁移；需总控接收并登记。
- 新增 `certificates.RegisterCleanupHTTP`（GET 列表、POST retry/cancel，主体必须由 Controller 认证回调注入），测试覆盖列表/重试/已解决取消冲突；中心路由注册仍为 patch。
- 测试：`go test ./internal/certificates/ -run TestCleanup -count=1` PASS（幂等记录、列表、重试成功/失败/预算耗尽、取消、拒绝、due 系统清理、审计）。真实清理记录 UNTESTED（无外部账号）；未验证证书不返回成功由 ACME client 现有路径保持。

## 5. 契约与黑盒文件

- 新增 `api/openapi/revisions/v09-r7/services-candidate.yaml`：发布目标/运行/逆序补偿、targetsPersisted、清理列表/重试/取消、稳定错误与冲突；`redocly lint` PASS；根 `redocly.yaml` 登记由总控 patch。
- `tests/integration/v09/services/file_transport_blackbox_test.go` 以公共 `transport`/生成类型验证 CX 当前只支持 skip/new-file，明确拒绝 overwrite/rename/createParents/非法读尺寸/路径逃逸；记录 CX 尚未支持 resume offset/rename reply 多 item 的失败用例，未改 CX 目录。
- CX 的 Linux 真实 file WSS 测试在本机 Windows SKIP；控制端/Agent file-root/显式映射仍未在本轮由 DS 验证，按任务单交 CX/总控。

## 门禁与阻塞

- `npm run check`（2026-11-01T16:04:17Z，代码 `3c3c4ef` / 文档 HEAD `3a29d79`）：policy/contracts/go/web/git 全部 PASS；`npm run check:security`：npm audit 0、govulncheck 无已知漏洞。
- `go test ./... -count=1`：全部 PASS（含 architecture/contracts/integration/POC）。
- `go test -race`：UNTESTED（CGO_ENABLED=0，无 gcc）。
- 阻塞：Controller `server.go` 发布桥/清理 journal/borrower 注入与迁移登记、Agent/Controller Nginx action 注册、文件端到端仍是共享 patch/实机；真实 DNS/证书/网关/Nginx Agent 生产执行、外部账号、awskr、双机、1C1G、arm64 未授权/未验收。
