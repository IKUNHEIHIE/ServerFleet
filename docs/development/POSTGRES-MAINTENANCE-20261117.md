# PostgreSQL 维护兼容与开发基线核对

任务 PG-MAINTENANCE-20261117；会话 codex-pg-maintenance-20261117-01。
范围为远程开发基线与备份/恢复/更新兼容；不执行整套核心功能实机验收，不更改线上部署。

## 已核对的开发基线

2026-11-17 从 GitHub main 3b32b1f51b1d9f9f862fcbc9b896153f5eaebbc7 建立独立
fix/postgres-maintenance-20261117，目录 awskr /home/admin/sf-pg-maintenance-20261117。
旧 /home/admin/sf-nodes-20261115 的 HEAD 是 62c295b25c487d0e46c61f8fefc4e283cce332de。
它的 128 个修改/未跟踪文件中，109 个字节相同；忽略 CRLF/LF 后 125 个与 main 相同。
其余两项 git-policy 文件少了 main 已有的 style 标题支持；
tools/pgmigrate/main.go 只有 CRLF 与末尾换行差异（详见远程证据）。
这三项没有作为新功能搬入任务分支，旧工作树保留原样，未 stash/切换分支/提交或清理。

线上程序为 /opt/serverfleet/bin/serverfleet-controller，
SHA-256 efef9a468a4547dedc9d033e0b63a4cb681a235cf5f5f9a6c2efbb9e4d696e5b，
运行参数确认使用 PostgreSQL，状态锚点 /var/lib/serverfleet/controller.db。
首页 HTTP 200、readyz 五项 ready。运行二进制没有可读的 Go 构建提交信息，
不能据此认定它等于 PR #54 的精确 head；本次未替换该程序或服务配置。

原始文件摘要、归一化比较、差异补丁、运行程序摘要保存在任务目录
reports/pg-maintenance/baseline.json 与 baseline-differences.patch（不含凭据）。

## 最终行为

- PostgreSQL 的 BackupTo 从一个 REPEATABLE READ READ ONLY 事务导出全部产品表，
  使用校验通过的完整迁移注册表重建可移植 SQLite 快照，保留迁移账本、
  外键、二进制空值与空 BLOB 区分，以及序列高水位。拒绝未知表/列及账本漂移；
  失败删除临时明文，成功以 0600 文件独占发布。
- 保持加密包 format=1、controller.db、离线 stage/reconcile 不变。
  恢复到 PostgreSQL 仅允许空 public schema；建表、导入、外键恢复及验证在一个事务内。
  无 TRUNCATE、无超级用户 session_replication_role、失败无部分提交。
- 新增 recovery restore-postgres，将已 reconciliation 的隔离快照导入空库；
  不解除隔离、不启动服务、不恢复原任务执行。activate/status 与监督器读取所选数据库，
  保留离线 Root 授权、独立真实 fence、持续复核与 UNKNOWN 约束。
- 更新 schema 指纹覆盖 PostgreSQL 列/类型/默认值、约束/索引、规则/触发器、视图、
  函数、RLS 与序列定义；应用行与序列当前计数不改变指纹。
- 监督器通过受策略摘要约束的私有 DSN 文件连接相同数据库；新增 -db-dsn-file。
  PostgreSQL 的 ReadOnly 连接实际启用只读事务默认值。
- 审计回执 ID 使用 INSERT RETURNING，修复 PostgreSQL 恢复激活的 SQLite 专用 ID 查询。
- pgmigrate 改为调用同一空库事务导入端口，改用 -pg-dsn-file，
  删除原 -pg 明文 DSN 与 -table 部分覆盖模式；已初始化目标明确拒绝。

这是应用级可移植备份，不替代 PostgreSQL 集群级备份、角色/权限/扩展或自定义 DDL 备份。
恢复必须使用与快照账本匹配的代码；较旧快照先经现有离线 reconcile 追加迁移。
监督器的 PostgreSQL 指纹需以新算法重新预置，不沿用旧表名哈希或伪造原策略摘要。
部署说明见 [PostgreSQL 维护](../../deployment/POSTGRES-MAINTENANCE.md)。

## 验证

专项测试在 awskr 用户私有 Unix socket 的独立 PostgreSQL 17 集群运行，
未连接站点数据库，表所有者为无超级权限的 maintenance_owner。
已验证：真实迁移、数据往返、拒绝覆盖、失败事务回滚、并发写入一致性、
DDL 指纹、私有连接绑定、恢复 Root/mTLS/owned-process fence 与激活。
竞态检查：隔离 PostgreSQL 集成包与整个 Controller bootstrap 包通过。
Web 类型检查、53 文件/481 用例与生产构建通过；npm audit 与 govulncheck 无漏洞。
策略/模块边界/actionlint 通过。完整接收检查与最终精确 head 的 CI 结果补 PR 评论，
不在本文预报尚未结束的检查。第一次完整检查因脚本固定 GOTOOLCHAIN=local 而选到
Go 1.27.3，policy/contracts/go 未运行成功；已选用服务器缓存的 1.27.4，分别重新检查。
保留首次失败日志与后续结果；不因 Web/专项通过声明 G2 或全产品验收完成。
本次未部署、未运行真实站点的供应商/用户流程、未验证整集群灾备。

## 接收检查补充

提交 efc7ba3665a2e313f0c585eb4d291c9b7f6a72e5 的 Foundation CI 44065480658
中 foundation、postgres-maintenance、dependencies 三项全部通过，含全仓 race、
安装回归及双架构构建。后续发行包文档收录补丁按新 head 再检查，结果写 PR #55 评论。

awskr 全仓 Go 检查保留 FAIL：监督子进程就绪超过 15 秒；
platform 包构建报 /tmp 空间不足，导致共享构建结果的 7 个用例失败。
这不挪作通过；本次 PostgreSQL 集成与 bootstrap 包 race 均通过。
源码核对、文档 79 处相对链接、diff 空白检查通过；现场旧文件摘要未改变。
