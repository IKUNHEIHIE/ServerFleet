# V09-R6-DS-02 · Wave6 连续实施状态（deepseek-06）

- 任务：`V09-R6-DS-02`；会话 `deepseek-06`；分支 `feature/v09-r6-services-ds06`；同步基线 `379d5e0e4818844eb44d8ac13010f251d215d8c0`；审查 `codex-review-02`。
- 允许目录沿用 DS 整包；未改 `apps/**`、Proto/gen/API、根依赖锁、共享迁移登记、CI 或原始资料；DDL/Controller 入口只交具体补丁（见 `SHARED-CHANGES.md`）。
- 代码提交：`bf3ec59` `feat(services): align file frames and harden provider seams`；文档提交随后形成，精确 HEAD 以 `git log -1` 为准。
- 未合并 main、未推送、未操作 awskr、未使用真实外部账号/秘密。

## 1. 真实文件接口（DS 侧已接生成候选 33—36）

- 删除旧的内部 oneof 40—47 方向；`internal/files/channel.go` 现在是 **FileOpenRequest/Reply + FileChunkRequest/Reply 的直接会话 API**（`FileBinding`、`FileOpenResult`、`FileChunkOp/Result`），由 Controller 适配器映射生成类型。
- 强制 `[A-Za-z0-9_-]{1,128}` 的 task/execution/transfer/node ID；`itemTransferID` 使用下划线，不再含冒号。execution ID 按源/目标节点分别传入（`SourceExecutionID`/`DestinationExecutionID`），并校验稳定值。
- 显式 `PathMapper`（`RelativePath`/`AbsolutePath`）：未配置构造即 `ErrPathMappingUnavailable`；绝对路径不直接下线。
- 每帧/每次 open 校验 TaskID/ExecutionID/TransferID/plan digest 回声、方向、size/SHA、resume/next offset、结果码白名单；chunk 片 ≤32KiB、文件 ≤64MiB。
- `ExecutionAdmission` 端口：执行前必须由 Controller 验证已持久接受的任务/执行 ID/plan digest；nil 失败关闭。
- 源读/目标写分别复权：`executionChannelAuthorizer` 在打开、每片、提交前调用；撤权即停止且不报成功。
- 中断/资源：源下载即时 `cappedWriter` 上限、私有 spool 目录检查、`ClaimTransfer` 原子占用、逐项 `SaveProgress`、`RecoverTransfers` 保守恢复（在途→unknown/partial，不重放）、目标写完成回执丢失→`ErrAgentUnknown` 不自动重试。
- 任务一致性：`files.TaskReporter` 接入真实 `tasks.Service.RecordResult`；新增真实 tasks 服务集成测试（submit→dispatch→coordinator→RecordResult→completed）。
- 上传授权：`AppendChunk`/`WriteUploadContent`/`GetUpload`/`AbortUpload` 均按当前权限复核；保留总控已加的完成前复核和缺通道失败审计。
- 同源/目标节点：`ErrSameNode`，HTTP 409 `same_node`，不再 500。

命令/结果：
- `go test ./internal/files/ -count=1` → PASS（含通道/协调器/硬化/真实 tasks 用例）。
- `go test ./... -count=1` → 全部 PASS（含 architecture/contracts/integration/POC）。
- 覆盖：生成帧 ID 规则、per-node execution ID、断线续传、摘要/未知提交、冲突策略真实拒绝、spool 上限、原子 claim、逐项持久化、重启恢复、首项后/分块中撤权、真实 tasks 终态、上传 append/get 撤权、同节点拒绝。
- 未覆盖：真实 Controller WSS 适配器（需总控应用补丁）、awskr/WSL 双机、真实低磁盘/符号链接竞态。**不是双端实机证据**。

## 2. CA / 供应商安全

- ACME：`ACMEConfig.TrustRoots` 必填；完整链 `leaf.Verify(Roots, Intermediates, ServerAuth, CurrentTime)`；叶子/CSR 公钥/域名/有效期绑定；authorization 仅 `dns` 类型，域名/wildcard 必须匹配；directory 及所有 authz/finalize/cert 端点同 origin，跨 origin 拒绝；DNS-01 publisher 返回 provider record ID，清理失败返回 `ErrACMEDNSCleanup` 且 `ACMEIssued.DNS01Records` 保留记录身份。
- QQ：token 端点仅在 HTTP 200 使用；非 200 即使 body 有 token 也不使用；401/403/429/5xx 分类。
- SMTP：提交阶段边界补齐：DATA 前断线=已知失败（空 outcome），终止点后确认丢失=unknown，终止点 5xx=rejected；新增本地 TLS 服务器故障注入。
- 秘密借用：Cloudflare/SMTP/QQ 的 `SecretResolver` 已替换为 **callback-scoped `SecretBorrower`**（ref+operation+subject/request/plan scope）；所有 HTTP/SMTP I/O 在 `WithSecret` 回调内完成；pre-check 失败不触网，post-check 失败返回 unknown/错误而非成功。DNS 服务在 provider 调用前填入当前 subject/request scope。
- 命令/结果：`go test ./internal/certificates ./internal/dns ./internal/notifications -count=1` → PASS（新增链/授权/跨 origin/清理 ID/借用边界/QQ token/SMTP 阶段测试）。
- 未覆盖：真实 Let's Encrypt/Cloudflare/QQ/SMTP 账号（无授权秘密），UNTESTED/BLOCKED。

## 3. 发布目标/补偿

- `Plan` 新增 gatewayIds/zoneId/certificateId/https/targetRevision；Create/Preview/HTTP 传入；stage 请求携带 SubjectID/RequestID + 目标快照。
- **DDL 只交候选**：`publishing.TargetMigrations()` 返回 `ds/0081_publishing_targets`，未登记进 `Migrations()`；`Repo` 通过 `PRAGMA table_info` 检测列，未接受 DDL 时 `TargetsPersisted=false`、不写入目标列、不伪报持久化；HTTP DTO 暴露该标志。接受 DDL 后同一代码自动持久化。
- 每阶段/每次逆序补偿前重新读取服务组 revision 并重新授权（`refreshStageAuthority`）；revision 变化/撤权会失败并持久化 `revision_conflict`/`authorization_denied`，后续阶段/补偿不执行。已有并发锁/阶段状态/幂等保持。
- 命令/结果：`go test ./internal/publishing/ -count=1` → PASS（目标持久化、阶段请求、阶段 revision/撤权、RollbackAll 阶段撤权、旧 schema fallback、HTTP/DTO）。
- 未覆盖：生产 DNS/证书/网关 publisher 仍由总控注入；Agent Nginx apply/reload/restore 未实现；不报生产发布成功。

## 门禁与剩余阻塞

- `npm run check`（2026-11-01T10:39:47Z，HEAD `bf3ec59`）：`policy/contracts/go/web/git` 全部 PASS；`npm run check:security`：npm audit 0、govulncheck 无已知漏洞。
- `go test -race`：UNTESTED（本机 CGO_ENABLED=0，无 gcc），不能声称 race 通过。
- 硬阻塞：
  1. Controller/transport 文件适配器、TaskAck 持久接受、`files.transfer` 能力/minor 协商与统一任务特例未接入（补丁见 SHARED-CHANGES）。
  2. `ExecutionAdmission`/`PathMapper`/Coordinator/Recovery 需总控组合；未接线时真实上传/下载/跨节点失败关闭。
  3. `publishing ds/0081`、task accepted 列及生产 publisher 需总控接收 DDL/契约。
  4. 秘密借用 gate 目前只放行 AI；Cloudflare/SMTP/QQ 的 module/scope 需要总控扩展并注入 borrow 适配器。
  5. 真实外部账号/awskr/双机/1C1G/arm64 未授权，UNTESTED/BLOCKED。
