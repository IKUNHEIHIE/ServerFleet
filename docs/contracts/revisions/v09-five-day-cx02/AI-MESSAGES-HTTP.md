# AI消息提交与分页HTTP适配（PROPOSED）

CX-05，沿用受保护候选的GET/POST `/api/v1/ai/sessions/{sessionId}/messages`，operationId为listAISessionMessagesG2/createAIMessageG2。`NewMessagesHTTP`未挂载main；与已实现SSE分别注册。没有修改OpenAPI字节/生成代码/公共依赖，也没有制造第二套任务API。

POST：真实TLS/当前Cookie+Origin+CSRF→单个8–128字节Idempotency-Key→严格JSON标量输入→当前提交gate→ConversationService.Submit及审计同Tx→序列化→再次CommitGate→200 AIMessage。返回role=user、已脱敏内容、真实持久messageId及空taskIds；不是assistant回答或推理/工具完成。无后台goroutine/默认模型，queued消费须由获准组合的有界服务显式调用Generate。模型关闭/配置或来源缺失503；相同key不同内容/配置/范围409，重复消息原样返回；若提交后撤权，扣留响应，后续合法重试按持久幂等返回同一消息。

严格输入包含content/clientMessageId/scopeRevision且仅这3项；最大512KiB、有效UTF8、无重复字段/大小写别名/多JSON/null/压缩/未知字段，content≤32000字符，服务修订从1开始。候选AIMessageInput最低0仍保留原字节，0与任何实际会话不匹配，返回409；正数过期同样409。错误固定problem+json、requestId，拒绝时不回显内容/key/原始错误。安全边界不接收请求自报actor、资源或taskIds。原候选Bearer选项须经S01迁移接收；受限opaque Bearer不具有本AI消息目的，不能从旧枚举推导放行。

GET：Cursor/Limit精确映射PageOfAIMessage。limit默认50、1–200；域端口每次最多100，HTTP最多取201作lookahead。每条内容重新经过当前Filter和scope交集检查；批量读取完成后复核owner/revision/status/allowedNodeIds，最终CommitGate后才输出。总JSON≤512KiB，内容大时本页可少于请求limit，hasMore/nextCursor精确推进，不丢消息。items空时为[]。消息按持久追加顺序，游标是绑定sessionId+scopeRevision+追加位置的canonical base64url JSON；不是权限凭据或保密令牌，客户端选择自身合法位置不扩大授权。跨会话/旧修订/越界/非规范cursor409；未知/重复query、GET body、query token400，HEAD405，明文HTTP403。

调用Authenticate须从请求ctx派生当前可信观察，SessionAccess在领域操作时用它进行当前动作与owner校验，不能把已知subject当认证或缓存连接级主体。CommitGate须在事务外复核同次观察，组合还要配置真实服务器读取期限/响应期限/连接预算；10秒请求ctx与字节界限不等于可以打断不合作的网络Reader。构造缺真实认证或gate拒绝。消息查询仍有正常网络写出窗口，不声称整个发送过程持有数据库锁。Cursor格式/容量和具体错误码为本实现接收候选，G2未冻结。

验证：root AI/updates/architecture PASS 2.929s/2.757s/1.484s，staticcheck PASS。新增不合法JSON/越权/模型停用/当前gate/幂等冲突/最终撤权扣留/byte分页不丢消息测试。固定DS源bb895498隔离实际SQLite/HTTPS十二项PASS8.893s，AI项目1.32s：真实CSRF缺失403、同Tx故障503、幂等重放、两页读取、SSE只发送一个新增事件、撤销Cookie后401。模型/Profile/Filter及部分业务权限桥为明确测试fixture；不是实际BYOK/模型/完整页面或生产任务接线验收。未将fixture注册到Controller入口。
