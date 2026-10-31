# G2 设计增量变更记录（不改变产品版本与 G1 ADR）

**日期：2026-10-29；阶段：G2 候选，不是已批准协议冻结。**

- 从 G1 已批准完整包复制源工件，原 v0.9 主契约、Xray 独立契约、产品规范、原型和历史 ADR 保持原样。
- 新增 `G2-Core-OpenAPI-candidate.yaml`：认证/初始化、用户与资源权限、审计、节点身份及能力、唯一任务查询/审批/事件流、AI 消息事件、Agent enrollment 共 29 路径 34 操作。
- 新增 `ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml`：在不覆盖原 151 操作和 Schema 的情况下供集成评审，形成 147 路径 185 操作。
- 任务命名候选统一 `/api/v1/tasks`；历史 `/jobs` 只作为内部领域或旧 ADR 示例，不启用第二套调度。
- 新增 Agent 的二进制 WebSocket Protobuf Envelope 草案、版本/能力协商、执行 ID 去重、断线状态同步消息。
- 新增通用错误（参考 RFC 9457）、CSRF 会话、幂等键、修订条件头以及权限拒绝记录的设计候选。
- 新增状态机、负向测试计划和冻结缺口登记；WASM Runtime、Agent 心跳/帧限额等仍须 G1 PoC。
- **未更改**产品 v0.9 功能范围、唯一 Xray 插件、旧版 Release、HTML 线上原型和任何已批准 ADR。
