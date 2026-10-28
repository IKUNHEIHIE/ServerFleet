/**
 * W01 合成测试数据（仅测试可用）。
 *
 * 规则：全部为合成节点，不含真实主机、凭据或 Controller 响应；
 * 生产代码不得 import 本目录（`npm run check:policy` 的前端 import 边界会失败）。
 * 其中“离线节点仍带 cpu=0”是刻意保留的反例：UI 必须显示无有效数据，而不是 0%。
 */
import type { CandidateNodeDetail, CandidateNodeMetrics } from "../candidate-adapter";
import { toNodeListItems } from "../candidate-adapter";
import type { NodeListState } from "../types";

export const onlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-online-01",
  displayName: "aws-tokyo-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:41:00Z",
  agentVersion: "v0.1.0",
  platform: "Ubuntu 24.04",
  revision: 3,
};

/** CPU 真实读数为 0 的在线节点：必须显示 0%，不能与“无数据”混同。 */
export const idleOnlineNodeDetail: CandidateNodeDetail = {
  nodeId: "node-online-02",
  displayName: "gcp-singapore-01",
  status: "online",
  lastSeenAt: "2026-10-29T02:40:00Z",
  agentVersion: "v0.1.0",
  platform: "Debian 13",
  revision: 1,
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

export const suspendedNodeDetail: CandidateNodeDetail = {
  nodeId: "node-suspended-01",
  displayName: "gcp-frankfurt-01",
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
  ["node-online-01", { cpuPercent: 18.4, memoryPercent: 46, sampledAt: "2026-10-29T02:41:00Z" }],
  ["node-online-02", { cpuPercent: 0, memoryPercent: 58.6, sampledAt: "2026-10-29T02:40:00Z" }],
  ["node-offline-01", { cpuPercent: 0, memoryPercent: 0, sampledAt: "2026-10-26T11:05:00Z" }],
  ["node-suspended-01", { cpuPercent: 12, memoryPercent: 29, sampledAt: "2026-10-28T19:12:00Z" }],
]);

export const readyNodes = toNodeListItems(
  [
    onlineNodeDetail,
    idleOnlineNodeDetail,
    offlineNodeDetail,
    suspendedNodeDetail,
    revokedNodeDetail,
    unknownNodeDetail,
  ],
  nodeMetrics,
);

export const loadingState: NodeListState = { status: "loading" };
export const failedState: NodeListState = { status: "error", message: "上游返回 503，列表未加载" };
export const emptyState: NodeListState = { status: "ready", nodes: [] };
export const readyState: NodeListState = { status: "ready", nodes: readyNodes };
