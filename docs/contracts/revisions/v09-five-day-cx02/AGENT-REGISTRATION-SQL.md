# Linux实际注册与事务内权限读取（PROPOSED）

R33 / CX-02、03、08；只在本任务隔离module组合固定DS源bb8954988fe7b2f832edf291803529d7745e4f0a，不是总控共享接收。`node tests/poc/v09/platform/run-identity.mjs`现同时编译/测试原SQLite包与实际apps/agent源副本；源哈希另存，不重写Agent实现。Agent生产源没有SQL或storage import，SQL仅在隔离测试适配。根依赖/迁移/Proto/生成/API不变。

为让R32 commit门真正读取**同一个事务**，本轮精确修订本域候选：

```go
EnrollmentStore.Update(context.Context,
    func(context.Context, []byte) ([]byte, error)) error
EnrollmentSQLBackend.InTx func(context.Context,
    func(context.Context, EnrollmentSQLStatements) error) error
```

仓储必须回调一次，提供继承取消的非nil可信txCtx，回调/审计/写入/提交任一错误回滚，不重试。应用适配在txCtx私有键绑定只读auth reader（从同一个DS Tx构造）；业务change/privileged将txCtx原样传给commit，SQL读取/审计/写入也使用它。不能从HTTP任意值构造此键、把“有context”当授权或在commit门嵌套auth writer；缺绑定直接拒绝。三参数构造与旧回调签名不能编译，需要总控同步接收。其他domain/DS公共端口未被假定已修改。

本机已有Ubuntu WSL2、UID1000、linux/amd64执行：实际Agent本地私钥/CSR→固定Root/HTTPS EnrollmentHTTP→真实EnrollmentService/SQLite pending→关闭重开数据库→已认证管理员决定→同Tx审计写后故障回滚→明确决定后issued→本地不可覆盖保存→重开身份文件→真实mTLS→管理员无默认节点权限拒绝吊销→显式节点grant→持久吊销后新mTLS握手失败、匿名注册重放不得再返回旧证书。真实会话在进入授权后/BEGIN前撤销，commit通过同Tx auth reader正确拒绝发令牌。CA signer为本机测试材料；新节点注册管理角色政策、资源resolver（仅已登记测试节点）和Current listener/profile仍明确测试输入，不代替正式PKI/profile/节点域/管理员HTTP页面或唯一任务执行验收。

首次Linux运行FAIL：最后一项拒绝审计不存在。实际DS validator只允许小写ReasonCode，而HTTP固定problem code为大写。适配现有七个固定码的明确映射，未知码拒绝，保留具体原因；未清空字段、放宽DS或改假成功。失败原始证据保留在reports/_review/v09-platform/linux-native/2026-10-30T095834811Z-1432c04b。第二次Ubuntu原生transport50/SQLite12/Agent含新组合22顶层PASS/无skip；LINUX-REGISTRATION-SQL-EVIDENCE.json记录精确源/二进制/日志及环境。22项含原Agent21项，不虚报22条新注册流程。Windows固定锁只读测试SQLite12与原Agent21通过，实际Linux-only组合明确SKIP；未冒充Windows Linux。

该组合没有WSS/统一任务/root执行/安装服务/arm64/1C1G/独立旧主控隔离证明。R28/R19另有真实WSS/执行组件证据，不能把独立组件拼写成已验收正式全链。运行profile、共享迁移/端口、正式PKI决策仍待总控；缺项只阻止具体挂载，继续其他独立实现。
