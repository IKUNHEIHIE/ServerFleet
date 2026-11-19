# 测试站日志清理

任务 LOG-CLEANUP-20261119，会话 codex-log-maintenance-20261119-01。工作树 /home/admin/sf-log-maintenance-20261119，分支 codex/log-maintenance-20261119，基线 9665e771c62a04795f8521e6fbe4da5d00a49c70。

系统设置「维护」新增日志清理。手动清理按已保存策略确认，定时清理开启后每日执行。默认保留最近 15 天，定时初始关闭。范围包括审计事件和已结束任务的执行日志，清理过程按批提交和保存删除数量。任务记录、审批、状态事件和正在执行的任务输出继续保留。支持 PostgreSQL 和 SQLite。新增迁移 ds/0092_log_cleanup，旧迁移和数据格式保留。
