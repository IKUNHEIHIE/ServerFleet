import { apiClient, type RequestOptions } from "./client";
import type {
  AuditEvent,
  BatchTaskJob,
  BatchTaskPreview,
  BootstrapRequest,
  BootstrapState,
  Grant,
  HealthPayload,
  Identity,
  ItemList,
  LoginRequest,
  LoginResult,
  NodeCapabilities,
  NodeDetail,
  NodeEnrollment,
  NodeEnrollmentToken,
  NodeGroup,
  NodeGroupFacet,
  NodeGroupResults,
  NodeIdentity,
  NodeSummary,
  Page,
  Permission,
  Role,
  ServiceGroup,
  Session,
  TaskExecution,
  TaskSummary,
  User,
} from "./dto";

function authPost<T>(path: string, body: unknown): Promise<{ data: T; status: number }> {
  return apiClient.request<T>(path, { method: "POST", body, csrf: false, authEvent: false });
}

export const authApi = {
  bootstrapStatus: () => apiClient.request<BootstrapState>("/api/v1/auth/bootstrap-status", { csrf: false }),
  bootstrap: (body: BootstrapRequest) => authPost<LoginResult>("/api/v1/auth/bootstrap", body),
  login: (body: LoginRequest) => authPost<LoginResult>("/api/v1/auth/login", body),
  logout: () => apiClient.request<void>("/api/v1/auth/logout", { method: "POST" }),
  me: () => apiClient.request<Identity>("/api/v1/auth/me"),
  sessions: () => apiClient.request<ItemList<Session>>("/api/v1/auth/sessions"),
  revokeSession: (sessionId: string) =>
    apiClient.request<void>(`/api/v1/auth/sessions/${encodeURIComponent(sessionId)}`, { method: "DELETE" }),
};

export const systemApi = {
  health: () => apiClient.request<HealthPayload>("/healthz", { csrf: false }),
};

export const nodesApi = {
  list: (options: { onlineOnly?: boolean; signal?: AbortSignal } = {}) =>
    apiClient.request<ItemList<NodeSummary>>("/api/v1/nodes", {
      query: { onlineOnly: options.onlineOnly ?? false },
      signal: options.signal,
    }),
  detail: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(nodeId)}`, { signal }),
  identity: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<NodeIdentity>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/identity`, { signal }),
  capabilities: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<NodeCapabilities>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/capabilities`, { signal }),
  suspend: (nodeId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient.request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/suspend`, { method: "POST", body }),
  revoke: (nodeId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient.request<NodeIdentity>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/revoke`, { method: "POST", body }),
};

export const enrollmentApi = {
  createToken: (body: { expiryMinutes?: number; maxUses?: number }) =>
    apiClient.request<NodeEnrollmentToken>("/api/v1/node-enrollment-tokens", { method: "POST", body }),
  list: (signal?: AbortSignal) =>
    apiClient.request<ItemList<NodeEnrollment>>("/api/v1/node-enrollments", { signal }),
  decide: (enrollmentId: string, body: { decision: "approve" | "reject"; reason?: string }) =>
    apiClient.request<NodeEnrollment>(`/api/v1/node-enrollments/${encodeURIComponent(enrollmentId)}/decision`, {
      method: "POST",
      body,
    }),
};

export const groupsApi = {
  list: (signal?: AbortSignal) => apiClient.request<ItemList<NodeGroup>>("/api/v1/groups", { signal }),
  get: (groupId: string, signal?: AbortSignal) =>
    apiClient.request<NodeGroup>(`/api/v1/groups/${encodeURIComponent(groupId)}`, { signal }),
  create: (body: { name: string; description?: string }) =>
    apiClient.request<NodeGroup>("/api/v1/groups", { method: "POST", body }),
  update: (groupId: string, body: { name?: string; description?: string; expectedRevision: number }) =>
    apiClient.request<NodeGroup>(`/api/v1/groups/${encodeURIComponent(groupId)}`, { method: "PATCH", body }),
  remove: (groupId: string) =>
    apiClient.request<void>(`/api/v1/groups/${encodeURIComponent(groupId)}`, { method: "DELETE" }),
  replaceMembers: (groupId: string, body: { memberNodeIds: readonly string[]; expectedRevision: number }) =>
    apiClient.request<NodeGroup>(`/api/v1/groups/${encodeURIComponent(groupId)}/members`, { method: "PUT", body }),
  classifications: (dimension: "region" | "provider" | "tag", signal?: AbortSignal) =>
    apiClient.request<NodeGroupFacet>("/api/v1/groups/classifications", { query: { dimension }, signal }),
  results: (
    query: {
      mode: "manual" | "automatic";
      groupId?: string;
      dimension?: "region" | "provider" | "tag";
      value?: string;
      search?: string;
      status?: string;
      cursor?: string;
      limit?: number;
    },
    signal?: AbortSignal,
  ) => apiClient.request<NodeGroupResults>("/api/v1/groups/node-results", { query, signal }),
};

export const usersApi = {
  list: (signal?: AbortSignal) => apiClient.request<Page<User>>("/api/v1/users", { signal }),
  get: (userId: string, signal?: AbortSignal) =>
    apiClient.request<User>(`/api/v1/users/${encodeURIComponent(userId)}`, { signal }),
  create: (body: { username: string; displayName?: string; password?: string; roleIds: readonly string[]; enabled?: boolean }) =>
    apiClient.request<User>("/api/v1/users", { method: "POST", body }),
  patch: (userId: string, body: { displayName?: string; roleIds?: readonly string[]; enabled?: boolean }) =>
    apiClient.request<User>(`/api/v1/users/${encodeURIComponent(userId)}`, { method: "PATCH", body }),
  roles: (signal?: AbortSignal) => apiClient.request<ItemList<Role>>("/api/v1/roles", { signal }),
  permissions: (signal?: AbortSignal) => apiClient.request<ItemList<Permission>>("/api/v1/permissions", { signal }),
  grants: (signal?: AbortSignal) => apiClient.request<Page<Grant>>("/api/v1/resource-grants", { signal }),
  createGrant: (body: {
    principalId: string;
    resourceType: "node" | "node_group" | "service_group";
    resourceId: string;
    permissions: readonly string[];
    expiresAt?: string;
  }) => apiClient.request<Grant>("/api/v1/resource-grants", { method: "POST", body }),
  revokeGrant: (grantId: string) =>
    apiClient.request<void>(`/api/v1/resource-grants/${encodeURIComponent(grantId)}`, { method: "DELETE" }),
};

export const auditApi = {
  events: (
    query: {
      actorType?: string;
      action?: string;
      resourceType?: string;
      result?: string;
      from?: string;
      to?: string;
      cursor?: string;
      limit?: number;
    } = {},
    signal?: AbortSignal,
  ) => apiClient.request<Page<AuditEvent>>("/api/v1/audit/events", { query, signal }),
  event: (eventId: string, signal?: AbortSignal) =>
    apiClient.request<AuditEvent>(`/api/v1/audit/events/${encodeURIComponent(eventId)}`, { signal }),
};

export const tasksApi = {
  list: (query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient.request<Page<TaskSummary>>("/api/v1/tasks", { query, signal }),
  get: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<TaskSummary>(`/api/v1/tasks/${encodeURIComponent(jobId)}`, { signal }),
  executions: (jobId: string, query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient.request<Page<TaskExecution>>(`/api/v1/tasks/${encodeURIComponent(jobId)}/executions`, { query, signal }),
  approve: (jobId: string, body: { planDigest: string; decision: "approve" | "deny"; reason?: string }) =>
    apiClient.request<TaskSummary>(`/api/v1/tasks/${encodeURIComponent(jobId)}/approve`, { method: "POST", body }),
  cancel: (jobId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient.request<TaskSummary>(`/api/v1/tasks/${encodeURIComponent(jobId)}/cancel`, { method: "POST", body }),
  resume: (jobId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient.request<TaskSummary>(`/api/v1/tasks/${encodeURIComponent(jobId)}/resume`, { method: "POST", body }),
  preview: (body: BatchTaskCreateBody) =>
    apiClient.request<BatchTaskPreview>("/api/v1/tasks/preview", { method: "POST", body }),
  create: (body: BatchTaskCreateBody) => apiClient.request<BatchTaskJob>("/api/v1/tasks", { method: "POST", body }),
  eventsUrl: (jobId: string) => `/api/v1/tasks/${encodeURIComponent(jobId)}/events`,
};

export interface BatchTaskCreateBody {
  readonly name: string;
  readonly target: { readonly type: "node_ids" | "node_group" | "service_group"; readonly nodeIds?: readonly string[]; readonly groupId?: string };
  readonly command: string;
  readonly dryRun: boolean;
  readonly dispatchMode?: "sequential" | "batch" | "parallel";
  readonly approvalId?: string;
}

export const serviceGroupsApi = {
  list: (signal?: AbortSignal) => apiClient.request<ItemList<ServiceGroup>>("/api/v1/service-groups", { signal }),
  get: (serviceGroupId: string, signal?: AbortSignal) =>
    apiClient.request<ServiceGroup>(`/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}`, { signal }),
  create: (body: ServiceGroupInput) =>
    apiClient.request<ServiceGroup>("/api/v1/service-groups", { method: "POST", body }),
  patch: (serviceGroupId: string, body: ServiceGroupInput) =>
    apiClient.request<ServiceGroup>(`/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}`, {
      method: "PATCH",
      body,
    }),
  remove: (serviceGroupId: string) =>
    apiClient.request<void>(`/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}`, { method: "DELETE" }),
  replaceMembers: (serviceGroupId: string, body: { memberNodeIds: readonly string[]; expectedRevision?: number }) =>
    apiClient.request<ServiceGroup>(`/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}/members`, {
      method: "PUT",
      body,
    }),
  setScheduling: (serviceGroupId: string, nodeId: string, body: { eligible: boolean; reason?: string }) =>
    apiClient.request<unknown>(
      `/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}/members/${encodeURIComponent(nodeId)}/scheduling`,
      { method: "PATCH", body },
    ),
  dispatchPreview: (serviceGroupId: string, signal?: AbortSignal) =>
    apiClient.request<unknown>(
      `/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}/dispatch-preview`,
      { signal },
    ),
};

export interface ServiceGroupInput {
  readonly name: string;
  readonly description?: string;
  readonly memberNodeIds?: readonly string[];
  readonly service?: { readonly port?: number; readonly protocol?: string; readonly healthCheck?: string };
  readonly loadBalancing?: { readonly enabled: boolean; readonly strategy?: string };
  readonly gatewayIds?: readonly string[];
  readonly expectedRevision?: number;
}

export type { RequestOptions };
