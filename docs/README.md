# 文档导航与适用范围

新接手先读 [HANDOFF](development/HANDOFF.md) 与 [CURRENT-STATE](development/CURRENT-STATE.md)。

| 用途 | 现行入口 | 使用规则 |
|---|---|---|
| 当前状态与交接 | [当前状态](development/CURRENT-STATE.md)、[开发交接](development/HANDOFF.md) | 状态统一入口，继续开发时随代码更新 |
| 产品与前端 | [功能说明](product/FEATURES.md)、[API](api/README.md) | 与当前 DTO/处理器交叉核对 |
| 安装、更新、恢复 | [部署](../deployment/README.md)、[raw 安装](../deployment/RAW-INSTALL.md)、[升级](../deployment/UPGRADE.md) | 使用现行脚本/配置，示例不包含可用秘密或身份 |
| 开发规则与目录 | [开发导航](development/README.md)、[AGENTS](../AGENTS.md) | 按当前任务执行，无历史模型/平台固定分工 |
| 批准约束 | [架构来源](architecture/README.md) | 按批准与替代关系；实现不能反向改写 ADR |
| 发布证据 | [v0.9.2](releases/v0.9.2.md)、[当前状态回执](development/CURRENT-STATE.md#发布与检查回执) | 精确提交/工作流与实机验收分开 |

## 历史与不可变资料

- `development/sprints/`、旧工作包、平台计划、`PRODUCT-STARTUP.md`、`CORE-PRIORITY-STARTUP.md`、任务板旧条目：保留当时任务、命令和限制供追溯。其中“当前”“待交付”“停止条件”只针对原会话，不自动成为新任务。
- `audits/`、审查报告与验收记录：按日期/提交理解；旧缺陷查看后续修复，PASS/FAIL/SKIP 不改写，不外推到新 head。
- `reference/`：原始需求/ADR/归档与校验材料保留原字节。“当前开发基线”是原交付包内部目录名，不表示最新源码状态。
- `contracts/`、`api/openapi/revisions/`：候选与增量协议资料；路径覆盖不代表字段全量核对或 G2 冻结。看 [API 阅读顺序](api/README.md#候选与版本阅读顺序)。

冲突先核对当前 main、处理器/DTO/配置、增量修订与批准 ADR，再修正现行入口。不要删除历史来隐藏差异。
