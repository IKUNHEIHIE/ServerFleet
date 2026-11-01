# Wave6-R1 共享差异与协调登记（gemini-06）

本轮改动全部在允许目录内：`apps/web/src`（AI 模型设置面板、显式生成、CredentialMetadata.revision、测试助手身份修正）、`tests/browser/v09`（正向套件 26 步重构 + probe 门控沿用总控版）、`progress/gemini/wave6-r1`。以下为对 CX/DS/总控的具体登记与请求。

## 1. 实证（总控/Go 域）：operator 角色 + 正确资源授权下，任务读取间歇性 403

**现象**（正向套件 P13，多次复现，约 50% run）：以 operator 角色 + `tasks.approve|tasks.read` 资源授权的用户登录后：
- `GET /api/v1/tasks`（LIST，gate 走 `AuthorizeSubjectAction` 角色检查）→ **200**；
- `GET /api/v1/tasks/{id}`（DETAIL，gate 走 `Authorize` 节点交集）→ **403 forbidden**，持续整个会话，重载不恢复；
- 同一 run 内同节点的 **administrator** 身份读取一切正常。

**身份存储真值**（失败 run 由 identity-dump.mjs 直读 `cx_auth_identity` payload，已含于套件失败输出）：
`approverUser: {roleIds:["operator"]|"administrator", epoch:1}`、`approverIdPresent:true`、对应 grant `{principalId:<审批人>, resourceType:"node", resourceId:<与任务目标一致>, permissions:["tasks.approve","tasks.read"], Nodes:[<同一节点>]}` ——状态完全正确。

**间歇性**：同一代码与夹具，约一半 run 通过（审批→CAPABILITY_REJECTED→target failed 全链路 PASS，证据 `evidence/positive-r1/positive-13-task-failed.png`），另一半持续 403。按 run 的差异初步观察与随机 userId 是否含 `_`/`-` 相关但未证实；`validPolicyID` 字面允许 `_`/`-`。

**请总控核查**（Go 域）：`composedTaskReadGate` → `identity.Authorize` → `grantedNodes`（internal/auth/management.go:439）的 `roleAllows` 与 grant 匹配路径；间歇性提示可能存在状态读取/缓存/revision 交互问题。已按规则以 BLOCKED 语义承载（状态 dump 证据），总控定位后本套件 P13 自动恢复全通过。

## 2. 实证（总控）：网关创建的真实安全门控

`POST /api/v1/gateways {nodeId, acknowledgeInstallImpact:true, installIfMissing:true, preserveExistingConfig:false}` → **409 `takeover_approval_required`（existing site takeover requires approval）**。这是 AC-06"安全接管/旧站点不覆盖/人工准入"的真实服务端门控；正向套件 B04 据此止步（BLOCKED，引用原始响应）。发布流程的接管审批 UI 流程（谁批准、批准后如何继续）当前无契约，如需要请 DS/总控给出端点后前端接入。

## 3. 实证（总控/CX）：BYOK 上游端点固定

`internal/ai/openai_text.go:49` 将 endpoint 固定为 `https://api.openai.com/v1/responses`，`Config.AIRuntime` 无可注入 base URL；且 `bootstrap.Compose` 未接 `AIGenerationQuota`（默认配额一律拒绝）。结果：模型设置/凭据/消息入队全部真实可用（B02 已验证到 read-back ok），但显式 generate 恒为 503 `GENERATION_OUTCOME_UNKNOWN`。CX Wave6-R1 的"本地 TLS 供应商可注入测试配置"需给总控最小组合 patch（注入 endpoint/quota）；落地后正向套件 B02 自动翻转为真实生成终态断言。

## 4. 请求（CX）：AI 生成的事件流可见性

`generation_http.go` 返回完整消息（缓冲至终态）。前端已按"完整消息"实现并标注；如后续提供浏览器逐 token 流（SSE），请给出事件契约，前端将按同一 UI 的流式分支接入（当前不伪造逐 token 展示）。

## 5. 历史登记延续

- 任务执行输出已持久化（task_logs）但无查询端点、executions 无 exitCode/startedAt/finishedAt——见 wave6 `SHARED-CHANGES.md` §1，仍未闭环，继续请求。
- 上一轮登记的其余项（Linux 相对 -db fail-fast、密钥状态端点、常驻连接字段）无变化，继续有效。

## 6. GM 侧已确认保留的总控修正

e7f15f7（probe 门控/expectedUnavailable）、cc36d59（jobId→taskId 归一、真实标题断言）、9d3aa2a（上传审计/写授权复核）全部保留并在其上构建；本轮未覆盖回旧版。
