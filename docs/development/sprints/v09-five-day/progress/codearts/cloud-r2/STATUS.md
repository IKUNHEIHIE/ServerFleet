# V09-CLOUD-HW-02 / codearts-02 进度（Cloud R2 连续整包）

日期：2026-11-03。产品代码起点 `51e0ee57e66493892eb5f5168b9e0690b53f3661`。

## 1. 共享接线 Draft PR（已提交）

- 分支：`integration/cloud-gateway-wiring-hw02`
- PR：https://github.com/IKUNHEIHIE/ServerFleet/pull/7 （Draft）
- head：`3a109046f90ee0d3a4a2958ab16533ba72186a0c`
- 提交：
  - `2ea9ea3` 真实发布桥、唯一 dispatcher/stage slot/真实 WSS uploader/child observer、Agent typed 注册、`cx/0010`
  - `0a54b1e` `-gateway-prefix` 必须在 `-file-root` 内
  - `3a10904` 受信部署文档支持 per-gateway `httpListen/httpsListen`

实现要点：

- `server.New` 只在 `-gateway-deployment` 与 `-file-deployment` 同时存在时启用；缺任一即启动失败。
- 唯一实例：`gatewayArtifactRegistry`、`GatewayArtifactStageSlot`、uploader、`gatewayTaskDispatcher`、`GatewayArtifactObserver`、production bridge；bridge 复用已有 dispatcher，不创建第二套。
- `agentLinkBridge` 先让 `FileClient.Deliver` 消费 TaskAck，再由子 observer 处理 Started/Finished；父任务结果不被子生命周期改写。
- Agent 仅在显式私有 `-gateway-prefix` 与 `-nginx-binary` 下注册 `gateway.nginx.site_apply` / `gateway.nginx.site_remove`。
- 迁移 `cx/0010_gateway_artifact_bindings` 已登记；完整集合 27 项，旧库 fixture 仍固定历史 SQL。

## 2. 能力分支增量（已提交）

- 分支：`feature/cloud-services-hw02`
- 从固定产品起点 `51e0ee5` 创建。
- 增量：DNS 删除/创建的“副作用未知”分类与 ACME 清理重试语义
  - 新增 `dns.ErrProviderOutcomeUnknown`；
  - `DeleteRecord`/`CreateRecord` 对传输错误、超时、取消返回未知，不删除本地记录、不自动重放；
  - `ACMEChallengePublisher` 清理改为状态机：成功后幂等、未知结果不重放、确定失败允许显式重试；
  - Cloudflare 传输错误同时保留 `ErrCloudflareTransport` 并标记为未知结果；
  - 新增 `internal/dns/outcome_test.go` 覆盖未知、超时、确定失败重试与幂等。

## 3. 已执行验证

环境：Huawei Cloud EulerOS 3.0 aarch64；Go 1.27.3；Node 24.22.1；npm 11.19.1；Go proxy `https://goproxy.cn,direct`（默认 proxy.golang.org 超时）。

- 共享 worktree：`go build ./...` PASS
- 共享 worktree：`go test ./apps/controller/server ./apps/controller/bootstrap` PASS
- 普通用户：`go test ./apps/agent/...` PASS（`apps/agent` 18.007s）
- 共享：`go test ./tests/integration/v09 -run 'TestCloudUploadMigration|TestWave2ControllerEndToEndHTTPSComposition|TestR1Migration'` PASS
- 共享：`go test ./tests/integration/v09/platform -run TestR2PlatformMigrationsUpgradeFromAdopted21AndReapply` PASS
- 能力：`go test ./internal/dns -count=1` PASS
- 能力：新增 4 个 DNS 测试全部 PASS
- nginx 1.27.5 已从源码编译到 `/opt/nginx-test/sbin/nginx`

## 4. BLOCKED / 未完成

- **真实正常产品路径端到端 BLOCKED**：Controller 公开 HTTP 发布 → 独立审批 → queued 上传 → 真实 Agent WSS/mTLS → 真实工件 → nginx -t/reload → HTTP 字节 → 持久终态。原因：共享 PR #7 未接收，不能把共享提交夹入能力分支；进程级组合夹具（管理员 bootstrap、enrollment、resource grant、gateway/service-group/zone/plan、独立审批人、真实 Agent `-serve`、nginx master）尚未落地。
- 逆序补偿专项（配置/reload/健康失败、上传后取消/撤权、目标修订、回执丢失、进程中断重启）缺进程级证据。
- ACME/cleanup 产品组合（订单生命周期、claim/lease、并发、审计失败、过期重启）缺组合级证据。
- 服务域剩余：DNS 部分成功清理、证书续期/分发/吊销、服务组快照、通知自动重试、监控/配额到期 worker 的 Controller 接线（中央 patch 交主控）。

## 5. 候选组合

- 共同基线：`51e0ee5`
- 共享 head：`3a10904`
- 能力 head：见本分支提交
- 组合方式：`git merge integration/cloud-gateway-wiring-hw02` 到临时组合 worktree；组合只用于验证，不推正式分支，不把共享提交夹入能力分支。

## 6. 剩余风险

- `apps/controller/bootstrap/bootstrap.go` 的 `-gateway-deployment` 是受信启动参数的最小中央改动，需主控确认窗口覆盖。
- 真实 nginx 只验证二进制可执行；未接管系统站点、未监听 80/443。
- 未知结果不重放的进程崩溃恢复仍依赖既有 cleanup journal，未新增 schema。
- 无真实供应商账号/云 API 调用。