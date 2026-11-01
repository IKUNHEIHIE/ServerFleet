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
