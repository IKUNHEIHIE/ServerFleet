# V09-5D-CX / codex-05（接续 codex-04）

## V09-5D-CX Wave 5（codex-05，2026-10-31）

- 任务 `V09-5D-CX`（Wave 5）；唯一会话 `codex-05`；分支 `feature/v09-platform-cx02`；工作目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`；起始基线 `3adf891f3155b6452f06f25df53fd20f553742ae`。本轮本地提交，未推送、未合并 `main`。
- **CX-W5-01（Agent CLI 受控单次 WSS+mTLS 连接）**：
  - `apps/agent/runtime/runtime.go` 新增 `-connect-once`、`-node-name`（可选标签）、`-wait-task-ms`（0–600000）；与 `-status`/`-enroll-token` 互斥；只接受 `https://`/`wss://` 的 DNS 主机名 origin（拒绝 IP 字面量、用户信息、路径/查询/片段），无参数启动仍输出原 M0-P1 骨架。
  - `apps/agent/connect.go` 从 `-state-dir` 加载已签发身份，固定 `-root-cert` 校验 TLS1.3 链根 SPKI、`/api/v1/agent/link` WSS 升级、`AgentHello`/`ControllerHello`、一次 `Heartbeat`+`Metrics` 真实 `/proc` 采样、至多一次受控 `tasks.command` 执行（`TaskAck`/`TaskStarted`/`TaskOutput`/`TaskFinished`），随后干净关闭；无自动重试/重入网，缺证书或未入网失败关闭。执行器 `apps/agent/executor.go` 的本地动作仅接受空参数自我检查（不提供任意 Shell），输出经固定过滤器。
  - `apps/agent/session.go` 增加可选 `InitialSample`/`Established`/`ExecutionFinished` 钩子（nil 时保持既有长连接行为）；`internal/transport/websocket.go` 新增有界 `Ping`（与并发读循环配合）并在 hijack 后清除 HTTP 请求遗留 deadline，避免 `http.Server` 10 秒超时杀掉长连接。
  - 测试：`apps/agent/{connect,metrics}_test.go`、`apps/agent/session_test.go`、`apps/agent/runtime/runtime_test.go`、`internal/transport/websocket_test.go`；Windows 本机全 PASS。
- **CX-W5-02（真实 controller + agent CLI 双进程闭环）**：`tests/integration/v09/platform/controller_cli_pki_test.go` 新增 `TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`：
  - 先用 `bootstrap.Compose` 初始化同一 `-db`/本地 PKI 并取得一次 setup token，再启动**真实编译的 controller 二进制**（`-db/-origin/-tls-cert/-tls-key`）；管理 TLS 证书由同一本地 Agent CA 签发（Agent CLI 单一 `-root-cert` 同时信任管理端与入网根）。
  - 真实编译的 `agent` 二进制执行 `-enroll-token` → `pending` → 管理员 `POST /api/v1/enrollments/{id}/decision` 批准 → 再执行 `-enroll-token` → `issued`，`-status` 验证 `enrolled=true`。
  - 管理员授予 `nodes.read`/`tasks.submit`/`tasks.command`/`tasks.read` 后，提交一条面向新节点的 `tasks.command` 任务；`-connect-once -wait-task-ms 60000` 建立 mTLS WSS，节点在 `GET /api/v1/nodes` 显示 `online=true` 且 `cpuPercent`/`memoryPercent` 已更新；任务经 WSS 执行完成（`completed`/target `succeeded`），`tasks.submit`、`enrollment.approved` 与 `nodes.agent_update` 审计落盘；连接关闭后节点回到离线。
  - 该用例在 Windows 明确 SKIP：Controller 入网与 Agent Hello 仅接纳 `platform=linux`（既有安全策略，本包未放宽）；WSL Ubuntu 24.04 amd64 上实跑 PASS。
- **集成修正（真实闭环暴露）**：普通 Web/工作流任务没有 AI 会话 scope，`TaskOffer.scope_revision` 合法为 `0`。Executor 原先把 `0` 当结构拒绝条件，使真实 Web 任务无法执行；现将 `0` 解释为“非 AI 作用域”，仍要求 normal privilege、空 plugin、合法 schema/参数，Controller 仍是唯一授权源。新增 `TestAgentExecutorAcceptsUnscopedStandardOffer` 固定该行为，AI 任务仍携带非零 scope。
- **本机验证（2026-10-31，Windows）**：
  - `go test ./apps/... ./internal/transport/... ./internal/workflows/... ./tests/integration/v09/platform/... -count=1`：全部包 `ok`（Wave 5 集成为 SKIP，原因如上）。
  - `npm run check`（最终 HEAD 重跑，`reports/check-all.json`，`2026-10-31T15:04:57.500Z`）**PASS**：policy/contracts/go/web/git 五套全过；Web 36/36 文件、316/316 测试；`tsc --noEmit`、Vite 构建、gofmt、go mod verify/tidy、controller+agent 构建、全量 Go 测试、vet、staticcheck 均通过。
  - `npm run check:security`（最终 HEAD 重跑，`reports/check-security.json`，`2026-10-31T15:05:19.971Z`）**PASS**：`npm audit --audit-level=high` 0 漏洞，`govulncheck ./...` "No vulnerabilities found."。
- **Linux 原生闭环证据（WSL Ubuntu 24.04 amd64，Go 1.27.3）**：
  ```
  go test ./tests/integration/v09/platform -run TestWave5ControllerAndAgentCLIFullLoopWSSAndTask -count=1 -v -timeout 5m
  --- PASS: TestWave5ControllerAndAgentCLIFullLoopWSSAndTask (4.25s)
  go test ./tests/integration/v09/platform -run TestWave5ControllerAndAgentCLIFullLoopWSSAndTask -count=3 -timeout 5m
  ok  ... 11.507s
  go test ./apps/agent/... ./apps/controller/... ./internal/transport/... ./tests/integration/v09/platform/... -count=1 -timeout 10m
  全部 ok；platform 7.618s
  ```
  Windows 全量 Go 测试中该用例为 SKIP；Linux 同源测试重复 3 次均 PASS，无抖动；`go test -race ./apps/agent/... ./internal/transport/...` 亦 PASS。
- **本 Wave 提交**：`0cd0ecd`（Agent `-connect-once`、传输 Ping、指标采样与单测）、`3e9dd49`（真实 controller+agent 双进程集成测试）；文档提交见本分支最新提交。工作区干净、未推送、未合并 `main`。
- **剩余风险 / 未覆盖**：
  1. Agent 入网与 Hello 仍仅接纳 Linux；Windows 本机只验证 SKIP 与单元层，真实安装包、自启动、arm64/1C1G 与物理 VPS 未验收。
  2. `-connect-once` 是受控一次连接，无守护/重连/轮换；`-wait-task-ms` 到期即干净退出，不构成常驻 Agent 运行时。
  3. 本地 CLI 的单一 `-root-cert` 同时承担 HTTPS 管理端信任与 Agent CA pin，测试用同一本地 CA 签发管理证书；生产 PKI 分离（PKI-OPS）仍未决策。
  4. 受控执行仅 `tasks.command` 的空参数自我检查动作；未提供任意命令/root Shell，也不代表 PRIV/沙箱准入已获批准。
  5. 集成测试在测试内用 Go 工具链编译两个二进制（`-buildvcs=false`），需要 Go 环境；不是发行包安装验收。G2、D01–D07、P1-C10 未冻结。

---

## V09-5D-CX Wave 4（codex-04，2026-10-31）

- 任务 `V09-5D-CX`（Wave 4）；唯一会话 `codex-04`；分支 `feature/v09-platform-cx02`；工作目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`；起始基线 `0f9a75e416a82a9589a29d4d9cf8a52bf0266066`（Wave 1/2/3 与 DS 四域组合接线已合入 main）。本轮本地提交，未推送、未合并 main。
- **CX-W4-01（工作流 HTTP 管理与受控执行）**：新增 `internal/workflows/read.go` 与 `internal/workflows/http.go`，提供 `GET/POST /api/v1/workflows`、`GET/PATCH /api/v1/workflows/{workflowId}`、`POST /api/v1/workflows/{workflowId}/runs`。定义按所有者隔离，`workflows.read`（只读）与 `workflows.write`（operator/administrator）权限键已注册；触发只记录冻结计划并调用 `Engine.Advance`，每步经组合 `SubmitTask` 进入唯一 `tasks.Service.Submit`，修改类/root 动作落 `awaiting_approval`，任务自批 `409`，重复触发由任务幂等键去重。`apps/controller/server/workflows.go` 提供 SQL 后端、审计（`workflows.*` 8 个动作）与路由挂载。测试：`internal/workflows` 9 项 PASS，含 `TestWorkflowHTTPCreateReadPatchAndOwnerRules`、`TestWorkflowHTTPTriggerEntersTaskServiceAtMostOnce`、`TestWorkflowReadOwnerIsolation`；`TestWave4WorkflowHTTPManagementApprovalAndAudit` 在真实 HTTPS+SQLite WAL 上验证 CRUD、reader 403、审批门控、自批 409、任务不重复与审计落盘。
- **CX-W4-02（CLI 自包含连接）**：新增 `apps/controller/localpki`（本地 ECDSA P-256 Agent CA，0600、部分/损坏状态失败关闭、30 天 CA/12 小时证书）与 `apps/controller/bootstrap`（CLI 参数、`-db` 默认初始化数据库旁 `pki/`、推导 `AgentAuthority/AgentServerName`、TLS1.3 + `VerifyClientCertIfGiven`）。`apps/controller/main.go` 改为使用该组合：无 `-db` 仍为 M0-P1 空壳；有 `-db` 时 enrollment/WSS 路由默认挂载，不再 503。Agent 侧新增 `apps/agent/runtime` CLI（无参保持原骨架输出并退出 0；`-status` 自检；`-enroll-token/-controller-url/-root-cert` 执行一次入网），`apps/agent/identity_store.go` 增加只读 `Status`。测试：`apps/controller/{bootstrap,localpki}`、`apps/agent/{,runtime}` 全部 PASS；`TestWave4ControllerCLILocalPKIEnrollment` 覆盖 CLI 默认组合的 token→pending→批准→issued 与重启根指纹稳定。
- **CX-W4-03（证据盘点）**：新增 [AC-EVIDENCE-WAVE4.md](AC-EVIDENCE-WAVE4.md)，逐条映射 `AC-01, 08, 10, 11, 12, 13, 14, 18, 19, 21, 22, 23, 24` 与 `POC-01, 02, 03, 04, 06, 08` 的实现文件、测试与外部边界；全部 AC 仍为 `NOT_ACCEPTED`。
- **本机验证（2026-10-31，Windows）**：`go test ./... -count=1 -timeout 20m` 全部包 `ok`、0 FAIL；Wave4 专项输出已记录于 AC-EVIDENCE-WAVE4。`npm run check`（`reports/check-all.json`，2026-10-31T13:24:41.313Z）policy/contracts/go/web/git 五套 **全部 PASS**：gofmt、go mod verify/tidy、controller+agent 构建、全量 Go 测试、vet、staticcheck、原始基线/契约/架构检查、Web 36 文件/306 测试、`tsc --noEmit`、Vite 生产构建与 `git diff --check` 均通过。`npm run check:security`（`reports/check-security.json`，2026-10-31T13:25:20.338Z）**PASS**：`npm audit --audit-level=high` 0 漏洞、`govulncheck ./...` "No vulnerabilities found."。
  - 历史失败如实记录：首次 `npm run check` 在 Web `node-add-page.test.tsx` 出现一次按钮查询失败（CPU 高负载下异步渲染竞态，本会话未修改 `apps/web`）；该单文件单独复跑 3/3 PASS，随后完整 `npm run check` 36/36 文件、306/306 测试 PASS。
- **本 Wave 提交**：`f374a27`（工作流 HTTP 与受控触发）、`217fa82`（本地 PKI/CLI 与 Agent runtime）、`6e98097`（证据文档）；最终验证 HEAD 见本分支最新提交，工作区干净、未推送、未合并 main。
- **剩余风险**：工作流状态写入与任务创建为两个独立持久事务（任务幂等键收敛，失败窗口未隐藏）；多步骤仅由显式 `POST /runs` 重放推进，无可信外部事件生产端；本地 PKI 仅供本机开发，不是 PKI-OPS/生产轮换方案；未在真实 Linux/arm64/1C1G 安装 Agent 或 Xray，未接外部模型/ACME/云资源。G2、D01–D07、P1-C10 未冻结。

---


## V09-5D-CX Wave 3（codex-03，2026-10-31）

- 任务 `V09-5D-CX`（Wave 3）；唯一会话 `codex-03`；分支 `feature/v09-platform-cx02`；工作目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`；起始基线 `98f39e04e73ee3150b9f6146d92102c59e5358c3`（Wave 1+2 已合入 main）。
- **最新代码/验证 SHA `9ef6a3b9c2041c69bbac0ab990ba3c5c912e7913`**；状态文档提交 `22ccdb8e53cf19053c26463cdbd14754456085aa`（仅文档）。本轮本地提交（未推送、未合并 main）：`8a2cb68`（agent enrollment/WSS）、`b381361`（AI 管理面）、`32ca09b`（插件中心/Xray 声明式门控）、`529a8d8`（更新/恢复 HTTP）、`9ef6a3b`（审计结果/审批复用/回滚校验修正）。
- **CX-W3-01**：新增 `internal/transport/websocket.go`（标准库严格 RFC6455：单二进制帧、客户端掩码、RSV/分片/超长拒绝、ping/pong/close）、`agent_link_http.go`、`enrollment_management_http.go`；`server.go` 组合 `/api/v1/enrollment-tokens*`、`/api/v1/enrollments*`（兼容 `/api/v1/node-*` 别名）、`/api/v1/agent/enroll` 与 `/api/v1/agent/link`；`ControllerLinks` 的 handshake/admit/event/disconnected 接通心跳/指标→`nodes.Service.ApplyAgentUpdate`，`agentLinkBridge` 将 TaskOffer 经 `tasks.Dispatcher` 发出并把 TaskAck/Started/Output/Finished 回写 `tasks.Service.RecordResult`；后台 `Tasks.Run` 启动、`Close` 停止。`TestWave3ControllerAgentEnrollmentWSSAndTaskExecution` 覆盖本地 CA、一次性 token、pending→人工批准→重取 issued、mTLS WSS、hello/heartbeat、提交+第二管理员审批+派发+回写，任务 `completed`；无客户端证书 WSS 403、错误 token 403。`TestWave2ControllerEndToEndHTTPSComposition` 保持通过。
- **CX-W3-02**：`internal/ai` 新增 `context.go`、`approvals.go`、`execution_ports.go`、`management_http.go`；挂载 security-policy、sessions、approval-policy GET/PATCH、grants POST/DELETE、approvals POST/decision、tools/execute、resource-recommendations，并复用 `MessagesHTTP`/`MessageEventsHTTP` 的 messages/events SSE。会话默认零资源授权，组授权保留成员快照；AI 审批要求已认证管理员（HumanWeb+Administrator），root Shell 的 AI 审批仍把任务提交进唯一 tasks 管线，任务级第二管理员审批/自批 409 兜底。`TestWave3AIManagementScopeApprovalsAndRootShellGate` 覆盖零授权、授权、审批一次性消费、重放 409、full_access 不绕过 root、非管理员审批 403、未配置模型时 messages 503。
- **CX-W3-03**：`internal/plugins` 新增 `repository.go`、`operations.go`、`http.go`、`xray_http.go`；挂载 plugins 列表/单查、repository GET/PUT、catalog GET/refresh、install/update/activation/uninstall、plugin-operations；声明式 xray-manager status/capabilities/nodes/instance/templates/users/traffic/configurations/jobs 在未安装/未启用时 403、运行时操作 501。核心 `internal/plugins` 不 import `plugins/xray-manager`，architecture 检查 PASS。`TestWave3PluginCenterRoutesAndXrayGate` 覆盖门控与诚实不可用。
- **CX-W3-04**：`internal/updates/http.go`、`internal/recovery/http.go` 挂载 updates check/plan/apply/jobs/job/rollback 与 backups/{id}/verify、disaster-recovery preview/jobs/job；无 release feed/installer/executor 时明确 503/501；backup verify 使用 `storage.VerifyFile` 真实 quick_check+迁移账本，junk/缺失分别返回 false/404；DR preview 不把客户端 fencing 布尔当证据，eligible=false、job 501。`TestWave3UpdatesAndRecoveryRoutes` 覆盖。

### 验证（2026-10-31 Asia/Shanghai）

- `git rev-parse HEAD` = `9ef6a3b9c2041c69bbac0ab990ba3c5c912e7913`。
- `gofmt -l apps internal plugins tests` 无输出；`go vet ./...` PASS；`staticcheck ./...` PASS。
- `go test ./... -count=1 -timeout 20m`：全部包 `ok`（含 `tests/contracts`、`tests/architecture`、`tests/integration/v09`、`tests/poc/v09/services`），0 FAIL；新增 4 个 platform 集成测试及 `internal/transport` WebSocket 负向测试均通过。
- `npm run check`：**PASS（exit 0）**。在短路径完整副本 `C:\kscheck` 上执行：`go-format`/`go-mod-verify`/`go-mod-tidy`/controller+agent build/`go-tests`/`go-vet`/`staticcheck`、source-integrity、契约与 g2r02/g2r03、architecture、git-diff/staged 全部通过；Web 34 个测试文件/283 项全部 PASS，`tsc --noEmit` 与 Vite 生产构建 PASS。
  - 原始长路径直接执行同一命令时，`go-format` 因脚本一次传入 310 个绝对路径（约 33.6k 字符）触发 Windows CreateProcess 32,767 上限 `ENAMETOOLONG`；源码未变，短路径副本由 robocopy 生成并仅重定位，验证后已删除。长路径曾出现 1 次两个 Web 测试偶发失败（node-add/plugins），单文件重跑及短路径全量均为 283/283 PASS。
- `npm run check:security`：**PASS（exit 0）**；`npm audit --audit-level=high` 0 漏洞；`govulncheck ./...` 输出 "No vulnerabilities found."（同样在源码一致的短路径副本执行）。
- 未修改 DS 所属 `internal/{storage,nodes,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,servicegroups,publishing}/**`、`apps/web/**`、根配置/锁/公共契约；候选 OpenAPI 保持字节不变。

### 剩余风险 / 明确未交付

1. `apps/controller/main.go` 尚未加载 Agent PKI/监听器参数；默认运行存在路由但 enrollment/WSS 返回 503 `AGENT_TRANSPORT_UNAVAILABLE`。生产 mTLS listener 需配置 ClientCAs 与 `VerifyClientCertIfGiven`/`RequireAndVerifyClientCert`；e2e 使用测试内 CA 与 httptest TLS。
2. Agent Hello 仅接受 `platform=linux`（本机 Windows e2e 用协议客户端模拟 linux）；真实 Agent 安装器、arm64、1C1G 未验收。
3. AI 模型/BYOK 未配置：messages POST 503 `AI_SOURCE_UNAVAILABLE`，模型设置公开路由与实际外部模型调用未交付。AI 审批要求真人管理员，但单管理员可自批 AI 审批；root 执行仍提交为 `awaiting_approval`，由 tasks 的第二管理员审批兜底（自批 409）。
4. 插件 install/update/activate 无包源/WASM 运行时，明确 501；repository 为私有文件持久化；plugin-operations 只记录可执行/空操作；xray-manager 仅声明式门控，真实 Xray 运行时、用户、流量、导出未交付。
5. updates 无 release feed/planner/installer：check/plan 503、apply 501、jobs 为空；DR preview 无法在 HTTP 层验证 target controller identity 与 fencing，eligible 恒 false、job 501；backup verify 真实，但备份创建/列表/保留策略端点仍缺失。
6. 未访问云/VPS/生产/真实凭据，未推送远程、未合并 main、未启动其他 Agent；G2 契约、D01–D07/P1-C10、WASM/Eino 均未冻结。完整 v0.9 仍未完成，本包只交付 Wave 3 CX 范围。

---

> 以下为 Wave 1/2 及更早历史记录，保持原文。

2026-10-31用户明确授权本会话接替“完成 V09-5D-CX 开发包”（01a188b2-b181-7230-94a7-136c5356739b）。新唯一会话标识codex-03；沿用本包分支、允许目录、审查人与停止条件。接手HEAD为`ecb7cd405a0630d814580d1a6123d3e9a56c3e97`，原会话已中断；保留并核验其7项未提交文件，无重置/清理或他人目录写入。公共任务板/所有权登记仍由总控单写，本行记录本次用户授权的接替关系。

**ACTIVE，完整v0.9尚未完成。** 用户要求持续完成CX-01—08；先前b61ad15“无独立工作”的停止判断已撤回。这里是当前状态，逐增量SHA/实际验证/历史失败见[RESUMED.md](RESUMED.md)；早期HANDOFF/EVIDENCE/COMMITS为历史快照，不能覆盖这里的新实现或当作最新全量检查。

基线/main `8a7dd4707d2d7e142e137d098296788d2c80eddd`；分支feature/v09-platform-cx02；实际目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`。用户消息中的外置ks.worktrees目录不存在，实际目录与任务单/Git一致。审查codex-review-01+安全/契约/owner。2026-10-30，Asia/Shanghai。

| 子项 | 当前实现与可复验证据 | 尚未交付 / 具体依赖 |
|---|---|---|
| CX-01 | 首接口检查点2fc8dab；独立认证RFC6902候选/兼容测试；S01—S04及各模块接线说明持续补充 | 认证/事务/tasks/Proto/WSS共享差异尚无总控接收SHA，未改受保护API/Proto/gen/根锁/迁移序列，不冻结G2 |
| CX-02 | 持久初始化/口令/限速/Cookie/CSRF/Origin/epoch/Tgate；未挂载真实HTTP handler含用户/角色/grants；同Tx审计；凭据密文/暂存/服务使用永久防重放；只读受限opaque Bearer；持久审批与完整任务消费绑定。实际SQLite/HTTPS、重启撤权/回滚测试 | 正式迁移/安全决策/唯一任务实际消费尚未接收；MFA政策与实现未完成（要求MFA即拒绝启用）；凭据轮换/撤销任务与页面、scope/approval实际接线未完成 |
| CX-03 | TLS1.3+mTLS当前指纹/吊销、ControllerLinks/Agent连接循环、接入审批/签发/重装撤销持久化；本地私钥/CSR/证书与固定Root HTTPS注册客户端/handler。新增Ubuntu实际CSR→pending→SQLite重启→真实身份/同Tx审批审计回滚→issued持久材料→mTLS→显式grant吊销/重放拒绝；真实WSS/Proto/Ping组件另有原生证据 | WSS生产依赖、PKI profile/轮换/信任包/Node同Tx接线未接收；注册管理角色/当前listener profile仍测试输入，无完整安装器与Agent运行profile，独立Linux安装/arm64/1C1G未验收 |
| CX-04 | Agent副作用前claim/start持久化、去重/TTL/取消/live对账/UNKNOWN；输出落盘前强制注册过滤、持久生产序号/有界日志；实际WSS→临时文件→脱敏日志→重连/部分丢帧实验。审批外层rollback/重放负向 | DS Command缺完整Offer字段、ApprovalConsumer缺source/AI session等完整绑定、SubmitTx及日志序号/补读/ACK未接收；root/Shell/插件准入未获批，真实Linux统一链未交付 |
| CX-05 | 持久AI零资源/逐动作grant/组快照/三级模式；消息幂等/fence/UNKNOWN；未挂载POST/list有界分页及真实Cookie HTTPS SSE；BYOK文本HTTPS适配、当前持久推理闸；新增默认停用的持久模型设置/版本绑定/同Tx独立审计与重启使用；工作流冻结顺序任务/停用/UNKNOWN暂停；Eino隔离PoC | 正式外发政策/生产内容过滤器、设置公开路由与页面组合、统一任务/审批/通知接线未完成；实际外部模型/BYOK未验收，元数据/角色/过滤/模型部分仍明确double，Eino未定生产框架 |
| CX-06 | 签名源/清单/声明UI/私有工件/持久生命周期，默认disabled/实例隔离/禁用先封Host/UNKNOWN；WASM PoC。平台密文配置历史/CAS/一次性恢复源与同Tx审计、真实SQLite重启及unknown不重跑。可选Xray两模板/custom审查/管理API隔离；R37中央实例绑定UUID/REALITY材料与实际一次导出fence；R38持久用户ID/统计标识、CAS/tombstone、期望与已应用状态分离、当前中央引用检查、快照分页，实际SQLite改名同UUID/重启停用/审计回滚/事务前撤销通过；历史26.6.1真实TLS/Vision/Stats实验 | 两份已知官方外部发布物安全FAIL，不得安装/发布；WASM/Host/来源/密钥未准入。平台历史/用户存储正式Host与完整typed审计schema、唯一任务及真实应用配置/健康证据/页面未接收；双架构安装/接管/目标预检与恢复部署、实际用户停用断连/QR/typed遥测持久业务未完成；历史源delivered不是部署成功，核心未编入Xray |
| CX-07 | 更新签名/序列/平台/哈希、同FD暂存一次借用；新增真实文件备份/切换/持久阶段与回执，UNKNOWN永久锁、明确健康失败+兼容性核验才精确恢复本次备份；恢复连续Root/撤销并集/数据库外hold闸，SQLite旧快照不清隔离 | UPDATE/PKI新决策/离线解密/旧主控隔离/实际恢复事务未接收；服务/health/审计提交适配仍明确double，Linux真实更新/迁移/回退/灾备未验收。无默认放行verifier |
| CX-08 | Controller/Agent骨架及四个Linux交叉编译脚本，构建/组合/恢复接缝说明 | main尚无新模块接收SHA，未在入口组装未接收业务；正式中间件/路由组合、Agent安装与完整发行物尚未完成 |

已提交R33 `60e67e583670894232ddca0787a74e31d35ba165`（事务内身份/实际Linux注册），R32 a9f1069、R31 c680e37、R30 7d0d8d1。R34密文历史/一次性源已完成实际SQLite及Ubuntu plugins11/SQLite13顶层验证，精确证据LINUX-PLUGIN-HISTORY-EVIDENCE.json；正式Host/统一审计与真实部署未验收。全量check最近五套PASS（10:04:17Z，R33），R34模块/architecture与staticcheck PASS，security新PASS（10:18:13Z）；外部Xray FAIL另列。继续包内独立实现。

以上R33—34检查时间为历史；接替后R37提交`7f05242`，R38提交`4ef7e53`。最新实现/原生与Windows检查以RESUMED.md和[本次codex-03交接](TAKEOVER-codex-03.md)为准。R37原生auth67/SQLite15 PASS；R38原生可选包17 PASS/3已阻断二进制SKIP、SQLite15 PASS。最终全量check五组PASS17:38:25.159Z/security PASS17:38:15.568Z（上海2026-10-31）；源码逐一吻合最终原生证据，保护文件零差异。中央材料v2禁止直接降级旧auth，用户pending/unknown不冒充部署成功；AC-23仍NOT_ACCEPTED，完整包ACTIVE，未宣称所有子项完成或不存在其他独立工作。

**新增实际Linux组件证据**：本机已有Ubuntu24.04 WSL2、UID1000、amd64；12包212顶层测试运行PASS（含Agent重复）/3实际Xray二进制SKIP。真实WSS/Agent副作用与重连、SQLite旧快照外部隔离、私有文件/Sync分支均已执行。Agent/恢复旧fixture的0700准备及错误检查问题修正，未放宽生产门槛；首轮FAIL归档。逐项见LINUX-NATIVE-EVIDENCE.json和LOCAL-LINUX-VALIDATION.md。不是物理VPS/安装/arm64/1C1G或完整统一任务验收。

上述12包是R28历史完整扫描；R29—33为各变化包原生重跑，逐次独立哈希证据不覆盖旧记录。R33注册链在Linux实际完成，仍不等于所有13个现有包重新跑过或正式统一任务接线通过。

**Xray外部发布物安全FAIL**：26.6.1符号扫描55项；正式新版26.10.29独立下载/双哈希核验后扫描3项，未执行新版。两份均不得发布/安装。R21真实TLS/Vision Stats/进程重启功能实验证据保留，不当安全验收。精确pin/发现/复查命令见XRAY-ARTIFACT-SECURITY.md；生产Stats typed telemetry/持久历史仍未接收，不能通过TaskOutput/float64绕接缝。

实际SQLite组合固定DS源`bb8954988fe7b2f832edf291803529d7745e4f0a`，单独module/sum，不是共享接收SHA。R33十二项Windows PASS8.390s；Ubuntu对应十二项及Agent注册含旧21项共22项PASS，新增事务ctx绑定真实权限读者，首次审计原因码不匹配FAIL保留并修正固定映射。角色/profile/资源resolver及其他Runtime/Task/Model测试输入明确记录。R19真实WSS/Agent隔离包PASS9.323s，业务准入仍fixture。npm ci/tools:install本轮已PASS，依赖未变；根security无漏洞不覆盖可选外部Xray二进制。Linux四个骨架交叉编译R19已PASS，未作为安装验收。

总控已确认98f6d94d仅依赖边登记且已在基线；RecordTx/扩展Request/DS新SQL接缝不是共同接口。四份PRIV/WC/PKI-OPS/UPDATE完整正式原文缺失，只提交独立PROPOSED决策，未补写历史。缺项只暂停具体接线，继续可独立实现及本地负向验证。没有访问生产/VPS/云/真实凭据、远程推送、合并main或启动其他Agent。

AC-01—24与完整PoC仍未验收。Windows局部实验/交叉编译不能替代Linux amd64/arm64、1核1GB、外部供应商或真实灾备证据。最终停止必须给最新HEAD、对应检查及未完成清单；当前不声明完整v0.9交付。
