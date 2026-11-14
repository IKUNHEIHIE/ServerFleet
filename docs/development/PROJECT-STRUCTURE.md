# 项目结构与维护入口

2026-11-14 核对。先读 [HANDOFF](HANDOFF.md)、[CURRENT-STATE](CURRENT-STATE.md)。本表提供现行源码导航；下述 R34 拆分背景是历史结构来源，不是未执行任务。

> v0.9.1 / R37：新增 Agent 身份续期与安全切换、明确接入读取授权、只读升级预检、通用就绪和有界节点诊断。现行迁移共50项（旧49项不变）。行为与接口见 [身份运维说明](IDENTITY-OPERATIONS.md)，升级/反代见仓库 deployment/UPGRADE.md 与 deployment/REVERSE-PROXY.md；旧审计/CI记录仅说明其当时基线。


R34 / 2026-11-12。保持既有Go模块、REST路径、数据目录和发行命令兼容；按职责拆文件和建立文档入口，未把业务搬入通用包或改变依赖方向。

| 路径 | 职责 |
|---|---|
| `apps/controller` | CLI/启动/部署与监督进程入口 |
| `apps/controller/server/server.go` | Config/Server、New依赖装配、调度与生命周期、认证入口 |
| `apps/controller/server/composition_catalog.go` | 正式迁移集合、审计action和权限目录；顺序/历史SQL不变 |
| `apps/controller/server/composition_routes.go` | 总HTTP挂载、身份/AI/插件/Agent入口；URL和鉴权回调不变 |
| `apps/controller/server/composition_identity.go` | 身份SQL事务、授权与任务/审计/通知读门 |
| `apps/controller/server/composition_adapters.go` | 组快照、节点/服务组查询、统一任务和各领域适配器 |
| `apps/controller/server/*_execution.go`等领域文件 | 具体统一任务执行装配，不能绕过tasks/identity/运行权威 |
| `apps/agent` | 主动WSS、能力执行、typed回执、受控文件/命令/更新和本地账本 |
| `apps/web/src/features` | 按产品功能组织页面；`components/ui`只放复用展示组件 |
| `apps/web/src/lib/api` | REST客户端、当前DTO、候选兼容；不能直连Agent/数据库 |
| `internal/<domain>` | Go领域模型/服务/HTTP/持久化适配；边界由dependencies.json检查 |
| `internal/netprobe` | 纯标准库网络/SSRF共用代码，不含数据库/任务/权限权威 |
| `plugins/xray-manager` | 可选Xray业务组件；核心禁止导入，不随核心Release打包 |
| `api/openapi/revisions` | 可审的增量候选；原候选不能覆写 |
| `proto` / `gen/go` | 固定工具生成的传输契约；禁止手改生成文件 |
| `deployment` | 用户部署/一键安装/签名/配置模板 |
| `scripts` | 固定工具、构建、规则、生成和API文档索引工具 |
| `tests` | 契约/架构/集成/隔离验证；生产不能反向导入fixture/mock |
| `docs/product` / `docs/api` | 现行用户功能与API说明、生成的路由声明索引 |
| `docs/audits` | 审计发现、证据、整改/剩余风险，不能冒充第三方认证 |
| `docs/development` | 工作记录、操作专题、任务板和验证历史 |
| `docs/architecture` / `docs/reference` | 架构来源/替代关系、原始不可变资料及批准ADR |

## 文档层次

状态汇总以 CURRENT-STATE 为入口，产品行为在 docs/product/FEATURES.md，API 在 docs/api，开发阅读顺序在 HANDOFF。历史任务/候选/验收原件保留原路径和字节，按[文档分类](../README.md)理解，不作当前任务/权限来源。

`composition_*`拆分在同一package中进行，未改导出函数、调用顺序、权限语义和迁移内容。后续若需改变模块边界或公共端口，仍须单独登记兼容差异，不能借整理取消授权或任意删除导出API。

## 清理准则

R34曾删除无调用的内部前端辅助函数/常量及仅赋值不读取的私有字段；R40只整理文档，没有继续删除代码。兼容路由、公共端口、可选Xray、恢复工具、原始包、历史测试/报告保留；未装配不等于死代码。构建产物/缓存/运行数据库由.gitignore隔离，不因整理移动用户运行目录。

新增/移动HTTP处理器后用生成器更新API索引，按代码影响检查；纯文档遵循[集成清单](INTEGRATION-CHECKLIST.md)，不运行完整CI。不用“编译成功”替代权限或产品审计。
