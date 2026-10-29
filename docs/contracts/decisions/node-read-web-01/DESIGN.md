# 首条只读节点列表 · Web 具体设计（A1 + B1）

状态：**PROPOSED**（设计建议，未批准、未实现）。所属任务 READ-WEB-DESIGN-01 / 会话 gemini-01；
审查人总控。基线 `4196b2e8ab9c389ae0830bf56f905d9bafa00d05`，分支 `docs/node-read-web-design-gm01`。

本文件只写设计，不修改 `apps/web` 生产代码、不新增 TSX/fetch/App/测试/mock，也不改任何契约或批准状态。
A1/B1 是负责人已确认的**方向**（`docs/contracts/decisions/node-read-01/OWNER-CHOICES.md`），
不是 D01–D07 的批准，也不冻结 G2。

## 1. 范围与非目标

| 项 | 本轮设计 | 说明 |
|---|---|---|
| 列表读取 | 兼容旧 `GET /api/v1/nodes` / `listNodesForTransfer` / `{items}` | 只读，无分页、无 total、无新路由 |
| 行类型 | 新增 legacy 局部行类型，不复用富化 `NodeListItemView` | 见 §3 |
| 详情 | **不在首版**；R1-B 迁移点见 §8 | 详情需要可信 `status`/`revision` |
| 富化字段 | 首版不显示 `status/revision/platform/agentVersion/lastSeenAt/sampledAt` | 不补假值 |
| 身份 | 浏览器用服务端会话 Cookie（B1），Web 不读 Cookie/Token | 列表契约当前仍 Bearer-only，见 §2.3 |
| 真实接线 | 不包含 | 无 fetch/App/登录；本轮无预览、无浏览器执行 |

非目标：`ip/tags/countryCode` 展示（D07 待定）、跨标签页会话同步（未定，§6.4）、任何写操作、
任何“本地过滤即授权”的表述。

## 2. 输入契约与既有事实（只读引用）

### 2.1 旧列表 DTO（现行合并候选与 r02 一致）

`api/openapi/g2-merged-candidate.yaml:6639-6678`（r02 同形：`api/openapi/revisions/g2-r02/merged-candidate.yaml:12905-12944`）：

| 字段 | 类型 | 必填 | 备注 |
|---|---|---|---|
| `id` | string | 是 | 节点标识 |
| `name` | string | 是 | 显示名 |
| `online` | boolean | 是 | 旧接口唯一的状态表达 |
| `region` / `provider` | string | 否 | 旧字段，有来源才输出（R1-A 建议保留） |
| `countryCode` | string `^[A-Z]{2}$` | 否 | 首版不显示（D07） |
| `ip` | string | 否 | 首版不显示（D07 敏感性） |
| `tags` | string[] | 否 | 首版不显示 |
| `cpuPercent` / `memoryPercent` | number \| null，0–100 | 否 | **无任何采样时间字段** |

列表操作：`GET /api/v1/nodes`，`operationId: listNodesForTransfer`，查询参数 `onlineOnly: boolean`
默认 `false`，成功响应 `{items: NodeSummary[]}`（`api/openapi/g2-merged-candidate.yaml:90-112`；
r03 同形：`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:20-35`，`NodeList` 见 `:209-217`）。

### 2.2 r03 候选的新增字段（本设计不使用）

`api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:217-302` 在保留旧字段的基础上新增可选
`status/revision/platform/agentVersion/lastSeenAt/sampledAt`，并用 `oneOf` 约束“出现 `status` 时
`online` 必须与之一致”。这些都是**候选提案**（D02–D04 OPEN，`docs/contracts/revisions/g2-r03/DECISIONS.md:7-12`），
首版既不一并读取，也不据此推导状态。

同一候选给出了合法旧响应示例 `legacyUntimed`（`:41-49`）：只有 `id/name/online/cpuPercent/memoryPercent`，
无状态、无 revision、无时间——这正是首版要正确处理的最坏情况。

### 2.3 契约缺口（本设计的阻塞依赖，不在本任务修改）

r03 候选把列表操作放在全局 `security: bearerAuth`（`:9-10`），只给**详情**声明了
`cookieSession` OR `bearerAuth`（`:193-196`）。B1 要求浏览器用服务端会话 Cookie，
因此**列表操作是否接受 `sf_session` Cookie、双凭据如何拒绝**必须由契约/认证设计批准（D05 + D06）。
在此之前：

- 本设计不假设列表已支持 Cookie，也不设计“Cookie 失败自动回退 Bearer”；
- Web 侧不读取、不存储任何凭据；请求是否需要 `credentials`/CSRF 头由未来认证设计决定；
- 这是 BLOCKED 依赖，不是 Web 能自行解决的兼容问题。

## 3. 局部 ViewModel：新增 legacy 行类型

### 3.1 为什么不能复用 `NodeListItemView`

现有 `NodeListItemView`（`apps/web/src/features/nodes/types.ts:66-78`）**要求** `revision: number`、
`status: NodeStatus`、`platform/agentVersion/lastSeenAt` 以及带 `sampledAt` 的 `MetricReading`。
旧 DTO 一个都没有：

| 富化字段 | 旧 DTO | 若强行复用会怎样 |
|---|---|---|
| `status` | 只有 `online: boolean` | 只能把 `false` 猜成 offline/suspended/revoked 之一——被明令禁止（`docs/contracts/decisions/node-read-01/OWNER-CHOICES.md` A1 行；PROPOSAL D02） |
| `revision` | 不存在 | 只能填 1/0/时间戳——伪造并发版本（PROPOSAL D03） |
| `platform/agentVersion/lastSeenAt` | 不存在 | 需编造来源 |
| `MetricReading.sampledAt` | 不存在 | 无法判定 fresh/stale，只能伪造时间或降级成 missing（丢失真实数值） |

结论：新增独立类型，**不做类型断言/强转**，也不放宽既有 `NodeListItemView` 的必填约束。

### 3.2 建议类型（设计草案，非最终命名）

```ts
// 局部展示类型，不是公共契约
type LegacyMetric =
  | { kind: "value"; percent: number; sampledAt: null }   // 旧 DTO 没有采样时间，永远 null
  | { kind: "absent" }                                    // 字段缺失或 null
  | { kind: "unavailable"; reason: "offline" }            // 节点不在线：历史值不当作当前读数
  | { kind: "invalid"; reason: "non-finite" | "out-of-range" };

interface LegacyNodeRow {
  id: string;          // 原样保留，不 trim、不改写
  name: string;        // 空字符串时展示回退（见 3.3），但类型仍是 string
  online: boolean;
  region: string | null;
  provider: string | null;
  cpu: LegacyMetric;
  memory: LegacyMetric;
}
```

`value` 分支显式带 `sampledAt: null`，因此“有数值但没有时间”在类型层面必须被处理，
展示层不可能把它当成新鲜读数；`unavailable("offline")` 让“离线节点的历史百分比不显示”
成为适配层的确定性输出，而不是靠渲染时忘记判断。`LegacyMetric` 没有 `fresh/stale` 分支，
“无时间的旧值被显示成新鲜”在类型层面不可表达。

### 3.3 逐字段转换/验证表

| 输入 | 规则 | 输出 | 非法/缺失时 |
|---|---|---|---|
| `id` | 必须是 `typeof === "string"` 且非空（含非全空白） | 原样字符串（不 trim，保留精确标识） | **整次失败**（身份字段，见 §3.3.2），不猜 ID、不丢弃该行 |
| `name` | string；空白或空 | 原样；渲染时空名回退显示 `id` 并加固定标记「名称为空」 | 非 string → 整个节点行丢弃 |
| `online` | 必须 `typeof === "boolean"` | `true/false` | 非 boolean → 丢弃该行（不按 falsy 处理） |
| `region` / `provider` | 可选 string | 非空 → 字符串；空串/空白 → `null` | 非 string → `null`（不猜） |
| `countryCode` / `ip` / `tags` | 首版不投影 | 忽略（即使存在） | 忽略，不报错（前向兼容） |
| `cpuPercent` / `memoryPercent` | **先按 `online` 判定当前可展示性**：`online=false` 时不展示任何历史百分比（无论数值是否合法）；`online=true` 时 `null`/缺失 → absent，有限数且在 0–100 → value（**0 与 100 合法**），`NaN`/`±Infinity` → invalid(non-finite)，越界 → invalid(out-of-range) | 见 §3.2、§3.3.1 | **不夹取、不以 0 补缺、不为离线节点显示历史值** |
| `status`/`revision`/`sampledAt` 等 r03 新字段若出现 | 首版忽略 | — | 不据此推导，不缓存 |
| 未知字段/扩展名 | 忽略未知字段 | — | 不因未知字段报错；与旧客户端扩展名冲突的实测属 D01 待办 |
| 重复 `id` | **整次失败**（身份冲突，见 §3.3.2），不任选一条渲染 | — | 不因顺序或显示偏好替后端决定权威记录 |
| 顶层 `items` 非数组 / 缺失 | 视为**响应形状非法**，整次失败（固定安全提示），不渲染部分数据 | — | 不是“0 个节点” |

这里刻意**没有**“部分行丢弃并计数”的 `dropped` 口径：身份/权威字段异常与重复 `id` 都整次失败，
纯展示字段异常在行内降级（见 §3.3.2）。因此不存在“只显示了一部分授权节点却未告知用户”的中间态。

### 3.3.1 online 与指标的交叉规则（WD01-R02）

旧候选允许 `online=false` 的节点携带历史 `cpuPercent/memoryPercent`（没有任何更新时间）。
若照原样显示，页面会出现「离线 + 23%」却没有时间，读者会把它当成当前读数——这违反需求
「离线节点显示数据缺失或更新时间」（`D:\admin\desktop\codex\myproject\ks\docs\development\READ-DESIGN-01-REPAIR.md:34`，主目录）。

因此适配层**先看 `online`，再看数值**：

| online | 数值 | 输出 | 页面显示 |
|---|---|---|---|
| `true` | `0` | `value(0, sampledAt:null)` | `0%` + 「无采样时间」 |
| `true` | `100` | `value(100, sampledAt:null)` | `100%` + 「无采样时间」 |
| `true` | 1–99 有限数 | `value(n, sampledAt:null)` | `n%` + 「无采样时间」 |
| `true` | `null`/缺失 | `absent` | `—` + 「无读数」 |
| `true` | `NaN`/`±Infinity` | `invalid(non-finite)` | `—` + 「读数非法」 |
| `true` | `-1`/`101` 等越界 | `invalid(out-of-range)` | `—` + 「读数非法」（不夹取） |
| `false` | 任意（含 0/100/23） | `unavailable("offline")` | `—` + 「离线，无当前读数」 |
| `false` | `null`/缺失 | `absent`（与上一种同文案） | `—` + 「离线，无当前读数」 |

要点：

- 离线行**仍然展示**（名称/连接/地区等），只是指标列不出现百分比；
- 不伪造更新时间、不把历史值标成「陈旧」（陈旧需要时间基准，旧 DTO 没有）；
- 不因该规则猜五状态：`online=false` 仍只写「离线」，不写暂停/吊销/未知；
- 该规则只影响展示判定，不改旧 schema、不改后端、不新增字段。

### 3.3.2 重复 ID、坏行与“整次失败”边界（澄清，WD01 附带）

原稿写「重复 id 保留首次、坏行 dropped」，这是作者提案，需说明它与“整体不返回部分成功”的关系。
修正后的规则：**身份/权威字段出错 → 整次失败；纯展示字段出错 → 行内降级标记，绝不丢行**。

| 情况 | 处理 | 理由 |
|---|---|---|
| 响应非对象、`items` 缺失或非数组 | **整次失败**（固定安全提示） | 无法判断是否拿到了完整可见集合 |
| `items` 中某一项不是对象 | **整次失败** | 结构不可解释，不能把未知内容当空 |
| `id` 缺失/非 string/空/全空白 | **整次失败** | `id` 是节点身份与授权可见性的锚点；跳过会让“少了一个节点”无法与“本来无权”区分 |
| `online` 缺失/非 boolean | **整次失败** | `online` 决定指标与计数口径；猜 true/false 等于伪造状态 |
| 出现重复 `id` | **整次失败** | 同一身份出现两条互斥记录时，任选一条都会让计数和“哪条是权威”失真；Web 不替后端做选择 |
| `name` 非 string | 行保留，显示 `id` + 「名称无法识别」 | 展示字段，不影响身份；丢行反而隐藏了授权节点 |
| `name` 为空字符串 | 行保留，显示 `id` + 「名称为空」 | 同上 |
| `region`/`provider` 非 string | 该字段显示 `—` | 展示字段 |
| `cpuPercent`/`memoryPercent` 类型不合法 | 按 §3.3.1 显示 `—` + 原因 | 展示字段，不丢行 |
| 未知字段/扩展名 | 忽略 | 前向兼容；扩展名冲突的旧客户端实测属 D01 |

与后端的关系：

- 后端若拒绝坏/重复 ID（D01 待探测），Web 的“整次失败”与之同向，不提供部分成功；
- 后端若返回部分合法 + 部分非法，Web **不会**渲染成“部分列表”，而是整次失败，
  避免用户把不完整的可见集合当成完整授权视图；
- 因此页面计数只需处理“已渲染行”，不再需要 `dropped` 计数（原稿的 dropped 提案撤销）。



### 3.4 适配位置与纯函数边界

- 新增 `legacy-node.ts`（设计建议路径 `apps/web/src/features/nodes/legacy-node.ts`）：
  类型 + 纯函数 `toLegacyRows(raw: unknown): { ok: true; rows: LegacyNodeRow[] } | { ok: false; reason: "shape" | "identity" }`。
  整次失败用返回值表达（不用异常做控制流），由加载容器转成固定安全提示。
- 规则全部是纯函数，可单测，不读时钟、不读环境、不发请求。
- 任何类型守卫失败都走“整次失败”（身份/权威字段）或“行内降级”（展示字段），
  **没有 `as NodeListItemView`**，也没有把 `online` 映射成 `status` 的代码。

## 4. 加载接缝：哪些可复用、哪些必须新建

### 4.1 现状盘点（逐文件）

| 现有文件 | 与旧 DTO 的关系 | 结论 |
|---|---|---|
| `node-loader.tsx`（`NodeSource` `:22-25`、`NodeLoader` `:54`） | 状态机（loading/error/ready、key+epoch、序号、Abort）与展示类型无关；但 `NodeSource.load` 返回 `NodeListItemView[]` | **逻辑复用、类型不复用**：抽出泛型状态机；`NodeLoader` 保持行为 |
| `LOAD_FAILURE_MESSAGE`（`:9`） | 固定安全文案，无类型依赖 | 直接复用 |
| `node-list.tsx`（`COLUMNS` `:58`、行 `tabIndex` `:153`） | 列与单元格绑定 `status/platform/agentVersion/cpu/memory/lastSeen` | 不能直接渲染旧行；新建 legacy 列表组件 |
| `node-filters.ts`（`matchesQuery` `:39`） | 搜索只看 `name`/`id`，但参数类型是 `NodeListItemView` | 把参数放宽为结构类型 `{ name: string; id: string }`（向后兼容，最小改动） |
| `node-list-panel.tsx` | 状态筛选依赖 `node.status` | 首版不复用；legacy 面板只有搜索/清空 |
| `node-detail.tsx` / `node-list-workspace.tsx` | 需要富化字段与选中详情 | 首版不引入；R1-B 再接入 |
| `metric-value.tsx` | 只认 `MetricReading`（含 `sampledAt`） | 不复用；新建 legacy 指标单元格 |
| `node-status-badge.tsx` | 只认 `NodeStatus` | 不复用；新建 `OnlineBadge`（在线/离线） |
| `candidate-adapter.ts`（`:81-107`） | 富化 DTO→VM 的时效判定 | 首版不调用；legacy 无时效可判 |

### 4.2 推荐结构（最小改动）

1. **抽出泛型加载状态机**：新增 `load-state.ts`，导出
   `useLoadState<T>(source: { key: string; load(o: { signal: AbortSignal }): Promise<readonly T[]> } | null)`，
   原样保留 W05-R2 的规则（精确 key、`key+validity` 世代、序号丢弃、abort、固定安全文案）。
   `node-loader.tsx` 改为该 hook 的精简包装（对外 props/行为不变），
   **抽取后必须重跑 `apps/web/src/features/nodes/node-loader.test.tsx` 现有全部用例**
   （本分支实测 `28 passed`，命令见 HANDOFF），并重新做一次富化路径的浏览器冒烟；
   历史 W06 证据只证明抽取前的旧实现，不能自动证明新 hook 或 legacy 实现。
   这一步是“为 legacy 打开类型口子”的唯一改动，且不改变富化行为。
2. **新增 legacy 源类型**：`LegacyNodeSource { key: string; load({signal}): Promise<unknown> }`；
   适配层在 source 内部把 `unknown` 变成 `LegacyNodeRow[]`，loader 只接收已校验的行
   （或由 loader 接收 `unknown` 再校验——推荐前者，保持 loader 与 DTO 无关）。
3. **新增 `legacy-node-loader.tsx`**：`useLoadState<LegacyNodeRow>` + 固定安全错误 + 手动重试；
   失败时**不保留**任何旧行/计数。
4. **新增 `legacy-node-list.tsx` + `legacy-node-panel.tsx`**：
   复用现有卡片/表格视觉与 420px `[contain:paint]` 横滚方案；
   行不可交互（首版无详情），因此**不设 `tabIndex`/不伪造 Enter 行为**（见 §7）。
5. **`index.ts` 只追加导出**，不改既有导出语义。

不建议：复制一份 loader（会同时维护两套竞态逻辑）、给 `NodeListItemView` 加可选字段
（会让“必须有 revision”在类型层面失效）、在页面里 `as` 成富化类型。

### 4.3 兼容取舍

| 方案 | 评价 |
|---|---|
| 泛型 hook 抽取（推荐） | 一处改动，两套 VM 共用同一竞态实现；缺点是要改已接收文件，需变更评审 |
| 复制 legacy loader | 不改已接收文件；缺点是两套 epoch/abort 逻辑，后续修复要改两处（W05-R1/R2 的教训） |
| 让 `NodeListItemView` 的 `status/revision` 变可选 | 破坏富化不变量与既有测试，且让“缺状态”在类型上与“有状态”混同 |

抽取后的验证责任（不能靠“历史已验收”结案）：

| 对象 | 必须重跑 | 不能替代 |
|---|---|---|
| 富化路径（`NodeLoader` 包装） | `node-loader.test.tsx` 全部用例（实数见 HANDOFF）+ 一次浏览器冒烟（键盘/失败重试/来源切换） | W05/W06 的旧证据（旧实现） |
| 新 hook 本身 | 同键稳定、key/可用性世代、迟到成功/失败、abort、卸载 | 仅靠代码移动后的“看起来一样” |
| legacy 路径 | §3.3.1/§3.3.2 的适配矩阵 + 首提交 + 迟到 + 卸载 + 键盘/窄屏 | 富化 fixture 的任何 PASS |

## 5. 页面组成、文案与计数

### 5.1 组成

```
[标题] 节点列表
[搜索框（本地）]                     [可选：服务端 onlineOnly 选择器]
[状态行：共 N 个节点 / 显示 X / 共 N 个节点（本次已加载）]
[表格：节点 | 连接 | 地区 | 提供方 | CPU | 内存 | 说明]
[空状态 / 错误 + 重试]
```

### 5.2 列与文案

| 列 | 内容 | 缺失/非法时 |
|---|---|---|
| 节点 | `name` + 小字 `id`（相同则只显示一次） | 名称为空 → 显示 `id` + 「名称为空」；名称非 string → 显示 `id` + 「名称无法识别」（行保留） |
| 连接 | `在线` / `离线` 徽标（只表达旧 `online` 布尔） | 类型非法/缺失 → **整次失败**（§3.3.2），不出现半张表 |
| 地区 / 提供方 | 有值才显示 | `null` 或非 string → `—`（不猜） |
| CPU / 内存 | `online=true` 且数值合法：`23%`/`0%`/`100%` + 固定标记「无采样时间」；`online=false`：`—` + 「离线，无当前读数」 | `true` + absent → `—`+「无读数」；`true` + invalid → `—`+「读数非法」 |
| 说明（脚注） | 「旧接口只提供 online 布尔，不区分暂停/吊销/未知；离线或无数值时不显示历史百分比，在线数值也无采样时间，不能判断新鲜度」 | — |

**不得**出现「陈旧」「新鲜」「最近在线」「Agent 版本」「状态：暂停/吊销/未知」等首版无来源的表述；
也不把 `online=false` 写成「已离线（已确认）」——只写「离线」。

### 5.3 计数口径

- 「共 N 个节点」：**当前已加载**的可见行数（服务端已按授权可见集合与 `onlineOnly` 过滤后的结果）；
- 本地搜索后：「显示 X / 共 N 个节点（本次已加载）」；
- 身份/权威字段异常或重复 id → **整次失败**，页面不显示表格与计数（不再有 `dropped` 口径）；
- 展示字段异常（名称/地区/指标）只在单元格内降级，仍计入 N，不改变“已加载行数”的含义；
- 绝不显示服务端总数、隐藏节点数、或“共 M 个（其中 K 个无权）”这类可通过计数侧信道泄露的信息；
- 空可见列表（`items: []`）与本地无匹配必须文案不同（沿用 W02 的区分）。

### 5.4 空 / 加载 / 失败

| 状态 | 文案 | 交互 |
|---|---|---|
| 首次加载 | 「正在加载节点…」 | 搜索禁用，无计数 |
| 服务端空可见集合 | 「暂无可见节点」+「你当前没有可查看的节点，或都被过滤掉了」 | 搜索可用但无意义，保留清空 |
| 本地搜索无匹配 | 「没有匹配结果」 | 清空搜索 |
| 失败 | 按 §6.4 的固定文案（401/403/503/形状非法分别不同） | 401 由认证层处理；403 显示无权且不跳登录；503/网络显示「重新加载」；均不显示原始错误 |
| 身份/权威字段坏或重复 id | 「响应格式无法识别」 | 无表格、无计数、无重试循环（可由调用方决定是否重试） |
| `onlineOnly=true` 且过滤后为空 | 同「暂无可见节点」（**不得**提示“没有在线节点”泄露隐藏节点存在） | — |

## 6. 会话、来源与失效（B1）

### 6.1 责任划分

| 角色 | 责任 |
|---|---|
| 未来认证/会话层（未实现） | 建立/续期/撤销会话；HttpOnly Cookie；CSRF/Origin 策略；**只有它**能判定会话是否失效（401）并决定是否跳登录 |
| 调用方（未来 App 壳） | 维护 `key`；会话结束/切换主体/切换 `onlineOnly` 时**先**变 key 再渲染；401 → 清会话并递增 `sessionEpoch`；**403 → 只清本列表视图/授权视图并递增展示世代，不清会话、不跳登录**；503/网络故障 → 保留会话 |
| Web 列表组件 | 只按 key 做隔离与展示；不读 Cookie/Token；不判断权限；不把 403 当登出；不自行重试认证 |

### 6.2 key 的构成（建议）

```
key = `${sessionEpoch}:${subjectRef ?? "anonymous"}:${onlineOnly ? "online" : "all"}:${viewEpoch}`
```

- `sessionEpoch`：调用方在登录/登出/401/未来撤权信号时递增的整数，**不是凭据**；
- `subjectRef`：可公开的稳定主体标识（若未来认证层提供）；没有时用 `anonymous`，不得由客户端自报 subject 充当；
- `onlineOnly`：服务端查询参数，改变它必须改变 key，从而让旧结果在首个提交即消失。
- `viewEpoch`：调用方维护的非秘密展示世代；403/503/网络故障失效时递增并实际参与 key。它只作废本列表的旧结果，不更改认证 `sessionEpoch` 或其他能力状态。

错误处理必须先进入终止错误态并关闭该列表的加载许可，再递增 `viewEpoch`；错误视图由调用方/legacy 外层保留，加载来源禁用且不发请求，不能因 key 变化自动重新加载形成错误循环。手动重试/重新检查才重新开放加载并递增 attempt；新的可信会话/查询切换按对应流程启动新请求。递增世代只让迟到结果失效，不清除终止错误或触发自动认证。

key 只用于渲染隔离，**不被当作认证、缓存许可或跨用户隔离的证明**；服务端仍逐请求鉴权。

### 6.3 失效时序（必须由调用方保证）

| 事件 | 期望顺序 | Web 可观察结果 |
|---|---|---|
| 登出 / 会话过期（**401**，由认证层判定） | 认证层清会话 → 调用方递增 `sessionEpoch` → 触发渲染 | 首个提交即 loading；旧行/计数/详情不存在；进行中请求被 abort；是否跳登录由认证层决定 |
| **403**（会话有效但无 nodes.read） | 只清本列表授权视图并递增**展示世代**（不改 `sessionEpoch`、不清会话） | 首个提交即显示固定无权提示；旧行/计数不残留；**其他已授权能力与登录状态保持**；不跳登录、不宣称服务端登出 |
| **503 / 身份来源或策略来源暂时不可用** | 保持会话；展示世代递增以清除旧行 | 固定「服务暂时不可用」+ 手动重试；**不伪装未登录**、不跳登录 |
| 网络中断/超时 | 同 503 | 同上；恢复后手动重试 |
| 切换主体（A→B） | 换 `subjectRef` → 新 key | 同上，不显示 A 的任何残留 |
| 会话有效但来源不可用（`load` 非函数） | key 不变但可用性变化 | 按 W05-R2 规则开启新世代，首个提交清空 |
| 恢复同一 key | 来源可用性恢复可开启新请求；若已有终止错误态，须先手动重试解除阻止 | 首个提交 loading，不复用旧快照；错误后不自动请求 |
| 手动重试 | 同 key，`attempt+1` | 当帧回到 loading，不保留旧行 |
| 卸载 | 组件卸载 | abort 本任务请求；迟到响应不更新状态 |

**Web 不能自行证明撤权已生效**：没有会话/HTTPS 证据时，只能保证“不再展示旧数据”，不能保证服务端已拒绝。
跨标签页登出（BroadcastChannel 等）未定，首版不实现（§9）。

### 6.4 错误语义分层与固定文案（WD01-R01）

401 与 403 的含义不同，不能共用一个“清会话”动作：

| 状态/故障 | 语义 | Web 展示 | 会话/登录状态 | 可重试 |
|---|---|---|---|---|
| 401 | 未认证/会话失效（**只有认证层能判定**） | 固定「登录已失效，请重新登录」；列表清空 | 由认证层清会话；调用方递增 `sessionEpoch` | 由认证层决定（通常跳登录） |
| 403 | 已认证，但整体无 `nodes.read` | 固定「无权查看节点列表」；列表清空 | **保持**会话与其他已授权能力 | 手动「重新检查」；不自动重试、不跳登录 |
| 503 | 策略/库存等可信来源暂时不可用 | 固定「服务暂时不可用，请稍后重试」 | 保持 | 手动重试 |
| 身份来源故障/未配置 | 无法确认主体或策略（不是“凭据无效”） | 同 503 文案；**不得**提示重新登录 | 保持（不声称已登出） | 手动重试 |
| 400/其它 4xx | 请求本身非法，例如 `onlineOnly` 值/重复参数不被服务端接受；具体解析/码待契约批准 | 固定「请求无法完成」 | 保持 | 先修正请求，可手动重试；无自动循环 |
| 500/网络/超时 | 服务端或链路故障 | 固定「无法连接服务」/「服务暂时不可用」 | 保持 | 手动重试 |
| 响应形状非法/重复 id | 数据不可信（§3.3.2） | 固定「响应格式无法识别」 | 保持 | 由调用方决定是否重试 |

实现约束：

- 错误文案是**常量**，不拼接 `message`/body/节点 ID；不记录原始异常（可能含内部信息）。
- 403 的处理函数**不得**调用会话清理或登录跳转 API；代码评审应能看出两个分支是分开的。
- 401 的分支同样不自行“证明”服务端已登出，只把会话状态交给认证层处理。
- 任何错误态都先清空当前列表视图（不保留旧行/计数），再渲染提示。

### 6.5 不做的缓存

- 不写 `localStorage`/`sessionStorage`/IndexedDB；不建跨 key 的内存缓存；
- 不自动轮询、不后台重试、不 service worker 缓存；
- 期望服务端 `no-store`（D06/D07 提案），但 Web 不依赖它来保证隔离。

## 7. 键盘与响应式（首版与 R1-B）

### 7.1 首版（无详情）

| 元素 | 行为 |
|---|---|
| 搜索框 | Tab 可达，`type="search"`，有可见 label |
| 可选 onlineOnly 选择器 | 若首版提供，Tab 可达；改变即换 key |
| 表格行 | **不可聚焦**（没有可执行动作）。不设 `tabIndex`、不绑定 Enter/Space，避免伪造可操作行 |
| 重试按钮 | 仅错误态出现；Tab 可达、Enter/Space 原生激活 |
| 焦点可见 | 所有可聚焦元素保留 `focus-visible` 环；不因“没有详情”而移除 |

因此首版**不存在**“Enter/Space 打开详情”的路径；这一点在 ACCEPTANCE 中记为 `N/A (v1)`，
R1-B 一旦加详情，必须重新实现并验收 W04 的焦点契约（见 7.2）。

### 7.2 R1-B 迁移点（详情接入时）

- 行变为可聚焦（roving tabindex）、`aria-current` 标记选中；
- Enter/Space 打开详情，焦点移入详情（`tabIndex=-1`）；
- 关闭/Escape 回到仍存在的来源行；行被本地搜索隐藏时回到搜索框；
- 来源切换/失效时若焦点在详情内，落到稳定位置；
- 这些都复用已接收 W03/W04 的规则，但**必须用 legacy 数据重新验收**，不能把 W06 的富化 fixture 证据直接迁移。

### 7.3 响应式

- 420px：卡片在视口内，表格在卡片内横向滚动（`[contain:paint]` + `overflow-x-auto`），页面不出现横向滚动条；
- 1280px：全部列可见，长名称 `whitespace-nowrap` 不折行、不重叠；
- 长名称/长 ID 不得撑破卡片；窄屏优先保证「节点」「连接」两列可见。

## 8. 与 R1-B 的兼容与迁移

| 项 | 首版（R1-A） | R1-B 增量 |
|---|---|---|
| 行类型 | `LegacyNodeRow` | 新增富化路径；旧行类型保留为兼容读取，不作第二权威 |
| 加载 | `useLoadState<LegacyNodeRow>` | 同一 hook 的 `NodeListItemView` 实例 |
| 列表 | legacy 列（无状态/时间） | 富化列（状态徽标、陈旧标记等） |
| 详情 | 无 | `NodeDetailPanel` + 选中焦点契约 |
| 计数 | 已加载可见行 | 同规则，可加状态分组（需 D02 批准） |

迁移时不得把 legacy 行“升级”成富化行（例如给 `online=true` 补 `status=online`），
除非 D02 批准且服务端确实输出 `status`；否则两条数据路径会给出不同权威。

## 9. 未定项与假设

| 项 | 现状 | 影响 |
|---|---|---|
| 列表是否接受会话 Cookie、双凭据拒绝规则 | 候选仅 Bearer；待 D05/D06 + 契约批准 | **BLOCKED**：在此之前无法真实接线 |
| CSRF/Origin 是否适用于只读 GET | 待认证设计 | 影响请求头，不影响本设计的隔离规则 |
| 坏 `onlineOnly` 的解析与 HTTP 码 | D06 待批 | 首版只接受 boolean；非法值由调用方拒绝发请求 |
| `region/provider` 是否首版展示 | PROPOSAL 建议保留 | 可裁剪，不影响类型 |
| 跨标签页登出同步 | **待与认证设计对齐**：本设计不实现 BroadcastChannel/`pageshow`/`visibilitychange` 刷新，也不声称跨标签隔离 | 其他标签页可能短暂显示旧主体数据，直到本地请求返回 401 或用户操作；不把这条写成已完成能力 |
| 身份来源故障时的固定文案 | 待认证设计（与 401 的区分原则已定，见 §6.4） | 文案可调整，但“不提示重新登录”的边界不变 |
| 计数是否需要“全部可见/在线”双计数 | 未定 | 首版只给已加载计数 |

## 10. 引用清单

- `docs/contracts/decisions/node-read-01/OWNER-CHOICES.md`（A1/B1 选择与边界）
- `docs/contracts/decisions/node-read-01/PROPOSAL.md:54-78,79-92`（R1-A/R1-B 范围、D01–D07 建议）
- `docs/contracts/decisions/node-read-01/ACCEPTANCE.md:17-33`（AUTH/FIELD/COMPAT/WEB-01/CACHE/ERR 矩阵）
- `api/openapi/g2-merged-candidate.yaml:90-112,6639-6678`（旧列表操作与 NodeSummary）
- `api/openapi/revisions/g2-r02/merged-candidate.yaml:12905-12944`（同形旧 DTO）
- `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:9-10,20-35,41-49,193-196,209-217,217-302`
- `docs/contracts/revisions/g2-r03/DECISIONS.md:7-12`（D02–D04 OPEN）
- `apps/web/src/features/nodes/types.ts:40-50,66-78,86-89`（现有 VM 与状态联合）
- `apps/web/src/features/nodes/node-loader.tsx:9,22-25,54-69`（复用边界与世代规则）
- `apps/web/src/features/nodes/node-loader.test.tsx`（现有加载器用例，实测 28 个）
- `apps/web/src/features/nodes/node-list.tsx:58,153`（列与行焦点）
- `apps/web/src/features/nodes/node-filters.ts:39`（搜索参数类型）
- `apps/web/src/features/nodes/candidate-adapter.ts:81-107`（时效判定，首版不调用）
- `D:\admin\desktop\codex\myproject\ks\docs\development\READ-DESIGN-01-REPAIR.md:33-35`（主目录，本轮未同步；WD01-R01/R02 与澄清要求）
- `D:\admin\desktop\codex\myproject\ks\docs\development\reviews\READ-DESIGN-01-REVIEW.md`（主目录；本项两个 P2 的审查依据）
- `docs/development/reviews/FOLLOWUP-03-REVIEW.md`（W05-R2 接收）、`docs/development/reviews/W06-R1-REVIEW.md`（W06-R01/R02 CLOSED，仅合成 fixture 证据）
