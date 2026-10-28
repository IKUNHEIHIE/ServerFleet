# P1-001 · 工程初始化选择记录

日期：2026-10-29。范围：已授权的 M0-P1 工程组织；不是修改 G1 的安全/运行时 ADR。

- 一个根 Go module `serverfleet.local/serverfleet`，两个程序入口，避免过早拆微服务或多模块。没有官方仓库地址，不虚构 GitHub module URL；以后重命名需显式迁移生成器配置。
- npm workspace 管理 Web 与根契约工具，一份锁文件，精确版本。Node 24、Go 1.27.3 在项目本地安装；CI 读取同一版本文件。Go 工具固定版本、本地生成，无远程生成服务。
- Controller 仅标准库存活端点；Agent 离线退出。暂不引入路由框架、SQLite 驱动、WASM、AI 框架或真实执行器，以免把未冻契约提前绑定进运行系统。
- 业务模块只保留最小内部端口；模型归属与 import 白名单由测试检查，Go 编译负责循环依赖检查。数据库连接、WAL 和性能测试留到后续授权任务。
- Redocly 使用 OpenAPI `spec` 规则加 operationId/ref/路径参数检查，避免把格式偏好冒充接口缺陷。自定义负向夹具证明检查有效。Buf 使用 STANDARD/FILE，兼容性基线仅保护候选演进，不表示 G2 已冻结。
- CI 使用固定 Action commit、只读权限、可重复依赖、缓存与失败日志。没有远程，线上执行待验证。

工具文档依据：[Vite](https://vite.dev/guide/)、[shadcn Vite](https://ui.shadcn.com/docs/installation/vite)、[Vitest](https://vitest.dev/config/)、[Buf](https://buf.build/docs/generate/tutorial/)、[Redocly](https://redocly.com/docs/cli/commands/lint)、[Go 下载](https://go.dev/dl/)。已通过 Context7 查当前配置，并以实际安装/测试结果为准。
