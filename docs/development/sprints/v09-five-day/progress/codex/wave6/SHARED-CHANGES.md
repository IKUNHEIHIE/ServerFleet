# 待总控接收的共享差异

## 密钥入口

`controller-keyring.patch` 是针对起点 server.go 的可应用差异，只保存于本任务 progress，没有直接改 apps/controller。新增 Config.CredentialKeyDir，并替换固定 local-default 构造。CLI 配置路径、目录建立/权限、测试夹具由总控协调；不接收此补丁主线固定密钥缺口仍存在。默认拒绝 gate 暂保留，后续 AI 接线差异替换它，不能全局放行。

兼容性：旧已初始化库无私有密钥文件将拒绝启动；不可自动覆盖/迁移真实数据。Windows 默认拒绝，需 Linux 实测或另经审查的 ACL 实现。API/Proto/gen 本增量无变化。

## Agent 文件候选

当前正在实现独立有界字节能力。必须由 DS 文件任务与总控共同接收数据帧/Proto、源读和目标写分别授权/执行 ID/不可变摘要，再接入真实 WSS。不得把分块放进 TaskOutput 或普通日志；未接线不算跨节点功能完成。

## AI + 密钥组合差异

`controller-ai-keyring.patch` 是对起点 server.go/ai.go 的完整差异，**替代** 单独 keyring patch；不叠加应用。配套两个 proposed.txt 可作 Go overlay 输入；已实际编译。新增 Config.AIGenerationQuota（无配置时拒绝生成）、服务 AIRuntime / AIModelsHTTP / AIGenerationHTTP、生命周期关闭、owner 配置和生成路由；credential gate 只允许当前私有 AI 生成观察，其他模块仍拒绝。新构建只接固定 HTTPS OpenAI 文本供应商，不引入 SDK、任意 baseURL、运行时或新依赖。

新增 HTTP 候选：GET/PUT `/api/v1/ai/model-settings`，PUT 为 `{expectedRevision,enabled,provider,model,credentialRef,credentialRevision}`；POST `/api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate` 无 body，成功返回既有 Message DTO；只对已经 queued 的消息显式调用。旧消息/SSE端点不改。总控须协调 OpenAPI/前端适配；未接收前不能宣传主线已启用。新增审计事件已有 typed ModelSettingsAuditEvent，旧 audit sink 不会吞事件，但接线应补 typed sink。

官方协议来源：https://developers.openai.com/api/reference/resources/responses/streaming-events 。文本 delta 不等于授权，部分文本在终端校验和借后复核前保留在服务端。

`ai-openapi.patch` 提议新增独立 `api/openapi/revisions/v09-r6/ai-candidate.yaml`，不改三份字节精确原件或 r03。其完整内容为 ai-openapi-candidate.yaml；2路径/3操作 YAML 解析 PASS、git apply --check PASS，尚未接根契约/兼容矩阵或生成前端；总控负责审查合并。Cookie 名使用真实 `__Host-sf_session`，原始候选仍写 sf_session，该差异需总控登记既有候选冲突，不静默照旧。

## 更新 / 恢复 / Xray 接线

`controller-platform.patch` 新增 Config.UpdateCheck / UpdatePlan / UpdateApply / RecoveryExecute / PluginRuntime / XrayTaskHost，并接入当前 Options 或构造器。它与 controller-ai-keyring.patch 可分别应用；两者加在一起已 overlay 编译。未提供的配置仍失败关闭，不影响其他已可用模块。

- 更新 worker：使用 DirectoryReleaseSource.Stage 得到同一个 StagedArtifact；计划解算器必须绑定当前 actor、持久 plan/task/execution、目标版本/摘要和真实备份证据，然后 NewTaskInstallExecutor 调 SlotInstaller。不能把浏览器 confirmation 当批准。Hook 每阶段重查权限、存储兼容、真实进程身份与健康；历史 receipt 不等于当前健康。job查询/回滚继续由统一任务视图组合，当前模块默认查询空/404未冒充完成。
- 恢复 HTTP 只入队统一 task。worker 调 RestoreIsolated（Linux 非 root，新任务私有目录）得到 restored_quarantined；Commit 适配必须在 DS 一笔事务写 tasks.Reconciliation 和 auth.InvalidateRestoredIdentity 输出以及审计，外部当前撤销/身份权威来自离线可信流程，不能取备份旧记录充当当前权威。独立恢复成功不自动 Release；每个敏感入口还要 latch.Check，旧主控 fence/新密钥/当前撤销真实性由 verifier 检查。
- Xray Runtime 只允许注入已通过安全评审的 AdmittedRuntime；TaskHost.Admit/Submit 对接统一任务动作、参数 schema 和批准，不是第二执行中心。无核心 import 可选业务包、无 WASM/Eino 选型。现有两份扫描 FAIL 的 Xray 工件绝不安装；没有新准入工件，真实插件预演/安装/导出/流量/用户端到端仍 BLOCKED，不能靠这个 Host 任务提交测试关闭。
- 共享端口属于候选；API/Proto/gen、任务动作注册/兼容、生产 supervisor、实机及前端由总控协调。作者未改共享迁移编号或锁文件。
