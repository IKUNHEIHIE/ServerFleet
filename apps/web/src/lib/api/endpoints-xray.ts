import { apiClient } from "./client";

export interface XrayNode {
  readonly nodeId: string;
  readonly name: string;
  readonly online: boolean;
  readonly managed: boolean;
  readonly provider?: string;
  readonly region?: string;
  readonly arch?: string;
  readonly xrayDetected?: boolean;
}

export interface XrayPreview {
  readonly previewId: string;
  readonly expiresAt: string;
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly checks: readonly {
    readonly nodeId: string;
    readonly eligible: boolean;
    readonly issues?: readonly { readonly code?: string; readonly message?: string }[];
    readonly existingInstallation?: boolean;
    readonly hasPortConflict?: boolean;
  }[];
  readonly eligible: boolean;
  readonly requiresAdoptionApproval?: boolean;
  readonly planHash: string;
}

export interface XrayJobRef {
  readonly jobId: string;
  readonly taskId?: string;
  readonly accepted: boolean;
  readonly targetNodeIdsSnapshot?: readonly string[];
  readonly status: "queued" | "running" | "partial" | "succeeded" | "failed" | "cancelled" | "expired";
  readonly correlationId?: string;
}

export interface XrayInstance {
  readonly nodeId: string;
  readonly managed: boolean;
  readonly adoptionState?: string;
  readonly version?: string;
  readonly serviceStatus: "not_installed" | "running" | "stopped" | "failed" | "unknown";
  readonly architecture?: string;
  readonly configRevision?: number;
  readonly inboundCount?: number;
  readonly lastError?: string;
}

export interface XrayTemplate {
  readonly templateId: "vless-tls-vision" | "vless-reality-vision" | "custom-json";
  readonly name: string;
  readonly flow?: string;
  readonly security: "tls" | "reality" | "custom";
  readonly supportedVersions?: readonly string[];
  readonly requiredParameters?: readonly string[];
}

export interface XrayValidation {
  readonly valid: boolean;
  readonly sanitizedDiff?: string;
  readonly supported?: boolean;
  readonly notes?: readonly string[];
  readonly perNode?: readonly { readonly nodeId: string; readonly eligible: boolean; readonly issues?: readonly { readonly message?: string }[] }[];
}

export interface XrayDeployPreview {
  readonly previewId: string;
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly expiresAt?: string;
  readonly sanitizedDiff?: string;
  readonly checks?: readonly { readonly nodeId: string; readonly eligible: boolean; readonly issues?: readonly { readonly message?: string }[] }[];
  readonly planHash: string;
  readonly valid: boolean;
}

export interface XrayConfigurationRevision {
  readonly revision: number;
  readonly nodeId: string;
  readonly configHash: string;
  readonly summary?: string;
  readonly createdAt: string;
  readonly createdBy?: string;
  readonly status: "active" | "archived" | "failed" | "restored";
  readonly sanitizedDiff?: string;
  readonly templateId?: string;
}

export interface XrayManagedUser {
  readonly userId: string;
  readonly nodeId: string;
  readonly inboundTag: string;
  readonly displayName: string;
  readonly statsIdentifier: string;
  readonly credentialRef?: string;
  readonly flow?: string;
  readonly enabled: boolean;
  readonly createdAt?: string;
  readonly totalUplinkBytes?: number;
  readonly totalDownlinkBytes?: number;
}

export interface XrayTrafficSample {
  readonly nodeId: string;
  readonly inboundTag?: string;
  readonly userId?: string;
  readonly scope: "node" | "inbound" | "user";
  readonly uplinkBytes: number;
  readonly downlinkBytes: number;
  readonly sampledAt: string;
  readonly complete: boolean;
  readonly gapReason?: string;
  readonly source: "xray-stats";
  readonly billingEquivalent?: false;
}

export interface XrayTrafficSeries {
  readonly items: readonly XrayTrafficSample[];
  readonly nextCursor?: string;
  readonly sourceType: "proxy_only";
  readonly notice?: string;
}

export interface XrayTrafficSettings {
  readonly samplingIntervalSeconds: number;
  readonly enableUserStats?: boolean;
  readonly enableInboundStats?: boolean;
  readonly enableOutboundStats?: boolean;
  readonly thresholdsBytes?: readonly number[];
}

export interface XrayClientExport {
  readonly format: string;
  readonly oneTimeValue: string;
  readonly expiresAt: string;
}

export const xrayApi = {
  capabilities: (signal?: AbortSignal) =>
    apiClient.request<Record<string, unknown>>("/api/v1/plugins/xray-manager/capabilities", { signal }),
  nodes: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly XrayNode[] }>("/api/v1/plugins/xray-manager/nodes", { signal }),
  instance: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<XrayInstance>(`/api/v1/plugins/xray-manager/nodes/${encodeURIComponent(nodeId)}/instance`, { signal }),
  instanceAction: (nodeId: string, body: { action: "start" | "stop" | "restart" | "update" | "uninstall"; targetVersion?: string; confirmRemoveNodeXray?: boolean; keepConfig?: boolean }) =>
    apiClient.request<XrayJobRef>(`/api/v1/plugins/xray-manager/nodes/${encodeURIComponent(nodeId)}/instance/actions`, { method: "POST", body }),
  templates: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly XrayTemplate[] }>("/api/v1/plugins/xray-manager/templates", { signal }),
  previewInstall: (body: {
    targets: { nodeIds: readonly string[] };
    targetVersion: string;
    manageExisting?: boolean;
    port?: number;
    batchPolicy: { mode: "normal" | "batch" | "canary"; batchSize?: number; concurrency: number; maxFailures: number; stopOnHealthFailure?: boolean };
  }) => apiClient.request<XrayPreview>("/api/v1/plugins/xray-manager/installations/preview", { method: "POST", body }),
  install: (body: { previewId: string; planHash: string; confirmAdoption?: boolean; approvalId?: string }) =>
    apiClient.request<XrayJobRef>("/api/v1/plugins/xray-manager/installations", { method: "POST", body }),
  validate: (body: { targetNodeIds: readonly string[]; document: Record<string, unknown>; targetXrayVersion: string }) =>
    apiClient.request<XrayValidation>("/api/v1/plugins/xray-manager/configurations/validate", { method: "POST", body }),
  deployPreview: (body: {
    targets: { nodeIds: readonly string[] };
    document: Record<string, unknown>;
    batchPolicy: { mode: "normal" | "batch" | "canary"; concurrency: number; maxFailures: number };
    expectedRevisionByNode?: Record<string, number>;
  }) => apiClient.request<XrayDeployPreview>("/api/v1/plugins/xray-manager/deployments/preview", { method: "POST", body }),
  deploy: (body: { previewId: string; planHash: string; approvalId?: string }) =>
    apiClient.request<XrayJobRef>("/api/v1/plugins/xray-manager/deployments", { method: "POST", body }),
  revisions: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<{ items: readonly XrayConfigurationRevision[] }>(
      `/api/v1/plugins/xray-manager/nodes/${encodeURIComponent(nodeId)}/configurations`,
      { signal },
    ),
  rollback: (nodeId: string, body: { revision: number; expectedCurrentRevision: number; approvalId?: string }) =>
    apiClient.request<XrayJobRef>(`/api/v1/plugins/xray-manager/nodes/${encodeURIComponent(nodeId)}/configurations/rollback`, { method: "POST", body }),
  users: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly XrayManagedUser[] }>("/api/v1/plugins/xray-manager/users", { signal }),
  createUser: (body: { nodeId: string; inboundTag: string; displayName: string; flow?: "xtls-rprx-vision"; generateCredential?: boolean }) =>
    apiClient.request<XrayManagedUser>("/api/v1/plugins/xray-manager/users", { method: "POST", body }),
  patchUser: (userId: string, body: { displayName?: string; enabled?: boolean; rotateCredential?: boolean; expectedRevision?: number }) =>
    apiClient.request<XrayManagedUser>(`/api/v1/plugins/xray-manager/users/${encodeURIComponent(userId)}`, { method: "PATCH", body }),
  deleteUser: (userId: string) =>
    apiClient.request<XrayJobRef>(`/api/v1/plugins/xray-manager/users/${encodeURIComponent(userId)}`, { method: "DELETE" }),
  exportClient: (userId: string, body: { format: "vless-uri" | "qrcode" | "json"; approvalId?: string }) =>
    apiClient.request<XrayClientExport>(`/api/v1/plugins/xray-manager/users/${encodeURIComponent(userId)}/client-export`, { method: "POST", body }),
  traffic: (signal?: AbortSignal) =>
    apiClient.request<XrayTrafficSeries>("/api/v1/plugins/xray-manager/traffic", { signal }),
  trafficSettings: (signal?: AbortSignal) =>
    apiClient.request<XrayTrafficSettings>("/api/v1/plugins/xray-manager/traffic/settings", { signal }),
  putTrafficSettings: (body: XrayTrafficSettings) =>
    apiClient.request<XrayTrafficSettings>("/api/v1/plugins/xray-manager/traffic/settings", { method: "PUT", body }),
  job: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<XrayJobRef>(`/api/v1/plugins/xray-manager/jobs/${encodeURIComponent(jobId)}`, { signal }),
};
