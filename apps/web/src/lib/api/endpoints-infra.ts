import { apiClient } from "./client";
import type {
  AsyncOperation,
  AvailabilityMonitor,
  DNSAccount,
  DNSConnectionTest,
  DNSProvider,
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
  putSecret: (channel: "smtp" | "qqbot", secret: string) =>
    apiClient.request<void>(`/api/v1/notifications/channels/${channel}/credentials`, { method: "PUT", body: { secret } }),
  test: (channel: "smtp" | "qqbot", body: { overrideTarget?: string } = {}) =>
    apiClient.request<NotificationDelivery>(`/api/v1/notifications/channels/${channel}/test`, {
      method: "POST",
      body,
      idempotencyKey: `notify-test-${channel}-${Date.now()}`,
    }),
  settings: (signal?: AbortSignal) => apiClient.request<NotificationSettings>("/api/v1/notifications/settings", { signal }),
  putSettings: (body: NotificationSettings) =>
    apiClient.request<NotificationSettings>("/api/v1/notifications/settings", { method: "PUT", body }),
  deliveries: (signal?: AbortSignal) =>
    apiClient.request<{ items: readonly NotificationDelivery[] }>("/api/v1/notifications/deliveries", { signal }),
};

export const dnsApi = {
  providers: (signal?: AbortSignal) => apiClient.request<{ items: readonly DNSProvider[] }>("/api/v1/dns/providers", { signal }),
  accounts: (signal?: AbortSignal) => apiClient.request<{ items: readonly DNSAccount[] }>("/api/v1/dns/accounts", { signal }),
  createAccount: (body: { providerCode: "cloudflare"; displayName: string; note?: string; enabled?: boolean; apiToken: string }) =>
    apiClient.request<DNSAccount>("/api/v1/dns/accounts", { method: "POST", body }),
  updateAccount: (accountId: string, body: { displayName?: string; note?: string; enabled?: boolean }) =>
    apiClient.request<DNSAccount>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}`, { method: "PATCH", body }),
  removeAccount: (accountId: string) =>
    apiClient.request<void>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}`, { method: "DELETE" }),
  rotateCredential: (accountId: string, apiToken: string) =>
    apiClient.request<void>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/credentials`, {
      method: "PUT",
      body: { apiToken },
    }),
  setDefault: (accountId: string) =>
    apiClient.request<DNSAccount>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/default`, { method: "PUT" }),
  test: (accountId: string) =>
    apiClient.request<DNSConnectionTest>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/test`, { method: "POST" }),
  zones: (accountId: string, signal?: AbortSignal) =>
    apiClient.request<{ items: readonly DNSZone[] }>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/zones`, { signal }),
  syncZones: (accountId: string) =>
    apiClient.request<{ items: readonly DNSZone[] }>(`/api/v1/dns/accounts/${encodeURIComponent(accountId)}/zones/sync`, { method: "POST" }),
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
  createRoute: (body: GatewayRouteInput) =>
    apiClient.request<GatewayRoute>("/api/v1/gateway-routes", { method: "POST", body }),
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
