import { apiClient } from "./client";
import { normalizeInstalledPlugin, normalizePluginList } from "./compat";
import type {
  AIApprovalEvent,
  AIApprovalMode,
  AIApprovalPolicy,
  AIApprovalRequest,
  AIContextStats,
  AIExecuteResult,
  AIMessage,
  AIModelBudget,
  AIModelBudgetInput,
  AIModelSettings,
  AIResourceRecommendation,
  AISecurityPolicy,
  AISession,
  AISessionSummary,
  Page,
  PluginCatalogEntry,
  PluginOperation,
  PluginRepository,
} from "./dto";

export type AgentRunState = "queued" | "thinking" | "waiting_approval" | "submitting" | "waiting_task" | "canceling" | "completed" | "failed" | "canceled" | "unknown" | "limit_reached";
export interface AgentStep {
  readonly number: number;
  readonly state: AgentRunState;
  readonly toolName?: string;
  readonly nodeIds: readonly string[];
  readonly parameters?: Readonly<Record<string, string>>;
  readonly approvalId?: string;
  readonly taskId?: string;
  readonly taskState?: string;
  readonly plan?: string;
  readonly result?: string;
  readonly messageId?: string;
  readonly startedAt: string;
  readonly reasoningContent?: string;
  readonly reasoningSummary?: string;
}
export interface AgentRun {
  readonly id: string;
  readonly sessionId: string;
  readonly goal: string;
  readonly state: AgentRunState;
  readonly driverActive: boolean;
  readonly revision: number;
  readonly scopeRevision: number;
  readonly maxSteps: number;
  readonly createdAt: string;
  readonly expiresAt: string;
  readonly steps: readonly AgentStep[];
  readonly finalText?: string;
  readonly stopReason?: string;
}
export interface AgentRunInput {
  readonly goal: string;
  readonly clientRunId: string;
  readonly scopeRevision: number;
  readonly maxSteps: number;
  readonly timeoutSeconds: number;
}
const runsPath = (sessionId: string) => `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/runs`;
export const aiAgentApi = {
  list: (sessionId: string, signal?: AbortSignal) => apiClient.request<{ items: readonly AgentRun[] }>(runsPath(sessionId), { signal }),
  create: (sessionId: string, body: AgentRunInput) => apiClient.request<AgentRun>(runsPath(sessionId), { method: "POST", body }),
  get: (sessionId: string, runId: string, signal?: AbortSignal) => apiClient.request<AgentRun>(`${runsPath(sessionId)}/${encodeURIComponent(runId)}`, { signal }),
  advance: (sessionId: string, runId: string, expectedRevision: number) => apiClient.request<AgentRun>(`${runsPath(sessionId)}/${encodeURIComponent(runId)}/advance`, { method: "POST", body: { expectedRevision } }),
  continueRun:(sessionId:string,runId:string,expectedRevision:number)=>apiClient.request<AgentRun>(runsPath(sessionId)+"/"+encodeURIComponent(runId)+"/continue",{method:"POST",body:{expectedRevision,additionalSteps:8,timeoutSeconds:900}}),
  cancel: (sessionId: string, runId: string, expectedRevision: number) => apiClient.request<AgentRun>(`${runsPath(sessionId)}/${encodeURIComponent(runId)}/cancel`, { method: "POST", body: { expectedRevision } }),
};

export const aiApi = {
  probeModel: (model: string) =>
    apiClient.request<{ model: string; toolCalls: boolean; reasoning: boolean; checkedAt: string }>(
      "/api/v1/ai/model-settings/probe", { method: "POST", body: { model }, timeoutMs: 50000 },
    ),
  createSession: () => apiClient.request<AISession>("/api/v1/ai/sessions", { method: "POST", body: {} }),
  sessions: (signal?: AbortSignal) => apiClient.request<{ items: readonly AISessionSummary[] }>("/api/v1/ai/sessions", { signal }),
  getSession: (sessionId: string, signal?: AbortSignal) =>
    apiClient.request<AISession>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}`, { signal }),
  grant: (sessionId: string, body: { resourceType: "node" | "service_group"; resourceId: string; allowedActions?: readonly string[] }) =>
    apiClient.request<AISession>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/grants`, { method: "POST", body }),
  revokeGrant: (sessionId: string, grantId: string) =>
    apiClient.request<AISession>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/grants/${encodeURIComponent(grantId)}`,
      { method: "DELETE" },
    ),
  recommend: (sessionId: string, body: { reason: string; neededCapabilities: readonly string[] }) =>
    apiClient.request<{ suggestions: readonly AIResourceRecommendation[] }>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/resource-recommendations`,
      { method: "POST", body },
    ),
  decideRecommendation: (
    sessionId: string,
    recommendationId: string,
    body: { requestId: string; approved: boolean; grants: readonly { resourceType: "node" | "service_group"; resourceId: string; allowedActions: readonly string[] }[] },
  ) =>
    apiClient.request<AISession>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/resource-recommendations/${encodeURIComponent(recommendationId)}/decision`,
      { method: "POST", body },
    ),
  approvalPolicy: (sessionId: string, signal?: AbortSignal) =>
    apiClient.request<AIApprovalPolicy>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approval-policy`, { signal }),
  updateApprovalPolicy: (sessionId: string, body: { mode: AIApprovalMode }) =>
    apiClient.request<AIApprovalPolicy>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approval-policy`, { method: "PATCH", body }),
  updatePreferences: (sessionId: string, body: { model: string; reasoningEffort: "low" | "medium" | "high" | "" }) =>
    apiClient.request<AISession>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/preferences`, { method: "PATCH", body }),
  context: (sessionId: string, signal?: AbortSignal) =>
    apiClient.request<AIContextStats>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/context`, { signal }),
  compactContext: (sessionId: string, body: { keep: number }) =>
    apiClient.request<AIContextStats>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/context/compact`, { method: "POST", body }),
  createApproval: (
    sessionId: string,
    body: { toolName: string; arguments: Record<string, unknown>; targetNodeIds: readonly string[]; scopeRevision: number },
  ) =>
    apiClient.request<AIApprovalRequest>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approvals`, { method: "POST", body }),
  decideApproval: (sessionId: string, approvalId: string, body: { decision: "approve" | "deny"; note?: string }) =>
    apiClient.request<AIApprovalRequest>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approvals/${encodeURIComponent(approvalId)}/decision`,
      { method: "POST", body },
    ),
  approvalEvents: (sessionId: string, query: { limit?: number; cursor?: string } = {}, signal?: AbortSignal) =>
    apiClient.request<{ items: readonly AIApprovalEvent[]; nextCursor?: string | null }>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approval-events`,
      { query, signal },
    ),
  securityPolicy: (signal?: AbortSignal) => apiClient.request<AISecurityPolicy>("/api/v1/ai/security-policy", { signal }),
  execute: (
    sessionId: string,
    body: { scopeRevision: number; toolName: string; targetNodeIds: readonly string[]; arguments: Record<string, unknown>; approvalId?: string },
  ) => apiClient.request<AIExecuteResult>("/api/v1/ai/tools/execute", { method: "POST", body: { ...body, sessionId } }),
  messages: (sessionId: string, query: { limit?: number; cursor?: string } = {}, signal?: AbortSignal) =>
    apiClient.request<Page<AIMessage>>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/messages`, { query, signal }),
  sendMessage: (sessionId: string, body: { content: string; clientMessageId: string; scopeRevision: number }) =>
    apiClient.request<AIMessage>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/messages`, {
      method: "POST",
      body,
      // 真实 Controller 要求 Idempotency-Key；用客户端消息 ID 让重试保持同一意图。
      idempotencyKey: body.clientMessageId,
    }),
  // 显式生成支持可选 SSE 预览；仅 completed 事件中的消息作为最终结果。
  // 中断、错误或缺少 completed 均拒绝，排队和预览文字不算生成成功。
  generate: (sessionId: string, messageId: string, onPreview?: (kind: string, text: string) => void) =>
    apiClient.request<AIMessage>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/messages/${encodeURIComponent(messageId)}/generate`,
      { method: "POST", timeoutMs: 190000, accept: onPreview ? "text/event-stream" : "application/json", onPreview },
    ),
  eventsUrl: (sessionId: string) => `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/events`,
};

/** 所有者作用域的模型配置：GET 为已保存配置（缺省=禁用+revision 0），PUT 走 owner CAS。 */
export const modelSettingsApi = {
  get: (signal?: AbortSignal) => apiClient.request<AIModelSettings>("/api/v1/ai/model-settings", { signal }),
  set: (body: {
    expectedRevision: number;
    enabled: boolean;
    provider: string;
    endpoint?: string;
    model: string;
    credentialRef: string;
    credentialRevision: number;
  }) => apiClient.request<AIModelSettings>("/api/v1/ai/model-settings", { method: "PUT", body }),
  catalog: () => apiClient.request<{ items: readonly string[]; endpoint: string }>("/api/v1/ai/model-settings/models", { method: "POST" }),
};

/**
 * 当前 owner 的窗口预算：GET 只读当前已保存策略/已用/剩余；PUT 是当前真人管理员
 * 的 CAS（expectedRevision），窗口显式、计数保留，不做重置或自动滚动。
 * 计数以服务端为准，页面不根据消息表或本地状态推算。
 */
export const modelBudgetApi = {
  get: (profileId: string, signal?: AbortSignal) =>
    apiClient.request<AIModelBudget>(`/api/v1/ai/model-settings/${encodeURIComponent(profileId)}/budget`, { signal }),
  set: (profileId: string, body: AIModelBudgetInput) =>
    apiClient.request<AIModelBudget>(`/api/v1/ai/model-settings/${encodeURIComponent(profileId)}/budget`, { method: "PUT", body }),
};

export const pluginsApi = {
  repository: (signal?: AbortSignal) => apiClient.request<PluginRepository>("/api/v1/plugins/repository", { signal }),
  putRepository: (body: { mode: "official" | "custom"; url?: string; ref: string; path: "Plugin" }) =>
    apiClient.request<PluginRepository>("/api/v1/plugins/repository", { method: "PUT", body }),
  catalog: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly PluginCatalogEntry[] }>("/api/v1/plugins/catalog", { signal }),
  refreshCatalog: () => apiClient.request<{ items: readonly PluginCatalogEntry[] }>("/api/v1/plugins/catalog/refresh", { method: "POST" }),
  installed: (signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/plugins", { signal })
      .then(result => ({ ...result, data: normalizePluginList(result.data, "installed plugins") })),
  installedOne: (pluginId: string, signal?: AbortSignal) =>
    apiClient
      .request<unknown>(`/api/v1/plugins/${encodeURIComponent(pluginId)}`, { signal })
      .then(result => ({ ...result, data: normalizeInstalledPlugin(result.data, "installed plugin") })),
  install: (pluginId: string, body: { version: string; sourceCommit: string; expectedSha256: string; acceptedPermissions: readonly string[]; enableAfterInstall?: boolean }) =>
    apiClient.request<PluginOperation>(`/api/v1/plugins/${encodeURIComponent(pluginId)}/install`, { method: "POST", body }),
  activation: (pluginId: string, enabled: boolean) =>
    apiClient.request<PluginOperation>(`/api/v1/plugins/${encodeURIComponent(pluginId)}/activation`, { method: "PUT", body: { enabled } }),
  update: (pluginId: string, body: { version: string; sourceCommit: string; expectedSha256: string; acceptedPermissions: readonly string[]; deferIfBusy?: boolean }) =>
    apiClient.request<PluginOperation>(`/api/v1/plugins/${encodeURIComponent(pluginId)}/update`, { method: "POST", body }),
  uninstall: (pluginId: string) =>
    apiClient.request<PluginOperation>(`/api/v1/plugins/${encodeURIComponent(pluginId)}`, { method: "DELETE" }),
  operation: (operationId: string, signal?: AbortSignal) =>
    apiClient.request<PluginOperation>(`/api/v1/plugin-operations/${encodeURIComponent(operationId)}`, { signal }),
};
