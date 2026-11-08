# ServerFleet

ServerFleet 是面向自托管环境的服务器集群管理项目，由 Go Controller、独立 Agent 和 React + TypeScript Web 管理台组成。Controller 统一管理节点、权限、任务、审批与审计；Agent 主动通过 WSS + mTLS 接入，Web 仅调用 Controller 的 REST/SSE。

项目目前处于 **v0.9 功能集成与发布验收阶段**。G0 产品与 G1 架构已确认；G2、完整产品验收和发布准入尚未关闭。以下“已接收”指已审查的实现及其验证范围，不等于全部功能已达到生产发布条件。

## 已核验的产品基线

截至2026-11-08（Asia/Shanghai），最近已核验的main基线为[f1d715b](https://github.com/IKUNHEIHIE/ServerFleet/commit/f1d715b532f063cc9c7e21264eaa0f7ea4d4c73a)，[独立main CI42458157478](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/42458157478)成功。#7/#8/#9已通过协调#12返修接收；#13补齐Controller暂存崩溃恢复及四份正式ADR原文。#13精确候选CI为Go971顶层PASS/7SKIP/0FAIL、Web380PASS，安全与构建通过；合并树与候选一致。正式迁移28项，原25项旧库fixture及历史SQL/checksum保持。

本分支继续V09-CLOUD-BROWSER-RESUME-01：正式文件页保存账号/节点隔离的待核验记录，用户重新选择原文件并核对完整摘要，再从Controller确认的偏移按1MiB分块继续。R10通过显式resumeBinding=true读取绑定，默认保留R8响应；已派发/终态/未知任务不重复提交。实现、接口和产品差异同步，最终验收以精确head的PR/CI记录为准，不提前当作完整发布认证。见[本轮记录](docs/development/sprints/v09-five-day/progress/chatgpt/cloud-r4/BROWSER-RESUME.md)和[R10差异](docs/contracts/revisions/v09-r10-file-resume.md)。

## 当前能力与剩余边界

| 领域 | 已接收的实现或检查点 | 仍待完成/验收 |
|---|---|---|
| 核心平台 | Controller/Agent、SQLite WAL、REST/SSE、WSS+mTLS、统一授权/任务/审批/审计；节点及管理台等业务模块已有实现 | 完整跨模块产品验收、正式规则实施核对与 G2 收口 |
| 文件执行 | 双 Agent 多文件及 skip/overwrite/rename；用户上传统一任务/审批/当前权限复核/取消/终态对账/真实摘要；冻结多目标分发检查点已接收 | Controller暂存恢复已接收；本分支补Web重选文件续传，真实浏览器/进程重启组合及Agent执行续传仍待完整验收 |
| 更新 | 实际 Agent 组件本地签名更新、健康检查和失败回滚，已有真实浏览器专项证据 | Controller 组件升级、可信在线发行与可部署装配；B05 仍开放 |
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
