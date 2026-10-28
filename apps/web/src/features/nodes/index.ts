/**
 * W01 节点列表的集成入口。
 *
 * 目前不接入 App：由集成负责人在统一集成时从这里引入 `NodeList`，
 * 并用真实 Controller REST 实现 `CandidateNodesSource`、在容器层把
 * 请求结果映射成 `NodeListState`（loading / error / ready）。
 *
 * 映射时必须传入 `FreshnessPolicy`（当前时间 + 最大样本年龄）：G2 没有冻结采样周期，
 * 因此本模块不提供生产默认值，阈值由产品/契约确认后由容器注入。
 */
export { NodeList } from "./node-list";
export type { NodeListProps } from "./node-list";
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
