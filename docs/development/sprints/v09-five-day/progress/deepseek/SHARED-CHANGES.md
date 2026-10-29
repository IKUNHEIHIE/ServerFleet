# DS 共享变更申请 · deepseek-02

公共文件保持总控单写。以下差异已在作者分支本地可运行，请总控用独立 contract/integration 提交接收；批准前不作为已冻结接口。

## 1. go.mod / go.sum：引入 SQLite 驱动

- 文件：`go.mod`、`go.sum`
- 精确差异（go.mod）：
  ```diff
   require (
   	google.golang.org/protobuf v1.36.12
  +	modernc.org/sqlite v1.61.0
   )
  +
  +require (
  +	github.com/dustin/go-humanize v1.1.0 // indirect
  +	github.com/google/uuid v1.6.0 // indirect
  +	github.com/mattn/go-isatty v0.0.24 // indirect
  +	github.com/ncruces/go-strftime v1.1.0 // indirect
  +	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
  +	golang.org/x/sys v0.49.0 // indirect
  +	modernc.org/libc v1.79.0 // indirect
  +	modernc.org/mathutil v1.7.1 // indirect
  +	modernc.org/memory v1.12.1 // indirect
  +)
  ```
- 理由：SQLite WAL 是已批准 G1 方向；本机 CGO_ENABLED=0 且无 gcc，选用纯 Go 的 modernc.org/sqlite。Go 1.27.3/windows-amd64 与 linux amd64/arm64 均可交叉编译（windows 实测，linux 未实测）。
- 调用方：仅 `internal/storage` 导入（blank + 错误类型）；领域包不接触驱动。
- 兼容性：新增依赖，不改现有 `google.golang.org/protobuf` 版本；`go mod tidy` 后 go.sum 增加 50 行。
- 测试：`go test ./...` 全绿；`go build ./apps/...` 由检查脚本覆盖。
- 回退：删除 storage 包对 driver 的导入并回退这两行；无数据库兼容负担（尚无生产数据）。

## 2. 迁移编号与台账约定（提议，待总控批准）

- 公共台账表由 storage 创建：`schema_migrations(version PK, name, checksum, applied_at)`。
- 作者不得抢写公共编号：本包建议使用域命名空间 `ds/0001_nodes`、`ds/0002_tasks`……；CX 使用 `cx/...`。版本字符串全序应用，跨命名空间不冲突。
- 每个迁移是单条语句数组，单事务执行；已应用迁移 checksum 不可改；库中存在未注册版本时拒绝启动（防降级）。
- 迁移代码归域所有（`internal/<domain>/migrations.go` 返回 `[]storage.Migration`），storage 只执行机制，不 import 业务。
- 若总控采用集中编号表，请以本约定为输入替换；storage 接口不变。

## 3. 领域→storage 的只读接缝（不新增跨域边）

- 新增 `storage.Queryer/Tx/Row/Rows/Result/NullString/NullTime/EncodeTime/DecodeTime/IsNotFound/IsUniqueViolation/IsBusy`。
- 新域只 import `internal/storage`，依赖白名单已登记；无反向 import。
- CX 需要的授权世代：`storage.Generations`（`Current`/`Advance`，CAS，持久）。auth 表仍由 CX 定义，本包只提供机制。
- 任务仓储/取消/并发约定在 DS-03 提交时补充到本节。

## 4. 未申请/未改动

- 未改 apps/controller、apps/agent、auth、transport、Web、API/Proto/gen、依赖图、CI、锁文件。
- 未新建 SSH 或第二执行队列；未接真实外部服务。
