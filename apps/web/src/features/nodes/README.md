# `features/nodes` · 节点列表组件（W01 / W01-R1）

只读展示节点列表的独立组件。**不接入 App**，不发起任何网络请求，不含 Agent WebSocket、
数据库或真实节点依赖；`App`/全局样式/`components/ui`/依赖清单均未改动。

## 组成

| 文件 | 职责 |
|---|---|
| `types.ts` | 局部 ViewModel 与质量类型；`NodeListState` 是 loading/error/ready 互斥联合 |
| `candidate-adapter.ts` | 候选 G2 DTO → ViewModel 的纯映射 + 指标有效性/时效判定 |
| `view-model.ts` | 状态元数据、指标展示判定、时间与计数格式化 |
| `node-list.tsx` | 组件：四状态、表格、roving tabindex 键盘导航 |
| `test/fixtures.ts` | 合成数据与固定时钟（仅测试可用） |

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
