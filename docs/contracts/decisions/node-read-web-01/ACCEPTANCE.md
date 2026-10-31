# 首条只读节点列表 · Web 验收计划（A1 + B1）

状态：**PROPOSED**（计划，未执行）。任务 READ-WEB-DESIGN-01 / 会话 gemini-01；审查人总控。
基线 `4196b2e8ab9c389ae0830bf56f905d9bafa00d05`。本文件不声称任何真实 HTTP/认证/浏览器已验收。

## 1. 证据等级

沿用 `docs/contracts/decisions/node-read-01/ACCEPTANCE.md:7-15` 的等级，并补充两项：

| 标记 | 含义 |
|---|---|
| `OFFLINE-UNIT` / `STATIC-CONTRACT` | 已有定义，见上引用 |
| `COMPONENT` | jsdom 组件测试；无真实网络/浏览器 |
| `BROWSER` | 真实浏览器 + CDP 真实按键（本文件新增） |
| `HTTP` | 真实 Controller httptest/真实服务（本文件新增） |
| `NONE` / `BLOCKED` | 无证据 / 依赖未批准决定 |

## 2. 历史证据边界（不能迁移）

| 历史工件 | 证明什么 | 不能证明什么 |
|---|---|---|
| W01–W06（富化 `NodeListItemView` fixture） | 富化列表/详情/焦点/竞态在合成数据上的行为 | **legacy 行类型**、旧 DTO 缺字段时的展示、旧 `online` 语义 |
| W05-R2 复审（`docs/development/reviews/FOLLOWUP-03-REVIEW.md`） | 隔离容器的世代/迟到/卸载规则 | 类型泛化后的行为（尚未实现） |
| W06-R1 复审（`docs/development/reviews/W06-R1-REVIEW.md`） | 合成 fixture 下的键盘/来源切换/迟到失败/卸载 | 真实 Cookie 会话、真实 HTTP、跨主体隔离 |

结论：本计划的每一行都以“legacy VM + 新接缝”为实现前提；历史 PASS 只作为**规则来源**，不作为本项 PASS。

## 3. 验收矩阵

| ID | 输入 / 时序 | 可观察结果 | 未来验证层次 | 当前状态 |
|---|---|---|---|---|
| WDTO-01 | 响应 `{items:[{id,name,online}]}` 合法；另测 `id` 缺失/非 string/空白、`online` 非 boolean、`name` 非 string | 合法行按服务端顺序渲染；`id`/`online` 异常 → **整次失败**（无表格、无计数、不显示部分列表）；`name` 非 string → 行保留并显示 `id` + 「名称无法识别」；不回显原始字段值 | COMPONENT（纯函数 + 组件） | NONE |
| WDTO-02 | 含 `region/provider`、空串、`countryCode/ip/tags`、未知扩展字段 | `region/provider` 空串→`—`；扩展字段被忽略且不报错；未知字段不导致整次失败 | COMPONENT | NONE |
| WDTO-03 | `online=true` × {0, 100, 1–99, null, 缺失, NaN, Infinity, -1, 101}；`online=false` × 同一组值 | `true`：`0`/`100`/普通值显示数值 + 「无采样时间」；`null`/缺失→`—`+「无读数」；`NaN`/`Infinity`/越界→`—`+「读数非法」，不夹取。`false`：**任何数值都不显示百分比**，统一 `—` + 「离线，无当前读数」；行本身仍展示 | COMPONENT | NONE |
| WDTO-04 | 任意旧响应（含 r03 新字段出现） | 类型层不存在 `status/revision/platform/agentVersion/lastSeenAt`；DOM 不出现「暂停/吊销/未知」「revision」「最近在线」；源码中无 `as NodeListItemView` 或 `online→status` 映射 | COMPONENT + 静态检查 | NONE |
| WDTO-05 | `items` 非数组/缺项、`id` 缺失/非 string/空白、`online` 非 boolean、重复 `id`；另测 `name` 非 string、`region` 非 string | 身份/权威字段异常或重复 id → **整次失败**（「响应格式无法识别」），无表格/无计数/无部分列表；`name`/`region`/指标等展示字段异常 → 行保留、单元格降级标记，仍计入计数 | COMPONENT | NONE |
| WCOUNT-01 | 8 条可见行（其中 2 条 `name` 非 string），本地搜索命中 3 条 | 初始「共 8 个节点」（2 条降级行仍计入）；搜索后「显示 3 / 共 8 个节点（本次已加载）」；任何情况下不出现服务端总数或隐藏节点计数；若出现重复 id 则整次失败、无计数 | COMPONENT | NONE |
| WCOUNT-02 | 服务端 `items: []`（授权可见为空） | 显示「暂无可见节点」，不显示「0 个节点」以外的推断，不提示“没有在线节点”；无表格、无计数摘要 | COMPONENT | NONE |
| WCOUNT-03 | 本地搜索无命中 | 显示「没有匹配结果」，与 WCOUNT-02 文案不同；清空后恢复 | COMPONENT | NONE |
| WONLINE-01 | `onlineOnly` 省略/false/true 三次请求（同会话） | 每次改变参数都换 key：首个提交即 loading，不显示上一参数的行/计数；`true` 结果只含 `online=true` | COMPONENT（key 驱动）；HTTP 待接 | NONE |
| WONLINE-02 | `onlineOnly=true` 且可见集合全为离线 | 200 `items:[]` → 「暂无可见节点」，不出现“没有在线节点”这类可推断隐藏节点存在的文案 | COMPONENT | NONE |
| WSES-01 | A 已 ready（8 行，打开搜索框）→ 换 key（模拟主体 A→B） | **布局阶段**首个提交即 loading；不含 A 的节点、计数、详情；请求 abort | COMPONENT（布局观察器）+ BROWSER | NONE |
| WSES-02 | A 已 ready → 模拟 401/登出：调用方递增 `sessionEpoch` | 同 WSES-01；焦点落到搜索框或列表区域；不保留 A 的任何 DOM | COMPONENT + BROWSER | NONE |
| WSES-03 | 同 key 但 `load` 变为非函数 → 恢复有效来源 | 首个提交 loading，旧行不复活（沿用 W05-R2 规则） | COMPONENT | NONE（规则来自已接收 W05-R2） |
| WSES-04 | 失败态点击「重新加载」 | 当帧 loading；无旧行/计数；新请求成功后替换；失败仍固定文案 | COMPONENT | NONE |
| WSES-05 | 请求 pending 时卸载组件 | 请求 signal `aborted=true`；迟到 settle 不产生渲染、无未处理拒绝 | COMPONENT | NONE（规则来自 W05-R2） |
| WRACE-01 | A pending → 切 B → B 成功 → A 迟到成功 | 仍显示 B；A 的 signal aborted；不抢焦点 | COMPONENT + BROWSER | NONE |
| WRACE-02 | 同上但 A 迟到失败 | 无错误态、无 alert；B 结果保持 | COMPONENT + BROWSER | NONE |
| WRACE-03 | B pending → 失效（无效来源）→ 恢复 B → 旧 B 迟到成功/失败 | 保持 loading，不被旧结果覆盖 | COMPONENT | NONE |
| WERR-01 | 401 / 403 / 503 / 身份来源故障 / 网络错误 / 形状非法 | 分别显示固定文案（「登录已失效，请重新登录」/「无权查看节点列表」/「服务暂时不可用，请稍后重试」/同 503 且**不得**提示重新登录/「无法连接服务」/「响应格式无法识别」）；DOM 不含原始 body/异常文本/节点 ID；每种错误态都不保留旧行/计数 | COMPONENT | NONE |
| WERR-02 | 响应 `items` 非数组、或身份/权威字段异常、或重复 id | **整次失败**，不渲染任何行/计数（不提供“部分成功”）；展示字段异常另行按 WDTO-05 行内降级 | COMPONENT | NONE |
| WERR-03 | 有效会话收到 403（整体无 `nodes.read`） | 只清本列表视图 + 固定无权提示；**不调用会话清理、不跳登录、不递增 `sessionEpoch`**；其他已授权能力的状态保持不变（用假的 auth store 观察未被调用） | COMPONENT | NONE |
| WSES-06 | 有效会话 + 403 后恢复授权（同一会话，权限被重新授予） | 手动「重新检查」后重新请求；成功则显示新列表；期间会话/登录状态未被清空 | COMPONENT | NONE |
| WSES-07 | 有效会话 + 身份来源/策略来源不可用（503） | 固定「服务暂时不可用」，**不提示重新登录**、不清会话；来源恢复后手动重试成功 | COMPONENT（HTTP 层次待接） | BLOCKED D05/D06 |
| WSES-08 | A pending → 403/503 失效，递增实际参与 key 的 viewEpoch → A 迟到 settle；保持多次渲染后手动重试 | 当帧无旧行/计数，保持固定终止错误；迟到结果不能覆盖；未手动重试前额外请求数为 0；手动重试恰好新发一次，不清会话、不改 sessionEpoch | COMPONENT（布局观察与调用次数） | NONE |
| WCACHE-01 | 渲染、搜索、切换 key、卸载全过程 | 无 `localStorage/sessionStorage/IndexedDB` 写入；无跨 key 复用；无自动轮询/后台重试 | COMPONENT + BROWSER（storage 事件） | NONE |
| WCOMPAT-01 | 抽取 `useLoadState<T>` 后：富化 `NodeLoader` 行为不变 | 重跑 `node-loader.test.tsx` 全部用例（本分支实测 **28** 个，不是旧稿写的 26）并全绿；再做一次富化路径浏览器冒烟（键盘/失败重试/来源切换）；新增 legacy 用例独立通过。历史 W06 证据只覆盖抽取前实现，不能替代 | COMPONENT + BROWSER | NONE |
| WKB-01 | 键盘从头 Tab | 顺序：搜索框 →（若启用）onlineOnly 选择器 → 错误态时的「重新加载」；表格行**不可聚焦**（无动作）；焦点环可见；Enter/Space 在非按钮行上无动作（`N/A (v1)`：首版无详情，不伪造可操作行） | BROWSER（CDP 真实按键） | NONE |
| WKB-02 | R1-B：行可聚焦后 Enter/Space 打开详情、Esc 返回 | 焦点移入详情、关闭返回来源行、被筛掉时回搜索框（复用 W03/W04 规则，但需用 legacy 数据重测） | BROWSER | N/A（v1 无详情） |
| WRESP-01 | 420×900 与 1280×900 | 420：卡片在视口内、页面 `scrollWidth == innerWidth`、表格在卡片内横滚、长名称不折行/不重叠；1280：全列可见、无重叠 | BROWSER（真实截图） | NONE |
| WHTTP-01 | 真实 Controller：未认证、空可见集合、`onlineOnly` 三态、隐藏节点在线 | 401 无 items；空集合 200 `{items:[]}` 且预检未被跳过；`onlineOnly=true` 不返回隐藏节点；响应 `no-store` | HTTP（需认证/会话实现） | BLOCKED D05/D06 |

### 3.1 本轮返修对应的覆盖（WD01-R01/R02）

| 返修项 | 原问题 | 本计划的覆盖 |
|---|---|---|
| WD01-R01 | 401 与 403 混写“清会话/跳登录” | WERR-01（401/403/503 分层文案）、WERR-03（403 不清会话）、WSES-06（403 后恢复授权）、WSES-07（身份来源故障 → 503 语义） |
| WD01-R02 | `online=false` 的历史百分比被显示且无更新时间 | WDTO-03（online × 数值交叉矩阵）、WDTO-05（身份异常整次失败） |
| 澄清：重复 ID / 部分坏行 | 原稿“保留首次 + dropped”未说明与整体无部分成功的关系 | WDTO-05（身份/权威字段异常与重复 id → 整次失败）、WERR-02、WCOUNT-01 |
| 澄清：泛型抽取 | 不能凭历史证据宣布新 hook/legacy 有效 | WCOMPAT-01、§4.1（28 用例实测，非旧稿 26） |
| 澄清：跨标签范围 | 与认证设计存在差异 | §6 UNTESTED（不实现、不声称完成） |

## 4. 执行规则（未来实现后）

### 4.1 组件层（jsdom，必须是真实组件）

- 用可控 Promise（deferred）驱动 loading/成功/失败/重试，**不用 sleep 或真实计时**制造竞态；
- 首提交隔离必须用 `useLayoutEffect` 在**布局阶段**记录 DOM 文本（早于被动 effect），
  只断言“最终 DOM 没有旧行”不算通过；
- 每个负向用例都要先在未修复实现上看到失败，再证明修复后通过（变异/回退验证）；
- 现有 `node-loader.test.tsx` 共 **28** 个用例（本分支 `npx vitest run src/features/nodes/node-loader.test.tsx` 实测 `28 passed`）；
  为 legacy 改动不得删除或弱化；抽取泛型 hook 后必须整组重跑，并另做一次浏览器冒烟（WCOMPAT-01）。
- 历史 W06 的 25 个浏览器断言只对应抽取前的富化实现，不能作为新 hook 或 legacy 组件的 PASS。

### 4.2 浏览器层（CDP）

- 只监听 `127.0.0.1` 的临时静态服务；独立 `--user-data-dir`；
- 按键用 `Input.dispatchKeyEvent`：Tab/Shift+Tab/Escape/Space 用 `rawKeyDown`，**Enter 用 `keyDown` + `text:"\r"`**
  （`rawKeyDown` 不激活原生按钮，W06 已实测）；
- 每个用例记录 `document.activeElement`、页面状态快照（表格/错误/计数/详情）与截图绝对路径；
- 首提交用例同时记录布局阶段文本；仅截图不足以证明“没有短暂旧数据”。

### 4.3 HTTP 层（真实闭环，尚未具备）

- 需要：会话 Cookie 签发/校验、列表接受 Cookie 的契约批准（D05）、预检 + 可见集合 + `onlineOnly` 顺序实现（N03-A 已接收的离线规则）、错误映射（D06）；
- 在此之前 `WHTTP-01` 保持 BLOCKED，`check:web` 通过也不能写成 HTTP PASS。

## 5. 与 D01–D07 的对应

| 决定 | 本计划的关联用例 |
|---|---|
| D01 旧客户端兼容 | WDTO-01/02、WONLINE-01（旧形状与 `onlineOnly` 语义） |
| D02 状态权威 | WDTO-04（不推导五状态）、WKB-02（R1-B 后才允许状态列） |
| D03 revision | WDTO-04（类型不出现 revision） |
| D04 采样/时效 | WDTO-03（无 sampledAt 不称新鲜） |
| D05 身份/凭据 | WSES-01/02、WSES-07、WHTTP-01（BLOCKED） |
| D06 错误/防枚举 | WERR-01/02/03、WSES-06/07、WONLINE-02、WHTTP-01 |
| D07 空库存/撤权/缓存 | WCOUNT-01/02、WSES-02、WCACHE-01、WHTTP-01 |

## 6. 未验证（UNTESTED）与阻塞

- **UNTESTED**：真实 HTTP、真实 Cookie 会话、CSRF/Origin、401 语义、服务端 `no-store`；
- **UNTESTED**：跨主体隔离（A 的浏览器会话看到 B 的数据）与撤权生效时序；
- **UNTESTED**：**跨标签页/`pageshow`/`visibilitychange` 的会话同步**——与认证设计存在范围差异，
  本设计保持“不实现、不声称已完成”；其他标签页可能短暂显示旧主体数据；
- **UNTESTED**：屏幕阅读器（NVDA/JAWS/VoiceOver）、Firefox/Safari、OS 缩放/高对比/触摸；
- **BLOCKED**：列表接受 Cookie 的契约批准（D05）、错误映射（D06）、身份来源故障的真实 HTTP 语义、Controller 列表接线；
- **不适用（v1）**：Enter/Space 打开详情、详情焦点返回——R1-B 实现后重新验收；
- 本计划不产生任何新 mock/fixture 进生产，也不把 W06 的富化 fixture 结果当成 legacy PASS。
