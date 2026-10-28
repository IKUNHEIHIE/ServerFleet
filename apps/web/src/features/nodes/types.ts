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
 * 一次百分比读数。
 * 契约规则要求“数据未知时用 null，不用 0 冒充”，因此 null 与 0 在此类型中严格区分：
 * - `percent: 0` 是真实读数，显示 0%；
 * - `percent: null` 是无有效数据，显示“无有效数据”标记。
 */
export interface MetricReading {
  /** 百分比 0–100；null 表示无有效数据。 */
  readonly percent: number | null;
  /** 采样时间（RFC 3339 UTC，来自候选契约口径）；null 表示未采样。 */
  readonly sampledAt: string | null;
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
