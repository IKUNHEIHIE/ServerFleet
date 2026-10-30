# 私有单文件更新切换（PROPOSED，未启用运行profile）

对应CX-07/08。`updates.SlotInstaller`只服务程序注册的一处专用私有目录，固定active文件；没有HTTP传路径、任意Shell/包URL执行或数据库操作。输入来自唯一任务冻结的InstallIntent和StagedArtifact；Previous须来自可信已安装记录，不能采信请求中的“当前版本”。仍使用既有受签名manifest/平台/单调sequence校验，不修改公共契约/迁移/锁。这里不是完整Linux安装器、发布密钥治理或UPDATE正式原文的替代。

Apply：当前任务/审批gate→旧文件精确长度/哈希→O_EXCL持久全局锁→不可覆盖execution目录/intent→同FD签名复验并有界复制candidate→校验并保存previous→持久stopping→真实Stop→持久switching→Rename替换active→再核哈希→持久starting→真实Start→验证HealthEvidence→Commit安装记录/序列高水位/审计→持久receipt→解除本次锁。文件0700/目录0700、记录0600；os.Root+Lstat/SameFile拒绝symlink/路径逃逸，目录须由安装监管进程独占，不声称隔离同UID恶意写者。Linux文件及目录Sync；不采用删除active再rename的后备方案。Windows实际文件测试只证明逻辑，不证明Linux断电原子性或ACL完整。

所有Gate/Stop/Start/Health/Commit由可信组合层显式注册，缺项拒绝；不提供默认成功实现。Health绑定execution、组件/版本/完整SHA、进程标识、新启动时间及30秒内观测；适配器还必须验证真实运行文件/服务身份、readiness和存储兼容，字段结构不能自证真实性。调用者给有截止时间的ctx、适配器合作取消；不承诺强行取消不合作回调。Commit必须与安装provenance/审计及最大已尝试sequence高水位同Tx，成功返回真实审计记录引用；失败/未知不在本组件重试。共享持久事务/唯一任务结果端口尚未接收，未挂载入口。

只对明确`ErrHealthUnhealthy`、原任务AllowRollback、当前回退gate及必需的RollbackSafe真实兼容性检查同时通过，才停止新服务、校验本次精确备份并恢复、重启旧版和重新health。回退不适用于不可逆或未知迁移；不能只在重启旧版后才发现数据库不兼容。回执state=rolled_back表示升级失败且本次旧文件恢复已确认，绝不能显示“更新成功”，也不能降低反回退高水位。没有自动重跑停止/启动、重复回退或猜测式恢复。

任意取消/异常/哈希失败/缺失health/服务结果不明/commit失败/落盘失败均ErrApplyUnknown，保留全局锁和永久execution记录；进程重开也不恢复执行。需要独立受控的人工对账/恢复步骤，当前没有“清锁强制继续”接口。Receipt只读历史提交证据，不作为当前live health；损坏/缺失记录拒绝，历史receipt不能授权后续更新。备份/日志留存清理未实现，必须另有有界保留策略，不能部署为长期自动更新器。

测试：真实签名暂存/临时文件、备份/切换/精确回退/重开回执、每阶段撤权、双实例锁、旧哈希/路径/缺hook/序列拒绝、模糊Stop/Start/Health/Commit与panic、损坏回执/备份，均独立测试。服务/权限/审计/存储兼容回调明确为double，未执行fixture字节或虚称实际systemd/数据库迁移/双架构安装。首轮Windows只读属性导致Rename失败，改为所有者0700并保留哈希复验；首轮回执比较因time.Time单调时钟未序列化误报，改为比较完整序列化值，未放宽生产校验。首次并发测试误共享同一个暂存对象会串行其内锁，修正为独立暂存对象模拟独立进程。
