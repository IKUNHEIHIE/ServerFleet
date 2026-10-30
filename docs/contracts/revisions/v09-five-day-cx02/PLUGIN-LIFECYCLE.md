# 插件持久生命周期候选

PackageVault在私有独立目录保存精确签名metadata、key-id、signature和payload，os.Root/O_EXCL/长度/哈希/签名/来源固定校验，读取时重复验证；构造不联网、不拉整仓库、不执行。VerifiedDescriptor供服务器任务planner冻结签名工件摘要。source禁止userinfo、query和fragment，避免令牌被作为来源元数据回显。未提供正式官方URL/发布密钥，不能虚构官方可安装制品；测试只用临时签名与保留invalid域。

LifecycleManager + SQLLifecycleStore保存安装/默认禁用、显式启动/停止、严格新版本升级、卸载tombstone、全局单调revision及每taskId永久防重放。capabilities必须与当前审查计划的consent完全一致，升级新增能力要重新确认；切换来源不会自动更换安装。缺gate/trust/仓储失败关闭；缺已准入runtime可以验证并保存disabled工件，但拒绝启用。

启用先持久starting/unknown意图，再在域事务外启动；Ready和当前任务gate复查后才enabled。禁用先持久拒绝Host/新任务，再Stop；失败为stop_unknown，不伪称已停/已卸载。Runtime每次启动使用唯一RuntimeInstance.ID并与当前记录核对，老任务清理只能停止自己的实例；新revision/新实例不会被旧启动晚到结果覆盖。已有Agent上的Xray不因卸载Controller插件而删除，模块无SSH/transport/可选Xray import。

CheckStoredInvocation核对实际capability、安装digest/revision/runtimeId、当前来源/签名、已准入runtime Ready及可信gate；只是生命周期准入，Host仍须经过唯一tasks的当前用户/精确节点/AI scope/审批/schema/审计，不存在执行旁路。RuntimeID/ActorID/taskId等是可信组合的绑定数据，不是客户端凭据。Runtime必须取消/停止同实例的在途Start，遵守5秒ctx；不合作函数无法被Go接口强行终止，需正式隔离profile/进程治理。

所有生命周期writes与audit消费用同DS Tx，nil audit仅持久保留出箱，不声称已到audit域。pending/unknown taskId不自动删/重跑；10000操作/事件、4MiB聚合上限。关闭重开后Enabled是持久意图，调用还必须实际Ready；不能把进程重启后的旧enabled字段当作已加载证明。不要在gate中嵌套auth writer，应使用已认证观察的只读当前校验。

这是治理/持久化实现，不选择生产WASM ABI/runtime。AdmittedRuntime默认nil；WASM局部内存/超时/Host拒绝PoC仍独立，不等于可发布的Xray插件或多租户隔离验收。公共迁移编号、统一Controller本地任务、真实Host ABI、包发行/官方源与密钥、Linux/1C1G完整PoC和HTTP页面仍待接线/接收。

实际验证分层：真实私有文件签名/篡改/权限consent；事务和runtime double测试启停/升级/未知/旧租约及并发晚到实例；真实SQLite+DS SQLRecorder测试安装与审计回滚、重启默认禁用与防重放、nil runtime拒绝、卸载保留工件。后者未伪造实际可执行插件或节点Xray成功。
