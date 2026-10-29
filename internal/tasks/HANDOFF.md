# internal/tasks 交接说明（TASK-01A）

范围：目标快照与未启动离线等待窗口的纯领域规则。未实现 Submitter、完整 TaskState、审批/计划摘要、调度/并发/队列、持久化、HTTP、Agent 下发或执行。

## 不变量

- `TargetSnapshot` 来自调用方明确提供的精确 node ID 集合：空/nil 集合、空 ID、首尾空白、内部空白、控制/格式/行分隔字符、通配符、非法 UTF-8 和重复 ID 均拒绝，不做 trim；保持提交顺序。`IDs()` 返回副本，输入与返回 slice 都不与快照共享可变存储；后续成员增加不会进入旧快照。零值等于空快照，绝不能解释为“全部节点”。
- 快照只冻结目标，不证明节点存在、用户有权、AI 会话授权或审批通过。真实派发前必须按当时权限、成员和审批重新校验。
- `WaitWindow` 使用调用方显式提供的时间与正时长；默认构造为项目已批准的 15 分钟等待窗口（局部默认，不是冻结公开合同）。零创建时间、零/负时长和零窗口均拒绝，不能变成无限等待。
- `Evaluate(now, state)` 需要显式 `now`。零/无效窗口对任何状态都拒绝；有效窗口下，`StartStateUnstarted` 的 `now < deadline` 返回 `pending`，`now >= deadline` 返回 `expired`（恰好截止即过期），`StartStateStarted` 返回 `not_applicable`，绝不改成 `expired` 或重新入队。未知状态和零 `now` 拒绝。
- 本模块不判断节点是否离线，也不读取或锁定任务阶段；调用方必须仅在已确认“未启动且离线”的场景应用等待窗口，并用一致的阶段快照调用，避免与状态变更竞态。
- `pending` 只表示未过期，不是执行许可；结果 `unknown` 不在本模块推断，不提供自动重试、重跑或补派发。

## 兼容与边界

- 现有 `ports.go` 与 `Request` 未变；本任务只新增 `TargetSnapshot`、`WaitWindow` 及局部错误/状态类型。
- 15 分钟是已批准的默认行为；`NewWaitWindow` 允许注入其他正时长用于测试和未来明确策略，但本模块没有生产策略系统、配置热更新或隐藏时钟读取。
- 未实现完整任务状态机；已启动任务的状态由未来调度/执行层管理，本模块只回答“等待窗口是否适用/过期”。

## 未完成 / 风险

- 无 Submitter、调度器、持久化、HTTP、Agent 下发或执行；无身份、RBAC、AI 会话授权和审批。
- 快照不校验节点存在或当前授权，权限撤销必须由真实派发链路每次复核；快照中的 ID 可能已失效。
- 过期判定是本地纯规则，`expired` 不触发任何实际停止/删除/重试；没有跨进程/重启持久化保证。
- 未做并发或性能验证；测试全部使用合成固定时间，不连接真实节点/Controller/数据库。

## 验证

`go test -count=1 -cover ./internal/tasks/...`、`npm run check:go`、`npm run check:policy`、`npm run check:security`。
