# AGENTS.md — Server Fleet 文档基线与 AI 开发纪律

本目录为 **v0.9/docs-r2 产品收口快照**，不是已完成的真实服务端代码仓库。

1. 先读 `README-ServerFleet-v0.9-docs-r2.md`，再读现行 `ServerFleet-Functional-Requirements-v0.9.md`，开发 Xray 时必须同时读 `Plugin/xray-manager/FR-PLUGIN-XRAY-001.md`。
2. **不要把 HTML 的交互模拟当真实执行或安全验证。** 当前无可部署 Controller/Agent、Xray 插件真实程序包和后端鉴权。
3. `ServerFleet-API-Contract-v0.9.yaml` 与 Xray 独立 OpenAPI 为草案。没有完成 G2 前不得宣称全部 API 冻结，也不得因缺失而无审查自行添加不兼容路径。
4. **G1 新架构决策已于 2026-10-29 获批准**：先读 `architecture/G1-DECISIONS-Accepted.md`、`architecture/ADR-PLUGIN-001-v0.3-Accepted.md`、`architecture/ADR-DEPLOY-001-v0.3-Accepted.md` 和 AI/PRIV 解释。旧编译期插件与仅接管已安装 Nginx 的冲突条款已被正式替代；不得再作为现行实现依据。
5. AI 默认零资源授权；每次工具调用后端校验用户权限、会话资源范围和审批；AI 任意 root Shell 不受“完全访问”豁免。
6. Web、AI、插件一律复用受控权限和后台任务体系；不得自行建隐藏的 Agent 执行通道或直接获取主控 CA 私钥、Cloudflare Token 等秘密。
7. 当前仅有 Xray 这一个**获批未开发**的可选插件。不得恢复已删除的六个演示插件，不得把 Xray 编译进核心 Release 作为既定方案。
8. 对节点变更必须有固定目标快照、幂等、逐节点结果、失败停批和不可逆风险说明。Xray 实例接管需要人工明确确认；卸载插件不可自动删除节点 Xray。
9. 不允许直接改写已批准的历史 ADR；新增的设计用独立提案记录，并标注影响及待批准项。
10. 任何文档或接口改动必须记录差异、运行验证、更新索引与验收矩阵；G1 决策已批准但 PoC 尚未完成，G2 接口仍未冻结，先完成隔离 PoC 和核心契约评审。

11. G1-D2 的 WASM 只是首选 PoC，不得在缺少证据时宣称已是最终实现；失败须提交替代 ADR。
12. G1 后可以开始 G2 接口设计及非争议工程脚手架，但不能自行认定 API/Protobuf 已冻结或开展未经批准的生产节点变更。
