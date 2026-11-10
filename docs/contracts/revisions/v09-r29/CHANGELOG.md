# R29 · 备份异地维护与异常人工处置

任务 V09-CLOUD-OPS-01；基于 main b49afc80。独立追加候选，不修改 G2 原始文件或冻结既有契约。生产实现、正式 React 页面和本候选一起交付。

| 类型 | 产品变化 | 接口／实现 |
| --- | --- | --- |
| 新增 | S3 兼容／WebDAV 异地密文上传；自动上传与完整读回校验 | `backups.upload`、`backups.remote.verify`，独立统一任务 |
| 新增 | 管理员备份策略；间隔、天数、最少份数、自动发起清理审批 | `/api/v1/backups/maintenance*`，修订 CAS |
| 新增 | 固定候选清单、保护说明、独立审批、逐项清理回执 | 冻结 digest／24h；`backups.retention` 修改类任务 |
| 新增 | 保留／取消保留、本地副本状态、活动下载保护 | `/{backupId}/protection`；`localStatus/pinned/localRevision` |
| 新增 | 统一异常收件箱，任务与 AI 终态异常自动收集 | `/api/v1/operations/incidents*`；独立人工记录 |
| 新增 | 认领、释放／接管、证据、提出结论、独立复核、驳回、重新打开、关联后续任务 | 原始执行状态和审批不改变；所有变更审计 |
| 新增 | 安全凭据“备份存储秘密” | kind `backup_storage_secret`，用途 `backup-storage`，模块 `backups` |
| 修改 | 自动备份身份及周期可由当前管理员在备份页配置 | 保存策略后 DB 策略优先于启动默认值；撤权停止后续执行 |
| 修改 | 备份页展示实际副本和维护任务；任务列表／详情包含人工处置入口 | 页面按需打开加载，当前身份变化清空；错误不伪造成功 |
| 修改 | 离线恢复后停用原自动策略、失效人工结论 | 需当前管理员重新配置／核验；不继承旧执行权 |
| 删除 | 无 | 不删除既有能力、测试、迁移或历史证据 |

数据库只追加 `cx/0021_backup_maintenance`、`cx/0022_operator_incidents`，完整组合共 42 个迁移。没有修改前 40 个迁移及其摘要；没有新增第三方依赖、Proto、生成文件、插件运行时或独立执行通道。

详细端点／请求／响应见 [operations-candidate.yaml](../../../../api/openapi/revisions/v09-r29/operations-candidate.yaml)。部署与处置流程见 [BACKUP-OPERATIONS](../../../development/BACKUP-OPERATIONS.md)。HTTPS、会话、CSRF、管理员、任务审批、当前凭据修订、运行权威和审计仍在代码层校验。接口列出的人工确认成功，只是人工证据结论，不代表把 UNKNOWN 改成机器成功。
