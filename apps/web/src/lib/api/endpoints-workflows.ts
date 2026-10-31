import { apiClient } from "./client";
import type { WorkflowCreateInput, WorkflowRunView, WorkflowTriggerResult, WorkflowUpdateInput, WorkflowView } from "./dto";

/**
 * 自动化工作流（GM-W5-01a）：对接 Wave 4 合入的真实 Controller 路由
 * internal/workflows/http.go —— 列表/创建/详情/更新（expectedRevision 乐观锁）/手动触发。
 * 任务提交仍经唯一任务服务；本层不执行任何步骤。
 */
export const workflowsApi = {
  list: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly WorkflowView[] }>("/api/v1/workflows", { signal }),
  create: (body: WorkflowCreateInput) =>
    apiClient.request<WorkflowView>("/api/v1/workflows", { method: "POST", body }),
  get: (workflowId: string, signal?: AbortSignal) =>
    apiClient.request<{ workflow: WorkflowView; runs: readonly WorkflowRunView[] }>(
      `/api/v1/workflows/${encodeURIComponent(workflowId)}`,
      { signal },
    ),
  update: (workflowId: string, body: WorkflowUpdateInput) =>
    apiClient.request<WorkflowView>(`/api/v1/workflows/${encodeURIComponent(workflowId)}`, {
      method: "PATCH",
      body,
    }),
  // 触发运行：202 Accepted。matched=false 表示事件与触发器不匹配（服务端不会伪造运行）。
  trigger: (workflowId: string, body: { eventId: string; kind: string; values?: Readonly<Record<string, string>> }) =>
    apiClient.request<WorkflowTriggerResult>(`/api/v1/workflows/${encodeURIComponent(workflowId)}/runs`, {
      method: "POST",
      body,
    }),
};
