# ServerFleet

ServerFleet 是面向自托管环境的服务器集群管理项目，由 Go Controller、独立 Agent 和 React + TypeScript Web 管理台组成。Controller 统一管理节点、权限、任务、审批与审计；Agent 主动通过 WSS + mTLS 接入，Web 仅调用 Controller 的 REST/SSE。

项目目前处于 **v0.9 功能集成与发布验收阶段**。G0 产品与 G1 架构已确认；G2、完整产品验收和发布准入尚未关闭。以下“已接收”指已审查的实现及其验证范围，不等于全部功能已达到生产发布条件。

## 当前集中编码包（未验收）

2026-11-09 正在 `feature/cloud-v09-code-completion-20261109` 连续补齐代码，起点为已接收 #16/main `9738648`。新增发布、PKI/恢复/备份、可信发行获取、节点独立 CSR/证书、公网签发和统一 DNS 执行/补偿等实现目前 **UNTESTED**；预计迁移 38 项只表示分支代码登记。按用户要求完成整包代码后统一验收，不用下方历史 CI 证明本分支。实际剩余项见[编码进度](docs/development/sprints/v09-five-day/progress/chatgpt/cloud-r7/CODE-COMPLETION.md)与[正式界面变化](docs/development/sprints/v09-five-day/progress/chatgpt/cloud-r7/PRODUCT-DELTA.md)。G2、B03—B06 与发布准入继续开放。

## 已核验的产品基线

截至2026-11-08（Asia/Shanghai），已接收main为[6031c1f](https://github.com/IKUNHEIHIE/ServerFleet/commit/6031c1f9d9b2b840ef5a477520aa937799bb3ba5)。#15精确候选[CI42485042788](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/42485042788) foundation/dependencies均success，Go983顶层/1301子项PASS、7SKIP、0FAIL，Web422PASS；已按精确head审查接收，独立main CI42486235557在记录时运行中，最终状态见GitHub。正式29项迁移，原25fixture及历史SQL/hash保持。

#12已接收#7/#8/#9及返修，#13暂存恢复/四ADR来源，#14正式Web重选文件与持久暂存续传，#15用户上传三策略/真实路径回执/原子checksum拒绝/安全cleanup后台重试/可信本地Agent更新启动入口均已合入；不再列为尚未实现。来源与证据见[装配回执](docs/development/sprints/v09-five-day/progress/chatgpt/cloud-r5/PRODUCT-ASSEMBLY.md)。

用户负责手动验证，ChatGPT单会话优先继续实现。本分支补正式Controller同源控制台（-web-dir）及仓库外双架构核心候选构建；只读有限构建资产，保留API和实时隔离门；真实四个程序/正式Web构建及版本/清单/源码一致性已核对，但仍为未签名/未认证候选。详见[启动/构建说明](docs/development/PRODUCT-STARTUP.md)与[本轮记录](docs/development/sprints/v09-five-day/progress/chatgpt/cloud-r6/DEPLOY-ASSEMBLY.md)。本部署增量待精确head原生CI与PR接收，不提前称main已提供。

## 当前能力与剩余边界

| 领域 | 已接收的实现或检查点 | 仍待完成/验收 |
|---|---|---|
| 核心平台 | Controller/Agent、SQLite WAL、REST/SSE、WSS+mTLS、统一授权/任务/审批/审计；节点及管理台等业务模块已有实现 | 完整跨模块产品验收、正式规则实施核对与 G2 收口 |
| 文件执行 | 双 Agent 多文件及 skip/overwrite/rename；用户上传统一任务/审批/当前权限复核/取消/终态对账/真实摘要；冻结多目标分发检查点已接收 | Controller暂存恢复/Web重选文件续传与用户上传三策略/真实回执已接收，真实浏览器/进程重启组合及Agent执行续传仍待完整验收 |
| 更新 | 实际 Agent 组件本地签名更新、健康检查和失败回滚，已有真实浏览器专项证据 | 可信本地Agent更新启动配置已接收；在线发行/完整批次与Controller更新需求差异继续跟踪，B05仍开放 |
| 网关与发布 | 工件传输适配、独立子执行、冻结计划/逐帧授权、持久绑定、终态 CAS 与并发负向 | Standalone路由checker、完整实际安装/发布/补偿及ACME组合；B04仍开放（已有隔离Agent-WSS-Nginx/HTTP字节与补偿证据） |
| 恢复 | 隔离基础、恢复运行门与异常任务对账，未知副作用不自动重放 | 实际灾备恢复、新权威建立、旧主控隔离/fence；B06 仍开放 |
| Xray 可选插件 | 已有插件代码、候选与部分隔离证据 | 安全工件与 Runtime 准入、完整产品验收；B03 仍开放 |

AI 运维、工作流、服务组、DNS/证书、监控/通知和配额等模块的已有实现与测试，不替代真实外部供应商及完整发布验收。物理双机、ARM 实际运行、硬限制 1 vCPU/1 GB RAM、正式规则实施差异与 ENV-W6-01 继续跟踪。

最近已接收的完整严格正向结果仍是历史 **31 PASS / 4 BLOCKED / 0 FAIL，退出码 1**，本轮没有把它改写为全量通过。详细证据与范围见[云端首轮接收报告](docs/development/reviews/V09-CLOUD-R1-REVIEW.md)及[机器结果](docs/development/reviews/V09-CLOUD-R1-RESULTS.json)。

## 当前开发方式与历史分工

Windows本地开发已停止，GitHub仓库保持私有。2026-11-08起由ChatGPT单会话接手实现、验证和集成，不再等待或启动三平台Agent。改动仍通过PR与检查接收；实现者自查不冒称另一个独立审核身份，不自我APPROVE。下表保留原分工追溯，尚未完成项已转交本次接手任务。

| 连续任务包 | 平台 / 唯一会话 | 范围 |
|---|---|---|
| V09-CLOUD-AM-02 | 智谱 AgentMore / agentmore-02 | 文件多目标/续传、更新、恢复和平台可靠性；不承担插件/代理组件 |
| V09-CLOUD-HW-02 | 华为云 CodeArts / codearts-02 | 网关正式装配、发布/补偿整链、ACME/cleanup 与服务域 |
| V09-CLOUD-WB-02 | 腾讯云 WorkBuddy / workbuddy-02 | **Xray 全部工作**、正式 Web、浏览器验收与证据矩阵 |

AM-02的#11检查点已合入；HW-02/WB-02已经提交#7/#8/#9并经复审，不能再称尚未派发。三包剩余转V09-CLOUD-TAKEOVER-01；这是所有权转交，不是整包完成或Runtime准入。

- [当前进度与未完成项](docs/development/sprints/v09-five-day/CURRENT-PROGRESS.md)
- [任务登记板](docs/development/TASK-BOARD.md)
- [Cloud R2 连续任务与共享接线计划](docs/development/sprints/v09-five-day/cloud-r2/PLAN.md)
- 完整派发词：[AgentMore](docs/development/sprints/v09-five-day/cloud-r2/AGENTMORE-02.md)、[CodeArts](docs/development/sprints/v09-five-day/cloud-r2/CODEARTS-02.md)、[WorkBuddy](docs/development/sprints/v09-five-day/cloud-r2/WORKBUDDY-02.md)
- 历史追溯：[Wave6-R3 审查](docs/development/reviews/V09-WAVE6-R3-REVIEW.md)、[GitHub 上传记录](docs/development/reviews/V09-GITHUB-UPLOAD.md)

## 开发与验证

先阅读 [AGENTS.md](AGENTS.md)、[架构与原始资料索引](docs/architecture/README.md)、[模块归属](docs/development/MODULE-OWNERSHIP.md)和[依赖规则](docs/development/DEPENDENCY-RULES.md)。固定 Go 1.27.3、Node 24.22.1、npm 11，具体工具与版本核对见[工具说明](docs/development/TOOLCHAIN.md)。

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

真实执行专项须满足各自的普通用户、私有目录、实际构建和网络前提；条件不足如实记录 BLOCKED/SKIP，不降低版本、权限或测试门槛。Windows 的工具缓存、工作目录、运行数据库与凭据不会随 Git 自动迁移。

开发授权不包含生产部署、awskr/其他 VPS、真实外部供应商账号或云 API。所有执行继续经过 Controller 的统一安全边界，未知副作用不自动重放；可选插件不编入核心 Release。
