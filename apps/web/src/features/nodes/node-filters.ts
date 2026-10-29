/**
 * W02 · 节点列表的本地搜索与状态筛选（纯函数）。
 *
 * 边界：
 * - 只过滤调用方传入的节点集合，**不是权限控制**：这里的可见性完全由上游决定，
 *   本地筛选既不授予也不收回任何权限，不能用来替代 Controller 的授权结果。
 * - 不修改输入：返回新数组，元素引用与顺序保持不变。
 * - 不读取时钟、不访问网络、不排序（排序属后续独立决策）。
 */
import type { NodeListItemView, NodeStatus } from "./types";

/** 状态筛选值：`all` 表示不按状态筛选。 */
export type NodeStatusFilter = NodeStatus | "all";

export interface NodeFilterCriteria {
  /** 原始输入（未裁剪）；纯空白视为不筛选。 */
  readonly query: string;
  readonly status: NodeStatusFilter;
}

/** 可筛选的状态，顺序即下拉框顺序（与状态徽标文案一致）。 */
export const FILTERABLE_STATUSES: readonly NodeStatus[] = [
  "online",
  "offline",
  "suspended",
  "revoked",
  "unknown",
];

/**
 * 归一化搜索词：裁剪首尾空白并转小写。
 * 大小写不敏感对 ASCII 名称有效；中文名称本就不区分大小写，逻辑一致。
 */
export function normaliseQuery(raw: string): string {
  return raw.trim().toLowerCase();
}

/** 名称或 ID 命中搜索词；空搜索词匹配全部。 */
export function matchesQuery(node: NodeListItemView, normalised: string): boolean {
  if (normalised === "") return true;
  return node.name.toLowerCase().includes(normalised) || node.id.toLowerCase().includes(normalised);
}

/** 状态命中；`all` 匹配全部。 */
export function matchesStatus(node: NodeListItemView, status: NodeStatusFilter): boolean {
  return status === "all" || node.status === status;
}

/**
 * 按条件筛选。
 * 返回新数组，元素顺序与输入一致，且不修改输入（含冻结数组）。
 */
export function filterNodes(
  nodes: readonly NodeListItemView[],
  criteria: NodeFilterCriteria,
): readonly NodeListItemView[] {
  const normalised = normaliseQuery(criteria.query);
  return nodes.filter(node => matchesQuery(node, normalised) && matchesStatus(node, criteria.status));
}

/**
 * 是否存在用户可清空的输入。
 * 用原始 query 判断：用户输入了空格也应能一键清空，即使它不改变结果集。
 */
export function hasActiveFilter(criteria: NodeFilterCriteria): boolean {
  return criteria.query !== "" || criteria.status !== "all";
}

/**
 * 结果计数文案，只描述当前已加载集合并说明筛选后还剩多少。
 * 源列表为空时返回 null：没有可计数的对象，不编造 "0 个节点" 之外的汇总。
 */
export function summariseVisible(visible: number, total: number): string | null {
  if (total <= 0) return null;
  if (visible === total) return `共 ${total} 个节点`;
  return `显示 ${visible} / 共 ${total} 个节点`;
}
