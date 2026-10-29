# TASK-01C 顺序批次进度纯值

范围：在已验收的 `BatchPlan` / `EvaluateBatch` 之上，新增不可变、可复制的顺序批次进度值。没有调度器、执行、审批、持久化、网络、队列、锁或恢复服务；未改动现有分批/等待规则、测试、`HANDOFF.md` 或 `ports.go`。

## 入口与状态

- `StartBatchProgress(plan)` 只接受非空计划，返回首批（索引 0）的 `waiting` 值；零计划拒绝。
- `ApplyBatch(results)` 只评价当前批，复用 `BatchPlan.EvaluateBatch`，返回新进度和 `BatchOutcome`：
  - `incomplete`：结果缺失、`running`、`unknown` 或未全部成功，进度原地等待，同一批可再次提交结果；
  - `advance`：只移动到紧邻下一批，索引加一；
  - `complete`：末批全部成功，进度终结；
  - `blocked`：任一 `failed`/`canceled`/`timed_out`，进度终结，后续批次不再推进。
- 非法结果（重复、批外、未知代码）返回 `EvaluateBatch` 的错误，旧进度不变；终结值再次 `ApplyBatch` 返回 `ErrProgressTerminal`，不能重开或跳批。
- `State`、`CurrentBatchIndex`、`CurrentBatchTargets`、`BatchCount`、`Valid`、`Terminal` 暴露明确状态；目标返回副本，输入结果不被修改。

## 纯值边界与重放限制

- 该类型是**可复制纯值**，不能被当作一次性票据、执行许可或持久记录。复制一个 `waiting` 旧值并再次提交相同结果，会重新得到同一推进结果；本模块没有 execution ID、持久 authoritative state、防回滚、防重放、并发互斥、崩溃恢复或锁。
- 调用方必须由可信任务服务保存唯一权威进度，将结果绑定到当前执行/批次，并在真实派发前继续完成当前授权、审批、离线 TTL 和可靠结果证据校验；不能用本值替代这些门禁。
- `advance`/`complete` 只是本地顺序结论，不发送、不重试、不取消、不回滚任何任务；`unknown` 不会被自动重跑。
- `StartBatchProgress` 复制计划批次；目标返回副本，输入结果不被保留。进度值按值复制，底层计划无公开修改入口；但纯值复制本身无法阻止旧副本被再次使用。需要防重放时必须在任务服务/执行 ID/持久状态层实现。

## 兼容与验证

- 只新增 `batch_progress.go`、对应测试和本说明；未修改 `batch_plan.go`、`batch_gate.go`、现有测试、`HANDOFF.md`、`ports.go`、依赖或其他模块。
- 命令：`go test -count=1 -cover ./internal/tasks/...`、`npm run check:go`、`npm run check:policy`、`npm run check:security`。
