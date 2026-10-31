# V09-WAVE4-REVIEW · Wave 4 本地契约闭环补齐、全模块真后端浏览器 E2E 扩展与 AC-01~24 证据盘点审查报告

- **任务 ID**：`V09-WAVE4-REVIEW`
- **集成会话**：`agy-review-01`
- **集成工作目录 / 分支**：`D:\admin\desktop\codex\myproject\ks\.worktrees\integration--v09-wave1-review`（`integration/v09-wave4-review`）
- **起始 `main` 基线**：`0f9a75e416a82a9589a29d4d9cf8a52bf0266066`（Wave 4 派发基线）
- **被审查三线提交**：
  - `V09-5D-CX`（`codex-04`）：代码 `217fa82bf853a6e7782e78fa0d1274e794f9739c` / 分支 HEAD `b02b15705147564b161973e17e25dfe43c80bd1b`（`feature/v09-platform-cx02`）
  - `V09-5D-DS`（`deepseek-04`）：代码 `22fe18373528098cf33ef381cf45cb2b0bf06253` / 分支 HEAD `122506d50978d758ced04e6cbb2f8e5cb3cab52a`（`feature/v09-services-ds02`）
  - `V09-5D-GM`（`gemini-04`）：`9b32a5b6c0b1458c2a8614594ea23d7b94caf03c`（`feature/v09-console-gm02`）
- **总控组合接线提交**：`0d762f0252270bace555f01f4ee5a39737c67a32`
- **结论**：**PASS（三线 Wave 4 独立审查通过、零跨线文件冲突，总控完成发布与文件传输桥接端口接线、全量门禁与 58 步真实 Go Controller 浏览器端到端验收全部通过，合入 `main`）**

---

## 1. 三线独立审查摘要

### 1.1 `V09-5D-CX`（工作流 HTTP、自包含 PKI 双进程 CLI 连接与平台 AC-01~24 / PoC 盘点，`b02b157`）
1. **范围与边界检查**：
   - 4 个提交、19 个文件变更，全部位于 `internal/workflows/`、`apps/controller/{bootstrap,localpki,main.go,server/}`、`apps/agent/`、`tests/integration/v09/platform/` 与 `docs/development/sprints/v09-five-day/progress/codex/`，未触碰 DS 业务包或 `apps/web`。
2. **核心实现核查**：
   - **CX-W4-01（自动化工作流 HTTP API 与主控组合接线）**：`internal/workflows/{http,read}.go` 与 `apps/controller/server/workflows.go` 实现 `GET/POST /api/v1/workflows`、`GET/PATCH /api/v1/workflows/{workflowId}`、`POST /api/v1/workflows/{workflowId}/runs`；通过 `tasks.Service.Submit` 执行节点动作、通过 `notifications.Service` 发送通知动作，只读角色禁止触发修改，所有管理与运行均写入权威审计。
   - **CX-W4-02（`apps/controller` 与 `apps/agent` CLI 自包含本地 PKI 连接模式）**：`apps/controller/localpki` 与 `apps/controller/bootstrap` 在指定 `-db` 时自动于 `<db-dir>/pki` 生成/加载本地 ECDSA P-256 Agent CA，并在真实进程中完整挂载 `/api/v1/enrollment-tokens`、`/api/v1/enrollments`、`/api/v1/agent/enroll` 与 `/api/v1/agent/link`；`apps/agent/runtime` 新增显式 CLI 标志（`-status`、`-enroll-token` 等）并保持无参默认骨架输出不变；`tests/integration/v09/platform/controller_cli_pki_test.go` 验证真实编译的 `controller` 与 `agent` 双进程完成入网、审批与 WSS+mTLS 连接。
   - **CX-W4-03（平台侧 AC-01~24 与 PoC-01/02/03/04/06/08 证据矩阵）**：提交 `docs/development/sprints/v09-five-day/progress/codex/AC-EVIDENCE-WAVE4.md`。

### 1.2 `V09-5D-DS`（监控历史与真实探测 HTTP、文件字节流上传校验与发布阶段编排适配，`122506d`）
1. **范围与边界检查**：
   - 6 个提交、16 个文件变更，全部位于 `internal/{monitoring,files,publishing}`、`tests/integration/v09/services/` 与 `docs/development/sprints/v09-five-day/progress/deepseek/`，严守不直接修改 `apps/controller` 的派发约束，并通过 `HANDOFF-WAVE4.md` 交接主控接线参数。
2. **核心实现核查**：
   - **DS-W4-01（可用性监控历史查询与真实本地 HTTP/TCP/TLS 探测端点）**：`internal/monitoring/{http,repository,service}.go` 实现 `GET /api/v1/monitoring/availability-monitors/{monitorId}/history`（基于 `monitor_checks` 真实分页查询，消除原 `501` 占位）与 `POST /api/v1/monitoring/availability-monitors/{monitorId}/probe`（受 SSRF 门控保护的真实 HTTP/TCP/TLS 探测并落库记录）。
   - **DS-W4-02（文件互传真实字节流上传通道与 SHA-256 完整性校验）**：`internal/files/{http,upload,service,repository}.go` 实现 `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content`，并在 `complete` 阶段强制校验已接收字节数与真实流式 SHA-256 摘要，大小或摘要不匹配返回 `409 Conflict` 且拒绝落盘。
   - **DS-W4-03（发布向导本地阶段编排桥接与 `run`/`rollback` HTTP 端点）**：`internal/publishing/{bridge,http,service}.go` 新增 `publishing.NewLocalBridge` 及 `POST /api/v1/publishing/publications/{publicationId}/run`、`/rollback`，真实持久化三阶段执行明细（`RunDetail`）与逆序回滚明细（`RollbackDetail`），未配置外部自动发布器时诚实停留在 `manual_required` / `publisher_unavailable`。
   - **DS-W4-04（数据业务侧 AC-01~24 与 PoC-04/05/07/08 证据矩阵）**：提交 `docs/development/sprints/v09-five-day/progress/deepseek/AC-EVIDENCE-WAVE4.md`。

### 1.3 `V09-5D-GM`（Wave 3 新增路由全模块真后端浏览器 E2E 扩展、契约细节打磨与前端 AC 证据盘点，`9b32a5b`）
1. **范围与边界检查**：
   - 1 个提交、44 个文件变更，全部位于 `apps/web/`、`tests/browser/v09/` 与 `docs/development/sprints/v09-five-day/progress/gemini/`，未修改任何 Go 文件或公共配置。
2. **核心实现核查**：
   - **GM-W4-01（真实 Go Controller 浏览器端到端验收扩展至 58 步）**：`tests/browser/v09/run-live-controller-acceptance.mjs` 从 Wave 3 的 32 步扩展至 **58 步（58/58 PASS）**，新增 17 张真后端浏览器截图（`live-16..32-*.png`），覆盖节点详情配额编辑（`PUT/GET /api/v1/nodes/node-alpha/traffic-quota`）、可用性监控创建与流量风险聚合、内部证书分发与吊销、发布向导预检与创建、AI 会话默认零资源授权/显式赋权/审批策略切换/一次性审批签发与受控执行（`202 Accepted`）、插件中心目录刷新与启用门控、更新中心检查与计划、在线 SQLite `VACUUM INTO` 备份校验与灾备恢复预检，以及全链路权威审计验证。
   - **GM-W4-02（前后端契约与页面体验打磨）**：`apps/web/src/lib/api/compat.ts` 与相关页面组件补齐配额首次未配置（`404 quota_not_found`）时的可编辑表单降级、证书/发布/AI/插件/更新/恢复响应兼容映射，Web 单测从 306 项增至 **316 项（36 文件全部 PASS）**。
   - **GM-W4-03（前端与浏览器侧 AC-01~24 证据矩阵）**：提交 `docs/development/sprints/v09-five-day/progress/gemini/AC-EVIDENCE-WAVE4.md`。

---

## 2. 总控组合接线与协调改动（`0d762f0`）

1. **`apps/controller/server/server.go` 桥接端口接线**：
   - 在 `server.Config` 中暴露可选 `PublishingBridge publishing.LocalBridgeConfig` 与 `UploadTransport files.UploadTransport`。
   - 在 `server.New` 中使用 `publishing.NewLocalBridge(cfg.PublishingBridge)` 替换原 3 个局部 `failClosed*Publisher` 结构体（未注入外部发布器时保持相同失败关闭语义，注入后支持真实三阶段发布与逆序回滚），并将 `cfg.UploadTransport` 传入 `files.NewService`。

---

## 3. 全量门禁、安全与浏览器端到端验证结果

| 检查项 | 命令 | 结果 |
|---|---|---|
| 原始基线与字节精确契约 | `node scripts/verify-source.mjs` | **PASS**（69 份原始文件 + 3 份字节不变 OpenAPI 候选完好） |
| 架构与 Web 边界检查 | `node scripts/web-boundaries.mjs` + `go test ./tests/architecture` | **PASS** |
| 契约检查（根 + `g2-r02` + `g2-r03`） | `node scripts/check.mjs contracts` | **PASS** |
| Go 格式 / 依赖 / 静态分析 | `gofmt -l` / `go mod verify` / `go mod tidy -diff` / `go vet ./...` / `staticcheck ./...` | **PASS** |
| Go 全量单测与集成测试 | `go test ./...` | **PASS**（602 顶层测试 + 1,132 子测试 = 1,734 PASS，0 FAIL） |
| Web 类型检查与单测 | `npm run typecheck --workspace @serverfleet/web` + `npm test --workspace @serverfleet/web` | **PASS**（36 测试文件 / 316 项测试全部 PASS） |
| Web 生产构建 | `npm run build --workspace @serverfleet/web` | **PASS** |
| 安全漏洞扫描 | `npm audit --audit-level=high` + `govulncheck ./...` | **PASS**（0 漏洞） |
| 浏览器验收（Stub 契约与退化场景） | `node tests/browser/v09/run-acceptance.mjs` | **PASS**（16/16 步全部通过） |
| 浏览器验收（真实 Go Controller + SQLite WAL + HTTPS） | `node tests/browser/v09/run-live-controller-acceptance.mjs` | **PASS**（58/58 步全部通过） |

---

## 4. AC-01 ~ AC-24 与 PoC-01 ~ PoC-08 综合盘点摘要

结合三线提交的 `AC-EVIDENCE-WAVE4.md`（[`codex`](../sprints/v09-five-day/progress/codex/AC-EVIDENCE-WAVE4.md)、[`deepseek`](../sprints/v09-five-day/progress/deepseek/AC-EVIDENCE-WAVE4.md)、[`gemini`](../sprints/v09-five-day/progress/gemini/AC-EVIDENCE-WAVE4.md)）：

1. **本地 Go + SQLite WAL + HTTPS + React 浏览器全栈已验证范围（`PASS_LOCAL`）**：
   - 24 项 AC 的本地数据库持久化、REST/SSE 契约、RBAC 默认零授权与资源级赋权、CSRF 恢复、双人异人审批、Agent 入网与 WSS+mTLS Protobuf 回路、手动分组与服务组 `expectedRevision` 并发保护、工作流编排、监控历史与本地 HTTP/TCP/TLS 探测（含 SSRF 拦截）、文件字节流上传与 SHA-256 校验、内部证书分发与 `keyRef` 不泄密、发布向导三阶段编排与逆序回滚、AI 默认零资源/服务组快照/三级审批（`full_access` 仍强制真人管理员逐次审批任意 root Shell）、插件中心哈希与权限门控、更新中心备份前置门控、SQLite `VACUUM INTO` 在线备份与旧主控隔离恢复、凭据写后即焚、流量配额风险评估，以及 58 步真实 Go Controller 无头浏览器端到端回归均已通过本地自动化验证。
2. **受外部环境或 G2 契约冻结约束的边界项（诚实保留 `PARTIAL_LOCAL` / `BLOCKED_EXTERNAL`）**：
   - **真实 Linux systemd / cgroup v2 / 多节点跨机数据面与真实 Xray / Nginx 二进制安装执行**（涉及 `AC-03`、`AC-06`、`AC-13`、`AC-21`~`AC-24` 及 `POC-03`、`POC-04`、`POC-05`）：本地在 Windows 环境下通过真实本地回环与进程级测试验证；跨机真实 Debian/Ubuntu amd64/arm64 root/systemd 执行按规则等待外部隔离 Linux 环境授权。
   - **外部第三方线上凭据与硬件密钥**（涉及 `AC-07` Cloudflare 线上 API、`AC-08` 公网 ACME CA、`AC-09` 真实外部 SMTP/QQ 网关、WebAuthn 硬件密钥）：本地已完成适配器与本地沙箱验证，不伪造外部线上投递状态。
   - **G2 契约与架构待拍板项**：`api/openapi/*.yaml` 保持字节不变，`G2`、`D01–D07`、`P1-C10` 以及 WASM/Eino ADK 运行时选型（`POC-01`/`POC-02`/`POC-06`）按规则保持 `OPEN` / 候选证据状态，等待项目负责人最终审阅。
