import type { NodeDetail, NodeSummary } from "@/lib/api/dto";
import { toNodeListItem } from "./candidate-adapter";
import type { NodeListItemView } from "./types";

/**
 * 将已批准列表 DTO 与详情 DTO 合并成既有富化 ViewModel。
 *
 * 只使用两端都真实返回的字段：详情提供 status/revision/platform/agentVersion/
 * lastSeenAt；列表提供 cpuPercent/memoryPercent（没有采样时间）。由于缺少
 * sampledAt，指标一律按“无采样时间”处理，不伪造新鲜读数。
 * 任一 summary 没有对应详情时返回 null，调用方必须整次失败而不是部分展示。
 */
export function toRichNodes(
  summaries: readonly NodeSummary[],
  details: readonly NodeDetail[],
): readonly NodeListItemView[] | null {
  const byId = new Map<string, NodeDetail>();
  for (const detail of details) {
    if (byId.has(detail.nodeId)) return null;
    byId.set(detail.nodeId, detail);
  }
  if (byId.size !== summaries.length) return null;

  const items: NodeListItemView[] = [];
  for (const summary of summaries) {
    const detail = byId.get(summary.id);
    if (!detail) return null;
    items.push(
      toNodeListItem(
        detail,
        { cpuPercent: summary.cpuPercent, memoryPercent: summary.memoryPercent, sampledAt: null },
        { now: Date.now(), maxSampleAgeMs: 1 },
      ),
    );
  }
  return items;
}
