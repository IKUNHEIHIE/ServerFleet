# 数据库之外的恢复隔离闸（候选）

实现 `internal/recovery/quarantine.go`。这不是正式PKI/UPDATE原文替代，也没有批准线上恢复。`BeginQuarantine`在恢复/打开数据库之前落盘，目标是已存在、独占、Linux权限0700的绝对目录；该目录必须在恢复数据库/快照范围之外。唯一hold绑定随机256位ID、快照SHA256、新Controller ID、Root SPKI、最小信任世代及撤销序列。元数据0600、O_EXCL、文件Sync及Linux目录Sync；错误/半写不删除重建。Windows测试只证明逻辑，不证明断电持久或Linux权限。

重复相同intent保留hold；不同intent拒绝。已写解除记录的目录不能再次Begin另一恢复回合。`OpenQuarantine`需要外部恢复登记保存的完整expected hold，不可从被恢复数据库取expected，不可将文件丢失解释为正常新安装。恢复目标及外部记录的可信配置/权限需受控安装流程接收；本模块不防能改写目标所有文件的系统管理员。

`Check(ctx)`缺hold、解除记录、当前verifier或来源失败即ErrQuarantined。它必须位于恢复profile中管理入口、Agent连接和每次任务派发之前；不取代普通身份/资源/审批/任务校验。不能只在启动时检查一次，也不能开放旧Cookie先进入管理API。

`Release`只能由程序注册的 `QuarantineVerifier.Prepare/Current` 驱动。Prepare返回前必须完成实际离线权威核验、旧Controller隔离、撤销并集、全部旧会话/审批失效、新密钥/epoch及在途任务UNKNOWN等协调事务和审计，返回实际审计ID和证据摘要。Current每次重新核对仍有效的权威、隔离及数据库对账状态；不能实现为只验JSON形状、摘要存在或HTTP `fenced=true`。回调5秒ctx、异常脱敏失败关闭，须遵守取消；接口不能强杀不合作函数。

解除记录只含绑定元数据/证据摘要和审计引用，不含CA或恢复密钥；先域事务和审计提交，再文件落盘，绝不删除hold。文件写失败可保持隔离；已有不完整记录不自动修复。随后Current拒绝时再次封禁，即使release.json存在。不提供HTTP解除接口、默认verifier、恢复密钥格式或伪造离线证据。

现有 `Reconcile` 仍是惰性计划：连续Root、信任/撤销序列不倒退、撤销并集、会话/审批全失效、在途UNKNOWN、过期终止，输出仍quarantine。DS任务实际表/世代事务、离线解密/权威材料/旧Controller隔离方法和正式安装profile尚未接收，所以真实解除保持BLOCKED；单元正向verifier为明确double。

实际SQLite实验使用固定DS源bb8954988fe7b2f832edf291803529d7745e4f0a与单独锁，不改公共storage或迁移登记：真实BackupTo→后续撤销Cookie→真实RestoreFile到新文件。旧快照确实含撤销前Cookie，但外部hold仍拒绝Check/Release，关闭重开闸后仍拒绝；RestoreFile再次覆写目标被拒绝。这是本机恢复隔离负向证据，不是完整灾备/加密备份/物理fencing验收。
