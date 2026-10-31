# 完整 v0.9 · Wave 3 全量路由补齐、Agent 回路接线与真后端浏览器验收

- **登记日期**：2026-10-31（Asia/Shanghai）
- **总控会话**：`agy-review-01`
- **前置已合入基线**：Wave 1（`4f5a459`）与 Wave 2（`a9a76d7`）已合入 `main`。当前 `apps/controller/server/server.go` 已组合 19 项 SQLite WAL 迁移及 `auth`、`nodes`、`servicegroups`、`tasks`、`files`、`audit`、`dns`、`notifications`、`gateway` 核心 HTTP 路由，并通过 HTTPS 端到端集成测试（`tests/integration/v09/controller_wave2_test.go`）。
- **Wave 3 目标**：在不突破模块边界且保持**三线零文件冲突**的前提下，补齐剩余后端领域的 HTTP 处理器与 Agent WSS 回路，完成前端对真实 Go Controller + SQLite WAL 的 Headless 浏览器端到端验收。

---

## 一、三线写入边界（严禁跨线修改，保持零冲突）

| 任务包 / 会话 | 分支 / Worktree | 允许写入目录 | 严禁触碰 |
|---|---|---|---|
| `V09-5D-CX`（Wave 3）<br>`codex-03` | `feature/v09-platform-cx02`<br>`.worktrees/feature--v09-platform-cx02` | `internal/{auth,transport,plugins,ai,workflows,updates,recovery}/**`、`apps/controller/**`、`apps/agent/**`、`plugins/xray-manager/**`、`tests/integration/v09/platform/**`、`tests/poc/v09/platform/**`、`docs/development/sprints/v09-five-day/progress/codex/**` | DS 所属 `internal/{storage,nodes,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,servicegroups,publishing}/**`、`apps/web/**`、公共配置/锁/契约 |
| `V09-5D-DS`（Wave 3）<br>`deepseek-02` | `feature/v09-services-ds02`<br>`.worktrees/feature--v09-services-ds02` | `internal/{storage,nodes,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,servicegroups,publishing}/**`、`tests/integration/v09/services/**`、`tests/poc/v09/services/**`、`docs/development/sprints/v09-five-day/progress/deepseek/**` | `apps/controller/**`（含 `server/server.go`，由总控在 Wave 3 集成时统一挂载 DS 新增 `RegisterHTTP`）、CX 所属 `internal/**`、`apps/web/**`、公共配置/锁/契约 |
| `V09-5D-GM`（Wave 3）<br>`gemini-02` | `feature/v09-console-gm02`<br>`.worktrees/feature--v09-console-gm02` | `apps/web/src/**`、`apps/web/index.html`、`apps/web/public/**`、`tests/browser/v09/**`、`docs/development/sprints/v09-five-day/progress/gemini/**` | 任何 Go 源码（`apps/controller/**`、`internal/**`）、根 `package.json`、`apps/web/package.json`、`apps/web/vite.config.ts`、公共配置/锁/契约 |

---

## 二、各线 Wave 3 子任务清单与验收要求

### 1. Codex（`V09-5D-CX` Wave 3 · 平台 HTTP 补齐、Agent WSS 回路与主控组合）

1. **`CX-W3-01`（`transport` Enrollment HTTP 与 Agent WSS 回路接入主控）**
   - 将 `internal/transport` 的 Enrollment HTTP（`/api/v1/enrollment-tokens*`、`/api/v1/enrollments*`、`/api/v1/agent/enroll`）与 Agent WSS 链路（`ControllerLinks`）挂载到 `apps/controller/server/server.go`（必须保持现有 Wave 2 路由与 `TestWave2ControllerEndToEndHTTPSComposition` 100% 通过）。
   - 在 `apps/controller/server/server.go` 中通过组合回调将 Agent 心跳/指标与 `nodes.Service.ApplyAgentUpdate` 接通，并将 `tasks.Service` 的 `Dispatcher` 及任务结果回写与 Agent 受控执行通道接通（不在 `internal/transport` 反向 import `nodes`/`tasks`）。
   - 在 `tests/integration/v09/platform/` 新增 Controller + Agent 本地注册、审批、mTLS/WSS 连接、心跳上报与受控任务执行的端到端集成测试。
2. **`CX-W3-02`（`ai` 与 `workflows` 管理 HTTP 路由补齐并接入主控）**
   - 在 `internal/ai` 补齐管理面 HTTP 处理器（对齐 `apps/web/src/lib/api/endpoints.ts` 的路径与 DTO）：
     - `GET /api/v1/ai/security-policy`
     - `POST /api/v1/ai/sessions`、`GET /api/v1/ai/sessions/{sessionId}`
     - `PATCH /api/v1/ai/sessions/{sessionId}/approval-policy`
     - `POST /api/v1/ai/sessions/{sessionId}/grants`、`DELETE /api/v1/ai/sessions/{sessionId}/grants/{grantId}`
     - `POST /api/v1/ai/sessions/{sessionId}/approvals`、`POST /api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision`
     - `POST /api/v1/ai/tools/execute`、`POST /api/v1/ai/sessions/{sessionId}/resource-recommendations`
     - 以及已有的 `MessagesHTTP`（`/messages`）与 `MessageEventsHTTP`（`/events` SSE），统一挂载到 `apps/controller/server/server.go`。
   - 保持 AI 会话默认零资源授权、服务组成员快照隔离、完全访问模式仍强制要求真人管理员审批任意 root Shell。
3. **`CX-W3-03`（`plugins` 与声明式 `xray-manager` HTTP 路由补齐并接入主控）**
   - 在 `internal/plugins` 补齐插件中心 HTTP 处理器并挂载到 `apps/controller/server/server.go`：
     - `GET /api/v1/plugins`、`GET/PUT /api/v1/plugins/repository`、`GET /api/v1/plugins/catalog`、`POST /api/v1/plugins/catalog/refresh`
     - `POST /api/v1/plugins/{pluginId}/install`、`PATCH /api/v1/plugins/{pluginId}/activation`、`POST /api/v1/plugins/{pluginId}/update`、`DELETE /api/v1/plugins/{pluginId}`、`GET /api/v1/plugin-operations/{operationId}`
     - 声明式 `/api/v1/plugins/xray-manager*` 门控与操作路由（注意：`internal/*` 核心包不得 import `plugins/xray-manager`，仅可在 `apps/controller` 组合或通过声明式接口桥接；停用/未安装时拒绝访问）。
4. **`CX-W3-04`（`updates` 与 `recovery` HTTP 路由补齐并接入主控）**
   - 在 `internal/updates` 与 `internal/recovery` 补齐 HTTP 处理器并挂载到 `apps/controller/server/server.go`：
     - `POST /api/v1/system/updates/check`、`POST /api/v1/system/updates/plan`、`POST /api/v1/system/updates/apply`、`GET /api/v1/system/updates/jobs`、`GET /api/v1/system/updates/jobs/{jobId}`、`POST /api/v1/system/updates/jobs/{jobId}/rollback`
     - `POST /api/v1/backups/{backupId}/verify`、`POST /api/v1/disaster-recovery/preview`、`POST /api/v1/disaster-recovery/jobs`、`GET /api/v1/disaster-recovery/jobs/{jobId}`。

---

### 2. DeepSeek（`V09-5D-DS` Wave 3 · 剩余业务域 HTTP 处理器与节点/任务扩展接口）

> **注意**：DS **不得修改 `apps/controller/**`**（避免与 CX 产生冲突）。DS 在各自 `internal/<domain>` 下提供 `RegisterHTTP` 与 `AuditActions() []string`，并在 `internal/<domain>/http_test.go` 及 `tests/integration/v09/services/` 中完成完整 HTTP 集成测试；总控将在 Wave 3 集成时把 DS 新增的 `RegisterHTTP` 一并接入 `apps/controller/server/server.go`。

1. **`DS-W3-01`（`internal/monitoring` 与 `internal/quota` HTTP 处理器）**
   - 新增 `internal/monitoring/http.go` + `http_test.go`：注册 `GET/POST /api/v1/monitoring/availability-monitors`、`PATCH /api/v1/monitoring/availability-monitors/{monitorId}`（支持 `paused` 切换）、`GET /api/v1/monitoring/traffic-risks`，导出 `AuditActions() []string`，对齐 `apps/web/src/lib/api/endpoints.ts`。
   - 新增 `internal/quota/http.go` + `http_test.go`：注册 `GET/PUT /api/v1/nodes/{nodeId}/traffic-quota`（或可挂载于 `/api/v1/nodes/` 的子路由处理器），导出 `AuditActions() []string`。
2. **`DS-W3-02`（`internal/certificates` 与 `internal/publishing` HTTP 处理器）**
   - 新增 `internal/certificates/http.go` + `http_test.go`：注册 `GET/POST /api/v1/certificates/internal/distributions`、`POST /api/v1/certificates/internal/distributions/{distributionId}/revoke`，导出 `AuditActions() []string`；严格保证私钥不入响应/日志/审计。
   - 新增 `internal/publishing/http.go` + `http_test.go`：注册 `POST /api/v1/publishing/preview`、`GET/POST /api/v1/publishing/publications`、`GET /api/v1/publishing/publications/{publicationId}`，导出 `AuditActions() []string`。
3. **`DS-W3-03`（`internal/gateway`、`internal/nodes` 与 `internal/tasks` 扩展 HTTP 操作）**
   - 扩展 `internal/gateway/http.go` + `http_test.go`：补充 `GET /api/v1/gateways/{gatewayId}/checks`（诚实返回当前已记录检查或明确状态，不伪造历史）与 `PATCH /api/v1/gateways/{gatewayId}/dispatch`（兼容 `{state: "enabled"|"paused"}` 与 `{enabled, expectedRevision}`）。
   - 扩展 `internal/nodes/http.go` + `http_test.go`：补充 `POST /api/v1/nodes/{nodeId}/suspend`、`POST /api/v1/nodes/{nodeId}/revoke`、`GET /api/v1/nodes/{nodeId}/identity`、`GET /api/v1/nodes/{nodeId}/capabilities`、`GET /api/v1/nodes/{nodeId}/diagnostics`。
   - 扩展 `internal/tasks/http.go` + `http_test.go`：补充 `GET /api/v1/tasks/{jobId}/executions` 与 `POST /api/v1/tasks/{jobId}/resume`。
4. **`DS-W3-04`（跨模块 HTTP + SQLite WAL 端到端流水线测试）**
   - 在 `tests/integration/v09/services/` 新增覆盖上述全部新增 HTTP 端点的真实 SQLite WAL 集成与越权/并发/回滚负向测试，并在 `docs/development/sprints/v09-five-day/progress/deepseek/STATUS.md` 更新 SHA 与输出。

---

### 3. Gemini（`V09-5D-GM` Wave 3 · 真实 Go Controller + SQLite WAL 浏览器端到端联调与适配）

> **注意**：项目内置 Go `1.27.3` 与 Node `24.22.1`（在 PowerShell 中先执行 `. ./scripts/env.ps1` 即可直接使用 `go` 与 `node`/`npm`）。GM **不得修改任何 Go 文件或 `apps/controller/**`**；若发现后端响应格式需要后端配合调整，在前端做兼容适配（严禁伪造数据或静默吞错）的同时将后端调整建议写入 `docs/development/sprints/v09-five-day/progress/gemini/SHARED-CHANGES.md`。

1. **`GM-W3-01`（前后端真实 DTO 响应契合度核对与兼容适配）**
   - 对照已合入 `main` 的 `apps/controller/server/server.go` 及 `internal/{auth,nodes,servicegroups,tasks,files,audit,dns,notifications,gateway}/http.go` 的真实 JSON 响应结构，逐一核对 `apps/web/src/lib/api/endpoints.ts` 与 `dto.ts`：
     - 例如：`nodes`/`groups`、`service-groups`、`tasks`（含 `jobId`/`id`、预览/提交/审批响应）、`files`、`audit/events`、`dns`、`notifications`、`gateways` 的字段名与列表包装格式。
     - 在 `apps/web/src/lib/api/` 中对真实 Go Controller 响应格式与候选契约格式做双兼容解析（保持严格校验：非法/越权/空数据绝不伪造默认成功或假数据）。
2. **`GM-W3-02`（基于真实 Go Controller + SQLite WAL 的 Headless Edge 浏览器验收脚本）**
   - 在 `tests/browser/v09/` 新增 `run-live-controller-acceptance.mjs`（保留原 `run-acceptance.mjs` 桩验收不破坏）：
     - 脚本自动构建/启动真实 `apps/controller`（传入临时 SQLite WAL `-db`、`-listen 127.0.0.1:<port>`、`-origin`）并托管 `apps/web/dist`，使用 Headless Edge 跑通真实全链路：
       1. 首次启动 Bootstrap 初始化管理员 -> 自动登录进入 `/overview`；
       2. 页面刷新后通过 `GET /api/v1/auth/me` 恢复 `csrfToken`（验证写操作未锁定）；
       3. `/users` 创建第二管理员与角色/资源授权，验证默认零节点范围与授权后 `/nodes` 可见性；
       4. `/groups` 与 `/service-groups` 创建分组、修改成员（`expectedRevision`）；
       5. `/tasks` 预览并提交 root 任务、验证提交人自批拦截与第二管理员审批、SSE 实时进度读取；
       6. `/dns`、`/gateways`、`/monitoring`（通知渠道）、`/audit` 真实读写与权限隔离验证。
3. **`GM-W3-03`（全量单测与双浏览器套件回归）**
   - 确保 `npm test --workspace @serverfleet/web`、`npm run build --workspace @serverfleet/web`、`node tests/browser/v09/run-acceptance.mjs` 与 `node tests/browser/v09/run-live-controller-acceptance.mjs` 全部 PASS，并在 `docs/development/sprints/v09-five-day/progress/gemini/` 记录真实截图与验证证据。
