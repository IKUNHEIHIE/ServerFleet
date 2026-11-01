# V09-R6-DS-02 · 共享差异与补丁（deepseek-06）

基线 `379d5e0`；作者代码 `bf3ec59`。以下为需要总控/CX 接收的共享入口、DDL、组合与契约差异；作者未改 `apps/**`、Proto/gen、根依赖/锁或迁移登记。旧 Wave6 文档的 oneof 40—47 提案**不再适用**，以本文件为准。

## 1. 文件通道：DS 控制器侧已改为生成帧会话 API

`internal/files/channel.go` 不再有 open/chunk oneof 40—47 的内部帧；对外是：

```go
type FileBinding struct {
    TaskID, ExecutionID, TransferID, NodeID string
    RelativePath string
    Direction ChannelDirection // read|write
    Size int64; SHA256 string
    ConflictPolicy ConflictPolicy
    ResumeOffset int64; CreateParents bool
    PlanDigestSHA256 string
}
type FileOpenResult struct {
    TaskID, ExecutionID, TransferID string
    Size int64; SHA256, RelativePath string
    ResumeOffset int64; Skipped, Complete bool
    ResultCode, PlanDigestSHA256 string
}
type FileChunkOp struct { Offset uint64; ReadBytes uint32; Data []byte; Finish, Cancel bool }
type FileChunkResult struct {
    TaskID, ExecutionID, TransferID string
    Offset, NextOffset uint64; Data []byte
    Complete bool; ResultCode string
}
type AgentFileSession interface {
    ReadChunk(context.Context, FileChunkOp) (FileChunkResult, error)
    WriteChunk(context.Context, FileChunkOp) (FileChunkResult, error)
    Cancel(context.Context, string) error
    Close() error
}
type AgentFileChannel interface {
    OpenFileChannel(context.Context, FileBinding) (AgentFileSession, FileOpenResult, error)
}
type PathMapper interface {
    RelativePath(ctx context.Context, nodeID, absolutePath string) (string, error)
    AbsolutePath(ctx context.Context, nodeID, relativePath string) (string, error)
}
type ExecutionAdmission interface {
    AdmitFileExecution(ctx context.Context, taskID, nodeID, executionID, planDigest string) error
}
```

- ID：`[A-Za-z0-9_-]{1,128}`；`transferID = <xferID>_<itemSequence>`，无冒号。`executionID` 分别使用源/目标节点的 `executionIDFor(taskID,node,planHash)`。
- DS 在 open 结果中校验 task/exec/transfer 回声、plan digest、size/SHA、resume/next offset；chunk 片 ≤32KiB、文件 ≤64MiB；结果码固定集合。方向由 binding 固定，帧不带方向。
- 源读/目标写分别复权在 open、每片、提交前；`ExecutionAdmission` nil 或失败即拒绝。
- 未知提交（finish 已发出但确认丢失）返回 `ErrAgentUnknown`，不重试；`ClaimTransfer`/`SaveProgress`/`RecoverTransfers` 已实现。

## 2. `internal/transport` 语义/帧校验（必须由总控接收）

`validation.go` 的 `knownPayload` 增加且只允许 Established 阶段：

```go
case *agentv1.Envelope_FileOpenRequest:  return m.FileOpenRequest, ControllerToAgent, Established
case *agentv1.Envelope_FileOpenReply:    return m.FileOpenReply, AgentToController, Established
case *agentv1.Envelope_FileChunkRequest: return m.FileChunkRequest, ControllerToAgent, Established
case *agentv1.Envelope_FileChunkReply:   return m.FileChunkReply, AgentToController, Established
```

新增校验（建议固定上限 `MaxFileBytes=64<<20`、`MaxFileChunkBytes=32<<10`，不把 64MiB 写成生产默认之外的安全声明）：

- 四个帧的 `task_id/execution_id/transfer_id` 必须匹配 `[A-Za-z0-9_-]{1,128}`。
- `FileOpenRequest`：`direction` 仅 `read|write`；`relative_path` 满足 `fs.ValidPath`、非 `.`、无 `\`/`:`、不以 `.sf-` 开头、≤1024；`size ≤64MiB`；`conflict_policy` 仅 `skip|rename|overwrite`；write 要求 64-hex `sha256`，read 可空；`plan_digest_sha256` 必须 64-hex。
- `FileChunkRequest`：`cancel=true` 时不得带 data/read_bytes/finish；否则 `data ≤32KiB`、`read_bytes ≤32KiB`、offset+len ≤64MiB。
- `FileChunkReply`：`data ≤32KiB`、next_offset ≤64MiB、`result_code` 仅白名单 `ok/complete/skipped/conflict/path_rejected/symlink_forbidden/insufficient_space/checksum_mismatch/unauthorized/io_error/canceled/protocol_error/unsupported/frame_too_large/unknown`；open reply 的 size/sha/resume 与请求绑定。
- 任何 File* 帧未协商 `files.transfer.v1`/minor<1 一律拒绝；不得由 Web/任务以外路径打开。

## 3. Controller 文件适配器（建议新文件 `apps/controller/server/file_channel.go`）

大致实现（需总控按现有 `ControllerLinks`/权限/审计风格落地并编译）：

```go
type fileHub struct {
    mu       sync.Mutex
    sessions map[string]*fileSession // transfer_id -> session
    links    *transport.ControllerLinks
    tasks    *tasks.Repo
    node     func(nodeID, taskID, execID, planDigest string) error
    now      func() time.Time
}

func (h *fileHub) OpenFileChannel(ctx context.Context, b files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
    if h == nil || h.links == nil || b.PlanDigestSHA256 == "" { return nil, files.FileOpenResult{}, files.ErrChannelUnavailable }
    if err := h.node(b.NodeID, b.TaskID, b.ExecutionID, b.PlanDigestSHA256); err != nil { return nil, files.FileOpenResult{}, err }
    s := &fileSession{hub: h, binding: b, replies: make(chan *agentv1.Envelope, 8)}
    h.mu.Lock(); if _, exists := h.sessions[b.TransferID]; exists { h.mu.Unlock(); s.Close(); return nil, files.FileOpenResult{}, files.ErrChannelProtocol }; h.sessions[b.TransferID] = s; h.mu.Unlock()
    req := &agentv1.Envelope{Message: &agentv1.Envelope_FileOpenRequest{FileOpenRequest: &agentv1.FileOpenRequest{
        TaskId: b.TaskID, ExecutionId: b.ExecutionID, TransferId: b.TransferID,
        RelativePath: b.RelativePath, Direction: string(b.Direction), Size: uint64(b.Size), Sha256: b.SHA256,
        ConflictPolicy: string(b.ConflictPolicy), ResumeOffset: uint64(b.ResumeOffset),
        CreateParents: b.CreateParents, PlanDigestSha256: b.PlanDigestSHA256,
    }}}
    if err := h.links.Send(ctx, b.NodeID, req); err != nil { s.Close(); return nil, files.FileOpenResult{}, err }
    env, err := s.await(ctx); if err != nil { s.Close(); return nil, files.FileOpenResult{}, err }
    reply := env.GetFileOpenReply(); if reply == nil { s.Close(); return nil, files.FileOpenResult{}, files.ErrChannelProtocol }
    return s, files.FileOpenResult{TaskID: reply.GetTaskId(), ExecutionID: reply.GetExecutionId(), TransferID: reply.GetTransferId(),
        Size: int64(reply.GetSize()), SHA256: reply.GetSha256(), RelativePath: reply.GetRelativePath(),
        ResumeOffset: int64(reply.GetResumeOffset()), Skipped: reply.GetSkipped(), Complete: reply.GetComplete(),
        ResultCode: reply.GetResultCode(), PlanDigestSHA256: reply.GetPlanDigestSha256()}, nil
}
```

- `fileSession.ReadChunk/WriteChunk` 发送 `FileChunkRequest` 并等待对应 `FileChunkReply`；必须校验 reply 的 task/exec/transfer/offset/next_offset/complete/result_code；`Cancel` 发送 `{Cancel:true}`，`Close` 从 hub 删除并关闭等待者。
- `fileHub.HandleReply(observation,envelope)` 按 `transfer_id` 路由到 session；未匹配/非本节点拒绝；字节不得写入任务日志。
- `agentLinkBridge.event` 增加：
  ```go
  case envelope.GetFileOpenReply() != nil, envelope.GetFileChunkReply() != nil:
      return b.files.HandleReply(ctx, observation, envelope)
  ```
- `agentLinkBridge.taskAck` 在 accepted 时持久记录接受（见下），拒绝时结束 session；`taskFinished`/`disconnected` 让该节点 session 失败。
- 传输 `handshake` 仅在配置允许且协议 minor≥1 时授予 `files.transfer.v1`；`agentAllowedActions()` 增加 `"files.transfer":"1"`（或正式 schema 版本），但不把任意 shell 加回。

### 3.1 任务“已持久接受”标记（DDL + repo patch）

`task_targets` 当前无 accepted 状态。候选 DDL（总控编号）：

```sql
ALTER TABLE task_targets ADD COLUMN accepted_at TEXT;
```

- `tasks.Target` 增加 `AcceptedAt time.Time`；`MarkAccepted(ctx,q,taskID,nodeID)` 仅在 `state='dispatched'` 时写入；`agentLinkBridge.taskAck` accepted 时调用。
- `files.ExecutionAdmission` 的 Controller 实现必须校验：任务存在、action=`files.transfer`、target 已 dispatched 且 `accepted_at` 非零、`executionIDFor(taskID,node,planHash)` 等于传入 ID、planHash 等于 binding digest、节点未 revoked/suspended。任一失败返回错误，不能打开文件会话。

### 3.2 调度特例（推荐实现）

`tasks` 的 `Dispatcher` 当前对每个 target 调一次。文件传输必须**一个任务一次协调器**：

```go
// 建议 tasks.Options 增加 ControllerExecutor：
type ControllerExecutor interface {
    ExecuteOnce(ctx context.Context, task Task) (DispatchResult, error)
}
// scheduler.dispatch 在 action_id == "files.transfer" 时：
//   1) 对已持久接受/已 dispatch 的目标不发送 shell TaskOffer；
//   2) 调 ExecuteOnce(task)；
//   3) accepted 后用 RecordResult 写两个目标；unknown 不重跑。
```

若不在 tasks 内加端口，至少由 Controller 组合在调用 `filesService.ExecuteTransferTask` 前将两个目标标记 dispatched，并在执行后 `tasksService.RecordResult`；不得让 Web 直接调用协调器。

## 4. 发布目标 DDL（候选，未登记）

本包新增 `publishing.TargetMigrations()`（未加入 `Migrations()`）：

```sql
ALTER TABLE publishing_plans ADD COLUMN gateway_ids TEXT NOT NULL DEFAULT '[]';
ALTER TABLE publishing_plans ADD COLUMN zone_id TEXT NOT NULL DEFAULT '';
ALTER TABLE publishing_plans ADD COLUMN certificate_id TEXT NOT NULL DEFAULT '';
ALTER TABLE publishing_plans ADD COLUMN https INTEGER NOT NULL DEFAULT 0;
ALTER TABLE publishing_plans ADD COLUMN target_revision INTEGER NOT NULL DEFAULT 1;
```

- `Repo` 用 `PRAGMA table_info` 检测列；未接受 DDL 时 `TargetsPersisted=false`，不写目标列，HTTP DTO 暴露 `targetsPersisted:false`；接受后同一代码自动持久化。旧迁移计数测试保持 19，没有静默选旧目标。
- 总控接收后把 `TargetMigrations()` 并入集中迁移表/编号并更新共享迁移计数。目标真实执行仍需生产 DNS/证书/网关 publisher 与 Controller 桥；当前不报生产成功。

## 5. 秘密借用注入（Controller patch）

`dns.SecretBorrower` / `notifications.SecretBorrower` 均已改为 callback-scoped：

```go
type SecretUse struct { Ref, Operation, SubjectID, RequestID, PlanDigest string }
type SecretBorrower interface {
    WithSecret(ctx context.Context, use SecretUse, fn func(context.Context, string) error) error
}
```

Controller 需提供（按 CX `auth.CredentialService.UseForService`）：

```go
type providerSecretBorrower struct { credentials *auth.CredentialService; lookup func(ref string) (auth.CredentialOperation, error) }
func (b *providerSecretBorrower) WithSecret(ctx context.Context, use dns.SecretUse, fn func(context.Context, string) error) error {
    op, err := b.lookup(use.Ref) // 当前 owner/ref revision/purpose/scope
    if err != nil { return err }
    op.SubjectID = use.SubjectID; op.Source = "system"; op.PlanDigest = use.PlanDigest
    op.Action = "use"; op.Module = "dns"/"notifications"; op.OperationID = uniqueUseID(use)
    return b.credentials.UseForService(ctx, op, func(ctx context.Context, secret []byte) error {
        defer clear(secret)
        return fn(ctx, string(secret))
    })
}
```

- 需要扩展 `server.go` 当前只允许 AI 的 credential gate：按 module/scope 放行 `dns-provider`、`smtp-password`、`qqbot-secret` 的当前 owner/引用 revision/operation，并保证 `UseForService` 的 before/after 检查覆盖供应商 I/O；不得暴露通用 HTTP/tool 解密入口。
- 注入 `dns.CloudflareConfig{Secrets: borrower}`、`notifications.SMTPConfig{Secrets: borrower}`、`QQConfig{Secrets: borrower}`；`dns.Service` 已在 provider 调用前设置 subject/request scope。

## 6. ACME / 通知安全

- `certificates.ACMEConfig` 新增必填 `TrustRoots *x509.CertPool`；Controller 必须显式加载配置的信任根（测试用本地 CA，生产用明确的 CA bundle），不要回调到系统根默认。
- `ACMEDNS01Publisher.Publish` 现在返回 `(recordID string, cleanup func(context.Context) error, error)`；`ACMEIssued.DNS01Records` 保留 FQDN/value/recordID，清理失败返回 `ErrACMEDNSCleanup`，调用方必须持久化失败记录 ID。
- `fetchAuthorization` 只接受 `dns` identifier、域名与请求一致、wildcard 标志一致；所有 ACME 端点必须与 directory 同 origin；跨 origin 返回 `ErrACMEOrigin`。
- QQ token 仅在 HTTP 200 使用；SMTP 阶段边界已细化（DATA 前断线=已知失败、终止点后丢失=unknown、终止点 5xx=rejected）。
- 真实供应商/CA 账号仍未授权，UNTESTED/BLOCKED。

## 7. Nginx/发布动作 schema（与 CX 对齐）

建议 Agent 受控动作（root 修改类，必须统一任务 + 审批 + 非交互参数）：

```json
{"action_id":"gateway.nginx.site_apply","schema":"1","params":{
 "operation":"apply","site_path":"/etc/nginx/conf.d/...","artifact_path":"/var/lib/serverfleet/staging/nginx/<gateway>/<sha>.conf",
 "config_sha256":"<64hex>","validate_command":"nginx -t","reload_command":"reload",
 "takeover_approved":"true","approval_ref":"<approval>"}}
```

- Agent 必须在 staging 根内校验 artifact 路径、SHA-256、site path、父目录/符号链接；校验通过才原子落盘并在 `nginx -t` 后 reload；失败恢复旧文件。remove 只在磁盘摘要等于 `config_sha256` 时删除；root/reload 不能由本包绕过。artifact 通过 DS 文件通道推送，不进入任务参数/日志。当前无真实 Agent apply/reload/restore 证据。
