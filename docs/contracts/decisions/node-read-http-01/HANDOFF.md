# READ-HTTP-DESIGN-01 交接

- 任务：A1 兼容只读列表的具体 HTTP 设计（A1+B1 方向已确认，详细规则仍 PROPOSED）
- 会话：DeepSeek `deepseek-01`；审查人：总控 `codex-review-01`
- 目录：`D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-http-design-ds01`
- 分支：`docs/node-read-http-design-ds01`
- 起始 HEAD：`4196b2e8ab9c389ae0830bf56f905d9bafa00d05`
- 最终提交：本次 docs 提交（精确哈希见交接消息；写作时不预填未生成哈希）
- 状态：**PROPOSED / 待安全、契约、节点审查；未实现，D01–D07/P1-C10 仍 OPEN，G2 未冻结**

## 文件

| 文件 | 内容 |
|---|---|
| `DESIGN.md` | 操作兼容差异、请求管线、onlineOnly 解析、字段投影/来源、分阶段错误、可信来源、撤权观察边界、缓存、旧客户端兼容、实施拆分 |
| `ACCEPTANCE.md` | 14 组可执行验收场景、观察方法、历史证据与 UNTESTED 边界 |
| `HANDOFF.md` | 本交接 |

没有修改既有文件、代码、候选、ports、OpenAPI、依赖或批准状态；只新增本目录 3 份 Markdown。

## 设计结论摘要

- **A1**：只保留既有 `GET /api/v1/nodes`、`listNodesForTransfer`、`{items}`、必填 `id/name/online`、`onlineOnly` 默认 false；不提供详情、`status/revision/platform/agentVersion/lastSeenAt/sampledAt`、分页/total 或新路由。
- **B1**：浏览器服务端会话 Cookie；非浏览器 Bearer 兼容支持仍待认证设计/D05 审查。Cookie 加入旧 Bearer-only 列表是契约变更，本设计没有修改 OpenAPI。
- **顺序**：认证 → query 解析 → N03-A 预检/库存/同次授权 → 可见集合 → onlineOnly 过滤 → 字段投影 → 全量序列化/提交。HTTP 不再单独预检，避免策略读取两次。
- **onlineOnly**：省略/false 返回全部可见节点（含离线）；true 只保留可见在线节点；空值、重复、非小写 `true/false` 值返回 400；坏 query 不触发策略/库存；具体 parser/HTTP 码待 D06。
- **字段**：只输出 `id/name/online/region/provider/cpuPercent/memoryPercent`；CPU/内存保留可空旧值但无 `sampledAt`，离线/非法/越界 → null，不夹取、不补 0、不称新鲜；`online` 来源映射只是待 D02 批准的提案。
- **错误**：按阶段映射 401/400/403/503/500/504；坏客户端 query 才能 400，坏库存 ID/重复 ID/同次 CheckNode InvalidCheck 不归咎客户端；列表保持旧 `Error` application/json；不泄露 ID/原异常。所有成功/错误 no-store。
- **来源**：Authenticator 由认证设计提供；PolicyReader/InventorySource 每请求一次、当前只读；N03-A 已接收的离线用例复用；无默认 mock/空成功，nil/未配置 fail closed。
- **撤权/输出**：T0 认证、T1 策略、T2 库存、T3 检查、T4 投影、T5 提交；只保证各时点的可观察结果，不保证策略/库存/输出原子性；T1 后撤权可能不阻止在途响应，下一请求必须重新预检；不靠再读一次消除 TOCTOU。
- **缓存/旧客户端**：成功与错误 no-store + 建议 Vary；不缓存 scope/列表；旧 Bearer、路径/操作/必填字段/onlineOnly 保持，Cookie 和新错误码/可选字段省略需真实客户端探测；不宣称完全兼容。

## 实际检查

| 检查 | 实际结果 |
|---|---|
| DESIGN/ACCEPTANCE 引用+关键覆盖检查（路径/行号、onlineOnly、错误阶段、字段、撤权、缓存、旧客户端；本地分支缺失时回退主目录已提交文件） | **PASS：60 处引用、22 个文件、0 个问题** |
| `git diff --cached --check` | PASS（提交前执行） |
| `npm run check:policy` | **PASS**（`reports/check-policy.json` 记录 `status: PASS`，executedAt 2026-10-29T14:15:38Z） |
| 真实 HTTP/认证/数据库/浏览器/代理/cache | **未运行 / UNTESTED**；本任务只提交设计，不声称 A1 已实现 |

没有运行 `check:contracts`、`check:go`、`check:web`、真实 HTTP 或浏览器测试；静态/离线证据只在 ACCEPTANCE 中标注为历史范围，不冒充真实闭环。

## 依赖与未定项

- **认证设计（READ-AUTH-DESIGN-01）**：Cookie 会话/Bearer 支持范围、双凭据拒绝、可信主体、会话撤销/停用/降权边界和错误阶段；HTTP 只消费其结果，不自行实现。
- **Web 设计（READ-WEB-DESIGN-01）**：legacy VM、无 sampledAt 指标显示、来源/身份切换首提交清理、onlineOnly 结果/无 total 计数和 Cookie 请求策略。
- **契约 owner**：Cookie 加入旧 Bearer-only 列表、onlineOnly parser、错误码/媒体、可选字段省略需批准；本设计不改 OpenAPI/r03/r02。
- **安全/节点/存储**：D01 旧客户端、D02 online/五状态权威、D04 指标采样、D05 凭据、D06 错误、D07 空库存/原子输出/撤权/缓存全部仍 OPEN；策略与库存来源的真实存储/快照/事务未实现。

## 剩余风险

- 设计未批准，不能作为实现或执行许可；Cookie/Bearer 兼容、错误码/parser、字段输出都可能被审查修改。
- 真实身份/会话/策略来源/库存来源/HTTP/缓存/输出原子性均无运行证据；N03-A/W05/W06 的离线/组件历史 PASS 不等于 HTTP 闭环。
- 严格 `onlineOnly` parser 可能拒绝旧客户端的 `1/0/TRUE` 等别名；`ip/tags/countryCode` 省略、CPU/内存 null/无 sampledAt、新错误码都可能影响旧客户端，需真实探测。
- 旧 `Error`/Problem 媒体差异保留；列表仍使用 Error，详情未来另批；不实现详情防枚举。
- 没有并发/race/Linux/真实浏览器/代理 cache 验证；不能宣称跨主体缓存或撤权已关闭。

## 停止

仅创建本地 docs 提交，交总控 codex-review-01 审查后停止；不合并 main、不推送、不启动实现或其他 Agent，不修改其他人的未提交内容。
