import { apiClient } from "./client";
import type {
  AIApprovalEvent,
  AIApprovalMode,
  AIApprovalPolicy,
  AIApprovalRequest,
  AIExecuteResult,
  AIMessage,
  AIResourceRecommendation,
  AISecurityPolicy,
  AISession,
  InstalledPlugin,
  Page,
  PluginCatalogEntry,
  PluginOperation,
  PluginRepository,
} from "./dto";

export const aiApi = {
  createSession: () => apiClient.request<AISession>("/api/v1/ai/sessions", { method: "POST", body: {} }),
  getSession: (sessionId: string, signal?: AbortSignal) =>
    apiClient.request<AISession>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}`, { signal }),
  grant: (sessionId: string, body: { resourceType: "node" | "service_group"; resourceId: string; allowedActions: readonly string[] }) =>
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
  updateApprovalPolicy: (sessionId: string, body: { mode: AIApprovalMode; durationSeconds?: 900 | 1800 | 3600; userConfirmedFullAccess?: boolean }) =>
    apiClient.request<AIApprovalPolicy>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/approval-policy`, { method: "PATCH", body }),
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
    apiClient.request<AIMessage>(`/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/messages`, { method: "POST", body }),
  eventsUrl: (sessionId: string) => `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/events`,
};

export const pluginsApi = {
  repository: (signal?: AbortSignal) => apiClient.request<PluginRepository>("/api/v1/plugins/repository", { signal }),
  putRepository: (body: { mode: "official" | "custom"; url?: string; ref: string; path: "Plugin" }) =>
    apiClient.request<PluginRepository>("/api/v1/plugins/repository", { method: "PUT", body }),
  catalog: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly PluginCatalogEntry[] }>("/api/v1/plugins/catalog", { signal }),
  refreshCatalog: () => apiClient.request<{ items: readonly PluginCatalogEntry[] }>("/api/v1/plugins/catalog/refresh", { method: "POST" }),
  installed: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly InstalledPlugin[] }>("/api/v1/plugins", { signal }),
  installedOne: (pluginId: string, signal?: AbortSignal) =>
    apiClient.request<InstalledPlugin>(`/api/v1/plugins/${encodeURIComponent(pluginId)}`, { signal }),
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
