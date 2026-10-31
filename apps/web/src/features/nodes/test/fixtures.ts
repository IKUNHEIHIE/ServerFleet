/**
 * W01-R1 合成测试数据（仅测试可用）。
 *
 * 规则：
 * - 全部为合成节点，不含真实主机、凭据或 Controller 响应；
 * - 生产代码不得 import 本目录（`npm run check:policy` 的前端 import 边界会失败）；
 * - 时间与时效阈值在这里固定写死，测试不读取机器日期；生产实现中没有任何默认阈值。
 * - 刻意保留的反例：离线节点仍带 cpu=0，验证 UI 不把坏数据/缺数据渲染成 0%。
 */
import type { CandidateNodeDetail, CandidateNodeMetrics } from "../candidate-adapter";
import { toNodeListItems } from "../candidate-adapter";
import type { FreshnessPolicy, NodeListState } from "../types";

/** 固定“当前时间”：2026-10-29T03:00:00Z，测试不得依赖机器时钟。 */
export const FIXED_NOW = Date.parse("2026-10-29T03:00:00Z");

/** 固定最大样本年龄：5 分钟。属可注入的展示策略，不是 G2 冻结常量。 */
export const MAX_SAMPLE_AGE_MS = 5 * 60 * 1000;

export const freshnessPolicy: FreshnessPolicy = { now: FIXED_NOW, maxSampleAgeMs: MAX_SAMPLE_AGE_MS };

export const onlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-online-01",
  displayName: "aws-tokyo-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:59:50Z",
  agentVersion: "v0.1.0",
  platform: "Ubuntu 24.04",
  revision: 3,
};

/** CPU 真实读数为 0 的在线节点：必须显示 0%，不能与“无数据”混同。 */
export const idleOnlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-online-02",
  displayName: "gcp-singapore-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:59:10Z",
  agentVersion: "v0.1.0",
  platform: "Debian 13",
  revision: 1,
};

/** 满负载节点：100% 是合法读数，也必须原样显示。 */
export const fullOnlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-online-03",
  displayName: "gcp-frankfurt-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:59:55Z",
  agentVersion: "v0.1.0",
  platform: "Debian 12",
  revision: 2,
};

/** 在线但样本已 19 分钟（> 5 分钟阈值）：数值保留，但必须露出“陈旧”与采样时间。 */
export const staleNodeDetail: CandidateNodeDetail = {
  nodeId: "node-stale-01",
  displayName: "legacy-berlin-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:41:00Z",
  agentVersion: "v0.1.0",
  platform: "Ubuntu 22.04",
  revision: 5,
};

/** 刻意携带 cpu=0 / memory=0 的离线节点，用于验证 0 不被当作有效读数。 */
export const offlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-offline-01",
  displayName: "azure-us-01",
  status: "offline",
  lastSeenAt: "2026-10-26T11:05:00Z",
  agentVersion: "v0.0.9",
  platform: "Ubuntu 22.04",
  revision: 7,
};

/** 暂停节点：即便上游给了新鲜样本，也不作为当前读数展示。 */
export const suspendedNodeDetail: CandidateNodeDetail = {
  nodeId: "node-suspended-01",
  displayName: "gcp-osaka-01",
  status: "suspended",
  lastSeenAt: "2026-10-28T19:12:00Z",
  agentVersion: "v0.1.0",
  platform: "Debian 12",
  revision: 2,
};

export const revokedNodeDetail: CandidateNodeDetail = {
  nodeId: "node-revoked-01",
  displayName: "legacy-revoked-01",
  status: "revoked",
  lastSeenAt: null,
  agentVersion: null,
  platform: "Debian 11",
  revision: 9,
};

/** 元数据与指标都缺失的未知节点：用于覆盖“未知”与“无有效数据”两种兜底。 */
export const unknownNodeDetail: CandidateNodeDetail = {
  nodeId: "node-unknown-07",
  displayName: "",
  status: "unknown",
  revision: 1,
};

export const nodeMetrics: ReadonlyMap<string, CandidateNodeMetrics> = new Map([
  ["node-online-01", { cpuPercent: 18.4, memoryPercent: 46, sampledAt: "2026-10-29T02:59:30Z" }],
  ["node-online-02", { cpuPercent: 0, memoryPercent: 58.6, sampledAt: "2026-10-29T02:59:00Z" }],
  ["node-online-03", { cpuPercent: 100, memoryPercent: 12, sampledAt: "2026-10-29T02:59:45Z" }],
  ["node-stale-01", { cpuPercent: 55, memoryPercent: 70, sampledAt: "2026-10-29T02:41:00Z" }],
  ["node-offline-01", { cpuPercent: 0, memoryPercent: 0, sampledAt: "2026-10-26T11:05:00Z" }],
  ["node-suspended-01", { cpuPercent: 12, memoryPercent: 29, sampledAt: "2026-10-29T02:58:00Z" }],
]);

export const allNodeDetails: readonly CandidateNodeDetail[] = [
  onlineNodeDetail,
  idleOnlineNodeDetail,
  fullOnlineNodeDetail,
  staleNodeDetail,
  offlineNodeDetail,
  suspendedNodeDetail,
  revokedNodeDetail,
  unknownNodeDetail,
];

export const readyNodes = toNodeListItems(allNodeDetails, nodeMetrics, freshnessPolicy);

/** 用任意合成节点拼一个就绪状态，便于组件负向测试（越界、缺时间等）。 */
export function buildReadyState(
  details: readonly CandidateNodeDetail[],
  metrics: ReadonlyMap<string, CandidateNodeMetrics> = new Map(),
): NodeListState {
  return { status: "ready", nodes: toNodeListItems(details, metrics, freshnessPolicy) };
}

export const loadingState: NodeListState = { status: "loading" };
export const failedState: NodeListState = { status: "error", message: "上游返回 503，列表未加载" };
export const emptyState: NodeListState = { status: "ready", nodes: [] };
export const readyState: NodeListState = { status: "ready", nodes: readyNodes };
