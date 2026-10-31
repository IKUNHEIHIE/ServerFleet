# ServerFleet

ServerFleet 是面向自托管环境的服务器集群管理项目，提供统一主控、节点 Agent 和 Web 管理界面，帮助运维人员集中管理服务器与日常运维任务。

项目由 Go 编写的模块化 Controller 与独立 Agent、基于 React 和 TypeScript 的管理台组成。Controller 提供 HTTP API 和持久化服务；Agent 通过受保护的连接与 Controller 通信，执行经授权的任务。平台还包含节点、任务、审计、监控、文件传输、自动化工作流、发布与恢复等管理能力。

本仓库是 ServerFleet v0.9 开发快照。核心 G2 契约仍处于评审阶段，功能实现和测试结果不代表已完成生产部署认证。
