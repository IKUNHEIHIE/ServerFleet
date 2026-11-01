# V09-R6-DS-02 · 交接与验证（deepseek-06）

- 分支 `feature/v09-r6-services-ds06`；基线 `379d5e0`；代码 `bf3ec59`；未推送/未合并/未操作 awskr。
- 共享补丁与 DDL 见 [SHARED-CHANGES.md](SHARED-CHANGES.md)，详细状态见 [STATUS.md](STATUS.md)。

## 总控接线顺序

1. **先接 DS-02 文件链路**：按 `SHARED-CHANGES` 第 2/3 节补 `internal/transport` File* 语义校验、Controller `file_channel.go` 适配器、TaskAck accepted 持久标记、`files.transfer.v1`/minor 协商与 `files.transfer` 动作 schema。随后在组合层构造：
   ```go
   coordinator, _ := files.NewTransferCoordinator(files.CoordinatorOptions{
       Repo: filesRepo, Transports: nodeTransports, Authorizer: authorizer,
       Admission: fileHub, Audit: auditRecorder, SpoolDir: filepath.Join(stagingDir, "spool"),
   })
   filesService, _ := files.NewService(files.Options{/* ... */ Coordinator: coordinator, Tasks: tasksService})
   ```
   `NodeFileTransports` 对每节点返回同时实现 Upload/Download 的 `files.ChannelTransport`；`ChannelTransportConfig.Paths` 必须是显式 ACL/根映射，未配置即失败关闭。
2. 调度：action `files.transfer` 必须一次协调器执行（`ExecuteTransferTask`），两个目标先处于 dispatched 且 TaskAck accepted，再由协调器写入；执行后 `tasks.Service.RecordResult` 由 `files.TaskReporter` 写入。不要对两台 Agent 各发 shell TaskOffer。
3. 启动时调用 `filesService.RecoverTransfers(ctx)`，把上次在途传输标为 partial/failed + unknown，不重放；`ClaimTransfer` 防止重复执行。
4. 供应商：按 `SHARED-CHANGES` 第 5 节注入 `providerSecretBorrower` 到 Cloudflare/SMTP/QQ；扩展 credential gate 的 module/scope，真实 I/O 必须在 `UseForService` 回调内。ACME 配置显式 `TrustRoots`；DNS-01 publisher 持久化 `DNS01Records`。
5. 发布：接受 `publishing.TargetMigrations`（ds/0081）与任务 accepted 列后，把 `publishing.TargetMigrations()` 加入集中迁移并更新共享迁移计数；否则 `TargetsPersisted=false`，不得把目标当作已持久。生产 publisher/Agent Nginx apply/reload 仍未实现。

## 本地可复现命令

```bash
go test ./internal/files/ ./internal/dns/ ./internal/notifications/ ./internal/certificates/ ./internal/publishing/ -count=1
go test ./... -count=1
npm run check
npm run check:security
```

当前结果：全部 PASS；web 329；Go 含 architecture/contracts/integration/POC；安全 0 漏洞。`go test -race` 未执行（CGO_ENABLED=0、无 gcc），不得声称 race 通过。

## 实机（由总控在 awskr/WSL 完成，作者不操作）

- 文件字节：使用两个真实 Agent（awskr 普通用户私有目录 + WSL/第二台授权机），各自私有文件根、临时 SQLite、127.0.0.1 高位端口；先验证 TaskOffer accepted，再创建传输。预期目标 SHA 与源一致、审计含源读/目标写、断线续传不重跑 unknown、取消/撤权不报成功、symlink/路径逃逸/低磁盘失败且有界。不得把单机两进程记作跨机证据。
- 供应商：只用本地 TLS fake 已覆盖；真实 Cloudflare/ACME/QQ/SMTP 需负责人逐项授权，未授权标 BLOCKED。
- Nginx：沿用上级 `wave6/HANDOFF.md` 的隔离 127.0.0.1:18080/18443 方案，不触碰系统 nginx/80/443；Agent apply/reload/restore 未实现，不能以语法/代理 smoke 代替统一任务发布成功。

## 停止条件

共享 Proto/transport/Controller/DDL 未接收、真实供应商/实机未授权、生产 publisher/Agent Nginx 未实现——这些是硬阻塞。作者已完成允许目录内的独立实现与本地证据，不再扩展其他模块。
