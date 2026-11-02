# Wave6-R3 · DS-04 交接

## 交付范围

- 提交：`1a98086`、`eed7538`、`2c1aee6`（具体见 `STATUS.md`）
- 分支：`feature/v09-r6-services-ds06`；起点 `db3cf87`
- 未合 main、未推送、未 SSH/awskr、未使用真实外部账号或秘密
- 所有改动限定在任务允许的 DS 目录 + `apps/agent/gateway_capability*.go`、`apps/controller/server/gateway_*.go`；`internal/files` 未修改

## 可直接复现的本地闭环

### 1. 全量 Go 测试

```bash
.tools/go/bin/go.exe test ./... -count=1
```

当前结果：全部包 `ok`。

### 2. 真 WSS + 真 nginx + HTTP 代理字节

前置：本机有可执行的 nginx（隔离 prefix 使用 ASCII 临时目录；Windows 建议 `C:/temp`）。作者使用 portable nginx：

```bash
NGINX_BIN=C:/temp/nginx-portable/nginx.exe .tools/go/bin/go.exe test ./apps/agent -run 'TestActualWSSGatewayApplyReloadAndHTTPBytes' -count=1 -v
```

覆盖：

- Agent 主动 WSS + TLS 1.3 + mTLS 会话；
- Agent 私有 nginx prefix，`nginx -t` + `nginx -s reload`；
- apply 后 HTTP 代理真实返回 `gateway-wss-ok`；
- 相同冻结参数幂等 apply；
- stale digest remove → FAILED 且站点仍 200；
- 正确 remove → reload → HTTP 非 200。

未设置 `NGINX_BIN` 且 PATH 无 nginx 时该测试 **SKIP**，不得记为 PASS。

### 3. Agent capability 安全/回滚

```bash
NGINX_BIN=C:/temp/nginx-portable/nginx.exe .tools/go/bin/go.exe test ./apps/agent -run 'Gateway' -count=1 -v
```

含：未授权 takeover、digest/path 拒绝、validate/reload 回滚、回滚失败 → UNKNOWN、真 nginx 隔离测试。Windows 上 symlinked-root 测试按既有条件 SKIP（需管理员权限创建 symlink）。

### 4. Controller 发布任务闭环 + 补偿

```bash
.tools/go/bin/go.exe test ./apps/controller/server -run 'Gateway|Publishing' -count=1 -v
```

覆盖：

- 审批前零上传、awaiting_approval 超时非成功、dispatch 撤权、上传失败、UNKNOWN、上传前后 target revision 变化、上传中取消、同计划幂等；
- 真实 `publishing.Service` + production bridge 三阶段 + tasks 调度 + 网关 dispatcher/host；
- 成功计划 `RollbackAll` 逆序删除站点/路由与自有 DNS 记录；
- Agent UNKNOWN 后失败阶段保留补偿摘要，显式回滚成功。

### 5. cleanup 并发

```bash
.tools/go/bin/go.exe test ./internal/certificates -run 'Concurrent|CancelWins|RetryDueDoesNotDuplicate|StaleClaim|RecoverStale|ClaimMigration' -count=10 -v
```

覆盖：并发 Retry 只删除一次、RetryDue 不重复、cancel 不可被晚到成功覆盖、stale claim 不重放（unknown）、迁移保留旧行。

### 6. 根检查

```bash
npm run check
npm run check:security
npx --no-install redocly lint api/openapi/revisions/v09-r7/services-candidate.yaml
```

作者运行结果：`npm run check` 通过（Go build/vet/staticcheck/tests、web 341 tests、contracts、git diff）；`check:security` 0 vulnerabilities；redocly valid。

## 启用生产前必须由中央应用（见 `SHARED-CHANGES.md`）

1. `certificates.CleanupClaimMigrations()` 注册到 `server.Migrations()`，同步 migration count 24→25 与 allowlist。
2. Agent 注册 `gateway.nginx.site_apply/remove` capability，program config 注入私有 prefix/site/artifact/main/nginx binary；未配置保持关闭。
3. Controller：
   - `agentAllowedActions()` 增加两个网关动作；
   - 用 late-bound dispatcher 包装 `tasks.Service.Dispatcher`；
   - tasks service 创建后调用 `NewProductionPublishingBridge`，把返回的网关 dispatcher 填入 late dispatcher；
   - CX 实现 `GatewayArtifactUploader`（接口已含 taskID），上传前 `CheckExecution`，只走现有 Agent file channel。
4. `ExtraPermissions()` 增加两个 Nginx 动作权限；`cleanup-service` 需 `certificates.cleanup.retry`。
5. OpenAPI candidate 注册根 Redocly 配置。

未完成上述任一装配时：

- 不得声称生产发布执行器已启用；
- 未注册 ds/0063 时 cleanup List/Retry 会因缺列失败；
- 直接走 `agentBridge` 的网关任务不会上传工件，Agent 会以 `artifact_missing` 失败，不能当作成功。

## 已知未完成 / BLOCKED

| 项 | 状态 | 说明 |
| --- | --- | --- |
| 真实 Cloudflare / ACME / QQ / SMTP 账号 | BLOCKED | 任务禁止真实外部账号；仅有本地 TLS/synthetic/黑盒证据 |
| ACME 实际签发产品流程 | BLOCKED | 仓库未调用 `IssueDNS01`；`PublicationACMEIssuer` 已提供失败记录接缝，待证书生命周期流程接入 |
| awskr / 双机 WSS | NOT RUN | 作者不得操作；本地 in-process WSS 已覆盖帧与真 nginx |
| `go test -race` | UNTESTED | Windows 无 cgo/gcc；并发测试用 `-count=10` |
| Agent WSS symlink 测试 | SKIP (Windows) | 需 Unix/管理员权限创建 symlink；Linux CI 应运行 |
| 系统 nginx/80/443 接管 | 明确不支持 | DS-04 要求不接管；仅私有 prefix/高位端口 |
| 真实生产 bridge/Dispatcher/迁移/权限/OpenAPI 注册 | 待中央 patch | 见 SHARED-CHANGES.md |

## 文件索引

- cleanup：`internal/certificates/cleanup.go`、`cleanup_claim_migrations.go`、`cleanup_http.go`、`cleanup_test.go`
- 网关执行：`apps/controller/server/gateway_worker.go`、`gateway_tasks.go`、`gateway_acme.go`
- 发布失败补偿：`internal/publishing/service.go`、`internal/publishing/production.go`
- Agent：`apps/agent/gateway_capability.go`、`gateway_capability_private_unix.go`、`gateway_capability_private_windows.go`
- 测试：`apps/controller/server/gateway_{review,compensation,publishing,secret}_test.go`、`apps/agent/gateway_capability{,_wss}_test.go`
- 契约：`api/openapi/revisions/v09-r7/services-candidate.yaml`
