# V09-WAVE2-WIRING · Wave 2 主控组合接线与 Web/API 端到端集成报告

- **任务 ID**：`V09-WAVE2-WIRING`
- **集成会话**：`agy-review-01`
- **集成工作目录 / 分支**：`D:\admin\desktop\codex\myproject\ks\.worktrees\integration--v09-wave1-review`（`integration/v09-wave2-wiring`）
- **起始 `main` 基线**：`4f5a4593d3a0d1f04c0ccd59809b4e5cd36b7283`（Wave 1 审查合入基线）
- **Wave 2 代码提交**：`c840800ec06b1cc41e8b3d5e2b5a85535536d891`
- **结论**：**PASS（`apps/controller` 组合接线、19 项 SQLite WAL 迁移、跨模块统一授权/审计接线、Web 会话 CSRF/Bootstrap 对齐与 HTTPS 端到端集成测试全部通过，合入 `main`）**

---

## 1. Wave 2 交付范围与实现摘要

### 1.1 `apps/controller` 模块化单体组合运行时（`apps/controller/server/server.go`、`apps/controller/main.go`）
1. **全量 SQLite WAL 迁移注册（19 项）**：
   - `Migrations()` 统一汇总 CX 平台迁移（`cx/0001_auth_identity` 至 `cx/0006_workflows`）与 DS 数据业务迁移（`0001_init` 至 `0013_publishing`），在 `server.New` 启动时通过 `storage.OpenSQLiteWAL` + `storage.Migrate` 自动幂等执行。
2. **统一审计目录与跨模块事务接线**：
   - `AuditActions()` 汇总去重 `auth`、`nodes`、`servicegroups`、`tasks`、`files`、`dns`、`notifications`、`gateway` 全部允许动作目录并注入 `audit.NewSQLRecorder`。
   - `auth.NewSQLIdentityStore` 通过 `identityAuditAppender` 将 `IdentityTx.AppendAudit` 桥接至 `audit.SQLRecorder.AppendTx`，确保身份/会话/授权变更与审计事件在同一 `BEGIN IMMEDIATE` 事务中原子落盘。
3. **统一认证、CSRF/Origin 防护与零信任默认授权**：
   - `auth.NewIdentityService` 接入基于真实 `nodes.Repo`、`nodes.GroupRepo`、`servicegroups.Repo` 的 `ResourceResolver`，并注册跨模块角色动作目录（`ExtraPermissions()`）。
   - 所有 `/api/v1/` 路由（除匿名 bootstrap/login 外）统一经过 `IdentityHTTP.AuthenticateRequest` 校验会话或 Scoped Bearer Token，强制设置 `Cache-Control: no-store` 与 `X-Request-ID`，Cookie 写操作强制校验同源 `Origin` 与 `X-CSRF-Token`。
   - `composedAuthorizer` 统一桥接 `nodes`、`servicegroups`、`tasks`、`files`、`audit`、`dns`、`notifications`、`gateway` 的授权检查：节点级动作通过 `auth.EvaluateNodePolicy` 严格按显式 `user_resource_grants` 判定（保留管理员默认零资源授权），目录级无节点动作通过 `AuthorizeSubjectAction` 校验角色权限。
4. **向后兼容的 CLI 入口（`apps/controller/main.go`）**：
   - 未提供 `-db` 时保持 M0-P1 最小安全占位行为（仅暴露 `/healthz`，其余 `/api/v1/` 返回 `501 Not Implemented`，`TestLivenessAndClosedManagementRoutes` 保持通过）。
   - 提供 `-db` 时启动完整 SQLite WAL 组合管理服务；提供 `-tls-cert` 与 `-tls-key` 时启用 HTTPS（`ListenAndServeTLS`）。

### 1.2 跨模块接口微调（`internal/auth`、`internal/tasks`）
1. **节点只读策略快照与目录动作授权（`internal/auth/session_views.go`、`internal/auth/management.go`）**：
   - 新增 `IdentityService.NodeReadPolicySnapshot(ctx)` 实现 `auth.PolicyReader`，支持 `auth.NewReadScopeEstablisher` + `nodes.NewReadUseCase` 的预检先行（preflight-before-inventory）只读链路。
   - 新增 `IdentityService.AuthorizeSubjectAction(ctx, subject, action)` 供非节点范围的目录操作鉴权。
   - 修复 `CreateResourceGrant` 在目标资源不存在时向上传播 `ErrInvalidCheck`（映射为 `400 Bad Request`），并在 `grantedNodes` 聚合时忽略已删除分组引用。
2. **双人审批自由文本原因与审计 `ReasonCode` 规范化（`internal/tasks/service.go`）**：
   - `tasks.Service.Decide` 将人工审批的完整自然语言原因（如 `"peer reviewed"`）保存在 `task_approvals.reason`，同时在包含空白字符时将审计事件的 `ReasonCode` 规范化为令牌值（`"approved"` / `"rejected"`），避免触发 `audit.ValidateEvent` 的单令牌格式拒绝。

### 1.3 Web 会话与初始化链路对齐（`apps/web`）
1. **刷新后内存态 CSRF Token 恢复（S01，`apps/web/src/lib/api/dto.ts`、`apps/web/src/app/session.tsx`）**：
   - `Identity` DTO 新增可选 `csrfToken?: string`；`SessionProvider` 在 `GET /api/v1/auth/me` 返回 `csrfToken` 时恢复内存态 CSRF Token 并解除写锁，未返回时继续保持只读写锁。
2. **首次初始化 Bootstrap 幂等键与登录衔接（`apps/web/src/lib/api/endpoints.ts`、`apps/web/src/app/session.tsx`）**：
   - `authApi.bootstrap` 自动附带 `Idempotency-Key`；当后端 `POST /api/v1/auth/bootstrap` 返回 `204 No Content` 时，`bootstrapSetup` 自动串联调用 `login(username, password)` 建立会话。

### 1.4 HTTPS 端到端集成测试（`tests/integration/v09/controller_wave2_test.go`）
- 新增 `TestWave2ControllerEndToEndHTTPSComposition`，在真实 `httptest.NewTLSServer` + SQLite WAL 临时库上完整验证：
  1. 19 项迁移自动初始化与 `/healthz` 探针；
  2. 匿名请求 `/api/v1/nodes` 返回 `401 Unauthorized`；
  3. `POST /api/v1/auth/bootstrap`（`204`）+ `POST /api/v1/auth/login`（`__Host-sf_session` Cookie + `csrfToken`）+ `GET /api/v1/auth/me` 刷新恢复 `csrfToken`；
  4. 管理员默认零资源授权（未授权前 `GET /api/v1/nodes` 返回空列表、`GET /api/v1/nodes/{id}` 返回 `403`），授予 `node-alpha` 资源权限后立即生效且 `node-beta` 仍被隔离；
  5. 手动节点分组创建、乐观锁 `expectedRevision` 成员更新与服务组创建；
  6. 高风险 `shell.exec`（`runAsRoot=true`）任务预览、提交（`approval_pending`）、提交人自批拒绝（`409 Conflict`）、第二管理员异人审批通过（`200 OK`）、调度派发与 `GET /api/v1/tasks/{jobId}/events` SSE 事件流读取；
  7. `GET /api/v1/audit/events` 权威审计事件落盘验证。

---

## 2. 组合验证与安全检查结果（`c840800ec06b1cc41e8b3d5e2b5a85535536d891`）

| 检查项 | 命令 | 结果 |
|---|---|---|
| 原始基线与字节精确契约 | `node scripts/verify-source.mjs` | **PASS**（69 份原始文件 + 3 份字节不变 OpenAPI 候选完好） |
| 架构与 Web 边界检查 | `node scripts/web-boundaries.mjs` + `go test ./tests/architecture` | **PASS** |
| 契约检查（根 + `g2-r02` + `g2-r03`） | `node scripts/check.mjs contracts` | **PASS** |
| Go 格式 / 依赖 / 静态分析 | `gofmt -l` / `go mod verify` / `go mod tidy -diff` / `go vet ./...` / `staticcheck ./...` | **PASS** |
| Go 全量单测与集成测试 | `go test ./...` | **PASS**（547 顶层测试 + 1,116 子测试 = 1,663 PASS，0 FAIL） |
| Web 类型检查与单测 | `npm run typecheck --workspace @serverfleet/web` + `npm test --workspace @serverfleet/web` | **PASS**（34 测试文件 / 283 项测试全部 PASS） |
| Web 生产构建 | `npm run build --workspace @serverfleet/web` | **PASS** |
| 安全漏洞扫描 | `npm audit --audit-level=high` + `govulncheck ./...` | **PASS**（0 漏洞） |

---

## 3. 剩余风险与边界说明

1. **真实外部环境与云/VPS/远程资源仍保持未连接**：Cloudflare 线上 API、真实 Linux systemd root 进程与 cgroup v2、多进程浏览器 WebAuthn 硬件密钥等受本地沙盒限制的外部环境项按规则保留离线/本地契约验证状态，不伪造线上通过记录。
2. **G2 公共契约仍未冻结**：`api/openapi/*.yaml` 三份候选保持字节不变，`D01–D07` 与 `P1-C10` 仍保持 `OPEN`，待负责人最终拍板冻结。
