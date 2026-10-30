# 共享变更请求（GM → 总控）

本文件只登记需要总控/契约 owner 协调的公共变更；作者不直接修改 OpenAPI、锁文件或 CI。

## 1. `/auth/me` 返回当前会话 CSRF 令牌（或等价受保护补取方式）

- 现状：`LoginResult.csrfToken` 只在登录/初始化响应中出现一次；`GET /api/v1/auth/me` 返回 `Identity`，没有 CSRF。页面刷新后 Cookie 会话仍有效，但前端内存中的 CSRF 丢失。
- 影响：所有 Cookie 会话写操作（创建/修改/审批/退出）无法进行，只能要求用户重新登录。当前 UI 明确锁定并提示，不伪造令牌。
- 请求：在 `Identity` 增加只读 `csrfToken`（与当前会话绑定，每次会话轮换/切换时更换），或提供一个受保护、同源、no-store 的补取响应。认证设计 §5 已提出此缺口（“候选 me 响应尚无此字段，需契约 owner 评审扩展”）。
- 兼容性：新增可选字段；不含会话秘密，不应写日志。前端在字段出现后自动采用，无需改端点路径。
- 测试：刷新后写操作仍带正确 `X-CSRF-Token`；跨会话/旧令牌被拒绝。

## 2. 工作流（workflows）管理契约缺失

- 现状：`api/openapi/g2-merged-candidate.yaml` 无 `/api/v1/workflows*` 操作；功能需求要求“触发器、条件、操作、人工审批、通知、启停与模拟执行”。
- 影响：GM-07 工作流页面不能实现真实规则 CRUD/审批；只能显示明确契约缺口，不能用本地状态伪造。
- 请求（最小可实施集合）：`GET/POST /api/v1/workflows`、`GET/PATCH/DELETE /api/v1/workflows/{id}`、`POST /api/v1/workflows/{id}/runs`、`GET /api/v1/workflows/{id}/runs`、以及人工审批决策端点；所有写操作经统一授权/审计，执行经统一任务系统。
- 兼容性：新命名空间，不影响既有操作；由 CX/DS 确认归属（MODULE-OWNERSHIP 登记 `internal/workflows` 属 CX 包）。
- 测试：未授权修改/触发被拒绝；审批失效不执行；任务目标仍走快照与当前权限校验。

## 3. 资源监控与告警规则契约（GM-06 依赖）

- 现状：有 `availability-monitors`（服务可用性）与通知投递接口，但没有节点资源监控历史/告警规则查询操作；节点列表只有当前 `cpuPercent/memoryPercent`。
- 影响：资源监控页面只能展示节点当前读数或明确“无历史接口”，不能伪造曲线与告警规则。
- 请求：确认是否由 DS-06 提供 `GET /api/v1/nodes/{nodeId}/metrics?from&to` 与告警规则 CRUD；若本期不提供，UI 将保持“契约缺口”状态并记录。

## 4. 备份记录列表/创建（GM-08 依赖）

- 现状：仅 `POST /api/v1/backups/{backupId}/verify` 与灾害恢复预览/作业，没有备份记录列表、创建备份操作。
- 影响：备份与恢复页面不能列出真实备份或创建备份；只能说明缺口与现有恢复入口。
- 请求：确认 DS-01/DS-09 是否提供备份列表/创建端点，或明确由恢复预览承担。

## 5. 任务契约：重复路径与创建响应不一致（GM-05 已按可实施子集接线）

- 现状：`api/openapi/g2-merged-candidate.yaml` 同时存在核心任务与 SafeTasks/Batch 的 `/api/v1/tasks` 与 `/api/v1/tasks/{jobId}` 定义；前者返回 `TaskSummary`（`taskId/state/revision/planDigest`），后者返回 `BatchTaskJob`（`jobId/state/targetType/...`）。YAML 键重复，YAML 解析会以后者覆盖前者。
- 前端当前选择：列表/详情/审批/取消/恢复/逐节点执行/SSE 使用核心 `TaskSummary` 语义（`taskId`）；创建与预演使用批量 DTO，取响应 `jobId` 作为任务详情 ID。若总控最终选择另一套字段，需要一次 DTO 适配提交，不在 UI 里同时猜测两套字段。
- 请求：总控确认唯一任务 DTO/路径（建议保留核心 `TaskSummary` + 创建返回 `taskId`，或明确 `jobId === taskId`），并补 `GET /tasks/{id}/events` 的事件 data schema。SSE 当前只投影已知字段（state/nodeId/progressPercent/message/sequence），不显示原始 data。
- 影响：详情从创建结果跳转依赖 `jobId` 与核心 `taskId` 是同一 ID；目前按契约缺省未额外转换，若服务端不是同一 ID 会显示“任务不存在”，不会伪造详情。
- 分批/金丝雀参数：`BatchTaskCreateRequest` 没有 `batchSize/canarySize/dryRunLevel`；创建页只提交 `dispatchMode` 与布尔 `dryRun`，并明确提示缺口。

## 6. 文件上传/下载通道（GM-05 BLOCKED）

- 契约明确“上传会话只声明元数据，实际字节传输通道待技术设计”；没有下载端点。
- 前端已实现目录浏览、创建文件夹、重命名、删除、大小计算、上传元数据声明、跨节点异步复制/取消；上传只到 `awaiting_data`，不调用 `complete` 冒充完成；下载不显示按钮。
- 请求：补充受控上传字节流（含断点/校验）与下载（含路径授权、范围/流式）端点，或明确本期交付边界。真实字节验收保持 UNTESTED/BLOCKED。

## 7. 与 CX/DS 检查点的对齐观察（未接收，不切换主线）

- CX 检查点 2fc8dab 提议 `bootstrap` 从 `LoginResult` 改为 204、Cookie 名改 `__Host-sf_session`、`me` 增加 CSRF 重载；前端当前仍按已接收候选的 `LoginResult` 实现，刷新后写入锁定。等总控接收后再改，避免把未接收提案当冻结接口。
- 前端已按 401/403/503 语义实现：401 终止会话、403 只清当前资源、503 保留会话；节点列表使用 Cookie credentials，等待列表 security 接收后即可真实生效。
- DS 的 SQLite/storage 接缝不出现在 Web 层；Web 不直连 DB/Agent。

## 8. GM-06 契约缺口（监控/证书/发布/网关）

- 服务监控检查历史：`GET /api/v1/availability-monitors/{id}/checks` 当前只返回 `{ok}`，没有检查时间、探测源状态或事件字段；UI 不展示伪造历史，建议补最小 `checks[]`（时间、结果、错误分类、探测源失联标记）。
- 资源监控：没有节点历史指标/告警规则端点；UI 只显示当前读数与 `/traffic-risks`，建议 DS-06 确认是否本期提供。
- 公网证书：只有 `certificates/internal/distributions`；没有 ACME/公网证书列表、申请、续期、吊销端点。UI 明确 BLOCKED，不伪造签发。
- 发布 DTO：`PublicationDraft08` 没有独立业务端口/网关端口映射字段，也没有逐网关配置差异；服务组端口只读展示。若产品要求“向导选择端口”，需扩展契约。
- 网关路由：`GatewayRoute` 没有 `gatewayId` 字段，无法从响应判断路由归属哪台网关；UI 仅展示服务端返回列表，不自行关联。建议补归属/检查字段。
- 通知测试：返回 delivery 状态；UI 只显示平台语义，不声称已读。SMTP/QQ 秘密只写。

## 9. GM-07 契约缺口（AI 会话 / 插件 / Xray）

- **AI 会话列表缺失**：`/api/v1/ai/sessions` 只有 `POST`，没有 `GET` 列表；`/ai` 入口无法显示当前用户的历史会话，只能新建后进入 `sessionId` 页面。请求：补 `GET /api/v1/ai/sessions?cursor=&limit=`（返回 `AISession[]` + `PageMeta`，仅当前用户可见会话），或明确本期不提供列表。
- **AI 会话无法关闭**：`/api/v1/ai/sessions/{sessionId}` 只有 `GET`，`AISession.status` 含 `closed` 但没有关闭/撤销整会话授权的操作。请求：补 `POST /api/v1/ai/sessions/{sessionId}/close`（或 DELETE），语义为立即失效全部授权与待批计划。
- **AI SSE 事件无数据契约**：`GET /api/v1/ai/sessions/{sessionId}/events` 只声明 `204`，没有 `text/event-stream` 响应体、事件名清单或 data schema（对比：任务 SSE 同样缺，见 §5）。前端按固定投影字段渲染（`event/id/toolName/state/message/progressPercent`），**从不渲染原始 data**；请补事件名与字段契约以便对齐。
- **审批执行响应字段**：`POST /api/v1/ai/tools/execute` 返回体未声明 `validatedNodeIds`/`state` 的最小集；前端只展示服务端返回值，不自造“已执行成功”。
- **插件操作与菜单联动**：`PluginOperation.status` 为异步（queued/verifying/draining/applying/succeeded/failed/rolled_back/deferred）。前端只把服务端 `GET /api/v1/plugins` 中 `enabled && status==="enabled"` 的 `xray-manager` 显示为导航入口；请求确认“热禁用后入口消失”的判定字段就是这两个（而非需要额外 `menuVisible`）。
- **卸载不保留数据选择**：`DELETE /api/v1/plugins/{pluginId}` 无 “保留/清除插件数据” 参数；FR §11.4 要求用户选择。UI 明确不承诺数据保留策略。请求：补 `?purgeData=` 或 body 字段。
- **Xray 插件契约状态**：`/api/v1/plugins/xray-manager/*` 已在候选中出现（capabilities/nodes/instance/actions/templates/installations/configurations/deployments/users/client-export/traffic/jobs），但 `FR-PLUGIN-XRAY-001` 的发行制品尚未发布，因此前端把“安装”按钮的可用性交给目录条目的 `sourceCommit + artifactSha256`：缺失即显示“不可安装/尚未发布”，不伪造下载成功。

## 10. GM-08 契约缺口（更新中心 / 备份与灾难恢复）

- **Agent 升级没有端点**：候选只有主控 `/api/v1/system/updates/{check,plan,apply,jobs,jobs/{id}/rollback}`；FR §12 要求「Agent 通过更新中心选择节点并分批升级」，但没有 Agent 版本清单、目标选择或分批升级操作，`/api/v1/tasks` 也缺分批/金丝雀参数（§5）。更新中心因此只显示契约缺口与到批量任务/节点列表的跳转，不伪造“选择节点分批升级”。请求：补 `GET /api/v1/agents/updates/check`、`POST /api/v1/agents/updates/jobs`（含 targetNodeIds、批次策略与熔断）或明确本期不做。
- **备份记录列表/创建缺失**（同 §4）：只有 `POST /api/v1/backups/{backupId}/verify`，没有 `GET /api/v1/backups`、`POST /api/v1/backups`、保留策略或存储位置字段。备份页因此不显示备份记录表格、不提供“立即备份”，backupId 必须由服务端（如更新作业的 `backupId`）或管理员提供。升级要求 `backupMode: "required"`，但没有可验证备份时前端只能禁止提交——请求补最小备份记录契约。
- **灾难恢复作业没有列表**：只有 `POST /api/v1/disaster-recovery/jobs` 与 `GET …/jobs/{jobId}`，页面刷新后无法从服务端找回作业；前端不本地存储作业记录，也不伪造历史。请求：补 `GET /api/v1/disaster-recovery/jobs?cursor=&limit=`。
- **`POST /api/v1/backups/{backupId}/verify` 返回 202 + `RecoveryPlan08`**：语义上“校验”返回的是恢复计划而不是校验结果（没有 `verified/valid` 字段或校验时间）。前端只按服务端返回的 `checks[]` 显示结论，未通过即禁止创建恢复作业；请求确认是否应返回独立 `BackupVerification`（含校验时间、校验和结果、可恢复性结论）。
- **`RecoveryPlan08.oldControllerFenced` 是声明**：契约注释已说明它不是充分的技术隔离证据。前端要求管理员显式勾选并提示服务端仍需独立校验；请求后端确认会用真实 fencing 证据覆盖该声明。

## 11. GM-09 验收桩说明（不是契约变更）

- `apps/web/acceptance/` 下新增本地验收桩（`stub-server.mjs` 托管 `dist` 并响应候选契约、`fixtures.mjs` 内存数据、`cdp-driver.mjs` 截图/求值驱动）。仅监听 127.0.0.1，**不被 `apps/web/src` 引用、不进入 dist**；生产失败路径仍只显示错误分类。
- 桩的目的是在 Controller 管理 API 缺失时验证**正式前端**的真实交互（登录/CSRF/401/403/503/SSE/迟到响应等）。它不是后端实现，也不能作为后端验收证据。
- 本机没有 Go 工具链，未运行 `apps/controller` 骨架；“仅 healthz”场景用同路由集的 `--healthz-only` 桩复现。
- 若总控不希望仓库内保留验收桩，可整体删除 `apps/web/acceptance/`，`ACCEPTANCE.md` 中的步骤与截图仍可复核当时行为。
