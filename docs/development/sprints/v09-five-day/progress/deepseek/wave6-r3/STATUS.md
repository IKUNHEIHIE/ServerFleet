# Wave6-R3 · DS-04 状态

- 会话：deepseek-06；审查人：codex-review-02
- 工作目录：`.worktrees/feature--v09-r6-services-ds06`
- 分支：`feature/v09-r6-services-ds06`
- 起点：`db3cf87`
- 当前 HEAD：见最后一次提交；工作树保持可运行增量
- 未合 main、未推送、未操作 awskr、未使用真实外部账号或秘密

## 提交

| SHA | 内容 |
| --- | --- |
| `1a98086` | cleanup 原子 claim/lease、cancel 优先级、重启未知结果不重放、I/O 前后复权、竞争测试与 ds/0063 候选 DDL |
| `eed7538` | 网关任务审批后上传、终态等待、同计划幂等；Agent 私有根/owner/二进制解析/路径竞态/回滚 UNKNOWN |
| `2c1aee6` | 发布失败补偿身份持久化、DNS 部分清理保留、真实三阶段+任务闭环、供应商 secret callback 绑定、services-candidate 对齐 |
| `dda43a1` | 工件上传绑定执行任务 ID（上传前 `CheckExecution`）+ wave6-r3 证据 |
| `73dbebb` | 每网关显式私有 nginx 根，缺失在写路由前 fail closed |
| `f8aa07b` | staticcheck 清理（未使用测试字段） |

## 已完成（对应 WAVE6-R3 DS 条目）

### 1. 真实发布执行器与终态

- `apps/controller/server/gateway_worker.go`
  - `gatewayArtifactRegistry`：仅内存登记冻结工件，审批前零 Agent 上传。
  - `gatewayTaskDispatcher`：`tasks.Dispatcher` 的唯一网关执行钩子；在**上传前**复核当前 approval（revision/plan hash/TTL/非自批）、发布计划 `targetRevision`、节点在线与当前主体授权；上传后再次复核同一组条件才把未修改的 offer 交给 Agent 链路。
  - 上传失败 / 撤权 / target 变化 / 上传后取消 → `Accepted=false`，任务失败，不进入 Agent。
  - `gatewayTaskHost`：提交 `OperationClassModifying + RequireApproval`，等待真实终态；`completed + 全部 target succeeded` 才返回成功；`awaiting_approval` 超时返回 `ErrGatewayAwaitingApproval`；`failed/partial_failure/canceled/expired` 与 `unknown` 分别失败；同计划 apply/remove 使用独立 idempotency key。
- `apps/controller/server/gateway_tasks.go`
  - `GatewaySiteApplier.Apply/Rollback` 在路由写入、任务上传/执行、补偿前读取当前计划并比较 `TargetRevision`、网关、zone、证书、域名、服务组。
  - 发布失败仍返回带 `RouteCreated/RouteRevision/SitePath/Digest/PlanID/PlanTargetRevision` 的部分摘要；补偿只删除 `RouteCreated=true` 且 revision 未变化的路由；站点移除失败不删路由。
  - `GatewayInstallPaths` 每网关显式 `SiteDir`/`ArtifactDir`，写入 Agent 私有 prefix；缺失时在写路由前 fail closed，不使用 `/etc/nginx` 默认值。
  - 删除 `TargetResourceID` 使用；`TargetType=node_ids`。
- 测试：`gateway_review_test.go` 10 项（审批前零上传、awaiting 非成功、撤权、上传失败、UNKNOWN、上传前后 target revision 变化、上传中取消、同计划幂等、成功闭环）；`gateway_compensation_test.go` 9 项（外来摘要、新 revision 路由、站点移除失败、失败摘要可补偿等）。

### 2. 发布成功/失败恢复

- `internal/publishing/service.go`：失败阶段保存已创建资源的补偿摘要（`ResultSummary`），显式 rollback 使用该身份，不猜测。
- `internal/publishing/production.go`
  - DNS 失败时尝试删除本阶段创建的记录；删除失败的身份保留在失败摘要中，错误不吞掉；显式回滚重试。
  - Gateway 失败时把部分摘要向上返回，`publishing.Service` 持久化后可由 `RollbackAll` 补偿。
- `apps/controller/server/gateway_publishing_test.go`：真实 `publishing.Service` + 三阶段 production bridge + tasks 调度 + 网关 dispatcher/host + fake Agent：
  - 成功：DNS 记录创建、上传 1 次、Agent 1 次、plan completed；`RollbackAll` 逆序删除站点/路由与自有 DNS 记录，plan canceled。
  - Agent UNKNOWN：plan partial_failure、gateway 阶段失败摘要保留；显式回滚（Agent 恢复 succeeded）删除路由与 DNS，plan canceled。
- `apps/agent/gateway_capability_wss_test.go`：真实 WSS + 真 nginx（隔离私有 prefix、高位 loopback）：
  - apply → `nginx -t` / reload → HTTP 代理返回 `gateway-wss-ok`；
  - 相同冻结参数幂等 apply 仍成功；
  - stale digest remove → FAILED 且站点仍 200；
  - 正确 remove → reload → HTTP 非 200。
  - 运行：`NGINX_BIN=C:/temp/nginx-portable/nginx.exe go test ./apps/agent -run TestActualWSSGateway -v` → PASS。

### 3. cleanup 并发（先返修）

- `internal/certificates/cleanup.go`
  - CAS claim：`attempts`/`state`/`claim_token`/`lease_until` 原子占用；lease 2m 上限 15m。
  - 并发 `Retry` / `RetryDue` 只允许一个 provider delete；失败审计 `cleanup_busy`。
  - `Cancel` 清理 claim 后，晚到的 provider 成功不能覆盖 canceled；晚到结果只写 `cleanup_superseded` 审计。
  - 过期 claim 恢复为 `unknown`（`provider_outcome_unknown`），绝不自动重放；`Retry` 对 unknown 拒绝。
  - provider I/O 前复核当前授权，I/O 后再次审计授权结果；结果仍按真实 provider 结果持久化。
  - `RecordFailure` 保持 `plan_id/domain/fqdn/record_id/record_value` 绑定。
- `internal/certificates/cleanup_claim_migrations.go`：**候选** `ds/0063_cleanup_claim`（重建表加入 `unknown` 状态、claim/lease 列）；未注册中央迁移。
- 测试：并发 Retry 只删一次、RetryDue 不重复、cancel 胜过晚到成功、stale claim 不重放、迁移保留旧数据、HTTP 路由；`-count=10` 通过。

### 4. Agent 网关安全

- `apps/agent/gateway_capability.go`
  - 私有 prefix/site/artifact 目录必须为绝对路径、目录、非 symlink；Unix 校验 owner=当前用户且组/其他无权限；main config 必须常规文件且组/其他不可写。
  - nginx 可执行文件启动时解析为绝对常规文件，拒绝含空白/分隔符的名称；argv 固定。
  - artifact 读取前逐段拒绝 symlink；Lstat/Open 后 `os.SameFile` 校验防止路径替换。
  - 站点写入/nginx -t/reload 失败后回滚；回滚自身失败或未确认 → `EXECUTION_RESULT_UNKNOWN`。
  - 不接管系统 nginx/80/443；root 由程序配置且必须位于私有 prefix。
- 测试：`gateway_capability_test.go` + 真 nginx 隔离测试；新增 restore 失败 UNKNOWN 用例；WSS 用例见上。

### 5. 供应商与契约

- `internal/dns`、`internal/notifications`：`SecretUse` 增加 `Module`/`Purpose`；Cloudflare/SMTP/QQ 调用点绑定固定值。
- `apps/controller/server/gateway_worker.go` 之外的 `gateway_tasks.go`：`providerSecretBorrower` 现在要求 lookup 的 module/purpose 与调用点一致，再将 owner/revision/plan digest/operation ID 交给 `CredentialUseService`；真实 auth 服务负责 I/O 前后复权与重放拒绝。
- `apps/controller/server/gateway_secret_test.go`：本地 TLS 正常/失败、缺引用/错 purpose 拒绝、secret 清零、operation 绑定。
- `apps/controller/server/gateway_acme.go`：`PublicationACMEIssuer` 在 ACME 失败且已有记录身份时，用有界未取消上下文写入 cleanup journal，绑定 subject/request/plan/domain/FQDN/recordID/value；journal 失败 join 到原错误。
- `api/openapi/revisions/v09-r7/services-candidate.yaml`：真实 HTTP 状态对齐（run 失败 502 而非 422）、cleanup `unknown` 状态、busy/unknown 409 说明；redocly lint 通过。

## 验证命令与结果

```text
.tools/go/bin/go.exe test ./... -count=1
→ 全部包 ok（apps/agent、apps/controller/server、internal/*、tests/*）

NGINX_BIN=C:/temp/nginx-portable/nginx.exe .tools/go/bin/go.exe test ./apps/agent -run 'Gateway' -count=1
→ PASS（含 TestActualWSSGatewayApplyReloadAndHTTPBytes、TestGatewayCapabilityRealNginxIsolated）

.tools/go/bin/go.exe test ./internal/certificates -run 'Concurrent|CancelWins|RetryDueDoesNotDuplicate|StaleClaim|RecoverStale' -count=10
→ ok

npm run check        → 通过（exit 0；Go format/vet/staticcheck/build/test、web 341 tests、contracts、git diff）
npm run check:security → 0 vulnerabilities（npm audit + govulncheck）
npx --no-install redocly lint api/openapi/revisions/v09-r7/services-candidate.yaml → valid
```

最终 HEAD 复核：`npm run check` exit 0（`reports/final-check.log`）；`npm run check:security` exit 0（`reports/final-security.log`）。

## 剩余阻塞 / 未测

1. **中央注册 patch（见 SHARED-CHANGES.md）**
   - `ds/0063_cleanup_claim` 迁移未注册；在注册前 cleanup journal 的 List/Retry 不能在生产库运行。
   - Agent 未注册 `gateway.nginx.site_apply/remove` capability 与运行时配置入口。
   - Controller 未安装 `gatewayTaskDispatcher`（需 late-bound dispatcher）与 `NewProductionPublishingBridge` 装配；若只启用旧 `agentBridge` 直接 dispatch，工件不会先上传。
   - auth 权限目录需增加两条 Nginx 动作权限，否则真实主体无法授权该任务 ActionID。
   - OpenAPI candidate 未注册根 Redocly 配置。
2. **真实外部供应商**：Cloudflare/ACME/QQ/SMTP 仅本地 synthetic/TLS/黑盒测试，未使用真实账号（按任务禁止）。
3. **awskr / 双机 WSS**：作者未操作；本地 WSS 已覆盖 Agent↔Controller 帧、真 nginx 与 HTTP 字节。
4. **race detector**：Windows 环境无 cgo/gcc，`go test -race` 无法运行；并发测试通过 `-count=10`。记为 UNTESTED。
5. **生产 nginx 接管**：明确不支持系统 nginx/80/443；仅私有 prefix + 高位端口。
6. **ACME 实际签发编排**：仓库当前没有被调用的 IssueDNS01 产品流程；`PublicationACMEIssuer` 是失败记录组合接缝，需证书生命周期流程接入后才生效。
