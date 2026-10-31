/**
 * W01/W02/W03 节点列表的集成入口。
 *
 * 目前不接入 App：由集成负责人在统一集成时从这里引入 `NodeListWorkspace`
 * （列表 + 只读详情）或更底层的 `NodeListPanel` / `NodeList`，
 * 并用真实 Controller REST 实现 `CandidateNodesSource`、在容器层把请求结果
 * 映射成 `NodeListState`（loading / error / ready）。
 *
 * 映射时必须传入 `FreshnessPolicy`（当前时间 + 最大样本年龄）：G2 没有冻结采样周期，
 * 因此本模块不提供生产默认值，阈值由产品/契约确认后由容器注入。
 *
 * 注意：搜索与状态筛选只作用于调用方传入的节点集合，不查询后端、也不代表任何权限变更；
 * 详情只读，不提供编辑/删除/执行入口。
 */
export { NodeList } from "./node-list";
export type { NodeActivateOrigin, NodeListHandle, NodeListProps } from "./node-list";
export { NodeLoader, LOAD_FAILURE_MESSAGE } from "./node-loader";
export type { NodeLoaderProps, NodeSource } from "./node-loader";
export { NodeListPanel } from "./node-list-panel";
export type { NodeListPanelHandle, NodeListPanelProps } from "./node-list-panel";
export { NodeListWorkspace } from "./node-list-workspace";
export type { NodeListWorkspaceProps } from "./node-list-workspace";
export { NodeDetailPanel } from "./node-detail";
export type { NodeDetailPanelProps } from "./node-detail";
export { NodeStatusBadge } from "./node-status-badge";
export { MetricValue } from "./metric-value";
export type { MetricValueProps } from "./metric-value";
export {
  FILTERABLE_STATUSES,
  filterNodes,
  hasActiveFilter,
  matchesQuery,
  matchesStatus,
  normaliseQuery,
  summariseVisible,
} from "./node-filters";
export type { NodeFilterCriteria, NodeStatusFilter } from "./node-filters";
export { PERCENT_MAX, PERCENT_MIN, toNodeListItem, toNodeListItems } from "./candidate-adapter";
export type {
  CandidateNodeDetail,
  CandidateNodeMetrics,
  CandidateNodesSource,
} from "./candidate-adapter";
export { describeMetric } from "./view-model";
export type { MetricPresentation } from "./view-model";
export {
  LEGACY_FORMAT_ERROR,
} from "./legacy-types";
export type { LegacyAdaptResult, LegacyMetric, LegacyNodeRow } from "./legacy-types";
export { adaptLegacyNodes, adaptMetric } from "./legacy-adapter";
export { LegacyNodeList, filterLegacyRows, normaliseLegacyQuery } from "./legacy-node-list";
export type { LegacyNodeListProps } from "./legacy-node-list";
export { toRichNodes } from "./rich-adapter";
export { NodesPage } from "./nodes-page";
export type {
  FreshnessPolicy,
  MetricQuality,
  MetricQualityReason,
  MetricReading,
  NodeListItemView,
  NodeListState,
  NodeStatus,
} from "./types";
