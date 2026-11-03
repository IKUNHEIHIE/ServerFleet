# ServerFleet

ServerFleet 是面向自托管环境的服务器集群管理项目，由 Go Controller、独立 Agent 和 React + TypeScript Web 管理台组成。Controller 统一管理节点、权限、任务、审批与审计；Agent 主动通过 WSS + mTLS 接入，Web 仅调用 Controller 的 REST/SSE。

项目目前处于 **v0.9 功能集成与发布验收阶段**。G0 产品与 G1 架构已确认；G2、完整产品验收和发布准入尚未关闭。以下“已接收”指已审查的实现及其验证范围，不等于全部功能已达到生产发布条件。

## 已核验的产品基线

截至 2026-11-03（Asia/Shanghai）：

- 产品基线：[51e0ee57e66493892eb5f5168b9e0690b53f3661](https://github.com/IKUNHEIHIE/ServerFleet/commit/51e0ee57e66493892eb5f5168b9e0690b53f3661)。
- 该基线的[独立 main CI](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/41615351426)已成功，foundation 与 dependencies 两个 job 均通过。
- Go：**902 个顶层测试 PASS、7 SKIP、0 FAIL**；Web：**348 PASS**。
- 策略、契约、生成一致性、锁文件检查、安全扫描及 Linux amd64/arm64 Agent 交叉构建通过；npm audit 0 漏洞、govulncheck 无已知漏洞。
- 正式 Controller 迁移 **26 项**。历史 SQL/checksum 保持不变，固定旧版数据库的升级与重复执行回归已通过。

上述结果绑定到该产品 SHA。后续仅文档提交不表示新增产品功能或重新完成全部验收；SKIP 保留，交叉构建不等于 ARM 实机运行，各套件结果不累加折算完成率。

## 当前能力与剩余边界

| 领域 | 已接收的实现或检查点 | 仍待完成/验收 |
|---|---|---|
| 核心平台 | Controller/Agent、SQLite WAL、REST/SSE、WSS+mTLS、统一授权/任务/审批/审计；节点及管理台等业务模块已有实现 | 完整跨模块产品验收、正式资料与 G2 收口 |
| 文件执行 | 双 Agent 多文件及 skip/overwrite/rename；用户上传统一任务/审批/当前权限复核/取消/终态对账/真实摘要 | 多目标分发、跨重启完整续传、异步上传任务 Web 体验 |
| 更新 | 实际 Agent 组件本地签名更新、健康检查和失败回滚，已有真实浏览器专项证据 | Controller 组件升级、可信在线发行与可部署装配；B05 仍开放 |
| 网关与发布 | 工件传输适配、独立子执行、冻结计划/逐帧授权、持久绑定、终态 CAS 与并发负向 | 中央装配、完整实际发布路径和逆序补偿；B04 仍开放 |
| 恢复 | 隔离基础、恢复运行门与异常任务对账，未知副作用不自动重放 | 实际灾备恢复、新权威建立、旧主控隔离/fence；B06 仍开放 |
| Xray 可选插件 | 已有插件代码、候选与部分隔离证据 | 安全工件与 Runtime 准入、完整产品验收；B03 仍开放 |

AI 运维、工作流、服务组、DNS/证书、监控/通知和配额等模块的已有实现与测试，不替代真实外部供应商及完整发布验收。物理双机、ARM 实际运行、硬限制 1 vCPU/1 GB RAM、正式资料缺项与 ENV-W6-01 继续跟踪。

最近已接收的完整严格正向结果仍是历史 **31 PASS / 4 BLOCKED / 0 FAIL，退出码 1**，本轮没有把它改写为全量通过。详细证据与范围见[云端首轮接收报告](docs/development/reviews/V09-CLOUD-R1-REVIEW.md)及[机器结果](docs/development/reviews/V09-CLOUD-R1-RESULTS.json)。

## 云端协作与下一轮

Windows 本地开发已交接到 GitHub 私有仓库；各平台使用独立环境、分支和测试材料。用户手动派发任务并回传 PR，ChatGPT 负责共享接口协调、独立审查和最终合并。

| 连续任务包 | 平台 / 唯一会话 | 范围 |
|---|---|---|
| V09-CLOUD-AM-02 | 智谱 AgentMore / agentmore-02 | 文件多目标/续传、更新、恢复和平台可靠性；不承担插件/代理组件 |
| V09-CLOUD-HW-02 | 华为云 CodeArts / codearts-02 | 网关正式装配、发布/补偿整链、ACME/cleanup 与服务域 |
| V09-CLOUD-WB-02 | 腾讯云 WorkBuddy / workbuddy-02 | **Xray 全部工作**、正式 Web、浏览器验收与证据矩阵 |

三包目前登记 **READY，开工/提交尚待核验**；生成派发词不等于平台已实际运行。上一轮 AM/HW/WB-01 接收的是检查点，整包剩余已转交，未标成完整 v0.9 完成。Xray 所有权转交不等于 Runtime 准入。

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
