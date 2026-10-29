/**
 * W01/W02 节点列表的集成入口。
 *
 * 目前不接入 App：由集成负责人在统一集成时从这里引入 `NodeListPanel`
 * （或更底层的 `NodeList`），并用真实 Controller REST 实现 `CandidateNodesSource`、
 * 在容器层把请求结果映射成 `NodeListState`（loading / error / ready）。
 *
 * 映射时必须传入 `FreshnessPolicy`（当前时间 + 最大样本年龄）：G2 没有冻结采样周期，
 * 因此本模块不提供生产默认值，阈值由产品/契约确认后由容器注入。
 *
 * 注意：`NodeListPanel` 的搜索与状态筛选只作用于调用方传入的节点集合，
 * 不查询后端、也不代表任何权限变更。
 */
export { NodeList } from "./node-list";
export type { NodeListProps } from "./node-list";
export { NodeListPanel } from "./node-list-panel";
export type { NodeListPanelProps } from "./node-list-panel";
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
export type {
  FreshnessPolicy,
  MetricQuality,
  MetricQualityReason,
  MetricReading,
  NodeListItemView,
  NodeListState,
  NodeStatus,
} from "./types";
