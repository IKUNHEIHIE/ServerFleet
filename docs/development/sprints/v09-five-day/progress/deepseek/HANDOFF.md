# DS 交接 · deepseek-02

## 当前 HEAD

- 基线 `8a7dd4707d2d7e142e137d098296788d2c80eddd`
- C1 `629a640d0e446f1dd791aa9fda73ca229823740d`：storage/migration seam

## 给 CX（codex-02）的接缝

1. 打开数据库：
   ```go
   db, err := storage.Open(ctx, storage.Config{Path: cfg.DatabasePath})
   migrator, err := storage.NewMigrator(cxMigrations, dsMigrations...)
   report, err := migrator.Apply(ctx, db)
   ```
   `Config{ReadOnly:true}` 只用于备份/诊断；启动后禁止迁移。
2. 事务：`db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { ... })`。事务内只能用 `tx`，不要再调 `db.InTx` 或 `db.Exec`（会因单写者锁自锁）。取消/panic 自动回滚。
3. 授权世代：`storage.NewGenerations(db)`；`Advance(ctx, scope, expected)` 以 CAS 拒绝陈旧写入；`expected=0` 仅首次创建。auth 表结构仍由 CX 定义，本机制不替代身份/审批。
4. 错误分类：`storage.IsNotFound/IsUniqueViolation/IsConstraintViolation/IsBusy/IsReadOnly/IsCorrupt`。
5. 迁移命名：建议 `cx/0001_auth`、`ds/0001_nodes`；每迁移单条语句数组；checksum 漂移与未知版本均拒绝启动。
6. 可信主体：本包 HTTP 层只接受 Controller 注入的 subject 解析函数，不解析 Cookie、不建第二身份源（DS-09 提交时给签名）。

## 给 GM（gemini-02）的业务接缝

- DTO 以 `api/openapi/g2-merged-candidate.yaml` 为输入；DS-02 起逐步提供对应 Handler/Service。失败返回固定错误码，不放默认 mock。
- 未完成端点不会返回 501 假成功：未实现即不注册路由，文档 `STATUS.md` 标明。
- 已注册（Candidate 路径）：`GET /api/v1/nodes`、`/groups`（含分类与成员）、`/service-groups`（含成员/暂停/派发预览）、`/tasks`（创建/预览/详情/列表/批准/取消/executions/SSE）、`/transfers`（创建/列表/详情/取消）、`/nodes/{nodeId}/files/uploads`（创建/完成）、`/audit/events`（列表/单条）。上传字节通道与目录列表仍由 CX transport 提供，DS 不直连 Agent。
- DTO 差异：任务 `partial_failure` 映射为 `partially_failed`；`unknown/expired` 原样暴露；审批需要 `planDigest`（先 GET 列表获得）；审计 eventId 暂为数字字符串。
- 固定错误体：`{"error":{"code":"...","message":"..."}}`；401/403/404/409/422/503 与候选一致，500 不返回内部细节。

## 与 Codex CX-01（2fc8dab，未接收）的对接点

- 身份事务：直接用 `storage.DB.InTx` + `storage.Tx`；auth 自有 SQL/表由 CX 定义，DS 不读 auth 私表，也不提供通用 namespace KV。
- 世代：`storage.Generations.Advance(scope, expected)` CAS；auth 不要同时维护第二份 epoch 真相。
- 任务提交：`SubmitRequest{CanonicalParams, Source, PluginID, ScopeRevision, ApprovalID, IdempotencyKey}`；计划摘要由 DS 计算，客户端摘要不作授权依据。
- 外部审批：实现 `tasks.ApprovalConsumer`，必须用回调提供的 `storage.Tx` 原子消费并返回审批人；DS 同事务落 revision/摘要绑定审批与审计，失败全回滚。
- HTTP：`tasks.Register(..., readGate)`；`ReadGate.AuthorizeTaskRead(subject,taskID)` 每次读取和每轮 SSE 复检，nil 即 403。CX 负责 Cookie/CSRF/401/503 与主体绑定。
- 未完成：scopeRevision 的执行期撤销复检、Controller 入口接线、真实 transport/Agent 帧。

## 迁移台账（本地候选，待总控批准）

`ds/0010_audit`、`ds/0020_nodes`、`ds/0021_servicegroups`、`ds/0030_tasks`、`ds/0031_tasks_cx_fields`、`ds/0040_files`、`ds/0050_monitoring`、`ds/0051_notifications`、`ds/0052_quota`、`ds/0060_dns`、`ds/0061_certificates`、`ds/0070_gateway`、`ds/0080_publishing`。0030 未改写，CX 增列走 0031。

## 已知阻塞

- `go.mod/go.sum` 与迁移编号需总控接收（见 `SHARED-CHANGES.md`）；本分支可独立运行。
- 真实 Debian/Ubuntu、Cloudflare、ACME、QQ/SMTP、Nginx 环境未授权/未提供：相关验收保持 UNTESTED。
