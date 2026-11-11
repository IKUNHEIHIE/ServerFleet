# 依赖与接口规则

2026-11-12 R34。`dependencies.json`是实际Go包导入白名单，`go test ./tests/architecture`验证以下apps/internal/gen/tests根及子目录；编译器另外检查循环依赖。下表从当前清单整理，历史文档中的26项迁移/空壳/旧依赖描述不代表当前实现。

| 调用方 | 允许的跨模块方向 |
|---|---|
| `apps/controller` | `internal/auth`、`internal/nodes`、`internal/tasks`、`internal/audit`、`internal/plugins`、`internal/transport`、`internal/storage`、`internal/ai`、`internal/workflows`、`internal/updates`、`internal/recovery`、`internal/files`、`internal/monitoring`、`internal/notifications`、`internal/quota`、`internal/dns`、`internal/certificates`、`internal/gateway`、`internal/servicegroups`、`internal/publishing`、`gen/go` |
| `apps/agent` | `internal/transport`、`gen/go`、`internal/netprobe` |
| `internal/auth` | `internal/audit`、`internal/storage` |
| `internal/nodes` | `internal/auth`、`internal/audit`、`internal/storage` |
| `internal/tasks` | `internal/auth`、`internal/nodes`、`internal/audit`、`internal/transport`、`internal/storage`、`internal/servicegroups` |
| `internal/audit` | `internal/storage` |
| `internal/plugins` | `internal/auth`、`internal/tasks`、`internal/audit`、`internal/storage` |
| `internal/transport` | `internal/audit`、`internal/storage`、`gen/go`、`internal/netprobe` |
| `internal/netprobe` | 无领域反向依赖 |
| `internal/storage` | 无领域反向依赖 |
| `internal/ai` | `internal/auth`、`internal/tasks`、`internal/nodes`、`internal/servicegroups`、`internal/audit`、`internal/storage` |
| `internal/workflows` | `internal/auth`、`internal/tasks`、`internal/nodes`、`internal/notifications`、`internal/audit`、`internal/storage` |
| `internal/updates` | `internal/transport`、`internal/auth`、`internal/tasks`、`internal/nodes`、`internal/audit`、`internal/storage` |
| `internal/recovery` | `internal/auth`、`internal/nodes`、`internal/tasks`、`internal/audit`、`internal/storage` |
| `internal/files` | `internal/auth`、`internal/nodes`、`internal/tasks`、`internal/audit`、`internal/storage` |
| `internal/monitoring` | `internal/auth`、`internal/nodes`、`internal/notifications`、`internal/audit`、`internal/storage`、`internal/netprobe` |
| `internal/notifications` | `internal/auth`、`internal/audit`、`internal/storage` |
| `internal/quota` | `internal/auth`、`internal/nodes`、`internal/notifications`、`internal/audit`、`internal/storage` |
| `internal/dns` | `internal/auth`、`internal/tasks`、`internal/audit`、`internal/storage` |
| `internal/certificates` | `internal/auth`、`internal/nodes`、`internal/tasks`、`internal/dns`、`internal/audit`、`internal/storage` |
| `internal/gateway` | `internal/auth`、`internal/nodes`、`internal/tasks`、`internal/servicegroups`、`internal/certificates`、`internal/audit`、`internal/storage` |
| `internal/servicegroups` | `internal/auth`、`internal/nodes`、`internal/audit`、`internal/storage` |
| `internal/publishing` | `internal/auth`、`internal/tasks`、`internal/nodes`、`internal/servicegroups`、`internal/gateway`、`internal/dns`、`internal/certificates`、`internal/audit`、`internal/storage` |
| `gen/go` | 无领域反向依赖 |
| `tests` | `apps/controller`、`apps/agent`、`internal/auth`、`internal/nodes`、`internal/tasks`、`internal/audit`、`internal/plugins`、`internal/transport`、`internal/storage`、`internal/ai`、`internal/workflows`、`internal/updates`、`internal/recovery`、`internal/files`、`internal/monitoring`、`internal/notifications`、`internal/quota`、`internal/dns`、`internal/certificates`、`internal/gateway`、`internal/servicegroups`、`internal/publishing`、`gen/go` |

## 不可变边界

- 只有storage持有database/sql；领域拥有模型/schema，Controller只负责经审查的组合端口。Controller跨域查询/任务/身份适配在同包composition_*文件，拆文件不扩张权限。
- Agent允许transport/gen及纯标准库netprobe，不导入Controller业务/DB；Web仅Controller REST/SSE。核心不能导入plugins/xray-manager、test/mock/fixture或参考资料。
- transport通过上层注入回调交给任务/节点等业务；updates→transport只消费公开typed监督交接。netprobe没有业务或数据库依赖，不是插件运行时。
- 子目录继承所属模块权限，不按相似前缀或`internal/*`通配放行。静态导入不证明运行时授权、SSRF、任务/凭据安全或沙箱隔离。
- 当前正式49迁移，旧SQL/checksum不可改；追加需独立审查。公共端口、外部契约或依赖调整登记contract/integration差异，不能为消循环搬入通用大包或取消规则。

## 维护

修改方向时同时审查调用方/数据归属/鉴权与兼容；更新JSON和本文，再跑架构负向、受影响包及完整检查。R34没有改变允许边或导入业务到Agent。接口生成/索引由固定工具生成，原始候选和生成文件不可手改；真实安全验收仍需对应负向和实机证据。
