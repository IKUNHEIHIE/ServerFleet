# GM-W4-03 · 前端与浏览器端到端 AC-01~24 证据盘点（gemini-04 · Wave 4）

- 会话：`gemini-04`（接续 `V09-5D-GM` Wave 4）；分支 `feature/v09-console-gm02`
- 起始基线：`0f9a75e416a82a9589a29d4d9cf8a52bf0266066`（main，含总控 Wave 3 组合接线）
- 验收命令：
  - Stub 契约验收：`node tests/browser/v09/run-acceptance.mjs` → **16/16 PASS**
  - 真实 Controller + SQLite WAL + HTTPS 验收：`node tests/browser/v09/run-live-controller-acceptance.mjs` → **58/58 PASS**
    （步骤 01–30 为 Wave 1–3 全链路回归；步骤 31–56 为本轮新增的 Wave 3 接线模块实测）
- 截图证据：
  - Stub：`evidence/0*..2*-…png`（登录/概览/写锁定/节点/任务 SSE/503/403/会话撤销/移动端等 22 张）
  - Live：`evidence/live/live-01..32-*.png`（bootstrap→授权→双人审批→SSE→DNS→网关→监控→审计→节点详情/配额→监控创建→证书→发布→AI→插件→Xray 门控→更新→备份恢复→新域审计）
- Vitest：36 个测试文件 / 316 项测试全部 PASS（`npm test --workspace @serverfleet/web`）
- 诚实边界原则：真实 Controller 未注入的适配器（更新源、插件目录源、发布器、恢复执行器、Prober、Agent 派发器、nginx Checker）一律按服务端语义诚实失败；本盘点不把「诚实 503/501」计为功能缺失失败，但明确标注为外部/集成边界。

## 逐条证据映射

图例：**页面** = 正式路由页面；**Vitest** = 关键单测文件；**Stub** = `run-acceptance.mjs` 步骤/截图；**Live** = `run-live-controller-acceptance.mjs` 步骤编号与截图。

### AC-01 节点接入、身份与生命周期
- 页面：`#/nodes`、`#/nodes/new`、`#/nodes/:nodeId`
- Vitest：`nodes-page.test.tsx`、`node-detail-page.test.tsx`（suspend/revoke 只提交 reason；详情 404 回退列表摘要）、`node-add-page.test.tsx`、`node-loader.test.tsx`
- Stub：节点列表 0/100 指标规则（stub-04）
- Live：05 零授权隔离、10 授权后可见/未授权隔离、31 身份/能力/诊断面板（`live-16`）、33 流量配额 PUT/GET（`live-17/18`）
- 边界：真实 Agent 入网/心跳/吊销后重连需 Agent 实机（UNTESTED）；`GET /api/v1/nodes/{id}` 详情路由真实 Controller 未提供，页面已按列表摘要诚实回退（SHARED-CHANGES §13.1）。

### AC-02 普通节点分组
- 页面：`#/groups`
- Vitest：`groups-page.test.tsx`
- Live：11 创建分组、12 expectedRevision 成员更新（`live-07`）
- 负向：未授权 nodeId 不出现在列表（Live 05/10）；取消不保存由成员抽屉关闭路径覆盖（unit）。

### AC-03 文件互传
- 页面：`#/files`
- Vitest：`files-page.test.tsx`
- Stub：文件页真实读桩（ACCEPTANCE §4）
- 边界：上传字节流/下载端点未入契约（SHARED-CHANGES §6）；真实字节验收 BLOCKED，页面不提供伪造下载/完成按钮。

### AC-04 服务组
- 页面：`#/service-groups`、`#/service-groups/:id`
- Vitest：`service-groups-page.test.tsx`
- Live：13 创建（成员节点选择）、14 expectedRevision 成员更新（`live-08`）
- 服务端按成员集合授权（空集合 403）已在编辑器内显式选择成员。

### AC-05 批量任务
- 页面：`#/tasks`、`#/tasks/:id`
- Vitest：`tasks-page.test.tsx`、`sse/event-stream.test.ts`
- Stub：05/06 任务 SSE 游标推进、秘密不渲染（stub-05）
- Live：15 预演要求审批、16 提交、17 planDigest 快照、18 自批 409、20/21 异人审批 200、22 SSE 实时事件（`live-09/10/11`）
- 边界：分批/金丝雀参数未入契约（SHARED-CHANGES §5）；无 Agent 派发器，审批后停留 queued（组合运行时真实行为）。

### AC-06 Nginx 网关
- 页面：`#/gateways`、`#/gateways/:id`
- Vitest：（网关面板在 gateways-page；无独立负面断言文件）
- Live：27 安装任务 202（`live-13`）、28 路由创建诚实 503 gateway_unavailable（Checker 未注入）
- 边界：`nginx -t` 健康检查与实机安装需 Linux 实机（UNTESTED）；调度资格由服务端声明，发布向导据此门控（Live 38）。

### AC-07 DNS 管理
- 页面：`#/dns`
- Vitest：`dns-page.test.tsx`、`compat.test.ts`（providers/zone sync 双形状）
- Live：25 providers 渲染、26 credentialRef 建号且凭据不回显（`live-12`）
- 边界：真实 Cloudflare API 未连接（凭据状态为待验证）。

### AC-08 公网证书与内部 TLS
- 页面：`#/certificates`
- Vitest：`certificates-page.test.tsx`
- Live：37 内部分发列表诚实空态（`live-20`）
- 边界：公网 ACME 端点未入契约，页面显示契约缺口（BLOCKED）；证书签发/轮换需服务组成员与真实 PKI 流程（本轮未触发）。

### AC-09 监控告警/QQ Bot/SMTP
- 页面：`#/monitoring`、`#/settings`（通知渠道）
- Vitest：`monitoring-page.test.tsx`（暂停 PATCH 白名单体）
- Live：29 通知渠道诚实空态、29b 通知策略真实 PUT、34 流量风险源、35 创建监控、36 暂停（`live-19`）
- 边界：QQ Bot/SMTP 真实投递需外部平台（UNTESTED）；通知渠道从零创建 UI 缺口见 SHARED-CHANGES §12.2。

### AC-10 AI 会话授权
- 页面：`#/ai`、`#/ai/:sessionId`
- Vitest：`ai-pages.test.tsx`
- Live：41 服务端安全策略（默认拒绝/服务端强制）、42 会话默认零授权（`live-23`）、43 授予 node-alpha（注册工具 `tasks.command` 交集，`live-24`）
- 服务端授权负向（未授权 nodeId 拒绝、旧 scopeRevision 重放）由 `internal/ai` 集成测试覆盖；前端不自算权限。

### AC-11 AI 三级审批
- 页面：`#/ai/:sessionId`（审批策略/工具审批页签）
- Vitest：`ai-pages.test.tsx`（full_access 确认流/限时）
- Live：44 automatic↔per_action 切换、45 一次性审批创建+决策（canonicalArgumentsHash）、46 受控执行经 tasks.Service.Submit 202 queued（`live-25/26`）
- 边界：AI 模型生成未接（ErrModelUnavailable 诚实 503）；root Shell 强制真人审批由后端强制（`ai.go`），前端不提供豁免入口。

### AC-12 插件平台
- 页面：`#/plugins`
- Vitest：`plugins-page.test.tsx`（目录缺 SHA-256 不可安装、停用后入口消失）、`compat.test.ts`（phase→status 归一）
- Live：47 来源/目录/已安装三面板真实读取（`live-27`）、48 刷新目录诚实 501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE`
- 边界：Xray 发行制品未发布（无 commit/SHA-256 条目），真实安装/热启停 UNTESTED。

### AC-13 更新中心
- 页面：`#/updates` 与 `#/settings`「系统更新」页签（同一 `ControllerUpdatePanel`）
- Vitest：`updates-page.test.tsx`
- Live：50 检查更新诚实 503 `UPDATE_SOURCE_UNAVAILABLE`、51 计划生成诚实 503 `UPDATE_PLANNER_UNAVAILABLE`、52 作业历史空（`live-29`）
- 边界：真实发布源/签名验证/安装与回滚需发布基础设施（UNTESTED）；备份门控（backupRequired 无 backupId 禁止 apply）由 unit 覆盖。

### AC-14 工作流、用户/权限、审计
- 页面：`#/workflows`（诚实 BLOCKED 页）、`#/users`、`#/audit`
- Vitest：`users-page.test.tsx`、`settings-audit.test.tsx`
- Live：06/07 创建操作员、08/09 资源授权、30 审计权威记录（`live-15`）、56 Wave 3 新域动作（monitoring.update/quota.policy/publishing.plan_create/ai.sessions.*）落盘（`live-32`）
- 边界：工作流 HTTP 契约缺失（SHARED-CHANGES §2，CX Wave 4 范围）；页面不提供本地规则 CRUD。

### AC-15 服务发布向导
- 页面：`#/publishing`
- Vitest：`publishing-page.test.tsx`
- Live：38 向导按服务端调度资格诚实门控（`live-21`）、39 `POST /api/v1/publishing/preview` eligible=true 且目标快照含 node-alpha、40 `POST /api/v1/publishing/publications` 202 queued + 三阶段计划 + 列表展示（`live-22`）
- 说明：38 的 UI 门控与 39/40 的真实端点调用均发生在同一浏览器会话（同源 fetch 带 Cookie/CSRF）；未伪造任何预检结果。
- 边界：发布器（DNS/证书/网关 stage 执行）未注入 → 计划停留 queued，不声称发布成功。

### AC-16 服务可用性监控
- 页面：`#/monitoring`（服务监控页签）
- Vitest：`monitoring-page.test.tsx`
- Live：35 创建 HTTP 监控（SSRF 边界内公网样式目标）、36 暂停
- 边界：探测历史/按需探测端点在当前基线未挂载（DS Wave 4 范围）；`.internal/.local` 目标被服务端 SSRF 策略拒绝的行为已在调试轮次中实证（保存失败 invalid_request），前端如实展示错误码。

### AC-17 安全批量执行补充
- 页面：`#/tasks`（预演级别说明）
- Live：15 `requiresApproval=true`（host.root）、18 自批拒绝
- 边界：预演级别/熔断阈值/任务有效期字段未入契约（SHARED-CHANGES §5）；「取消不冒充回滚」由任务页文案与状态机保证（unit）。

### AC-18 节点恢复/主控灾备
- 页面：`#/backups`、`#/nodes/:id`（连接诊断）
- Vitest：`backups-page.test.tsx`
- Live：53 真实本地备份（VACUUM INTO 夹具）经 `POST /api/v1/backups/{id}/verify` 通过 `backup_integrity`（`live-30`）、54 预演勾选 fenced 后服务端仍返回 `target_identity_not_observed`/`fencing_evidence_unavailable` 未通过（`live-31`）、55 恢复作业创建保持禁用
- 边界：真实双主控隔离演练需第二实例（UNTESTED）；节点断线诊断的 Agent 重连部分需 Agent 实机。

### AC-19 安全凭据
- 页面：`#/settings`（凭据页签）、`#/dns`、`#/monitoring`
- Vitest：`settings-audit.test.tsx`
- Live：26 DNS credentialRef 引用（不回显）、29b 通知策略 PUT
- 边界：凭据轮换/撤销的真实凭据服务动作（credentials.create/rotate/revoke）未在浏览器验收中触发（页面提供入口，写路径由服务端负向测试覆盖）；导出离线 Root 私钥功能不存在于界面。

### AC-20 流量配额/到期提醒
- 页面：`#/nodes/:nodeId`（流量与配额面板）
- Vitest：`node-detail-page.test.tsx`（404=未配置、可首次创建；非 404 错误仍禁用编辑）、`legacy-adapter.test.ts`（缺失不填零）
- Live：32 未配置诚实空态、33 配置 500GB 后 PUT/GET 一致（`live-17/18`）、34 风险列表读取
- 边界：使用样本来自 Agent 采样（无 Agent 时 `usedGb` 显示“缺失（离线或未采集）”，不按 0 处理）；到期提醒通知需通知渠道（UNTESTED）。

### AC-21 Xray 安装
- 页面：`#/plugins/xray`（门控）、`#/plugins`
- Vitest：`plugins-page.test.tsx`（入口联动）
- Live：49 未启用时声明式控制台被拦截，仅显示「插件未启用 + 前往插件中心」（`live-28`）
- 边界：官方制品未发布，批量预检/安装/接管 UNTESTED。

### AC-22 Xray 配置
- 页面：`#/plugins/xray`（配置页签，启用后可达）
- Vitest：`plugins-page.test.tsx`
- Stub：11-xray-1280x900.png（声明式渲染，不执行插件脚本）
- 边界：模板/预检/差异/回滚需插件启用与真实 Xray 实例（UNTESTED）。

### AC-23 Xray 用户与导出
- 同 AC-21/22 门控边界；用户管理/受控导出 UI 已实现（`xray-page.tsx`），真实实例验收 UNTESTED。

### AC-24 Xray 流量
- 同上；流量页签声明 `sourceType` 口径与缺口显示（`proxy_only`），代理流量不冒充网卡/云账单；真实 StatsService 采样 UNTESTED。

## 与需求的差异登记（本轮新增，详见 SHARED-CHANGES §13）

1. 真实 Controller 无 `GET /api/v1/nodes/{id}` 详情路由 → 页面子资源独立加载 + 列表摘要回退。
2. 真实节点生命周期拒绝 `expectedRevision` → 前端不再提交乐观锁字段。
3. 监控 PATCH/POST 使用 `DisallowUnknownFields` → 前端改用白名单请求体。
4. 插件响应真实字段为 `phase`（无 `status`）→ `compat.normalizeInstalledPlugin` 双形状归一。
5. 部分域错误码嵌套于 `error.code` → 客户端兼容提取，UI 显示具体错误码。
6. 流量配额真实响应不回传重置日/时区/来源 → DTO 改可选并诚实显示「未返回」。
7. 监控目标 `.internal` 后缀被 SSRF 策略拒绝 → 验收改用公网样式目标；前端错误码已可见。
8. 目录刷新失败此前无提示 → 补充诚实错误 Notice。
