import type { MetricReading, NodeListItemView, NodeListState, NodeStatus } from "./types";

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

const pad = (value: number) => String(value).padStart(2, "0");

/**
 * 百分比展示。
 * 缺失（null / 非有限数）返回 null，由调用方渲染“无有效数据”，不能用 0 或空字符串顶替。
 * 不做 0–100 截断：契约越界应暴露给契约审查，而不是在展示层悄悄修正。
 */
export function formatPercent(reading: MetricReading): string | null {
  const { percent } = reading;
  if (percent === null || !Number.isFinite(percent)) return null;
  return `${Math.round(percent)}%`;
}

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
