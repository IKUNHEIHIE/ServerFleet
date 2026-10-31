import { apiClient } from "./client";
import { normalizeDnsProviders, normalizeZoneSync } from "./compat";
import type {
  AsyncOperation,
  AvailabilityMonitor,
  DNSAccount,
  DNSConnectionTest,
  DNSRecord,
  DNSRecordInput,
  DNSZone,
  GatewayHealthChecks,
  GatewayNode,
  GatewayProvisionJob,
  GatewayRoute,
  GatewayRouteInput,
  InternalCertificateJob,
  NotificationChannel,
  NotificationDelivery,
  NotificationSettings,
  PublicationDraft,
  PublicationPreview,
  QQBotConfig,
  SMTPConfig,
} from "./dto";

export const monitoringApi = {
  list: (signal?: AbortSignal) => apiClient.request<readonly AvailabilityMonitor[]>("/api/v1/availability-monitors", { signal }),
  create: (body: AvailabilityMonitor) =>
    apiClient.request<AvailabilityMonitor>("/api/v1/availability-monitors", { method: "POST", body }),
  update: (monitorId: string, body: AvailabilityMonitor) =>
    apiClient.request<AvailabilityMonitor>(`/api/v1/availability-monitors/${encodeURIComponent(monitorId)}`, {
      method: "PATCH",
      body,
    }),
  remove: (monitorId: string) =>
    apiClient.request<{ ok: boolean }>(`/api/v1/availability-monitors/${encodeURIComponent(monitorId)}`, { method: "DELETE" }),
  checks: (monitorId: string, signal?: AbortSignal) =>
    apiClient.request<{ ok: boolean }>(`/api/v1/availability-monitors/${encodeURIComponent(monitorId)}/checks`, { signal }),
};

export const notificationsApi = {
  channels: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly NotificationChannel[] }>("/api/v1/notifications/channels", { signal }),
  putChannel: (channel: "smtp" | "qqbot", body: { enabled: boolean; smtp?: SMTPConfig; qqbot?: QQBotConfig }) =>
    apiClient.request<NotificationChannel>(`/api/v1/notifications/channels/${channel}`, { method: "PUT", body }),
  // 真实 Controller：POST …/credentials + {secretRef}（凭据引用，绝不传输明文）。
  putSecret: (channel: "smtp" | "qqbot", secretRef: string) =>
    apiClient.request<NotificationChannel>(`/api/v1/notifications/channels/${channel}/credentials`, {
      method: "POST",
      body: { secretRef },
    }),
  test: (channel: "smtp" | "qqbot") => {
    // 真实 Controller：测试发送幂等键在请求体内（{idempotencyKey}），无 overrideTarget 扩展字段。
    const key = `notify-test-${channel}-${Date.now()}`;
    return apiClient.request<NotificationDelivery>(`/api/v1/notifications/channels/${channel}/test`, {
      method: "POST",
      body: { idempotencyKey: key },
      idempotencyKey: key,
    });
  },
  settings: (signal?: AbortSignal) => apiClient.request<NotificationSettings>("/api/v1/notifications/settings", { signal }),
  putSettings: (body: NotificationSettings) =>
    apiClient.request<NotificationSettings>("/api/v1/notifications/settings", { method: "PUT", body }),
  deliveries: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly NotificationDelivery[] }>("/api/v1/notifications/deliveries", { signal }),
};

export const dnsApi = {
  providers: (signal?: AbortSignal) =>
    apiClient
      .request<unknown>("/api/v1/dns/providers", { signal })
      .then(result => ({ ...result, data: normalizeDnsProviders(result.data, "dns providers") })),
  accounts: (signal?: AbortSignal) => apiClient.request<{ items: readonly DNSAccount[] }>("/api/v1/dns/accounts", { signal }),
  // 真实 Controller：{displayName, note, enabled, credentialRef, makeDefault}；凭据只接受 secret-store 引用，不接受明文 token。
  createAccount: (body: { providerCode: "cloudflare"; displayName: string; note?: string; enabled?: boolean; apiToken: string }) =>
    apiClient.request<DNSAccount>("/api/v1/dns/accounts", {
      method: "POST",
      body: {
        displayName: body.displayName,
        note: body.note ?? "",
        enabled: body.enabled ?? true,
        credentialRef: body.apiToken,
        makeDefault: false,
      },
    }),
  updateAccount: (accountId: string, body: { displayName?: string; note?: string; enabled?: boolean }) =>
    apiClient.request<DNSAccount>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}`, { method: "PATCH", body }),
  removeAccount: (accountId: string) =>
    apiClient.request<void>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}`, { method: "DELETE" }),
  // 真实 Controller 无独立换凭据端点：凭据更新走 PATCH 账号的 credentialRef。
  rotateCredential: (accountId: string, apiToken: string) =>
    apiClient.request<DNSAccount>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}`, {
      method: "PATCH",
      body: { credentialRef: apiToken },
    }),
  // 真实 Controller 的设默认是 POST。
  setDefault: (accountId: string) =>
    apiClient.request<DNSAccount>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/default`, { method: "POST" }),
  test: (accountId: string) =>
    apiClient.request<DNSConnectionTest>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/test`, { method: "POST" }),
  zones: (accountId: string, signal?: AbortSignal) =>
    apiClient.request<{ items: readonly DNSZone[] }>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/zones`, { signal }),
  // 真实 Controller：POST {providerZoneId}，返回单个 zone 对象；候选契约为 {items}。两者都归一化为列表。
  syncZones: (accountId: string, body: { providerZoneId: string }) =>
    apiClient
      .request<unknown>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/zones/sync`, { method: "POST", body })
      .then(result => ({
        ...result,
        data: normalizeZoneSync<DNSZone>(
          result.data,
          value => typeof value === "object" && value !== null && typeof (value as Record<string, unknown>).id === "string",
          "dns zone sync",
        ),
      })),
  records: (zoneId: string, signal?: AbortSignal) =>
    apiClient.request<{ items: readonly DNSRecord[] }>(`/api/v1/dns/zones/${encodeURIComponent(zoneId)}/records`, { signal }),
  createRecord: (zoneId: string, body: DNSRecordInput) =>
    apiClient.request<DNSRecord>(`/api/v1/dns/zones/${encodeURIComponent(zoneId)}/records`, { method: "POST", body }),
  updateRecord: (zoneId: string, recordId: string, body: DNSRecordInput) =>
    apiClient.request<DNSRecord>(`/api/v1/dns/zones/${encodeURIComponent(zoneId)}/records/${encodeURIComponent(recordId)}`, {
      method: "PATCH",
      body,
    }),
  removeRecord: (zoneId: string, recordId: string) =>
    apiClient.request<void>(`/api/v1/dns/zones/${encodeURIComponent(zoneId)}/records/${encodeURIComponent(recordId)}`, {
      method: "DELETE",
    }),
};

export const gatewaysApi = {
  list: (signal?: AbortSignal) => apiClient.request<{ items: readonly GatewayNode[] }>("/api/v1/gateways", { signal }),
  get: (gatewayId: string, signal?: AbortSignal) =>
    apiClient.request<GatewayNode>(`/api/v1/gateways/${encodeURIComponent(gatewayId)}`, { signal }),
  create: (body: { nodeId: string; acknowledgeInstallImpact: true; installIfMissing?: boolean; preserveExistingConfig?: boolean }) =>
    apiClient.request<GatewayProvisionJob>("/api/v1/gateways", { method: "POST", body }),
  setDispatch: (gatewayId: string, enabled: boolean) =>
    apiClient.request<GatewayNode>(`/api/v1/gateways/${encodeURIComponent(gatewayId)}`, { method: "PATCH", body: { enabled } }),
  provision: (gatewayId: string) =>
    apiClient.request<GatewayProvisionJob>(`/api/v1/gateways/${encodeURIComponent(gatewayId)}/provision`, { method: "POST" }),
  checks: (gatewayId: string, signal?: AbortSignal) =>
    apiClient.request<GatewayHealthChecks>(`/api/v1/gateways/${encodeURIComponent(gatewayId)}/checks`, { signal }),
  provisionJob: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<GatewayProvisionJob>(`/api/v1/gateway-provisions/${encodeURIComponent(jobId)}`, { signal }),
  routes: (signal?: AbortSignal) => apiClient.request<{ items: readonly GatewayRoute[] }>("/api/v1/gateway-routes", { signal }),
  // 真实 Controller：路由创建必须带 gatewayId（internal/gateway/http.go 强制校验）。
  createRoute: (body: GatewayRouteInput & { readonly gatewayId: string }) =>
    apiClient.request<GatewayRoute>("/api/v1/gateway-routes", {
      method: "POST",
      body: {
        gatewayId: body.gatewayId,
        domain: body.domain,
        upstream: body.upstream ?? "",
        serviceGroupId: body.serviceGroupId ?? "",
        https: body.https,
        enabled: body.enabled,
      },
    }),
  updateRoute: (routeId: string, body: GatewayRouteInput) =>
    apiClient.request<GatewayRoute>(`/api/v1/gateway-routes/${encodeURIComponent(routeId)}`, { method: "PATCH", body }),
  removeRoute: (routeId: string) =>
    apiClient.request<void>(`/api/v1/gateway-routes/${encodeURIComponent(routeId)}`, { method: "DELETE" }),
};

export const certificatesApi = {
  distributions: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly InternalCertificateJob[] }>("/api/v1/certificates/internal/distributions", { signal }),
  create: (
    body: {
      serviceGroupId: string;
      credentialType: "trust_bundle" | "per_node_identity";
      identityProfile?: string;
      validityHours?: number;
      confirmServiceImpact: boolean;
      expectedGroupRevision: number;
    },
    idempotencyKey: string,
  ) =>
    apiClient.request<InternalCertificateJob>("/api/v1/certificates/internal/distributions", {
      method: "POST",
      body,
      idempotencyKey,
    }),
  get: (jobId: string, signal?: AbortSignal) =>
    apiClient.request<InternalCertificateJob>(`/api/v1/certificates/internal/distributions/${encodeURIComponent(jobId)}`, { signal }),
  revoke: (jobId: string, body: { reason: string; includeAllIssuedLeafs?: boolean }, idempotencyKey: string) =>
    apiClient.request<InternalCertificateJob>(`/api/v1/certificates/internal/distributions/${encodeURIComponent(jobId)}/revoke`, {
      method: "POST",
      body,
      idempotencyKey,
    }),
  rotate: (jobId: string, body: { reason: string; targetExpiryDays?: number }, idempotencyKey: string) =>
    apiClient.request<InternalCertificateJob>(`/api/v1/certificates/internal/distributions/${encodeURIComponent(jobId)}/rotate`, {
      method: "POST",
      body,
      idempotencyKey,
    }),
};

export const publicationsApi = {
  list: (signal?: AbortSignal) => apiClient.request<readonly AsyncOperation[]>("/api/v1/publications", { signal }),
  preview: (body: PublicationDraft) =>
    apiClient.request<PublicationPreview>("/api/v1/publications/preview", { method: "POST", body }),
  create: (body: PublicationDraft) => apiClient.request<AsyncOperation>("/api/v1/publications", { method: "POST", body }),
  get: (publicationId: string, signal?: AbortSignal) =>
    apiClient.request<AsyncOperation>(`/api/v1/publications/${encodeURIComponent(publicationId)}`, { signal }),
};
