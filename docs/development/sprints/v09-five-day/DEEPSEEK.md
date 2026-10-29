# V09-5D-DS · 数据与业务服务复合任务

会话 deepseek-02；分支 `feature/v09-services-ds02`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-services-ds02`；审查 codex-review-01+安全/契约/相关owner。核对总控派发精确SHA/分支/目录，不复用deepseek-01旧设计分支。

先读根AGENTS、README、架构/来源、模块归属/依赖规则、任务板、[共同计划](PLAN.md)、现行需求/AC矩阵、G1安全ADR、原tasks/nodes/auth/audit离线实现与已接收HTTP/认证/Web设计。完整v0.9是本次目标；不再每个小项完工就停止，先交首个接缝/可运行增量再连续推进。

## 允许写入

- `internal/storage/**`、`internal/nodes/**`、`internal/tasks/**`、`internal/audit/**`。
- 新领域目录 `internal/files/**`、`internal/monitoring/**`、`internal/notifications/**`、`internal/quota/**`、`internal/dns/**`、`internal/certificates/**`、`internal/gateway/**`、`internal/servicegroups/**`、`internal/publishing/**`；单一owner为本包。本次交接已登记最小依赖方向，可直接使用；额外跨模块import边交总控协调。
- `tests/integration/v09/services/**`、`tests/poc/v09/services/**`、`docs/development/sprints/v09-five-day/progress/deepseek/**`、自己的忽略`reports/_review/v09-services/`。
- 不改auth/transport/Controller与Agent入口、Web、可选插件、公共配置/锁/API/Proto/gen或原始资料。数据库迁移公共编号/台账、go依赖和公开ports按PLAN提交精确共享差异，不自行抢写。

## 连续子任务

| ID | 实施目标与可验收结果 |
|---|---|
| DS-01 | 首3小时提交SQLite驱动/版本与最小依赖差异、WAL/连接/事务/迁移注册语义、授权世代/任务仓储与取消/并发约定，给CX接缝。业务数据模型归领域，storage不反向import业务，只有storage使用database/sql；auth表由CX定义，迁移编号总控分配。随后实现并验证重启保持/坏库/事务失败/并发与恢复 |
| DS-02 | 可信节点库存/状态/指标、普通分组/自动分类、服务组独立资源及成员快照；按当前权限先预检再库存/过滤/计数，空库不绕过，坏必填无部分成功。真实节点数据来自CX受控Agent事件，当前性/版本/采样来源不伪造；先接已批准增量，富化字段按具体契约准入 |
| DS-03 | 统一任务持久提交/计划/审批接缝/调度/结果/日志/SSE与取消，复用已接收快照、分批/金丝雀/进度/熔断、TTL规则；任务修改使旧审批失效，执行前重新校验，未知结果不重跑副作用；通过CX transport/Agent受控通道，不能加SSH或第二队列 |
| DS-04 | 审计完整写入/检索/受限视图/保留期与关联任务；所有敏感修改和拒绝可追溯，去敏，失败策略明确。实际执行结果与前端状态一致，不用内存testkit替代持久审计 |
| DS-05 | 文件真实目录/路径权限、上传下载/双节点复制、校验/断点/冲突/取消/部分文件结果；源读与目标写分别授权，符号链接/路径穿越/空间不足/节点断线必测。Agent文件能力由CX实现，本包给精确命令语义并复用统一任务，不直接访问节点 |
| DS-06 | 资源采集消费、HTTP/TCP/TLS服务监测/失败恢复/维护、告警事件/去重/QQ与SMTP配置测试和备用；流量独立周期/估算/到期提醒，离线缺数据不补零，超额不自动断网；防SSRF/越权探测和通知重试风暴 |
| DS-07 | Cloudflare多账号/Zone/记录/只写凭据引用；公网ACME与内部PKI/独立身份分开，证书续签/下发/轮换/吊销；密钥生命周期安全接缝由CX提供，私钥不复制多节点、不返GET/日志/AI。外部测试须明确授权条件，未实测不报成功 |
| DS-08 | Nginx已有站点安全接管/经批准安装、健康/端口冲突/人工调度准入、服务组与网关路由；DNS/证书/网关一体化发布向导后端，分阶段结果和失败恢复。不能覆盖旧站点、把健康等于已准入或DNS缓存当即时回滚；POC-05需真实隔离证据 |
| DS-09 | 向CX提供模块HTTP注册/可信主体消费、任务与事件接缝；向GM提供已协调DTO/固定错误/请求示例/事件与任务ID，不抢改入口。给全局备份/恢复一致快照接缝、POC-07存储/任务/SSE吞吐和1核1GB资源证据；用重启/取消/并发/部分失败验证业务整体 |

## 依赖与交付

所有修改经Controller权限/任务/审批/审计，复用CX鉴权与受控命令，不用测试subject或默认allow兜底。首检查点先交DS-01及节点只读垂直增量；被CX接口/共享依赖阻塞时登记具体文件/patch/错误，继续同包其他独立实现和测试。本次已登记的依赖方向可直接用，额外边走总控，不关闭边界测试。

检查点逐项记录到自己progress的`STATUS.md`/`HANDOFF.md`/`SHARED-CHANGES.md`，包含SHA、实际命令/输出、端点/事件、数据持久与权限负向结果、候选依赖、环境。测试至少覆盖跨节点/账号/Zone/路径越权、空库/坏源/部分结果、并发revision/幂等/取消/TTL、崩溃恢复、任务审批失效、敏感日志、通知去重和SSRF；真实文件字节/第三方/Nginx证据与double分开。

按PLAN全部子项连续做，常规修复自行完成；库/API具体用法查Context7。交最终精确HEAD和证据后停止，确无独立工作时说明硬阻塞；不合并main/推送/重置/启动其他Agent。全量v0.9不得删功能或以501/假成功抵交付。
