# 模块归属与维护边界

2026-11-12 R34。当前由主控单会话协调开发/接口审查，具体任务、唯一会话、允许目录与停止条件以TASK-BOARD为准。旧三平台任务保留历史，不据此启动其他Agent或推断其当前写权限。

| 模块 | 职责/权威模型 |
|---|---|
| `apps/controller` | 程序生命周期、CLI和Controller依赖装配；server/composition_*按目录/路由/身份/适配分工 |
| `apps/agent` | 独立主动WSS+mTLS、能力执行、typed回执和本地账本 |
| `internal/auth` | 身份、会话、RBAC、节点授权与凭据；Bearer校验端口已实现但HTTP读取未装配 |
| `internal/nodes` | 节点资产、普通分类分组、指标和生命周期 |
| `internal/tasks` | 唯一任务/计划/审批/调度/执行/事件与异常人工处置权威 |
| `internal/audit` | 持久审计、授权读取和保留 |
| `internal/plugins` | 包来源/签名/生命周期、声明式Host端口；运行时仍需准入 |
| `internal/transport` | mTLS/WSS/Protobuf、能力和身份协议；经回调进入业务 |
| `internal/netprobe` | 仅标准库的HTTP/TCP/TLS与SSRF共享实现 |
| `internal/storage` | SQLite WAL连接、事务与迁移机制；不拥有领域规则 |
| `internal/ai` | 模型/会话/范围/工具/预算及多轮Agent运行状态 |
| `internal/workflows` | 事件匹配/工作流修订与步骤计划；执行交给统一任务 |
| `internal/updates` | 可信获取/签名/安装槽/监督与健康回滚 |
| `internal/recovery` | 加密备份、异地存储/清理、隔离恢复和权威/fence |
| `internal/files` | 文件源/目标/摘要、传输和显式续传 |
| `internal/monitoring` | 服务监控来源/观察、连续阈值和告警outbox |
| `internal/notifications` | SMTP/QQ配置、凭据引用和投递事实 |
| `internal/quota` | 主机流量增量、覆盖/账期/配额和到期提醒 |
| `internal/dns` | Cloudflare账号/Zone/记录及受控供应商操作 |
| `internal/certificates` | 公网/内部证书、CSR/信任、安装/续签策略与清理 |
| `internal/gateway` | Nginx网关、路由、健康/调度及任务 |
| `internal/servicegroups` | 服务成员/修订/暂停与引用 |
| `internal/publishing` | 冻结发布计划、阶段/补偿、DNS批次与证书替换 |
| `gen/go` | 固定Proto生成输出，无领域反向依赖 |
| `tests` | 合成/隔离/契约/集成证据，生产不可导入 |
| `apps/web/src/features` | 正式React功能页，权限展示来自Controller；不能替代服务端授权 |
| `apps/web/src/lib/api` | REST/DTO/已知候选兼容；不伪造观测或接受矛盾结果 |
| `api/openapi` / `proto` | 公共候选/字段号，原始源不可改写；独立revision审查 |
| `plugins/xray-manager` | 唯一已批准可选业务；核心不编入，生产执行装配继续OPEN |
| `scripts` / CI | 固定工具、架构/契约/生成/安全/文档漂移检查 |
| `docs/product` / `docs/api` / `docs/audits` | 现行功能/接口/审计；历史记录不充当当前状态 |

## 共享写入与兼容

Controller组合适配可经审查调用各域服务/查询/统一任务端口，不能取数据库或内部map来旁路授权执行。数据模型与schema归领域；storage只管理机制。允许方向以[依赖规则](DEPENDENCY-RULES.md)及JSON为准，实际安全另靠当前权限/审批/回执/隔离门。

根与Web包配置/锁、Go模块、buf/redocly、Proto/gen、公共迁移、CI和依赖规则由集成负责人协调。变更前登记任务和兼容差异；迁移只追加，49项历史SQL/checksum保持。R34是同包拆分和内部无引用代码清理，没有变更Go导出接口或允许依赖。

任务分支与PR可以提交，集成负责人在精确head审查/检查成功后以expected SHA正常合并；不直接推main、不强推、不自我APPROVE。各平台用同一GitHub账号不构成独立多方审核。

原始需求/ADR、候选、旧任务/验收/失败/SKIP保留原路径和字节。未装配的受控端口/可选插件不等于死代码；前端原型也不能直接代替正式产品。当前状态见[功能说明](../product/FEATURES.md)、[项目结构](PROJECT-STRUCTURE.md)、[任务板](TASK-BOARD.md)和[审计](../audits/2026-11-12.md)。
