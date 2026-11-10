import { apiClient } from "./client";
export interface BackupDestination { kind: "s3" | "webdav"; endpoint: string; bucket?: string; region?: string; prefix: string; credentialRef: string; credentialRevision: number }
export interface BackupPolicy { revision: number; ownerId: string; updatedAt: string; scheduledBackups: boolean; intervalHours: number; autoUpload: boolean; scheduleCleanup: boolean; localDays: number; localKeep: number; remoteDays: number; remoteKeep: number; requireVerifiedRemote: boolean; destination?: BackupDestination }
export type BackupPolicyInput = Omit<BackupPolicy, "revision" | "ownerId" | "updatedAt">;
export interface RemoteBackup { id: string; backupId: string; taskId: string; verifiedTaskId?: string; state: string; destination: BackupDestination; object: { key: string; sha256: string; bytes: number; etag?: string; versionId?: string }; errorCode?: string; updatedAt: string }
export interface CleanupCandidate { scope: string; backupId: string; remoteId?: string; sha256: string; bytes: number; localRevision: number }
export interface CleanupPlan { digest: string; policyRevision: number; expiresAt: string; candidates: readonly CleanupCandidate[]; protected: readonly { scope: string; backupId: string; reason: string }[]; approvalRequired: true }
export interface CleanupReceipt { taskId: string; planDigest: string; state: string; at: string; items: readonly { candidate: CleanupCandidate; state: string; reason?: string }[] }
export interface MaintenancePage { policy: BackupPolicy; remoteObjects: readonly RemoteBackup[]; cleanupReceipts: readonly CleanupReceipt[] }
export interface OperationTask { taskId: string; status: string; planHash: string; approvalRequired: boolean }
const backup = "/api/v1/backups";
export const backupMaintenanceApi = {
  get: (signal?: AbortSignal) => apiClient.request<MaintenancePage>(`${backup}/maintenance`, { signal }),
  configure: (expectedRevision: number, policy: BackupPolicyInput) => apiClient.request<BackupPolicy>(`${backup}/maintenance/policy`, { method: "PUT", body: { expectedRevision, policy } }),
  upload: (id: string, idempotencyKey: string) => apiClient.request<OperationTask>(`${backup}/${encodeURIComponent(id)}/remote-upload`, { method: "POST", body: { idempotencyKey } }),
  verify: (id: string, idempotencyKey: string) => apiClient.request<OperationTask>(`${backup}/remotes/${encodeURIComponent(id)}/verify`, { method: "POST", body: { idempotencyKey } }),
  protect: (id: string, expectedRevision: number, pinned: boolean, note: string) => apiClient.request<unknown>(`${backup}/${encodeURIComponent(id)}/protection`, { method: "POST", body: { expectedRevision, pinned, note } }),
  preview: () => apiClient.request<CleanupPlan>(`${backup}/maintenance/cleanup-preview`, { method: "POST", body: {} }),
  cleanup: (planDigest: string, idempotencyKey: string) => apiClient.request<OperationTask>(`${backup}/maintenance/cleanup`, { method: "POST", body: { planDigest, idempotencyKey } }),
};
export interface OperatorIncident {
  id: string; revision: number; sourceDigest: string; state: "open" | "investigating" | "review_required" | "resolved";
  source: { kind: string; id: string; ownerId: string; revision: number; state: string; taskId?: string; sessionId?: string; action?: string; nodeIds: readonly string[]; reason?: string };
  assigneeId?: string; resolvedBy?: string; createdAt: string; updatedAt: string; followupTaskIds: readonly string[];
  proposal?: { outcome: string; evidence: string; proposedBy: string; sourceDigest: string; at: string };
  events: readonly { sequence: number; action: string; actor: string; note?: string; at: string }[];
}
export type IncidentAction = "claim" | "release" | "take_over" | "note" | "propose" | "approve" | "reject" | "reopen" | "link_task";
export interface IncidentDecision { expectedRevision: number; requestId: string; action: IncidentAction; note: string; outcome?: string; followupTaskId?: string }
const incidents = "/api/v1/operations/incidents";
export const incidentsApi = {
  list: (signal?: AbortSignal) => apiClient.request<{ items: readonly OperatorIncident[] }>(incidents, { signal }),
  get: (id: string, signal?: AbortSignal) => apiClient.request<OperatorIncident>(`${incidents}/${encodeURIComponent(id)}`, { signal }),
  sync: () => apiClient.request<{ observed: number }>(`${incidents}/sync`, { method: "POST", body: {} }),
  decide: (id: string, body: IncidentDecision) => apiClient.request<OperatorIncident>(`${incidents}/${encodeURIComponent(id)}/actions`, { method: "POST", body }),
};
