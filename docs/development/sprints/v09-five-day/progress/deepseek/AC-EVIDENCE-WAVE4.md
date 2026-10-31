# DS Wave 4 AC / POC 证据盘点

- 任务：`V09-5D-DS` Wave 4；会话 `deepseek-04`；分支 `feature/v09-services-ds02`；基线 `0f9a75e`
- 代码提交：`bfe3b93`、`238ef81`、`fcc7390`、`22fe183`
- Wave 5 增量（会话 `deepseek-05`，基线 `3adf891`，代码提交 `d475c8a`/`f2f7b9e`/`506c977`/`25939d8`）见第 4 节。
- 口径：本表只登记 **DS（数据与核心业务服务）侧** 的代码、自动化测试与可复现命令。`docs/development/sprints/v09-five-day/ACCEPTANCE.json` 中的条目仍为 `NOT_ACCEPTED`；下表状态表示 DS 侧证据成熟度，不等于完整 v0.9 验收通过。
- 状态图例：`LOCAL-PASS` 本机真实 SQLite 文件库/HTTP 自动化通过；`PARTIAL` 另有隔离 VM/真实第三方只读证据但缺关键闭环；`BLOCKED` 需未授权的真实节点/供应商环境；`UNTESTED` 未执行。
- 统一命令（本分支，固定 Go 1.27.3 / Node 24.22.1）：
  - `go test ./...` → 全部 PASS（含 `tests/architecture`、`tests/contracts`、`tests/integration/v09/services`、`tests/poc/v09/services/...`）
  - `go test ./internal/monitoring ./internal/files ./internal/publishing ./tests/integration/v09/services -count=1` → PASS

## 1. 逐条映射

| AC | 名称 | DS 子项 | 代码（本包） | 自动化测试（代表性） | 本轮/既有证据 | 状态 | 未覆盖的外部边界 |
|---|---|---|---|---|---|---|---|
| AC-02 | 普通分组与自动分类 | DS-02 | `internal/nodes/{group_repository,service,http,migrations}.go`；`ClassifyVisible` 只读分类 | `internal/nodes/group_test.go`：`TestOrdinaryGroupReplaceMembersGuardsRevision`、`TestOrdinaryGroupReplaceMembersIsAtomicAcrossGoroutines`、`TestOrdinaryGroupFilterMembersDoesNotMutateMembership`、`TestOrdinaryAndServiceGroupsStaySeparate`；`visibility_test.go`：`TestVisibleFiltersToAllowedNodes`、`TestVisibleWithRealNodePolicyAuthorizerMixedGrants`；`classification_test.go`：`TestClassifySyntheticNodes` | `go test ./internal/nodes` PASS；自动分类只读、成员变更有 revision CAS、不可见节点被过滤 | LOCAL-PASS（后端） | 浏览器端到端由 GM；分类数据依赖真实 Agent 采样（未接真实 Agent 时只用受控 `ApplyAgentUpdate`） |
| AC-03 | 文件互传 | DS-05 | `internal/files/{http,service,upload,model,repository}.go`；新增 `PUT .../uploads/{uploadId}/content` 与 `complete` 的真实字节/SHA-256 校验 | `TestUploadResumeChecksumAndTransportRetry`、`TestUploadChecksumMismatchFails`、`TestHTTPUploadContentAndCompleteChecksum`（分块续传 + SHA）、`TestHTTPUploadContentRejections`（缺字节/摘要不符/超限）、`TestTransferAuthorizationIsSplitPerEndpoint`、`TestPathValidation`、`TestSymlinkFollowingRejected`、`TestWave4ServiceHTTPEndToEnd/file_upload_bytes_and_checksum` | 本机真实暂存文件 + 注入 transport fake；校验字节数、摘要、审计、409/413/422/400 负向 | LOCAL-PASS（控制器暂存闭环）；真实节点字节 BLOCKED | 两个真实 Linux 节点之间的字节复制、节点掉线/部分文件结果需要 CX `UploadTransport`/Agent；本包不建 SSH 旁路 |
| AC-04 | 服务组 | DS-02/03 | `internal/servicegroups/{service,repository,http,migrations}.go` | `TestServiceGroupPersistence`、`TestServiceGroupRevisionAndUnknownMembers`、`TestServiceGroupVisibility`、`TestDispatchPreviewRequiresOnlineHealthyAndGateway`、`TestTargetSnapshotLaterMembersDoNotEnterOldSnapshot`（tasks） | `go test ./internal/servicegroups ./internal/tasks` PASS；成员快照/revision 绑定，旧任务不因成员变更扩张 | LOCAL-PASS（后端） | 浏览器/跨模块引用（网关/证书/AI）由各 owner 与 GM 验证 |
| AC-05 | 批量任务 | DS-03 | `internal/tasks/**`（提交/审批/分批/金丝雀/熔断/TTL/取消/SSE） | `TestRootTaskCanaryAndApproval`、`TestReviseInvalidatesApproval`、`TestBatchFailureBlocksLaterBatches`、`TestUnknownDispatchIsTerminalAndNotRetried`、`TestOfflineWaitExpiresAfterTTL`、`TestConcurrentRunOnceDispatchesOnce`、`TestApprovalExpiryIsRecheckedBeforeLaterBatch`、`TestSSEStopsWhenReadAuthorizationIsRevoked` | `go test ./internal/tasks` PASS；未知结果不重跑、审批失效、TTL、撤权停止 SSE | LOCAL-PASS（fake Dispatcher）；真实 Agent/传输 BLOCKED | 真实 Agent 正常/分批/金丝雀与网络断开由 CX transport 端到端验证 |
| AC-06 | Nginx 网关 | DS-08 | `internal/gateway/{service,route,provision,http}.go` | `TestProvisionRejectsExistingSiteWithoutApproval`、`TestProvisionSubmitsOneTaskAndReconciles`、`TestApplyProvisionFailureRedactsAndDisables`、`TestRouteEnableRequiresChecker`、`TestRouteDomainAndTargetValidation`、`TestHTTPGatewayChecksAndDispatch` | 服务层/HTTP PASS；隔离 VM 曾安装 Nginx 1.26.3、HTTP 200、80 端口冲突 `EADDRINUSE`（2026-10-30） | PARTIAL | POC-05 受控接管需真实 Agent/Controller 任务闭环；旧站点保护与半安装失败未实测 → **POC-05 NOT VERIFIED** |
| AC-07 | DNS 管理 | DS-07 | `internal/dns/**`（credential_ref、Zone/记录、单默认原子切换） | `TestDNSAccountZoneRecordPersistAcrossRestart`、`TestDNSRecordCRUD`、`TestDNSDuplicateZoneAndRecordConflict`、`TestDNSSingleDefaultIsAtomic`、`TestDNSCredentialReferenceNeverExposed`、`TestDNSNilProviderFailsClosed`、`TestDNSHTTPAccountZoneAndRecordFlow` | 注入 provider fake 全 PASS；隔离 VM 对 Cloudflare `xihuani.com` 真实创建/读回/删除 TXT 并清理（2026-10-30，未入仓库凭据） | PARTIAL | 多账号隔离、过期 Token、跨 Zone 越权与供应商拒绝的完整矩阵未实测 |
| AC-08 | 公网证书与内部 TLS | DS-07 | `internal/certificates/**`、`internal/transport`（CX 证书签发） | `TestNoKeyMaterialInViewsOrAudit`、`TestRotateChangesKeyReferenceWithoutLeaking`、`TestRevokeUpdatesCertificateAndDistributions`、`TestExpectedRevisionMismatchFailsBeforeSideEffects`、`TestReadsAreRestrictedToDistributionNodes`、`TestHTTPCertificateDistributionLifecycle` | 注入 Issuer fake PASS；响应/审计无 keyRef/私钥；隔离 VM `certbot --dns-cloudflare --dry-run`（LE staging, DNS-01）成功并清理挑战记录 | PARTIAL | 真实节点独立身份、错误 CA/过期/撤销身份信任链、真实私钥生命周期（CX）未实测 |
| AC-09 | 监控告警与 QQ/SMTP | DS-06 | `internal/monitoring/**`、`internal/notifications/**` | `TestTransitionAlertExactlyOnce`、`TestMaintenanceSuppressesAlertsButKeepsStateAndChecks`、`TestDedupeWindowSuppressesSecondSend`、`TestQQFailureFallsBackToSMTPExactlyOnce`、`TestUnknownAndRateLimitedAreNotRetried`、`TestRetryHonorsBackoffAndLimit`、`TestNotificationSecretValueNeverStoredOrReturned` | 通知状态机/去重/备用/退避 PASS；隔离 VM 仅取得 QQ `access_token`，未发送消息 | PARTIAL | 真实 QQ 消息投递、SMTP 投递、供应商拒绝与重试风暴沙箱 UNTESTED |
| AC-15 | 服务发布向导 | DS-02/07/08 | `internal/publishing/**`：预检、三阶段计划、`RunDetail`/`RollbackDetail`、`LocalBridge`；HTTP `preview/publications/run/rollback` | `TestCreatePlanPersistsOrderedStages`、`TestRunOrdersStagesAndPersistsBeforeNext`、`TestRunFailureStopsAndRetryResumes`、`TestPersistenceRestartResumesWithoutRerunningSucceededStages`、`TestStageResultSummaryRedacted`、`TestHTTPPublicationRunAndRollback`、`TestHTTPPublicationRunFailureAndRetry`、`TestLocalBridgeFailsClosedWithoutFunctions`、`TestWave4ServiceHTTPEndToEnd/publication_execution_and_rollback` | 真实 SQLite WAL HTTP：三阶段顺序执行、网关阶段失败→`502 stage_failed`+真实阶段状态、重试跳过已成功阶段、逐阶段回滚补偿 | LOCAL-PASS（注入 publisher）；真实 DNS/证书/网关供应商 BLOCKED | 未授权 Zone、真实部分网关失败、DNS 传播延迟与缓存回滚边界；provider 生产适配器未接线时 run 保持 `502 publisher_unavailable` |
| AC-16 | 服务可用性监控 | DS-06 | `internal/monitoring/{service,http,ssrf,repository}.go`；新增 history/probe HTTP | `TestSSRFMatrix`（34 个逃逸/负向用例 + 4 个公网正例）、`TestSSRFAllowlistOverride`、`TestRunDueDispatchesProbeKindAndTimeout`、`TestProbeErrorRedacted`、`TestMaintenanceSuppressesAlertsButKeepsStateAndChecks`、`TestHTTPMonitorProbeAndHistory`、`TestHTTPMonitorProbeDeniedAndFailsClosed`、`TestWave4ServiceHTTPEndToEnd/monitoring_probe_history` | 真实 SQLite WAL HTTP：probe up→down、连续失败计数、持久历史 limit/排序、SSRF 拒绝、未注入 Prober `503 prober_unavailable`；隔离 VM 曾用真实 HTTP 探针跑 up→down→up | LOCAL-PASS/PARTIAL | 真实公网 HTTP/TCP/TLS 目标、DNS 解析后私网地址（需 Prober 复核）、探测器自身失联与站点宕机区分依赖生产 Prober |
| AC-17 | 安全批量执行 | DS-03 | `internal/tasks/**` | `TestTargetSnapshotPreservesOrderAndIsolatesSlices`、`TestRootTaskCanaryAndApproval`、`TestEvaluateBatchFailureBlocksLaterBatches`、`TestOfflineWaitExpiresAfterTTL`、`TestCancelBeforeAndAfterDispatch`、`TestDispatchDeniedAfterRevocationFailsTask` | `go test ./internal/tasks` PASS；冻结目标、批间熔断、取消≠回滚、撤权后派发失败 | LOCAL-PASS（fake Dispatcher） | 真实 Agent 分阶段执行与 dry-run 级别说明由 CX transport 端到端；AI 任意 root Shell 审批见 CX |
| AC-18 | 节点恢复与主控灾备 | DS-01/09（支持） | `internal/storage/{backup}.go`、`internal/recovery/**`（CX 主实现）、审计/迁移一致性 | `TestBackupRestoreAcrossDomains`（13→19 迁移全库快照/恢复/损坏拒绝）、`TestRestoreRejectsCorruptSnapshot`、`TestRecoveryPreservesRevocationsAndNeverReactivatesWork`、`TestQuarantineRejectsMisbindingStaleAndAmbiguousRecords` | 本地真实文件库备份/恢复/损坏拒绝 PASS；恢复不复活撤销/过期任务 | LOCAL-PASS（存储接缝） | 隔离离线恢复、真实 PKI 信任材料、旧主控隔离、双主控同时发令 → CX/POC-08 BLOCKED |
| AC-20 | 流量配额与到期提醒 | DS-02/06 | `internal/quota/**`、`internal/monitoring/http.go`（traffic-risks 由 composition 桥接） | `TestMissingUsageIsNotZero`、`TestNoAutoBlockOnExceeded`、`TestWarningAndExceededThresholds`、`TestWarningReminderDedupeAndEscalation`、`TestExpiryReminderDedupe`、`TestEstimateExpiryRequiresEnoughSamples`、`TestMonotonicAndCorrection`、`TestListPoliciesFiltersDeniedNodes`、`TestHTTPTrafficQuotaRoundTrip`、`TestWave3ServiceHTTPEndToEnd/quota` | 缺失不补零、估算与计费区分、阈值去重、超额只告警不断网、并发 PUT 原子、可见性过滤 | LOCAL-PASS（fake 通知） | 真实 Agent/供应商账单、周期/时区边界跨月实测与真实提醒投递 UNTESTED |

## 2. POC-05 / POC-07

### POC-05 · Nginx 网关安全接管 — **NOT VERIFIED（BLOCKED）**

- 已具备：`internal/gateway` 服务层（现有站点接管必须显式批准、`nginx -t`/端口/健康由 Checker 端口判定、启用需健康+准入、provision 只提交一个统一任务并回写分步结果）与 HTTP `/checks`、`/dispatch`、provision 端点；单测 `TestProvisionRejectsExistingSiteWithoutApproval`、`TestProvisionSubmitsOneTaskAndReconciles`、`TestRouteEnableRequiresChecker` 等 PASS。
- 隔离 VM 历史证据（2026-10-30）：Nginx 1.26.3 安装并 active，`http://127.0.0.1/` 返回 200；80 端口二次绑定得到 `EADDRINUSE`。
- 缺口：受控接管/安装需要 Controller→Agent 任务通道执行真实主机操作；本轮未接线，旧站点不被破坏、半安装失败、配置冲突、健康失败、维护停用等逐项场景 **未执行**。不得以服务层 fake 测试代替 POC-05。

### POC-07 · 主控性能与资源 — **PARTIAL（非 1C1G 标准主机）**

- 采集实现：`tests/poc/v09/services`（10/30/100 模拟代理心跳写入、SSE、任务日志；报告 `reports/_review/v09-services/poc07-report.json`，忽略目录）。
- 历史结果（见 `AC-MAP.md`）：
  - Windows 开发机：10/30/100 代理 9370.9 / 5680.9 / 5298.8 writes/s；DB ≤980KB；checkpoint 后 WAL 0；heap ≤2.6MB。
  - 隔离 Debian 13 x86_64（2 vCPU/464MB RAM+swap，`taskset -c 0 GOMAXPROCS=1`）：8535.3 / 7615.4 / 7487.6 writes/s；DB ≤1016KB；WAL 0；heap ≤2.5MB；SSE 3 事件 <1ms。
- 缺口：不是标准化 1 核 1GB 主机验收；P95、长时间（>30min）稳定性、内存上限与磁盘增长曲线未采集。状态保持 PARTIAL，不宣称 POC-07 PASS。

## 3. 跨条目剩余风险

1. **无迁移新增**：为保持 Wave 2 集成测试的 19 项迁移断言，本轮未新增迁移；monitor 的 `sourceNodeId` 未持久化，因此 AC-16 的“按关联节点可见性”无法在服务层实现，history/probe 仅强制 `monitoring.read`/`monitoring.write`。需契约/迁移决策后补。
2. **端口未生产实现**：文件 `UploadTransport`、监控 `Prober`、发布三阶段 publisher 均需组合层注入；未注入时相关端点失败关闭（503/502），不伪造成功。Wave 5 已提供 `monitoring.NewNetProber` 与 `files.NewLocalStagingTransport` 两个**本地可用实现**供总控回退接线（见第 4 节与 `HANDOFF-WAVE5.md`），但生产 Agent 通道与 publisher 仍属 CX/后续任务。
3. **provider 错误脱敏**：publishing 只回稳定错误码；files/monitoring 同样不返回原始上游文本。
4. **外部环境清单**：真实 Linux 双架构/1C1G、Cloudflare 多账号、ACME 生产、QQ/SMTP 实际投递、Nginx 受控接管、真实 Agent WSS/mTLS 与文件字节通道 → 未授权/未执行，保持 BLOCKED/UNTESTED。

## 4. Wave 5 增量证据（DS-W5-01/02，会话 deepseek-05）

- 分支 `feature/v09-services-ds02`；起始基线 `3adf891f3155b6452f06f25df53fd20f553742ae`
- 代码提交：`d475c8a`（`feat(monitoring): add ssrf-revalidating net prober`）、`f2f7b9e`（`feat(files): add local staging upload and download transport`）、`506c977`（`test(services): exercise real prober and local staging transport over http`）、`25939d8`（`fix(monitoring): keep probe failures classified when context is intact`）
- 未新增迁移；未修改 `apps/**`、公共契约/Proto/生成代码/根锁文件；HTTP 路由与 `RegisterHTTP` 签名未变。

| AC | Wave 5 子项 | 代码（本包） | 自动化测试（代表性） | 本轮证据 | 状态 | 未覆盖的外部边界 |
|---|---|---|---|---|---|---|
| AC-03 | 文件互传（受控本地暂存通道） | `internal/files/local_transport.go`：`NewLocalStagingTransport(rootDir)` 实现 `UploadTransport`+`DownloadTransport`；原子写入、字节数/SHA-256 核对、符号链接与路径逃逸拒绝 | `TestLocalStagingPushPullRoundTrip`、`TestLocalStagingRejectsInvalidInputs`、`TestLocalStagingRejectsSymlinkedDirectories`、`TestLocalStagingRejectsSymlinkedTargetFile`、`TestLocalStagingPullRejections`、`TestLocalStagingHonorsCancellation`、`TestNewLocalStagingTransportRejectsBadRoot`、`TestLocalStagingWindowsReservedNames`；`TestWave5RealProberAndLocalStagingTransport/real_local_staging_upload_and_download` | 真实 `t.TempDir()` 磁盘：HTTP 创建上传 → PUT 字节 → complete（真实 SHA-256）→ 磁盘文件一致 → `Download` 读回一致；逃逸/符号链接/大小/摘要负向全部拒绝 | LOCAL-PASS（受控本地目录闭环）；真实节点字节 BLOCKED | 两个真实 Linux 节点间复制、节点掉线/部分失败、节点端权限与磁盘配额仍依赖 CX Agent 通道；本实现仅作本地闭环，不得冒充生产 Agent |
| AC-16 | 服务可用性监控（真实探测器） | `internal/monitoring/prober.go`：`NewNetProber` 实现 HTTP/TCP/TLS；自定义 `DialContext` 对每个解析 IP 二次 SSRF 校验；代理禁用、重定向逐跳复检；稳定错误码 + 固定摘要 | `TestNetProberHTTPHealthyUsesRealTransport`、`TestNetProberHTTPStatusFailureDoesNotEchoBody`、`TestNetProberRejectsDNSRebindingToLoopback`、`TestNetProberRejectsMixedPublicAndPrivateAnswers`、`TestNetProberLoopbackAllowedOnlyForExplicitHost`、`TestNetProberRejectsMetadataAndLocalhostNames`、`TestNetProberTCPProbe`、`TestNetProberTLSProbeVerifiesCertificate`、`TestNetProberHTTPRespectsTimeout`、`TestNetProberDNSFailureIsClassified`；`TestWave5RealProberAndLocalStagingTransport/real_http_probe_persists_history` | 真实 `httptest.Server` HTTP 探测 → `/probe` 200/up/latency/statusCode 落库 → `/history` 读回；404 目标 → down + `http_status_404` 且响应体不回显；TLS 正向使用注入 RootCAs，未信任与明文端口均失败关闭 | LOCAL-PASS（真实本地网络 + 注入解析器）；公网目标 UNTESTED | 真实公网 HTTP/TCP/TLS、IPv6-only、证书异常主机、真实 DNS TTL 切换（DNS Rebinding 仅用注入 `HostResolver` 验证解析后拦截）；健康口径固定 2xx/3xx |

### 4.1 统一验证（本分支）

- `go test ./internal/monitoring/... ./internal/files/... ./tests/integration/v09/services/... -count=1` → PASS（monitoring 0.613s / files 0.402s / services 0.669s）。
- `TestWave5RealProberAndLocalStagingTransport` → PASS（`real_http_probe_persists_history`、`real_local_staging_upload_and_download`）。
- `npm run check` → PASS（policy、contracts、go、web、git；新增测试在 `reports/go-tests.log` 中执行）。
- `npm run check:security` → PASS（`npm audit` 0 漏洞；`govulncheck ./...` 无漏洞）。
- 接线参数与行为变化见 [HANDOFF-WAVE5.md](HANDOFF-WAVE5.md)；总控未接线前 `server.go` 仍保持原有失败关闭语义，本包不修改 `apps/controller`。
