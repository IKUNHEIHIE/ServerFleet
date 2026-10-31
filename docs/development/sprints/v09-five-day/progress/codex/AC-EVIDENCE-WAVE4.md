# V09 Wave 4 · CX 平台安全域 AC 与 PoC 证据盘点

- **任务**：`V09-5D-CX` Wave 4（`CX-W4-01` ~ `CX-W4-03`）
- **唯一会话**：`codex-04`（接续 `V09-5D-CX` Wave 3 `codex-03`）
- **分支 / Worktree**：`feature/v09-platform-cx02` / `.worktrees/feature--v09-platform-cx02`
- **起始基线**：`0f9a75e416a82a9589a29d4d9cf8a52bf0266066`
- **盘点时间**：2026-10-31（Asia/Shanghai）
- **结论口径**：以下 `AC-*` 仍全部为 `NOT_ACCEPTED`，PoC 仅登记本仓库可复验的自动化/隔离实验证据；`G2`、`D01–D07`、`P1-C10` 未冻结，未访问云/VPS/生产或真实第三方凭据。本文件不把任何局部测试写成完整产品验收。

---

## 0. Wave 5 增量（2026-10-31，codex-05）

- **提交**：`0cd0ecd`（`feat(agent): add controlled single-shot WSS+mTLS connect-once mode`）、`3e9dd49`（`test(platform): drive real controller and agent CLI full WSS task loop`）；文档提交见本分支最新提交。基线 `3adf891f3155b6452f06f25df53fd20f553742ae`，未推送、未合并 `main`。
- **新增实现**：
  - `apps/agent/runtime/runtime.go`：`-connect-once` / `-node-name` / `-wait-task-ms`，严格 https/wss origin、互斥模式与失败关闭；无参仍为原 M0-P1 骨架。
  - `apps/agent/connect.go`：固定根证书 → 已签发身份 → TLS1.3 mTLS → `/api/v1/agent/link` WSS → Hello/Heartbeat/Metrics → 至多一次受控 `tasks.command` 自检动作 → 干净关闭；无重试、无任意 Shell。
  - `apps/agent/metrics.go`：真实 Linux `/proc/stat` + `/proc/meminfo` 采样（100ms 差分，CPU/内存百分比，非 Linux 失败关闭）。
  - `apps/agent/session.go`：可选 `InitialSample`/`Established`/`ExecutionFinished` 一次性钩子；`internal/transport/websocket.go`：有界 `Ping`（并发读循环协作）与 hijack 后清除 HTTP deadline。
  - `apps/agent/executor.go`：`ScopeRevision=0` 作为普通 Web/工作流任务的合法“非 AI 作用域”值；normal/空 plugin/schema/参数约束不变，Controller 仍是唯一授权源。
- **新增测试**：
  - `TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`（`tests/integration/v09/platform/controller_cli_pki_test.go`）：真实编译 controller 与 agent 二进制、真实 SQLite WAL/本地 PKI/HTTPS/mTLS WSS、token→pending→批准→issued、`-status`、`nodes.read/tasks.command` 授权、在线心跳/指标、`tasks.command` 任务完成、`tasks.submit`/`enrollment.approved`/`nodes.agent_update` 审计、断开后离线。Windows 按 Linux-only 入网/Hello 策略明确 SKIP；WSL Ubuntu 24.04 amd64/Go 1.27.3 实跑 PASS（单次 4.25s，`-count=3` 11.507s；同源 platform+agent+transport 包 7.618s 全 PASS）。
  - `TestControlledCommandActionIsBounded`、`TestAdmitControlledOfferRejectsForeignShapes`、`TestParseAgentLinkEndpointRejectsUnsafeOrigins`、`TestAgentConnectOnceFailsClosedWithoutIssuedIdentity`、`TestParseProcCPUStatAggregateLine`、`TestParseProcMemInfo`、`TestSampleAgentMetricsFailsClosedOffLinux`、`TestAgentSessionInitialHeartbeatMetricsAndEstablishedHook`、`TestWebSocketPingPongProbe`、`TestWebSocketPingIsBoundedWithoutReader`、`TestAgentExecutorAcceptsUnscopedStandardOffer`。
- **门禁**：Windows `go test ./apps/... ./internal/transport/... ./internal/workflows/... ./tests/integration/v09/platform/... -count=1` 全 ok（Linux-only 用例 SKIP）；`npm run check` PASS（`reports/check-all.json`，`2026-10-31T14:59:35.597Z`；Web 316/316）；`npm run check:security` PASS（`2026-10-31T14:59:58.789Z`，0 漏洞 / govulncheck 无发现）。
- **边界**：仍是 Linux-only、非物理 VPS/arm64/1C1G、单次连接无守护/重连/轮换；本地 CA 同时签发管理端证书仅为自包含测试布局，不代表 PKI-OPS/生产分离方案；受控执行不是任意命令或 root Shell，不代表 PRIV/沙箱准入。

---

## 1. Wave 4 新增实现与验证

### 1.1 CX-W4-01 工作流 HTTP 管理与受控执行

| 内容 | 文件 |
|---|---|
| 定义读取/列表/运行历史 | `internal/workflows/read.go` |
| HTTP 管理面（列表/创建/详情/启停更新/触发） | `internal/workflows/http.go` |
| 引擎与只读输入校验单测 | `internal/workflows/http_test.go` |
| Controller 组合（SQL 后端、任务提交、审计、权限键、路由） | `apps/controller/server/workflows.go`、`apps/controller/server/server.go` |
| 真实 HTTPS + SQLite WAL 集成测试 | `tests/integration/v09/platform/controller_workflows_test.go` |

**安全语义**

- 工作流定义按所有者隔离：`GET/PATCH` 他主体定义返回 `404`；列表只返回当前主体自己的定义。
- 权限键：`workflows.read`（`ReadOnly`，reader 角色可见）与 `workflows.write`（operator/administrator）；只读主体创建、修改、触发均 `403`。
- 触发 `POST /api/v1/workflows/{workflowId}/runs` 只记录冻结计划并调用 `Engine.Advance`；每个步骤统一经组合 `SubmitTask` → **唯一 `tasks.Service.Submit`**，按动作类别进入 `awaiting_approval`（`host.patch`/`host.root`）或标准队列（`tasks.command`）；`HumanApproval` 可强制标准动作进入审批。
- 任务审批仍由 `tasks` 域治理：提交者自批 `409`（集成测试实测）；任务幂等键 `workflow-<sha256>` 保证重复触发不产生重复副作用。
- 审计动作登记：`workflows.save`、`workflows.trigger`、`workflows.dispatch`、`workflows.task.submit`、`workflows.invalidated`、`workflows.paused_unknown`、`workflows.failed`、`workflows.complete`；集成测试查询 `/api/v1/audit/events` 验证落盘。
- 无本地执行旁路：引擎不持有 Dispatcher，也没有队列/脚本求值；未匹配事件返回 `matched=false` 且不建任务。

**命令与输出（本机 Windows，2026-10-31）**

```
$ go test ./internal/workflows -run 'TestWorkflow' -count=1 -v
--- PASS: TestWorkflowSequentialTasksAtMostOnceAndUnknownPauses (0.00s)
--- PASS: TestWorkflowSuccessDisableAndCurrentOwnerDenial (0.00s)
--- PASS: TestWorkflowCorruptPersistenceAndClockFailClosed (0.00s)
--- PASS: TestWorkflowHTTPCreateReadPatchAndOwnerRules (0.00s)
--- PASS: TestWorkflowHTTPTriggerEntersTaskServiceAtMostOnce (0.00s)
--- PASS: TestWorkflowHTTPRejectsMalformedAndForeignRequests (0.00s)
--- PASS: TestWorkflowReadOwnerIsolation (0.00s)
--- PASS: TestWorkflowFrozenInputsStableIdempotencyAndConditions (0.00s)
--- PASS: TestWorkflowUnknownActionWildcardAndDisabledDenied (0.00s)
ok  serverfleet.local/serverfleet/internal/workflows  2.500s

$ go test ./tests/integration/v09/platform -run 'TestWave4' -count=1 -v
--- PASS: TestWave4ControllerCLILocalPKIEnrollment (0.31s)
--- PASS: TestWave4WorkflowHTTPManagementApprovalAndAudit (0.48s)
ok  serverfleet.local/serverfleet/tests/integration/v09/platform  1.184s
```

**剩余风险 / 未覆盖**

1. 工作流状态写入与任务创建是两个独立持久事务（`tasks.Service.Submit` 不暴露同事务提交端口，且禁止 CX 修改 DS 任务包）。失败窗口由任务幂等键 + 触发/推进重放收敛；若状态写入失败，任务仍可能经唯一任务管线执行且其提交已审计。该窗口已在实现注释与本节如实登记，未隐藏为原子事务。
2. 多步骤推进仅由显式 `POST /runs`（同一 `eventId`）重放推进；未实现后台调度器，未接 `schedule`/`monitor.alert`/`task.completed` 的可信事件生产端（这些事件当前只能由受控触发方显式提供）。
3. 工作流 ID 由服务端生成；未提供跨主体共享/管理员托管他人工作流的语义。
4. `OpenAPI` 候选未定义工作流端点，本实现为候选接缝，未冻结契约。

### 1.2 CX-W4-02 Controller / Agent CLI 本地自包含连接

| 内容 | 文件 |
|---|---|
| 本地 Agent CA 生成/加载（ECDSA P-256、0600、部分状态失败关闭） | `apps/controller/localpki/localpki.go` |
| CLI 参数、组合、TLS（TLS1.3 + 可选客户端证书） | `apps/controller/bootstrap/bootstrap.go`、`apps/controller/main.go` |
| Agent CLI 编排（无参骨架 / `-status` / 入网一次尝试） | `apps/agent/runtime/runtime.go`、`apps/agent/main.go` |
| 身份状态只读投影 | `apps/agent/identity_store.go`（`Status`） |
| 测试 | `apps/controller/localpki/localpki_test.go`、`apps/controller/bootstrap/bootstrap_test.go`、`apps/agent/runtime/runtime_test.go`、`apps/agent/main_test.go`、`tests/integration/v09/platform/controller_cli_pki_test.go` |

**行为**

- 无 `-db`：Controller 保持 M0-P1 空壳（`health.Handler()`，HTTP 非 TLS），不初始化 PKI。
- 有 `-db`：默认在数据库旁 `<db目录>/pki` 初始化/加载本地 CA（`local-agent-ca.pem` + `local-agent-ca-key.pem`），自动填充 `server.Config.EnrollmentIssuer/AgentAuthority/AgentServerName`，挂载 `/api/v1/enrollment-tokens`、`/api/v1/enrollments`、`/api/v1/agent/enroll`、`/api/v1/agent/link`，不再返回 `503 AGENT_TRANSPORT_UNAVAILABLE`。
- 新增参数：`-agent-pki-dir`、`-agent-authority`、`-agent-server-name`；默认 authority 从 `-listen` 推导（回环 IP → `localhost:<port>`）。
- 管理 TLS：TLS 1.3，`VerifyClientCertIfGiven` + `ClientCAs=本地 Agent CA`，浏览器无客户端证书不受影响，Agent mTLS 链路可提交证书。
- Agent 无参仍打印原骨架行并退出 0；`-status -state-dir <abs> -root-pin <64hex>` 输出本地身份状态；`-enroll-token ... -controller-url https://... -root-cert ...` 执行**一次**显式入网（pending/issued 均可见，denied/失败退出 1，无自动重试/替换）。
- 本地 CA 私钥不导出、不进入 HTTP；文件 `0600`（Linux 加载时校验权限）；CA 30 天、Agent 证书 12 小时；不自动轮换，部分/损坏状态拒绝自动重建。

**命令与输出（本机 Windows，2026-10-31）**

```
$ go test ./apps/agent/... ./apps/controller/... -count=1
ok  serverfleet.local/serverfleet/apps/agent  3.121s
ok  serverfleet.local/serverfleet/apps/agent/runtime  2.052s
ok  serverfleet.local/serverfleet/apps/controller/bootstrap  0.462s
ok  serverfleet.local/serverfleet/apps/controller/health  2.323s
ok  serverfleet.local/serverfleet/apps/controller/localpki  2.990s

$ go test ./tests/integration/v09/platform -run TestWave4ControllerCLILocalPKIEnrollment -count=1 -v
--- PASS: TestWave4ControllerCLILocalPKIEnrollment (0.31s)
```

集成测试覆盖：CLI 默认组合挂载而不是 503；匿名管理面 403；管理员 bootstrap/login/CSRF；一次性 token → Agent CSR pending → 管理员批准 → 同请求 issued（`trustGeneration=1`）；管理 TLS 配置；重启后本地根指纹不变。

**剩余风险 / 未覆盖**

1. 本地 PKI 是开发自包含便利，不是 PKI-OPS 决策、在线 Root CA 授权或生产轮换/吊销分发方案；CA 私钥明文落盘（0600）仅限本机开发。
2. 未运行真实 Linux Agent 安装包与自启动、arm64/1C1G、真实节点心跳/断线重连验收；`agent Hello` 仍只接受 `platform=linux`。
3. Agent CLI 入网走既有 HTTPS 固定根客户端；本机测试验证了 Controller 侧与 CLI 状态/参数编排，未在测试内以子进程方式启动 Agent 二进制完成真实网络轮询。
4. 管理 TLS 证书仍由部署方提供（`-tls-cert/-tls-key`），未自动签发服务器证书。

### 1.3 CX-W4-03 全量回归

```
$ go test ./... -count=1 -timeout 20m
（所有包 ok，0 FAIL；含 tests/architecture、tests/contracts、tests/integration/v09/*、tests/poc/v09/*）

$ npm run check
policy/contracts/go/web/git = PASS（reports/check-all.json，2026-10-31T13:24:41.313Z；Web 36/36 文件、306/306 测试）

$ npm run check:security
PASS（reports/check-security.json，2026-10-31T13:25:20.338Z；npm audit 0 漏洞，govulncheck: No vulnerabilities found.）
```

首次 `npm run check` 在 Web `node-add-page.test.tsx` 出现一次高负载异步渲染竞态失败；单文件复跑与第二次完整门禁均 PASS，已如实登记于 `STATUS.md`。

---

## 2. AC 逐项映射（CX 负责后端部分）

> 状态列含义：`IMPLEMENTED_PARTIAL` = 本仓库有实现与自动化负向测试，但完整产品验收仍未完成；全部条目整体仍为 `NOT_ACCEPTED`。

| AC | 名称 | 实现文件 | 自动化测试 | 状态 | 真实边界 |
|---|---|---|---|---|---|
| AC-01 | 节点接入、身份与生命周期 | `internal/transport/{enrollment,enrollment_http,enrollment_management_http,peer_identity,controller_link,agent_link_http,websocket,connection_guard}.go`；`apps/controller/server/{enrollment,agent_links}.go`；`apps/controller/{localpki,bootstrap,main.go}`；`apps/agent/{identity_store,enrollment_client,session,connect,metrics}.go` | `TestEnrollmentOneTimeCSRApprovalReinstallAndLiveRevocation`、`TestEnrollmentExpiryPinSignatureAndAdminGate`、`TestPeerRealLocalTLSAndLiveRevocation`、`TestPeerWrongCANameAndMissingClientCertificate`、`TestWave3ControllerAgentEnrollmentWSSAndTaskExecution`、`TestWave4ControllerCLILocalPKIEnrollment`、`TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`、`TestEnsureCreatesAndReloadsStableLocalCA`、`TestEnsureRejectsPartialAndCorruptState`、`TestComposeInitializesLocalAgentPKI`、`TestAgentStatusReportsFreshDirectoryAsNotEnrolled` | IMPLEMENTED_PARTIAL | 真实 Linux 安装/自启动、物理节点心跳/重连/轮换、arm64/1C1G、正式 PKI profile 未验收；Wave 5 双进程闭环在 Windows 按 Linux-only 策略 SKIP、WSL Ubuntu 实跑 PASS |
| AC-08 | 公网证书与内部 TLS | `internal/transport/certificate_issuer.go`；`apps/controller/localpki/localpki.go`；内部证书分发为 DS `internal/certificates`（本会话不修改） | `TestPeerRealLocalTLSAndLiveRevocation`、`TestPeerWrongCANameAndMissingClientCertificate`、`TestWave4ControllerCLILocalPKIEnrollment`；DS 侧 `TestHTTPCertificateDistributionLifecycle`、`TestRotateChangesKeyReferenceWithoutLeaking`、`TestRevokeUpdatesCertificateAndDistributions`、`TestNoKeyMaterialInViewsOrAudit` | IMPLEMENTED_PARTIAL | ACME/公网供应商、真实节点证书分发与吊销传播、PKI-OPS 决策未执行 |
| AC-10 | AI 会话资源授权 | `internal/ai/{sessions,scope,context,management_http,execution_ports}.go`；`apps/controller/server/ai.go` | `TestPersistentAIScopePerActionGroupSnapshotAndRevocation`、`TestAIScopePolicyExpiryRevisionFailureAndRootApproval`、`TestToolScopeZeroSnapshotRemovalAndCurrentRBAC`、`TestWave3AIManagementScopeApprovalsAndRootShellGate` | IMPLEMENTED_PARTIAL | 真实外部模型/BYOK 未接；插件/批量间接越权仅单测层 |
| AC-11 | AI 三级审批 | `internal/ai/{approvals,tool_policy,management_http,execution_ports}.go` | `TestToolRootRequiresRealAdminApprovalInFullAccess`、`TestToolFullExpiryPluginDisableAndSecretExport`、`TestPersistentAIScope...`、`TestWave3AIManagementScopeApprovalsAndRootShellGate` | IMPLEMENTED_PARTIAL | 真实两名管理员运维、外部模型工具链、root 真实执行未验收 |
| AC-12 | 插件平台 | `internal/plugins/{lifecycle,lifecycle_runtime,package_verify,vault,history,repository,http,xray_http,operations}.go` | `TestPluginVerifiedInstallDefaultDisabledLifecycleAndOldLeaseDenial`、`TestPluginMissingRuntimeTamperedArtifactConsentAndUnknownStop`、`TestPluginUpgradeRequiresNewerVersionAndExactNewConsent`、`TestPackageSignatureSourceUIAndDisable`、`TestHistory*`、`TestWave3PluginCenterRoutesAndXrayGate` | IMPLEMENTED_PARTIAL | WASM/Host ABI 与资源隔离未准入；无生产插件源；运行时为显式 double |
| AC-13 | 更新中心 | `internal/updates/{verify,staging,install,http}.go` | `TestReleaseSignaturePlatformDigestRollbackAndCancel`、`TestStagingRealFilesRecheckOneShotAndCleanup`、`TestInstallRealFilesReceiptReopenAndPermanentExecutionFence`、`TestInstallKnownUnhealthyRollsBackOnlyExactBackup`、`TestInstallUnknownStagesPersistAndDoNotRetryAfterReopen`、`TestWave3UpdatesAndRecoveryRoutes` | IMPLEMENTED_PARTIAL | 无正式 release feed/安装器；真实 Linux 服务重启/回退、不可逆迁移不称回滚未执行 |
| AC-14 | 工作流、用户权限与审计 | **新增** `internal/workflows/{engine,plan,sql,read,http}.go`；`apps/controller/server/{server.go,workflows.go}`；`internal/auth/*`、`internal/audit/*` | `TestWorkflowHTTPCreateReadPatchAndOwnerRules`、`TestWorkflowHTTPTriggerEntersTaskServiceAtMostOnce`、`TestWorkflowHTTPRejectsMalformedAndForeignRequests`、`TestWorkflowReadOwnerIsolation`、`TestWave4WorkflowHTTPManagementApprovalAndAudit`、`TestIdentityAuthenticationRevocationAndOwnership`、`TestBrowserOriginCredentialAndCookieBoundaries` | IMPLEMENTED_PARTIAL | 工作流状态/任务提交非单事务；外部队列事件生产端缺失；真实浏览器页面由 GM 负责 |
| AC-18 | 节点恢复与主控灾备 | `internal/recovery/{quarantine,reconcile,http}.go`；`internal/updates`（重启/阶段持久化） | `TestRecoveryPreservesRevocationsAndNeverReactivatesWork`、`TestQuarantineDurableAndLiveVerifierRequired`、`TestQuarantineRejectsMisbindingStaleAndAmbiguousRecords`、`TestQuarantineCancellationPanicAndPartialReleaseDeny`、`TestWave3UpdatesAndRecoveryRoutes` | IMPLEMENTED_PARTIAL | 真实离线恢复、恢复私钥材料、旧主控隔离/外部 fencing 未提供，不能声称可恢复 |
| AC-19 | 安全凭据 | `internal/auth/{credential_cipher,credentials,credentials_http,bearer}.go` | `TestCredentialEncryptedLifecycleAndIdempotence`、`TestCredentialFailsClosedAndNeverRetriesUnknown`、`TestCredentialProviderCompletionAuditFailureAndActionBinding`、`TestBrowserOriginCredentialAndCookieBoundaries` | IMPLEMENTED_PARTIAL | 真实第三方供应商凭据注入、密钥轮换作业/正式加密决策未验收；GET/日志/审计不返回秘密已由测试覆盖 |
| AC-21 | Xray 安装 | `plugins/xray-manager`（可选包，核心零静态导入）；`internal/plugins/xray_http.go` 门控 | `TestPlugin...`、`TestTemplateBothVersionsAndNoUncontrolledSecretRead`、`TestWave3PluginCenterRoutesAndXrayGate` | IMPLEMENTED_PARTIAL | 官方外部发布物安全扫描 FAIL，不得安装/发布；Debian/Ubuntu amd64/arm64 真实安装/接管/端口冲突未执行 |
| AC-22 | Xray 配置 | `plugins/xray-manager/{config,custom}.go` | `TestTemplateBothVersionsAndNoUncontrolledSecretRead`、`TestTemplateRejectsUnsafeOrStaleFacts`、`TestCustomReviewPreservesBytesAndReturnsNoConfigValues`、`TestCustomReviewRejectsPublicOrIndirectManagementAPI`、`TestCustomReviewRejectsAmbiguityBoundsAndFiles`；`TestPinnedXrayActualConfigTestAndLoopbackStart` 在默认门禁因缺少显式 `SF_XRAY_LOCAL_TEST_BINARY` 而 SKIP | IMPLEMENTED_PARTIAL | 目标 Xray 版本预检、REALITY Vision 真实客户端、失败保留旧配置未在允许环境验收；密钥不出响应由测试覆盖 |
| AC-23 | Xray 用户与受控导出 | `plugins/xray-manager/{materials,users,export}.go` | `TestManagedUsersKeepImmutableAttributionAndSeparateDesiredFromApplied`、`TestManagedUserCASReceiptsAuditRollbackAndUnknownApplication`、`TestManagedUsers*`、`TestClientExportURITLSRealityAndJSONOnlyDeliverAfterCredentialFence`、`TestClientExportRevocationBadLoanAndDeliveryFailureNeverProduceFalseSuccess` | IMPLEMENTED_PARTIAL | 真实 Xray 持久身份/统计归属、QR/导出交付、中央材料 Host 未部署；历史原生证据见 `LINUX-XRAY-MATERIALS-EVIDENCE.json`、`LINUX-XRAY-USERS-EVIDENCE.json`、`LINUX-XRAY-EXPORT-EVIDENCE.json` |
| AC-24 | Xray 流量 | `plugins/xray-manager/{stats,traffic}.go` | `TestStatsExactInt64MissingIsUnavailableAndDimensionsDoNotDoubleCount`、`TestStatsRejectsAmbiguousOverflowAndInvalidBindings`、`TestTrafficBaselineReplayRestartMissingAndReset`；`TestPinnedXrayActualVisionTrafficStatsAndProcessReset` 默认 SKIP | IMPLEMENTED_PARTIAL | 真实 StatsService 持久采集/重启缺口/主机配额区分未部署；禁止把代理流量称为 VPS 计费 |

---

## 3. PoC 逐项映射

| PoC | 范围 | 本仓库证据 | 状态 | 未验证边界 |
|---|---|---|---|---|
| POC-01 | 插件生命周期 | `internal/plugins/lifecycle_test.go`、`lifecycle_concurrency_test.go`、`package_verify_test.go`、`history_test.go`；`TestWave3PluginCenterRoutesAndXrayGate` | PARTIAL | 真实 Host/WASM 生命周期、热更新失败回退、1C1G 资源预算未执行；禁用后后端拒绝已单测 |
| POC-02 | WASM 资源/隔离 | 隔离 PoC（`tests/poc/v09/platform/wasm_test.go.txt`，wazero v1.12.0）：`TestWASMMemoryAndUnregisteredHostDenied`、`TestWASMInfiniteLoopTerminatesAndRuntimeSurvives`；历史输出见 `EVIDENCE.json` | PARTIAL | 仅隔离合成模块；1C1G RSS/CPU 峰值、完整 Host ABI、持久生命周期未验证；不申请生产运行时 |
| POC-03 | Xray 配置 | `plugins/xray-manager/config_test.go`、`custom_test.go`；pinned 二进制测试默认 SKIP；安全扫描结论见 `docs/development/sprints/v09-five-day/progress/codex/SHARED-CHANGES.md` 与 Xray 安全记录 | BLOCKED（发布物）/PARTIAL（规则层） | 26.6.1 与 26.10.29 外部发布物安全 FAIL，禁止安装；Debian/Ubuntu 双架构真实配置测试未执行 |
| POC-04 | Xray 流量 | `stats_test.go`、`traffic_test.go`；历史 Windows 26.6.1 StatsService/Vision 实验记录于 `SHARED-CHANGES.md` | PARTIAL | REST 持久遥测、真实重启/断连缺口、用户/入站归属到页面未验收；不混用主机配额 |
| POC-06 | AI 鉴权 | `internal/ai` 测试：零授权、组快照、撤权、过期/重放、`full_access` 不豁免 root 真人管理员审批；`TestWave3AIManagementScopeApprovalsAndRootShellGate` | PASS（模拟模型层） | 真实外部模型/BYOK、真实工具执行与生产内容过滤 UNTESTED |
| POC-08 | 备份恢复 | `internal/storage` `VerifyFile`、`internal/recovery` 隔离/恢复测试、`TestWave3UpdatesAndRecoveryRoutes` | PARTIAL | 恢复包离线解密材料、Root 信任连续、旧主控隔离/撤销任务不复活未在真实灾备环境执行；不得声称可恢复 |

---

## 4. 明确未交付 / 停止条件

1. **全部 AC-01~24 仍 `NOT_ACCEPTED`**；本文件只登记 CX 负责的后端实现与自动化证据，不代表 GM 浏览器证据或 DS 业务域完成。
2. 未访问云/VPS/生产、未使用真实凭据、未安装真实 Xray/Agent/插件、未连接外部模型或公网证书供应商。
3. 未修改 DS 业务包、`apps/web/`、`tests/browser/`、根公共文件、生成代码、`api/openapi/*.yaml` 与迁移台账；候选契约保持字节不变。
4. 本 Wave 的 G2、D01–D07、P1-C10、WASM/Host、Eino 生产框架、PKI-OPS、UPDATE/恢复正式决策均未冻结；上述实现不能当作冻结证据。
5. 未合并 `main`、未推送远程、未启动其他 Agent。
