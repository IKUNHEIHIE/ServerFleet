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
