/**
 * 候选 G2 Nodes DTO 的局部可替换适配层。
 *
 * 边界（重要）：
 * - G2 尚未冻结：这里只做纯映射 `DTO → ViewModel`，不实现任何网络访问，也不缓存。
 * - 候选契约当前只有 `GET /api/v1/nodes/{nodeId}`（单节点详情），尚无节点集合读取操作，
 *   因此 `CandidateNodesSource` 只是留给集成负责人的接缝，本文件不提供实现。
 * - 集成本页面时：用真实 Controller REST 实现 `CandidateNodesSource`，并在此处替换映射；
 *   调用方不得绕过本文件直接消费候选 DTO。
 *
 * 指标有效性（W01-R1）：
 * - 只有 0–100 内的有限数、且采样时间可解析、不晚于当前时间、年龄不超过策略阈值的样本，
 *   才产出 `fresh`；超过阈值的产出 `stale`（保留数值，但由展示层强制露出“陈旧”与采样时间）。
 * - 越界值不夹取成 0/100，缺失时间不假定为“刚刚采样”，未来时间不当作有效当前数据。
 * - 是否新鲜取决于调用方注入的 `FreshnessPolicy`；实现中没有任何日期或采样周期常量。
 */
import type {
  FreshnessPolicy,
  MetricQualityReason,
  MetricReading,
  NodeListItemView,
  NodeStatus,
} from "./types";

/**
 * 候选 `NodeDetail` 的子集（api/openapi/g2-core-candidate.yaml）。
 * 仅声明本列表需要的字段；未知字段忽略，避免把未冻结字段带进展示层。
 */
export interface CandidateNodeDetail {
  readonly nodeId: string;
  readonly displayName: string;
  readonly status: NodeStatus;
  readonly lastSeenAt?: string | null;
  readonly agentVersion?: string | null;
  readonly platform?: string | null;
  readonly revision: number;
}

/**
 * 指标的临时本地形状。
 * 候选契约目前没有 CPU/内存读数对象，此形状只用于隔离 Mock 与 ViewModel 之间，
 * 不是契约，不得在任何文档或接口描述中当作 G2 字段。契约补齐后原地替换。
 */
export interface CandidateNodeMetrics {
  readonly cpuPercent?: number | null;
  readonly memoryPercent?: number | null;
  readonly sampledAt?: string | null;
}

/** 数据来源接缝：由集成负责人注入真实实现；本轮不提供、不调用任何实现。 */
export interface CandidateNodesSource {
  listNodes(options?: { readonly signal?: AbortSignal }): Promise<readonly CandidateNodeDetail[]>;
}

/** 局部百分比模型的下界（含）。 */
export const PERCENT_MIN = 0;
/** 局部百分比模型的上界（含）。 */
export const PERCENT_MAX = 100;

function noValue(reason: MetricQualityReason, sampledAt: string | null = null): MetricReading {
  return { quality: "missing", percent: null, sampledAt, reason, ageMs: null };
}

function invalidValue(reason: MetricQualityReason, sampledAt: string | null, ageMs: number | null): MetricReading {
  return { quality: "invalid", percent: null, sampledAt, reason, ageMs };
}

function normaliseTimestamp(value: string | null | undefined): string | null {
  const trimmed = typeof value === "string" ? value.trim() : "";
  return trimmed ? trimmed : null;
}

/**
 * 判定一条读数。
 *
 * 顺序有意如此：先排除“不值得展示”的输入，再判定时间可信度，最后才是新鲜/陈旧分级。
 * 任何一步失败都返回 `percent: null`，因此不存在“坏数据但显示百分比”的路径。
 */
function assessReading(
  raw: number | null | undefined,
  sampledAtRaw: string | null | undefined,
  policy: FreshnessPolicy,
  isOnline: boolean,
): MetricReading {
  if (!isOnline) return noValue("node-not-online");
  if (raw === null || raw === undefined) return noValue("absent");
  if (typeof raw !== "number" || !Number.isFinite(raw)) return noValue("non-finite");
  // 越界不在展示层夹取：宁可不显示，也不把错误读数改写成看似合理的 0/100。
  if (raw < PERCENT_MIN || raw > PERCENT_MAX) return invalidValue("out-of-range", null, null);

  const sampledAt = normaliseTimestamp(sampledAtRaw);
  if (!sampledAt) return noValue("no-timestamp");
  const sampledTime = Date.parse(sampledAt);
  if (Number.isNaN(sampledTime)) return noValue("invalid-timestamp");

  // 策略不可用时退化到“一律陈旧”，保证配置错误不会让样本被当成新鲜值。
  const policyUsable =
    Number.isFinite(policy.now) && Number.isFinite(policy.maxSampleAgeMs) && policy.maxSampleAgeMs > 0;
  const ageMs = policyUsable ? policy.now - sampledTime : null;

  if (ageMs !== null && ageMs < 0) return invalidValue("future-timestamp", sampledAt, ageMs);
  if (ageMs === null || ageMs > policy.maxSampleAgeMs) {
    return { quality: "stale", percent: raw, sampledAt, reason: null, ageMs };
  }
  return { quality: "fresh", percent: raw, sampledAt, reason: null, ageMs };
}

function nonEmpty(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

/**
 * 把候选节点映射成列表行。
 *
 * 指标规则：
 * - 只有 `online` 节点才可能产生读数（离线/暂停/吊销/未知一律无有效数据）；
 * - 数值必须在局部 0–100 内且有限，采样时间必须可解析、不晚于 `policy.now`；
 * - 年龄超过 `policy.maxSampleAgeMs` 的样本降级为 `stale`，由展示层带“陈旧”与时间露出。
 */
export function toNodeListItem(
  detail: CandidateNodeDetail,
  metrics: CandidateNodeMetrics | null | undefined,
  policy: FreshnessPolicy,
): NodeListItemView {
  const isOnline = detail.status === "online";

  return {
    id: detail.nodeId,
    name: nonEmpty(detail.displayName) ?? detail.nodeId,
    revision: detail.revision,
    status: detail.status,
    platform: nonEmpty(detail.platform),
    agentVersion: nonEmpty(detail.agentVersion),
    lastSeenAt: nonEmpty(detail.lastSeenAt),
    cpu: assessReading(metrics?.cpuPercent, metrics?.sampledAt, policy, isOnline),
    memory: assessReading(metrics?.memoryPercent, metrics?.sampledAt, policy, isOnline),
  };
}

/** 批量映射，保持输入顺序（排序策略属后续产品决策，不在本轮实现）。 */
export function toNodeListItems(
  details: readonly CandidateNodeDetail[],
  metricsById: ReadonlyMap<string, CandidateNodeMetrics> | undefined,
  policy: FreshnessPolicy,
): readonly NodeListItemView[] {
  return details.map(detail => toNodeListItem(detail, metricsById?.get(detail.nodeId) ?? null, policy));
}
