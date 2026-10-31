# V09-WAVE3-REVIEW · Wave 3 全量路由补齐、Agent WSS 回路与真实 Controller 浏览器端到端验收审查报告

- **任务 ID**：`V09-WAVE3-REVIEW`
- **集成会话**：`agy-review-01`
- **集成工作目录 / 分支**：`D:\admin\desktop\codex\myproject\ks\.worktrees\integration--v09-wave1-review`（`integration/v09-wave3-review`）
- **起始 `main` 基线**：`98f39e04e73ee3150b9f6146d92102c59e5358c3`（Wave 3 派发基线）
- **被审查三线提交**：
  - `V09-5D-CX`（`codex-03`）：代码 `9ef6a3b9c2041c69bbac0ab990ba3c5c912e7913` / 分支 HEAD `04ff698452b7a347063386f83948a3b3c8fbb053`（`feature/v09-platform-cx02`）
  - `V09-5D-DS`（`deepseek-02`）：代码 `a7e0852ab4ca393e18b5f199f83f0fd3750df755` / 分支 HEAD `a858817313f02d2bacb48dda08b1956c3f19557b`（`feature/v09-services-ds02`）
  - `V09-5D-GM`（`gemini-02`）：`2aebec31d59db4093276acab7023c3c62865a0ba`（`feature/v09-console-gm02`）
- **总控组合接线与协调提交**：`038ecaf8e1511163d33b95bc511d92107c8fe36a`
- **结论**：**PASS（三线 Wave 3 独立审查通过、零跨线文件冲突，总控完成 DS 四域 HTTP 与主控组合接线、全量门禁与真实 Go Controller 浏览器端到端验收全部通过，合入 `main`）**

---

## 1. 三线独立审查摘要

### 1.1 `V09-5D-CX`（平台、安全、Agent 传输、AI 会话与插件/更新/恢复 HTTP，`04ff698`）
1. **范围与边界检查**：
   - 7 个提交、27 个文件变更，全部位于 `internal/{transport,ai,plugins,updates,recovery}`、`apps/controller/server/`、`apps/agent/`、`tests/integration/v09/platform/` 与 `docs/development/sprints/v09-five-day/progress/codex/`，未触碰 DS 业务包或 `apps/web`，未引入外部 WebSocket 依赖。
2. **核心实现核查**：
   - **CX-W3-01（入网管理 HTTP + 标准库 RFC6455 WebSocket + Agent WSS 回路）**：`internal/transport/websocket.go` 使用 Go 标准库实现有界 RFC6455 握手与二进制帧读写；`internal/transport/enrollment_management_http.go` 与 `agent_link_http.go` 实现入网 Token 签发、入网申请列表/审批及 `/api/v1/agent/link` mTLS + Protobuf `Envelope` 双向通信；`apps/controller/server/agent_links.go` 将 Agent 采样上报接入 `nodes.Service.IngestAgentSample`，将 `tasks.Dispatcher` 与任务结果回传打通。
   - **CX-W3-02（AI 会话管理、工具门控、一次性审批与推荐 HTTP）**：`internal/ai/management_http.go`、`approvals.go`、`context.go`、`execution_ports.go` 实现会话创建/查询、资源授权与撤权、审批策略切换、一次性审批签发/消费（满额自动失效）及任意 root Shell 强制真人管理员逐次审批门控（即使会话处于 `full_access` 亦不可豁免）。
   - **CX-W3-03（插件中心与可选 Xray 声明式 HTTP）**：`internal/plugins/{http,xray_http,operations,repository}.go` 实现仓库配置、目录刷新、SHA-256 哈希与权限确认校验、启用/停用生命周期与 Xray 声明式路由门控（未启用时拒绝全部 `/api/v1/plugins/xray-manager/*` 业务子路由），核心模块零静态导入 `plugins/xray`。
   - **CX-W3-04（更新中心与备份灾备恢复 HTTP）**：`internal/updates/http.go` 与 `internal/recovery/http.go` 实现只读检查/计划、显式确认与可验证备份前置的 Apply/Rollback，以及备份校验、恢复预检与旧实例隔离确认（`oldInstanceFenced=true`）门控。

### 1.2 `V09-5D-DS`（监控、配额、证书、发布与网关/节点/任务扩展 HTTP，`a858817`）
1. **范围与边界检查**：
   - 10 个提交、29 个文件变更，全部位于 `internal/{monitoring,quota,certificates,publishing,gateway,nodes,tasks}`、`tests/integration/v09/services/` 与 `docs/development/sprints/v09-five-day/progress/deepseek/`，严守不直接修改 `apps/controller` 的派发约束，并通过 `HANDOFF-WAVE3.md` 提供精确的主控挂载说明。
2. **核心实现核查**：
   - **DS-W3-01（可用性监控与流量风险 HTTP + Quota 风险端口）**：`internal/monitoring/http.go` 与 `internal/quota/risks.go` 通过组合层 `TrafficRiskSource` 接口读取可见节点配额风险，保持 `monitoring` 与 `quota` 零互相导入；未实现的监控历史接口诚实返回 `501 monitoring_history_unavailable`。
   - **DS-W3-02（节点流量配额 HTTP）**：`internal/quota/http.go` 实现 `GET/PUT /api/v1/nodes/{nodeId}/traffic-quota`，仅暴露警告/到期风险评估，不产生自动停机或阻断副作用。
   - **DS-W3-03（内部 PKI 证书分发与生命周期 HTTP）**：`internal/certificates/http.go` 实现 `GET/POST /api/v1/certificates/internal/distributions` 及 `renew`/`revoke`/`rotate`，強制校验 `confirm=true` 与服务组 `expectedGroupRevision` 快照，响应永远只含 `keyRef` 引用而不含私钥材料。
   - **DS-W3-04（发布向导 HTTP + Gateway/Nodes/Tasks 扩展端点）**：`internal/publishing/http.go` 实现发布预检与三阶段编排；`internal/gateway/http.go` 补齐 `/checks` 与 `/dispatch`；`internal/nodes/http.go` 补齐 `suspend`/`revoke`/`identity`/`capabilities`/`diagnostics`；`internal/tasks/http.go` 补齐 `resume` 并在 `batchTaskJob` 中返回 `revision` 与 `planDigest`。

### 1.3 `V09-5D-GM`（真实 Go Controller 浏览器端到端验收与前后端契约对齐，`2aebec3`）
1. **范围与边界检查**：
   - 1 个提交、48 个文件变更，全部位于 `apps/web/`、`tests/browser/v09/` 与 `docs/development/sprints/v09-five-day/progress/gemini/`，未修改任何 Go 文件或公共契约配置。
2. **核心实现核查**：
   - **GM-W3-01 / GM-W3-03（前后端 DTO/路径/权限键兼容适配）**：新增 `apps/web/src/lib/api/compat.ts`（19 项单测）与 `apps/web/src/lib/permissions/backend-aliases.ts`（4 项单测），在不丢失候选契约语义的前提下完成 Web 页面对真实 Go Controller 端点的请求/响应双向适配。
   - **GM-W3-02（真实 Go Controller + SQLite WAL + HTTPS 浏览器端到端验收）**：修复 `tests/browser/v09/stub-server.mjs` UTF-8 夹具并在 `tests/browser/v09/run-live-controller-acceptance.mjs` 中自动编译启动真实 Go Controller（临时 SQLite WAL + 自签 TLS），驱动无头 Edge/Chromium 完成 32 步真实后端验收（含首次初始化 Bootstrap、刷新恢复 CSRF、默认零授权、显式资源授权、手动分组/服务组 `expectedRevision` 更新、root 任务预览与提交、自批 `409 Conflict` 拒绝、异人双人审批、SSE 实时事件流、DNS/网关/通知/权威审计链路）及 16 步 Stub 契约退化验收，留存 24 张截图证据。

---

## 2. 总控组合接线与协调改动（`038ecaf8e1511163d33b95bc511d92107c8fe36a`）

1. **`apps/controller/server/server.go` 全量组合接线**：
   - 在 `server.New` 中实例化 `monitoring.Repo`、`quota.Repo`、`certificates.Repo`、`publishing.Repo` 及对应领域服务（`monitoring.NewService`、`quota.NewService`、`certificates.NewService`、`publishing.NewService`），在 `registerRoutes()` 中挂载 `monitoring.RegisterHTTP`、`quota.RegisterHTTP`、`certificates.RegisterHTTP`、`publishing.RegisterHTTP`。
   - 补齐跨模块桥接适配器：`composedTrafficRiskSource`（桥接 `quota.Service.ListTrafficRisks` 至 `monitoring.TrafficRiskSource`）、`composedCertTaskBridge`（桥接内部证书分发至 `tasks.Service.Submit`）、`composedServiceGroupLookup` 的 `Revision`/`Members`/`LookupGroup` 方法，以及未注入外部发布器时默认失败关闭（返回 `publishing.ErrPublisherUnavailable`）的 `failClosedPublisher`。
   - 在 `ExtraPermissions()` 与 `composedAuthorizer.Authorize` 中补齐 Wave 3 新增权限键（`nodes.suspend`、`nodes.revoke`、`tasks.resume`、`monitoring.*`、`quota.*`、`certificates.*`、`publishing.*`）与无节点目录读/写鉴权分支。
   - 修复 `server.New` 错误清理闭包 `cleanup`，在初始化中途失败时同步关闭 `s.PluginVault` 句柄，防止 Windows 临时目录文件句柄残留。
2. **`apps/controller/server/server.go` 与 `internal/tasks/http.go` 任务动作 ID 校验修复（回应 GM `SHARED-CHANGES.md` §12.3）**：
   - `resolveOperationClass` 对空字符串或含空白/冒号的非法动作标识返回 `tasks.ErrUnknownOperationClass`；`internal/tasks/http.go:writeError` 将 `auth.ErrInvalidCheck` 映射为 `400 Bad Request`、`auth.ErrPolicyUnavailable` 映射为 `403 Forbidden`，消除自由文本命令导致的 `500 internal_error`。
3. **`scripts/check.mjs` Windows 命令行长度修复（回应 CX `STATUS.md` 报告）**：
   - `goFiles(dir)` 跳过 `.worktrees` 目录并返回相对路径（`path.relative(root, full)`），消除 Windows 下深层 Worktree 路径执行 `gofmt -l` 触发的 32,767 字符 `CreateProcess` `ENAMETOOLONG` 限制。
4. **`apps/web/src/features/plugins/plugins-page.test.tsx` 异步渲染等待修复**：
   - 将第 130 行同步 `screen.getByText("已安装·停用")` 改为 `await screen.findByText("已安装·停用")`，消除高并发 CPU 负载下 `useEffect` 完成前的竞态失败。
5. **`tests/integration/v09/controller_wave2_test.go` 扩展**：
   - 修复 `newClient` 复用 `tlsServer.Client()` 指针导致的 CookieJar 覆盖问题，并新增对 `monitoring`、`quota`、`certificates`、`publishing`、`nodes/{id}/{identity,capabilities,diagnostics}`、`tasks/{id}/executions` 及非法命令 `400 Bad Request` 的组合 HTTPS 端到端断言。

---

## 3. 全量门禁、安全与浏览器端到端验证结果

| 检查项 | 命令 | 结果 |
|---|---|---|
| 原始基线与字节精确契约 | `node scripts/verify-source.mjs` | **PASS**（69 份原始文件 + 3 份字节不变 OpenAPI 候选完好） |
| 架构与 Web 边界检查 | `node scripts/web-boundaries.mjs` + `go test ./tests/architecture` | **PASS** |
| 契约检查（根 + `g2-r02` + `g2-r03`） | `node scripts/check.mjs contracts` | **PASS** |
| Go 格式 / 依赖 / 静态分析 | `gofmt -l` / `go mod verify` / `go mod tidy -diff` / `go vet ./...` / `staticcheck ./...` | **PASS** |
| Go 全量单测与集成测试 | `go test ./...` | **PASS**（573 顶层测试 + 1,128 子测试 = 1,701 PASS，0 FAIL） |
| Web 类型检查与单测 | `npm run typecheck --workspace @serverfleet/web` + `npm test --workspace @serverfleet/web` | **PASS**（36 测试文件 / 306 项测试全部 PASS） |
| Web 生产构建 | `npm run build --workspace @serverfleet/web` | **PASS** |
| 安全漏洞扫描 | `npm audit --audit-level=high` + `govulncheck ./...` | **PASS**（0 漏洞） |
| 浏览器验收（Stub 契约与退化场景） | `node tests/browser/v09/run-acceptance.mjs` | **PASS**（16/16 步全部通过） |
| 浏览器验收（真实 Go Controller + SQLite WAL + HTTPS） | `node tests/browser/v09/run-live-controller-acceptance.mjs` | **PASS**（32/32 步全部通过） |

---

## 4. 剩余风险与边界说明

1. **未入候选契约的端点继续诚实暴露 `BLOCKED` / `501`**：如自动化工作流 HTTP API、监控历史查询、文件字节流上传通道、备份列表查询等在冻结候选 OpenAPI 中未定义的接口，前后端均按规则诚实展示契约缺口或返回失败关闭状态，不伪造本地假成功。
2. **外部真实环境与 G2 契约冻结边界保持不变**：Cloudflare 线上 API、真实 Linux systemd/cgroup v2 root 执行、WebAuthn 硬件密钥及远程/VPS/云资源按规则未连接；`api/openapi/*.yaml` 保持字节不变，`G2`、`D01–D07` 与 `P1-C10` 仍为 `OPEN`。
