# DS 进度 · deepseek-02

分支 `feature/v09-services-ds02`；基线 `8a7dd470`。每个检查点记录实际命令与结果；未验证项明确标 UNTESTED/BLOCKED。

## 检查点 C1 · DS-01 存储/迁移接缝

- SHA：`629a640d0e446f1dd791aa9fda73ca229823740d`（`feat(storage): add SQLite WAL, immediate transactions and migration seam`）
- 依赖：`modernc.org/sqlite v1.61.0`（纯 Go、CGO=0，无需 gcc）；直接依赖 1 个 + 9 个 indirect。go.mod/go.sum 属总控单写文件，精确差异见 `SHARED-CHANGES.md`，需总控接收后再同步主线。
- 实现：`internal/storage/{storage,tx,migrate,generation,backup,errors}.go`、`internal/storage/testkit/db.go`。只有 storage 导入 `database/sql`；领域包使用 `storage.Queryer/Tx/Row/Result`。
- 语义：
  - `Open` 强制 WAL、busy_timeout、foreign_keys=1、synchronous=NORMAL、启动 `PRAGMA quick_check`；只读模式禁止写。
  - `InTx` 进程内单写者排队（ctx 可取消）、`BEGIN IMMEDIATE`；fn 返回错误/ctx 取消/panic 一律回滚；回滚后不可能报告成功。
  - 迁移按命名空间版本全序应用，checksum 漂移=拒绝，库中存在未注册版本=拒绝（防降级）；每个迁移单事务。
  - `Generations` 提供持久单调计数与 CAS（expected 0 仅首次创建），给 CX 作凭据/会话世代接缝。
  - `BackupTo`=VACUUM INTO+只读校验；`RestoreFile` 拒绝覆盖现有文件、先校验再原子改名。
- 实际命令/输出：
  - `./.tools/go/bin/go test ./internal/storage/...` → `ok ... 3.182s`
  - `./.tools/go/bin/go test ./...` → 全部 ok（含 architecture/contracts）
  - `go mod tidy` 已运行，go.mod 直接 require modernc.org/sqlite。
- 测试覆盖（真实文件库，非 :memory:）：重启保持+迁移跳过、坏库拒绝、错误回滚、取消回滚、panic 回滚、24 并发写者全部成功、checksum 漂移拒绝、未知版本拒绝、备份/二次备份拒绝/恢复、只读拒绝写、generation 并发 CAS 仅 1 成功、迁移版本校验。
- 未覆盖/风险：坏库仅覆盖“非 SQLite 文件”与启动 quick_check；页级损坏需真实损坏样本（UNTESTED）。1C1G 与 100 节点吞吐留给 POC-07（DS-09）。跨进程并发写只依赖 busy_timeout，未做多进程压力（UNTESTED）。
- 阻塞：无。`go.mod/go.sum` 需总控接收；迁移版本命名（`ds/....`）需总控批准，未批准前仅本包使用。

## 检查点 C2 · DS-02 节点/分组/服务组（进行中）
