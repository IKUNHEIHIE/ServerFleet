# V09-5D-GM · gemini-02 进度

分支 `feature/v09-console-gm02`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-console-gm02`；基线 `8a7dd4707d2d7e142e137d098296788d2c80eddd`。

> **Wave 4（gemini-04）**：起始基线 `0f9a75e416a82a9589a29d4d9cf8a52bf0266066`（main，总控已挂载 Wave 3 全部路由）。本轮交付见文末「Wave 4 检查点（gemini-04）」一节。
> **Wave 5（gemini-05）**：起始基线 `3adf891f3155b6452f06f25df53fd20f553742ae`（main，Wave 4 复审合入）。本轮交付见文末「Wave 5 检查点（gemini-05）」一节。

## 检查点

| 检查点 | SHA | 范围 | 页面/接口 | 测试 | 阻塞 |
|---|---|---|---|---|---|
| GM-01 | 4155b9c | 应用壳、路由/导航、API 客户端、SSE、会话/权限上下文、登录/初始化/概览 | `/overview` ← `/healthz`、`/api/v1/nodes`、`/api/v1/tasks`、`/api/v1/audit/events`；登录 ← `POST /api/v1/auth/login`；刷新恢复 ← `GET /api/v1/auth/me` | `npm test --workspace @serverfleet/web` 207 PASS；`npm run build --workspace @serverfleet/web` PASS；`node scripts/web-boundaries.mjs` PASS | 后端管理 API 仍未实现：真实 Controller 仅 `/healthz`，登录/概览在真实环境会显示不可用 |
| GM-02/03 | f49e637 | 会话失效/恢复语义、W07 legacy 列表、真实节点列表 | `/nodes` ← `GET /api/v1/nodes?onlineOnly=`；富化视图 ← `GET /api/v1/nodes/{nodeId}`（候选详情，需显式切换） | 235 PASS；build PASS；boundaries PASS | 详情契约 D02–D04 未冻结；真实管理 API 未提供，浏览器验证待 GM-09 |
| GM-04 | ffb2a14 | 节点接入/详情、分组双视图与成员抽屉、服务组、用户/角色/授权/会话、审计、安全凭据 | `/nodes/new` ← enrollment tokens/enrollments；`/nodes/{id}` ← detail/identity/capabilities/diagnostics/traffic-quota/suspend/revoke；`/groups` ← groups/classifications/node-results/members；`/service-groups[/:id]` ← CRUD/members/scheduling/preview；`/users` ← users/roles/permissions/grants/sessions；`/audit`；`/settings` ← credentials/sessions | 253 PASS；build PASS；boundaries PASS | 无安装命令/接入历史/密码重置/角色 CRUD 端点；真实 API 未部署，浏览器验收待 GM-09 |
| GM-05 | 372ff55 | 任务中心/创建预演/审批/取消/恢复/SSE、双栏文件浏览/文件夹/重命名/删除/大小/上传元数据/跨节点传输 | `/tasks` `/tasks/:id` ← tasks/preview/approve/cancel/resume/executions/events；`/files` ← nodes/files/directories/rename/size/uploads/transfers | 259 PASS；build PASS；boundaries PASS | 无下载端点与上传字节通道；`/tasks` 路径在候选契约中重复；分批/金丝雀参数缺失；真实字节验收 BLOCKED |
| GM-06 | 5e2de69 | 监控/通知、配额编辑、DNS、证书、网关、发布向导 | `/monitoring` ← availability-monitors/traffic-risks/notifications；`/settings` 通知渠道；`/dns` ← providers/accounts/zones/records；`/certificates` ← internal distributions；`/gateways[/:id]` ← gateways/routes/checks/dispatch/provision；`/publishing` ← preview/publications；节点配额 ← traffic-quota PUT | 266 PASS；build PASS；boundaries PASS | 检查历史只返回 ok；无资源历史/公网证书端点；发布无端口/逐网关差异；网关路由无 gatewayId；真实供应商/节点验收 UNTESTED |
| GM-07 | a7be1b0 | AI 运维（资源授权/三级审批/流式消息）、工作流契约缺口页、插件中心、Xray 声明式插件界面 | `/ai` ← security-policy、`POST /api/v1/ai/sessions`；`/ai/:sessionId` ← session/approval-policy/grants/approvals(+decision)/messages/approval-events/tools/execute/resource-recommendations + SSE `…/events`；`/workflows`（无端点，BLOCKED 页）；`/plugins` ← plugins/repository/catalog(+refresh)/install/activation/update/DELETE/plugin-operations；`/plugins/xray` ← `GET /api/v1/plugins/xray-manager` 门控 + nodes/templates/users/traffic(+settings)/installations/configurations/deployments/jobs | 275 PASS；typecheck PASS；build PASS；boundaries PASS | AI 会话列表/关闭端点缺失、AI SSE 无事件数据契约；workflows 完全无契约；Xray 发行制品未发布（目录缺 commit/SHA-256 即“不可安装”）；真实 AI/插件验收待 GM-09 |
| GM-08 | 08a707a | 更新中心（检查/计划/一键更新/作业/回滚）、备份校验与灾难恢复、系统设置「系统更新」页签共用同一实现 | `/updates` 与 `/settings`（系统更新） ← `POST /api/v1/system/updates/check|plan|apply`、`GET …/jobs`、`GET …/jobs/{id}`、`POST …/jobs/{id}/rollback`；`/backups` ← `POST /api/v1/backups/{id}/verify`、`POST /api/v1/disaster-recovery/preview|jobs`、`GET …/jobs/{id}`；节点诊断已在 GM-04 的 `/nodes/:nodeId` 接入 | 282 PASS；typecheck PASS；build PASS；boundaries PASS | Agent 升级无端点（只显示缺口，不伪造分批升级）；备份记录列表/创建缺失；灾难恢复作业无列表；`verify` 返回 RecoveryPlan 而非校验结论 |
| GM-09 | 7a70ac2（验收桩+导航联动）+ 本记录提交 | 浏览器端到端验收：真实登录表单、刷新后 CSRF 锁定与重新登录、401/403/503、迟到响应丢弃、重复激活去重、插件入口即时联动、420×900 与 1280×900、键盘焦点与跳转链接、“仅 healthz”后端的诚实失败 | 127.0.0.1 验收桩托管 `dist` 并按候选契约响应；截图 22 张见 `evidence/`；完整步骤/限制见 `ACCEPTANCE.md` | 282 PASS；typecheck/build/boundaries PASS；桩上全部验收步骤通过 | 真实 Controller/供应商/Agent 未验收（本机无 Go）；上传字节、工作流、Agent 更新、备份记录等契约缺口仍在 |

## GM-02/03 交付细节

### 会话与权限（GM-02）

- 401：全局终止门控，清空身份并回到登录页；同一时刻只处理一次，不自动重试或循环。
- 403：只清当前资源，保留 Cookie 会话与导航；手动刷新再次 403 也不登出。
- 503：保留会话；初次加载显示可重试错误，静默刷新失败保留上次成功数据并显式提示“可能已过期”。
- 刷新恢复：`/auth/me` 成功补回身份；因契约不含 CSRF，写操作锁定并展示原因，不用会话 SID 伪造。
- 跨标签：BroadcastChannel 内存同步登录/退出/失效；CSRF 不写 localStorage/sessionStorage。
- BFCache/pageshow/visibility：暂停 SSE、复核身份、`viewEpoch` 自增使当前资源重新请求，旧响应按序号丢弃。
- 登录表单不持久化密码/令牌；退出请求带 `X-CSRF-Token`，失败时本地清除并提示服务器撤销未确认。

### 节点列表（GM-03 / W07）

- 默认兼容视图只消费 `GET /api/v1/nodes` 的已批准字段：`id/name/online` 与可空指标，`onlineOnly` 由服务端过滤。
- 适配规则：缺 `items`/非数组、非对象行、坏 `id`/`online`、重复 `id` 整次失败且无部分表格/计数；name 空/非法回退 ID 并固定提示；region/provider 非法显示缺失。
- 指标：离线一律不显示历史百分比；在线有限 0–100（含 0/100）显示并标“无采样时间”；`null`→无读数，非有限→读数非法，越界→超出范围；不夹取、不补 0、不补造采样时间。
- 搜索仅作用于已加载 name/id；服务端空集与本地无命中是不同文案；行不可聚焦、无详情/执行入口、无行内按钮。
- 富化视图需用户显式切换，才逐节点请求候选详情；若任一详情缺失/重复/ID 不匹配则整次失败。列表 DTO 无采样时间，CPU/内存不会被显示为当前读数。
- 保留原有 179 个测试与富化组件；新增 legacy/rich 适配测试与页面集成测试。

## GM-07 交付细节

### AI 运维（FR-AI-SCOPE-005 / FR-AI-APPROVAL-006）

- `/ai` 入口显示服务端安全策略（默认拒绝、服务端强制），只能新建会话；候选契约没有会话列表端点，因此不伪造历史会话（缺口见 `SHARED-CHANGES.md` §9）。
- 会话页所有数据来自 `GET /api/v1/ai/sessions/{id}`：`scopeRevision`、`resourceGrants`、服务端计算的 `allowedNodeIds`；授权/撤权后静默刷新，页面不空白、不本地推算权限。
- 添加授权必须由管理员从**服务端返回的节点/服务组列表**选择具体 ID；AI 建议只展示原因与所需能力，批准时仍要管理员显式选 ID，未选则拒绝提交。
- 三级审批：模式卡片切换走 `PATCH …/approval-policy`；`full_access` 必须勾选风险确认并选择 15/30/60 分钟，提交 `userConfirmedFullAccess: true`；页面固定提示“完全访问不扩大资源范围”。
- 工具审批：`POST …/approvals` 返回 `canonicalArgumentsHash` 与目标快照后，才允许一次性 `decision`；执行走 `POST /api/v1/ai/tools/execute` 并携带当前 `scopeRevision`，前端不自造执行成功。
- SSE：只渲染投影字段（event/id/toolName/state/message/progressPercent）；测试断言原始 `output` 中的 `SECRET-AI-OUTPUT` 从不出现在 DOM。终止（401/403/404）显示错误且不循环重连。
- 消息发送携带 `clientMessageId` 与当前 `scopeRevision`；只展示服务端 `redactedContent`。

### 插件中心与 Xray（FR-PLUGIN-004 / FR-PLUGIN-XRAY-001）

- 来源：`GET/PUT /api/v1/plugins/repository`，第三方 Fork 必须填 GitHub URL 并显示为非官方；保存后提示“不会自动替换已安装插件”。
- 目录：只读元数据；缺 `sourceCommit` 或 `artifactSha256` 的条目显示“缺少 SHA-256 / 不可安装”，确认按钮禁用，不伪造下载成功。
- 安装：权限逐项勾选后才真正提交（未勾全时点击确认不发请求），提交体固定为 `version/sourceCommit/expectedSha256/acceptedPermissions/enableAfterInstall`。
- 启停/更新/卸载全部走服务端异步操作，返回 `PluginOperation` 后弹出操作详情，明确“只有 succeeded 才算完成，失败时旧版保持可用”。
- 导航入口只在 `GET /api/v1/plugins` 返回 `id==="xray-manager" && enabled && status==="enabled"` 时出现；测试覆盖“已安装但停用 → 入口消失”。
- `/plugins/xray` 为声明式渲染：标签/标题/说明来自核心内 manifest 常量，渲染器不执行任何插件脚本；未启用时只显示“插件未启用 + 前往插件中心”，不渲染任何控制台。

### 工作流

- `g2-merged-candidate.yaml` 无 `/api/v1/workflows*`；页面为诚实 BLOCKED 页，列出已批准但待接线的能力，不提供本地规则 CRUD、假通知或模拟执行成功。

## 结构说明

- `apps/web/src/app/router.ts`：hash 路由、路径参数匹配、查询串构建；`app/navigation.ts` 保留完整 v0.9 功能地图入口。
- `apps/web/src/lib/api/client.ts`：`credentials: include`、Cookie 会话、固定错误分类、CSRF 头、超时/中止、401 全局事件；不把非 JSON 200/404 当 DTO，不回显原始错误体。
- `apps/web/src/lib/sse/event-stream.ts`：有状态 SSE 解析（跨 chunk 保留 id/event/data）、401/403/404 终止、503/断线退避重连、Last-Event-ID；`use-event-stream` 在页面隐藏时暂停。
- `apps/web/src/app/session.tsx`：Cookie 会话恢复、401 失效、403 不清会话、503 保会话、跨标签 BroadcastChannel（仅内存传递 CSRF，不落 localStorage）、BFCache/pageshow/visibility 恢复、`viewEpoch` 供页面丢弃旧数据。
- 登录响应中的 CSRF 仅存内存；刷新后 `/auth/me` 不返回 CSRF，写操作显式锁定并提示重新登录。

## 契约缺口（已确认，需 CX 协调）

1. `GET /api/v1/auth/me` 只返回 `Identity`，不返回当前会话绑定的 CSRF 令牌；刷新后同源写操作无法安全恢复。已在 UI 以“安全写入已锁定”明确说明，不用会话 SID 伪造或新增裸路由。
2. `g2-merged-candidate.yaml` 无工作流（workflows）管理操作；GM-07 的工作流页面在契约补齐前只能显示“契约缺口”，不能伪造规则 CRUD。精确请求见 `SHARED-CHANGES.md`。

## GM-08 交付细节

### 更新中心（FR-UPDATE-005 §12）

- 检查更新：`POST /api/v1/system/updates/check`（stable/beta），只显示服务端返回的当前/最新版本、发布时间、签名验证、平台兼容与检查时间；检查不安装、不重启、不静默升级。签名未通过时显式标红并说明禁止安装。
- 升级计划：`POST /api/v1/system/updates/plan`，默认 `dryRun: true`；展示 `planId`、合格性、迁移可逆性、逐项预检（名称/结果/原因）与预计影响。计划不合格或任一预检未通过时禁止创建作业。
- 一键更新：`POST /api/v1/system/updates/apply`，提交体固定 `{planId, confirmation, backupMode:"required"}`；`confirmation` 必须逐字输入目标版本，且必须勾选风险确认，否则按钮禁用。每次提交带 `Idempotency-Key`。
- 备份门控：计划 `backupRequired` 为真而作业历史中没有任何服务端返回的 `backupId` 时，显示“缺少可验证备份”并禁止提交（备份记录契约缺失，见 SHARED-CHANGES §4/§10），不伪造备份成功。
- 作业与历史：`GET /api/v1/system/updates/jobs` 表格 + `GET …/jobs/{id}` 详情（阶段、错误码、备份、升级后健康检查、发起人、时间）；`rollbackEligible` 为真才允许回滚，回滚必须填原因，走 `POST …/jobs/{id}/rollback`。202 一律表述为“已受理”，完成状态以作业详情为准；`manual_recovery_required` 明确说明需要人工恢复。
- Agent 升级：候选契约无端点，页面显示契约缺口并跳转到批量任务/节点列表，不提供假的“选择节点分批升级”。
- 系统设置 → 系统更新：复用同一 `ControllerUpdatePanel`，因此“检查更新/一键更新”与更新中心共用同一份状态和历史，不存在第二套更新配置。

### 备份与恢复（FR §12.3 / 灾难恢复演练）

- 明确告知没有备份记录列表/创建端点：不显示伪造的备份表格、不提供“立即备份”，backupId 由服务端（如更新作业 `backupId`）或管理员提供。
- 校验备份：`POST /api/v1/backups/{id}/verify`（无请求体），按返回的 `RecoveryPlan` 显示备份/目标主控/期望版本/期望节点/密钥引用与 `checks[]`；无检查项时明确“不能据此认定备份可用”。
- 恢复预演：`POST /api/v1/disaster-recovery/preview` 提交完整计划；`oldControllerFenced` 必须由管理员显式勾选并提示这是必要声明而非充分隔离证据；`recoveryKeyReference` 只填引用，页面提示严禁明文且不保存。
- 创建作业：只有在已预演、fenced 为真、目标主控非空且预演检查全部通过时才允许 `POST /api/v1/disaster-recovery/jobs`；返回 `AsyncOperation` 后显示状态/阶段/审计 ID，`GET …/jobs/{id}` 刷新。任一检查未通过显示“预演未通过”并禁用提交。
- 说明恢复成功不等于业务恢复（还需控制面健康、数据库可读写、Agent 基本功能、证书有效性）；作业无列表端点，刷新页面后不本地保留历史。

## 下一步

GM-01—GM-09 全部检查点已提交。最终 HEAD 与未完成清单见 `ACCEPTANCE.md`；后续工作取决于总控是否接收共享接口提案（`SHARED-CHANGES.md`）以及 Controller 管理 API 的实现进度。

## Wave 3 检查点（gemini-02 · 起始基线 `98f39e0` · 检查点提交见 `git log` 本节末）

| 任务 | 内容 | 结果 |
|---|---|---|
| GM-W3-01 | 对照真实 Controller 9 个域的 `http.go` 响应结构，在 `apps/web/src/lib/api/compat.ts` 实现双兼容解析（分页平铺 `nextCursor`、任务 `taskId/jobId` 双形状、executions 精简条目、DNS providers `{code,name}`、zone sync 单对象、transfer items 字段别名）；严格校验：未知形状/缺失必需字段抛 `ApiError(kind:"invalid")`，`page.limit` 缺失为 `null`，绝不伪造默认成功 | 306 项单测 PASS（新增 compat 23 项） |
| GM-W3-01 | 请求侧对齐真实后端：users PATCH 带 `If-Match` 乐观锁；users/grants 创建带 `Idempotency-Key`；DNS 凭据改 `credentialRef` 引用（去除明文 apiToken）、setDefault 改 POST；notifications 凭据改 `POST {secretRef}`、test 幂等键入 body；UI 权限键经 `lib/permissions/backend-aliases.ts` 显式映射到真实权限目录（`tasks.approve`/`nodes.write` 等，AdminOnly 的 `host.root` 不映射） | typecheck/build PASS |
| GM-W3-01 | 前端补齐真实契约能力：服务组编辑器增加成员节点选择（真实后端按成员集合授权，空集合 403）；网关路由编辑器增加“所属网关”选择（真实创建强制 `gatewayId`）；任务命令字段客户端校验单一动作 ID（自由文本触发后端 500）；audit 的 `actorType` 过滤映射为后端 `source` | 见 SHARED-CHANGES §12 |
| GM-W3-02 | 新增 `tests/browser/v09/run-acceptance.mjs`（自动化桩验收，等价 Wave 1 GM-09 手工流程）：登录→概览→刷新 CSRF 锁定→重新登录→节点 0%/离线规则→任务 SSE（游标推进、秘密不渲染）→503 保会话→403 不登出→会话撤销 401→移动视口截图；`--out` 指定证据目录 | **16/16 PASS** |
| GM-W3-02 | 新增 `tests/browser/v09/run-live-controller-acceptance.mjs`：构建并启动真实 `apps/controller`（`-db 临时 SQLite WAL -listen 127.0.0.1:<port> -origin https://... -tls-cert/-tls-key`；后端强制直连 HTTPS），同源 TLS 托管 `apps/web/dist` 并原样转发 `/api/*`（SSE 流式透传）；以 node:sqlite 承担本机安装器（PrepareSetup 同算法写入 setup token 摘要、播种 2 个节点夹具）；Headless Edge 跑通：Bootstrap 初始化→自动登录→刷新 `/auth/me` 恢复 CSRF→零范围隔离→创建操作员+双人授权→分组/服务组与 expectedRevision 成员更新→root 任务预演/提交/自批 409/异人审批 200/SSE→DNS（providers/credentialRef 建号）→网关（安装任务 202、路由创建按服务端 Checker 缺口诚实 503）→通知（空态诚实 + 通知策略真实 PUT）→审计（bootstrap/user.create/grant.create/tasks.submit） | **32/32 PASS** |
| GM-W3-02 | 修复 `tests/browser/v09/stub-server.mjs`：Wave 1 集成移动目录时双重编码损坏（Node SyntaxError，桩验收不可运行），已按原逻辑以干净 UTF-8 重写；`fixtures.mjs` 有损乱码登记待总控决策 | 见 SHARED-CHANGES §12.4 |

### Wave 3 证据

- 桩验收截图：`evidence/stub/stub-01..09*.png`（登录、概览、写锁定、节点、SSE、503、403、会话撤销、移动端）。
- 真实后端截图：`evidence/live/live-01..15*.png`（bootstrap、概览、零范围、授权、分组、服务组、任务等待审批、自批 409、异人审批+SSE、DNS、网关、监控、审计）。
- 后端协调建议与诚实缺口：`SHARED-CHANGES.md` §12（任务 command 500、路由 Checker 503、服务组成员授权、路由 gatewayId、渠道从零创建、PrepareSetup 安装器形态）。

### Wave 3 边界说明

- 真实后端验收未注入 `Dispatcher`/`GatewayChecker`/`DNSProvider`（`apps/controller/main.go` 组合即如此）：任务审批后停留在 queued（无 Agent 派发）、路由创建诚实 503、DNS 账号 credentialStatus 为待验证。均为组合运行时的真实行为，界面如实呈现，未伪造。
- 监控可用性探测（`/api/v1/availability-monitors`）与任务 `resume` 在当前 `main` 基线尚未挂载（DS-W3/CX-W3 范围），界面显示诚实错误分类；不阻塞本轮验收。

Wave 3 全部工作收束于基线 `98f39e0` 之上的单一提交 `feat(web): adapt console to real controller contract and add live browser acceptance`（分支 feature/v09-console-gm02 HEAD，本文件与该提交同时落盘，故不写自身 SHA）；工作树干净，未修改任何 Go 源码、`package.json`、`vite.config.ts` 或公共契约文件。

## Wave 4 检查点（gemini-04 · 起始基线 `0f9a75e`）

| 任务 | 内容 | 结果 |
|---|---|---|
| GM-W4-01 | 扩展 `tests/browser/v09/run-live-controller-acceptance.mjs`（32→58 步）：在真实 Go Controller + SQLite WAL + HTTPS 上实测 Wave 3 新增接线模块——31/32/33 节点详情（身份/能力/诊断）与流量配额 PUT/GET、34/35/36 监控与流量风险（创建/暂停/风险源）、37 证书分发列表、38/39/40 发布向导（调度资格诚实门控 + preview eligible + 三阶段发布计划与列表）、41–46 AI（安全策略、零授权会话、node-alpha 会话授权、审批策略切换、一次性审批创建+决策、受控执行 202）、47/48 插件中心（repository/catalog/已安装 + 刷新目录诚实 501）、49 Xray 门控、50–52 更新中心（503 诚实 + 作业空态）、53–55 备份校验（VACUUM INTO 真实备份夹具）与恢复预演 fail-closed、56 新域审计动作落盘；管理员授权新增 `monitoring.*`/`quota.*`/`certificates.read`/`publishing.*`/`tasks.command`/`host.patch` | **58/58 PASS**；截图 `evidence/live/live-16..32-*.png` |
| GM-W4-02 | 真后端联调发现的契约差异在 `apps/web` 对齐（详见 SHARED-CHANGES §13）：节点详情 404 → 子资源独立加载 + 列表摘要回退；生命周期请求去除 `expectedRevision`（真实后端 400 `revision_not_supported`）；监控 POST/PATCH 白名单请求体（真实后端 `DisallowUnknownFields`）；`compat.normalizeInstalledPlugin/normalizePluginList`（真实 `phase` → `status` 归一）；客户端兼容嵌套 `error.code`；`TrafficQuota` 重置日/时区/来源改可选并诚实显示「未返回」；`NodeCapabilities` 协议版本改可选；目录刷新失败补错误提示；备份校验文案去除硬编码 202 | Vitest 36 文件 / **316 项 PASS**（新增 10 项）；typecheck PASS |
| GM-W4-03 | 新增 `AC-EVIDENCE-WAVE4.md` 逐条登记 AC-01~24 的页面、Vitest、Stub/Live 步骤与截图；更新 `STATUS.md`/`SHARED-CHANGES.md`/`ACCEPTANCE.md` | 完成 |

### Wave 4 验证结果

- `node tests/browser/v09/run-acceptance.mjs` → **16/16 PASS**（Stub 契约与退化场景回归）。
- `node tests/browser/v09/run-live-controller-acceptance.mjs` → **58/58 PASS**（真实 Controller + SQLite WAL + HTTPS）。
- `npm run check` / `npm run check:security` → PASS（本轮收尾门禁，输出见提交说明）。

### Wave 4 边界说明

- 组合运行时未注入的适配器按服务端语义诚实失败并断言：更新源/计划器 503、插件目录源 501、网关 Checker 缺失 503、恢复预演 `target_identity_not_observed`/`fencing_evidence_unavailable` 未通过且恢复作业保持禁用。
- 监控创建使用公网样式目标：服务端 SSRF 策略（`internal/monitoring/ssrf.go`）拒绝 `.internal` 等内网后缀，第一轮联调以 `invalid_request` 实证后调整，前端可见具体错误码。
- AI 消息生成未接模型（`ErrModelUnavailable`）；AI 会话的 approval-events 与 recommendation decision 路由在当前 `server.go` 未注册，界面显示诚实加载失败（登记 SHARED-CHANGES §13.3）。
- 未修改任何 Go 文件、公共契约、`package.json`/锁文件或 CI 配置；`apps/web/dist` 为构建产物不入库。

## Wave 5 检查点（gemini-05 · 起始基线 `3adf891`）

| 任务 | 内容 | 结果 |
|---|---|---|
| GM-W5-01a | 移除 `#/workflows` 的 `BLOCKED：无工作流接口` 静态占位，新增 `apps/web/src/lib/api/endpoints-workflows.ts`（`workflowsApi`：list/create/get/update/trigger 对接 Wave 4 合入的 `GET/POST /api/v1/workflows*`、`PATCH …/{id}`（`expectedRevision`）、`POST …/{id}/runs`）；重写 `workflows-page.tsx`：工作流列表（标识/启停/触发器+匹配/版本/步骤摘要）、创建表单（触发器标识、目标节点 ID、动作 `tasks.command`/`host.patch`/`host.root`、每行 `key=value` 参数、`humanApproval` 开关）、启停切换、手动触发（显示运行状态与生成的 `tsk_` 任务 ID）、详情弹窗（工作流 + 运行记录/关联任务 ID）；`backend-aliases.ts` 对齐后端键 `workflows.read`/`workflows.write`（UI 键 `workflow.read`/`workflow.write`），无写权限时诚实禁用写操作 | 新增 `workflows-page.test.tsx` 6 项（创建契约体/expectedRevision 启停/触发任务 ID/matched=false 诚实/详情任务 ID/只读禁用）+ `backend-aliases.test.ts` 2 项，全部 PASS |
| GM-W5-01b | `client.ts` 新增 `requestRaw`（`application/octet-stream` + `X-CSRF-Token` + `X-Requested-With: ServerFleetWeb`，字节体独立复制）；`filesApi.uploadContent(nodeId, uploadId, body, offset=0)` 调用 `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content?offset=0`；重写 `files-page.tsx` 的 `handleFiles`：单文件上传会话（`POST …/uploads`，带浏览器 crypto.subtle 计算的 SHA-256）→ 立即读取文件字节上传字节流 → `POST …/complete` 触发服务端字节数与 SHA-256 校验；`200 completed` 显示完成并刷新目录，`503 unavailable` 诚实显示「文件字节流（X B）已上传并通过服务端 SHA-256 校验；当前节点文件传输通道未注入（503 unavailable）」；页面能力边界说明同步更新（下载端点仍无，不伪造） | `files-page.test.tsx` 4 项 PASS（含 PUT 字节体/头断言与 503 诚实分支） |
| GM-W5-01c | `monitoringApi` 新增 `history(monitorId, limit)`（`GET …/availability-monitors/{id}/history`）与 `probe(monitorId)`（`POST …/probe`）；移除从未在真实后端存在的 `checks` 端点用法；监控页「检查历史」改为真实历史弹窗（检查时间/状态 up·down·unknown/延迟/状态码/错误摘要，无记录诚实显示「暂无检查记录」），新增「立即探测」按钮：成功显示服务端探测结果，失败诚实显示 HTTP 状态与错误码（如 `503 prober_unavailable`）；`tests/browser/v09/fixtures.mjs` 补齐 history/probe、文件字节通道、发布 run/rollback 与工作流 CRUD 桩路由 | `monitoring-page.test.tsx` 5 项 PASS（真实历史渲染/空态/探测成功/503 诚实/请求断言） |
| GM-W5-01d | `publicationsApi` 新增 `run(publicationId)` 与 `rollback(publicationId, stage)`（真实后端要求 body `{stage}`，比任务单描述多一个必填参数）；发布记录操作列新增「执行发布」，阶段详情弹窗新增「执行发布」与逐阶段「回滚此阶段」；`502 stage_failed`/`rollback_failed` 诚实展示并刷新列表与详情的服务端阶段状态 | `publishing-page.test.tsx` 4 项 PASS（执行/回滚请求体、502 诚实与阶段刷新） |
| GM-W5-02 | `run-live-controller-acceptance.mjs` 58→**70 步**：57 工作流列表真实读取、58 UI 创建工作流 201、59 启停 PATCH×2（revision 2/3 服务端核验）、60 手动触发 202 + `tsk_` 任务 ID、61 详情弹窗运行记录、62 工作流任务在统一任务中心可见（无绕行通道）、63 检查历史真实空态、64 立即探测诚实 503 `prober_unavailable`、65 文件字节流上传（DataTransfer 注入真实文件字节 → PUT content `bytesReceived` 服务端核对 → complete 503 诚实展示）、66 发布执行诚实 502 `stage_failed`（dns 阶段持久化 failed）、67 阶段回滚诚实 502 `rollback_failed`、68 Wave 5 审计闭环（`workflows.save/trigger/task.submit`、`files.upload_create`、`publishing.stage/run/rollback`）；管理员授权新增 `files.write`/`workflows.read`/`workflows.write` | **70/70 PASS**；新截图 `evidence/live/live-33..38-*.png` |
| GM-W5-03 | 更新 `STATUS.md`/`ACCEPTANCE.md`/`AC-EVIDENCE-WAVE4.md`（Wave 5 增量）；顺带修复 `node-add-page.test.tsx` 既有负载竞态（批准按钮在异步面板内，`getByRole` 改 `findByRole`，断言语义不变）与 `plugins-page.test.tsx` 中断言旧 BLOCKED 占位的用例（改为断言真实接口失败时的诚实错误态） | 完成 |

### Wave 5 验证结果

- `node tests/browser/v09/run-acceptance.mjs` → **16/16 PASS**（exit 0）。
- `node tests/browser/v09/run-live-controller-acceptance.mjs` → **70/70 PASS**（真实 Controller + SQLite WAL + HTTPS；首轮 69/70，唯一失败为脚本读取被页面同步清空的 file input，修正读取时机后全绿）。
- `npm run check` → PASS（Go 全量测试 PASS；Web 37 文件 / **329 项** PASS，较 Wave 4 +13；typecheck/build/contracts/policy PASS）。
- `npm run check:security` → PASS（npm audit 0 漏洞；govulncheck No vulnerabilities found）。

### Wave 5 边界说明

- 组合运行时未注入监控探测器（`NetProber`，DS Wave 5 交付、总控接线）、文件传输通道（`LocalStagingTransport`）与外部发布器：立即探测 503 `prober_unavailable`、上传 complete 503 `unavailable`（字节已落暂存并通过 SHA-256 校验，界面如实区分“校验通过”与“未送达远端”）、发布 run 502 `stage_failed`、回滚 502 `rollback_failed`。均为服务端 fail-closed 语义，验收断言诚实错误。
- 工作流手动触发提交真实任务（`tasks.command` 标准级、`RequireApproval: false`）；`humanApproval: true` 的步骤会进入任务审批流（由任务中心处理），本轮 E2E 未覆盖该分支（属任务域既有验收范围）。
- 服务器端 `POST …/workflows/{id}/runs` 在事件与触发器不匹配时返回 `{matched:false, run:null}`（202），页面如实显示“未创建运行”；单测覆盖。
- 未修改任何 Go 文件、OpenAPI/Proto、`package.json`/锁文件或 CI 配置。
