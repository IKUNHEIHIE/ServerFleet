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

`foundation` job 仍为 failure。**真实失败只有一项**：`tests/integration/v09/platform` 的
`TestR2HTTPUnifiedWorkerTwoActualAgentsMultiFileAndUnknown`（缺预构建实际 Agent 二进制）与
`TestR3ActualProductAgentHTTPApprovedSignedInstallAndCompatibleRollback`（缺真实 release 构建产物）。
基线 CI #4（job 125266284154）已同样失败，非本 PR 回归。

**证据更正**：此前把 `no-unresolved-refs`、`operation-operationId-unique`、
`path-parameters-defined` 记为“额外契约失败”不准确。它们来自 `reports/g2r03-negative/`
的故意非法样本，是 Redocly 写入 stderr 的预期诊断，GitHub Actions 仅因 stderr 自动标红；
随后 r03 套件继续执行并明确通过（`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml`
valid，8 项静态契约断言全 ✔）。因此它们不是 `foundation` 失败原因，也不影响退出码。
真正的 `exit 1` 来自 `go-tests failed` 与上述两项 platform 测试。

## 检查点 3（R1）：真实 WSS 任务/计划/执行绑定（2026-11-03）

### 新增/修改

| 文件 | 作用 |
|---|---|
| `apps/controller/server/gateway_wss.go` | 真实 `files.AgentFileChannel` 适配 `transport.FileClient`：构造合法 `files.transfer.v1` offer、相对路径映射、`WithChannelScope`（task/execution/transfer/plan）与逐块 `WithChannelAuthorizer` |
| `apps/controller/server/gateway_binding.go` | 持久 `gateway_artifact_bindings` 绑定、候选迁移、`NewGatewayArtifactGate`（按持久 execution 判定归属并校验 offer/节点/参数/帧 ID） |
| `apps/controller/server/gateway_worker.go` | dispatcher 优先 `UploadBound`，构造 execution/transfer/plan binding；新增 `CheckArtifactStage`（queued + 审批 + target revision + 节点授权 + 取消） |
| `apps/controller/server/gateway_wss_test.go` | 真实 `ChannelTransport` + 持久绑定的 6 个用例 |
| `docs/.../cloud-r1/CENTRAL-PATCH-R1.md` | 中央/Agent 入口可应用补丁、可信配置与组合验证步骤 |

`tasks.CheckExecution`（running）未放宽；queued 上传使用等价受审阶段门。未绑定调用
`Upload` 显式返回 `ErrTaskBindingRequired`，不回退本地 staging。

### 验证

```sh
go test ./apps/controller/server -run "TestGatewayChannelUploader|TestGatewayFileOffer|TestGatewayArtifactGate" -count=1 -v
# 6 个用例 PASS

go test ./apps/controller/server -count=1     # ok 0.836s
go test ./tests/architecture -count=1         # ok 0.103s
go vet ./apps/controller/server               # 无输出
gofmt -l <新文件>                              # 空
```

### Node/npm（已固定并实测）

- 安装 Node `v24.22.1`、npm `11.19.1` 到 `/home/service/.tools/node`。
- `npm ci`：成功，added 127 packages（46s）。
- `npm run tools:install`：成功安装 protoc-gen-go / staticcheck / actionlint / govulncheck。
- `npm run check:security`：**PASS**（`found npm-audit: 0 vulnerabilities`；govulncheck `No vulnerabilities found`）。
- `npm run check:go`：**FAIL**，唯一失败为 `tests/integration/v09/platform` 两项缺预构建产物；
  其余包（含 `apps/controller/server`、`apps/agent`、`tests/architecture`）通过。
  `check:web`/`check` 全量未执行 → 保留 **BLOCKED/UNTESTED**，不记为通过。

## 检查点 4（R2）：三项 P1 返修（2026-11-03）

### R2-01 子执行身份独立

- 新增 `gatewayArtifactExecutionID(taskID,nodeID,planHash,gatewayID)`，稳定产出 `gwa-...`；
  与 `agentLinkBridge` 的 `executionIDFor`（`exec-...`）分离，Agent ledger 不会把文件 offer
  与 Nginx apply 当作同一执行。
- 审查方提供的 `TestReviewerGatewayArtifactAndApplyNeedDistinctExecutions` 已保存为
  `apps/controller/server/reviewer_collision_test.go`，修复后 **PASS**（此前 FAIL）。

### R2-02 子执行 Ack/Started/Finished 路由

- `gateway_child.go`：`GatewayArtifactObserver` 按持久 `execution_id` 认领子事件，更新
  `state/result`，**不调用 `tasks.RecordResult`**，因此上传成功不会提前完成父发布任务。
- 状态机：Ack→accepted、Started→started、Finished→finished；重复事件幂等，终态冲突拒绝，
  Finished 早于 Started 记 `EXECUTION_RESULT_UNKNOWN`，节点不符拒绝，重连（同节点新连接）接受。
- 中央 `agent_links.go` 的三行插入在 `CENTRAL-PATCH-R2.md` 给出真实 diff，本任务未直接改中央文件。

### R2-03 每帧校验冻结计划/参数/绑定

- `CheckArtifactStage` 与新增 `CheckArtifactChild` 均校验：
  `task.PlanHash == binding.PlanDigest`、`operation/artifact_path/site_path/config_sha256`
  与绑定一致、当前审批（含 revision 与 TTL）仍有效、plan target revision 与节点授权未变。
- 任务 `Revise` 并重新审批后，旧 `PlanDigest` 的绑定被拒绝（新增用例覆盖）。

### 验证

```sh
go test ./apps/controller/server -run "TestGatewayArtifact|TestGatewayChild|TestReviewer|TestGatewayChannel|TestGatewayFileOffer" -count=1
# PASS（含 reviewer 复现用例与 10 个 R2 用例）

go test ./apps/controller/server -count=1     # ok 1.017s
go test ./tests/architecture -count=1         # ok 0.096s
go vet ./apps/controller/server               # 无输出
gofmt -l <新文件>                              # 空
```

### 仍未完成

- 跨进程完整组合（Controller 子执行 → 真实 Agent ledger → Nginx → HTTP 字节 → 父终态）
  需要应用 `CENTRAL-PATCH-R2.md` 后才能在启动组合中验证；当前未伪造该证据。
  Agent 侧真实 WSS/Nginx/HTTP 仍由 `TestActualWSSGatewayApplyReloadAndHTTPBytes` 覆盖。

## 尚未完成（保持 OPEN）

- 整体 Foundation CI 仍 FAIL，真实原因仅 `tests/integration/v09/platform` 两项缺预构建产物；本任务不宣称 CI 全绿。
- 中央接线（迁移版本、`server.New`、Agent runtime 参数、observer/gate 组合）需主控应用 `CENTRAL-PATCH-R2.md`。
- 公开 HTTP→审批→工件→真实 Agent WSS→nginx→HTTP 字节→持久终态→逆序补偿的**进程级组合证据**尚未跑通（需中央接线后可启动组合）。
- ACME/cleanup 产品组合与供应商未知结果独立项仍未完成。
- `check:web`、`npm run check` 全量未执行。