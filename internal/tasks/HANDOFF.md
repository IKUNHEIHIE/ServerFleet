# internal/tasks 交接说明（TASK-01A / TASK-01B）

范围：目标快照、未启动离线等待窗口，以及默认分批计划与后批推进的纯领域规则。未实现 Submitter、完整 TaskState、审批/计划摘要、调度/并发/队列、持久化、HTTP、Agent 下发或执行。

## 不变量

- `TargetSnapshot` 来自调用方明确提供的精确 node ID 集合：空/nil 集合、空 ID、首尾空白、内部空白、控制/格式/行分隔字符、通配符、非法 UTF-8 和重复 ID 均拒绝，不做 trim；保持提交顺序。`IDs()` 返回副本，输入与返回 slice 都不与快照共享可变存储；后续成员增加不会进入旧快照。零值等于空快照，绝不能解释为“全部节点”。
- 快照只冻结目标，不证明节点存在、用户有权、AI 会话授权或审批通过。真实派发前必须按当时权限、成员和审批重新校验。
- `WaitWindow` 使用调用方显式提供的时间与正时长；默认构造为项目已批准的 15 分钟等待窗口（局部默认，不是冻结公开合同）。零创建时间、零/负时长和零窗口均拒绝，不能变成无限等待。
- `Evaluate(now, state)` 需要显式 `now`。零/无效窗口对任何状态都拒绝；有效窗口下，`StartStateUnstarted` 的 `now < deadline` 返回 `pending`，`now >= deadline` 返回 `expired`（恰好截止即过期），`StartStateStarted` 返回 `not_applicable`，绝不改成 `expired` 或重新入队。未知状态和零 `now` 拒绝。
- 本模块不判断节点是否离线，也不读取或锁定任务阶段；调用方必须仅在已确认“未启动且离线”的场景应用等待窗口，并用一致的阶段快照调用，避免与状态变更竞态。
- `pending` 只表示未过期，不是执行许可；结果 `unknown` 不在本模块推断，不提供自动重试、重跑或补派发。

## 分批计划与后批门槛（TASK-01B）

- `OperationClass` 必须是未来可信动作注册/任务服务提供的分类；仅支持 `standard`、`root`、`modifying`，零值和未知分类拒绝。AI/客户端自报不能把 root/修改类降成普通类。
- `NewDefaultBatchPlan` 复用 `TargetSnapshot`：空快照拒绝，普通任务每批最多 5 个目标，root/修改类首批 1 个、后续每批最多 5 个；保持快照顺序，不丢失或重复目标。`Batches()` 返回深拷贝，修改返回批次不能改变计划。
- 计划只决定批次形状，不证明节点存在、资源权限、动作安全、人工审批或实际并发已落实。
- `EvaluateBatch` 只对明确当前批做纯结果判断：所有目标均为 `succeeded` 且还有后批时返回 `advance`；最后一批全部成功返回 `complete`。任一 `failed`/`canceled`/`timed_out` 返回 `blocked`，后批不得启动；缺失结果或存在 `running`/`unknown`/未全部成功时返回 `incomplete`，不推进也不自动重试。重复、批外结果、未知结果代码和非法批索引返回错误。
- 仅 `succeeded` 是成功证据；ACK、exitCode=0 或输出截断不在本类型中，也不能推断成功。已在运行的本批任务不由此规则回滚或取消，调度层必须按批顺序执行且只在 `advance` 后考虑下一批。

## 兼容与边界

- 现有 `ports.go` 与 `Request` 未变；本模块只新增 `TargetSnapshot`、`WaitWindow`、`BatchPlan`、结果分类及局部错误/状态类型。
- 15 分钟和 5/1→5 都是已批准的默认行为；`NewWaitWindow` 允许注入其他正时长用于测试和未来明确策略，但本模块没有生产策略系统、配置热更新或隐藏时钟读取。
- 未实现完整任务状态机；已启动任务的状态由未来调度/执行层管理，本模块只回答“等待窗口是否适用/过期”及“当前批是否满足后批顺序门槛”。

## 未完成 / 风险

- 无 Submitter、调度器、持久化、HTTP、Agent 下发或执行；无身份、RBAC、AI 会话授权和审批。
- 快照不校验节点存在或当前授权，权限撤销必须由真实派发链路每次复核；快照中的 ID 可能已失效。
- 批次默认值（5、1→5）与分类均不是公开冻结合同；分类来源、结果证据、批次执行顺序和阶段一致性必须由未来可信任务服务保证，本模块没有结果存储或重试。
- `EvaluateBatch` 无状态：不保存前批结果，也不携带 execution ID 或结果新鲜度。调用方必须按序评估、只在 `advance` 后启动后批；跳过失败批或复用旧结果无法由本函数识别。
- 过期判定和批次门槛都是本地纯规则，`expired`/`blocked`/`incomplete` 不触发任何实际停止/删除/重试；没有跨进程/重启持久化保证。
- 未做并发或性能验证；测试全部使用合成固定时间，不连接真实节点/Controller/数据库。

## 验证

`go test -count=1 -cover ./internal/tasks/...`、`npm run check:go`、`npm run check:policy`、`npm run check:security`。
