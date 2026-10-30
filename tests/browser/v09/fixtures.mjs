/**
 * 浏览器验收桩的内存数据。仅用于本地 GM-09 验收：不是 Controller 实现，
 * 也不会进入 dist 或被 apps/web/src 引用。每个写操作都修改 state，便于观察前后变化。
 */
import { http, page } from "./harness.mjs";

const NOW = "2026-10-30T02:00:00Z";

const NODES = [
  { id: "n-1", name: "东京-01", online: true, region: "ap-northeast-1", provider: "AWS", countryCode: "JP", ip: "13.112.8.21", tags: ["web", "prod"], cpuPercent: 0, memoryPercent: 61.5 },
  { id: "n-2", name: "大阪-02", online: true, region: "ap-northeast-2", provider: "AWS", countryCode: "JP", ip: "13.209.44.7", tags: ["web"], cpuPercent: 87, memoryPercent: 100 },
  { id: "n-3", name: "新加坡-01", online: false, region: "ap-southeast-1", provider: "Aliyun", countryCode: "SG", ip: "47.74.12.9", tags: ["backup"], cpuPercent: null, memoryPercent: null },
  { id: "n-4", name: "法兰克福-01", online: true, region: "eu-central-1", provider: "Hetzner", countryCode: "DE", ip: "116.202.9.11", tags: ["db"], cpuPercent: 32.4, memoryPercent: 55 },
  { id: "n-5", name: "弗吉尼亚-03", online: true, region: "us-east-1", provider: "AWS", countryCode: "US", ip: "54.88.3.2", tags: ["web", "canary"], cpuPercent: 12, memoryPercent: 40.2 },
  { id: "n-6", name: "悉尼-01", online: false, region: "ap-southeast-2", provider: "Vultr", countryCode: "AU", ip: "139.99.130.4", tags: ["backup"], cpuPercent: null, memoryPercent: null },
];

const nodeById = id => NODES.find(node => node.id === id);

function detail(node) {
  const live = state.nodeStatus?.[node.id] ?? (node.online ? "online" : "offline");
  return {
    nodeId: node.id,
    displayName: node.name,
    status: live,
    lastSeenAt: live === "online" ? "2026-10-30T01:59:30Z" : "2026-10-28T22:10:00Z",
    agentVersion: "0.9.0-dev",
    platform: "linux/amd64",
    revision: 3,
  };
}

const state = {};

function seed() {
  state.groups ??= [
    { id: "g-1", name: "生产 Web", kind: "manual", description: "对外服务节点", memberNodeIds: ["n-1", "n-2", "n-5"], revision: 4, memberCount: 3 },
    { id: "g-2", name: "备份专用", kind: "manual", description: "离线备份节点", memberNodeIds: ["n-3", "n-6"], revision: 2, memberCount: 2 },
  ];
  state.serviceGroups ??= [
    {
      id: "sg-1",
      revision: 3,
      name: "官网服务",
      description: "HTTP 服务组成员",
      memberNodeIds: ["n-1", "n-2", "n-5"],
      service: { port: 8080, protocol: "http", healthCheck: "GET /healthz" },
      loadBalancing: { enabled: true, strategy: "round_robin" },
      gatewayIds: ["gw-1"],
      healthyNodeIds: ["n-1", "n-2"],
      dispatchEligibleNodeIds: ["n-1", "n-2"],
      createdAt: "2026-10-01T02:00:00Z",
    },
  ];
  state.users ??= [
    { userId: "u-admin", username: "admin", displayName: "验收管理员", roleIds: ["admin"], enabled: true, revision: 1 },
    { userId: "u-ops", username: "ops", displayName: "运维值班", roleIds: ["operator"], enabled: true, revision: 2 },
  ];
  state.grants ??= [
    { grantId: "grant-1", principalId: "u-ops", resourceType: "node_group", resourceId: "g-1", permissions: ["node.read", "task.submit"], createdAt: "2026-10-12T02:00:00Z" },
  ];
  state.sessions ??= [
    { sessionId: "sess-current", createdAt: "2026-10-30T01:00:00Z", expiresAt: "2026-10-30T10:00:00Z", lastSeenAt: "2026-10-30T01:59:00Z", current: true },
    { sessionId: "sess-other", createdAt: "2026-10-29T12:00:00Z", expiresAt: "2026-10-30T12:00:00Z", lastSeenAt: "2026-10-30T01:10:00Z", current: false },
  ];
  state.audit ??= [
    { eventId: "ev-1", occurredAt: "2026-10-30T01:58:00Z", actorType: "user", actorId: "u-admin", action: "node.suspend", resourceType: "node", resourceId: "n-6", result: "success", requestId: "req-1001", redactedSummary: "管理员暂停了节点（命令输出已脱敏）" },
    { eventId: "ev-2", occurredAt: "2026-10-30T01:40:00Z", actorType: "ai", actorId: "ai-session", action: "ai.tools.execute", resourceType: "node", resourceId: "n-1", result: "denied", requestId: "req-1002", redactedSummary: "AI 请求超出会话授权范围，被服务端拒绝" },
    { eventId: "ev-3", occurredAt: "2026-10-30T01:20:00Z", actorType: "user", actorId: "u-ops", action: "task.cancel", resourceType: "task", resourceId: "task-1", result: "success", requestId: "req-1003" },
  ];
  state.tasks ??= [
    { taskId: "task-1", state: "awaiting_approval", createdAt: "2026-10-30T01:30:00Z", createdBy: "u-ops", targetNodeIdsSnapshot: ["n-1", "n-2"], revision: 2, planDigest: "digest-aaa", expiresAt: "2026-10-30T03:00:00Z" },
    { taskId: "task-2", state: "running", createdAt: "2026-10-30T01:45:00Z", createdBy: "u-admin", targetNodeIdsSnapshot: ["n-5"], revision: 1, planDigest: "digest-bbb" },
    { taskId: "task-3", state: "completed", createdAt: "2026-10-29T22:00:00Z", createdBy: "u-admin", targetNodeIdsSnapshot: ["n-4"], revision: 1, planDigest: "digest-ccc" },
  ];
  state.transfers ??= [
    { id: "tr-1", sourceNodeId: "n-1", sourcePaths: ["/srv/www/index.html"], destinationNodeId: "n-2", destinationDir: "/srv/www", conflictPolicy: "skip", status: "running", progressPercent: 42, totalBytes: 1048576, copiedBytes: 440401, createdAt: "2026-10-30T01:50:00Z" },
    { id: "tr-2", sourceNodeId: "n-4", sourcePaths: ["/var/backups/db.tar"], destinationNodeId: "n-3", destinationDir: "/data", conflictPolicy: "overwrite", status: "failed", progressPercent: 100, totalBytes: 52428800, copiedBytes: 0, createdAt: "2026-10-29T20:00:00Z", error: { code: "NODE_OFFLINE", message: "目标节点离线" } },
  ];
  state.monitors ??= [
    { id: "mon-1", name: "官网 HTTPS", type: "https_placeholder", target: "https://example.com", sourceNodeId: "n-1", intervalSeconds: 60, timeoutSeconds: 5, failureCount: 0, recoveryCount: 1, state: "healthy", lastCheckedAt: "2026-10-30T01:59:00Z", notifyChannels: ["smtp"] },
  ];
  state.channels ??= [
    { channel: "smtp", enabled: true, credentialStatus: "configured", smtp: { host: "smtp.example.com", port: 465, security: "tls", sender: "alert@example.com", recipient: "ops@example.com" } },
    { channel: "qqbot", enabled: false, credentialStatus: "not_configured", qqbot: { appId: "1000001", targetType: "group", targetId: "2000002" } },
  ];
  state.deliveries ??= [
    { id: "dl-1", channel: "smtp", status: "provider_accepted", providerCode: "250", message: "平台已接受，不保证已读", createdAt: "2026-10-30T00:10:00Z" },
  ];
  state.dnsAccounts ??= [
    { id: "da-1", providerCode: "cloudflare", displayName: "主账号", note: "生产域名", enabled: true, isDefault: true, credentialStatus: "valid", zoneCount: 1 },
  ];
  state.zones ??= [{ id: "zone-1", accountId: "da-1", name: "example.com", status: "active" }];
  state.records ??= [
    { id: "rec-1", zoneId: "zone-1", type: "A", name: "www", content: "13.112.8.21", ttl: 120, proxied: true, modifiedAt: "2026-10-29T10:00:00Z" },
    { id: "rec-2", zoneId: "zone-1", type: "TXT", name: "@", content: "v=spf1 -all", ttl: 300, proxied: false, modifiedAt: "2026-10-20T10:00:00Z" },
  ];
  state.gateways ??= [
    { id: "gw-1", nodeId: "n-5", name: "入口-弗吉尼亚", region: "us-east-1", countryCode: "US", online: true, status: "ready", nginxInstalled: true, checksPassed: true, enabled: true, schedulingEligible: true, nginxVersion: "1.27.2" },
    { id: "gw-2", nodeId: "n-4", name: "入口-法兰克福", region: "eu-central-1", countryCode: "DE", online: true, status: "maintenance", nginxInstalled: true, checksPassed: true, enabled: true, schedulingEligible: false, nginxVersion: "1.27.2", lastError: "维护中，不参与调度" },
  ];
  state.routes ??= [
    { id: "route-1", domain: "www.example.com", serviceGroupId: "sg-1", https: true, enabled: true },
  ];
  state.certJobs ??= [
    {
      jobId: "cert-1",
      serviceGroupId: "sg-1",
      memberNodeIdsSnapshot: ["n-1", "n-2", "n-5"],
      credentialType: "per_node_identity",
      state: "partial",
      createdAt: "2026-10-29T09:00:00Z",
      requestedBy: "u-admin",
      results: [
        { nodeId: "n-1", state: "verified", certificateSerial: "0x1a2b", identity: "spiffe://fleet/n-1", expiresAt: "2027-01-27T09:00:00Z" },
        { nodeId: "n-2", state: "verified", certificateSerial: "0x1a2c", identity: "spiffe://fleet/n-2", expiresAt: "2027-01-27T09:00:00Z" },
        { nodeId: "n-5", state: "waiting_offline", retryable: true, errorCode: "NODE_OFFLINE" },
      ],
    },
  ];
  state.publications ??= [
    { id: "pub-1", status: "succeeded", createdAt: "2026-10-28T03:00:00Z", stages: [{ name: "dns", status: "succeeded" }, { name: "gateway", status: "succeeded" }], auditId: "aud-pub-1" },
  ];
  state.aiSessions ??= {};
  state.approvals ??= [];
  state.aiMessages ??= [];
  state.plugins ??= [
    { id: "xray-manager", version: "0.1.0", status: "enabled", enabled: true, sourceCommit: "c0ffee", installedAt: "2026-10-28T02:00:00Z", permissions: ["plugins.node.manage"] },
  ];
  state.pluginOps ??= [];
  state.updateJobs ??= [
    { jobId: "job-1", fromVersion: "0.7.0", toVersion: "0.8.0", state: "failed", phase: "health_check", errorCode: "HEALTH_FAILED", backupId: "bk-1", rollbackEligible: true, requestedBy: "u-admin", createdAt: "2026-10-29T01:00:00Z", updatedAt: "2026-10-29T01:05:00Z" },
    { jobId: "job-2", fromVersion: "0.8.0", toVersion: "0.8.0", state: "completed", phase: "completed", backupId: "bk-0", rollbackEligible: false, requestedBy: "u-admin", createdAt: "2026-10-20T01:00:00Z", updatedAt: "2026-10-20T01:06:00Z" },
  ];
  state.drJobs ??= {};
  state.xrayUsers ??= [
    { userId: "xu-1", nodeId: "n-1", inboundTag: "vless-in", displayName: "运营账户", statsIdentifier: "stats-1", enabled: true, createdAt: "2026-10-15T02:00:00Z", totalUplinkBytes: 104857600, totalDownlinkBytes: 314572800 },
  ];
  state.nodeStatus = {};
  state.nextId = 10;
}

seed();

function id(prefix) {
  state.nextId += 1;
  return `${prefix}-${state.nextId}`;
}

function aiSession(sessionId) {
  const record = state.aiSessions[sessionId];
  if (!record) return null;
  const allowed = new Set();
  for (const grant of record.resourceGrants) {
    if (grant.resourceType === "node") allowed.add(grant.resourceId);
    if (grant.resourceType === "service_group") {
      const group = state.serviceGroups.find(item => item.id === grant.resourceId);
      for (const nodeId of group?.memberNodeIds ?? []) allowed.add(nodeId);
    }
  }
  return { ...record, allowedNodeIds: [...allowed] };
}

const policyDefault = sessionId => ({
  sessionId,
  mode: "per_action",
  effectiveMode: "per_action",
  scopeRevision: state.aiSessions[sessionId]?.scopeRevision ?? 1,
  maxFullAccessSeconds: 3600,
  enforcedRestrictions: ["root shell 仍需人工确认", "完全访问不扩大资源授权范围"],
});

export const routes = {
  // ---------- 节点 ----------
  "GET /api/v1/nodes": ctx => {
    const onlineOnly = ctx.query.get("onlineOnly") === "true";
    return { items: NODES.filter(node => !onlineOnly || node.online) };
  },
  "GET /api/v1/nodes/{nodeId}": ctx => {
    const node = nodeById(ctx.params.nodeId);
    if (!node) return http(404, { code: "NOT_FOUND", message: "节点不存在" });
    return detail(node);
  },
  "GET /api/v1/nodes/{nodeId}/identity": ctx => ({
    nodeId: ctx.params.nodeId,
    certificateFingerprintSha256: "aa:bb:cc:dd:ee:ff:00:11",
    issuerFingerprintSha256: "11:22:33:44:55:66:77:88",
    serial: "0x00a1",
    notAfter: "2027-04-01T00:00:00Z",
    revoked: state.nodeStatus?.[ctx.params.nodeId] === "revoked",
    lastVerifiedAt: "2026-10-30T01:58:00Z",
  }),
  "GET /api/v1/nodes/{nodeId}/capabilities": ctx => ({
    nodeId: ctx.params.nodeId,
    protocolMajor: 2,
    protocolMinor: 1,
    capabilities: ["exec", "files", "metrics", "traffic_quota"],
    observedAt: "2026-10-30T01:58:00Z",
  }),
  "GET /api/v1/nodes/{nodeId}/diagnostics": ctx => ({
    nodeId: ctx.params.nodeId,
    connectionState: nodeById(ctx.params.nodeId)?.online ? "online" : "unreachable",
    lastHeartbeatAt: "2026-10-30T01:59:30Z",
    agentVersion: "0.9.0-dev",
    identityVerified: true,
    pendingTasks: [{ id: "task-1", expired: false, requiresReapproval: true }],
    diagnosticNotes: ["验收桩数据：真实后端尚未实现诊断接口"],
  }),
  "GET /api/v1/nodes/{nodeId}/traffic-quota": ctx => ({
    nodeId: ctx.params.nodeId,
    quotaGb: 1000,
    usedGb: 321.4,
    cycleResetDay: 1,
    timezone: "Asia/Tokyo",
    direction: "combined",
    source: "agent_estimate",
    dataComplete: true,
    thresholdsPct: [80, 95],
    remindDays: [3, 1],
    predictionGb: 980,
    predictionConfidence: "medium",
  }),
  "PUT /api/v1/nodes/{nodeId}/traffic-quota": ctx => ({ ...ctx.body, nodeId: ctx.params.nodeId }),
  "POST /api/v1/nodes/{nodeId}/suspend": ctx => {
    state.nodeStatus[ctx.params.nodeId] = "suspended";
    return detail(nodeById(ctx.params.nodeId));
  },
  "POST /api/v1/nodes/{nodeId}/revoke": ctx => {
    state.nodeStatus[ctx.params.nodeId] = "revoked";
    return { nodeId: ctx.params.nodeId, revoked: true, lastVerifiedAt: NOW };
  },

  // ---------- 接入 ----------
  "POST /api/v1/node-enrollment-tokens": () => http(201, { id: id("tok"), token: "stub-token-7f3c9a", expiresAt: "2026-10-30T03:00:00Z" }),
  "GET /api/v1/node-enrollments": () => ({
    items: [
      { id: "en-1", name: "香港-01", status: "pending", countryCode: "HK", region: "hk-1" },
      { id: "en-2", name: "洛杉矶-01", status: "approved", countryCode: "US", region: "us-west-2" },
    ],
  }),
  "POST /api/v1/node-enrollments/{enrollmentId}/decision": ctx => ({
    id: ctx.params.enrollmentId,
    name: "香港-01",
    status: ctx.body?.decision === "reject" ? "rejected" : "approved",
    countryCode: "HK",
    region: "hk-1",
  }),

  // ---------- 分组 ----------
  "GET /api/v1/groups": () => ({ items: state.groups }),
  "POST /api/v1/groups": ctx => {
    const group = { id: id("g"), name: ctx.body.name, kind: "manual", description: ctx.body.description, memberNodeIds: [], revision: 1, memberCount: 0 };
    state.groups.push(group);
    return http(201, group);
  },
  "GET /api/v1/groups/{groupId}": ctx => {
    const group = state.groups.find(item => item.id === ctx.params.groupId);
    return group ? { ...group, memberCount: group.memberNodeIds.length } : http(404, { code: "NOT_FOUND", message: "分组不存在" });
  },
  "PATCH /api/v1/groups/{groupId}": ctx => {
    const group = state.groups.find(item => item.id === ctx.params.groupId);
    Object.assign(group, ctx.body, { revision: group.revision + 1 });
    return group;
  },
  "DELETE /api/v1/groups/{groupId}": ctx => {
    state.groups = state.groups.filter(item => item.id !== ctx.params.groupId);
    return http(204, undefined);
  },
  "PUT /api/v1/groups/{groupId}/members": ctx => {
    const group = state.groups.find(item => item.id === ctx.params.groupId);
    group.memberNodeIds = [...(ctx.body?.memberNodeIds ?? [])];
    group.revision += 1;
    group.memberCount = group.memberNodeIds.length;
    return group;
  },
  "GET /api/v1/groups/classifications": ctx => {
    const dimension = ctx.query.get("dimension") ?? "region";
    const counts = new Map();
    for (const node of NODES) {
      const value = dimension === "region" ? node.region : dimension === "provider" ? node.provider : node.tags?.[0];
      if (!value) continue;
      counts.set(value, (counts.get(value) ?? 0) + 1);
    }
    return { dimension, items: [...counts.entries()].map(([value, nodeCount]) => ({ value, nodeCount })) };
  },
  "GET /api/v1/groups/node-results": ctx => {
    const groupId = ctx.query.get("groupId");
    const value = ctx.query.get("value");
    const dimension = ctx.query.get("dimension") ?? "region";
    let items = NODES;
    if (groupId) {
      const group = state.groups.find(item => item.id === groupId);
      items = NODES.filter(node => group?.memberNodeIds.includes(node.id));
    } else if (value) {
      items = NODES.filter(node => (dimension === "region" ? node.region : dimension === "provider" ? node.provider : node.tags?.[0]) === value);
    }
    const search = (ctx.query.get("search") ?? "").toLowerCase();
    if (search) items = items.filter(node => node.name.toLowerCase().includes(search) || node.id.includes(search));
    const status = ctx.query.get("status");
    if (status === "online" || status === "offline") items = items.filter(node => (status === "online") === node.online);
    return { items, total: items.length, nextCursor: null };
  },

  // ---------- 服务组 ----------
  "GET /api/v1/service-groups": () => ({ items: state.serviceGroups }),
  "POST /api/v1/service-groups": ctx => {
    const group = {
      id: id("sg"),
      revision: 1,
      name: ctx.body.name,
      description: ctx.body.description,
      memberNodeIds: [...(ctx.body.memberNodeIds ?? [])],
      service: ctx.body.service,
      loadBalancing: ctx.body.loadBalancing ?? { enabled: false },
      gatewayIds: [...(ctx.body.gatewayIds ?? [])],
      healthyNodeIds: [...(ctx.body.memberNodeIds ?? [])],
      dispatchEligibleNodeIds: [...(ctx.body.memberNodeIds ?? [])],
      createdAt: NOW,
    };
    state.serviceGroups.push(group);
    return http(201, group);
  },
  "GET /api/v1/service-groups/{serviceGroupId}": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.params.serviceGroupId);
    return group ?? http(404, { code: "NOT_FOUND", message: "服务组不存在" });
  },
  "PATCH /api/v1/service-groups/{serviceGroupId}": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.params.serviceGroupId);
    Object.assign(group, ctx.body, { revision: group.revision + 1 });
    return group;
  },
  "DELETE /api/v1/service-groups/{serviceGroupId}": ctx => {
    state.serviceGroups = state.serviceGroups.filter(item => item.id !== ctx.params.serviceGroupId);
    return http(204, undefined);
  },
  "PUT /api/v1/service-groups/{serviceGroupId}/members": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.params.serviceGroupId);
    group.memberNodeIds = [...(ctx.body?.memberNodeIds ?? [])];
    group.revision += 1;
    group.healthyNodeIds = [...group.memberNodeIds];
    group.dispatchEligibleNodeIds = [...group.memberNodeIds];
    return group;
  },
  "PATCH /api/v1/service-groups/{serviceGroupId}/members/{nodeId}/scheduling": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.params.serviceGroupId);
    if (ctx.body?.paused) group.dispatchEligibleNodeIds = group.dispatchEligibleNodeIds.filter(nodeId => nodeId !== ctx.params.nodeId);
    else if (!group.dispatchEligibleNodeIds.includes(ctx.params.nodeId)) group.dispatchEligibleNodeIds.push(ctx.params.nodeId);
    return { ok: true };
  },
  "GET /api/v1/service-groups/{serviceGroupId}/dispatch-preview": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.params.serviceGroupId);
    return {
      serviceGroupId: ctx.params.serviceGroupId,
      gatewayEligible: (group?.gatewayIds?.length ?? 0) > 0,
      eligibleNodeIds: group?.dispatchEligibleNodeIds ?? [],
      excludedNodes: (group?.memberNodeIds ?? [])
        .filter(nodeId => !(group?.dispatchEligibleNodeIds ?? []).includes(nodeId))
        .map(nodeId => ({ nodeId, reason: "成员被暂停或无健康检查结果" })),
      policy: group?.loadBalancing?.strategy,
    };
  },

  // ---------- 用户 / 授权 / 会话 ----------
  "GET /api/v1/users": () => page(state.users),
  "POST /api/v1/users": ctx => {
    const user = { userId: id("u"), username: ctx.body.username, displayName: ctx.body.displayName, roleIds: [...(ctx.body.roleIds ?? [])], enabled: ctx.body.enabled ?? true, revision: 1 };
    state.users.push(user);
    return http(201, user);
  },
  "GET /api/v1/users/{userId}": ctx => state.users.find(item => item.userId === ctx.params.userId) ?? http(404, { code: "NOT_FOUND", message: "用户不存在" }),
  "PATCH /api/v1/users/{userId}": ctx => {
    const user = state.users.find(item => item.userId === ctx.params.userId);
    if (!user) return http(404, { code: "NOT_FOUND", message: "用户不存在" });
    Object.assign(user, ctx.body, { revision: user.revision + 1 });
    return user;
  },
  "GET /api/v1/roles": () => ({
    items: [
      { roleId: "admin", label: "管理员", permissions: ["node.manage", "task.submit", "user.manage"], builtIn: true },
      { roleId: "operator", label: "运维", permissions: ["node.read", "task.read", "task.submit"], builtIn: true },
    ],
  }),
  "GET /api/v1/permissions": () => ({
    items: [
      { key: "node.read", description: "查看节点", resourceType: "node" },
      { key: "node.manage", description: "管理节点", resourceType: "node" },
      { key: "task.submit", description: "提交任务", resourceType: "task" },
      { key: "user.manage", description: "管理用户", resourceType: "user" },
    ],
  }),
  "GET /api/v1/resource-grants": () => page(state.grants),
  "POST /api/v1/resource-grants": ctx => {
    const grant = { grantId: id("grant"), ...ctx.body, createdAt: NOW };
    state.grants.push(grant);
    return http(201, grant);
  },
  "DELETE /api/v1/resource-grants/{grantId}": ctx => {
    state.grants = state.grants.filter(item => item.grantId !== ctx.params.grantId);
    return http(204, undefined);
  },
  "GET /api/v1/auth/sessions": () => ({ items: state.sessions }),
  "DELETE /api/v1/auth/sessions/{sessionId}": ctx => {
    state.sessions = state.sessions.filter(item => item.sessionId !== ctx.params.sessionId);
    return http(204, undefined);
  },

  // ---------- 审计 ----------
  "GET /api/v1/audit/events": () => page(state.audit),
  "GET /api/v1/audit/events/{eventId}": ctx => state.audit.find(item => item.eventId === ctx.params.eventId) ?? http(404, { code: "NOT_FOUND", message: "事件不存在" }),

  // ---------- 凭据 ----------
  "GET /api/v1/credentials": () => [
    { id: "cred-1", name: "Cloudflare 主账号 Token", kind: "cloudflare_token", status: "valid", usageScope: ["dns"], references: ["da-1"], lastValidatedAt: "2026-10-29T02:00:00Z", createdAt: "2026-10-01T02:00:00Z" },
    { id: "cred-2", name: "SMTP 授权码", kind: "smtp_password", status: "unverified", usageScope: ["notifications"], lastValidatedAt: "2026-10-20T02:00:00Z" },
  ],
  "POST /api/v1/credentials": ctx => http(201, { id: id("cred"), name: ctx.body.name, kind: ctx.body.kind, status: "unverified", usageScope: ctx.body.usageScope, createdAt: NOW }),
  "GET /api/v1/credentials/{credentialId}/references": () => ({ ok: true }),
  "POST /api/v1/credentials/{credentialId}/rotate": ctx => ({ ok: true, id: ctx.params.credentialId, status: "unverified" }),
  "POST /api/v1/credentials/{credentialId}/revoke": ctx => ({ ok: true, id: ctx.params.credentialId, status: "revoked" }),

  // ---------- 任务 ----------
  "GET /api/v1/tasks": () => page(state.tasks),
  "GET /api/v1/tasks/{jobId}": ctx => state.tasks.find(item => item.taskId === ctx.params.jobId) ?? http(404, { code: "NOT_FOUND", message: "任务不存在" }),
  "GET /api/v1/tasks/{jobId}/executions": ctx => page([
    { executionId: "ex-1", taskId: ctx.params.jobId, nodeId: "n-1", state: "completed", startedAt: "2026-10-30T01:31:00Z", finishedAt: "2026-10-30T01:31:05Z", exitCode: 0, outputTruncated: false, lastSequence: 4 },
    { executionId: "ex-2", taskId: ctx.params.jobId, nodeId: "n-2", state: "running", startedAt: "2026-10-30T01:31:06Z", exitCode: null, outputTruncated: true, lastSequence: 7 },
  ]),
  "POST /api/v1/tasks/{jobId}/approve": ctx => {
    const task = state.tasks.find(item => item.taskId === ctx.params.jobId);
    task.state = ctx.body?.decision === "deny" ? "canceled" : "queued";
    task.revision += 1;
    return task;
  },
  "POST /api/v1/tasks/{jobId}/cancel": ctx => {
    const task = state.tasks.find(item => item.taskId === ctx.params.jobId);
    task.state = "canceled";
    task.revision += 1;
    return task;
  },
  "POST /api/v1/tasks/{jobId}/resume": ctx => {
    const task = state.tasks.find(item => item.taskId === ctx.params.jobId);
    task.state = "queued";
    task.revision += 1;
    return task;
  },
  "POST /api/v1/tasks/preview": ctx => {
    const ids = ctx.body?.target?.nodeIds ?? ["n-1"];
    return { targetNodeIdsSnapshot: ids, eligibleNodeIds: ids.filter(nodeId => nodeById(nodeId)?.online), deniedNodeIds: ids.filter(nodeId => !nodeById(nodeId)?.online), requiresApproval: true };
  },
  "POST /api/v1/tasks": ctx => {
    const jobId = id("task");
    const task = {
      taskId: jobId,
      state: "awaiting_approval",
      createdAt: NOW,
      createdBy: "u-admin",
      targetNodeIdsSnapshot: [...(ctx.body?.target?.nodeIds ?? [])],
      revision: 1,
      planDigest: "digest-new",
    };
    state.tasks.unshift(task);
    return http(202, { jobId, name: ctx.body?.name, targetType: ctx.body?.target?.type ?? "node_ids", targetNodeIdsSnapshot: task.targetNodeIdsSnapshot, state: task.state, createdAt: NOW });
  },

  // ---------- 文件 ----------
  "GET /api/v1/nodes/{nodeId}/files": ctx => {
    const path = ctx.query.get("path") ?? "/";
    const entries = path === "/"
      ? [
          { name: "srv", path: "/srv", type: "directory", sizeBytes: null, modifiedAt: "2026-10-29T10:00:00Z" },
          { name: "var", path: "/var", type: "directory", sizeBytes: null, modifiedAt: "2026-10-29T10:00:00Z" },
          { name: "etc", path: "/etc", type: "symlink", sizeBytes: 8, modifiedAt: "2026-09-01T10:00:00Z" },
        ]
      : [
          { name: "index.html", path: `${path === "/" ? "" : path}/index.html`, type: "file", sizeBytes: 5120, modifiedAt: "2026-10-30T01:00:00Z", version: "v3", permissions: "rw-r--r--" },
          { name: "assets", path: `${path === "/" ? "" : path}/assets`, type: "directory", sizeBytes: null, modifiedAt: "2026-10-30T00:30:00Z" },
        ];
    const search = (ctx.query.get("search") ?? "").toLowerCase();
    return { nodeId: ctx.params.nodeId, path, entries: search ? entries.filter(entry => entry.name.toLowerCase().includes(search)) : entries, nextCursor: null };
  },
  "POST /api/v1/nodes/{nodeId}/files/size": ctx => ({ path: ctx.body?.path ?? "/", sizeBytes: 734003200, itemsScanned: 41 }),
  "POST /api/v1/nodes/{nodeId}/files/directories": ctx => http(201, { name: ctx.body?.name, path: `${ctx.body?.parentPath === "/" ? "" : ctx.body?.parentPath}/${ctx.body?.name}`, type: "directory", sizeBytes: null, modifiedAt: NOW }),
  "PATCH /api/v1/nodes/{nodeId}/files/rename": ctx => ({ name: ctx.body?.newName, path: `${ctx.body?.path?.replace(/[^/]+$/, "")}${ctx.body?.newName}`, type: "file", sizeBytes: 5120, modifiedAt: NOW }),
  "DELETE /api/v1/nodes/{nodeId}/files": () => http(204, undefined),
  "POST /api/v1/nodes/{nodeId}/files/uploads": ctx => http(201, { uploadId: id("up"), status: "awaiting_data", expiresAt: "2026-10-30T03:00:00Z", files: ctx.body?.files }),
  "GET /api/v1/transfers": () => ({ items: state.transfers }),
  "POST /api/v1/transfers": ctx => {
    const transfer = { id: id("tr"), ...ctx.body, status: "queued", progressPercent: 0, copiedBytes: 0, createdAt: NOW };
    state.transfers.unshift(transfer);
    return http(202, transfer);
  },
  "GET /api/v1/transfers/{transferId}": ctx => state.transfers.find(item => item.id === ctx.params.transferId) ?? http(404, { code: "NOT_FOUND", message: "传输不存在" }),
  "POST /api/v1/transfers/{transferId}/cancel": ctx => {
    const transfer = state.transfers.find(item => item.id === ctx.params.transferId);
    transfer.status = "cancelled";
    transfer.completedAt = NOW;
    return transfer;
  },

  // ---------- 监控 / 通知 ----------
  "GET /api/v1/availability-monitors": () => state.monitors,
  "POST /api/v1/availability-monitors": ctx => http(201, { id: id("mon"), ...ctx.body, state: "pending", failureCount: 0, recoveryCount: 0 }),
  "PATCH /api/v1/availability-monitors/{monitorId}": ctx => ({ ...state.monitors[0], ...ctx.body, id: ctx.params.monitorId }),
  "DELETE /api/v1/availability-monitors/{monitorId}": () => ({ ok: true }),
  "GET /api/v1/availability-monitors/{monitorId}/checks": () => ({ ok: true }),
  "GET /api/v1/notifications/channels": () => ({ items: state.channels }),
  "PUT /api/v1/notifications/channels/{channel}": ctx => {
    const channel = state.channels.find(item => item.channel === ctx.params.channel);
    Object.assign(channel, ctx.body);
    return channel;
  },
  "PUT /api/v1/notifications/channels/{channel}/credentials": () => http(204, undefined),
  "POST /api/v1/notifications/channels/{channel}/test": ctx => {
    const delivery = { id: id("dl"), channel: ctx.params.channel, status: ctx.params.channel === "smtp" ? "provider_accepted" : "queued", providerCode: "250", message: "验收桩：平台已接受，无法验证最终送达", createdAt: NOW };
    state.deliveries.unshift(delivery);
    return delivery;
  },
  "GET /api/v1/notifications/settings": () => ({ smtpFallbackOnQQFailure: true, maxRetryAttempts: 3, deduplicationWindowSeconds: 300 }),
  "PUT /api/v1/notifications/settings": ctx => ctx.body,
  "GET /api/v1/notifications/deliveries": () => ({ items: state.deliveries }),
  "GET /api/v1/traffic-risks": () => [{ nodeId: "n-3", kind: "quota_near_limit", severity: "warning", estimated: true }],

  // ---------- DNS ----------
  "GET /api/v1/dns/providers": () => ({ items: [{ code: "cloudflare", displayName: "Cloudflare", supported: true, capabilities: { supportsProxy: true, supportsZoneSync: true, supportedRecordTypes: ["A", "AAAA", "CNAME", "TXT", "MX"] } }] }),
  "GET /api/v1/dns/accounts": () => ({ items: state.dnsAccounts }),
  "POST /api/v1/dns/accounts": ctx => {
    const account = { id: id("da"), providerCode: "cloudflare", displayName: ctx.body.displayName, note: ctx.body.note, enabled: ctx.body.enabled ?? true, isDefault: false, credentialStatus: "not_configured", zoneCount: 0 };
    state.dnsAccounts.push(account);
    return http(201, account);
  },
  "PATCH /api/v1/dns/accounts/{accountId}": ctx => {
    const account = state.dnsAccounts.find(item => item.id === ctx.params.accountId);
    Object.assign(account, ctx.body);
    return account;
  },
  "DELETE /api/v1/dns/accounts/{accountId}": ctx => {
    state.dnsAccounts = state.dnsAccounts.filter(item => item.id !== ctx.params.accountId);
    return http(204, undefined);
  },
  "PUT /api/v1/dns/accounts/{accountId}/credentials": () => http(204, undefined),
  "PUT /api/v1/dns/accounts/{accountId}/default": ctx => {
    for (const account of state.dnsAccounts) account.isDefault = account.id === ctx.params.accountId;
    return state.dnsAccounts.find(item => item.id === ctx.params.accountId);
  },
  "POST /api/v1/dns/accounts/{accountId}/test": () => ({ status: "partial", zoneRead: true, recordWrite: false, message: "验收桩：有 Zone 读取权限，缺少记录写入权限", checkedAt: NOW }),
  "GET /api/v1/dns/accounts/{accountId}/zones": ctx => ({ items: state.zones.filter(zone => zone.accountId === ctx.params.accountId) }),
  "POST /api/v1/dns/accounts/{accountId}/zones/sync": ctx => ({ items: state.zones.filter(zone => zone.accountId === ctx.params.accountId) }),
  "GET /api/v1/dns/zones/{zoneId}/records": ctx => ({ items: state.records.filter(record => record.zoneId === ctx.params.zoneId) }),
  "POST /api/v1/dns/zones/{zoneId}/records": ctx => {
    const record = { id: id("rec"), zoneId: ctx.params.zoneId, ...ctx.body, modifiedAt: NOW };
    state.records.push(record);
    return http(201, record);
  },
  "PATCH /api/v1/dns/zones/{zoneId}/records/{recordId}": ctx => {
    const record = state.records.find(item => item.id === ctx.params.recordId);
    Object.assign(record, ctx.body, { modifiedAt: NOW });
    return record;
  },
  "DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}": ctx => {
    state.records = state.records.filter(item => item.id !== ctx.params.recordId);
    return http(204, undefined);
  },

  // ---------- 网关 ----------
  "GET /api/v1/gateways": () => ({ items: state.gateways }),
  "GET /api/v1/gateways/{gatewayId}": ctx => state.gateways.find(item => item.id === ctx.params.gatewayId) ?? http(404, { code: "NOT_FOUND", message: "网关不存在" }),
  "POST /api/v1/gateways": ctx => http(202, { id: id("prov"), nodeId: ctx.body?.nodeId, gatewayId: "gw-new", status: "queued", phase: "queued", createdAt: NOW }),
  "PATCH /api/v1/gateways/{gatewayId}": ctx => {
    const gateway = state.gateways.find(item => item.id === ctx.params.gatewayId);
    gateway.enabled = ctx.body?.enabled;
    gateway.schedulingEligible = gateway.enabled && gateway.status === "ready" && gateway.checksPassed;
    return gateway;
  },
  "POST /api/v1/gateways/{gatewayId}/provision": ctx => http(202, { id: id("prov"), nodeId: "n-4", gatewayId: ctx.params.gatewayId, status: "installing", phase: "install", createdAt: NOW }),
  "GET /api/v1/gateways/{gatewayId}/checks": () => ({ nginxInstalled: true, serviceRunning: true, configValid: true, healthProbePassed: true, checkedAt: NOW }),
  "GET /api/v1/gateway-provisions/{jobId}": ctx => ({ id: ctx.params.jobId, nodeId: "n-4", gatewayId: "gw-2", status: "succeeded", phase: "finished", createdAt: NOW, checks: { nginxInstalled: true, serviceRunning: true, configValid: true, healthProbePassed: true, checkedAt: NOW } }),
  "GET /api/v1/gateway-routes": () => ({ items: state.routes }),
  "POST /api/v1/gateway-routes": ctx => {
    const route = { id: id("route"), ...ctx.body };
    state.routes.push(route);
    return http(201, route);
  },
  "PATCH /api/v1/gateway-routes/{routeId}": ctx => {
    const route = state.routes.find(item => item.id === ctx.params.routeId);
    Object.assign(route, ctx.body);
    return route;
  },
  "DELETE /api/v1/gateway-routes/{routeId}": ctx => {
    state.routes = state.routes.filter(item => item.id !== ctx.params.routeId);
    return http(204, undefined);
  },

  // ---------- 证书 ----------
  "GET /api/v1/certificates/internal/distributions": () => ({ items: state.certJobs }),
  "POST /api/v1/certificates/internal/distributions": ctx => {
    const group = state.serviceGroups.find(item => item.id === ctx.body?.serviceGroupId);
    const job = {
      jobId: id("cert"),
      serviceGroupId: ctx.body?.serviceGroupId,
      memberNodeIdsSnapshot: [...(group?.memberNodeIds ?? [])],
      credentialType: ctx.body?.credentialType ?? "trust_bundle",
      state: "queued",
      createdAt: NOW,
      requestedBy: "u-admin",
      results: (group?.memberNodeIds ?? []).map(nodeId => ({ nodeId, state: nodeById(nodeId)?.online ? "queued" : "waiting_offline" })),
    };
    state.certJobs.unshift(job);
    return http(202, job);
  },
  "GET /api/v1/certificates/internal/distributions/{jobId}": ctx => state.certJobs.find(item => item.jobId === ctx.params.jobId) ?? http(404, { code: "NOT_FOUND", message: "证书任务不存在" }),
  "POST /api/v1/certificates/internal/distributions/{jobId}/revoke": ctx => {
    const job = state.certJobs.find(item => item.jobId === ctx.params.jobId);
    job.state = "revoking";
    return http(202, job);
  },
  "POST /api/v1/certificates/internal/distributions/{jobId}/rotate": ctx => {
    const job = state.certJobs.find(item => item.jobId === ctx.params.jobId);
    job.state = "running";
    return http(202, job);
  },

  // ---------- 发布 ----------
  "GET /api/v1/publications": () => state.publications,
  "POST /api/v1/publications/preview": ctx => ({
    eligible: true,
    targetNodeIdsSnapshot: ["n-1", "n-2"],
    configDiff: [`+ server www.${ctx.body?.domain ?? "example.com"}`, "+ proxy_pass http://sg-1"],
    checks: [{ name: "DNS 账号可用", passed: true }, { name: "证书已签发", passed: Boolean(ctx.body?.certificateId), reason: ctx.body?.certificateId ? undefined : "未选择证书" }],
    warnings: ["验收桩：发布端口未在契约中声明，以服务组端口为准"],
  }),
  "POST /api/v1/publications": () => http(202, { id: id("pub"), status: "queued", createdAt: NOW, stages: [{ name: "dns", status: "queued" }, { name: "gateway", status: "queued" }], auditId: "aud-pub-new" }),
  "GET /api/v1/publications/{publicationId}": ctx => state.publications.find(item => item.id === ctx.params.publicationId) ?? { id: ctx.params.publicationId, status: "running", createdAt: NOW, stages: [{ name: "dns", status: "succeeded" }, { name: "gateway", status: "running" }] },

  // ---------- AI ----------
  "GET /api/v1/ai/security-policy": () => ({ defaultDeny: true, serverSideEnforced: true, groupGrantMemberPolicy: "frozen_snapshot_intersection", allowUnscopedDiscovery: false, scopeRevisionRequired: true, lateralMovementRestricted: true, approvalPolicyServerEnforced: true, noScopeExpansionFromApprovalMode: true }),
  "POST /api/v1/ai/sessions": () => {
    const sessionId = id("ai");
    state.aiSessions[sessionId] = { id: sessionId, scopeRevision: 1, resourceGrants: [], status: "active", createdBy: "u-admin", createdAt: NOW };
    return http(201, aiSession(sessionId));
  },
  "GET /api/v1/ai/sessions/{sessionId}": ctx => aiSession(ctx.params.sessionId) ?? http(404, { code: "NOT_FOUND", message: "AI 会话不存在" }),
  "POST /api/v1/ai/sessions/{sessionId}/grants": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    const grant = { grantId: id("gr"), resourceType: ctx.body?.resourceType, resourceId: ctx.body?.resourceId, allowedActions: ctx.body?.allowedActions ?? ["nodes.read"], grantedAt: NOW, grantedBy: "u-admin" };
    if (ctx.body?.resourceType === "service_group") {
      const group = state.serviceGroups.find(item => item.id === ctx.body?.resourceId);
      grant.memberNodeIdsSnapshot = [...(group?.memberNodeIds ?? [])];
    }
    record.resourceGrants.push(grant);
    record.scopeRevision += 1;
    return aiSession(ctx.params.sessionId);
  },
  "DELETE /api/v1/ai/sessions/{sessionId}/grants/{grantId}": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    record.resourceGrants = record.resourceGrants.filter(item => item.grantId !== ctx.params.grantId);
    record.scopeRevision += 1;
    return aiSession(ctx.params.sessionId);
  },
  "POST /api/v1/ai/sessions/{sessionId}/resource-recommendations": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    const suggestion = { id: id("rec"), reason: ctx.body?.reason ?? "需要更多指标", resourceType: "node", recommendedCapability: "nodes.read", metadataOnly: true, status: "pending" };
    record.recommendations = [...(record.recommendations ?? []), suggestion];
    return { suggestions: [suggestion] };
  },
  "POST /api/v1/ai/sessions/{sessionId}/resource-recommendations/{recommendationId}/decision": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    const decisions = (record.decisions ??= {});
    if (decisions[ctx.params.recommendationId]) return http(409, { code: "ALREADY_DECIDED", message: "该建议已处理" });
    decisions[ctx.params.recommendationId] = ctx.body?.approved ? "approved" : "rejected";
    return { id: ctx.params.recommendationId, status: decisions[ctx.params.recommendationId] };
  },
  "GET /api/v1/ai/sessions/{sessionId}/approval-policy": ctx => aiSession(ctx.params.sessionId)?.approvalPolicy ?? policyDefault(ctx.params.sessionId),
  "PATCH /api/v1/ai/sessions/{sessionId}/approval-policy": ctx => {
    const sessionId = ctx.params.sessionId;
    const record = state.aiSessions[sessionId];
    if (!record) return http(404, { code: "NOT_FOUND", message: "AI 会话不存在" });
    if (ctx.body?.mode === "full_access" && !ctx.body?.userConfirmedFullAccess) return http(422, { code: "CONFIRMATION_REQUIRED", message: "完全访问必须由管理员显式确认" });
    record.approvalPolicy = {
      sessionId,
      mode: ctx.body.mode,
      effectiveMode: ctx.body.mode,
      expiresAt: ctx.body.mode === "full_access" ? new Date(Date.now() + (ctx.body.durationSeconds ?? 900) * 1000).toISOString() : null,
      scopeRevision: record.scopeRevision,
      maxFullAccessSeconds: 3600,
      enforcedRestrictions: ["root shell 仍需人工确认"],
    };
    return record.approvalPolicy;
  },
  "POST /api/v1/ai/sessions/{sessionId}/approvals": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    const approval = { id: id("ap"), sessionId: record.id, state: "pending", toolName: ctx.body?.toolName ?? "metrics.read", canonicalArgumentsHash: `hash-${state.nextId}`, targetNodeIdsSnapshot: [...(ctx.body?.targetNodeIds ?? [])], scopeRevision: record.scopeRevision, riskLevel: "read", expiresAt: "2026-10-30T04:00:00Z", createdBy: "u-admin" };
    state.approvals.push(approval);
    return http(201, approval);
  },
  "POST /api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision": ctx => {
    const approval = state.approvals.find(item => item.id === ctx.params.approvalId);
    if (!approval) return http(404, { code: "NOT_FOUND", message: "审批不存在" });
    if (approval.state !== "pending") return http(409, { code: "ALREADY_DECIDED", message: "批准凭证只能使用一次" });
    approval.state = ctx.body?.decision === "approve" ? "approved" : "denied";
    return approval;
  },
  "GET /api/v1/ai/sessions/{sessionId}/approval-events": () => ({
    items: [
      { id: "ae-1", sessionId: "ai-11", actor: "u-admin", action: "approval.create", mode: "per_action", toolName: "metrics.read", targetNodeIds: ["n-1"], result: "approved", timestamp: "2026-10-30T01:20:00Z" },
      { id: "ae-2", sessionId: "ai-11", actor: "ai", action: "tools.execute", mode: "automatic", toolName: "list.nodes", result: "auto_approved", timestamp: "2026-10-30T01:10:00Z" },
    ],
  }),
  "POST /api/v1/ai/tools/execute": ctx => {
    const record = state.aiSessions[ctx.body?.sessionId];
    if (!record) return http(404, { code: "NOT_FOUND", message: "AI 会话不存在" });
    if (ctx.body?.scopeRevision !== record.scopeRevision) return http(412, { code: "SCOPE_REVISION_STALE", message: "资源授权已变化，旧计划不能继续执行" });
    return { accepted: true, validatedNodeIds: [...(ctx.body?.targetNodeIds ?? [])], state: "queued", scopeRevision: record.scopeRevision };
  },
  "GET /api/v1/ai/sessions/{sessionId}/messages": ctx => {
    const record = state.aiSessions[ctx.params.sessionId];
    const stored = state.aiMessages.filter(message => message.sessionId === ctx.params.sessionId);
    const seeded = record?.seeded ? [] : [{ messageId: "m-seed", sessionId: ctx.params.sessionId, role: "assistant", createdAt: "2026-10-30T01:00:00Z", redactedContent: "验收桩：已授权资源之外的节点不会返回任何信息。" }];
    if (record) record.seeded = true;
    return page([...seeded, ...stored]);
  },
  "POST /api/v1/ai/sessions/{sessionId}/messages": ctx => {
    const message = { messageId: id("m"), sessionId: ctx.params.sessionId, role: "user", createdAt: NOW, redactedContent: String(ctx.body?.content ?? "").slice(0, 200) };
    state.aiMessages.push(message);
    const reply = { messageId: id("m"), sessionId: ctx.params.sessionId, role: "assistant", createdAt: NOW, redactedContent: "验收桩回复：只处理当前会话已授权范围内的资源。" };
    state.aiMessages.push(reply);
    return http(201, reply);
  },

  // ---------- 插件 ----------
  "GET /api/v1/plugins/repository": () => ({ mode: "official", ref: "main", path: "Plugin", resolvedCommit: "c0ffee", officialVerified: true }),
  "PUT /api/v1/plugins/repository": ctx => ({ mode: ctx.body?.mode ?? "official", url: ctx.body?.url, ref: ctx.body?.ref ?? "main", path: "Plugin", resolvedCommit: "c0ffee", officialVerified: ctx.body?.mode !== "custom" }),
  "GET /api/v1/plugins/catalog": () => ({
    items: [
      { id: "xray-manager", name: "Xray 节点管理器", version: "0.1.0", description: "官方可选插件", minControllerVersion: "0.9.0", entryType: "declarative", manifestHash: "mh-1", artifactSha256: "sha-abc", signature: "sig-1", permissions: ["plugins.node.manage"], sourceCommit: "c0ffee" },
      { id: "xray-manager", name: "Xray 节点管理器（预发布）", version: "0.2.0-preview", description: "尚未发布发行制品", entryType: "declarative", manifestHash: "mh-2", permissions: ["plugins.node.manage"] },
    ],
  }),
  "POST /api/v1/plugins/catalog/refresh": () => ({ items: [] }),
  "GET /api/v1/plugins": () => ({ items: state.plugins }),
  "GET /api/v1/plugins/{pluginId}": ctx => state.plugins.find(item => item.id === ctx.params.pluginId) ?? http(404, { code: "NOT_FOUND", message: "插件未安装" }),
  "POST /api/v1/plugins/{pluginId}/install": ctx => {
    const plugin = { id: ctx.params.pluginId, version: ctx.body?.version, status: ctx.body?.enableAfterInstall ? "enabled" : "installed_disabled", enabled: Boolean(ctx.body?.enableAfterInstall), sourceCommit: ctx.body?.sourceCommit, permissions: [...(ctx.body?.acceptedPermissions ?? [])], installedAt: NOW };
    const existing = state.plugins.findIndex(item => item.id === plugin.id);
    if (existing >= 0) state.plugins[existing] = plugin;
    else state.plugins.push(plugin);
    const op = { id: id("op"), pluginId: plugin.id, action: "install", status: "verifying", targetVersion: plugin.version };
    state.pluginOps.push(op);
    return http(202, op);
  },
  "PUT /api/v1/plugins/{pluginId}/activation": ctx => {
    const plugin = state.plugins.find(item => item.id === ctx.params.pluginId);
    if (!plugin) return http(404, { code: "NOT_FOUND", message: "插件未安装" });
    plugin.enabled = Boolean(ctx.body?.enabled);
    plugin.status = plugin.enabled ? "enabled" : "installed_disabled";
    const op = { id: id("op"), pluginId: plugin.id, action: plugin.enabled ? "activate" : "deactivate", status: "applying" };
    state.pluginOps.push(op);
    return http(202, op);
  },
  "POST /api/v1/plugins/{pluginId}/update": ctx => {
    const plugin = state.plugins.find(item => item.id === ctx.params.pluginId);
    if (!plugin) return http(404, { code: "NOT_FOUND", message: "插件未安装" });
    const op = { id: id("op"), pluginId: plugin.id, action: "update", status: "queued", previousVersion: plugin.version, targetVersion: ctx.body?.version };
    plugin.status = "updating";
    state.pluginOps.push(op);
    return http(202, op);
  },
  "DELETE /api/v1/plugins/{pluginId}": ctx => {
    state.plugins = state.plugins.filter(item => item.id !== ctx.params.pluginId);
    const op = { id: id("op"), pluginId: ctx.params.pluginId, action: "uninstall", status: "queued" };
    state.pluginOps.push(op);
    return http(202, op);
  },
  "GET /api/v1/plugin-operations/{operationId}": ctx => state.pluginOps.find(item => item.id === ctx.params.operationId) ?? { id: ctx.params.operationId, pluginId: "xray-manager", action: "install", status: "succeeded" },

  // ---------- Xray（声明式插件） ----------
  "GET /api/v1/plugins/xray-manager/capabilities": () => ({ install: true, configure: true, users: true, traffic: true, clientExport: true }),
  "GET /api/v1/plugins/xray-manager/nodes": () => ({
    items: NODES.map(node => ({ nodeId: node.id, name: node.name, online: node.online, managed: node.online, provider: node.provider, region: node.region, arch: "amd64", xrayDetected: node.online })),
  }),
  "GET /api/v1/plugins/xray-manager/nodes/{nodeId}/instance": ctx => ({ nodeId: ctx.params.nodeId, managed: true, adoptionState: "managed", version: "25.9.11", serviceStatus: nodeById(ctx.params.nodeId)?.online ? "running" : "unknown", architecture: "amd64", configRevision: 4, inboundCount: 2 }),
  "POST /api/v1/plugins/xray-manager/nodes/{nodeId}/instance/actions": ctx => {
    const action = ctx.body?.action;
    const op = { jobId: id("xjob"), taskId: "task-2", accepted: true, targetNodeIdsSnapshot: [ctx.params.nodeId], status: "queued", correlationId: "corr-1" };
    if (action === "stop") op.status = "queued";
    return http(202, op);
  },
  "GET /api/v1/plugins/xray-manager/templates": () => ({
    items: [
      { templateId: "vless-reality-vision", name: "VLESS + Reality", security: "reality", flow: "xtls-rprx-vision", supportedVersions: ["25.9.11"], requiredParameters: ["serverName", "privateKey"] },
      { templateId: "vless-tls-vision", name: "VLESS + TLS", security: "tls", flow: "xtls-rprx-vision", supportedVersions: ["25.9.11"], requiredParameters: ["domain", "certificate"] },
      { templateId: "custom-json", name: "自定义 JSON", security: "custom" },
    ],
  }),
  "POST /api/v1/plugins/xray-manager/installations/preview": ctx => http(200, {
    previewId: id("prev"),
    expiresAt: "2026-10-30T03:00:00Z",
    targetNodeIdsSnapshot: ctx.body?.targetNodeIds ?? [],
    checks: (ctx.body?.targetNodeIds ?? []).map(nodeId => ({ nodeId, eligible: Boolean(nodeById(nodeId)?.online), existingInstallation: false, hasPortConflict: false })),
    eligible: true,
    requiresAdoptionApproval: false,
    planHash: "plan-1",
  }),
  "POST /api/v1/plugins/xray-manager/installations": () => http(202, { jobId: id("xjob"), taskId: "task-2", accepted: true, targetNodeIdsSnapshot: ["n-1"], status: "queued", correlationId: "corr-install" }),
  "POST /api/v1/plugins/xray-manager/configurations/validate": ctx => ({ valid: true, sanitizedDiff: "+ inbound vless-reality-vision", supported: true, notes: ["验收桩：真实校验由插件执行"], perNode: [{ nodeId: "n-1", eligible: true }] }),
  "POST /api/v1/plugins/xray-manager/deployments/preview": ctx => ({ previewId: id("prev"), targetNodeIdsSnapshot: ctx.body?.targetNodeIds ?? ["n-1"], expiresAt: "2026-10-30T03:00:00Z", sanitizedDiff: "+ route rule", checks: [{ nodeId: "n-1", eligible: true }], planHash: "plan-2", valid: true }),
  "POST /api/v1/plugins/xray-manager/deployments": () => http(202, { jobId: id("xjob"), taskId: "task-2", accepted: true, targetNodeIdsSnapshot: ["n-1"], status: "queued" }),
  "GET /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations": ctx => ({
    items: [
      { revision: 4, nodeId: ctx.params.nodeId, configHash: "hash-4", summary: "启用 Reality 入站", createdAt: "2026-10-29T08:00:00Z", createdBy: "u-admin", status: "active", templateId: "vless-reality-vision" },
      { revision: 3, nodeId: ctx.params.nodeId, configHash: "hash-3", summary: "回滚前配置", createdAt: "2026-10-28T08:00:00Z", createdBy: "u-admin", status: "archived" },
    ],
  }),
  "POST /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations/rollback": ctx => http(202, { jobId: id("xjob"), accepted: true, targetNodeIdsSnapshot: [ctx.params.nodeId], status: "queued" }),
  "GET /api/v1/plugins/xray-manager/users": () => ({ items: state.xrayUsers }),
  "POST /api/v1/plugins/xray-manager/users": ctx => {
    const user = { userId: id("xu"), nodeId: ctx.body?.nodeId ?? "n-1", inboundTag: ctx.body?.inboundTag ?? "vless-in", displayName: ctx.body?.displayName ?? "新用户", statsIdentifier: id("stats"), enabled: true, createdAt: NOW };
    state.xrayUsers.push(user);
    return http(201, user);
  },
  "PATCH /api/v1/plugins/xray-manager/users/{userId}": ctx => {
    const user = state.xrayUsers.find(item => item.userId === ctx.params.userId);
    Object.assign(user, ctx.body);
    return user;
  },
  "DELETE /api/v1/plugins/xray-manager/users/{userId}": ctx => {
    state.xrayUsers = state.xrayUsers.filter(item => item.userId !== ctx.params.userId);
    return http(202, { jobId: id("xjob"), accepted: true, targetNodeIdsSnapshot: ["n-1"], status: "queued" });
  },
  "POST /api/v1/plugins/xray-manager/users/{userId}/client-export": ctx => ({ format: "uri", oneTimeValue: "vless://stub-once-9f2c@example.com:443?security=reality#stub", expiresAt: "2026-10-30T02:10:00Z" }),
  "GET /api/v1/plugins/xray-manager/traffic": () => ({
    items: [
      { nodeId: "n-1", inboundTag: "vless-in", scope: "inbound", uplinkBytes: 104857600, downlinkBytes: 524288000, sampledAt: "2026-10-30T01:55:00Z", complete: true, source: "xray-stats", billingEquivalent: false },
      { nodeId: "n-2", inboundTag: "vless-in", scope: "inbound", uplinkBytes: 0, downlinkBytes: 0, sampledAt: "2026-10-30T01:55:00Z", complete: false, gapReason: "节点离线，采样不完整", source: "xray-stats", billingEquivalent: false },
    ],
    nextCursor: undefined,
    sourceType: "proxy_only",
    notice: "仅为代理统计，不等同云厂商账单",
  }),
  "GET /api/v1/plugins/xray-manager/traffic/settings": () => ({ samplingIntervalSeconds: 60, enableUserStats: true, enableInboundStats: true, enableOutboundStats: false, thresholdsBytes: [1073741824] }),
  "PUT /api/v1/plugins/xray-manager/traffic/settings": ctx => ({ samplingIntervalSeconds: 60, ...ctx.body }),
  "GET /api/v1/plugins/xray-manager/jobs/{jobId}": ctx => ({ jobId: ctx.params.jobId, taskId: "task-2", accepted: true, targetNodeIdsSnapshot: ["n-1"], status: "running", correlationId: "corr-1" }),

  // ---------- 主控更新 ----------
  "POST /api/v1/system/updates/check": ctx => ({
    currentVersion: "0.8.0",
    latestVersion: "0.9.0",
    channel: ctx.body?.channel ?? "stable",
    updateAvailable: true,
    releaseNotes: "0.9 管理台、插件平台与更新中心。",
    publishedAt: "2026-10-20T00:00:00Z",
    signatureVerified: true,
    platformCompatible: true,
    checkedAt: NOW,
  }),
  "POST /api/v1/system/updates/plan": ctx => ({
    planId: id("pl"),
    targetVersion: ctx.body?.targetVersion,
    eligible: true,
    migrationReversible: true,
    preflightChecks: [{ name: "磁盘空间", passed: true }, { name: "任务排空", passed: true }, { name: "迁移兼容", passed: true }],
    estimatedImpact: "管理面重启约 30 秒",
    backupRequired: true,
  }),
  "POST /api/v1/system/updates/apply": ctx => {
    const job = { jobId: id("ujob"), fromVersion: "0.8.0", toVersion: "0.9.0", state: "queued", phase: "queued", backupId: "bk-1", rollbackEligible: false, requestedBy: "u-admin", createdAt: NOW, updatedAt: NOW };
    state.updateJobs.unshift(job);
    return http(202, job);
  },
  "GET /api/v1/system/updates/jobs": () => ({ items: state.updateJobs }),
  "GET /api/v1/system/updates/jobs/{jobId}": ctx => state.updateJobs.find(item => item.jobId === ctx.params.jobId) ?? http(404, { code: "NOT_FOUND", message: "更新作业不存在" }),
  "POST /api/v1/system/updates/jobs/{jobId}/rollback": ctx => {
    const job = state.updateJobs.find(item => item.jobId === ctx.params.jobId);
    if (!job) return http(404, { code: "NOT_FOUND", message: "更新作业不存在" });
    job.state = "rolling_back";
    job.updatedAt = NOW;
    return http(202, job);
  },

  // ---------- 备份 / 灾难恢复 ----------
  "POST /api/v1/backups/{backupId}/verify": ctx => {
    if (ctx.params.backupId === "bk-missing") return http(404, { code: "NOT_FOUND", message: "备份不存在" });
    return http(202, {
      backupId: ctx.params.backupId,
      targetControllerId: "ctl-2",
      oldControllerFenced: false,
      expectedVersion: "0.9.0",
      expectedNodeIds: ["n-1", "n-2", "n-5"],
      recoveryKeyReference: "key-ref-1",
      checks: [{ name: "校验和", passed: true }, { name: "版本兼容", passed: true }],
    });
  },
  "POST /api/v1/disaster-recovery/preview": ctx => ({
    ...ctx.body,
    checks: [
      { name: "校验和", passed: true },
      { name: "旧主控隔离", passed: Boolean(ctx.body?.oldControllerFenced), reason: ctx.body?.oldControllerFenced ? undefined : "必须声明旧主控已隔离" },
      { name: "期望节点身份", passed: true },
    ],
  }),
  "POST /api/v1/disaster-recovery/jobs": ctx => {
    const job = { id: id("dr"), status: "running", createdAt: NOW, stages: [{ name: "fence", status: "succeeded" }, { name: "restore", status: "running" }], auditId: id("aud") };
    state.drJobs[job.id] = job;
    return http(202, job);
  },
  "GET /api/v1/disaster-recovery/jobs/{jobId}": ctx => state.drJobs[ctx.params.jobId] ?? { id: ctx.params.jobId, status: "succeeded", createdAt: NOW, stages: [{ name: "restore", status: "succeeded" }] },
};

export { state };
