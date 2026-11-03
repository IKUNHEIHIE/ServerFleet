# 当前架构与来源

原始资料保存在 `../reference/ServerFleet-ALL-v0.9-G2/`，不参与源码依赖。包根 `00-请先阅读.md` 和 SHA256 清单是来源说明。

现行文件集为 `01-当前开发基线/`，阅读次序：

1. `README-ServerFleet-G2.md`、`README-ServerFleet-v0.9-docs-r2.md` 与 `AGENTS.md`。
2. `ServerFleet-Functional-Requirements-v0.9.md`、`Plugin/xray-manager/FR-PLUGIN-XRAY-001.md`，以及 `docs/PRODUCT-ACCEPTANCE-MATRIX-v0.9.md`。
3. `architecture/G1-DECISIONS-Accepted.md`、`ARCHITECTURE-BASELINE.md`、`SOURCE-AND-SUPERSESSION-INDEX.md`。
4. 正式 `ADR-PLUGIN-001-v0.3-Accepted.md`、`ADR-DEPLOY-001-v0.3-Accepted.md`、`ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md`；`architecture/history/` 中未冲突旧条款继续有效。
5. `g2/G2-CORE-CONTRACT-RULES.md`、`AGENT-TRANSPORT-AND-STATE-MACHINE.md`、两份 OpenAPI 候选、Agent Proto、`G2-FREEZE-AND-GAP-REGISTER.md` 与负向测试计划。

G1 的 PLUGIN v0.3 替代旧编译期内置业务插件条款；DEPLOY v0.3 扩展全新安装 Nginx 路径；AI/PRIV 附录保留强制审批。`jobs.submit` 是历史内部能力名，当前公开候选只用 `/api/v1/tasks`。

资料包未独立收录 PRIV、WC、PKI-OPS、UPDATE 的所有原始 ADR。涉及其详细规则需从原项目归档核验；P1 阶段的非业务实施边界是历史，当前已有更新/恢复等实现，但不能用实现反向补造缺失的正式条款。历史包不展开到源码中。

## 当前实现与验证范围（2026-11-03）

已接收产品基线为 `51e0ee57e66493892eb5f5168b9e0690b53f3661`，独立 main CI 成功，正式迁移26项。Controller/Agent、SQLite WAL、REST/SSE、WSS+mTLS、统一任务/审批/审计已有实现；真实文件CLI/双Agent多文件及三种冲突策略、文件页面、AI预算HTTP/UI与TLS生成已有验证。

云端首轮补齐用户上传统一任务/审批/取消/终态对账/实际摘要；接收网关工件、独立子执行、冻结计划与持久终态CAS；Agent组件本地签名更新/健康回滚已有真实浏览器专项证据。仅这些检查点获接收，不代表全部业务已装配或G2冻结。

多目标/完整续传、网关中央装配及完整发布/补偿、Controller可信更新/在线发行装配、实际灾备新权威/旧主控隔离等仍未完成。1 vCPU / 1 GB RAM、物理双机、ARM实际运行和真实外部供应商仍待验收；WASM/Eino未获Runtime准入。Xray当前全范围由WorkBuddy承担，所有权变更不代表准入获批，可选插件仍不能编入核心Release。

最新状态见[任务板](../development/TASK-BOARD.md)、[当前进度](../development/sprints/v09-five-day/CURRENT-PROGRESS.md)、[云端首轮接收报告](../development/reviews/V09-CLOUD-R1-REVIEW.md)与[Cloud R2任务](../development/sprints/v09-five-day/cloud-r2/PLAN.md)。默认缺可信配置仍拒绝，编译/CI不能代替完整运行时安全与产品验收。
