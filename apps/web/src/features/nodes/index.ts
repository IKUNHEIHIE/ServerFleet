/**
 * W01 节点列表的集成入口。
 *
 * 目前不接入 App：由集成负责人在统一集成时从这里引入 `NodeList`，
 * 并用真实 Controller REST 实现 `CandidateNodesSource`、在容器层把
 * 请求结果映射成 `NodeListState`（loading / error / ready）。
 */
export { NodeList } from "./node-list";
export type { NodeListProps } from "./node-list";
export { toNodeListItem, toNodeListItems } from "./candidate-adapter";
export type {
  CandidateNodeDetail,
  CandidateNodeMetrics,
  CandidateNodesSource,
} from "./candidate-adapter";
export type { MetricReading, NodeListItemView, NodeListState, NodeStatus } from "./types";
