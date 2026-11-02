# Wave6-R2 · 总控已接收的共享实现

2026-11-02；总控/审查 codex-review-02。技术版本 `23e56d55098a4fa7655ac4c1c03874384b64de5a`，联合原生测试版本 `95adeb53174b54172dcff372ef568c6c3440c477`；后者到前者只删除测试宿主已废弃的类型并修正注释。最终统一起点以主目录 `reports/v09-wave6-r2-review-handoff.json` 的 finalHead 为准，包含本说明、审查证据与下一轮任务。完整 v0.9、G2 与发布准入均未通过。

## 已实际应用，勿重放历史补丁

已应用 CX-03 的 controller-files、controller-ai-budget、redocly-ai-budget、controller-platform、updates-openapi 五份补丁；新增 DS cleanup 组合/路由与迁移登记。中央代码已在 Git 中，无需 overlay。

`prepare-wave6-r2-*-overlays.mjs`、`prepare-wave6-r2-update-contract.mjs`、旧 patches 检查器及作者 HANDOFF 的应用步骤是原分支历史重现资料，**不要在统一新基线上运行或再次应用**。当前正式组合使用 `tests/poc/v09/platform/run-wave6-r2-combined.mjs`，已移除 overlay/build-tag/prepare；它要求干净 HEAD。

| 接缝 | 当前位置和语义 |
|---|---|
| 统一文件执行 | `apps/controller/server/file_worker.go`、server.Config.FileExecution；内部使用 tasks worker、持久 execution binding、正式 WSS。不得重复注册 dispatcher 或额外发送 TaskOffer |
| 路径映射 | `internal/files/root_mapper.go` 的 NewRootPathMapper：明确 nodeID→逻辑绝对根；Agent 的私有 file-root 是实际本地根。不能自动剥掉 `/`、用服务端路径猜 Agent 路径或允许任意节点动态自注册根 |
| 文件 spool | FileExecutionConfig.SpoolDir；创建私有目录，CheckExecutionSpool 保持所有权/权限校验；AI/plugin 额外 ScopeGate 缺失拒绝 |
| AI 预算 | 正式 GET/PUT `/api/v1/ai/model-settings/{profileId}/budget`，实现 internal/ai/budget_http.go；owner 读取、当前真人管理员 CAS 配置、Cookie/Origin/CSRF、profile 和 Recheck；计数/fence 不随策略重置 |
| 更新 | server.Config.UpdateExecution/UpdateResolvePlan、updates/task_worker.go、task_jobs.go、install_sql.go；统一任务/审批、真实安装来源与冻结计划 resolver 由可信程序注入；默认 CLI 未提供实际发布源/部署装配 |
| 恢复 | recovery.RuntimeGuard、QuarantineLatch 与 server/transport/tasks 的当前门；恢复验证器仍需真正新权威/旧主控 fence，不从确认文本或预演成功推定 |
| cleanup | server.CertificateCleanup；四条 certificates.cleanup.* 权限仅管理员，HTTP 通过真实会话；Config.CertificateCleanupCleaner 默认 nil，retry 明确 unavailable；没有启用自动 RetryDue 或真实供应商删除 |
| 发布 | gateway_tasks.go 与 publishing.NewProductionBridge 是候选适配；默认仍 NewLocalBridge，未自动切换为 production。提交/审批前不上传，queued/awaiting_approval 不能算发布或补偿成功 |

## 迁移与契约

正式 Controller 现在 **24 项迁移**：原 21 项加 `cx/0008_file_execution_bindings`、`cx/0009_update_journal`、`ds/0062_acme_cleanup`。保留原顺序/checksum，已核对原 19→24、21→24 和重复启动。作者 overlay 原先只组合 23 项，不是当前正式组合数量。

根 Redocly 已登记 `api/openapi/revisions/v09-r7/{ai-budget,services,updates}-candidate.yaml`。更新实现的 `id/taskId/status/targetVersion` 与旧 G2 `jobId/state/toVersion` 有显式差异；原三份候选、69 项原文、Proto/gen/基线未改，不能宣称完整向后兼容或 G2 冻结。DS services 候选和发布实际任务终态须下一轮继续对齐。

## 测试宿主安全修正

GM fixturehost 保留程序注入 LocalAIProvider；**已删除无认证 GET /fixture/configure**。遥测仅绑定显式 loopback，只读健康/计数；合成上游密钥通过私有 0600 文件在启动前注入，不进 URL/明文日志。预算配置现在由已登录浏览器带 CSRF 调用上述正式 PUT。不能恢复旧旁路，也不能将测试宿主导入产品。

同名节点必须按稳定 nodeID 选择。Tasks 页 checkbox value/可访问名称含 nodeID；P13 创建后核验真实 targetNodeIdsSnapshot，再由只获该节点权限的 Operator 审批。旧两个 enrollment-token 名称与选择器取最后一项造成的目标混淆已有回归测试和本轮成功证据；不得放宽授权解决 403。

## 未完成的准入项

- 浏览器 B01 宿主尚未注入 FileExecution/路径映射或 Agent file-root；后端双真实 Agent 多文件已有 6 组联合证据，页面闭环仍未完成。上传分发、其他冲突策略/续传范围按需求继续，不把当前 skip 支持说成全功能。
- 实际发布尚无完整可等待的网关任务 worker/工件到达/Agent 注册/终态补偿组合；本轮安全返修只保证当前链路失败关闭。生产桥不能提前默认启用。
- cleanup 的 execute→setStateAt 无原子执行 claim，provider I/O 期间 cancel/retry 的并发与当前授权问题尚未关闭；限次/取消不能仅靠单线程测试认定。DS-04 先返修再接真实 cleaner，默认 nil 保持。
- GatewayCapability 的私有根所有权/权限与失败回滚无法确认时 UNKNOWN 仍需补验，不直接接管已有站点。实际 Nginx 独立测试不等于统一任务发布成功。
- B03 Xray 缺可准入工件/Runtime；B05 缺正式产品更新来源/安装装配；B06 缺真实新权威/旧主控 fence 和第二隔离主控执行。正式 PRIV/WC/PKI-OPS/UPDATE 原文缺项、ARM 运行、物理双机、1C1G 及真实外部账号验收保持未关闭。
- 本轮未重新观察或修改 awskr 原服务，ENV-W6-01 沿用上轮未关闭状态，不据此描述当前远端进程状态。

接收证据见 [本轮报告](../../reviews/V09-WAVE6-R2-REVIEW.md)，下一轮所有权与停止条件见 [WAVE6-R3](WAVE6-R3.md)。三位不再把上述已接收中央补丁作为等待条件；新共享差异仍交具体 patch，总控审查后接收。
