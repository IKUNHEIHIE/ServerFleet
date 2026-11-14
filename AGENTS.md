# Server Fleet · 开发会话规则

## 开始前

先读 [开发交接](docs/development/HANDOFF.md)、[当前状态](docs/development/CURRENT-STATE.md)，再读任务涉及的功能、接口和模块文档。当前用户任务决定工作范围；旧任务、旧模型分工、历史部署授权不自动成为新会话指令。未明确要求时不启动其他 Agent。

项目已有 v0.9.2 核心发行物，不处于 P1 Mock 阶段；完整 G2 冻结/全量验收未记录完成。历史“待实现”不代表当前缺口，代码、CI、正式版命名不代表实机验收。冲突按交接入口核对并修正文档。

已批准需求与 ADR 仍约束开发。原始资料、候选与历史证据不重写；docs/reference 中的 AGENTS 是归档原文，不给现行源码分配任务。HTML 是原型，正式前端为 React。

## 工作目录与集成

- 记录任务 ID、唯一会话 ID、允许目录、验证与停止条件；可按当前请求推导常规实施细节，无需重复确认已授权工作。
- 独立任务使用独立分支/工作目录，并行时使用独立 Worktree；共享契约、生成配置、锁、迁移、CI、依赖边界由集成负责人协调。
- 不修改他人工作、不自动 stash、强推、reset --hard、clean -fd 或覆盖冲突。
- 已授权既有公有仓库 IKUNHEIHIE/ServerFleet 的任务分支/PR 与负责人正常合并；精确 head 审查，expected SHA 保护。不直接推 main、不绕过保护、不自动合并、不自我 APPROVE；同账号不冒称独立审查。
- 开发授权不自动扩展到生产/VPS、真实供应商秘密、云 API 或其他仓库。旧 Windows awskr 和历史云会话授权不继承，具体部署遵循当前会话授权。

## 架构与安全

- Controller 为 Go 模块化单体/SQLite WAL；Agent 主动 WSS+mTLS/Protobuf；Web 仅 Controller REST/SSE。
- 执行经过统一授权、任务、审批、审计；Web/AI/插件不能直连 Agent、数据库或另造 SSH 执行通道。
- 管理员无节点通配；AI 默认零资源，服务组冻结成员快照。“完全访问”仍保留任意 root Shell 真人管理员审批。不暴露私钥、令牌或凭据明文。
- 普通任务默认并发 5，root/修改类默认 1→5；离线未启动默认 TTL 15 分钟。UNKNOWN 不自动重放副作用，人工结案不改写原结果。
- 离线 Root/在线 Intermediate 分离，每节点独立私钥；链唯一并止于固定自签 Root。不得放宽校验解决安装/续期。
- 可选插件不进核心 Release；唯一批准业务插件为 Xray，生产 Runtime/Host 尚未装配。Eino/WASM/PoC 存在不代表准入，禁止生产默认放行 Mock。
- 迁移只追加，既有 SQL/checksum 不变；固定工具生成代码。未装配端口/可选插件不能仅因当前无调用删作死代码。

## 验证与交付

代码/协议/构建/依赖变化按 [工具链](docs/development/TOOLCHAIN.md)和[集成清单](docs/development/INTEGRATION-CHECKLIST.md)检查，记录精确提交与 FAIL/SKIP/BLOCKED/UNTESTED。优先 rg，外部 API/SDK 核对官方当前文档。

**纯文档遵循用户要求，不运行完整 CI**：核对相对链接、来源与状态、diff 空白、仅文档范围，提交及实际合并标题用 [skip ci]。涉及代码/配置/构建/契约则按影响验证；生成索引用生成器。未运行不写成通过，不绕过分支保护。

提交与合并标题遵循 type(scope): 描述。PR 说明最终行为/验证/限制；最终 head 结果优先补评论，避免为记绿灯编辑说明触发重复 CI。需要轮询时每十分钟一次。

功能、API 或 UI 行为变化同步产品/API 文档，区分新增、修改、删除。完成后更新当前状态和任务板；保留历史基线与结论，不把文档整理写成新产品验收。
