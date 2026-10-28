/**
 * 候选 G2 Nodes DTO 的局部可替换适配层。
 *
 * 边界（重要）：
 * - G2 尚未冻结：这里只做纯映射 `DTO → ViewModel`，不实现任何网络访问，也不缓存。
 * - 候选契约当前只有 `GET /api/v1/nodes/{nodeId}`（单节点详情），尚无节点集合读取操作，
 *   因此 `CandidateNodesSource` 只是留给集成负责人的接缝，本文件不提供实现。
 * - 集成本页面时：用真实 Controller REST 实现 `CandidateNodesSource`，并在此处替换映射；
 *   调用方不得绕过本文件直接消费候选 DTO。
 */
import type { MetricReading, NodeListItemView, NodeStatus } from "./types";

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

const NO_READING: MetricReading = Object.freeze({ percent: null, sampledAt: null });

function normalisePercent(value: number | null | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function nonEmpty(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

/**
 * 把候选节点映射成列表行。
 *
 * 指标规则（对应 FR：离线节点指标必须显示无有效数据而非虚假 0%）：
 * 只有 `online` 节点才暴露读数；离线/暂停/吊销/未知一律视为无有效数据，
 * 即使上游塞来 `0` 也不会被当作真实读数显示。
 */
export function toNodeListItem(
  detail: CandidateNodeDetail,
  metrics?: CandidateNodeMetrics | null,
): NodeListItemView {
  const isOnline = detail.status === "online";
  const cpuPercent = isOnline ? normalisePercent(metrics?.cpuPercent) : null;
  const memoryPercent = isOnline ? normalisePercent(metrics?.memoryPercent) : null;
  const sampledAt = cpuPercent === null && memoryPercent === null ? null : nonEmpty(metrics?.sampledAt);

  return {
    id: detail.nodeId,
    name: nonEmpty(detail.displayName) ?? detail.nodeId,
    revision: detail.revision,
    status: detail.status,
    platform: nonEmpty(detail.platform),
    agentVersion: nonEmpty(detail.agentVersion),
    lastSeenAt: nonEmpty(detail.lastSeenAt),
    cpu: cpuPercent === null ? NO_READING : { percent: cpuPercent, sampledAt },
    memory: memoryPercent === null ? NO_READING : { percent: memoryPercent, sampledAt },
  };
}

/** 批量映射，保持输入顺序（排序策略属后续产品决策，不在本轮实现）。 */
export function toNodeListItems(
  details: readonly CandidateNodeDetail[],
  metricsById?: ReadonlyMap<string, CandidateNodeMetrics>,
): readonly NodeListItemView[] {
  return details.map(detail => toNodeListItem(detail, metricsById?.get(detail.nodeId) ?? null));
}
