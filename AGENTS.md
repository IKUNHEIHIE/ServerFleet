# Server Fleet · AI 开发会话规则

## 当前关卡和资料

P1 工程基础已完成。G0 产品已确认、G1 架构获批，G2 未冻结。当前授权任务与审查状态以 `docs/development/TASK-BOARD.md` 为准；G2-R01 只作为评审资料与候选接收，不批准运行时 profile、真实执行或接口冻结。先读 README、`docs/architecture/README.md`、`docs/development/MODULE-OWNERSHIP.md`、`docs/development/DEPENDENCY-RULES.md` 和任务单。完成分配的任务后停止，不自行扩展任务或启动其他 Agent。

需求优先于候选；已批准 G1/安全 ADR 的约束有效。`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线` 是现行原始文件集。原始 ZIP、参考资料和已批准历史 ADR 不可改写；`02-历史交付包` 仅追溯。HTML 是交互原型，不可直接作为 React 正式工程。发现冲突登记到 `docs/contracts/P1-CANDIDATE-REVIEW.md`，不可静默选旧方案。

## 一个任务一个会话一个工作目录

- 开始前必须有任务 ID、会话唯一 ID（如 `codex-01`/`deepseek-02`/`gemini-01`）、允许目录、审查人和停止条件。模型名称不等于唯一会话。
- 并行任务在独立 Git Worktree、独立分支执行；主工作目录留给集成负责人。跨模块或共享文件修改先提交具体差异供负责人批准。只读检查和当前任务已授权的常规修复可直接完成。
- 不得修改其他任务目录、切换他人分支、强推、`reset --hard`、`clean -fd`、绕过冲突覆盖文件或删除他人数据。不自动 stash 他人变更。
- 公共契约、生成配置、根锁文件、数据库公共结构、CI、依赖规则由集成负责人协调；独立 `contract/...` 变更说明兼容性。生成代码只能用固定工具生成。
- 不自动创建/推送远程仓库、不使用真实凭据、不访问生产/VPS/云资源。此 P1 指令明确禁止云资源操作，AWS 规则不触发建云流程。

## 不可绕过的架构

- Controller 是 Go 模块化单体，未来 SQLite WAL；Agent 主动 WSS+mTLS，每帧 Protobuf；Web 只调用 Controller REST/SSE。
- 所有执行经 Controller 的统一授权、任务、审批、审计；Web、AI、插件不直连 Agent、不访问数据库、不另造 SSH 执行通道。
- AI 默认零资源授权、服务组成员快照；完全访问不豁免 AI 任意 root Shell 的真人管理员审批，不暴露 CA/恢复私钥或 Token 明文。
- 普通任务并发默认 5；root/修改类默认 1→5；离线未启动默认 15 分钟 TTL；未知结果不自动重跑有副作用操作。
- 可选插件不进入核心 Release。唯一已批准可选插件为 Xray；仅声明式 UI，WASM 和 Eino ADK 仍待 PoC，禁止擅定 Runtime 或恢复已删演示插件。
- 端口是临时内部协作接口，不是完整安全实现或 G2 冻结证据。不得加默认放行的生产 mock。

## 修改和验证

2026-11-01负责人最新授权：由codex-review-02重新总控，并可使用Windows已保存SSH别名`awskr`作Controller测试节点。仅总控可在该主机普通用户私有目录开展隔离测试，不触碰其已有服务/站点/数据库/防火墙，不扩展其他主机/云API或使用真实外部秘密。该具体授权优先于上文P1对指定主机测试访问的限制，其他安全/架构约束保持；详见`docs/development/sprints/v09-five-day/CONTROL-RESUME.md`。

使用库、框架、SDK、API 或 CLI 时先用 Context7 `resolve-library-id`，再查当前文档；普通业务逻辑/脚本/代码审查无需调用。优先 `rg` 检索。保持简单、仅做任务范围内改动。

提交遵循 `type(scope): 描述`；分支为 `feature/`、`fix/`、`contract/`、`integration/`、`docs/` 或 `chore/`。合并前 `npm ci`、`npm run tools:install`、`npm run check`、`npm run check:security`；根据模块再跑专用检查和负向测试。任何 FAIL/BLOCKED/UNTESTED 如实记录，不得删除测试或伪造成功。

任务提交必须写明文件、命令、输出、候选依赖与剩余风险。审查公共接口必须同时检查 OpenAPI/Proto、生成代码、兼容性和安全边界。只有集成负责人合并 main，模块作者不得自行合并。负责人批准远程连接后再配置真实 CODEOWNERS 和分支保护。
