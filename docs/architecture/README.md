# 当前架构与来源

原始资料保存在 `../reference/ServerFleet-ALL-v0.9-G2/`，不参与源码依赖。包根 `00-请先阅读.md` 和 SHA256 清单是来源说明。

现行文件集为 `01-当前开发基线/`，阅读次序：

1. `README-ServerFleet-G2.md`、`README-ServerFleet-v0.9-docs-r2.md` 与 `AGENTS.md`。
2. `ServerFleet-Functional-Requirements-v0.9.md`、`Plugin/xray-manager/FR-PLUGIN-XRAY-001.md`，以及 `docs/PRODUCT-ACCEPTANCE-MATRIX-v0.9.md`。
3. `architecture/G1-DECISIONS-Accepted.md`、`ARCHITECTURE-BASELINE.md`、`SOURCE-AND-SUPERSESSION-INDEX.md`。
4. 正式 `ADR-PLUGIN-001-v0.3-Accepted.md`、`ADR-DEPLOY-001-v0.3-Accepted.md`、`ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md`；`architecture/history/` 中未冲突旧条款继续有效。
5. `g2/G2-CORE-CONTRACT-RULES.md`、`AGENT-TRANSPORT-AND-STATE-MACHINE.md`、两份 OpenAPI 候选、Agent Proto、`G2-FREEZE-AND-GAP-REGISTER.md` 与负向测试计划。

G1 的 PLUGIN v0.3 替代旧编译期内置业务插件条款；DEPLOY v0.3 扩展全新安装 Nginx 路径；AI/PRIV 附录保留强制审批。`jobs.submit` 是历史内部能力名，当前公开候选只用 `/api/v1/tasks`。

资料包未独立收录 PRIV、WC、PKI-OPS、UPDATE 的所有原始 ADR。涉及其详细实现前需从原项目归档核验；P1 不实施这些业务，也不捏造条款。历史包不展开到源码中。

1 vCPU / 1GB RAM 仍待硬限制/实机验收；WASM/Eino 未锁定。当前已有SQLite、授权、WSS+mTLS、随机持久密钥、AI持久预算/typed审计与本地TLS生成，文件WSS模块及自己拥有的更新子进程有隔离证据；完整任务文件worker/发布/灾备等仍有缺口。最新状态以`../development/TASK-BOARD.md`与`../development/reviews/V09-WAVE6-R1-REVIEW.md`为准；编译和静态检查不能代替运行时安全验证。
