# Server Fleet

轻量服务器群控系统：Go Controller、主动连接的独立 Agent、React/Vite 管理台。Controller 集中处理权限、任务、审批与审计；Agent 使用 WSS + mTLS；Web 只访问 Controller。

当前为 **v0.9 功能集成版本，完整产品尚未结项**。核心功能可以进入用户实机联调，Xray 可选插件执行环境、受限 Bearer 读取装配及发布验收仍有明确缺口。功能实现、运行前提与验收状态分别记录，CI 通过不替代实机验收。

## 从这里开始

| 目的 | 入口 |
|---|---|
| 看功能、操作流程与未完成项 | [现行功能说明](docs/product/FEATURES.md) |
| 安装、初始化与启动 | [部署说明](deployment/README.md)、[一键安装](deployment/INSTALL.md) |
| 接口、鉴权、状态与兼容规则 | [API 说明](docs/api/README.md)、[路由索引](docs/api/ROUTES.md) |
| 定位源码、目录和依赖边界 | [项目结构](docs/development/PROJECT-STRUCTURE.md) |
| 本次审计、整改与保留问题 | [R34 审计报告](docs/audits/2026-11-12.md) |
| 开发任务与历史验收 | [任务板](docs/development/TASK-BOARD.md)、[进度记录](docs/development/sprints/v09-five-day/CURRENT-PROGRESS.md) |

## 当前实现

节点接入与资源授权、服务组、批量任务与独立审批、文件传输和显式续传、网关及服务发布、DNS 和证书、AI 运维执行循环、监控及通知、流量配额、Controller/Agent 受监督更新、加密备份和异地上传、保留清理、灾备激活、UNKNOWN 人工处置均有源码与产品入口。具体运行条件和未完成边界以[功能矩阵](docs/product/FEATURES.md)为准。

最近已接收 PR #24 / main `8aa1ac0d7071e1ac4f9325ea37d2e1ae84616941`；精确候选 CI 43053508380：Go 1055 顶层 PASS / 11 SKIP / 0 FAIL，Web 435 PASS，安全扫描与 Linux 双架构构建通过。该记录仅描述 R33 基线，不替代后续提交的检查。本次 R34 接收结果在对应 PR 评论记录。

## 开发与检查

固定 Go **1.27.4**、Node 24.22.1、npm 11；详见[工具链](docs/development/TOOLCHAIN.md)。

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

接口声明索引由源码生成，新增或移动路由后执行：

```sh
node scripts/api-docs.mjs --write
```

完整检查包含索引漂移检查。真实 Nginx 集成用例要求环境安装 Nginx，缺失时如实失败；其他环境依赖及 SKIP 见测试报告。普通/root 执行、供应商和灾备测试须满足各自隔离与授权前提。

## 贡献和兼容

先读 [AGENTS.md](AGENTS.md)、[架构来源](docs/architecture/README.md)、[模块归属](docs/development/MODULE-OWNERSHIP.md)与[依赖规则](docs/development/DEPENDENCY-RULES.md)。现行代码由主控单会话协调，不启动历史分工中的其他 Agent。

所有执行走统一任务，UNKNOWN 不自动重放副作用。可选插件不进入核心 Release；WASM/Eino 未获运行时准入。数据库迁移只追加，不改历史 SQL/checksum；原始资料、旧候选和已批准 ADR 保留用于追溯。

[原始需求与 ADR](docs/architecture/README.md)、[历史开发与验收入口](docs/development/sprints/v09-five-day/CURRENT-PROGRESS.md)。历史时点的“未实现/未测试”须结合后来 PR 判断，不能当作当前功能清单。
