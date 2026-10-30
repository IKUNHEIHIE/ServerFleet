# Auth当前事务读取来源扩展（PROPOSED）

R36 / CX-02、06；为中央实例绑定材料等操作准备同Tx权限/插件/任务观察，不增加通用RecordTx、不修改DS源或公共schema。新增本域候选：

```go
type IdentityTransactionStore interface {
    IdentityStore
    UpdateWithContext(context.Context,
        func(context.Context, []byte) ([]byte, error)) error
}
// IdentitySQLBackend optional field:
InTxContext func(context.Context,
    func(context.Context, IdentitySQLStatements) error) error
```

业务已有update/IdentityStore.Update保持兼容；SQL backend至少提供旧InTx或新InTxContext之一，配置新项时SQL读/审计/写都使用可信txCtx。需要同事务来源的新操作必须走私有updateTransaction→UpdateWithContext，只有旧InTx时明确ErrIdentityUnavailable，绝不以原请求ctx冒充Tx reader。SQL及业务层均检查回调一次、非nil/未取消ctx、原请求取消、初始化不可反转和16MiB上限；跳过/重复回调不能报成功。

应用适配在BEGIN IMMEDIATE内以私有context键绑定同一个DS Tx的只读身份/策略reader。键、Source字面值及字段存在不是权限证明，消费方仍须验证真实当前主体、精确节点/实例/任务/审批/插件状态；ctx不能来自客户端或被用于嵌套写事务。本轮验证ctx与同一实际SQL事务内可查询的Tx关联，不声称新增材料、Host、权限政策或公开路由已完成/获批。正式迁移/事务接收SHA仍缺；原安全资料未补写。

两个新单元顶层测试覆盖旧源缺失/回调缺失与重复/nil/取消/原始状态回滚，PASS4.045s。首轮测试错误假定fixture为空，实际fixture已初始化；改为与原字节精确比较后通过，未放宽初始化或取消要求。完整auth/architecture PASS28.516s/1.514s、staticcheck PASS。实际SQLite十四项PASS8.951s，新增项验证旧适配失败、新适配在同Tx查询、取消回滚、旧业务使用新适配仍可初始化且永久标记不能清；Agent21 PASS7.053s/真实Linux-only注册Windows明确SKIP。

Ubuntu非root原生auth58/SQLite14顶层PASS，LINUX-AUTH-TRANSACTION-EVIDENCE.json为精确源/二进制/日志。此为同Tx来源组件证据，不等同所有新模块正式组合、root/Agent准入或完整v0.9。继续中央Xray材料/真实单次导出接线，公共接口由总控独立接收。
