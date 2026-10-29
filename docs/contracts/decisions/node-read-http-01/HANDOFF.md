# READ-HTTP-DESIGN-01-R1 交接

- 任务：A1 兼容只读列表 HTTP 设计（HD01-R01 身份源错误分支、HD01-R02 最小可信来源返修）
- 会话：DeepSeek `deepseek-01`；审查人：总控 `codex-review-01`
- 目录：`D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-http-design-ds01`
- 分支：`docs/node-read-http-design-ds01`
- 原方案提交（保留）：`ece5efd77e8c99fb9a9d1f19eb31db5bd78b3734`
- 返修起点 HEAD：`ece5efd77e8c99fb9a9d1f19eb31db5bd78b3734`（未重写历史）
- 最终提交：本次 docs 返修提交（精确哈希见交接消息；写作时不预填未生成哈希）
- 状态：**PROPOSED / 待安全、契约、节点审查；未实现，D01–D07/P1-C10 仍 OPEN，G2 未冻结**
- 返修依据：`docs/development/READ-DESIGN-01-REPAIR.md:19-21`（HD01-R01/R02）；复审 `docs/development/reviews/READ-DESIGN-01-REVIEW.md:29-39`。认证设计 `docs/contracts/decisions/node-read-auth-01/DESIGN.md:40,44-54,64-68,132-140,144-151` 只读核对，仍为 PROPOSED。

## 本次返修

- **HD01-R01**：DESIGN/ACCEPTANCE 将「凭据缺失/无效/过期/撤销/歧义」的 401 与「有效凭据但身份/会话来源不可用/坏记录」的 503 `IDENTITY_UNAVAILABLE` 分开；补优先级（Tauth 在 query 前，身份源故障 + 坏 query 仍 503；凭据无效 + 坏 query 为 401）、策略/库存 0 次、恢复后同凭据成功、不提示重登/不清会话；Tgate 失败按凭据失效/身份源不可用/授权世代变化分别映射并丢弃缓冲。
- **HD01-R02**：新增最小可信来源表：主体/会话/Token、显式 grants/授权世代、节点库存的各自权威持有者；受控建立/更新；每次请求的一次性只读快照/并发约束；初始空库、已知零授权、未配置来源、坏数据、失败行为；明确哪些数据等待真实 auth/Agent/存储接入，禁止公开默认 mock/假节点。
- **认证提案对齐**：只读核对 `48409bac` 的 Tauth/Tpolicy/Tgate/Tcommit/Tr、受限 Bearer 求交/上限与迁移差异，全部按 PROPOSED 处理；不将 Tgate、Cookie 名、opaque Token 或旧 JWT 兼容写成已接受。旧 Bearer **传输/路径**保留不等于旧 Bearer **凭据值**兼容；需盘点签发方/客户端并迁移。
- 保留原设计正确部分：唯一 GET、onlineOnly 参数/default、认证先行、授权后过滤、全量构建、不将后端 InvalidCheck 映射 400、隐藏计数、无伪造字段、Error 媒体和取消/缓存边界。

## 文件

| 文件 | 内容 |
|---|---|
| `DESIGN.md` | 操作兼容差异、请求管线、onlineOnly 解析、字段投影/来源、分阶段错误、可信来源、撤权观察边界、缓存、旧客户端兼容、实施拆分 |
| `ACCEPTANCE.md` | 17 组可执行验收场景（含身份源故障与最小来源）、观察方法、历史证据与 UNTESTED 边界 |
| `HANDOFF.md` | 本交接 |

没有修改既有文件、代码、候选、ports、OpenAPI、依赖或批准状态；只新增本目录 3 份 Markdown。

## 设计结论摘要

- **A1**：只保留既有 `GET /api/v1/nodes`、`listNodesForTransfer`、`{items}`、必填 `id/name/online`、`onlineOnly` 默认 false；不提供详情、`status/revision/platform/agentVersion/lastSeenAt/sampledAt`、分页/total 或新路由。
- **B1**：浏览器服务端会话 Cookie；非浏览器 Bearer 兼容支持仍待认证设计/D05 审查。认证提案使用服务器签发 opaque 受限 Token；旧 Bearer 传输/路径保留不等于旧 JWT/任意 Token 值兼容，需盘点旧签发方/客户端并迁移。Cookie 加入旧 Bearer-only 列表是契约变更，本设计没有修改 OpenAPI。
- **顺序**：Tauth 凭据/身份来源 → onlineOnly 解析 → Tpolicy/N03-A 预检/库存/同次授权 → 可见集合 → onlineOnly 过滤 → 字段投影/序列化缓冲 → Tgate（PROPOSED）→ 提交。HTTP 不再单独预检，避免策略读取两次。
- **onlineOnly**：省略/false 返回全部可见节点（含离线）；true 只保留可见在线节点；空值、重复、非小写 `true/false` 值返回 400；坏 query 不触发策略/库存；具体 parser/HTTP 码待 D06。
- **字段**：只输出 `id/name/online/region/provider/cpuPercent/memoryPercent`；CPU/内存保留可空旧值但无 `sampledAt`，离线/非法/越界 → null，不夹取、不补 0、不称新鲜；`online` 来源映射只是待 D02 批准的提案。
- **错误**：凭据无效 401；有效凭据后身份/会话源不可用 503 `IDENTITY_UNAVAILABLE`（不是 401/重登）；query 坏 400；策略拒绝 403/不可用 503；库存失败 503；坏库存 ID 500；取消/期限 504/无响应；列表保持旧 `Error` application/json；不泄露 ID/原异常；所有成功/错误 no-store。
- **来源**：Authenticator 由认证设计提供；PolicyReader/InventorySource 每请求一次、当前只读；N03-A 已接收的离线用例复用；新增权威持有/受控建立更新/初始空库/失败行为设计；无默认 mock/空成功，nil/未配置 fail closed。
- **撤权/输出**：Tauth、Tpolicy、库存读取、同次检查、投影/缓冲、Tgate（PROPOSED）、Tcommit；只保证各时点可观察结果，不保证策略/库存/输出原子性；Tgate 未批准/未实现时保留窗口，不靠再读一次消除 TOCTOU。
- **缓存/旧客户端**：成功与错误 no-store + 建议 Vary；不缓存 scope/列表；路径/操作/必填字段/onlineOnly 保持；旧 Bearer 值/Cookie/新错误码/可选字段省略需真实客户端/签发方探测；不宣称完全兼容。

## 实际检查

| 检查 | 实际结果 |
|---|---|
| DESIGN/ACCEPTANCE/HANDOFF 引用+关键覆盖检查（路径/行号、Tauth/Tpolicy/Tgate、identity unavailable、受限 Bearer、来源权威、onlineOnly、错误阶段、缓存、旧客户端） | **PASS：78 处引用、25 个文件、0 个问题；3 个主目录文件（auth DESIGN、REPAIR、REVIEW）按当前主线已提交记录校验** |
| `git diff --cached --check` | PASS（提交前执行） |
| `npm run check:policy` | **PASS**（`reports/check-policy.json` 记录 `status: PASS`，executedAt 2026-10-29T16:01:04Z） |
| 真实 HTTP/认证/数据库/浏览器/代理/cache | **未运行 / UNTESTED**；本任务只提交设计，不声称 A1 已实现 |

没有运行 `check:contracts`、`check:go`、`check:web`、真实 HTTP 或浏览器测试；静态/离线证据只在 ACCEPTANCE 中标注为历史范围，不冒充真实闭环。

## 依赖与未定项

- **认证设计（READ-AUTH-DESIGN-01，PROPOSED）**：Tauth/Tpolicy/Tgate/Tcommit/Tr、身份源 `identity.unavailable` 分类、Cookie/受限 Bearer 支持与迁移、双凭据拒绝、会话撤销/停用/降权边界；HTTP 只消费其结果，不自行实现。Tgate/opaque Token/旧凭据迁移未批准。
- **Web 设计（READ-WEB-DESIGN-01）**：legacy VM、无 sampledAt 指标显示、来源/身份切换首提交清理、onlineOnly 结果/无 total 计数和 Cookie 请求策略。
- **契约 owner**：Cookie 加入旧 Bearer-only 列表、Cookie 名/Bearer 格式迁移、onlineOnly parser、错误码/媒体、可选字段省略需批准；本设计不改 OpenAPI/r03/r02。
- **安全/节点/存储**：D01 旧客户端、D02 online/五状态权威、D04 指标采样、D05 凭据、D06 错误、D07 空库存/原子输出/撤权/缓存全部仍 OPEN；主体/会话/Token、显式 grants/授权世代、节点库存的权威来源与建立/更新/快照/事务未实现。

## 剩余风险

- 设计未批准，不能作为实现或执行许可；Cookie/Bearer 兼容、错误码/parser、字段输出都可能被审查修改。
- 真实身份/会话/策略来源/库存来源/HTTP/缓存/输出原子性均无运行证据；N03-A/W05/W06 的离线/组件历史 PASS 不等于 HTTP 闭环。
- 身份源不可用/坏记录与凭据无效的分类依赖认证设计 PROPOSED；Tgate 尚未批准/实现，Tpolicy 后撤权/输出窗口仍可能保留。
- 最小可信来源的权威持有/建立/更新/初始空库设计只是提案；没有真实 identity/policy/inventory 存储、Agent 注册或安装配置。
- 旧 Bearer 传输/路径保留不等于旧 JWT/外部 Token 值兼容；Cookie 名/Bearer opaque 迁移、旧签发方/客户端盘点未完成。
- 严格 `onlineOnly` parser 可能拒绝旧客户端的 `1/0/TRUE` 等别名；`ip/tags/countryCode` 省略、CPU/内存 null/无 sampledAt、新错误码都可能影响旧客户端，需真实探测。
- 旧 `Error`/Problem 媒体差异保留；列表仍使用 Error，详情未来另批；不实现详情防枚举。
- 没有并发/race/Linux/真实浏览器/代理 cache 验证；不能宣称跨主体缓存或撤权已关闭。

## 停止

仅创建本地 docs 提交，交总控 codex-review-01 审查后停止；不合并 main、不推送、不启动实现或其他 Agent，不修改其他人的未提交内容。
