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

## 检查点 C2 · DS-02 节点/分组/服务组

- SHA：`b858e0d`。迁移 `ds/0020_nodes`、`ds/0021_servicegroups`。
- 实现：节点库存（JSON tags、原始指标+采样来源）、普通分组 revision CAS、服务组独立资源/成员/暂停调度/派发预览；所有写经注入 Authorizer 并在同事务写审计，纯拒绝落审计；审计失败回滚；读先 `PrepareNodeReadScope` 再读库存/过滤/计数，空库不绕过。
- 测试：`go test ./internal/nodes/ ./internal/servicegroups/` → ok。覆盖重启保持、seq 幂等/陈旧拒绝、revoked 终态、离线不补零、分组 revision 冲突、未知成员无部分成功、拒绝审计、审计失败回滚、可见性过滤、派发准入（online+healthy+!paused+gateway）、删除守卫。
- 未覆盖：真实 CX Agent 事件来源（用受控 `ApplyAgentUpdate` 入口，运输层未接）；服务组 gatewayIds/healthyNodeIds 富化由 composition 提供。

## 检查点 C3 · DS-04 审计持久化

- SHA：`b7fc5a2`。迁移 `ds/0010_audit`。
- 实现：append-only 表 + UPDATE 触发器阻止篡改；`AppendTx` 与业务同事务；TaskID/NodeIDs/Source/ReasonCode 结构化字段；保留期 `PurgeBefore` 需 ≥1h 陈旧、先写保留事件；分页检索按 subject/action/result/request/task/source。
- 测试：`go test ./internal/audit/` → ok。覆盖重启、过滤/分页、回滚、触发器、20 并发写、保留期、非法事件零写入。

## 检查点 C4 · DS-03 统一任务调度

- SHA：`619eb24`；CX 适配 `e6e2aa8`。迁移 `ds/0030_tasks`，新增 `ds/0031_tasks_cx_fields`（0030 未改写）。
- 实现：持久提交/目标快照/幂等键、审批 revision+计划摘要绑定、1→5 金丝雀/默认 5 分批、离线 15m TTL、批次熔断、取消、SSE 游标、去敏日志、崩溃恢复（在途→unknown 不重跑）、每次派发复检授权/审批；仅经注入 `Dispatcher` 端口。
- CX 适配：`CanonicalParams`、`Source/PluginID`、`ScopeRevision`、外部 `ApprovalConsumer`（同一 `storage.Tx` 原子消费+审计）、`ReadGate` 逐次读取/每轮 SSE 复检。
- 测试：`go test ./internal/tasks/` → ok。覆盖幂等、金丝雀、审批失效/过期、撤权、取消、unknown 不重试、TTL、批次阻塞、崩溃恢复、并发 RunOnce 单次派发、重复结果、日志去敏、外部审批回滚。
- 未覆盖：AI scopeRevision 的执行期复检需 CX 授权范围端口；真实 transport/Agent 端到端未接。

## 检查点 C5 · DS-05 文件

- SHA：`628d280`。迁移 `ds/0040_files`。
- 实现：POSIX 远端路径校验（穿越/重复分隔符/控制字符/反斜杠/尾斜杠/符号链接拒绝）、源读+目标写+transfer 分别授权、dry run 零写入、幂等键、任务失败/未提交恢复、完成态与项目结果一致性降级、断点上传（精确 offset、分块/总量限制、SHA-256、传输失败可重试）、下载/上传仅经注入 transport 端口。
- 测试：`go test ./internal/files/` → ok。覆盖路径矩阵、双端拒绝审计、dry run、提交失败、取消、恢复、校验不符、重试、下载拒绝、HTTP 幂等/上传会话。

## 检查点 C6 · DS-07 DNS/证书

- SHA：`276d9d0`。迁移 `ds/0060_dns`、`ds/0061_certificates`。
- 实现：Cloudflare 账号仅保存 secret-store `credential_ref`+状态，Provider 端口注入；Zone/记录 CRUD/冲突/单默认原子切换；内部 PKI 与 ACME 分表，私钥仅 `KeyRef`、PEM 输入拒绝；分发记录绑定服务组 revision 并经统一任务提交；续签/轮换/吊销走 Issuer 端口；私钥不返回/不入审计/不入 JSON。
- 测试：`go test ./internal/dns/ ./internal/certificates/` → ok；架构边界通过。覆盖重启、拒绝审计、审计失败回滚、空结果 fail-closed、revision 冲突、密钥材料扫描。

## 检查点 C7 · DS-09 模块 HTTP/备份/POC

- SHA：`f132735`；`e6e2aa8` 增加任务读闸门。无新增迁移。
- 实现：nodes/tasks/audit/files/servicegroups 的 stdlib HTTP 注册，可信 `Principal` 注入、固定错误映射、严格 JSON（拒未知字段）、分页/游标、任务 SSE（Last-Event-ID + 每轮复检）、审计只读 API（无修改/保留路由）；跨五域快照备份/恢复/损坏拒绝；POC-07 10/30/100 代理吞吐采集。
- 实测（Windows dev，非 1C1G）：10 代理 9370.9 writes/s；30 代理 5680.9；100 代理 5298.8；DB 980KB、WAL checkpoint 后 0KB、heap ~2.6MB、SSE 3 事件 0ms。报告写入忽略目录 `reports/_review/v09-services/poc07-report.json`。`go test ./tests/integration/v09/services/ ./tests/poc/v09/services/` → ok。
- 未覆盖：1C1G/Linux 验收（BLOCKED）；DNS/证书/监控/通知/配额/网关/发布的 HTTP 目前只有服务层端口（DS-06/DS-08 完成后补或列缺口）；Controller 入口接线属 CX。

## 当前未完成/阻塞

- DS-06 监控/通知/配额、DS-08 网关/发布：子代理实现中。
- 所有真实外部环境（Linux 双架构、Cloudflare/ACME、QQ/SMTP、Nginx）UNTESTED/BLOCKED；不得以本地假端口计为验收。
- `go.mod/go.sum`、迁移命名、HTTP 契约仍未获总控接收；按候选处理。

## 检查点 C8 · DS-06 监控/通知/配额

- SHA：`247862d`（通知域）+ `a3eacba`（监控/配额）。迁移 `ds/0050_monitoring`、`ds/0051_notifications`、`ds/0052_quota`。
- 实现：HTTP/TCP/TLS 监测、SSRF 目标策略与逃逸形式拒绝、维护窗、上下线迁移告警、注入 Prober（nil fail-closed）；SMTP/QQ 仅存 secretRef、去重窗口、QQ→SMTP 备用一次、显式重试上限/退避、未知结果不重试；流量周期/权威修正、缺失≠0、估算门控、提醒去重、超额仅告警不断网。通知 HTTP 注册（读取需注入 gate，响应不含 secretRef）。
- 测试：`go test ./internal/monitoring/ ./internal/notifications/ ./internal/quota/` → ok；staticcheck 通过。
- 未覆盖/风险：监控先持久状态后发通知，告警投递失败不会下轮自动重发；SSRF 不做 DNS 解析，需真实 Prober 复核；真实 SMTP/QQ/网络目标 UNTESTED。
