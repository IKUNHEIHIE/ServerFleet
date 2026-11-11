# 当前架构与来源

原始资料保存在 `../reference/ServerFleet-ALL-v0.9-G2/`，不参与源码依赖。包根 `00-请先阅读.md` 和 SHA256 清单是来源说明。

现行文件集为 `01-当前开发基线/`，阅读次序：

1. `README-ServerFleet-G2.md`、`README-ServerFleet-v0.9-docs-r2.md` 与 `AGENTS.md`。
2. `ServerFleet-Functional-Requirements-v0.9.md`、`Plugin/xray-manager/FR-PLUGIN-XRAY-001.md`，以及 `docs/PRODUCT-ACCEPTANCE-MATRIX-v0.9.md`。
3. `architecture/G1-DECISIONS-Accepted.md`、`ARCHITECTURE-BASELINE.md`、`SOURCE-AND-SUPERSESSION-INDEX.md`。
4. 正式 `ADR-PLUGIN-001-v0.3-Accepted.md`、`ADR-DEPLOY-001-v0.3-Accepted.md`、`ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md`；`architecture/history/` 中未冲突旧条款继续有效。
5. `g2/G2-CORE-CONTRACT-RULES.md`、`AGENT-TRANSPORT-AND-STATE-MACHINE.md`、两份 OpenAPI 候选、Agent Proto、`G2-FREEZE-AND-GAP-REGISTER.md` 与负向测试计划。

G1 的 PLUGIN v0.3 替代旧编译期内置业务插件条款；DEPLOY v0.3 扩展全新安装 Nginx 路径；AI/PRIV 附录保留强制审批。`jobs.submit` 是历史内部能力名，当前公开候选只用 `/api/v1/tasks`。

原69项资料包未独立收录的PRIV/WC/PKI-OPS/UPDATE四份已批准原文，2026-11-08已从用户原始归档找回，单独保存在[恢复来源](../reference/recovered-adrs-20261108/README.md)，字节和SHA见该目录清单；原69项/旧包不变。先按现行G1替代关系核对，不用旧原文覆盖后续批准解释，不用当前实现反向补造条款。来源恢复不等于权限分离/更新/灾备已实现或G2已冻结。

## 当前实现与审计入口（2026-11-12）

当前核心实现、条件装配与验收状态见[现行功能说明](../product/FEATURES.md)，接口见[API说明](../api/README.md)，目录/模块边界见[项目结构](../development/PROJECT-STRUCTURE.md)和[依赖规则](../development/DEPENDENCY-RULES.md)。R33已由PR24合入main8aa1ac0，正式49项迁移；R34审计及结构整改单独按精确head检查/接收，不用旧CI替代。

Controller为Go模块化单体，SQLite WAL；Agent主动WSS+mTLS、Protobuf，每帧当前授权；Web仅REST/SSE。统一任务/审批/审计、独立节点资源范围、UNKNOWN不重放、离线Root与恢复代次/旧权威隔离约束继续有效。可选Xray不进核心Release，PluginRuntime/Eino/WASM未因代码存在而获得准入；受限Bearer读取HTTP尚未装配。

原始需求/批准ADR保留不可变；当前实现不能反向改写批准条款，接口候选未冻结。历史检查、物理/供应商/Root/硬1C1G和G2发布准入边界见[进度记录](../development/sprints/v09-five-day/CURRENT-PROGRESS.md)；历史“尚未实现”的语句不替代后续已合并源码。
