# V09-WAVE5-REVIEW · Wave 5 本地全栈收官、Wave 4 新接口前后端打通与双进程 CLI 连接审查报告

- **任务 ID**：`V09-WAVE5-REVIEW`
- **集成会话**：`agy-review-01`
- **集成工作目录 / 分支**：`D:\admin\desktop\codex\myproject\ks\.worktrees\integration--v09-wave1-review`（`integration/v09-wave5-review`）
- **起始 `main` 基线**：`3adf891f3155b6452f06f25df53fd20f553742ae`（Wave 5 派发基线）
- **被审查三线提交**：
  - `V09-5D-CX`（`codex-05`）：代码 `3e9dd49e3d518f716fd0cc4e99502eb88db97693` / 分支 HEAD `cad136b158c9bf93dccf9a849a41f5cf900cb4f0`（`feature/v09-platform-cx02`）
  - `V09-5D-DS`（`deepseek-05`）：代码 `be70f9df64eddb4f894b2893106fc4563fb9bf5a` / 分支 HEAD `df8eebd1915a7f070322076746c9c44557a9e3a3`（`feature/v09-services-ds02`）
  - `V09-5D-GM`（`gemini-05`）：`639eff20526a12acee8900abcac62a5d4376b732`（`feature/v09-console-gm02`）
- **总控组合接线提交**：`aef21ae9fdd2c9c70775008ecf1b51949e5973ea`
- **结论**：**PASS（三线 Wave 5 独立审查通过、零跨线文件冲突，总控完成 `NetProber` 与 `LocalStagingTransport` 默认组合接线、全量门禁、WSL Ubuntu 双进程 CLI 闭环复验与 70 步真实 Go Controller 浏览器端到端验收全部通过，合入 `main`）**

---

## 1. 三线独立审查摘要

### 1.1 `V09-5D-CX`（Agent CLI `-connect-once` 受控单次 WSS+mTLS 连接、心跳采样与双进程任务执行闭环，`cad136b`）
1. **范围与边界检查**：
   - 4 个提交、16 个文件变更，全部位于 `apps/agent/`、`internal/transport/`、`tests/integration/v09/platform/` 与 `docs/development/sprints/v09-five-day/progress/codex/`，严守模块边界，未触碰 DS 业务包或 `apps/web`。
2. **核心实现核查**：
   - **CX-W5-01（`apps/agent` CLI `-connect-once` 受控单次连接、指标采样与任务执行）**：`apps/agent/runtime/runtime.go`、`apps/agent/connect.go`、`apps/agent/metrics.go`、`apps/agent/session.go` 与 `apps/agent/executor.go` 实现 `-connect-once`（配合 `-state-dir`、`-controller-url`、`-root-cert`、可选 `-node-name` / `-wait-task-ms`），无参数启动 `go run ./apps/agent` 保持原 M0-P1 骨架输出不变；`internal/transport/websocket.go` 补齐有界 `Ping` 并在 hijack 后清理 HTTP 请求遗留 deadline；`apps/agent/executor.go` 对齐非 AI 标准 Web/工作流任务的 `scope_revision == 0` 语义（仍强制校验动作白名单、普通权限与空插件）。
   - **CX-W5-02（真实编译 `controller` + `agent` 双进程入网、WSS+mTLS 上线与任务执行端到端验证）**：`tests/integration/v09/platform/controller_cli_pki_test.go` 新增 `TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`，完整覆盖真实编译双进程 `-enroll-token` 提交 CSR（`pending`）→ 管理员批准 → 再次运行 `-enroll-token` 落盘签发证书（`issued`）→ `-status` 自检 → 授予 `nodes.read`/`tasks.submit`/`tasks.command`/`tasks.read` → `-connect-once` 建立 WSS+mTLS 连接并上报真实心跳/采样 → 提交 `tasks.command` 经 WSS 执行完成（`completed` / `succeeded`）→ 审计落盘 → 断开后节点回到离线。Windows 下按 Linux-only 策略诚实 `SKIP`，WSL Ubuntu 24.04 amd64 连续 3 次及总控在集成分支独立复验（`2.80s`）均 `PASS`。
   - **CX-W5-03（进度与证据文档更新）**：更新 `docs/development/sprints/v09-five-day/progress/codex/STATUS.md` 与 `AC-EVIDENCE-WAVE4.md`。

### 1.2 `V09-5D-DS`（防 DNS Rebinding 真实 `NetProber` 与本地受控 `LocalStagingTransport`，`df8eebd`）
1. **范围与边界检查**：
   - 7 个提交、9 个文件变更，全部位于 `internal/monitoring/`、`internal/files/`、`tests/integration/v09/services/` 与 `docs/development/sprints/v09-five-day/progress/deepseek/`，严守不直接修改 `apps/controller` 的派发约束，并通过 `HANDOFF-WAVE5.md` 交接主控接线参数。
2. **核心实现核查**：
   - **DS-W5-01（`internal/monitoring` 受二次 SSRF 复核保护的标准库 `NetProber`）**：`internal/monitoring/prober.go` 与 `prober_test.go` 实现 `monitoring.NewNetProber`，支持真实 HTTP/HTTPS、TCP、TLS（强制 TLS 1.2+ 证书链校验）探测，禁用环境变量代理以防绕过，并在自定义 `DialContext` / `dialTCP` 建立连接前对每次 DNS 解析出的全部 IP 重新执行 SSRF 策略校验（阻断 DNS Rebinding 指向回环/私网/链路本地/云元数据地址；云元数据地址即使配置白名单亦无条件拒绝），返回脱敏错误分类码而绝不回显响应体或响应头。
   - **DS-W5-02（`internal/files` 受控本地目录 `LocalStagingTransport` 与集成测试）**：`internal/files/local_transport.go` 与 `local_transport_test.go` 实现同时满足 `UploadTransport` 与 `DownloadTransport` 的 `files.NewLocalStagingTransport(rootDir)`，逐级 `os.Lstat` 拒绝符号链接目录/目标、Windows 保留设备名与路径逃逸，通过同目录临时文件 + 流式字节数与 SHA-256 复核 + `Sync` + 原子 `Rename` 落盘；`tests/integration/v09/services/wave4_http_test.go` 新增 `TestWave5RealProberAndLocalStagingTransport` 端到端验证。
   - **DS-W5-03（交接说明与证据文档更新）**：提交 `HANDOFF-WAVE5.md`、更新 `STATUS.md` 与 `AC-EVIDENCE-WAVE4.md`。

### 1.3 `V09-5D-GM`（Wave 4 四组后端新接口前端全量打通与 70 步真后端浏览器验收，`639eff2`）
1. **范围与边界检查**：
   - 1 个提交、42 个文件变更，全部位于 `apps/web/`、`tests/browser/v09/` 与 `docs/development/sprints/v09-five-day/progress/gemini/`，未修改任何 Go 文件或公共配置。
2. **核心实现核查**：
   - **GM-W5-01（工作流、文件字节流上传、监控历史与立即探测、发布执行与回滚前端打通）**：
     - `apps/web/src/features/workflows/workflows-page.tsx` 移除原静态 `BLOCKED` 占位，通过 `workflowsApi`（`endpoints-workflows.ts`）完整接入工作流列表、创建、带 `expectedRevision` 启停切换、手动触发运行（展示受控生成的 `taskId` 并可跳转任务中心）及运行记录详情弹窗，并在 `backend-aliases.ts` 中对齐 `workflows.read` / `workflows.write` 权限映射；
     - `apps/web/src/lib/api/client.ts` 新增 `requestRaw` 二进制流请求通道，`apps/web/src/features/files/files-page.tsx` 实现创建上传会话 → `PUT .../content?offset=0` 真实字节流上传 → `POST .../complete` 触发服务端字节数与 SHA-256 校验；
     - `apps/web/src/features/monitoring/monitoring-page.tsx` 将“检查历史”与“立即探测”按钮对接真实 `GET .../history` 与 `POST .../probe`；
     - `apps/web/src/features/publishing/publishing-page.tsx` 新增“执行发布”（`POST /api/v1/publications/{id}/run`）与阶段级“回滚此阶段”（`POST /api/v1/publications/{id}/rollback`）；
     - 同时修复 `node-add-page.test.tsx` 异步按钮等待，Web 单测从 36 文件 / 316 项扩展至 **37 文件 / 329 项全部 PASS**。
   - **GM-W5-02（真实 Go Controller 浏览器端到端验收扩展至 70 步）**：`tests/browser/v09/run-live-controller-acceptance.mjs` 从 58 步扩展至 **70 步（70/70 PASS）**，新增 6 张真后端浏览器截图（`live-33..38-*.png`）。
   - **GM-W5-03（进度与证据文档更新）**：更新 `STATUS.md`、`ACCEPTANCE.md` 与 `AC-EVIDENCE-WAVE4.md`。

---

## 2. 总控组合接线与协调改动（`aef21ae`）

1. **`apps/controller/server/server.go` 默认注入 `NetProber` 与 `LocalStagingTransport`**：
   - 当 `cfg.UploadTransport == nil` 时，默认构造 `files.NewLocalStagingTransport(filepath.Join(stagingDir, "nodes"))` 并暴露于 `s.FilesTransport`，使真实 Controller 的文件上传完成端点（`POST /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/complete`）在完成流式 SHA-256 校验后原子落盘至受控节点暂存目录并记录 `files.upload_complete` 审计。
   - 当 `cfg.MonitoringProber == nil` 时，默认构造 `monitoring.NewNetProber(monitoring.NetProberConfig{AllowedHosts: cfg.MonitoringAllowedHosts})`，使真实 Controller 的可用性监控立即探测端点（`POST /api/v1/availability-monitors/{monitorId}/probe`）执行受二次 SSRF 保护的真实网络探测、持久化检查样本至 `monitor_checks` 并记录 `monitoring.check` 审计。
   - `/healthz` 阶段标识更新为 `"v0.9-wave5"`。
2. **组合集成测试与 70 步真后端浏览器验收对齐**：
   - 在 `tests/integration/v09/controller_wave2_test.go` 中新增对默认 `FilesTransport`（`POST .../uploads` → `PUT .../content?offset=0` → `POST .../complete` → `200 OK` + `PullFile` 字节一致验证）与默认 `NetProber`（`POST .../probe` → `200 OK` + `GET .../history` 样本落库）的端到端断言。
   - 在 `tests/browser/v09/run-live-controller-acceptance.mjs` 的 Step 64、Step 65 与 Step 68 中对齐已注入 `NetProber` 与 `LocalStagingTransport` 的真实 200 成功路径及 `files.upload_complete` / `monitoring.check` 审计记录，并刷新对应浏览器截图（`live-35`、`live-36`、`live-38`）。

---

## 3. 全量门禁、安全、WSL 双进程与浏览器端到端验证结果

| 检查项 | 命令 | 结果 |
|---|---|---|
| 原始基线与字节精确契约 | `node scripts/verify-source.mjs` | **PASS**（69 份原始文件 + 3 份字节不变 OpenAPI 候选完好） |
| 架构与 Web 边界检查 | `node scripts/web-boundaries.mjs` + `go test ./tests/architecture` | **PASS** |
| 契约检查（根 + `g2-r02` + `g2-r03`） | `node scripts/check.mjs contracts` | **PASS** |
| Go 格式 / 依赖 / 静态分析 | `gofmt -l` / `go mod verify` / `go mod tidy -diff` / `go vet ./...` / `staticcheck ./...` | **PASS** |
| Go 全量单测与集成测试（Windows） | `go test ./...` | **PASS**（636 顶层测试 + 1,134 子测试 = 1,770 PASS，0 FAIL） |
| Go 双进程 CLI + WSS+mTLS + 任务闭环（WSL Ubuntu 24.04 amd64） | `TestWave5ControllerAndAgentCLIFullLoopWSSAndTask` | **PASS**（`2.80s` 完整跑通 `controller` + `agent` 双进程入网、WSS+mTLS 心跳/指标与 `tasks.command` 执行） |
| Web 类型检查与单测 | `npm run typecheck --workspace @serverfleet/web` + `npm test --workspace @serverfleet/web` | **PASS**（37 测试文件 / 329 项测试全部 PASS） |
| Web 生产构建 | `npm run build --workspace @serverfleet/web` | **PASS** |
| 安全漏洞扫描 | `npm audit --audit-level=high` + `govulncheck ./...` | **PASS**（0 漏洞） |
| 浏览器验收（Stub 契约与退化场景） | `node tests/browser/v09/run-acceptance.mjs` | **PASS**（16/16 步全部通过） |
| 浏览器验收（真实 Go Controller + SQLite WAL + HTTPS） | `node tests/browser/v09/run-live-controller-acceptance.mjs` | **PASS**（70/70 步全部通过） |

---

## 4. 剩余边界与停止说明

1. **本地全栈与契约边界**：Wave 1 至 Wave 5 已完成全部 13 个业务与平台域的 SQLite WAL 持久化、REST/SSE 路由组合、默认 `NetProber` / `LocalStagingTransport` / `LocalBridge` 接线、Controller + Agent 双进程 CLI 自包含 PKI 入网与 `-connect-once` WSS+mTLS 任务执行闭环、前端全部功能页面真实 API 接入，以及 **70 步真实 Go Controller 无头浏览器端到端验收**。
2. **外部环境与待冻结项诚实保留**：真实外部生产/VPS/云资源（Cloudflare 线上 API、公网 ACME CA、外部 SMTP/QQ Bot 网关、外部 AI 模型 BYOK、跨机 Debian/Ubuntu amd64/arm64 物理节点 systemd 安装与 cgroup v2、Xray 官方二进制安全扫描阻断项）按 `AGENTS.md` 规定未在本地伪造；`api/openapi/*.yaml` 保持字节不变，`G2`、`D01–D07`、`P1-C10` 及 WASM/Eino ADK 运行时选型保持 `OPEN`，等待项目负责人最终审阅与后续指令。
