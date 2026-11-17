# PostgreSQL 备份、恢复与更新

适用于本次维护兼容代码；v0.9.2 原发行包不自动获得这些能力。
线上是否部署以实际程序摘要为准。[实现与验证记录](../docs/development/POSTGRES-MAINTENANCE-20261117.md)。

## 私有连接配置

将 PostgreSQL 连接字符串放在运行用户可读的绝对路径普通文件中，权限 0600，父目录 0700。
Controller 原有 HTTPS、PKI、Origin 等参数保持原部署要求；数据库参数使用
-db-driver postgres -db-dsn-file /private/postgres.dsn -db /private/controller.db。
-db 是本地状态锚点，PostgreSQL 数据不在该文件；不要复制锚点冒充数据库备份。
-db-dsn-file 与 -db-dsn 不能并用。错误输出不包含连接字符串。

新 provision 的 controllerArguments 同样使用 -db-driver 和 -db-dsn-file，
生成监督器记录 databaseDriver、databaseDsnFile、databaseDsnSha256，
文件内容不进入监督器参数或备份包。更换连接配置属于策略变更，
不能通过改文件、沿用旧策略摘要来静默切换数据库。

## 加密备份

按原 [部署说明](README.md) 配置 age 公钥、固定加密程序摘要、配置/身份文件及统一任务。
PostgreSQL 与 SQLite 保持相同的加密包格式和恢复验证接口；
应用 schema 来自已验证迁移，数据来自单个 PostgreSQL 只读一致性事务。
数据库服务用户需能读取全部产品表和序列；缺少读取权限则备份失败。

快照是私有可移植 SQLite 文件，包含原账本与数据，随后进入原加密管线。
它不包含 PostgreSQL 登录角色、集群设置、扩展、自定义对象或 DSN。
需要 PostgreSQL 集群级备份时另按 PostgreSQL 官方 pg_dump/pg_basebackup 流程管理，
不能将本应用备份称为整集群快照。

## 离线恢复到 PostgreSQL

1. 按原 [恢复流程](README.md) 用可信摘要、离线 age identity、原 Root pin 完成
   recovery stage 和 recovery reconcile，得到隔离目录中的 restored.db 与 reconciled.json。
   现有 Root 新代次、凭据轮换、任务 UNKNOWN 与隔离规则不变。
2. 由操作者预建一个独立空 PostgreSQL 数据库及其所有者，public schema 不能有已有对象；
   该用户需要建表权限，不需要超级用户。将新连接字符串写入单独 0600 文件。
3. 在离线环境执行：

    controller recovery restore-postgres -destination /private/restored -postgres-dsn-file /private/postgres.dsn -offline-ack

   导入 schema、数据、外键与序列在同一事务内完成，目标非空即拒绝。
   此命令不会解除 hold，不连接旧 Controller，不签发新授权，也不启动任何服务。
4. 在新监督器的受信配置中选择 postgres 及该私有 DSN 文件，
   database 仍指向 /private/restored/restored.db 作为本地隔离锚点；
   databaseDsnSha256 为去除文件首尾空白后的 DSN UTF-8 SHA-256。
   重新预置本次算法的 PostgreSQL schemaSha256，并让子进程使用相同数据库参数。
5. 继续原 recovery activate/status 流程，必须通过真实独立旧进程 fence 与持续校验。
   导入成功或有 reconciled.json 不代表可启动。不要删除 restored.db 或 hold 等离线证据。

普通停止状态的 SQLite → PostgreSQL 导入可使用同一源码中的工具：

    go run ./tools/pgmigrate -sqlite /private/snapshot.db -pg-dsn-file /private/postgres.dsn

源必须是停止后的一致性独立快照、完整账本与当前代码匹配；目标必须为空。
原 -pg 和 -table 参数不再支持；不再提供直接清空已有目标表的模式。
有现有数据的目标请新建独立数据库，不在导入工具中覆盖。

## 更新兼容

Controller 与产品更新使用当前数据库的 DDL 指纹。
PostgreSQL 指纹包含列类型、约束、索引、规则、触发器、视图与序列定义；
增删普通行或分配序列号不改变指纹。
若新程序迁移改变 DDL，原“可安全回滚”判断仍应拒绝；
本次没有增加数据库自动降级、自动恢复旧任务或绕过签名/审批的路径。

参考：[一致性隔离](https://www.postgresql.org/docs/17/transaction-iso.html)、
[外键约束验证](https://www.postgresql.org/docs/17/sql-altertable.html)。
