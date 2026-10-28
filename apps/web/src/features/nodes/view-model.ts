import type {
  MetricQualityReason,
  MetricReading,
  NodeListItemView,
  NodeListState,
  NodeStatus,
} from "./types";

/**
 * 状态的展示元数据。
 * 颜色只是辅助：每个状态同时输出中文标签，避免仅靠颜色传达信息（WCAG 1.4.1）。
 */
export interface NodeStatusMeta {
  readonly label: string;
  readonly badgeClassName: string;
  readonly dotClassName: string;
}

export const nodeStatusMeta: Record<NodeStatus, NodeStatusMeta> = {
  online: {
    label: "在线",
    badgeClassName: "border-emerald-600/30 bg-emerald-50 text-emerald-700",
    dotClassName: "bg-emerald-500",
  },
  offline: {
    label: "离线",
    badgeClassName: "border-border bg-muted text-muted-foreground",
    dotClassName: "bg-muted-foreground/60",
  },
  suspended: {
    label: "已暂停",
    badgeClassName: "border-amber-600/30 bg-amber-50 text-amber-800",
    dotClassName: "bg-amber-500",
  },
  revoked: {
    label: "已吊销",
    badgeClassName: "border-destructive/40 bg-destructive/10 text-destructive",
    dotClassName: "bg-destructive",
  },
  unknown: {
    label: "未知",
    badgeClassName: "border-border bg-transparent text-muted-foreground",
    dotClassName: "border border-muted-foreground/60 bg-transparent",
  },
};

/**
 * 指标单元格的可展示结果。
 *
 * 只有 `kind: "value"` 才允许出现数字；`stale` 数值必须同时带 `note`（含“陈旧”与采样时间），
 * 由组件以可见文本渲染，而不是鼠标悬停提示。
 */
export interface MetricPresentation {
  readonly kind: "value" | "empty";
  /** kind=value 时为形如 `37%` 的文本；kind=empty 时为 null。 */
  readonly value: string | null;
  /** 陈旧样本的可见说明；新鲜样本为 null。 */
  readonly note: string | null;
  /** kind=empty 时的可读说明（同时作为读屏文本）。 */
  readonly reason: string | null;
  readonly stale: boolean;
}

const reasonText: Record<MetricQualityReason, string> = {
  "node-not-online": "无有效数据（节点不在线或状态未确认）",
  absent: "无有效数据（未上报）",
  "non-finite": "无有效数据（读数非法）",
  "out-of-range": "无有效数据（超出 0–100 范围）",
  "no-timestamp": "无有效数据（缺少采样时间）",
  "invalid-timestamp": "无有效数据（采样时间无法解析）",
  "future-timestamp": "无有效数据（采样时间晚于当前时间）",
};

const pad = (value: number) => String(value).padStart(2, "0");

/**
 * 时间展示，固定按 UTC 输出并显式带单位，避免依赖运行环境时区导致同一数据两种读数。
 * 时区呈现策略（本地时间 or UTC）尚未由产品确认，见交付说明的剩余问题。
 */
export function formatLastSeen(lastSeenAt: string | null): string | null {
  if (!lastSeenAt) return null;
  const time = new Date(lastSeenAt);
  if (Number.isNaN(time.getTime())) return null;
  return (
    `${time.getUTCFullYear()}-${pad(time.getUTCMonth() + 1)}-${pad(time.getUTCDate())} ` +
    `${pad(time.getUTCHours())}:${pad(time.getUTCMinutes())} UTC`
  );
}

/**
 * 指标展示判定。
 *
 * 缺失、非法与未来时间一律落到 `empty`，不出现任何百分比；陈旧样本保留数值，
 * 但同时返回带采样时间的可见说明，调用方不得只渲染数值。
 */
export function describeMetric(reading: MetricReading): MetricPresentation {
  if (reading.percent === null) {
    return {
      kind: "empty",
      value: null,
      note: null,
      reason: reading.reason ? reasonText[reading.reason] : "无有效数据",
      stale: false,
    };
  }

  if (reading.quality === "stale") {
    const sampled = formatLastSeen(reading.sampledAt);
    return {
      kind: "value",
      value: `${Math.round(reading.percent)}%`,
      note: sampled ? `陈旧 · 采样于 ${sampled}` : "陈旧 · 采样时间未知",
      reason: null,
      stale: true,
    };
  }

  return { kind: "value", value: `${Math.round(reading.percent)}%`, note: null, reason: null, stale: false };
}

/** 表头的数量徽标文案。失败状态不允许出现任何“已加载/成功”措辞。 */
export function countLabel(state: NodeListState): string {
  switch (state.status) {
    case "loading":
      return "加载中";
    case "error":
      return "无法加载";
    case "ready":
      return `${state.nodes.length} 个节点`;
  }
}

/** 是否存在可用的可访问名称来源：标题为空时由组件回退到默认标题。 */
export function resolveTitle(title: string | undefined): string {
  const trimmed = title?.trim();
  return trimmed ? trimmed : "节点列表";
}

/**
 * 在线/未在线摘要，用于列表副标题。
 * 总数由标题右侧徽标承担，这里不再重复，避免同一句话在卡片里说两遍。
 */
export function summariseNodes(nodes: readonly NodeListItemView[]): string {
  if (nodes.length === 0) return "已授权节点接入后会显示在这里";
  const online = nodes.filter(node => node.status === "online").length;
  return `${online} 个在线 · ${nodes.length - online} 个未在线`;
}
