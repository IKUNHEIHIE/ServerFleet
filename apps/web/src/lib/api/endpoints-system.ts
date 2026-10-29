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
