# V09-CLOUD-AM-01 · agentmore-01 · 第一检查点

日期：2026-11-03（Asia/Shanghai）。会话 agentmore-01；分支 `feature/cloud-platform-am01`；起点 main `1ca79072232bfdcee2b9f4582ac020d4430d0c2b`（与既定基线一致，未变）。

## 交付内容

### 第一部分：修复 foundation CI 三个真实产品构建测试前置条件
- CI run 41441915813 中 SKIP/FAIL 的三个测试（R2 双 Agent 统一 worker、R3 可信文件部署 CLI、R3 真实产品更新回滚）均因缺少显式预构建产品二进制。
- 新增 `tests/integration/v09/platform/productbuild_test.go`：未提供 `SF_*_BINARY` 环境变量时，测试运行期从仓库正式代码构建真实产品二进制（`go build -trimpath`，controller/agent/agent@1.0.0/agent@1.0.1，构建命令+SHA-256 记录于测试日志）；显式环境变量覆盖路径完整保留，未弱化任何断言。
- 本地实测（含 `-race`）：R3 CLI 13.08s PASS、R2 91.19s PASS、R3 产品更新 24.82s PASS。

### 第二部分：上传统一任务化（WAVE6-R4 CX-05 首个可运行检查点）
- `CompleteUpload` 校验摘要后不再直接调 transport：经 `UploadTaskSubmitter.SubmitUpload` 提交统一任务（复用已注册 `files.transfer` 动作 + `kind=upload` 计划分支），HTTP 返回 202+taskId；未配置任务提交器时保留旧直推路径（隔离组合兼容）。
- 任务执行：`liveUpload` 每帧复核节点在线/当前授权/上传绑定；复用 Gate 逐帧授权状态机；staging 流式重算 SHA-256；终态仅由 worker 经 `ApplyUploadTaskUpdate` 落库（completed/failed/cancelled；写前取消/拒绝/校验失败 → 已知终态，绑定已 opened 后不确定 → failed(agent_unknown)+任务 UNKNOWN，不自动重放）。
- 取消：统一任务取消驱动 worker 分类；dispatching 上传的 AbortUpload 转发任务取消。
- 新增迁移候选 `ds/0041_file_upload_task`（file_uploads.task_id 列+索引；lexicographic 排序紧随 ds/0040 建表之后；正式编号由总控定夺）。
- 端到端测试 `TestAM01UploadHTTPUnifiedTaskActualAgentShaVerified`（26.87s PASS，含 -race）：真实 HTTP 上传字节 → 202 → 统一任务 → 真实 Agent 落盘 SHA-256 核对 + 取消 + UNKNOWN 重启围栏。
- 审计：复用既有白名单动作（files.upload_complete/upload_abort、tasks.submit 自带任务关联审计），未新增审计动作，无中央 server.go 改动。

## 跨边界机械差异（供主控复核）
`shared-test-migration-count.diff`：新增候选迁移使组合迁移 25→26（backup/restore 场景 13→14），四处共享测试的计数断言随之机械更新，测试语义未改：
- `tests/integration/v09/platform/recovery_guard_r2_test.go`（本包在允许范围内）
- `tests/integration/v09/wave6_r1_migration_test.go`
- `tests/integration/v09/controller_wave2_test.go`
- `tests/integration/v09/services/backup_restore_test.go`

## 验证记录
- `go build ./...`、`go vet ./...` PASS；internal 全部 23 包 + apps 单元测试 PASS。
- 全量 `npm run check`（CI=1，含 -race go test ./...、web 348 用例、buf/redocly/git 策略）执行至 go-tests 通过后因沙箱环境重启中断；go 套件（go-format/mod verify/构建/go-tests-race）此前已实测通过，web 348 PASS 与 git 策略 PASS 亦已实测；最终一次性全绿复验见 PR 描述附注。
- `check:security`：npm audit 通过；govulncheck v1.9.0 首次安装后复跑。

## 风险与剩余
- 上传 64MiB 通道上限（transport.FileMaxBytes）在任务化路径生效；>64MiB 的 CompleteUpload 返回错误（既有 documented 限制，本次不扩容）。
- 跨主控重启续传（持久 offset/绑定/校验和）为 WAVE6-R4 后续子项，本检查点仅提供 UNKNOWN 围栏不重放。
- 中央 server.go / AuditActions 未改动；未来如需独立 `files.upload_dispatch` 审计动作，由总控共享 PR 加入目录。
