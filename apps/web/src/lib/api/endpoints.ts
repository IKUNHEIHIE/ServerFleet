import { apiClient, type RequestOptions } from "./client";
import { normalizeListPage, normalizeTaskExecutions, normalizeTransferItem, requireTaskIdentifier } from "./compat";
import type {
  AuditEvent,
  BatchTaskJob,
  BatchTaskPreview,
  BootstrapRequest,
  BootstrapState,
  ConflictPolicy,
  CreateTransferRequest,
  CredentialMetadata,
  DirectoryList,
  FileEntry,
  FileSizeResult,
  Grant,
  HealthPayload,
  Identity,
  ItemList,
  LoginRequest,
  LoginResult,
  NodeCapabilities,
  NodeDetail,
  NodeDiagnostic,
  NodeEnrollment,
  NodeEnrollmentToken,
  NodeGroup,
  NodeGroupFacet,
  NodeGroupResults,
  NodeIdentity,
  NodeSummary,
  Permission,
  Role,
  ServiceGroup,
  ServiceGroupDispatchPreview,
  Session,
  TaskSummary,
  TrafficQuota,
  TransferJob,
  UploadCompletion,
  UploadProgress,
  UploadSession,
  UploadState,
  User,
} from "./dto";

function authPost<T>(path: string, body: unknown, idempotencyKey?: string): Promise<{ data: T; status: number }> {
  return apiClient.request<T>(path, { method: "POST", body, csrf: false, authEvent: false, idempotencyKey });
}

export const authApi = {
  bootstrapStatus: () => apiClient.request<BootstrapState>("/api/v1/auth/bootstrap-status", { csrf: false }),
  bootstrap: (
    body: BootstrapRequest,
    idempotencyKey = `bootstrap-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
  ) => authPost<LoginResult | undefined>("/api/v1/auth/bootstrap", body, idempotencyKey),
  login: (body: LoginRequest) => authPost<LoginResult>("/api/v1/auth/login", body),
  logout: () => apiClient.request<void>("/api/v1/auth/logout", { method: "POST" }),
  me: () => apiClient.request<Identity>("/api/v1/auth/me"),
  sessions: (query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient.request<ItemList<Session>>("/api/v1/auth/sessions", { query, signal }),
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
  diagnostics: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<NodeDiagnostic>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/diagnostics`, { signal }),
  trafficQuota: (nodeId: string, signal?: AbortSignal) =>
    apiClient.request<TrafficQuota>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/traffic-quota`, { signal }),
  setTrafficQuota: (nodeId: string, body: TrafficQuota) =>
    apiClient.request<TrafficQuota>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/traffic-quota`, {
      method: "PUT",
      body,
    }),
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
  list: (query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/users", { query, signal })
      .then(result => ({ ...result, data: normalizeListPage<User>(result.data, parseUser, "users") })),
  get: (userId: string, signal?: AbortSignal) =>
    apiClient.request<User>(`/api/v1/users/${encodeURIComponent(userId)}`, { signal }),
  create: (body: { username: string; displayName?: string; password?: string; roleIds: readonly string[]; enabled?: boolean }) =>
    apiClient.request<User>("/api/v1/users", {
      method: "POST",
      body,
      // 真实 Controller 的用户创建是幂等接口，强制要求 16–128 字符的 Idempotency-Key。
      idempotencyKey: idempotencyKey("user-create"),
    }),
  patch: (userId: string, body: { displayName?: string; roleIds?: readonly string[]; enabled?: boolean }, expectedRevision?: number) =>
    apiClient.request<User>(`/api/v1/users/${encodeURIComponent(userId)}`, {
      method: "PATCH",
      body,
      // 真实 Controller 强制要求 If-Match 乐观锁（缺失返回 428）。
      ifMatch: expectedRevision === undefined ? undefined : `"${expectedRevision}"`,
    }),
  roles: (signal?: AbortSignal) => apiClient.request<ItemList<Role>>("/api/v1/roles", { signal }),
  permissions: (signal?: AbortSignal) => apiClient.request<ItemList<Permission>>("/api/v1/permissions", { signal }),
  grants: (query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/resource-grants", { query, signal })
      .then(result => ({ ...result, data: normalizeListPage<Grant>(result.data, parseGrant, "grants") })),
  createGrant: (body: {
    principalId: string;
    resourceType: "node" | "node_group" | "service_group";
    resourceId: string;
    permissions: readonly string[];
    expiresAt?: string;
  }) =>
    apiClient.request<Grant>("/api/v1/resource-grants", {
      method: "POST",
      body,
      idempotencyKey: idempotencyKey("grant-create"),
    }),
  revokeGrant: (grantId: string) =>
    apiClient.request<void>(`/api/v1/resource-grants/${encodeURIComponent(grantId)}`, { method: "DELETE" }),
};

/** 幂等键：服务端要求 16–128 字节、无空白与 `*?[]` 字符（internal/auth validPolicyID）。 */
function idempotencyKey(scope: string): string {
  const random = Math.random().toString(36).slice(2, 10);
  const key = `sf-web-${scope}-${Date.now().toString(36)}-${random}`;
  return key.length > 128 ? key.slice(0, 128) : key;
}

/** 用户 DTO 解析：仅接受已知形状；服务端键为 userId/username/roleIds/enabled/revision。 */
function parseUser(value: unknown): User {
  if (typeof value !== "object" || value === null) throw new TypeError("user entry is not an object");
  const record = value as Record<string, unknown>;
  const userId = typeof record.userId === "string" ? record.userId : null;
  const username = typeof record.username === "string" ? record.username : null;
  const roleIds = Array.isArray(record.roleIds) && record.roleIds.every(role => typeof role === "string") ? (record.roleIds as string[]) : null;
  if (userId === null || username === null || roleIds === null) throw new TypeError("user entry missing userId/username/roleIds");
  const user: {
    userId: string;
    username: string;
    displayName?: string;
    roleIds: string[];
    enabled: boolean;
    revision: number;
  } = { userId, username, roleIds, enabled: record.enabled === true, revision: typeof record.revision === "number" ? record.revision : 0 };
  if (typeof record.displayName === "string" && record.displayName.length > 0) user.displayName = record.displayName;
  return user;
}

/** 资源授权 DTO 解析：服务端键为 grantId/principalId/resourceType/resourceId/permissions。 */
function parseGrant(value: unknown): Grant {
  if (typeof value !== "object" || value === null) throw new TypeError("grant entry is not an object");
  const record = value as Record<string, unknown>;
  const grantId = typeof record.grantId === "string" ? record.grantId : null;
  const principalId = typeof record.principalId === "string" ? record.principalId : null;
  const resourceType = typeof record.resourceType === "string" ? record.resourceType : null;
  const resourceId = typeof record.resourceId === "string" ? record.resourceId : null;
  const permissions = Array.isArray(record.permissions) && record.permissions.every(p => typeof p === "string")
    ? (record.permissions as string[])
    : null;
  if (grantId === null || principalId === null || resourceType === null || resourceId === null || permissions === null) {
    throw new TypeError("grant entry missing required fields");
  }
  return { grantId, principalId, resourceType, resourceId, permissions };
}

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
  ) => {
    // 真实 Controller（internal/audit/http.go）按 source 过滤主体来源；
    // 候选契约的 actorType 枚举与后端 source 值的对应关系：user↔web，其余同名。
    const { actorType, ...rest } = query;
    const source = actorType ? (actorType === "user" ? "web" : actorType) : undefined;
    return apiClient
      .request<unknown>("/api/v1/audit/events", { query: { ...rest, source }, signal })
      .then(result => ({ ...result, data: normalizeListPage<AuditEvent>(result.data, parseAuditEvent, "audit events") }));
  },
  event: (eventId: string, signal?: AbortSignal) =>
    apiClient.request<AuditEvent>(`/api/v1/audit/events/${encodeURIComponent(eventId)}`, { signal }),
};

/** 审计事件解析：服务端键为 eventId/occurredAt/actorType/action/result/requestId。 */
function parseAuditEvent(value: unknown): AuditEvent {
  if (typeof value !== "object" || value === null) throw new TypeError("audit event is not an object");
  const record = value as Record<string, unknown>;
  const eventId = typeof record.eventId === "string" ? record.eventId : null;
  const occurredAt = typeof record.occurredAt === "string" ? record.occurredAt : null;
  const action = typeof record.action === "string" ? record.action : null;
  const result = typeof record.result === "string" ? record.result : null;
  const requestId = typeof record.requestId === "string" ? record.requestId : null;
  if (eventId === null || occurredAt === null || action === null || result === null || requestId === null) {
    throw new TypeError("audit event missing required fields");
  }
  const event: {
    eventId: string;
    occurredAt: string;
    actorType: AuditEvent["actorType"];
    action: string;
    result: AuditEvent["result"];
    requestId: string;
    actorId?: string;
    resourceType?: string;
    resourceId?: string;
    taskId?: string;
    redactedSummary?: string;
  } = {
    eventId,
    occurredAt,
    action,
    result: result as AuditEvent["result"],
    requestId,
    actorType: (typeof record.actorType === "string" ? record.actorType : "system") as AuditEvent["actorType"],
  };
  for (const key of ["actorId", "resourceType", "resourceId", "taskId", "redactedSummary"] as const) {
    const raw = record[key];
    if (typeof raw === "string" && raw.length > 0) event[key] = raw;
  }
  return event;
}

export const tasksApi = {
  list: (query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/tasks", { query, signal })
      .then(result => ({ ...result, data: normalizeListPage<TaskSummary>(result.data, parseTaskSummary, "tasks") })),
  get: (jobId: string, signal?: AbortSignal) =>
    apiClient
      .request<unknown>(`/api/v1/tasks/${encodeURIComponent(jobId)}`, { signal })
      .then(result => {
        // 真实 GET /api/v1/tasks/{jobId} 返回 batch 形状（jobId/results）；
        // 候选契约与真实 approve 响应返回 summary 形状（taskId/planDigest）。两者都接受。
        const taskId = requireTaskIdentifier(result.data, "task detail");
        return { ...result, data: { ...(result.data as TaskSummary & BatchTaskJob), taskId } };
      }),
  executions: (jobId: string, query: { cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient
      .request<unknown>(`/api/v1/tasks/${encodeURIComponent(jobId)}/executions`, { query, signal })
      .then(result => ({ ...result, data: normalizeTaskExecutions(result.data, "task executions") })),
  approve: (jobId: string, body: { planDigest: string; decision: "approve" | "deny"; reason?: string }) =>
    apiClient
      .request<unknown>(`/api/v1/tasks/${encodeURIComponent(jobId)}/approve`, { method: "POST", body })
      .then(result => {
        requireTaskIdentifier(result.data, "task approve");
        return { ...result, data: result.data as TaskSummary };
      }),
  cancel: (jobId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient
      .request<unknown>(`/api/v1/tasks/${encodeURIComponent(jobId)}/cancel`, { method: "POST", body })
      .then(result => {
        requireTaskIdentifier(result.data, "task cancel");
        return { ...result, data: result.data as TaskSummary };
      }),
  resume: (jobId: string, body: { reason: string; expectedRevision?: number }) =>
    apiClient
      .request<unknown>(`/api/v1/tasks/${encodeURIComponent(jobId)}/resume`, { method: "POST", body })
      .then(result => {
        requireTaskIdentifier(result.data, "task resume");
        return { ...result, data: result.data as TaskSummary };
      }),
  preview: (body: BatchTaskCreateBody) => apiClient.request<BatchTaskPreview>("/api/v1/tasks/preview", { method: "POST", body }),
  create: (body: BatchTaskCreateBody) => apiClient.request<BatchTaskJob>("/api/v1/tasks", { method: "POST", body }),
  eventsUrl: (jobId: string) => `/api/v1/tasks/${encodeURIComponent(jobId)}/events`,
};

/**
 * 任务摘要解析：服务端键为 taskId/state/createdAt/targetNodeIdsSnapshot/revision/planDigest；
 * 缺一即整条非法，不做部分展示。
 */
function parseTaskSummary(value: unknown): TaskSummary {
  if (typeof value !== "object" || value === null) throw new TypeError("task entry is not an object");
  const record = value as Record<string, unknown>;
  const taskId = typeof record.taskId === "string" ? record.taskId : null;
  const state = typeof record.state === "string" ? record.state : null;
  const createdAt = typeof record.createdAt === "string" ? record.createdAt : null;
  const planDigest = typeof record.planDigest === "string" ? record.planDigest : null;
  const revision = typeof record.revision === "number" ? record.revision : null;
  const snapshot = Array.isArray(record.targetNodeIdsSnapshot) && record.targetNodeIdsSnapshot.every(id => typeof id === "string")
    ? (record.targetNodeIdsSnapshot as string[])
    : null;
  if (taskId === null || state === null || createdAt === null || planDigest === null || revision === null || snapshot === null) {
    throw new TypeError("task entry missing taskId/state/createdAt/planDigest/revision/targetNodeIdsSnapshot");
  }
  const task: {
    taskId: string;
    state: TaskSummary["state"];
    createdAt: string;
    targetNodeIdsSnapshot: string[];
    revision: number;
    planDigest: string;
    createdBy?: string;
    expiresAt?: string;
  } = { taskId, state: state as TaskSummary["state"], createdAt, targetNodeIdsSnapshot: snapshot, revision, planDigest };
  if (typeof record.createdBy === "string" && record.createdBy.length > 0) task.createdBy = record.createdBy;
  if (typeof record.expiresAt === "string" && record.expiresAt.length > 0) task.expiresAt = record.expiresAt;
  return task;
}

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
  setScheduling: (serviceGroupId: string, nodeId: string, body: { paused: boolean; reason?: string }) =>
    apiClient.request<unknown>(
      `/api/v1/service-groups/${encodeURIComponent(serviceGroupId)}/members/${encodeURIComponent(nodeId)}/scheduling`,
      { method: "PATCH", body },
    ),
  dispatchPreview: (serviceGroupId: string, signal?: AbortSignal) =>
    apiClient.request<ServiceGroupDispatchPreview>(
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

export const credentialsApi = {
  list: (signal?: AbortSignal) => apiClient.request<readonly CredentialMetadata[]>("/api/v1/credentials", { signal }),
  create: (body: {
    name: string;
    kind: string;
    secret: string;
    usageScope: readonly string[];
    expiresAt?: string;
  }) => apiClient.request<CredentialMetadata>("/api/v1/credentials", { method: "POST", body }),
  references: (credentialId: string, signal?: AbortSignal) =>
    apiClient.request<{ ok: boolean }>(`/api/v1/credentials/${encodeURIComponent(credentialId)}/references`, { signal }),
  rotate: (credentialId: string, body: { name: string; kind: string; secret: string; usageScope: readonly string[]; expiresAt?: string }) =>
    apiClient.request<unknown>(`/api/v1/credentials/${encodeURIComponent(credentialId)}/rotate`, { method: "POST", body }),
  revoke: (credentialId: string) =>
    apiClient.request<unknown>(`/api/v1/credentials/${encodeURIComponent(credentialId)}/revoke`, { method: "POST" }),
};

export const trafficApi = {
  risks: (signal?: AbortSignal) =>
    apiClient.request<readonly { nodeId: string; kind: string; severity: string; estimated: boolean }[]>(
      "/api/v1/traffic-risks",
      { signal },
    ),
};

export const filesApi = {
  list: (
    nodeId: string,
    query: { path: string; search?: string; cursor?: string; limit?: number },
    signal?: AbortSignal,
  ) =>
    apiClient.request<DirectoryList>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files`, { query, signal }),
  remove: (nodeId: string, path: string, recursive: boolean) =>
    apiClient.request<void>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files`, {
      method: "DELETE",
      query: { path, recursive },
    }),
  size: (nodeId: string, path: string) =>
    apiClient.request<FileSizeResult>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files/size`, {
      method: "POST",
      body: { path },
    }),
  createDirectory: (nodeId: string, body: { parentPath: string; name: string }) =>
    apiClient.request<FileEntry>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files/directories`, {
      method: "POST",
      body,
    }),
  rename: (nodeId: string, body: { path: string; newName: string; expectedVersion?: string }) =>
    apiClient.request<FileEntry>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files/rename`, {
      method: "PATCH",
      body,
    }),
  createUpload: (
    nodeId: string,
    body: {
      destinationDir: string;
      conflictPolicy?: ConflictPolicy;
      files: readonly { name: string; sizeBytes: number; sha256?: string }[];
    },
  ) =>
    apiClient.request<UploadSession>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/files/uploads`, {
      method: "POST",
      body,
    }),
  // GM-W5-01：真实字节通道。offset 必须与服务端已收字节数一致（否则 409）；
  // 服务端写入后回传 bytesReceived，最终由 complete 触发字节数与 SHA-256 校验。
  uploadContent: (
    nodeId: string,
    uploadId: string,
    body: ArrayBuffer | Uint8Array | string,
    offset = 0,
  ) =>
    apiClient.requestRaw<UploadProgress>(
      `/api/v1/nodes/${encodeURIComponent(nodeId)}/files/uploads/${encodeURIComponent(uploadId)}/content?offset=${offset}`,
      body,
      { method: "PUT" },
    ),
  completeUpload: (nodeId: string, uploadId: string) =>
    apiClient.request<UploadCompletion>(
      `/api/v1/nodes/${encodeURIComponent(nodeId)}/files/uploads/${encodeURIComponent(uploadId)}/complete`,
      { method: "POST" },
    ),
  // 刷新/断线恢复：读回服务端真实状态（含 dispatching 与 taskId），不依赖前端记忆。
  uploadStatus: (nodeId: string, uploadId: string, signal?: AbortSignal) =>
    apiClient.request<UploadState>(
      `/api/v1/nodes/${encodeURIComponent(nodeId)}/files/uploads/${encodeURIComponent(uploadId)}`,
      { signal },
    ),
};

export const transfersApi = {
  list: (query: { status?: string; cursor?: string; limit?: number } = {}, signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/transfers", { query, signal })
      .then(result => ({ ...result, data: normalizeListPage<TransferJob>(result.data, parseTransferJob, "transfers") })),
  create: (body: CreateTransferRequest, idempotencyKey: string) =>
    apiClient
      .request<unknown>("/api/v1/transfers", { method: "POST", body, idempotencyKey })
      .then(result => ({ ...result, data: parseTransferJob(result.data) })),
  get: (transferId: string, signal?: AbortSignal) =>
    apiClient
      .request<unknown>(`/api/v1/transfers/${encodeURIComponent(transferId)}`, { signal })
      .then(result => ({ ...result, data: parseTransferJob(result.data) })),
  cancel: (transferId: string) =>
    apiClient
      .request<unknown>(`/api/v1/transfers/${encodeURIComponent(transferId)}/cancel`, { method: "POST" })
      .then(result => ({ ...result, data: parseTransferJob(result.data) })),
};

/** 传输作业解析：条目走 normalizeTransferItem 兼容候选（path/state/bytes）与真实（sourcePath/status/bytesCopied）。 */
function parseTransferJob(value: unknown): TransferJob {
  if (typeof value !== "object" || value === null) throw new TypeError("transfer is not an object");
  const record = value as Record<string, unknown>;
  const id = typeof record.id === "string" ? record.id : null;
  const sourceNodeId = typeof record.sourceNodeId === "string" ? record.sourceNodeId : null;
  const destinationNodeId = typeof record.destinationNodeId === "string" ? record.destinationNodeId : null;
  const destinationDir = typeof record.destinationDir === "string" ? record.destinationDir : null;
  const status = typeof record.status === "string" ? record.status : null;
  const createdAt = typeof record.createdAt === "string" ? record.createdAt : null;
  if (id === null || sourceNodeId === null || destinationNodeId === null || destinationDir === null || status === null || createdAt === null) {
    throw new TypeError("transfer missing required fields");
  }
  const job: {
    id: string;
    sourceNodeId: string;
    sourcePaths: string[];
    destinationNodeId: string;
    destinationDir: string;
    conflictPolicy?: TransferJob["conflictPolicy"];
    status: TransferJob["status"];
    progressPercent: number;
    totalBytes?: number | null;
    copiedBytes?: number;
    items?: TransferJob["items"];
    createdAt: string;
    startedAt?: string | null;
    completedAt?: string | null;
  } = {
    id,
    sourceNodeId,
    sourcePaths: Array.isArray(record.sourcePaths) ? record.sourcePaths.filter((p): p is string => typeof p === "string") : [],
    destinationNodeId,
    destinationDir,
    status: status as TransferJob["status"],
    progressPercent: typeof record.progressPercent === "number" && Number.isFinite(record.progressPercent) ? record.progressPercent : 0,
    createdAt,
  };
  if (typeof record.conflictPolicy === "string" && record.conflictPolicy.length > 0) {
    job.conflictPolicy = record.conflictPolicy as TransferJob["conflictPolicy"];
  }
  if (record.totalBytes === null || typeof record.totalBytes === "number") job.totalBytes = record.totalBytes;
  if (typeof record.copiedBytes === "number") job.copiedBytes = record.copiedBytes;
  if (Array.isArray(record.items)) job.items = record.items.map(item => normalizeTransferItem(item, "transfers"));
  if (typeof record.startedAt === "string" && record.startedAt.length > 0) job.startedAt = record.startedAt;
  if (typeof record.completedAt === "string" && record.completedAt.length > 0) job.completedAt = record.completedAt;
  return job;
}

export type { RequestOptions };
