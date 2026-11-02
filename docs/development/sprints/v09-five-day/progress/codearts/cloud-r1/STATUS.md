# V09-CLOUD-HW-01 · codearts-01 状态

- 任务ID：V09-CLOUD-HW-01
- 唯一会话：codearts-01
- 分支：`feature/cloud-services-hw01`
- 工作目录：`/home/service/ServerFleet-HW01`
- 基线：`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`（main，与统一起点一致）
- 开工时间：2026-11-03（Asia/Shanghai）
- 状态：IN PROGRESS

## 环境实测

| 项 | 实测 |
|---|---|
| 平台 | Huawei Cloud EulerOS 3.0 aarch64，2 vCPU / 7.8GiB |
| 普通用户 | `service` uid=1000，私有根 `/home/service`（0700） |
| Go | 1.27.3（官方 linux-arm64 包，`/home/service/.tools/go`） |
| Node/npm | 系统 Node v22.11.0 / npm 10.9.0；仓库要求 24.22.1 / npm 11，**待安装** |
| Nginx | 1.31.8 源码构建，`/home/service/.tools/nginx/sbin/nginx`（私有 prefix，未接管系统站点/80/443） |
| Git 通道 | `github.com:443` 在部分 IP 上不稳定；改用 `ssh.github.com:443` + 仓库 deploy key（id 168517374）克隆/推送 |
| Go 模块代理 | `proxy.golang.org` 不可达，使用 `https://goproxy.cn,direct`（`GOSUMDB=off`，仍以 go.sum 校验） |

## 检查点 1：真实 gateway CI 失败修复（2026-11-03）

### 复现

CI run 41441915813 / job 125266284154 中 `foundation` 失败：

- `TestGatewayCapabilityRealNginxIsolated` → `gateway_capability_test.go:345: agent: gateway capability rejected`
- `TestActualWSSGatewayApplyReloadAndHTTPBytes` → `gateway_capability_wss_test.go:74: agent: gateway capability rejected`

本地以 `service` 用户在可写包目录复现，输出与 CI 完全一致：

```sh
NGINX_BIN=/home/service/.tools/nginx/sbin/nginx go test ./apps/agent \
  -run "TestGatewayCapabilityRealNginxIsolated|TestActualWSSGatewayApplyReloadAndHTTPBytes" -count=1 -v
# --- FAIL: TestGatewayCapabilityRealNginxIsolated (0.00s)
#     gateway_capability_test.go:345: agent: gateway capability rejected
# --- FAIL: TestActualWSSGatewayApplyReloadAndHTTPBytes (0.00s)
#     gateway_capability_wss_test.go:74: agent: gateway capability rejected
```

### 根因

`apps/agent/gateway_capability_test.go` 的 `asciiTestRoot` 无条件尝试 `C:/temp`、`D:/temp`。这两个前缀只在 Windows 上是绝对路径；在 Linux 上是相对路径，当包目录可写时 `os.MkdirAll("C:/temp")` 成功，函数返回相对路径 `C:/temp`。

`newGatewayCapability` 要求 `filepath.IsAbs(prefix)` 且 `filepath.Clean` 不变，于是拒绝夹具根并返回 `errGatewayCapability`。复现后确认仓库内实际生成了 `apps/agent/C:/temp`。

这解释了“Windows 与旧 Linux 环境通过、GitHub runner 失败”：取决于包目录是否可写，而非产品权限逻辑。

### 修复

`asciiTestRoot` 的盘符候选只在 `runtime.GOOS == "windows"` 时尝试；Unix 直接使用 `os.TempDir()`。生产侧私有根/owner/mode 检查未放宽。

### 验证

```sh
go test ./apps/agent -run "TestGatewayCapabilityRealNginxIsolated|TestActualWSSGatewayApplyReloadAndHTTPBytes" -count=1 -v
# --- PASS: TestGatewayCapabilityRealNginxIsolated (0.26s)
# --- PASS: TestActualWSSGatewayApplyReloadAndHTTPBytes (0.27s)

go test ./apps/agent -count=1
# ok  serverfleet.local/serverfleet/apps/agent  18.180s
```

检查点提交：`4be0f32`（`fix(agent): use absolute temp roots for gateway fixtures`）。

## 检查点 2：实际工件上传器与 Agent 能力注册（2026-11-03）

### 新增文件

| 文件 | 作用 |
|---|---|
| `apps/controller/server/gateway_artifact.go` | 实际 `GatewayArtifactUploader`：校验 task/node、绝对且干净路径、1..64KiB、重算 SHA-256，然后调用既有 `files.UploadTransport.PushFile`，不引入 SSH 或第二执行队列 |
| `apps/controller/server/gateway_artifact_test.go` | 真实字节落盘、摘要不符不写、空/相对/不干净路径、超限、空内容、坏摘要拒绝、缺 transport 关闭 |
| `apps/agent/gateway_registration.go` | Agent 侧网关能力注册：从私有 `Prefix` 推导 `conf/`、`artifacts/`、`nginx.conf`，缺配置或非私有根一律关闭 |
| `apps/agent/gateway_registration_test.go` | 两个 typed action 完整注册；空/相对/不干净/放宽权限/缺二进制全部拒绝 |

### 生命周期决定

现有 `gatewayTaskDispatcher.Dispatch` 只在统一任务仍 `queued`、审批已通过、计划 target revision 未变、节点当前授权仍有效时上传工件，并在上传后再次复查审批、target、撤权与取消；`tasks.CheckExecution` 的 `running` 要求没有被硬套到 queued 上传阶段，也没有放宽为“任意 queued 可上传”。已有 `TestGatewayTaskAwaitingApprovalNeverUploads`、`TestGatewayTaskRevokedAtDispatchStopsUpload`、`TestGatewayTaskTargetRevisionChangedBeforeUpload/AfterUpload`、`TestGatewayTaskCancelDuringUploadStopsAgent` 覆盖这些语义，本次未改其行为，只补上真实 Uploader 实现。

### 验证

```sh
go test ./apps/agent -run TestGatewayRegistration -count=1 -v      # PASS（含 6 个子用例）
go test ./apps/controller/server -run TestGatewayArtifactUploader -count=1 -v  # PASS（含 7 个子用例）
go vet ./apps/agent ./apps/controller/server                       # 无输出
go test ./apps/agent -count=1                                      # ok 18.280s
go test ./apps/controller/server -count=1                          # ok 0.604s
gofmt -l <新增文件>                                                # 空
```

### 尚未接线（需要中央/后续协调）

- `server.New` 仍默认使用 `LocalBridge`；把 `NewProductionPublishingBridge`、本 Uploader 与 Agent 能力配置接入默认程序组合是中央文件差异，登记在 `SHARED-CHANGES.md`，本任务不直接改。
- `apps/agent/runtime` 的 `-gateway-prefix` / `-nginx-bin` 参数与 `connect.go` 注册调用属 Agent 入口，需 AgentMore/中央接线；本任务已提供可调用的 `newGatewayRegistration`。
- `files.AgentFileChannel` 的生产实现（WSS 文件帧适配）仍缺失；当前 Uploader 通过注入的 `files.UploadTransport` 工作，本地组合用 `LocalStagingTransport` 验证真实字节，生产需可信程序注入真实通道。

### CI 复验（head `75c5fce`）

PR #2 触发 run 41526891590：

- `dependencies` job：success。
- `apps/agent` 包：PASS（20.457s）。
- `TestGatewayCapabilityRealNginxIsolated`：PASS（0.35s）。
- `TestActualWSSGatewayApplyReloadAndHTTPBytes`：PASS（0.27s）。

第一次 PR run（41524264749，head `f00ab16`）在路径修复后暴露第二个真实失败：`EXECUTION_RESULT_UNKNOWN`。根因是夹具启动 nginx master 后立即 apply，慢速 runner 上 `nginx -s reload` 与 master 启动竞态，capability 正确返回 unknown 补偿结果。`75c5fce` 在两个真实 nginx 夹具中等待 pid 文件后再 apply；产品侧 unknown 语义与私有根检查未放宽，本地连续 3 轮 PASS。

`foundation` job 仍为 failure，剩余原因均不在本任务写权限：

1. `tests/integration/v09/platform`：`TestR2HTTPUnifiedWorkerTwoActualAgentsMultiFileAndUnknown`、`TestR3ActualProductAgentHTTPApprovedSignedInstallAndCompatibleRollback`（CX/AgentMore）。
2. 契约检查：`no-unresolved-refs`、`operation-operationId-unique`、`path-parameters-defined`（OpenAPI/Redocly，契约负责人）。

## 尚未完成（保持 OPEN）

- 整体 Foundation CI 仍为 FAIL：`tests/integration/v09/platform` 的三项（AgentMore/CX 范围）不在本任务写权限内，本任务不宣称 CI 全绿。
- Node 24.22.1 / npm 11 安装与 `npm ci`、`tools:install`、`check`、`check:security` 尚未执行。
- 网关整链（公开 HTTP→审批→上传→WSS→nginx reload→HTTP 字节→持久终态）、ACME/cleanup 产品组合、供应商独立项仍在推进。