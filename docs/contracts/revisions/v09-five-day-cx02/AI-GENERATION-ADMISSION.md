# 推理中的凭据借用闸（候选）

`ConversationService.CheckCredentialOperation(ctx, op)`供Controller组合进auth.CredentialOperationGate的AI分支；不是新增HTTP endpoint，不向模型/请求导出凭据或内部观察。只接收Generate在持久generating fence、成对历史构造/脱敏、当前scope/Profile复核后创建的私有ctx观察，包含完整GenerationRequest摘要、节点交集和关闭闸。Provider仅得到派生ctx；即使它保留WithoutCancel(ctx)，回调返回即关闭，成功/失败/异常路径都不能用旧观察再借密钥。

每次核对op的subject/credentialRef/operation/digest/module=ai/source=ai/action=use/空change/current revision条件，再读当前身份动作、owner、会话状态/修订、allowedNodeIds、启用Profile，事务内核持久run仍generating及相同profile/scope。正常凭据服务另做kind/purpose/密文绑定/revision/撤销/有效期/永久使用fence与审计；本闸不能替代它。没有观察或手工创建“看起来正确”的GenerationRequest均拒绝；真实 HTTP 请求无法生成未导出的ctx key。闸不授予节点/root/tool能力。

组合示意为`if op.Module=="ai" && op.Source=="ai" { return conversation.CheckCredentialOperation(ctx,op) }`，还必须组合现有当前真实身份/服务目的检查，其他模块分别走自己的准入。不要将其他模块缺适配时默认return nil。Resolver必须读当前启用Profile和CredentialMetadata确定ExpectedRevision；本组件不自行推导凭据revision，也未实现模型设置持久管理或PRIV外发授权。运行时迁移/统一事务/入口未接收前仍不注册main。

测试：AI/architecture PASS12.699s/1.406s、staticcheck PASS；篡改所有operation绑定、无观察、原始actor变化、同修订节点交集移除、scope改变/关闭/Profile关闭/run变unknown、返回后WithoutCancel观察均拒绝。R25真实加密凭据→本机TLS→Conversation成功测试现采用本闸，非return nil授权替身。固定DS源实际SQLite十二项PASS8.850s：模型double只在持久generating且当前观察核验后emit；裸Background上下文拒绝。模型、Profile/Filter/部分资源授权来源仍为明确fixture；没有外部模型、正式BYOK或完整生产授权链验收。
