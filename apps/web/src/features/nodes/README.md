# `features/nodes` · 节点列表组件（W01 / W01-R1 / W02 / W03）

只读展示节点列表的独立组件。**不接入 App**，不发起任何网络请求，不含 Agent WebSocket、
数据库或真实节点依赖；`App`/全局样式/`components/ui`/依赖清单均未改动。

## 组成

| 文件 | 职责 |
|---|---|
| `types.ts` | 局部 ViewModel 与质量类型；`NodeListState` 是 loading/error/ready 互斥联合 |
| `candidate-adapter.ts` | 候选 G2 DTO → ViewModel 的纯映射 + 指标有效性/时效判定 |
| `view-model.ts` | 状态元数据、指标展示判定、时间与计数格式化 |
| `node-list.tsx` | 组件：四状态、表格、roving tabindex 键盘导航、选中行高亮 |
| `node-filters.ts` | 搜索/状态筛选纯函数（不改输入、不排序、不访问网络） |
| `node-list-panel.tsx` | W02 包装：搜索框、状态筛选、清空，组合 `NodeList` |
| `node-detail.tsx` | W03 只读详情：状态/平台/Agent/最近在线 + CPU/内存质量 |
| `node-list-workspace.tsx` | W03 包装：保存选中 ID，组合面板与详情 |
| `metric-value.tsx` / `node-status-badge.tsx` | 列表与详情共用的指标、状态展示 |
| `test/fixtures.ts` | 合成数据与固定时钟（仅测试可用） |

## 列表选择与只读详情（W03）

`NodeListWorkspace` 组合搜索面板与详情区：

- **只保存选中 ID**：`useState<string | null>`，每次渲染都用调用方本轮的 ready 集合
  `find(id)` 解析，不缓存节点对象。
- **立即隐藏**：加载中、失败，或该 ID 已不在来源集合时，详情区直接消失；**不会**自动改选
  其他节点，也不会回退展示上一轮的数据。
- **同一 ID 更新**：重新解析到新记录（指标、状态、时间都取当前值），不会显示旧快照。
- **重新出现**：选中 ID 会保留；若该 ID 之后又出现在新集合中，详情按当前记录重新呈现。
- **与筛选的关系**：搜索/状态筛选只影响表格可见行，不改变授权范围；被筛掉的节点只要仍在
  已加载集合内，详情继续可用（隐藏不是权限动作）。
- **交互**：点击行或行上 `Enter`/`Space` 选择；详情区标题为“节点详情”（`region` 有可访问
  名称）；`关闭详情` 按钮与 `Escape` 均可关闭。非模态，无弹窗库。
- **只读**：仅展示 ViewModel 已有字段，不新增 IP/地区/凭据/连接诊断，不推断关机或认证状态；
  除关闭外没有任何操作按钮。

已知限制：详情出现时不会主动把焦点移入，也没有实时播报（未加 `aria-live`）；读屏用户需
自行导航到“节点详情”区域。


## 本地搜索与筛选（W02）

`NodeListPanel` 在 `NodeList` 之上加了三个控件，均有可访问名称：
搜索框（`搜索节点`，`type="search"`，位于命名 `search` 区域）、状态下拉（`状态筛选`）、
`清空筛选` 按钮（无输入时禁用）。

| 行为 | 约定 |
|---|---|
| 搜索范围 | 仅当前传入节点的 `name` 与 `id`，子串匹配 |
| 大小写/空白 | 首尾空白裁剪、大小写不敏感；纯空白视为不筛选 |
| 状态筛选 | `all` 或四个状态之一，选项文案与状态徽标一致 |
| 顺序 | 保持调用方传入顺序，不排序、不去重 |
| 不变性 | 返回新数组，元素引用与源数组内容都不改动（含传入冻结数组） |
| 计数 | 只描述当前已加载集合：`共 N 个节点` / `显示 X / 共 N 个节点`；加载与失败不显示计数 |
| 空状态 | 源列表为空 → `暂无节点`；有节点但筛选无命中 → `没有匹配结果`（两者互不替代） |
| 权限 | 筛选只影响本视图，**不构成权限控制**，可见性完全由上游决定 |

`NodeList` 为此新增两个可选属性，默认行为不变：`emptyContent`（替换空列表内容）与
`description={null}`（隐藏副标题，避免筛选场景出现“已授权节点接入后…”这类误导摘要）。

加载/失败时搜索与筛选控件禁用；已验收的离线、`0%`/`100%`、陈旧可见时间与键盘交互
在筛选后仍保持不变（`node-list-panel.test.tsx` 有回归用例）。


## 指标质量（W01-R1 的核心）

`MetricReading.quality` 只有四种取值，`percent` 非 null 时必为 0–100 的有限数：

| 判定 | 触发条件 | 展示 |
|---|---|---|
| `fresh` | 0–100 有限数，采样时间可解析、不晚于当前时间、年龄 ≤ 阈值 | 百分比 |
| `stale` | 同上，但年龄 > 阈值 | 百分比 **+ 可见「陈旧 · 采样于 … UTC」** |
| `invalid` | 非有限数、越界（<0 或 >100）、采样时间在未来 | `—` + 可访问原因，不显示数字 |
| `missing` | 节点非在线、未上报、缺采样时间、采样时间无法解析 | `—` + 可访问原因，不显示数字 |

规则细节：

- **越界不夹取**：`-10` / `150` 不会变成 `0%` / `100%`，也不会被静默修正。
- **零值不特殊化**：真实 `0%` 与 `100%` 是合法读数，照常显示。
- **离线不猜数**：`offline`/`suspended`/`revoked`/`unknown` 即使上游给了新鲜样本也不展示读数。
- **时效信息可见**：陈旧说明是文本节点，不使用 `title`/悬停承载，键盘与触摸同样可读。

## 时效策略必须注入

```ts
import { toNodeListItems } from "@/features/nodes";

const items = toNodeListItems(details, metricsById, {
  now: Date.now(),          // 调用方提供，测试固定
  maxSampleAgeMs: 5 * 60_000, // 由产品/契约确认，模块内没有默认值
});
```

实现中**没有任何日期或采样周期常量**：G2 候选尚未冻结指标采样周期，这里只是可注入的
展示策略。策略值非法（`maxSampleAgeMs <= 0` 或 `now` 非有限数）时退化为“一律陈旧”，
保证配置错误不会把样本当作新鲜值展示。

## 可访问性

- 区块用 `<section aria-labelledby>`，表格可访问名称来自同一个 `<h2>`。
- 状态同时有文字标签与颜色；指标缺失原因通过 `sr-only` 文本给出。
- 行采用 roving tabindex：`ArrowUp/ArrowDown/Home/End` 移动，`Enter/Space` 激活。
- 加载用 `role="status"` 且 `aria-busy`，失败用 `role="alert"`，失败分支不渲染任何行。

## 验证

```bash
npm run check:web     # typecheck + vitest + build
npm run check:policy  # 前端 import 边界（生产不得 import test/）
```

测试固定时钟 `FIXED_NOW`（`test/fixtures.ts`），不读取机器日期。
