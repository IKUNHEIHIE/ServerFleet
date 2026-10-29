# V09-5D-CX 继续实施记录

会话 codex-02；审查 codex-review-01+安全/契约/owner；从b61ad15继续，仍在任务单登记的主仓库.worktrees目录和feature/v09-platform-cx02。用户明确要求继续完成；此前STATUS对“无独立工作”的判断不充分，现撤回该停止判断，不撤销真实测试记录。

## R01 真实身份持久化组合

主线仍8a7dd470，没有新的接口接收；只读核对已提交DS方案（pin bb8954988fe7b2f832edf291803529d7745e4f0a），未合并/改写他人目录。原S02通用RecordTx申请由本轮auth专用SQL适配替代；auth定义业务表和永久初始化标记，不分配公共迁移编号，不在storage中建KV域逻辑。

代码：internal/auth/identity_sql.go。通过窄函数把DS Queryer/Tx映射到auth自有IdentityStore，不导入database/sql，不猜不存在的公共API。构造不自动建表，IdentitySchemaStatements只提供待注册的业务迁移语句；缺表/缺行拒绝，绝不解释为未初始化。Update必须同一BEGIN IMMEDIATE、回调一次、取消/失败回滚；永久ever_initialized防止业务payload被错误清空后重开setup。

实验：tests/poc/v09/platform/run-identity.mjs将指定提交的storage生产源读入本任务忽略reports目录，使用单独固定module/sum（modernc.org/sqlite v1.61.0）。不改根锁/依赖，不使DS候选成为共同接口。`node tests/poc/v09/platform/run-identity.mjs --bootstrap-lock` 首轮PASS 4.265s；随后固定锁使用-mod=readonly重验。真实SQLite文件/WAL双连接并发初始化、关闭重开后会话撤销/initialized、取消回滚、反初始化/删除状态均验证。没有生产数据库或真实凭据。

仍待审：auth业务schema/存储接缝正式接收、审计消费、主入口接线、完整安全候选。继续实施用户/RBAC/显式授权与HTTP；不以这些未接收事项停止包内其他工作。
