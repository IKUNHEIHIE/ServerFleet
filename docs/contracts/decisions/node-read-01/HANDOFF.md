# READ-PLAN-01-R1 交接说明

- 任务：首条只读闭环的可批准方案与验收包（来源事实、onlineOnly 兼容、选择题返修）
- 会话：DeepSeek `deepseek-01`
- 目录：`D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-plan-ds01`
- 分支：`docs/node-read-plan-ds01`
- 原方案提交（保留）：`d0709fc1fe0a5518000bdb7b81f8c1f8f82334e1`
- 历史基线：`98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`
- 返修起点 HEAD：`d0709fc1fe0a5518000bdb7b81f8c1f8f82334e1`（未重写历史）
- 最终提交：本次 docs 返修提交（精确哈希见交接消息；写作时不预填未生成哈希）
- 状态：**PROPOSED / 待负责人批准；G2 未冻结；D01–D07 与 P1-C10 仍 OPEN**
- 当前主线核对：`ddf8c5b`；N03-A 已接收（`7ef7877`/`0ec6221`），W05 已接收（`b44352f`/`ddf8c5b`）。原 98a1f6b 未接收叙述保留为历史基线语境，不代表当前主线状态。

## 文件

| 文件 | 内容 |
|---|---|
| `PROPOSAL.md` | 推荐路线、字段/来源分层、R1-A/R1-B 范围（含 `onlineOnly`）、D01–D07 收敛、3 个负责人问题、实施拆分、缺失证据 |
| `ACCEPTANCE.md` | 可执行验收矩阵（含 `onlineOnly` 与当前 N03-A/W05 证据）、现有证据等级、未来测试层次、不能以离线/静态冒充真实闭环 |
| `HANDOFF.md` | 本交接 |

没有修改既有代码、候选、r03/原契约、批准状态、ports、依赖或其他任务文件。

## 本次返修

- **RP01-R01（字段/来源事实）**：PROPOSAL 新增“字段存在/权威/公开形状”分层表；明确 Node 已有 `Status` 和 `Metrics.ObservedAt`，真正缺 `revision/platform/agentVersion`；`Status` 权威/暂停吊销优先级与公共 `sampledAt` 语义仍是 D02/D04 待批提案；`platform/agentVersion` 是可选公开字段，公开 DTO 缺失时省略、局部 VM 可为 null，不是详情/R1-B 的非空前置。
- **RP01-R02（onlineOnly）**：R1-A、D01/D06/D07、Controller 拆分和 ACCEPTANCE 均补上 `onlineOnly:boolean` 默认 false；明确“先认证/策略预检 → 授权可见集合 → onlineOnly 过滤”；新增 COMPAT-02 覆盖省略/false/true、隐藏在线节点、过滤后空集、策略故障和坏 query（解析/HTTP 码待 D06）。
- **选择题**：Q1 只保留首版范围；Q3 改为独立的状态权威问题（legacy online 布尔 vs 五状态权威），不再用“现在/以后”的重复阶段选择；Q1 选 R1-full 时 Q3 必须选五状态权威，选项互相一致。
- **当前事实**：保留 98a1f6b 历史基线语境，同时标注当前主线 `ddf8c5b` 已接收 N03-A 与 W05；两者只是离线用例/隔离加载容器，不是真实 HTTP/fetch/浏览器闭环。

## 实际检查

| 检查 | 实际结果 |
|---|---|
| 引用/覆盖检查（脚本扫描 3 份文档的路径+行引用、D01–D07、验收关键词、Q 数量；分支缺失时回退主目录已提交文件） | **PASS：68 处引用、25 个文件、0 个路径/行号/内容覆盖问题；2 个主目录文件（N03-A-REVIEW、FOLLOWUP-03-REVIEW）按 [当前主线] 校验** |
| `git diff --cached --check` | PASS（提交前执行） |
| `npm run check:policy` | PASS（本返修最终内容后复跑；`reports/check-policy.json` 记录 `status: PASS`） |
| 真实 HTTP/认证/数据库/浏览器/cache 验收 | **未运行 / UNTESTED**；本任务仅文档方案，不声称已完成 |

未运行 `check:contracts`、`check:go`、`check:web` 或任何真实运行验收；本任务不修改实现或契约，也不把 r03 静态检查/离线模块测试当作真实接口已验收。

## 负责人需选择的问题（最多 3 个，未回复不构成批准）

1. **首版范围**：推荐 **R1-A legacy 列表**；替代为 **R1-full 列表+详情**。R1-A 先闭环身份、可见过滤、`onlineOnly` 兼容和旧列表，但首版 Web 没有详情/状态/版本/新鲜指标；R1-full 体验完整，但详情真正需要 status（D02）与 revision（D03）来源，`platform/agentVersion` 公开可省略、局部 VM 可为 null，不要求非空来源，且仍需存储原子性，首条闭环推迟。
2. **Web 首版凭证**：推荐**服务端会话 Cookie（HttpOnly/SameSite）**绑定可信主体，Bearer 仅供非浏览器；需批准列表增加 Cookie 认证及会话/CSRF/撤销设计。替代为 Bearer-only API 先行，但 Web 安全 Token 获取/存储未解决，可能不是完整 Web 闭环。
3. **状态权威**：推荐首版只用 legacy `online` 布尔，`status` 留到 R1-B；替代为现在批准五状态为权威并明确暂停/吊销与连接在线优先级（若 Q1 选 R1-full，本项必须选替代，才能生成详情必填 `status`）。

## 实施拆分与阻塞

1. 负责人批准 Q1–Q3 与 D01–D07 选定范围；契约 owner 统一修订原操作（不能用 r03 候选当冻结），保留 `onlineOnly` 参数/默认语义。
2. N03-A 已在当前主线接收（`7ef7877`/`0ec6221`）；Controller 可基于其 `ReadVisibleInventory` 接线，但不得把离线接收当 HTTP 完成。
3. 认证/会话/可信主体绑定；依赖 Q2/D05。
4. Controller R1-A：scope → inventory → check → 授权可见集合 → `onlineOnly` → legacy DTO/错误/no-store；依赖 1–3。
5. Web R1-A legacy source/列表 VM；依赖 4；当前主线 W05 `NodeLoader` 已接收（`b44352f`）可复用 loading/世代隔离，但真实 fetch/DTO 适配仍需实现。
6. 节点元数据源：revision（详情必需）、status 权威/优先级（D02）、共同采样时间（D04）；`platform/agentVersion` 仅可选，公开 DTO 缺失时省略、局部 VM 可为 null，不作为非空前置。
7. R1-B 富化列表/详情/当前 Web ViewModel；依赖 1/3/6。
8. 真实 HTTP/浏览器/安全验收；依赖 4/5（R1-B 时加 6/7）。本轮未执行。

可并行：认证设计、元数据源、契约统一、Web source 准备；必须先等：owner 批准、身份绑定、统一契约。N03-A/W05 已接收，但均不等于真实 HTTP/浏览器闭环。`PROPOSAL.md` 第 7 节有完整证据要求。

## 证据缺口与风险

- 认证/会话/CSRF/Token 撤销 ADR 与实现缺失；D05。
- 节点 revision 来源/存储原子性缺失；D03；当前 Node **已有 Status 和 Metrics.ObservedAt**，真正缺 revision/platform/agentVersion；Status 权威/优先级与公共 sampledAt 语义仍待 D02/D04，不是结构缺失。
- `platform/agentVersion` 是公开可选字段，公开 DTO 缺失时省略、局部 VM 可为 null，不是详情或 R1-B 的非空前置；不能把它们与 revision/status 真正前置条件混为一谈。
- `onlineOnly` 参数/默认 false 已存在于原 Merged/r03，但真实 HTTP 的预检→可见集合→过滤顺序、true/false/省略、隐藏在线节点、过滤后空集、坏 query 均无接线证据；坏 query 解析/HTTP 码待 D06。
- 真实旧客户端未知字段/扩展名兼容、真实 HTTP 错误/防枚举/限流、跨主体缓存均无证据；D01/D06/D07。
- N03-A 与 W05 已在当前主线接收（离线用例/隔离加载容器），但都不等于 Controller/HTTP/真实 fetch/浏览器；W06 真实浏览器验收尚未执行。
- 当前 Web `NodeListItemView` 需要 revision/status，R1-A 需要局部 legacy ViewModel/适配；R1-B 才使用当前五状态 VM。
- 本文件的历史引用基于 `98a1f6b`；当前主线事实标 `[当前主线] ddf8c5b`。后续契约/实现变更需重新核对行号和事实，不能把本方案当冻结判断。

## 停止

仅创建本地 docs 提交，交总控 codex-review-01 审核后停止；不合并 main、不推送、不启动实现或其他 Agent，不读取 Codex/Gemini 未提交内容。
