# ServerFleet

ServerFleet 是面向自托管环境的服务器集群管理项目，提供统一主控、节点 Agent 和 Web 管理界面，帮助运维人员集中管理服务器与日常运维任务。

项目由 Go 编写的模块化 Controller 与独立 Agent、基于 React 和 TypeScript 的管理台组成。Controller 提供 HTTP API 和持久化服务；Agent 通过受保护的连接与 Controller 通信，执行经授权的任务。平台还包含节点、任务、审计、监控、文件传输、自动化工作流、发布与恢复等管理能力。

本仓库是 ServerFleet v0.9 开发快照。核心 G2 契约仍处于评审阶段，功能实现和测试结果不代表已完成生产部署认证。

## 当前进度

2026-11-02，Wave6-R3 三线增量已审查、返修并集成；下一轮三个任务均未开始。文件页面、双 Agent 文件传输和冲突策略、AI 预算及实际 Agent 本地签名更新已有隔离验证。完整 v0.9 仍有 Xray、完整发布、更新浏览器/部署、真实灾备等缺口，G2 与发布准入尚未关闭。

- [当前进度与未完成项](docs/development/sprints/v09-five-day/CURRENT-PROGRESS.md)
- [最新审查与验证结果](docs/development/reviews/V09-WAVE6-R3-REVIEW.md)
- [任务登记板](docs/development/TASK-BOARD.md)及[下一轮任务包（尚未开工）](docs/development/sprints/v09-five-day/WAVE6-R4.md)
- [GitHub 上传记录](docs/development/reviews/V09-GITHUB-UPLOAD.md)

## 开发与验证

先阅读 [AGENTS.md](AGENTS.md)、[架构与原始资料索引](docs/architecture/README.md)、[模块归属](docs/development/MODULE-OWNERSHIP.md)和[依赖规则](docs/development/DEPENDENCY-RULES.md)。固定工具及验证步骤见[工具说明](docs/development/TOOLCHAIN.md)。GitHub Actions 是否通过，以对应提交的实际运行记录为准。
