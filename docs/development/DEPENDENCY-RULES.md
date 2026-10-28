# 依赖与接口规则

`dependencies.json` 是 Go 包导入白名单，与 MODULE-OWNERSHIP 的允许依赖一致。`go test ./tests/architecture` 解析所有 apps/controller、apps/agent、internal、gen、tests Go 文件；越界模块、未登记的本地目录、核心导入可选插件/测试包均失败。子目录继承所属模块权限；不根据同名前缀误放行。只有 storage 可导入 `database/sql`。Go 编译器检查实际包循环依赖。

| 调用方 | 允许的跨模块方向 |
|---|---|
| Controller 组合入口 | 各 internal 模块、gen |
| Agent | transport、gen |
| tasks | auth、nodes、audit、transport、storage |
| plugins 平台 | auth、tasks、audit、storage |
| nodes | auth、audit、storage |
| auth | audit、storage |
| transport | audit、storage、gen |
| audit | storage |
| storage / gen | 无领域反向依赖 |

传输层收到事件时，通过上层组合注入的回调/适配端口交给 tasks/nodes；不得反向 import 业务包造成环。跨模块只能调用 owner 审查的端口，禁止取得其数据库表/内部 map 或绕过服务直接执行。业务模型由领域拥有；storage 管连接/事务/迁移机制，公共 schema 和迁移编号需要集成审查，P1 未建立业务数据库。

Web 仅消费 Controller REST/SSE；API DTO 将来由契约适配层转换成 ViewModel。测试 fixture/mock 只能出现在 test 或 `*.test.*`；生产不得导入这些目录。`npm run check:policy` 包含前端 import 边界检查。TypeScript strict + Vite build 检查类型/模块可构建性。

检查能力有限：静态 import 检查不能证明运行时 RBAC、网络安全、SQL 正确性或插件沙箱隔离。真实安全验收仍需负向测试与审查；禁止把通过 import 检查当成已实现授权。

公共端口、外部契约或依赖图调整必须走 `contract/...` / `integration/...`，列出调用方、兼容性、迁移、测试与回退。实现者不能以修复循环依赖为由把所有类型搬进公共“大杂烩”包或取消规则。
