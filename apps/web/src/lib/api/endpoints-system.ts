import { apiClient } from "./client";
import type { AsyncOperation, ControllerUpdateJob, ControllerUpdatePlan, RecoveryPlan, UpdateCheckResult } from "./dto";

/** 主控更新与灾难恢复；Agent 升级没有专用端点，只能通过批量任务表达。 */
export const updatesApi = {
  check: (body: { channel: "stable" | "beta" }, idempotencyKey?: string) =>
    apiClient.request<UpdateCheckResult>("/api/v1/system/updates/check", { method: "POST", body, idempotencyKey }),
  plan: (body: { targetVersion: string; dryRun?: boolean }, idempotencyKey?: string) =>
    apiClient.request<ControllerUpdatePlan>("/api/v1/system/updates/plan", { method: "POST", body, idempotencyKey }),
  apply: (body: { planId: string; confirmation: string; backupMode: "required" }, idempotencyKey?: string) =>
    apiClient.request<ControllerUpdateJob>("/api/v1/system/updates/apply", { method: "POST", body, idempotencyKey }),
  jobs: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly ControllerUpdateJob[] }>("/api/v1/system/updates/jobs", { signal }),
  job: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<ControllerUpdateJob>(`/api/v1/system/updates/jobs/${encodeURIComponent(jobId)}`, { signal }),
  rollback: (jobId: string, body: { reason: string }, idempotencyKey?: string) =>
    apiClient.request<ControllerUpdateJob>(`/api/v1/system/updates/jobs/${encodeURIComponent(jobId)}/rollback`, { method: "POST", body, idempotencyKey }),
};

export const recoveryApi = {
  verifyBackup: (backupId: string, idempotencyKey?: string) =>
    apiClient.request<RecoveryPlan>(`/api/v1/backups/${encodeURIComponent(backupId)}/verify`, { method: "POST", idempotencyKey }),
  preview: (body: RecoveryPlan, idempotencyKey?: string) =>
    apiClient.request<RecoveryPlan>("/api/v1/disaster-recovery/preview", { method: "POST", body, idempotencyKey }),
  createJob: (body: RecoveryPlan, idempotencyKey?: string) =>
    apiClient.request<AsyncOperation>("/api/v1/disaster-recovery/jobs", { method: "POST", body, idempotencyKey }),
  job: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<AsyncOperation>(`/api/v1/disaster-recovery/jobs/${encodeURIComponent(jobId)}`, { signal }),
};

export interface EncryptedBackupRecord {
  readonly id: string;
  readonly taskId: string;
  readonly kind: "full" | "key_challenge";
  readonly status: "preparing" | "ready" | "unknown";
  readonly sha256?: string;
  readonly bytes: number;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly recipientFingerprint: string;
  readonly remoteStatus: string;
  readonly errorCode?: string;
}
export interface EncryptedBackupPage {
  readonly items: readonly EncryptedBackupRecord[];
  readonly recipientFingerprint: string;
  readonly recoveryKeyConfirmed: boolean;
 readonly automaticBackupsConfigured: boolean;
}
export interface BackupTaskAdmission {
  readonly taskId: string;
  readonly status: string;
  readonly targetType: "controller";
  readonly controllerId: string;
  readonly planHash: string;
  readonly approvalRequired: boolean;
}
export const encryptedBackupsApi = {
  list: (signal?: AbortSignal) => apiClient.request<EncryptedBackupPage>("/api/v1/backups", { signal }),
  create: (idempotencyKey: string) => apiClient.request<BackupTaskAdmission>("/api/v1/backups", { method: "POST", body: { idempotencyKey } }),
  challenge: (idempotencyKey: string) => apiClient.request<BackupTaskAdmission>("/api/v1/backups/key-challenges", { method: "POST", body: { idempotencyKey } }),
  confirm: (challengeId: string, proofSha256: string) => apiClient.request<{ confirmed: boolean }>("/api/v1/backups/key-confirmation", { method: "POST", body: { challengeId, proofSha256 } }),
};

export interface TrustedReleaseSource {
  readonly version: string;
  readonly sourceSha256: string;
  readonly host: string;
  readonly acquisitionIsInstallation: false;
}
export const releaseSourceApi = {
  get: (signal?: AbortSignal) => apiClient.request<TrustedReleaseSource>("/api/v1/system/updates/source", { signal }),
  acquire: (idempotencyKey: string) => apiClient.request<BackupTaskAdmission>("/api/v1/system/updates/acquire", { method: "POST", body: { idempotencyKey } }),
};
