# DS Wave 6 · 共享变更与契约申请（deepseek-06）

公共 Proto/生成代码、Controller 组合入口、依赖锁、迁移编号仍由总控单写。以下为可运行的作者侧差异与所需接线，批准前不是冻结契约。

## 1. Agent 文件通道 Proto（申请新增）

现状：仅 `TaskOffer/TaskOutput`，输出片上限 32KiB 且进入任务日志/截断语义，无法表达偏移、校验、恢复、取消与非文本字节。文件传输若复用输出流会污染任务日志并丢失续传。

建议在 `proto/serverfleet/agent/v1/agent.proto` 的 `Envelope.oneof` 末尾追加（不修改任何已有字段号）：

```proto
    FileOpen file_open = 40;
    FileOpenAck file_open_ack = 41;
    FileChunk file_chunk = 42;
    FileAck file_ack = 43;
    FileComplete file_complete = 44;
    FileCompleteAck file_complete_ack = 45;
    FileCancel file_cancel = 46;
    FileError file_error = 47;
```

```proto
enum FileDirection {
  FILE_DIRECTION_UNSPECIFIED = 0;
  FILE_DIRECTION_UPLOAD = 1;
  FILE_DIRECTION_DOWNLOAD = 2;
}
enum FileConflictPolicy {
  FILE_CONFLICT_POLICY_UNSPECIFIED = 0;
  FILE_CONFLICT_POLICY_SKIP = 1;
  FILE_CONFLICT_POLICY_OVERWRITE = 2;
  FILE_CONFLICT_POLICY_RENAME = 3;
}

message FileOpen {
  string transfer_id = 1;          // controller-scoped, reused across reconnects
  string execution_id = 2;         // task execution identity
  string task_id = 3;
  FileDirection direction = 4;
  string path = 5;                 // absolute POSIX path inside the agent jail
  uint64 offset = 6;               // download resume offset; 0 first attempt
  uint64 size = 7;                 // upload declared size; 0 for download
  string sha256 = 8;               // upload expected digest
  FileConflictPolicy conflict_policy = 9;
  bool create_parents = 10;
  uint32 mode = 11;                // final file mode, must be 0600
  uint32 protocol_minor = 12;
}
message FileOpenAck {
  string transfer_id = 1;
  string execution_id = 2;
  uint64 size = 3;
  string sha256 = 4;
  uint64 resume_offset = 5;
  string resolved_path = 6;
  bool skipped = 7;
}
message FileChunk {
  string transfer_id = 1;
  string execution_id = 2;
  uint64 offset = 3;
  bytes data = 4;                  // <= 32 KiB; default 16 KiB
}
message FileAck {
  string transfer_id = 1;
  string execution_id = 2;
  uint64 next_offset = 3;
}
message FileComplete {
  string transfer_id = 1;
  string execution_id = 2;
  uint64 size = 3;
  string sha256 = 4;
}
message FileCompleteAck {
  string transfer_id = 1;
  string execution_id = 2;
  uint64 size = 3;
  string sha256 = 4;
}
message FileCancel {
  string transfer_id = 1;
  string execution_id = 2;
  string reason_code = 3;
}
message FileError {
  string transfer_id = 1;
  string execution_id = 2;
  string error_code = 3;           // stable, no upstream text
  string summary = 4;              // redacted, bounded
}
```

方向矩阵（`Established` 阶段）：

| 操作 | Controller→Agent | Agent→Controller |
|---|---|---|
| upload | `file_open`, `file_chunk`, `file_complete`, `file_cancel` | `file_open_ack`, `file_ack`, `file_complete_ack`, `file_error`, `file_cancel` |
| download | `file_open`, `file_ack`, `file_complete_ack`, `file_cancel` | `file_open_ack`, `file_chunk`, `file_complete`, `file_error`, `file_cancel` |

语义：

- `transfer_id` 在同一文件传输的多次重连中稳定；控制器实现使用 `<transferID>:<itemSequence>`，重连后 Agent 以该 ID + `execution_id` 做持久去重。
- upload：`file_open_ack.resume_offset` 是 Agent 已持久的分片字节数（0 或已有 part）；控制器从该偏移续读；每片一个 `file_ack`（`next_offset` 严格等于已收字节数）；`file_complete` 后 Agent 校验整文件大小与 SHA-256，原子改名并回 `file_complete_ack`（回显真实大小/摘要）。
- download：`file_open.offset` 为续传起点；Agent 必须回相同 `resume_offset` 与整文件大小/摘要；每片由控制器写盘成功后回 `file_ack`；Agent 以 `file_complete` 结束，控制器回 `file_complete_ack`。
- 冲突策略：`skip` 回 `skipped=true`；`overwrite` 原子替换；`rename` 回 `resolved_path`（同目录内）。
- 取消：任一端 `file_cancel` 终止；Agent 删除未完成 part；已改名完成的文件不回滚。
- 错误码固定集合：`path_rejected`、`symlink_forbidden`、`insufficient_space`、`checksum_mismatch`、`conflict`、`unauthorized`、`io_error`、`protocol_error`、`frame_too_large`、`canceled`。`summary` 必须脱敏，不含路径以外的敏感值、不含文件内容。
- 帧上限沿用现有 64KiB；`file_chunk.data` ≤ 32KiB（默认 16KiB），防止 base64/日志/内存放大。
- 新增能力标识 `files.transfer.v1`：AgentHello 声明，ControllerHello 只在配置允许时授予；未授予时拒绝任何 `file_*` 帧。

生成：由总控用固定 buf/工具重新生成 `gen/go`；本包未改 Proto/生成物。

## 2. Controller/Transport 接线（申请，代码不在本包）

1. `internal/transport` 增加控制器侧文件会话端口（建议）：
   ```go
   type FileSessionOpener interface {
       OpenFileSession(ctx context.Context, nodeID, transferID, executionID string, open agentv1.FileOpen) (FileSession, error)
   }
   ```
   `FileSession` 按 `transfer_id` 复用已认证的 Agent 连接，串行写帧、按 `transfer_id` 分发读帧；每个节点同一时刻只允许一个有界会话，写帧复用现有重连/心跳/序列号。
2. `apps/controller/server` 用该端口构造 `files.AgentFileChannel` 适配器与 `files.NodeFileTransports`，再构造 `files.NewTransferCoordinator` 并注入 `files.Options.Coordinator`。示例：
   ```go
   coordinator, _ := files.NewTransferCoordinator(files.CoordinatorOptions{
       Repo: filesRepo, Transports: nodeTransports, Authorizer: authorizer,
       Audit: auditRecorder, SpoolDir: filepath.Join(stagingDir, "spool"),
   })
   filesService, _ := files.NewService(files.Options{ /* 既有字段 */ Coordinator: coordinator })
   ```
3. `tasks` 派发对 `action_id == files.transfer` 走控制器协调器，而不是向两台 Agent 各发一个 shell `TaskOffer`：调度器拿到 `task_id`/执行身份后调用
   ```go
   filesService.ExecuteTransferTask(ctx, taskID, files.TransferExecution{SubjectID, RequestID, ExecutionID})
   ```
   并把逐项结果/取消写回统一任务。其余动作的派发路径不变。
4. 当前 `tasks.Command` 没有 `ExecutionID`。申请在共享端口增加 `ExecutionID string`（只加字段），由 CX 派发适配器生成稳定值并在 `TaskOffer.execution_id`、`file_open.execution_id` 中复用；在未接收前，本包协调器使用稳定的 `task_<taskID>_<transferID>`，仍能绑定 Agent 去重，但不等于 CX 执行 ID。
5. 源读/目标写授权在创建时已分别检查；协调器在执行前重新检查 `files.read`（源）、`files.write`（目标）与 `files.transfer`（两端），撤权即失败关闭；拒绝审计写 `TaskID`。

## 3. Agent 侧必须满足（CX 实现，非本包）

- 文件根目录可配置，且默认拒绝绝对路径逃逸、`..`、空段、控制字符、反斜杠；逐段 `Lstat`/`O_NOFOLLOW` 拒绝符号链接；不做 `MkdirAll` 跟随链接。
- 仅 `0600` 最终文件、`0700` 目录；临时 part 与最终文件同目录，`fsync` 后原子 `rename`。
- 校验磁盘剩余空间（至少声明大小 − 已收字节）；不足回 `insufficient_space`，不得写半文件冒充成功。
- 持久去重：`transfer_id + execution_id` 完成记录；重复 `file_complete` 回原结果而不是重写；取消/过期清理 part。
- 每帧/每会话超时、有界内存、单路径单写者；不得读取或记录文件内容到任务日志。
- 仅在对应 `tasks.transfer` 任务 offer 已被本机接受、执行身份匹配、能力已授予时接受 `file_open`；`file_open` 本身不能绕过任务授权。

## 4. 已在本包完成（可运行增量）

- `internal/files/channel.go`：版本化帧、边界/错误码/上下文身份校验。
- `internal/files/channel_transport.go`：上传/下载、每片 ACK、断线续传（seekable 源）、整文件 SHA-256 双重校验、冲突策略、取消、错误映射；未注入通道时失败关闭。
- `internal/files/coordinator.go`：`files.transfer` 跨节点协调器；从任务 ID 加载传输记录、执行期重新授权、源下载→本地 spool→目标上传、逐项部分结果、审计、取消不重试未知副作用；`Service.ExecuteTransferTask` 写回统一任务结果。
- `LocalStagingTransport` 保留测试用途，不冒充远端。

## 5. 剩余依赖与风险

- 真实 WSS 字节链路需 CX/总控按上述端口与 Proto 接线；本包仅用同帧边界的 fake agent 验证字节/续传/校验/错误/部分结果，属控制器侧真实实现而非双机实机证据。
- 目标盘空间、符号链接竞态、跨文件系统 rename、IPv6/跨架构节点需 awskr 双进程/真实 Agent 验收。
- `tasks.Command.ExecutionID` 未接收前无法把文件会话绑定到 CX 真实执行 ID；已登记稳定替代值。
