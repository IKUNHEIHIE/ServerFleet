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

## 5. 对 Codex CX-01（2fc8dab，未接收）S02/S03 的具体适配差异

以下为讨论/测试输入，不是已冻结接口；未获总控接收前不接主线。

### S02 storage：不提供通用 `RecordTx/View/Update` KV

- 现状：`storage.Open` + `DB.InTx(ctx, func(ctx, Tx) error)`；`Tx` 提供 `Exec/Query/QueryRow`、`Result.RowsAffected`。没有 namespace/key/value 记录 API。
- 身份事务可直接在一个 `InTx` 内完成：条件删除/更新 setup 行并检查 `RowsAffected==1` → 插入管理员 → 写入 initialized；任一失败或 ctx 取消自动回滚，回调只执行一次，`InTx` 返回 nil 才可交付秘密。
- 串行化：进程内写者信号量 + `BEGIN IMMEDIATE`；跨进程用 `busy_timeout`。读用连接池并在 WAL 下并发。
- 隔离理由：通用 KV 无法在 storage 层执行“auth namespace 仅组合根绑定”的安全断言；auth 表/迁移应由 CX 定义，storage 只提供事务机制，符合既定边界。
- 差异待定：`me` 若需要多条查询的一致快照，可改用一条 JOIN，或由总控批准新增 `DB.InView`（`BEGIN DEFERRED` 只读事务）；当前未实现。
- 授权世代：`storage.Generations.Advance(ctx, scope, expected)` 是持久 CAS（`expected=0` 仅首次创建），可直接承载 credentialEpoch/sessionEpoch；不要同时维护 auth 行内 epoch 与 Generations 两份真相。
- 错误分类：`IsNotFound/IsUniqueViolation/IsConstraintViolation/IsBusy/IsReadOnly/IsCorrupt`；不要解析错误文本。
- 并发初始化测试：同一 setup 行只允许一个 `RowsAffected=1`；取消/提交失败回滚；重启后 initialized/撤销仍生效。用 `InTx` 即可表达，无需新增 `records.go`。

### S03 tasks：字段已落到 `SubmitRequest`，PlanDigest 不接受客户端输入

- 现有 `SubmitRequest` 已含：`CanonicalParams []byte`、`IdempotencyKey`、`ScopeRevision uint64`、`Source audit.Source`、`PluginID`、`ApprovalID`；目标快照仍由 `NewTargetSnapshot` 冻结。
- `PlanDigestSHA256` 由服务端计算（action + class + revision + 规范参数 + 快照顺序），客户端提交的摘要不参与信任决策；HTTP 审批要求摘要与当前计划完全一致。
- 外部审批原子消费：`ApprovalConsumer.ConsumeApproval(ctx, tx, approvalID, subjectID, planDigest, nodeIDs)` 收到的是 **DS 的同一事务**，必须使用该 tx；返回审批人后 DS 在同一事务写入 revision/摘要绑定的 `task_approvals` 行并审计。消费者/审计失败 → 任务与消费一起回滚。派发前仍复检摘要、审批人≠提交人、TTL（默认 30m，可配）。
- 参数：接受 map 或规范 JSON；服务端重新规范化，拒绝密码/token/私钥类键，≤32 键、≤32KB。`Source=plugin` 必须带 `PluginID`。
- 未完成的适配：`ScopeRevision` 已持久化并随任务可读，但 AI 授权范围撤销的执行期复检仍需 CX 提供授权范围读取端口；当前不会因 scopeRevision 变化自动终止已派发任务。
- HTTP 注册签名为 `Register(mux, service, authenticator, groupResolver, classResolver, readGate)`；`ReadGate` 每次读取与每次 SSE 输出复检，nil 即 403。CX 必须实现该端口，DS 不解析 Cookie/CSRF。
- 兼容性：旧 `internal/tasks/ports.go` 未改；新字段只加在 Service 层请求，旧离线测试继续通过。没有第二队列或旁路。
- 迁移：`ds/0030_tasks` 保持原 checksum；新增 `ds/0031_tasks_cx_fields` 增补 scope_revision/plugin_id/source/approval_ref。若总控已把 0030 应用到持久库，直接顺序应用 0031，不修改 0030。


## 6. 模块 HTTP 注册现状（候选，未接收）

- 已注册：`nodes.RegisterHTTP`、`servicegroups.RegisterHTTP`、`tasks.Register`、`files.RegisterHTTP`、`audit.Register`、`dns.RegisterHTTP`、`notifications.RegisterHTTP`、`gateway.RegisterHTTP`（部分）。
- 所有注册函数都要求 Controller 注入 `Authenticator`（主体+request id）；不解析 Cookie/CSRF、不做 401/503 语义替代。
- 未注册：monitoring/quota（g2-merged 无路径）、certificates（候选节点本地 CSR/信任包与 KeyRef 模型冲突）、publishing（候选无路径）、gateway `/checks`（缺独立 serviceRunning 观测）。
- 契约差异：DNS 以 `credentialRef` 取代 `apiToken`（DS 永不接收原始令牌）；文件上传 HTTP 仅会话创建/完成，字节通道待 CX；gateway 路由创建需 `gatewayId` 扩展字段；任务审批需先取 `planDigest`；审计 eventId 为数字字符串。
- 上述若需并入主线，请总控按 OpenAPI/契约流程裁决；本包未改 API/Proto/gen。

## 7. g2r03 候选 provenance 重基线请求（非代码缺陷）

- 文件：`tests/contracts/g2r03/build-candidate.mjs` 的 `sources.node` 哈希（候选工件，未由本包修改）。
- 期望：`internal/nodes/node.go` sha256 `c61c768a2d2f6f5729219c8a459258b3f34ff83f14c578253c3abba2fd63eb78`。
- 实际：`b9f42e1c201493241de2f3daf055e2addaa80a999e0eb71f36376be448adfa62`。
- 原因：DS-02 需要持久化 CountryCode、CreatedAt/UpdatedAt 与受控事件序列 eventSeq；`Node` 是同包公开 DTO/领域模型，字段必须定义在 node.go。未改 `visibility.go`、`auth/node_policy.go`、API/Proto/gen。
- 请求：总控在接收本包时重算 g2r03 provenance（或明确该检查仅针对提案快照）。不要通过删除测试或手改生成物规避。
