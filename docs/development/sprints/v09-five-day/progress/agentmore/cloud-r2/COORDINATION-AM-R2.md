# AM-02 协调差异（第一检查点）

2026-11-04主控返修：新候选确定为`ds/0042_file_distribution`，仅新Version重编号，SQL字节不变。本PR单独组合27项；#7未接收，日后与cx/0010组合才28项。R9契约/错误DTO及真正25项历史fixture修复见[REPAIR-R1.md](REPAIR-R1.md)。下方占位编号/机械计数为原提案历史，不能据此放宽旧库断言。

## 候选迁移（am/0001_file_distribution）

- 位置：internal/files/migrations.go 末尾新增一项，SQL 字节见该文件；最终版本号由主控分配（当前用 `am/0001_file_distribution` 占位，主控可按其序列重编号，SQL 字节不变）。
- 新表：file_distributions（staging 头行，含 task_id 绑定）、file_distribution_targets（每目标冻结身份/写路径/状态/字节/摘要/错误，唯一索引 (distribution_id, node_id)）。

## 共享测试计数断言机械更新（迁移 +1）

与 AM-01 第一检查点同模式，仅改计数常量、不改语义：

- tests/integration/v09/platform/recovery_guard_r2_test.go：26 → 27（本包允许范围内）
- tests/integration/v09/wave6_r1_migration_test.go：26 → 27
- tests/integration/v09/controller_wave2_test.go：26 → 27（两处）
- tests/integration/v09/cloud_upload_history_test.go：26 → 27
- tests/integration/v09/services/backup_restore_test.go：14 → 15（files.Migrations() 子集）

## 中央文件未改动

server.go / Migrations() 装配、AuditActions、agent_links.go、connect、迁移登记表均未动；分发复用既有 files.transfer 动作与统一任务管线，无新审计动作目录需求（分发审计事件复用 files.distribution_create/complete/cancel 新动作串，均在 internal/files 域内注册——若主控要求并入中央 AuditActions 清单，本候选 diff 可供直接采纳）。

## 无他家代码消费

本检查点不依赖 CodeArts 网关候选或 WorkBuddy 任何未合并 PR；分支不含未接收共享提交。
