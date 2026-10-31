# 完整 v0.9 · Wave 4 任务单（本地全链路闭环、全模块真后端浏览器 E2E 与 AC-01~24 逐项证据盘点）

- **登记时间**：2026-10-31（Asia/Shanghai）
- **总控会话**：`agy-review-01`
- **前置状态**：
  - Wave 1（`4f5a459`）、Wave 2（`a9a76d7`）与 Wave 3（`9453718`，代码提交 `038ecaf`）已全部合入 `main`。
  - 当前 `apps/controller` 已组合 19 项 SQLite WAL 迁移及 `auth`、`nodes`、`servicegroups`、`tasks`、`files`、`audit`、`dns`、`notifications`、`gateway`、`monitoring`、`quota`、`certificates`、`publishing`、`transport`（入网与 Agent WSS）、`ai`、`plugins`（含可选 Xray 声明式门控）、`updates`、`recovery` 全域 HTTP/SSE/WSS 路由；Go 573 顶层 + 1,128 子测试（1,701 项）PASS，Web 36 文件 / 306 单测 PASS，Stub 16/16 + Live Controller 32/32 浏览器端到端验收 PASS，安全扫描 0 漏洞。
- **Wave 4 核心目标**：
  1. **CX（`V09-5D-CX` Wave 4）**：补齐 `internal/workflows` HTTP 管理与触发路由并挂载至 `apps/controller/server`；为 `apps/controller` 与 `apps/agent` CLI 补齐可选的本地自包含 PKI / Agent 连接运行参数（保持无参默认行为向后兼容）；完成平台安全域 AC 与 PoC-01/02/03/04/06/08 逐项实证盘点。
  2. **DS（`V09-5D-DS` Wave 4）**：补齐 `internal/monitoring` 基于 `availability_results` 表的真实探测历史查询与按需探测 HTTP 端点（消除本地 `501` 缺口）；补齐 `internal/files` 分阶段上传的本地字节写入与 SHA-256 校验闭环；补齐 `internal/publishing` 可复用的本地服务适配层；完成数据业务域 AC 与 PoC-05/07 逐项实证盘点。
  3. **GM（`V09-5D-GM` Wave 4）**：基于 Wave 3 新挂载的主控路由（`monitoring`、`quota`、`certificates`、`publishing`、`ai`、`plugins`/`xray`、`updates`、`recovery`），扩展真实 Go Controller + SQLite WAL + HTTPS 浏览器端到端验收脚本覆盖其余全部管理台模块；完成前端与浏览器 E2E 对 AC-01~24 的逐项证据映射。

---

## 1. 写入边界与协作铁律（零文件重叠）

| 任务包 / 建议会话 ID | 分支 / Worktree | 允许写入目录 | 严禁修改目录 |
|---|---|---|---|
| **V09-5D-CX（Wave 4）** / `codex-04` | `feature/v09-platform-cx02` / `.worktrees/feature--v09-platform-cx02` | `apps/controller/`、`apps/agent/`、`internal/{auth,transport,plugins,ai,workflows,updates,recovery}/`、`plugins/xray/`、`tests/integration/v09/platform/`、`docs/development/sprints/v09-five-day/progress/codex/` | `internal/{storage,nodes,servicegroups,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,publishing}/`、`apps/web/`、`tests/browser/`、根公共文件 |
| **V09-5D-DS（Wave 4）** / `deepseek-02`（或 `deepseek-04`） | `feature/v09-services-ds02` / `.worktrees/feature--v09-services-ds02` | `internal/{storage,nodes,servicegroups,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,publishing}/`、`tests/integration/v09/services/`、`docs/development/sprints/v09-five-day/progress/deepseek/` | `apps/controller/`、`apps/agent/`、`internal/{auth,transport,plugins,ai,workflows,updates,recovery}/`、`apps/web/`、根公共文件 |
| **V09-5D-GM（Wave 4）** / `gemini-02`（或 `gemini-04`） | `feature/v09-console-gm02` / `.worktrees/feature--v09-console-gm02` | `apps/web/`、`tests/browser/v09/`、`docs/development/sprints/v09-five-day/progress/gemini/` | 所有 Go 目录（`apps/controller`、`apps/agent`、`internal`、`plugins`、`tests/integration`）、根公共文件 |

> **注意**：`apps/controller/server/` 仍由 **CX** 独占写入；**DS** 新增或扩展的 HTTP 路由/适配器在各自 `internal/<domain>/` 包内提供并写入 `progress/deepseek/HANDOFF-WAVE4.md`，由总控在 Wave 4 集成阶段统一挂载至 `apps/controller/server/server.go`。

---

## 2. 三线具体子项与验收要求

### 2.1 Codex · `V09-5D-CX`（Wave 4：工作流 HTTP + CLI 自包含入网连接 + 平台 AC/PoC 盘点）

1. **CX-W4-01：自动化工作流 HTTP 管理与受控执行挂载（`internal/workflows` + `apps/controller/server`）**
   - 基于已实现的 `internal/workflows`（`cx/0006_workflows` 表与 `Engine`/`Store`），新增 `internal/workflows/http.go`：
     - `GET /api/v1/workflows`：按主体权限列出已持久化工作流定义；
     - `POST /api/v1/workflows`：创建工作流定义（校验触发条件、目标范围、动作类型，写审计事件）；
     - `GET /api/v1/workflows/{workflowId}`、`PATCH /api/v1/workflows/{workflowId}`（启用/停用/更新并推进版本）；
     - `POST /api/v1/workflows/{workflowId}/runs`（或触发接口）：必须经过统一授权、`tasks.Service` 任务提交与审批门控，严禁绕过任务/审批直接执行。
   - 在 `apps/controller/server/` 中挂载工作流 HTTP 路由（注册相应权限键与审计动作），并在 `tests/integration/v09/platform/` 新增端到端测试验证工作流 CRUD、只读用户拒绝修改、触发进入统一任务/审批队列及审计落盘。
2. **CX-W4-02：`apps/controller` 与 `apps/agent` CLI 本地自包含连接增强**
   - 在 `apps/controller/main.go` 中支持可选参数（或在指定 `-db` 时自动在数据目录旁初始化本地 `transport.CertificateIssuer` 并注入 `server.Config.EnrollmentIssuer`/`AgentAuthority`/`AgentServerName`），使独立启动的 `controller` 进程默认即可使用 `/api/v1/enrollment-tokens`、`/api/v1/enrollments`、`/api/v1/agent/enroll` 与 `/api/v1/agent/link`（不再因未传 Issuer 而在真实进程下返回 `503 AGENT_TRANSPORT_UNAVAILABLE`）。
   - 在 `apps/agent/main.go` 中保持无参默认行为（打印骨架状态并退出 0，确保既有 `apps/agent` 测试通过）的前提下，支持可选 CLI 参数（如 `-controller-url`、`-enroll-token`、`-state-dir` 等）调用 `apps/agent/runtime` 完成一次入网/连接或状态自检，并补充单测。
3. **CX-W4-03：平台安全域 AC 与 PoC 证据盘点**
   - 新增 `docs/development/sprints/v09-five-day/progress/codex/AC-EVIDENCE-WAVE4.md` 并更新 `STATUS.md`，逐条列出 CX 负责的 `AC-01, 08, 10, 11, 12, 13, 14, 18, 19, 21, 22, 23, 24` 与 `POC-01, 02, 03, 04, 06, 08` 的已实现代码文件、自动化测试名称、通过命令输出及仍受外部 Linux/硬件环境限制的真实边界。

### 2.2 DeepSeek · `V09-5D-DS`（Wave 4：监控探测历史 + 文件字节上传校验 + 发布适配层 + 业务 AC/PoC 盘点）

1. **DS-W4-01：服务可用性监控探测执行与历史记录 HTTP（`internal/monitoring`）**
   - 在 `internal/monitoring/http.go` 中将 `GET /api/v1/monitoring/availability-monitors/{monitorId}/history` 接入真实的 `availability_results` 表查询（支持 `limit` 分页，校验 `monitoring.read` 与目标节点可见性，不再返回 `501 monitoring_history_unavailable`）。
   - 新增 `POST /api/v1/monitoring/availability-monitors/{monitorId}/probe`（或等效触发路由），调用 `Service` 执行一次受 SSRF/Allowlist 保护的探测并持久化探测结果（未配置 `Prober` 时失败关闭返回 `503 prober_unavailable`）。
2. **DS-W4-02：文件分阶段上传字节流写入与 SHA-256 落盘校验（`internal/files`）**
   - 在 `internal/files/http.go` 与 `internal/files/service.go` 中新增 `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content`（或支持向 `stagingDir` 写入受大小限制的上传字节流），使客户端可在调用 `POST /api/v1/nodes/{nodeId}/files/uploads` 声明上传后，真实上传字节内容并由 `POST /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/complete` 校验实际文件字节大小与 SHA-256 摘要一致后转为 `completed`。
3. **DS-W4-03：跨模块服务发布编排本地适配器与数据业务域 AC/PoC 盘点（`internal/publishing`）**
   - 提供可由主控组合层直接注入的本地领域桥接适配器（或完善 `publishing` 阶段执行/回滚测试夹具），在 `tests/integration/v09/services/` 中补充监控历史、文件真实字节上传与 Complete 校验、发布三阶段编排与失败补偿的 HTTP 集成测试。
   - 新增 `docs/development/sprints/v09-five-day/progress/deepseek/HANDOFF-WAVE4.md` 与 `AC-EVIDENCE-WAVE4.md`，更新 `STATUS.md`，逐条映射 DS 负责的 `AC-02, 03, 04, 05, 06, 07, 08, 09, 15, 16, 17, 18, 20` 及 `POC-05, 07` 的证据与外部环境边界。

### 2.3 Gemini · `V09-5D-GM`（Wave 4：Wave 3 全模块真后端浏览器 E2E 扩展 + 前端契约收口 + AC-01~24 浏览器证据盘点）

1. **GM-W4-01：扩展真实 Go Controller + SQLite WAL + HTTPS 浏览器端到端验收覆盖 Wave 3 新增路由**
   - 当前 `apps/controller/server/server.go` 已完整挂载 `monitoring`、`quota`、`certificates`、`publishing`、`ai`、`plugins`（含 `xray`）、`updates`、`recovery`。
   - 扩展 `tests/browser/v09/run-live-controller-acceptance.mjs`（或新增 `tests/browser/v09/run-live-wave4-acceptance.mjs` 并由主验收流程调用），在真实 Go Controller + SQLite WAL + HTTPS 上通过无头浏览器实测并截图取证以下模块：
     1. **节点详情与流量配额**（`#/nodes/node-alpha`：查看节点身份/能力/诊断，配置并读取 `PUT/GET /api/v1/nodes/node-alpha/traffic-quota`）；
     2. **监控与流量风险**（`#/monitoring`：创建可用性监控项 `POST /api/v1/monitoring/availability-monitors`、读取监控列表与 `/api/v1/monitoring/traffic-risks` 配额预警）；
     3. **证书管理**（`#/certificates`：读取内部 PKI 证书分发列表 `GET /api/v1/certificates/internal/distributions`）；
     4. **服务发布向导**（`#/publishing`：对已创建服务组执行 `POST /api/v1/publishing/preview` 预检、提交 `POST /api/v1/publishing/publications` 创建三阶段发布计划并验证列表展示）；
     5. **AI 助手与安全策略**（`#/ai`：读取 `/api/v1/ai/security-policy`、创建 AI 会话、验证初始零资源授权、授予 `node-alpha` 会话权限、切换审批策略、创建并决策一次性工具审批）；
     6. **插件中心与 Xray 声明式门控**（`#/plugins` 与 `#/plugins/xray`：读取插件仓库配置与目录、验证未启用时 Xray 管理页被门控拦截）；
     7. **更新中心与备份恢复**（`#/updates` 与 `#/backups`：执行更新检查与计划生成、创建本地备份并校验、在勾选 `oldInstanceFenced` 后执行恢复预检）。
2. **GM-W4-02：Web 契约兼容层与窄屏/可访问性细节打磨（`apps/web`）**
   - 在真后端浏览器联调上述 7 大模块过程中，如发现字段名、状态枚举或错误码细节差异，在 `apps/web/src/lib/api/compat.ts` 与对应 `apps/web/src/features/**` 中对齐并补充 Vitest 单测（严禁伪造客户端假成功）。
3. **GM-W4-03：前端与浏览器端到端 AC-01~24 证据盘点**
   - 新增 `docs/development/sprints/v09-five-day/progress/gemini/AC-EVIDENCE-WAVE4.md` 并更新 `STATUS.md` 与 `SHARED-CHANGES.md`，逐条登记 AC-01~24 对应的正式页面、Vitest 测试、Stub/Live 浏览器验收步骤编号与截图路径。
