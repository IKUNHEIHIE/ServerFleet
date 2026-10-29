/** Controller 契约 DTO（仅声明控制台消费的字段；未知字段一律忽略）。 */

export interface PageMeta {
  readonly nextCursor: string | null;
  readonly limit: number;
  readonly hasMore: boolean;
}

export interface ItemList<T> {
  readonly items: readonly T[];
}

export interface Page<T> extends ItemList<T> {
  readonly page: PageMeta;
}

export interface Identity {
  readonly userId: string;
  readonly displayName: string;
  readonly roles: readonly string[];
  /** 服务端权限结果。缺失表示被告知“无结果”，不能由客户端按角色推断。 */
  readonly permissions?: readonly string[];
  readonly isBootstrapAdmin?: boolean;
}

export interface LoginRequest {
  readonly username: string;
  readonly password: string;
  readonly secondFactorCode?: string;
}

export interface LoginResult {
  readonly identity: Identity;
  readonly sessionExpiresAt: string;
  readonly csrfToken?: string;
}

export interface BootstrapState {
  readonly bootstrapAvailable: boolean;
  readonly initialized: boolean;
  readonly setupMethod?: "local-console-token";
}

export interface BootstrapRequest {
  readonly setupToken: string;
  readonly username: string;
  readonly password: string;
}

export interface Session {
  readonly sessionId: string;
  readonly createdAt?: string;
  readonly expiresAt: string;
  readonly lastSeenAt?: string;
  readonly current: boolean;
}

export interface NodeSummary {
  readonly id: string;
  readonly name: string;
  readonly online: boolean;
  readonly region?: string;
  readonly provider?: string;
  readonly countryCode?: string;
  readonly ip?: string;
  readonly tags?: readonly string[];
  readonly cpuPercent?: number | null;
  readonly memoryPercent?: number | null;
}

export type NodeStatus = "online" | "offline" | "suspended" | "revoked" | "unknown";

export interface NodeDetail {
  readonly nodeId: string;
  readonly displayName: string;
  readonly status: NodeStatus;
  readonly lastSeenAt?: string;
  readonly agentVersion?: string;
  readonly platform?: string;
  readonly revision: number;
}

export interface NodeIdentity {
  readonly nodeId: string;
  readonly certificateFingerprintSha256?: string;
  readonly issuerFingerprintSha256?: string;
  readonly serial?: string;
  readonly notAfter?: string;
  readonly revoked: boolean;
  readonly lastVerifiedAt?: string;
}

export interface NodeCapabilities {
  readonly nodeId: string;
  readonly protocolMajor: number;
  readonly protocolMinor: number;
  readonly capabilities: readonly string[];
  readonly observedAt?: string;
}

export type NodeGroupKind = "manual" | "automatic";

export interface NodeGroup {
  readonly id: string;
  readonly name: string;
  readonly kind: NodeGroupKind;
  readonly description?: string;
  readonly memberNodeIds?: readonly string[];
  readonly revision: number;
  readonly memberCount?: number;
}

export interface NodeGroupFacet {
  readonly dimension: "region" | "provider" | "tag";
  readonly items: readonly { readonly value: string; readonly label?: string; readonly nodeCount: number }[];
}

export interface NodeGroupResults {
  readonly items: readonly NodeSummary[];
  readonly total: number;
  readonly nextCursor?: string | null;
}

export interface NodeEnrollmentToken {
  readonly id: string;
  readonly token: string;
  readonly expiresAt: string;
}

export interface NodeEnrollment {
  readonly id: string;
  readonly name: string;
  readonly status: "pending" | "approved" | "rejected";
  readonly countryCode?: string;
  readonly region?: string;
}

export interface User {
  readonly userId: string;
  readonly username: string;
  readonly displayName?: string;
  readonly roleIds: readonly string[];
  readonly enabled: boolean;
  readonly revision: number;
}

export interface Role {
  readonly roleId: string;
  readonly label: string;
  readonly permissions: readonly string[];
  readonly builtIn?: boolean;
}

export interface Permission {
  readonly key: string;
  readonly description: string;
  readonly resourceType?: string;
}

export interface Grant {
  readonly grantId: string;
  readonly principalId: string;
  readonly resourceType: string;
  readonly resourceId: string;
  readonly permissions: readonly string[];
  readonly createdAt?: string;
  readonly expiresAt?: string;
}

export interface AuditEvent {
  readonly eventId: string;
  readonly occurredAt: string;
  readonly actorType: "user" | "agent" | "plugin" | "ai" | "system";
  readonly actorId?: string;
  readonly action: string;
  readonly resourceType?: string;
  readonly resourceId?: string;
  readonly result: "allowed" | "denied" | "success" | "failure" | "partial";
  readonly requestId: string;
  readonly taskId?: string;
  readonly redactedSummary?: string;
}

export type TaskState =
  | "awaiting_approval"
  | "queued"
  | "waiting_node"
  | "running"
  | "paused"
  | "completed"
  | "partially_failed"
  | "failed"
  | "canceling"
  | "canceled"
  | "unknown"
  | "expired";

export interface TaskSummary {
  readonly taskId: string;
  readonly state: TaskState;
  readonly createdAt: string;
  readonly createdBy?: string;
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly revision: number;
  readonly planDigest: string;
  readonly expiresAt?: string;
}

export interface TaskExecution {
  readonly executionId: string;
  readonly taskId: string;
  readonly nodeId: string;
  readonly state: string;
  readonly startedAt?: string;
  readonly finishedAt?: string;
  readonly exitCode?: number | null;
  readonly outputTruncated?: boolean;
  readonly lastSequence: number;
}

export interface BatchTaskPreview {
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly eligibleNodeIds: readonly string[];
  readonly deniedNodeIds: readonly string[];
  readonly requiresApproval: boolean;
}

export interface BatchTaskJob {
  readonly jobId: string;
  readonly name?: string;
  readonly targetType: string;
  readonly targetResourceId?: string;
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly state: string;
  readonly results?: readonly { readonly nodeId: string; readonly state: string; readonly error?: string }[];
  readonly createdAt?: string;
}

export interface ServiceGroup {
  readonly id: string;
  readonly revision: number;
  readonly name: string;
  readonly description?: string;
  readonly memberNodeIds: readonly string[];
  readonly service?: { readonly port?: number; readonly protocol?: string; readonly healthCheck?: string };
  readonly loadBalancing: { readonly enabled: boolean; readonly strategy?: string };
  readonly gatewayIds?: readonly string[];
  readonly healthyNodeIds?: readonly string[];
  readonly dispatchEligibleNodeIds?: readonly string[];
  readonly createdAt?: string;
}

export interface HealthPayload {
  readonly status: string;
  readonly stage?: string;
}

export interface NodeDiagnostic {
  readonly nodeId: string;
  readonly connectionState: "online" | "unreachable" | "identity_rejected" | "unknown";
  readonly lastHeartbeatAt?: string;
  readonly agentVersion?: string;
  readonly identityVerified?: boolean;
  readonly pendingTasks?: readonly { readonly id: string; readonly expired?: boolean; readonly requiresReapproval?: boolean }[];
  readonly diagnosticNotes?: readonly string[];
}

export interface TrafficQuota {
  readonly nodeId: string;
  readonly quotaGb: number;
  readonly usedGb?: number;
  readonly cycleResetDay: number;
  readonly timezone: string;
  readonly direction: "ingress" | "egress" | "combined";
  readonly source: "agent_estimate" | "provider_billing";
  readonly dataComplete?: boolean;
  readonly thresholdsPct?: readonly number[];
  readonly expiryType?: "instance" | "trial" | "credit" | "other";
  readonly expiresAt?: string;
  readonly remindDays?: readonly number[];
  readonly predictionGb?: number;
  readonly predictionConfidence?: "insufficient" | "low" | "medium" | "high";
}

export interface TrafficRisk {
  readonly nodeId: string;
  readonly kind: string;
  readonly severity: string;
  readonly estimated: boolean;
}

export interface CredentialMetadata {
  readonly id: string;
  readonly name: string;
  readonly kind: "cloudflare_token" | "smtp_password" | "qqbot_secret" | "ai_api_key" | "other";
  readonly status: "unverified" | "valid" | "invalid" | "expired" | "revoked";
  readonly usageScope: readonly string[];
  readonly references?: readonly string[];
  readonly lastValidatedAt?: string;
  readonly expiresAt?: string;
  readonly createdAt?: string;
}

export interface ServiceGroupDispatchPreview {
  readonly serviceGroupId: string;
  readonly gatewayEligible: boolean;
  readonly eligibleNodeIds: readonly string[];
  readonly excludedNodes?: readonly { readonly nodeId: string; readonly reason?: string }[];
  readonly policy?: string;
}

export interface ServiceGroupMemberStatus {
  readonly nodeId: string;
  readonly paused: boolean;
  readonly reason?: string;
  readonly online?: boolean;
  readonly healthy?: boolean;
  readonly dispatchEligible: boolean;
}

export type ConflictPolicy = "skip" | "overwrite" | "rename";

export interface FileEntry {
  readonly name: string;
  readonly path: string;
  readonly type: "file" | "directory" | "symlink";
  readonly sizeBytes?: number | null;
  readonly modifiedAt?: string | null;
  readonly version?: string | null;
  readonly permissions?: string | null;
}

export interface DirectoryList {
  readonly nodeId: string;
  readonly path: string;
  readonly entries: readonly FileEntry[];
  readonly nextCursor?: string | null;
}

export interface FileSizeResult {
  readonly path: string;
  readonly sizeBytes: number;
  readonly itemsScanned?: number;
}

export interface UploadSession {
  readonly uploadId: string;
  readonly status: "awaiting_data";
  readonly expiresAt?: string;
}

export interface UploadCompletion {
  readonly uploadId: string;
  readonly status: "completed" | "failed";
  readonly files?: readonly FileEntry[];
}

export type TransferStatus = "queued" | "running" | "completed" | "partial_failure" | "failed" | "cancelling" | "cancelled";

export interface TransferItemResult {
  readonly path?: string;
  readonly state?: string;
  readonly error?: string;
  readonly bytes?: number;
}

export interface TransferJob {
  readonly id: string;
  readonly sourceNodeId: string;
  readonly sourcePaths: readonly string[];
  readonly destinationNodeId: string;
  readonly destinationDir: string;
  readonly conflictPolicy?: ConflictPolicy;
  readonly status: TransferStatus;
  readonly progressPercent: number;
  readonly totalBytes?: number | null;
  readonly copiedBytes?: number;
  readonly items?: readonly TransferItemResult[];
  readonly createdAt: string;
  readonly startedAt?: string | null;
  readonly completedAt?: string | null;
  readonly error?: { readonly code: string; readonly message: string; readonly requestId?: string };
}

export interface CreateTransferRequest {
  readonly sourceNodeId: string;
  readonly sourcePaths: readonly string[];
  readonly destinationNodeId: string;
  readonly destinationDir: string;
  readonly conflictPolicy: ConflictPolicy;
  readonly followSymlinks?: false;
  readonly dryRun?: boolean;
}

export type MonitorState = "healthy" | "degraded" | "unhealthy" | "pending" | "unknown" | "paused";

export interface AvailabilityMonitor {
  readonly id?: string;
  readonly name: string;
  readonly type: "http" | "tcp" | "tls";
  readonly target: string;
  readonly sourceNodeId?: string;
  readonly linkedResource?: { readonly kind: string; readonly id: string };
  readonly intervalSeconds: number;
  readonly timeoutSeconds: number;
  readonly failureCount?: number;
  readonly recoveryCount?: number;
  readonly state?: MonitorState;
  readonly lastCheckedAt?: string;
  readonly notifyChannels?: readonly ("qqbot" | "smtp")[];
}

export interface SMTPConfig {
  readonly host?: string;
  readonly port?: number;
  readonly security?: "tls" | "starttls" | "none";
  readonly sender?: string;
  readonly recipient?: string;
  readonly password?: never;
}

export interface QQBotConfig {
  readonly appId?: string;
  readonly targetType?: "private" | "group";
  readonly targetId?: string;
  readonly appSecret?: never;
}

export interface NotificationChannel {
  readonly channel: "smtp" | "qqbot";
  readonly enabled: boolean;
  readonly credentialStatus: "not_configured" | "configured" | "invalid";
  readonly smtp?: SMTPConfig;
  readonly qqbot?: QQBotConfig;
}

export interface NotificationDelivery {
  readonly id: string;
  readonly channel: "smtp" | "qqbot";
  readonly status: "queued" | "submitted" | "provider_accepted" | "rejected" | "rate_limited" | "failed" | "delivery_unknown";
  readonly providerCode?: string;
  readonly message?: string;
  readonly createdAt?: string;
}

export interface NotificationSettings {
  readonly smtpFallbackOnQQFailure: boolean;
  readonly maxRetryAttempts: number;
  readonly deduplicationWindowSeconds?: number;
}

export interface DNSProvider {
  readonly code: "cloudflare";
  readonly displayName: string;
  readonly supported: boolean;
  readonly capabilities: {
    readonly supportsProxy?: boolean;
    readonly supportsZoneSync?: boolean;
    readonly supportedRecordTypes?: readonly string[];
  };
}

export interface DNSAccount {
  readonly id: string;
  readonly providerCode: "cloudflare";
  readonly displayName: string;
  readonly note?: string;
  readonly enabled: boolean;
  readonly isDefault: boolean;
  readonly credentialStatus: "not_configured" | "pending_verification" | "valid" | "invalid" | "missing_permissions";
  readonly zoneCount?: number;
}

export interface DNSZone {
  readonly id: string;
  readonly accountId: string;
  readonly name: string;
  readonly status?: string;
}

export interface DNSRecordInput {
  readonly type: "A" | "AAAA" | "CNAME" | "TXT" | "MX";
  readonly name: string;
  readonly content: string;
  readonly ttl?: number;
  readonly proxied?: boolean;
  readonly priority?: number;
}

export interface DNSRecord extends DNSRecordInput {
  readonly id: string;
  readonly zoneId: string;
  readonly modifiedAt?: string;
}

export interface DNSConnectionTest {
  readonly status: "success" | "failed" | "partial";
  readonly zoneRead: boolean;
  readonly recordWrite: boolean;
  readonly message?: string;
  readonly checkedAt?: string;
}

export interface GatewayNode {
  readonly id: string;
  readonly nodeId: string;
  readonly name: string;
  readonly region?: string;
  readonly countryCode?: string;
  readonly online: boolean;
  readonly status: "pending" | "installing" | "verifying" | "ready" | "failed" | "maintenance";
  readonly nginxInstalled: boolean;
  readonly checksPassed: boolean;
  readonly enabled: boolean;
  readonly schedulingEligible: boolean;
  readonly lastError?: string;
  readonly nginxVersion?: string;
}

export interface GatewayProvisionJob {
  readonly id: string;
  readonly nodeId: string;
  readonly gatewayId?: string;
  readonly status: "queued" | "installing" | "verifying" | "succeeded" | "failed" | "cancelled";
  readonly phase?: string;
  readonly createdAt: string;
  readonly error?: { readonly code: string; readonly message: string; readonly requestId?: string };
  readonly checks?: GatewayHealthChecks;
}

export interface GatewayHealthChecks {
  readonly nginxInstalled: boolean;
  readonly serviceRunning: boolean;
  readonly configValid: boolean;
  readonly healthProbePassed: boolean;
  readonly checkedAt?: string;
  readonly message?: string;
}

export interface GatewayRouteInput {
  readonly domain: string;
  readonly upstream?: string;
  readonly serviceGroupId?: string;
  readonly https: boolean;
  readonly enabled: boolean;
}

export interface GatewayRoute extends GatewayRouteInput {
  readonly id: string;
}

export interface CertificateNodeResult {
  readonly nodeId: string;
  readonly state: "queued" | "keygen" | "csr_pending" | "signed" | "distributing" | "verified" | "failed" | "waiting_offline" | "revoked";
  readonly certificateSerial?: string;
  readonly identity?: string;
  readonly expiresAt?: string;
  readonly errorCode?: string;
  readonly retryable?: boolean;
}

export interface InternalCertificateJob {
  readonly jobId: string;
  readonly serviceGroupId: string;
  readonly memberNodeIdsSnapshot: readonly string[];
  readonly credentialType: "trust_bundle" | "per_node_identity";
  readonly state: "queued" | "running" | "completed" | "partial" | "failed" | "revoking" | "revoked";
  readonly results: readonly CertificateNodeResult[];
  readonly createdAt?: string;
  readonly requestedBy?: string;
}

export interface PublicationDraft {
  readonly name?: string;
  readonly serviceGroupId: string;
  readonly gatewayIds: readonly string[];
  readonly dnsAccountId?: string;
  readonly zoneId?: string;
  readonly domain: string;
  readonly https: boolean;
  readonly certificateId?: string;
  readonly createMonitor?: boolean;
}

export interface PublicationPreview {
  readonly eligible?: boolean;
  readonly targetNodeIdsSnapshot?: readonly string[];
  readonly configDiff?: readonly string[];
  readonly checks?: readonly { readonly name: string; readonly passed: boolean; readonly reason?: string }[];
  readonly warnings?: readonly string[];
}

export interface AsyncOperation {
  readonly id: string;
  readonly status: "queued" | "prechecking" | "running" | "partially_completed" | "succeeded" | "failed" | "paused" | "manual_intervention";
  readonly createdAt: string;
  readonly stages?: readonly { readonly name: string; readonly status: string; readonly message?: string }[];
  readonly auditId?: string;
}

export type AIApprovalMode = "per_action" | "automatic" | "full_access";

export interface AIResourceGrant {
  readonly grantId: string;
  readonly resourceType: "node" | "service_group";
  readonly resourceId: string;
  readonly memberNodeIdsSnapshot?: readonly string[];
  readonly allowedActions: readonly string[];
  readonly grantedAt?: string;
  readonly grantedBy?: string;
}

export interface AISession {
  readonly id: string;
  readonly scopeRevision: number;
  readonly resourceGrants: readonly AIResourceGrant[];
  readonly allowedNodeIds?: readonly string[];
  readonly status: "active" | "closed";
  readonly createdBy?: string;
  readonly createdAt?: string;
  readonly approvalPolicy?: AIApprovalPolicy;
}

export interface AIApprovalPolicy {
  readonly sessionId: string;
  readonly mode: AIApprovalMode;
  readonly effectiveMode: AIApprovalMode;
  readonly expiresAt?: string | null;
  readonly scopeRevision: number;
  readonly maxFullAccessSeconds?: number;
  readonly enforcedRestrictions?: readonly string[];
}

export interface AIResourceRecommendation {
  readonly id: string;
  readonly reason: string;
  readonly resourceType: "node" | "service_group" | "gateway" | "unknown";
  readonly recommendedCapability: string;
  readonly metadataOnly?: boolean;
  readonly status: "pending" | "approved" | "rejected";
}

export interface AIApprovalRequest {
  readonly id: string;
  readonly sessionId: string;
  readonly state: "pending" | "approved" | "denied" | "expired" | "revoked" | "consumed";
  readonly toolName: string;
  readonly canonicalArgumentsHash: string;
  readonly targetNodeIdsSnapshot: readonly string[];
  readonly scopeRevision: number;
  readonly riskLevel?: "read" | "low" | "medium" | "high" | "critical";
  readonly expiresAt: string;
  readonly createdBy?: string;
}

export interface AIApprovalEvent {
  readonly id: string;
  readonly sessionId: string;
  readonly actor: string;
  readonly action: string;
  readonly mode?: AIApprovalMode;
  readonly toolName?: string;
  readonly targetNodeIds?: readonly string[];
  readonly result: "approved" | "denied" | "auto_approved" | "blocked" | "expired" | "changed";
  readonly timestamp: string;
  readonly reason?: string;
}

export interface AISecurityPolicy {
  readonly defaultDeny: boolean;
  readonly serverSideEnforced: boolean;
  readonly groupGrantMemberPolicy?: string;
  readonly allowUnscopedDiscovery?: boolean;
  readonly scopeRevisionRequired?: boolean;
  readonly lateralMovementRestricted?: boolean;
  readonly approvalPolicyServerEnforced?: boolean;
  readonly noScopeExpansionFromApprovalMode?: boolean;
}

export interface AIExecuteResult {
  readonly taskId?: string;
  readonly accepted: boolean;
  readonly validatedNodeIds: readonly string[];
  readonly operation?: string;
  readonly state: "queued" | "running" | "completed" | "denied" | "canceled";
  readonly deniedReason?: string;
  readonly scopeRevision: number;
}

export interface AIMessage {
  readonly messageId: string;
  readonly sessionId: string;
  readonly role: "user" | "assistant" | "tool";
  readonly redactedContent?: string;
  readonly createdAt: string;
  readonly taskIds?: readonly string[];
}

export interface PluginRepository {
  readonly mode: "official" | "custom";
  readonly url?: string;
  readonly ref: string;
  readonly path: "Plugin";
  readonly resolvedCommit?: string;
  readonly officialVerified?: boolean;
}

export interface PluginCatalogEntry {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly description?: string;
  readonly minControllerVersion?: string;
  readonly entryType?: "declarative" | "wasm";
  readonly manifestHash: string;
  readonly artifactSha256?: string;
  readonly signature?: string;
  readonly permissions: readonly string[];
  readonly sourceCommit?: string;
}

export interface InstalledPlugin {
  readonly id: string;
  readonly version: string;
  readonly status: "installed_disabled" | "starting" | "enabled" | "draining" | "updating" | "uninstalling" | "failed";
  readonly enabled: boolean;
  readonly sourceURL?: string;
  readonly sourceCommit: string;
  readonly permissions?: readonly string[];
  readonly lastError?: string;
  readonly installedAt?: string;
}

export interface PluginOperation {
  readonly id: string;
  readonly pluginId: string;
  readonly action: "install" | "activate" | "deactivate" | "update" | "uninstall";
  readonly status: "queued" | "verifying" | "draining" | "applying" | "succeeded" | "failed" | "rolled_back" | "deferred";
  readonly previousVersion?: string;
  readonly targetVersion?: string;
  readonly error?: { readonly code: string; readonly message: string; readonly requestId?: string };
  readonly auditId?: string;
}

export interface UpdateCheckResult {
  readonly currentVersion: string;
  readonly latestVersion?: string;
  readonly channel: "stable" | "beta";
  readonly updateAvailable: boolean;
  readonly releaseNotes?: string;
  readonly publishedAt?: string;
  readonly signatureVerified?: boolean;
  readonly platformCompatible?: boolean;
  readonly checkedAt: string;
}

export interface ControllerUpdatePlan {
  readonly planId: string;
  readonly targetVersion: string;
  readonly eligible: boolean;
  readonly migrationReversible?: boolean;
  readonly preflightChecks?: readonly { readonly name: string; readonly passed: boolean; readonly reason?: string }[];
  readonly estimatedImpact?: string;
  readonly backupRequired: boolean;
}

export type ControllerUpdateState =
  | "queued"
  | "preflight"
  | "verifying"
  | "backing_up"
  | "installing"
  | "restarting"
  | "health_check"
  | "completed"
  | "failed"
  | "rolling_back"
  | "rolled_back"
  | "manual_recovery_required";

export interface ControllerUpdateJob {
  readonly jobId: string;
  readonly fromVersion: string;
  readonly toVersion: string;
  readonly state: ControllerUpdateState;
  readonly phase?: string;
  readonly errorCode?: string;
  readonly backupId?: string;
  readonly rollbackEligible?: boolean;
  readonly healthChecks?: readonly { readonly name: string; readonly passed: boolean }[];
  readonly requestedBy?: string;
  readonly createdAt: string;
  readonly updatedAt?: string;
}

export interface RecoveryPlan {
  readonly backupId: string;
  readonly targetControllerId: string;
  readonly oldControllerFenced: boolean;
  readonly expectedVersion?: string;
  readonly expectedNodeIds?: readonly string[];
  readonly recoveryKeyReference?: string;
  readonly checks?: readonly { readonly name: string; readonly passed: boolean; readonly reason?: string }[];
}
