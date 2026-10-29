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

## 已知阻塞

- `go.mod/go.sum` 与迁移编号需总控接收（见 `SHARED-CHANGES.md`）；本分支可独立运行。
- 真实 Debian/Ubuntu、Cloudflare、ACME、QQ/SMTP、Nginx 环境未授权/未提供：相关验收保持 UNTESTED。
