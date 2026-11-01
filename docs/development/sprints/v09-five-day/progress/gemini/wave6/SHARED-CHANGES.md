# Wave 6 共享差异与协调登记（gemini-06）

本轮全部改动在允许目录内：`apps/web/src`（任务详情错误列 + 接入令牌提示对齐 + 新单测）、
`tests/browser/v09`（新增正向成功套件，未动 70 步回归）、`progress/gemini/**`。
以下是对 CX/DS/总控的具体请求与本轮实证发现，供总控集中接收。

## 1. 请求（CX）：任务执行日志/输出查询端点

**实证**：真实 Agent 执行（正向套件 P10）成功后，输出文本
（`server-fleet agent self-check ok host=… platform=linux arch=amd64`）经
`agentLinkBridge.taskLog` → `tasks.Repo.AppendLog` 持久化进 `task_logs`
（`internal/tasks/repository.go:446`），但 **没有任何 HTTP 端点暴露它**：
`GET /api/v1/tasks/{id}/executions` 只返回 `nodeId/state/error`
（`internal/tasks/http.go:332`），SSE 事件流也不携带日志。

**影响**：AC-05「实际运维任务完成」的前端证据链目前只能断言状态与执行结果，
无法向管理员展示"实际执行了什么、输出是什么"——`WAVE6.md` GM-1 要求的
"实际运维任务完成"正向验收缺最后一环。

**精确请求**：`GET /api/v1/tasks/{jobId}/logs?after=<id>&limit=<n>`（或 executions
条目附加经服务端净化的 `output`/`exitCode`/`startedAt`/`finishedAt` 字段，即
`agent_links.go` 已有的 `sanitizeAgentOutput` 口径）。前端 `task-detail-page.tsx`
的「逐节点执行」表已预留 `exitCode/startedAt/finishedAt/输出截断` 列与兼容解析
（`compat.normalizeTaskExecutions`），服务端一旦返回即自动渲染，无需再改前端。

## 2. 实证（DS/总控）：跨节点传输创建返回 500 internal_error

**实证**：正向套件 B01——对真实接入并在线的节点
`POST /api/v1/transfers {sourceNodeId==destinationNodeId=<真实节点>, sourcePaths, destinationDir, conflictPolicy:"overwrite"}`（带 `Idempotency-Key`）
→ **500 `{"error":{"code":"internal_error","message":"request failed"}}`**。

**期望**：未接线的跨节点字节通道应返回类型化的 4xx/503（如 `transfer_unavailable`），
而不是 500。500 可能是 source==destination 未校验，也可能是 `files.transfer` 任务
经 `agentLinkBridge.Dispatch` 后无能力承接的未处理路径——请 DS/总控核查
`internal/files` 的 `CreateTransfer → Submitter(=composedTaskBridge) → tasks.Service.Submit`
链路。当前基线被记为 BLOCKED（含原始响应），不阻塞其他项。

## 3. 实证（总控/CX）：Controller 在 Linux 上对相对 `-db` 路径启动即失败

**实证**：WSL 内 `./controller -db db/controller.db …` →
`initialize controller: plugins: package rejected`。原因：staging 目录由 `-db`
推导为相对路径，`plugins.NewPackageVault` 要求 `filepath.IsAbs`
（`internal/plugins/vault.go:27`）。

**影响**：awskr 部署用绝对路径不受影响；但 Linux 上的 fail-fast 错误信息
（`plugins: package rejected`）与真实原因（staging 路径非绝对）相距很远，排障成本高
（本轮消耗多轮调试）。建议要么在 `bootstrap.ParseOptions` 把 DBPath 转绝对，
要么在 vault 拒绝时给出含路径的明确错误。不阻塞，登记待总控裁量。

## 4. 请求（CX）：安全密钥管理状态端点（WAVE6.md GM-2 对齐项）

`WAVE6.md` CX-1 交付"真实随机主密钥 + 私有文件权限 + 重启可解密 + 轮换/失败关闭"。
前端需要**不含任何秘密材料**的状态查询，用于 `#/settings` 凭据页签：

- 期望形状（建议）：`GET /api/v1/security/keyring` →
  `{ configured: boolean, algorithm: string, createdAt?: string, rotatedAt?: string, generations: number, lockboxVersion: number }`
  ——只读、管理员授权、永不返回密钥材料/摘要明文。
- 收到契约后前端交付：设置页"密钥管理状态"面板（只显示状态与轮换时间，
  不显示/不缓存任何秘密），并提供对应单测与浏览器验收步骤。

## 5. 请求（CX）：Agent 常驻连接的节点可见字段（WAVE6.md GM-2 对齐项）

CX-2 交付常驻连接/有界重连/持续心跳。节点列表/详情当前只有 `online` +
`metrics*` + 连接诊断。前端为呈现"常驻节点状态"需要（建议）：

- 节点视图增加 `connectionMode: "oneshot" | "resident"`（或 `residentSince`/
  `lastHeartbeatAt`），由服务端按真实链路状态计算，前端不自算。
- 收到契约后前端交付：节点列表/详情的连接模式标识 + 正向套件
  "常驻节点在线"用例翻转。

## 6. 修正（GM 本轮已做）：接入令牌有效期提示与真实约束不一致

`#/nodes/new` 的有效期提示曾写"服务端允许 1–60 分钟"，而
`internal/transport/enrollment_management_http.go:153` 硬约束 `1–10`
（超出返回 `ENROLLMENT_TOKEN_POLICY`）。已改为「1–10 分钟（ENROLLMENT_TOKEN_POLICY
硬约束）」并把输入 `max` 从 60 改为 10（`apps/web/src/features/nodes/node-add-page.tsx`）。

## 7. 契约缺口（历史遗留，仍未闭环）

- `GET /api/v1/auth/me` 不返回会话绑定 CSRF（刷新后写锁定）——Wave 1 登记，仍未解。
- 分批/金丝雀参数、任务 TTL/熔断阈值、工作流人工审批分支的 UI 覆盖、Agent 升级端点、
  备份记录列表/创建、恢复作业列表——见 `progress/gemini/SHARED-CHANGES.md` §2–§10
  与 §12/§13（Wave 3–5 版本），本轮无变化，继续有效。
