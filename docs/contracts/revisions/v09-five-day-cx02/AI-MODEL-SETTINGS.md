# 持久模型配置与凭据revision（PROPOSED）

CX-05；ModelSettingsService复用AI自己cx_ai_sessions聚合的可选Models字段，不新增公共SQL表/迁移编号/根依赖。旧Version1缺Models时保持未配置/关闭；损坏owner/重复profileId/非法配置/审计绑定关闭失败，不补默认模型/凭据。每owner最多1份/共1000份，整个AI聚合仍8MiB/10000出箱上限。

程序端口：`NewModelSettingsService(sessionService, CredentialMetadataCheck)`、`Get(ctx)`、`Set(ctx,expectedRevision,input)`、`Profile(ctx,owner)`、`ResolveCredential(ctx,GenerationRequest,digest)`。Input只有enabled、固定provider=openai-responses、精确model、credentialRef与credentialRevision，无key/URL/任意参数/owner字段；ID由服务端生成。Get返回**已保存配置**，enabled不是provider健康或Key有效承诺。未配置返回disabled/revision0/空ID，Profile拒绝。保存使用CAS、人类Web当前主体、相同事务审计/出箱；并发同revision仅一提交。Profile只供同owner推理使用，不改变任何session/node grants/审批策略。

CredentialMetadataCheck必须验证当前owner对精确ref的使用权，返回新鲜ID/revision/kind=ai_api_key/仅ai用途/未撤销未过期状态；不是“能列出所有凭据元数据就能使用”。新引用即使保存成disabled也必须核验，防止把API Key误作ref落入普通配置；禁用已有同一引用或清空配置不依赖Metadata服务可用。unverified只表示尚未验证上游，允许明确配置后尝试，不能标成真实模型可用；本组件不做网络健康检查或秘密读取。Model字段拒绝常见sk-误贴值，完整脱敏/外发政策仍需正式PRIV/独立新决定，不能将此最小校验称DLP。

每次启用/修改递增配置revision，Profile.revision绑定它。凭据轮换后原credentialRevision不匹配即不可推理，必须真人重新绑定；排队消息保留原Profile、修改后409/不可自动换模型重发，旧queued记录需要上层明确反馈/另建消息。运行中模型关闭/配置变化，当前凭据闸和完成检查拒绝，保留UNKNOWN，不持久部分回答。ResolveCredential只输出当前元数据，仍须真实CredentialService.UseForService及ConversationService.CheckCredentialOperation私有观察；没有任意解密入口。

已知生成ctx额外禁止所有human=true的AI会话/模型管理动作，不能借原始Web会话的HumanWeb标记调用AddGrant/SetPolicy/Set模型配置。真人在另一条已认证/CSRF Web上下文仍可修改或停用；回调返回后的WithoutCancel上下文也不会变成真人。测试中的并发真人变化使用独立上下文，不以放宽这个门模拟通过。

## 审计接缝精确增量

现有SessionSQLStatements新增 `AppendModelAudit func(context.Context, []ModelSettingsAuditEvent) error`；事件字段SubjectID/ProfileID/Action/Revision/At。必须与同次模型配置写入使用同一DS Tx。原AppendAudit类型/会话事件不变；模型事件绝不塞给旧适配器冒充session或默默丢新字段。nil只保留真实持久出箱，不宣称已进入全局审计。新增typed sink接收后才移除相应事件；任一sink/最终提交失败整笔回滚。

固定DS旧Event无通用资源revision字段，隔离测试显式以 `RequestID=model-<profileId>-r<revision>` 保存非秘密变更引用、Action=ai.models.update/SourceWeb，profile修订仍在typed事件与聚合中；这是一项待审兼容映射，未修改DS/API/迁移。生产可接收此关联规则或由总控协调原生资源字段，不能忽略profile/revision后回nil。旧sink保留模型出箱、正常session事件独立确认已做负向验证。

SessionAccess的ai.models.read/update为当前主体/角色/同源CSRF边界的候选分支语义，**不是node权限**；不能为个人配置虚构nodeId、通配grant或把已知subject当认证。公开模型设置路径/DTO、具体角色政策/凭据引用服务适配及该新sink仍须总控/GM/安全审查，当前没有挂载设置HTTP或修改受保护OpenAPI。建议组合`Profile=settings.Profile`、`Resolver=settings.ResolveCredential`，保留批准后的强制Filter；没有正式外发政策时仍不启用真实模型。

## 实际验证

Windows AI/architecture PASS12.916s/1.458s，staticcheck PASS；默认关闭/owner/CAS并发/轮换/撤销/过期/Metadata故障可禁用/持久损坏/推理上下文自提权拒绝/模型改变导致UNKNOWN或旧排队拒绝/旧audit适配保留出箱均测。首轮新增测试有局部err作用域编译FAIL，修正fixture后通过，无生产门槛删减。

固定DS源bb895498实际SQLite十二项PASS8.396s，含模型配置+审计提交前故障共同回滚、成功重开保持、消息Profile从持久设置读取及原HTTPS/SSE回归。metadata授权/模型/Filter/部分角色为明确fixture，非真实BYOK/生产角色政策验收。Ubuntu WSL/UID1000本轮仅重跑变化的AI和SQLite：30+12顶层运行PASS/无SKIP，精确源和二进制哈希见LINUX-MODEL-SETTINGS-EVIDENCE.json，不冒充全12包又一次全量。runner新增subset选择及每次独立目录，保留历史日志/二进制；源快照包括未提交新增Go文件，前后新增/修改源也会失败。
