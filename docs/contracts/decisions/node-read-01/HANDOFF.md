# READ-PLAN-01 交接说明

- 任务：首条只读闭环的可批准方案与验收包
- 会话：DeepSeek `deepseek-01`
- 目录：`D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-plan-ds01`
- 分支：`docs/node-read-plan-ds01`
- 起始提交：`98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`
- 最终提交：本次 docs 交付提交（精确哈希见交接消息；写作时不预填未生成哈希）
- 状态：**PROPOSED / 待负责人批准；G2 未冻结；D01–D07 与 P1-C10 仍 OPEN**

## 文件

| 文件 | 内容 |
|---|---|
| `PROPOSAL.md` | 推荐路线、当前事实/建议分层、R1-A/R1-B 范围、D01–D07 收敛、3 个负责人问题、实施拆分、缺失证据 |
| `ACCEPTANCE.md` | 可执行验收矩阵、现有证据等级、未来测试层次、不能以离线/静态冒充真实闭环 |
| `HANDOFF.md` | 本交接 |

没有修改既有代码、候选、r03/原契约、批准状态、ports、依赖或其他任务文件。

## 实际检查

| 检查 | 实际结果 |
|---|---|
| 引用/覆盖检查（本地脚本，扫描 3 份文档中的路径+行引用、D01–D07 与验收关键词） | **PASS：61 处引用、24 个文件、0 个路径/行号问题**（本文写作前的 PROPOSAL/ACCEPTANCE；HANDOFF 无新增路径引用） |
| `git diff --check` | PASS（提交前执行） |
| `npm run check:policy` | **PASS**（`reports/check-policy.json` 记录 `status: PASS`，executedAt 2026-10-29T09:58:08Z） |
| 真实 HTTP/认证/数据库/浏览器/cache 验收 | **未运行 / UNTESTED**；本任务仅文档方案，不声称已完成 |

未运行 `check:contracts`、`check:go`、`check:web` 或任何真实运行验收；本任务不修改实现或契约，也不把 r03 静态检查/离线模块测试当作真实接口已验收。

## 负责人需选择的问题（最多 3 个，未回复不构成批准）

1. **首版范围**：推荐 **R1-A list-only legacy 兼容列表**；替代为 R1-full 列表+详情+富化。R1-A 先闭环身份、可见过滤、空库存预检和旧列表，但首版 Web 没有详情/状态/版本/新鲜指标；R1-full 体验完整，但先要节点 revision/status/platform/agentVersion 来源、D02–D04 和存储原子性，首条闭环推迟。
2. **Web 首版凭证**：推荐**服务端会话 Cookie（HttpOnly/SameSite）**绑定可信主体，Bearer 仅供非浏览器；需批准列表增加 Cookie 认证及会话/CSRF/撤销设计。替代为 Bearer-only API 先行，但 Web 安全 Token 获取/存储未解决，可能不是完整 Web 闭环。
3. **节点元数据权威**：推荐把 revision/status/platform/agentVersion 与共同采样时间作为 **R1-B 前置来源任务**，R1-A 不伪造；替代为长期 legacy-only，实现快但无法提供详情/状态/新鲜指标，并需维护 legacy Web 分支。

## 实施拆分与阻塞

1. 负责人批准 Q1–Q3 与 D01–D07 选定范围；契约 owner 统一修订原操作（不能用 r03 候选当冻结）。
2. N03-A（预检先行离线读取用例）必须先接收；Controller 不得在未接收前自拼一套。
3. 认证/会话/可信主体绑定；依赖 Q2/D05。
4. Controller R1-A：scope → inventory → check → legacy DTO/错误/no-store；依赖 1–3。
5. Web R1-A legacy source/列表 VM；依赖 4；不要依赖未接收的 W05，需与 W05 owner 协调。
6. 节点元数据源（revision/status/platform/agentVersion/共同 sampledAt）；依赖 Q3/D02–D04，阻塞 R1-B。
7. R1-B 富化列表/详情/当前 Web ViewModel；依赖 1/3/6。
8. 真实 HTTP/浏览器/安全验收；依赖 4/5（R1-B 时加 6/7）。本轮未执行。

可并行：认证设计、N03-A、元数据源与契约统一；必须等待：owner 批准、N03-A 接收、身份绑定和统一契约。`PROPOSAL.md` 第 7 节有完整证据要求。

## 证据缺口与风险

- 认证/会话/CSRF/Token 撤销 ADR 与实现缺失；D05。
- 节点 revision 来源/存储原子性缺失；D03；当前 `internal/nodes.Node` 无 revision/platform/agentVersion。
- 五状态权威来源/优先级、共同 sampledAt/时效阈值缺失；D02/D04。
- 真实旧客户端未知字段/扩展名兼容、真实 HTTP 错误/防枚举/限流、跨主体缓存均无证据；D01/D06/D07。
- N03-A 已分配未接收；W05 加载容器未接收且 W05-R01 仍 OPEN；不能把未提交内容当依赖。
- 当前 Web `NodeListItemView` 需要 revision/status，R1-A 需要局部 legacy ViewModel/适配；R1-B 才使用当前五状态 VM。
- 本文件引用基于 `98a1f6b`；后续契约/实现变更需重新核对行号和事实，不能把本方案当冻结判断。

## 停止

仅创建本地 docs 提交，交总控 codex-review-01 审核后停止；不合并 main、不推送、不启动实现或其他 Agent，不读取 Codex/Gemini 未提交内容。
