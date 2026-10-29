# internal/nodes 交接说明（N01 / N01-R1 / N02）

范围：纯领域规则与测试适配。未接数据库、Agent、HTTP、真实注册/吊销或服务组调度。

## 不变量

- `Node.Usage(now, maxAge)` 是唯一展示指标入口：仅当节点 online、样本存在、CPU/内存均为有限值、采样时间有效且未过期（也不在未来）时返回 `ok=true`。合法 `0` 百分比返回可用；离线、缺失、NaN、正负 Inf 一律返回 `ok=false`。`Metrics` 是原始数据，不得直接展示。
- `Filter` 对名称/地址/标签做大小写不敏感匹配，非法状态拒绝；`Classify` 只读，空值跳过，同一节点重复标签只计一次，结果按值排序。
- 列表和计数必须先经 `Visible`；推荐使用 `VisibleFilter` / `VisibleClassify` 组合入口。`Visible` 逐节点授权并固定 `nodes.read`：**只有 `errors.Is(err, auth.ErrDenied)` 过滤该节点**；`ErrInvalidCheck`、`ErrPolicyUnavailable`、取消/期限及未知错误必须中止并返回 `nil` 结果，绝不返回部分列表或分类计数。取消/期限包括发生在最后一次 `Authorize` 内、`Authorize` 返回 nil 但 ctx 已取消，以及被包装的 ctx 错误。
- 主体不做 trim：空白返回 `ErrMissingSubject`，带首尾空白返回 `ErrInvalidSubject`，不会把不同主体静默映射为已授权身份。
- `OrdinaryGroup` 与 `ServiceGroup` 是独立类型；普通分组不参与网关调度、证书或 AI 授权。成员替换与普通分组改名均要求 `expectedRevision`，在同一锁内校验；过期写入返回 `ErrRevisionConflict` 且不改变状态；相同集合/相同值视为 no-op，不推进 revision。
- `testkit.MemoryReader` 仅供测试，禁止接入生产组合根。

## 调用者兼容变化（N02）

- 授权后端或策略读取失败过去可能产生“成功空列表/部分列表”；现在返回错误且结果为 `nil`。调用方必须传播错误，不能把错误结果缓存或展示成“无可见节点”。
- 主体不再自动 trim；依赖旧行为的调用方必须先由可信上游规范化身份，不得直接把客户端值作为 SubjectID。
- `tests/testkit.ErrDenied` 保留原有名称与类型，同时通过 `errors.Is` 匹配 `auth.ErrDenied`；测试替身返回该 sentinel 时会被当作明确拒绝过滤。
- 真实 `auth.NodePolicyAuthorizer` 已参与组合测试；但真实身份绑定、可信策略存储、HTTP/持久化和生产组合根接线仍未实现。

## 未完成 / 后续

- `Visible` 每个节点调用一次 `Authorize`，策略可能在节点之间变化；批量授权与性能评估留待 G2 接口冻结。
- 若上游把 `auth.ErrDenied` 与其他致命分类通过 `errors.Join` 合并，当前 `errors.Is` 语义会按拒绝过滤；真实 `NodePolicyAuthorizer` 不产生该形态，组合根必须保证错误分类单一，后续需在 auth 契约中显式约束。
- 服务组调度、网关、证书、持久化不在本模块范围；百分比仅校验有限性，0–100 域约束待明确指标定义后再加。
- 未接 HTTP、真实审计写入或权限缓存；调用方必须自行处理非 `ErrDenied` 的授权错误。

## 验证

`go test -count=1 -cover ./internal/nodes/... ./tests/testkit/...`、`npm run check:go`、`npm run check:policy`、`npm run check:security`。
