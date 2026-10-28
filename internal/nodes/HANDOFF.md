# internal/nodes 交接说明（N01 / N01-R1）

范围：纯领域规则与测试适配。未接数据库、Agent、HTTP、真实注册/吊销或服务组调度。

## 不变量

- `Node.Usage(now, maxAge)` 是唯一展示指标入口：仅当节点 online、样本存在、CPU/内存均为有限值、采样时间有效且未过期（也不在未来）时返回 `ok=true`。合法 `0` 百分比返回可用；离线、缺失、NaN、正负 Inf 一律返回 `ok=false`。`Metrics` 是原始数据，不得直接展示。
- `Filter` 对名称/地址/标签做大小写不敏感匹配，非法状态拒绝；`Classify` 只读，空值跳过，同一节点重复标签只计一次，结果按值排序。
- 列表和计数必须先经 `Visible`；推荐使用 `VisibleFilter` / `VisibleClassify` 组合入口。`Visible` 逐节点授权并 fail-closed；非 ctx 的 authorizer 错误只过滤该节点，但取消/超时必须传播，包括发生在最后一次 `Authorize` 内、`Authorize` 返回 nil 但 ctx 已取消，以及被包装的 ctx 错误。
- `OrdinaryGroup` 与 `ServiceGroup` 是独立类型；普通分组不参与网关调度、证书或 AI 授权。成员替换与普通分组改名均要求 `expectedRevision`，在同一锁内校验；过期写入返回 `ErrRevisionConflict` 且不改变状态；相同集合/相同值视为 no-op，不推进 revision。
- `testkit.MemoryReader` 仅供测试，禁止接入生产组合根。

## 未完成 / 后续

- auth 拒绝与基础设施错误尚未分类（需 auth owner 设计）；当前非 ctx 的授权错误按“不可见”处理。
- `Visible` 每个节点调用一次 `Authorize`，批量授权与性能评估留待 G2 接口冻结。
- 服务组调度、网关、证书、持久化不在本模块范围；百分比仅校验有限性，0–100 域约束待明确指标定义后再加。

## 验证

`go test -count=1 -cover ./internal/nodes/...`、`npm run check:go`、`npm run check:policy`。
