# V09 五日冲刺 · Wave 5 本地全栈收官、Wave 4 新接口前后端打通与双进程 CLI 连接任务单

- **派发会话**：`agy-review-01`
- **派发日期**：2026-10-31（Asia/Shanghai）
- **基线概念**：Wave 4 已合入 `main`（`f4609f5253d2f4aeff7b3d77eafa71cbc9adc030`），包含工作流 HTTP API（`/api/v1/workflows*`）、Controller/Agent CLI 自包含本地 PKI、监控历史与探测 HTTP（`/api/v1/availability-monitors/{id}/history`、`/probe`）、文件字节流上传与 SHA-256 校验（`PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content`、`POST .../complete`）、发布执行与回滚 HTTP（`POST /api/v1/publications/{id}/run`、`/rollback`）及 58 步真实 Go Controller 浏览器端到端验收。
- **本轮目标**：
  1. **前端与浏览器端到端（`gemini-05`）**：将 Wave 4 刚合入 `main` 的 4 组后端新端点（工作流管理与运行、文件字节流上传与 SHA-256 校验、监控历史与立即探测、发布三阶段执行与逆序回滚）全部接入 `apps/web` 正式页面，并将 `tests/browser/v09/run-live-controller-acceptance.mjs` 从 58 步扩展至 **66+ 步真实 Go Controller 浏览器验收**。
  2. **数据与业务服务（`deepseek-05`）**：在 `internal/monitoring` 与 `internal/files` 中补齐受 SSRF 复核保护的真实标准库 `NetProber` 实现与受控本地暂存目录 `LocalStagingTransport` 实现，通过 `HANDOFF-WAVE5.md` 交接总控组合接线。
  3. **平台与安全（`codex-05`）**：在 `apps/agent/runtime` 与 `apps/agent/main.go` 中补齐已入网 Agent 的受控单次 WSS+mTLS 连接、心跳/指标上报与任务接收执行模式（`-connect-once`），并在 `tests/integration/v09/platform/controller_cli_pki_test.go` 中完成真实编译 `controller` + `agent` 双进程从入网审批到 WSS+mTLS 在线心跳与任务执行的完整闭环验证。

---

## 1. 通用边界与铁律（必须遵守）

1. **禁止跨模块越界修改**：
   - `codex-05` 只允许写：`internal/{auth,transport,plugins,ai,workflows,updates,recovery}`、`apps/controller`、`apps/agent`、`plugins/xray-manager`、`tests/integration/v09/platform`、`docs/development/sprints/v09-five-day/progress/codex`。**严禁修改 DS 的 11 个 `internal/*` 包或 `apps/web`**。
   - `deepseek-05` 只允许写：`internal/{storage,nodes,servicegroups,tasks,files,monitoring,notifications,quota,dns,certificates,gateway,publishing,audit}`、`tests/integration/v09/services`、`tests/poc/v09/services`、`docs/development/sprints/v09-five-day/progress/deepseek`。**严禁修改 `apps/controller`、`apps/agent`、CX 的 7 个 `internal/*` 包或 `apps/web`**。需要 `apps/controller/server/server.go` 接线的构造函数与配置项请写在 `docs/development/sprints/v09-five-day/progress/deepseek/HANDOFF-WAVE5.md`，由总控在 Wave 5 审查时统一接线。
   - `gemini-05` 只允许写：`apps/web`、`tests/browser/v09`、`docs/development/sprints/v09-five-day/progress/gemini`。**严禁修改任何 Go 文件、OpenAPI/Proto 或根配置**。
2. **禁止修改保护文件**：
   - 严禁修改 `docs/reference/**`、`api/openapi/*.yaml`、`proto/**`、`gen/**`、`go.mod`、`go.sum`、`package.json`、`package-lock.json`、`docs/development/dependencies.json`、`scripts/check.mjs`。
3. **环境与提交规范**：
   - PowerShell 执行命令前必须先加载本地固定工具链：`. .\scripts\env.ps1`。
   - 提交标题必须匹配 `^(feat|fix|docs|chore|test|ci|refactor|build|perf|revert)(\([a-z0-9-]+\))?!?: .+`。
   - 不得合并 `main`，不得操作其他 Worktree，不得推送远程。

---

## 2. `V09-5D-CX` Wave 5 任务包（会话 ID：`codex-05`）

- **工作目录**：`D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`
- **分支**：`feature/v09-platform-cx02`

### 子项清单

1. **CX-W5-01：`apps/agent` CLI 受控单次 WSS+mTLS 连接、心跳采样与任务执行模式（`-connect-once`）**
   - 在 `apps/agent/runtime/runtime.go` 与 `apps/agent/main.go` 中扩展 CLI 标志：
     - 支持 `-connect-once`（配合 `-state-dir <abs-dir>`、`-controller-url <https-or-wss-url>`、`-root-cert <ca-pem-path>` 及可选 `-node-name` / `-wait-task-ms`）。
     - 无参数启动 `go run ./apps/agent` 时**必须保持原 M0-P1 骨架输出不变**，确保既有测试不回归。
     - 当指定 `-connect-once` 时，从 `-state-dir` 加载已批准落盘的本地节点证书与私钥（未入网或证书缺失时失败关闭并返回非零退出码），使用固定根证书 `-root-cert` 连接 Controller `/api/v1/agent/link`（WSS+mTLS），复用 `apps/agent/session.go`（或受控封装）完成 `AgentHello` / `ControllerHello` 握手、上报一次 `Heartbeat` 与 `Metrics` 采样，若连接期间收到 `TaskOffer` 则通过受控 `Executor` 执行并回传 `TaskAck` / `TaskStarted` / `TaskOutput` / `TaskFinished`，完成后干净关闭连接并退出 `0`。
2. **CX-W5-02：真实 `controller` + `agent` CLI 双进程入网、WSS+mTLS 上线与任务执行端到端测试**
   - 在 `tests/integration/v09/platform/controller_cli_pki_test.go` 中新增测试（如 `TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`）：
     1. 启动真实 `controller`（带 `-db` 本地自包含 PKI）；
     2. 管理员完成 Bootstrap/Login 并签发一次性入网 Token；
     3. 调用 `agent` CLI（或 `runtime.Run`）执行 `-enroll-token` 提交 CSR（状态 `pending`）；
     4. 管理员调用 `POST /api/v1/enrollments/{id}/decide` 批准后，再次调用 `agent` CLI `-enroll-token` 落盘签发证书，并用 `-status` 验证已入网状态；
     5. 为管理员授予该新节点的 `node.read` 与 `tasks.command` 权限，提交一条面向该节点的 `tasks.command` 任务；
     6. 调用 `agent` CLI `-connect-once` 连接 Controller `/api/v1/agent/link`，验证节点在 `GET /api/v1/nodes` 中变为在线且采样已更新，且该任务经 WSS+mTLS 执行并更新为完成状态，审计日志完整记录。
3. **CX-W5-03：进度与证据文档更新**
   - 更新 `docs/development/sprints/v09-five-day/progress/codex/STATUS.md` 与 `AC-EVIDENCE-WAVE4.md`（补充 Wave 5 新增测试与提交记录）。

---

## 3. `V09-5D-DS` Wave 5 任务包（会话 ID：`deepseek-05`）

- **工作目录**：`D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-services-ds02`
- **分支**：`feature/v09-services-ds02`

### 子项清单

1. **DS-W5-01：`internal/monitoring` 受 SSRF 复核保护的真实标准库 HTTP/TCP/TLS `NetProber` 实现**
   - 在 `internal/monitoring/prober.go`（及 `internal/monitoring/prober_test.go`）中实现满足 `monitoring.Prober` 接口的 `NewNetProber(cfg NetProberConfig)`：
     - 支持 `ProbeHTTP`（`http`/`https`）、`ProbeTCP`（`tcp`）、`ProbeTLS`（`tls`）三种真实网络探测，遵循传入 `ctx` 与 `timeout`。
     - **防 DNS Rebinding 二次 SSRF 校验**：在建立底层 TCP 连接前（如自定义 `DialContext` 解析 IP 后连接前），对解析出的每个目标 IP 再次调用 `monitoring.ValidateTarget`（或同源 IP 校验逻辑）拦截回环、私网、链路本地、云元数据地址；仅当目标主机名显式位于 `cfg.AllowedHosts` 受控白名单时才放行本地回环测试地址。
     - 返回脱敏的 `ProbeResult`（`Status`、`LatencyMs`、`StatusCode`、`ErrorCode`、`ErrorSummary`），绝不回显原始响应体或敏感头。
2. **DS-W5-02：`internal/files` 受控本地目录 `LocalStagingTransport` 实现与端到端集成测试**
   - 在 `internal/files/local_transport.go`（及 `internal/files/local_transport_test.go`）中实现满足 `files.UploadTransport` 与 `files.DownloadTransport` 接口的 `NewLocalStagingTransport(rootDir string)`：
     - `PushFile(ctx, nodeID, remotePath, content, size, sha256Hex)`：校验 `nodeID` 与 `ValidateRemotePath(remotePath)`，在 `<rootDir>/<nodeID>/...` 下安全创建目录并原子写入文件（拒绝符号链接与路径逃逸，核对写入字节数与 `sha256Hex`）。
     - `PullFile(ctx, nodeID, remotePath, sink)`：校验路径安全后从 `<rootDir>/<nodeID>/...` 流式读取已落地文件。
   - 在 `tests/integration/v09/services/wave4_http_test.go` 中新增集成测试：注入真实 `NewNetProber`（配合本地 `httptest.Server` 与 `AllowedHosts`）和真实 `NewLocalStagingTransport`（配合 `t.TempDir()`），验证 HTTP `/probe` → `/history` 真实探测落库，以及 HTTP 创建上传 → `PUT .../content` → `POST .../complete` 真实落盘与 `Download` 读回一致。
3. **DS-W5-03：交接说明与证据文档更新**
   - 编写 `docs/development/sprints/v09-five-day/progress/deepseek/HANDOFF-WAVE5.md`（写明总控在 `apps/controller/server/server.go` 中默认回退接入 `monitoring.NewNetProber` 与 `files.NewLocalStagingTransport` 的精确构造参数），并更新 `STATUS.md` 与 `AC-EVIDENCE-WAVE4.md`。

---

## 4. `V09-5D-GM` Wave 5 任务包（会话 ID：`gemini-05`）

- **工作目录**：`D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-console-gm02`
- **分支**：`feature/v09-console-gm02`

### 子项清单

1. **GM-W5-01：将 Wave 4 新增的 4 组真实后端接口完整接入 `apps/web` 页面与单测**
   - **自动化工作流页面（`apps/web/src/features/workflows/workflows-page.tsx` + `workflows-page.test.tsx`）**：
     - 移除原 `BLOCKED：无工作流接口` 静态占位，在 `apps/web/src/lib/api/` 中新增 `workflowsApi` 对接真实 Controller：
       - `GET /api/v1/workflows` → `{ items: WorkflowView[] }`（字段：`id`、`revision`、`enabled`、`trigger`、`match`、`steps: [{ actionId, nodeIds, parameters, humanApproval }]`）
       - `POST /api/v1/workflows` → 创建工作流（`201 Created`）
       - `GET /api/v1/workflows/{workflowId}` → `{ workflow: WorkflowView, runs: WorkflowRunView[] }`
       - `PATCH /api/v1/workflows/{workflowId}` → 更新启停或步骤（带 `expectedRevision`）
       - `POST /api/v1/workflows/{workflowId}/runs` → 触发运行（请求体 `{ eventId, kind, values }`，返回 `{ workflowId, eventId, matched, state, step, taskId }`）
     - 页面需支持：工作流列表展示、创建工作流表单（选择触发器标识、目标节点 ID、动作 `tasks.command` / `host.patch` / `host.root`、参数键值与 `humanApproval` 开关）、启停切换、手动触发运行及查看详情/关联任务 ID；只读角色（无 `workflows.write` 或 `workflow.manage`）诚实禁用写操作。注意在 `apps/web/src/lib/permissions/backend-aliases.ts` 中对齐后端权限键 `workflows.read` / `workflows.write`。
   - **文件互传页面字节流上传与完成校验（`apps/web/src/features/files/files-page.tsx` + `files-page.test.tsx`）**：
     - 在 `apps/web/src/lib/api/client.ts` 或 `endpoints.ts` 中新增 `filesApi.uploadContent(nodeId, uploadId, body: ArrayBuffer | Uint8Array | string, offset = 0)`，调用 `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content?offset=${offset}`（携带 `Content-Type: application/octet-stream`、`X-CSRF-Token` 与 `X-Requested-With: ServerFleetWeb`）。
     - 在 `files-page.tsx` 的 `handleFiles` 中：单文件创建上传会话（`POST .../uploads`）后，立即读取文件字节并调用 `uploadContent` 上传字节流，再调用 `completeUpload`（`POST .../complete`）触发服务端字节数与 SHA-256 校验：
       - 若 `completeUpload` 返回 `200 OK`（`status: "completed"`），显示上传完成并刷新目录；
       - 若服务端未注入远端节点传输层返回 `503 unavailable`，在界面上诚实展示“文件字节流（X B）已上传并通过服务端 SHA-256 校验；当前节点文件传输通道未注入（503 unavailable）”，不再停留在旧的“字节通道未实现”静态提示。
   - **监控告警页面立即探测与真实检查历史（`apps/web/src/features/monitoring/monitoring-page.tsx` + `monitoring-page.test.tsx`）**：
     - 在 `monitoringApi`（`apps/web/src/lib/api/endpoints-infra.ts`）中新增：
       - `history: (monitorId: string, limit = 20, signal?: AbortSignal) => apiClient.request<{ items: readonly MonitorCheckEntry[] }>(\`/api/v1/availability-monitors/\${encodeURIComponent(monitorId)}/history\`, { query: { limit }, signal })`
       - `probe: (monitorId: string) => apiClient.request<{ monitor: AvailabilityMonitor; check: MonitorCheckEntry }>(\`/api/v1/availability-monitors/\${encodeURIComponent(monitorId)}/probe\`, { method: "POST" })`
     - 在服务监控列表操作列中：将“检查历史”对接真实的 `monitoringApi.history`（展示检查时间、状态 `up/down/unknown`、延迟 `latencyMs` 与错误摘要；无记录时诚实显示“暂无检查记录”），并新增“立即探测”按钮调用 `monitoringApi.probe`（若目标触发 SSRF 拦截返回 `422` 或未注入探测器返回 `503 prober_unavailable`，诚实展示服务端错误码与原因）。同时在 `tests/browser/v09/stub-server.mjs` 中补齐相应路由兼容以确保 `run-acceptance.mjs` 不回归。
   - **服务发布页面执行与回滚操作（`apps/web/src/features/publishing/publishing-page.tsx` + `publishing-page.test.tsx`）**：
     - 在 `publicationsApi`（`apps/web/src/lib/api/endpoints-infra.ts`）中新增：
       - `run: (publicationId: string) => apiClient.request<AsyncOperation>(\`/api/v1/publications/\${encodeURIComponent(publicationId)}/run\`, { method: "POST" })`
       - `rollback: (publicationId: string) => apiClient.request<AsyncOperation>(\`/api/v1/publications/\${encodeURIComponent(publicationId)}/rollback\`, { method: "POST" })`
     - 在发布记录操作列或阶段详情弹窗中新增“执行发布”与“回滚”按钮，调用真实接口并刷新发布记录/阶段状态（若未配置外部发布器导致 `run` 返回 `502 stage_failed`，诚实展示服务端失败关闭错误并刷新查看更新后的阶段状态）。
2. **GM-W5-02：扩展真实 Go Controller 浏览器端到端验收（`tests/browser/v09/run-live-controller-acceptance.mjs`）**
   - 在现有 58 步基础上新增 Wave 5 真后端浏览器步骤（目标 **66+ 步全部 PASS**），至少包括：
     1. **工作流端到端**：为管理员在 `node-alpha` 上确保具备 `workflows.read` / `workflows.write`（及 `tasks.command`），在 `/#/workflows` 页面通过 UI 创建一条工作流（`POST /api/v1/workflows -> 201`）、切换启停状态（`PATCH /api/v1/workflows/{id} -> 200`）、点击触发运行（`POST /api/v1/workflows/{id}/runs -> 202`）并在页面上看到生成的受控 `taskId`；
     2. **监控历史与探测端到端**：在 `/#/monitoring` 服务监控分区点击“检查历史”（`GET /api/v1/availability-monitors/{id}/history -> 200`）与“立即探测”（`POST /api/v1/availability-monitors/{id}/probe`，验证 SSRF 拦截或 `503 prober_unavailable` 诚实反馈）；
     3. **文件字节流上传与完成校验端到端**：在 `/#/files` 选择本地文件触发 `POST /api/v1/nodes/node-alpha/files/uploads -> 201`、`PUT .../content?offset=0 -> 200`（真实写入字节并核对 `bytesReceived`）与 `POST .../complete`（验证字节与 SHA-256 校验通过后的诚实响应展示）；
     4. **服务发布执行与回滚端到端**：在 `/#/publishing` 对已创建的发布单点击“执行发布”（`POST /api/v1/publications/{id}/run`）与“回滚”（`POST /api/v1/publications/{id}/rollback -> 200`），并验证阶段详情更新；
     5. **Wave 4/5 审计闭环**：在 `/#/audit` 验证 `workflows.*`、`files.upload.*`、`publishing.*` 等新增真实审计记录。
   - 保存新增截图 `live-33-*.png` 等至 `docs/development/sprints/v09-five-day/progress/gemini/evidence/live/`。
3. **GM-W5-03：进度与证据文档更新**
   - 更新 `docs/development/sprints/v09-five-day/progress/gemini/STATUS.md`、`ACCEPTANCE.md` 与 `AC-EVIDENCE-WAVE4.md`。
