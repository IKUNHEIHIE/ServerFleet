/**
 * W01 · 节点列表的局部 ViewModel 类型。
 *
 * 这些类型属于本功能的局部展示层，不是公共契约：G2 未冻结前不得被其他功能复用，
 * 也不得直接当作 Controller DTO 使用。DTO → ViewModel 的转换只发生在
 * `candidate-adapter.ts`，接口变化时替换该文件即可。
 */

/**
 * 与候选 `NodeDetail.status` 枚举同名的节点状态。
 * `unknown` 表示控制器自身也无法确认状态，界面必须按“未知”展示，不得推定在线。
 */
export type NodeStatus = "online" | "offline" | "suspended" | "revoked" | "unknown";

/**
 * 指标质量。决定单元格是否允许出现数字：
 * - `fresh`：样本新鲜有效，显示百分比；
 * - `stale`：样本存在但已超过最大样本年龄，保留数值时必须同时露出“陈旧”与采样时间；
 * - `invalid`：数值或时间不合法，不得显示成普通百分比；
 * - `missing`：上游没有可用读数，或没有可用采样时间。
 */
export type MetricQuality = "fresh" | "stale" | "invalid" | "missing";

/** 指标不可用/降级的具体原因，用于给出可读说明，避免所有异常都显示成同一个破折号而无从排查。 */
export type MetricQualityReason =
  | "node-not-online"
  | "absent"
  | "non-finite"
  | "out-of-range"
  | "no-timestamp"
  | "invalid-timestamp"
  | "future-timestamp";

/**
 * 一次百分比读数及其质量判定。
 *
 * 不变式：`percent` 非 null 时必为 0–100 的有限数，且 `quality` 只能是 `fresh` 或 `stale`。
 * 缺失、非法、未来时间一律 `percent: null`，因此展示层不可能把坏数据当成 0 或普通读数。
 */
export interface MetricReading {
  readonly quality: MetricQuality;
  /** 0–100 的有限数；null 表示不展示数值。 */
  readonly percent: number | null;
  /** 采样时间（RFC 3339 UTC）；仅 fresh/stale 保证可解析。 */
  readonly sampledAt: string | null;
  /** 不可用原因；fresh/stale 为 null。 */
  readonly reason: MetricQualityReason | null;
  /** 采样年龄毫秒（policy.now − 采样时间）；策略不可用时为 null。 */
  readonly ageMs: number | null;
}

/**
 * 采样时效策略。
 *
 * 必须由调用方显式注入，测试固定取值：G2 候选没有冻结采样周期，
 * 因此实现中不提供生产默认常量，也不把任何日期或周期写死。
 */
export interface FreshnessPolicy {
  /** 当前时间（epoch 毫秒）。 */
  readonly now: number;
  /** 最大样本年龄（毫秒）。超过即视为陈旧。 */
  readonly maxSampleAgeMs: number;
}

/** 节点列表的一行。 */
export interface NodeListItemView {
  readonly id: string;
  readonly name: string;
  /** 来自候选 DTO 的乐观并发版本号；后续条件请求（If-Match）需要，本轮不发起任何写操作。 */
  readonly revision: number;
  readonly status: NodeStatus;
  readonly platform: string | null;
  readonly agentVersion: string | null;
  /** 最近一次与 Controller 保持连接的时间（RFC 3339 UTC）。 */
  readonly lastSeenAt: string | null;
  readonly cpu: MetricReading;
  readonly memory: MetricReading;
}

/**
 * 列表的互斥状态。
 *
 * 用具名联合而不是若干布尔量，是为了让“失败却显示成功”在类型层面无法表达：
 * `error` 分支没有 `nodes`，`ready` 分支没有 `error`。
 */
export type NodeListState =
  | { readonly status: "loading" }
  | { readonly status: "error"; readonly message?: string }
  | { readonly status: "ready"; readonly nodes: readonly NodeListItemView[] };
