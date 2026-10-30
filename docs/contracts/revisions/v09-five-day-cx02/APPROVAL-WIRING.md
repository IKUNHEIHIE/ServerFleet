# 审批持久化与完整任务绑定候选

auth.ApprovalService保存pending→approved/denied→consumed、revision、主体epoch、批准者epoch/真人Web来源/管理员事实、策略世代、期限和已消费taskId。新增tasks.approve权限，仍须显式节点grant，角色不赋予节点。AI任意root Shell即使full也要求真人管理员；普通读角色无批准/修改能力。GET也查当前权限，不因管理员角色读取全部未授权节点的计划。

Request只收服务器prepared-plan引用；mandatory resolver返回统一任务planner的规范摘要、action、source/plugin/AI会话与scopeRevision、冻结nodes、到期和schema生成的可审查脱敏摘要。客户端不能提交这些结构成为权威事实。重复plan引用改参冲突，不重建已消费/过期/失效记录。ttl构造显式给1–30分钟，测试5分钟，是新决策输入，不补写成历史已批准数值。

Decide只由真实Web严格TLS/Origin/Cookie/CSRF组合调用，不能注册为AI/Host工具。批准时再查用户/批准者当前动作与tasks.approve的逐节点授权、epoch、policyRevision、AI范围/插件。根Shell还查当前管理员。模型boolean/mode/approvalId不构成批准。Get显示因epoch/策略变化而revoked，过期为expired；不将已无效批准显示为就绪。

ConsumeTx没有独立提交版本，只接受调用方正在持有的DS Tx映射为IdentitySQLStatements；同一事务读取auth私有状态、复核完整Binding、消费一次并写审计，再由tasks写入/提交。外层插入/审计/取消/commit失败全部回滚。Binding必须取实际服务器任务字段，不得照抄approval记录制造相等：taskId、subjectId、actionId、digest、source、pluginId、aiSessionId、scopeRevision、nodes。任何一项不符拒绝，旧批准不能重放给新任务。

## S03补充请求（未接收）

已核对DS pin bb8954988fe7b2f832edf291803529d7745e4f0a及当前feature/v09-services-ds02，main仍8a7dd470。当前ApprovalConsumer仅接approvalId/subjectId/digest/nodes，不传source/scope/plugin/AI session；planHash未绑定这些字段。不能用这个旧端口无损适配完整auth消费。请DS/总控以以下精确类型/调用差异协调（不改他人文件）：

```go
// internal/tasks/service.go，替换现有ApprovalConsumer签名
type ApprovalConsumption struct {
    ApprovalID, TaskID, SubjectID, ActionID, PlanDigest string
    Source audit.Source
    PluginID, AISessionID string
    ScopeRevision uint64
    NodeIDs []string
}
type ApprovalConsumer interface {
    ConsumeApproval(context.Context, storage.Tx, ApprovalConsumption) (string, error)
}
// 在既有Service.Submit的同Tx调用处，全部从实际server-built task/request取值：
approver, err := s.approvals.ConsumeApproval(txCtx, tx, ApprovalConsumption{
    ApprovalID: request.ApprovalID, TaskID: task.ID,
    SubjectID: task.SubmitterID, ActionID: task.ActionID, PlanDigest: task.PlanHash,
    Source: source, PluginID: task.PluginID, AISessionID: task.AISessionID,
    ScopeRevision: task.ScopeRevision, NodeIDs: snapshot.IDs(),
})
```

配套精确字段：SubmitRequest和Task增加AISessionID string；持久task schema/repository新增ai_session_id（新不可变迁移，不改0030/0031已给版本），读取/插入/更新/修订一并保持。planHash输入必须同时包括SubmitterID、ActionID、OperationClass、Revision、CanonicalParams、TargetType/TargetResourceID、Source、PluginID、AISessionID、ScopeRevision、排序冻结NodeIDs。需server-only PreparePlan复用相同规范器返回摘要和脱敏预览，不能让客户端提供摘要或抄写本地AI BindCall为tasks摘要。根文件/Proto/生成/schema编号由总控单写，本包未改；这份类型/字段请求尚不能直接当完整数据库patch合入。

调用方apps/controller适配同Tx→auth.ConsumeTx；旧实现者签名需编译期更新，缺全字段失败关闭，不保留弱签名的默认兼容放行。回退是不挂载新审批/执行入口，保留已有离线逻辑。验收需同Tx回滚、改任意metadata/节点/参数/主体拒绝、root强制真人管理员、epoch/插件/AI scope撤回、重启防重放。DS当前对submitter==approver保守拒绝；AI发起与同账号真人Web批准的策略需要总控明确，不能自行绕过DS限制或声称该场景已接通。

时钟边界：ConsumeTx因外层错误回滚时不能单独保存新ClockFloor。入口应在进入task Tx之前完成当前auth观察，失败后需在事务外保存安全时钟观察；系统时间回退/恢复仍须正式安全决策。不能仅凭内层一次过期拒绝就声称跨外层rollback的时钟观察永久保存。正常Get/Decide过期观察会真实保存。

证据：单元包含完整metadata不符、root非管理员、改参/改节点、审批重放、CSRF、过期/回拨、批准者口令epoch、外层提交失败回滚。隔离真实SQLite+DS SQLRecorder验证消费+审计外层失败一起回滚，关闭重开仍approved，最终提交后重开仍不可重放。planner和执行任务为明确fixture，非实际DS任务/Agent/真人浏览器完整验收。
