# AM-R1 协调差异说明（供主控独立共享 PR 审查）

会话 agentmore-01；分支 feature/cloud-platform-am01。本分支为自洽可运行（CI 全绿）交付，
以下变更同时以精确 diff 形式供主控拆分/定夺编号后另行接收：

## 1. 正式迁移装配
`internal/files/migrations.go` 新增 `ds/0041_file_upload_task`（file_uploads.task_id 列+索引），
经 `files.Migrations()` 自动装配；版本字符串紧随 `ds/0040_files` 排序（迁移器按字典序全局排序）。
建议总控定夺最终编号（WAVE6-R4 建议 cx/0010 命名空间；如需 cx/ 命名空间，需同时调整排序策略或拆分为独立候选）。

## 2. 共享测试断言（shared-test-migration-count.diff）
组合迁移计数 25→26 / backup-restore 13→14 的机械更新，四处：
- tests/integration/v09/platform/recovery_guard_r2_test.go（本会话允许范围）
- tests/integration/v09/wave6_r1_migration_test.go
- tests/integration/v09/controller_wave2_test.go
- tests/integration/v09/services/backup_restore_test.go
测试语义未改。

## 3. HTTP 202/taskId 合同变化（候选说明，未改 OpenAPI/Proto/Web）
- `POST /api/v1/nodes/{id}/files/uploads/{id}/complete`：统一任务路径下 200 → **202 Accepted**，
  响应增加 `taskId` 与 `status:"dispatching"`；旧直推组合仍为 200/completed（向后兼容）。
- 新增只读端点 `GET /api/v1/nodes/{id}/files/uploads/{id}`：暴露 `status`（awaiting_data/dispatching/
  completed/failed/cancelled/aborted）与 `taskId`，用于任务终态懒对账观察；需要 Idempotency-Key。
- `PUT content` 与 `POST complete` 的响应 DTO 同步携带真实状态与 taskId。
OpenAPI 候选建议登记于 api/openapi/revisions/v09-r8/files-candidate.yaml（由总控接收，
本分支未新增契约文件）；Web 前端无需在本分支适配（不越界改 Web）。
