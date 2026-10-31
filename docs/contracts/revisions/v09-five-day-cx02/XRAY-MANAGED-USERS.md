# Xray持久用户元数据与任务应用状态（PROPOSED）

R38 / CX-06，codex-03接续codex-02。实现位于可选`plugins/xray-manager/users.go`；核心没有import/编入它，未挂载公开路由、启动插件运行时、调用Agent或Xray管理API。

## 业务行为

`ManagedUserService`按`ManagedUserScope{NodeID, InstanceID}`隔离。`ManagedUser`保存不变userId、inboundId、中央IdentityRef/CredentialRevision、DisplayName、StatsEmail、期望Enabled/Deleted、Revision、待应用task/plan、Application及已确认AppliedRevision/AppliedConfigRevision。只有引用和元数据，不接受UUID/私钥。StatsEmail固定为原版本模板已有的`sf-user-<userId>`；模板与本服务共用同一函数，统计CounterKey必须包含InstanceID，改显示名不改凭据或统计归属。

`Change(ctx, operation, change)`仅允许create、rename、set-enabled、remove；按预期修订CAS及永久operationId/完整输入摘要去重，不能移入站、换userId或重绑定凭据。remove保留不可重用的tombstone，保留历史流量关联；不是物理删除Xray配置或撤销中央凭据。create/重新启用需要同Tx中央材料检查：准确node/instance/inbound/user/purpose/reference/revision且当前ready。停用/删除已有用户不依赖可用私钥，避免已撤销材料阻止关闭用户。

每项修改持久记为`Application=pending`和准确TaskID/PlanDigest，既有已确认配置修订只作为历史观察保留，不能误报新修改已生效。`RecordApplication`只能接受同一待应用task/plan、当前user revision及已确认配置修订的`applied`或明确`unknown`；queued/前端开关不是应用成功。unknown后迟到success不能翻回成功，新计划必须重新CAS并获得当前授权。此服务不派发、排队、重试或调用HandlerService，实际配置持久化/目标版本测试/部署/健康检查及必要断开现有连接由唯一受控任务链实施。

持久PendingAction区分纯改名与物理配置变化：create/set-enabled/remove成功必须带严格递增的配置修订；纯显示名可引用同一已应用配置修订。确认启用用户应用成功前再次同Tx核查中央引用，材料已撤销/变化时不能标applied。输入证据及当前权限均参与gate，配置修订数字本身不充当部署证明。

`Read`仅返回上述元数据。export读取只允许真人web、启用且未删除、当前修订明确applied且中央引用仍ready的用户；返回引用供R35/R37受控导出借用，绝不直接返回连接秘密。pending/unknown/disabled/deleted/中央撤销或版本变化均拒绝导出。原配置/插件可信启用与修订仍由当前组合reader/admission复核，本服务不代替它们。

`List`每页1—100项，按不变userId排序、可明确请求tombstone；AfterID必须带有效SnapshotID。游标绑定当前完整持久快照、viewer、node/instance和删除过滤；任意用户变更/应用确认使旧游标冲突，跨主体/过滤不能复用。返回值独立复制，不能用显示名作为顺序/统计ID。列表是当前元数据观察，最终输出gate必须复核当前身份/节点及插件准入；网络输出仍有正常Tgate之后窗口，不能宣称与网络发送原子。

## 精确Host/安全候选

```go
type ManagedUserStore interface {
    View(context.Context, func(context.Context, []byte) error) error
    Update(context.Context,
        func(context.Context, []byte) ([]byte, []ManagedUserAudit, error)) error
}
type ManagedUserGate func(context.Context, ManagedUserCheck) error
type ManagedUserMaterialCheck func(context.Context,
    ManagedUserScope, ManagedUser) error
```

所有依赖构造必需，无默认store/gate/material。Host只暴露本签名包/节点/实例的私有业务元数据，不暴露数据库或任意SQL。Update必须在同一可信Tx调用一次回调、以txCtx绑定只读当前权限来源，原子提交返回state与完整typed audit；取消、审计、写入或COMMIT失败全部回滚且不自动重试回调。回调未调用/重复调用、nil/取消context拒绝。Missing/corrupt/跨scope/未知format拒绝，不自动初始化；`ManagedUserSeed(scope)`只供已审查新实例安装时一次插入，不能对丢失状态INSERT OR REPLACE。

ManagedUserCheck包含完整operation、实际Change、Application证据、分页请求与阶段；gate必须按这些真实参数比对服务器冻结计划，而非只信任传入PlanDigest/SubjectID/TaskID。prepare、同Tx commit-prepare/commit、读取/最终输出均复核真实认证/CSRF或准确受控任务来源、显式节点grant、当前插件可信启用/修订、AI/工作流资源交集与适用审批。application阶段还须验证该唯一任务真实配置持久化/健康/结果证据，不能用布尔断言自证部署成功。权限目录候选为node范围`xray.users.read/manage/export`，不新增默认节点授权；AI不能使用export。

ManagedUserAudit保留完整operation（含subject/node/instance/user/task/source/plan/期限）、Change、实际Application配置修订与结果、user revision及时间。普通DS摘要不可吞掉完整字段；测试同时写typed probe与真实DS审计。公共存储SDK、typed审计schema/迁移、权限目录、DTO兼容与正式Host接收由总控协调，本包没有分配公共表/迁移编号或修改OpenAPI/Proto/生成/根依赖。

最多5000用户（含tombstone）、10000永久operation receipts、8MiB元数据；达到上限明确拒绝，不逐出历史让旧ID或请求复活。旧数据库快照回退依旧需要外部恢复隔离；本聚合不独自解决已回退数据库的防复活问题。没有自动安装或运行尚未准入的Xray。

## 验证和未完成

新增6个顶层单元覆盖稳定ID/统计归属、期望与应用状态、CAS并发、重复/变参请求、审计回滚、源/提交阶段撤权、材料不可用与panic、跨实例/错误ref、unknown后迟到回执、缺失/损坏不重置、分页范围/快照/主体/过滤/复制隔离；停用/删除在中央材料已撤销时仍可进行，重新启用和导出拒绝。

扩展R37实际SQLite测试：真实当前身份/显式用户权限、同Tx中央材料绑定，阻止把user-a引用移给user-b；用户创建/应用确认→真实中央UUID借用→导出；改名后同一UUID与新标签；typed+DS审计写后故障使数据/审计均回滚；重开数据库后disabled/pending仍拒绝导出且分页保留状态；BEGIN前真实会话撤销拒绝修改。私有SQL探针只在隔离测试adapter，不是已批准Host/迁移。

Ubuntu WSL2非rootUID1000原生可选包17顶层PASS/3真实Xray二进制SKIP，SQLite15顶层PASS/无skip，精确源/二进制/日志见`LINUX-XRAY-USERS-EVIDENCE.json`。task/profile/配置部署证据及plugin当前快照仍明确fixture；没有宣称真实Xray用户停用/断连、安装、Host runtime、公开页面/QR或AC-23验收。后续必须先接收唯一任务/配置/Host接缝，并取得安全准入的目标发布物。
