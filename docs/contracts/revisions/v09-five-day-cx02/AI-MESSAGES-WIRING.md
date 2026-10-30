# AI持久消息、生成防重放与SSE（候选）

`ai.NewConversationService`复用SessionService的cx_ai_sessions聚合/当前owner gate/同Tx审计，不新增公共迁移编号。旧scope-only聚合仍可读取；非空Conversation严格验证消息、请求摘要、run、事件游标及引用。`MessageInput/Message`沿用既有content/clientMessageId/scopeRevision及messageId/sessionId/role/redactedContent/createdAt/taskIds模型；不把内部Profile/Run作为HTTP DTO输出。

## 实际行为

- `Submit(ctx,sessionId,idempotencyKey,input)`先查当前Web主体/会话修订/可用模型配置，再经强制Filter脱敏后同Tx保存user消息、queued推理、元数据事件和审计。返回的是已提交用户消息，绝不伪造assistant回复。重复clientMessageId及幂等键必须完全匹配脱敏内容/修订/配置摘要，否则409；不会再创建事件或重复计费调用。
- `Generate(ctx,sessionId,messageId)`由可信组合明确调用，没有构造即启动的后台worker。先同Tx保存generating防重放及审计，再调用程序注册的Provider。当前owner/action/scope/节点交集/模型配置每次输出和结束重查；输出仅文本，TaskIDs始终为空。任何模型返回的“命令/任务已成功”文本不具有执行权威。工具另经AssessTool→唯一tasks/approval/audit，未绕开S03阻塞。
- 错误/异常/取消/停用/撤权/修订变化/超限/完成事务失败均不输出assistant成功，尽力独立提交unknown事件及审计；失败记录留generating也永久拒绝重放。生成器忽略emit错误不能将后续局部输出标成功，结束后保留的callback拒绝调用。时间/错误原文不作为模型回答保存。
- 成功回答、终态事件和审计同Tx；下一条推理按已完成user/assistant成对上下文构造，即使后一条用户消息在前一回答完成前已提交，也不丢前一回答。unknown轮次不伪造回答或自动重试。并行/乱序启动拒绝；generating表示已发起/结果待核实，进程重启不凭旧记录声称worker仍活跃，旧推理需要外部故障对账，不能自动重新调用。
- Profile/Filter/Generate三个回调均无默认放行。Profile只交付当前配置ID/revision/model/credentialRef；真实BYOK须由可信桥用`GenerationRequest.OperationID`接`CredentialService.UseForService`，绑定当前owner/凭据revision/用途与准入，秘密只在借用回调内给真正provider，不进消息/事件/模型工具参数。没有实现/安装默认模型适配器，也没有实际外部模型证据。
- Filter必须在持久、Provider和读出前应用当前资源/秘密策略；不能将测试替换函数作为生产DLP。当前后端不会向提示词注入CA/恢复私钥、Token、凭据库明文或节点数据。已脱敏文本仍可能涉及个人/业务资料，必须经正式PRIV/模型配置策略接收，不能借本候选宣称完整隐私条款已批准。

## 边界与容量

30秒推理ctx、≤1024次串行化输出回调、≤128000输出字节/32000字符；每次输入上下文≤64消息/256000字节，不静默截断。每会话≤256轮/512消息/1024元数据事件，聚合总8MiB，满时失败关闭，当前没有自动删除防重放或秘密明文归档。回调必须遵守取消；普通Go函数接口不能强杀不合作provider。无1核1GB性能或无限会话/消息承诺。

`Messages`和`Events`逐次查当前owner/action，读内容再经Filter和当前范围核对。关闭会话不重新开授权；未来scope资料不入旧资源快照。内部分页按序号。R24已补未挂载的POST/list消息Handler与Cursor/PageMeta适配，详见[AI-MESSAGES-HTTP.md](AI-MESSAGES-HTTP.md)；仍待契约/组合接收，不另造平行公开路径。

## SSE

`NewMessageEventsHTTP`只注册既有GET `/api/v1/ai/sessions/{sessionId}/events`，未自动挂到Controller。构造强制真实Authenticate和CommitGate；Authenticate须从bounded request context派生当前观察，每轮/每帧重新走TLS/Origin/Cookie，SessionAccess用只读当前观察，写出前CommitGate在事务外。支持`Last-Event-ID: <sessionId>.<sequence>`，拒绝跨会话/重复/未来/非规范游标和query token；不接受HEAD/明文HTTP。30秒有界流、500ms复查、5秒写期限；重连续读持久元数据事件，不能拿旧连接主体永久放行。

事件只含sessionId/sequence/kind/messageId/state/at，不包含文本delta、秘密或节点内容；需要文字时另走当前授权/脱敏消息读取。错误使用problem+json和固定状态，来源故障503、凭据401、来源/权限403区分；开始发送后撤权/故障直接关闭。HTTP输出仍有网络写入窗口，不声称事务锁覆盖整个网络发送。生产组合还需SSE连接预算、正式消息API/页面和相应契约接收，当前没有对外启用。

## 审计适配及真实证据

SessionAuditEvent新增MessageID/Result，scope-only历史出箱Result空值规范成success。结果未知保持领域unknown，不能假装provider已失败或成功。固定DS审计只支持allowed/denied/success/failure/partial；隔离适配暂用`failure + reasonCode=ai_generation_unknown + SourceSystem`表示服务无法确认完成，RequestID=MessageID。这个明确映射及未来原生unknown结果须总控审查；未修改DS枚举。首次实验用大写reason被其验证器拒绝，unknown事务没有提交；保留FAIL，改为其允许的小写原因码后通过，未降审计门槛。

本机真实SQLite测试：创建消息+审计在commit前故障一同回滚；回答+终态审计故障不生成成功；unknown持久化并重启后拒绝再次调用Provider；真实HTTPS/Cookie/Origin/CommitGate的SSE从0顺序发送，游标续读只发新增事件，真实会话撤销后≤下一轮关闭。Profile/Filter/Provider在测试中明确double，未标真实模型/BYOK。固定DS源与隔离锁沿用run-identity.mjs，根依赖及共享接口未变。
