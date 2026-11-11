# HTTP 路由声明索引

由 `node scripts/api-docs.mjs --write` 从 Go AST 与已登记 OpenAPI 生成；不要手改。

共 **266** 个不同的 method/path 声明，其中 **0** 个尚无同路径或明确兼容别名 OpenAPI 候选。声明包含别名、可选配置和拒绝式入口，**不代表全部功能已装配或可执行**。GET 的隐式 HEAD 不单独计数。

入口、安全规则、手写协议端点和字段规范见 [接口使用说明](README.md)。候选列列出登记顺序中最后一个同路径或明确别名参考（JSON 的 candidateMatch 区分两者）；字段仍须结合该领域实现/修订说明，不能用路径匹配冒充字段审计。完整候选引用见 [JSON](route-inventory.json)。

## agent

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| POST | `/api/v1/agent/command-admission` | [command_tasks.go](../../apps/controller/server/command_tasks.go) · registerCommandRoutes | [checkNormalCommandAdmissionR20](../../api/openapi/revisions/v09-r20/command-profiles-candidate.yaml) |
| POST | `/api/v1/agent/root-admission` | [command_tasks.go](../../apps/controller/server/command_tasks.go) · registerCommandRoutes | [checkRootBrokerAdmissionR20](../../api/openapi/revisions/v09-r20/command-profiles-candidate.yaml) |
| POST | `/api/v1/agent/update-admission` | [resident_updates.go](../../apps/controller/server/resident_updates.go) · registerResidentUpdateRoutes | [residentSupervisorCurrentAdmission](../../api/openapi/revisions/v09-r22/resident-updates-candidate.yaml) |

## ai

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/ai/sessions/{sessionId}/grants/{grantId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [revokeAIResourceGrant](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/model-settings` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [getAIModelSettingsR6](../../api/openapi/revisions/v09-r6/ai-candidate.yaml) |
| GET | `/api/v1/ai/model-settings/{profileId}/budget` | [ai_budget_http.go](../../apps/controller/server/ai_budget_http.go) · RegisterAIBudgetRoutes | [getOwnerAIBudgetR7](../../api/openapi/revisions/v09-r7/ai-budget-candidate.yaml) |
| GET | `/api/v1/ai/security-policy` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [getAISecurityPolicy](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [getAISession](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}/approval-policy` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [getAISessionApprovalPolicy](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}/events` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [streamAISessionEventsG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}/messages` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [listAISessionMessagesG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}/runs` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [listAIAgentRunsR28](../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml) |
| GET | `/api/v1/ai/sessions/{sessionId}/runs/{runId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [getAIAgentRunR28](../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml) |
| PATCH | `/api/v1/ai/sessions/{sessionId}/approval-policy` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [updateAISessionApprovalPolicy](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [createAISession](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/approvals` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [createAIToolApproval](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [decideAIToolApproval](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/grants` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [grantAIResource](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/messages` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [createAIMessageG2](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [generateAIQueuedMessageR6](../../api/openapi/revisions/v09-r6/ai-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/resource-recommendations` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [recommendAIResources](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/runs` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [createAIAgentRunR28](../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/runs/{runId}/advance` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [continueAIAgentRunR28](../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml) |
| POST | `/api/v1/ai/sessions/{sessionId}/runs/{runId}/cancel` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [cancelAIAgentRunR28](../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml) |
| POST | `/api/v1/ai/tools/execute` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [executeAIScopedTool](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/ai/model-settings` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerAIRoutes | [putAIModelSettingsR6](../../api/openapi/revisions/v09-r6/ai-candidate.yaml) |
| PUT | `/api/v1/ai/model-settings/{profileId}/budget` | [ai_budget_http.go](../../apps/controller/server/ai_budget_http.go) · RegisterAIBudgetRoutes | [configureOwnerAIBudgetR7](../../api/openapi/revisions/v09-r7/ai-budget-candidate.yaml) |

## audit

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/audit/events` | [http.go](../../internal/audit/http.go) · Register | [listAuditEvents](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/audit/events/{eventId}` | [http.go](../../internal/audit/http.go) · Register | [getAuditEvent](../../api/openapi/g2-merged-candidate.yaml) |

## auth

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/auth/sessions/{sessionId}` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [revokeOwnSession](../../api/openapi/g2-merged-candidate.yaml) |
| DELETE | `/api/v1/auth/tokens/{tokenId}` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [revokeRestrictedTokenR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| GET | `/api/v1/auth/bootstrap-status` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [getBootstrapStatus](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/auth/me` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [getCurrentIdentity](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/auth/sessions` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listOwnSessions](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/auth/tokens` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listRestrictedTokensR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/auth/bootstrap` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [bootstrapInitialAdmin](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/auth/login` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [login](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/auth/logout` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [logout](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/auth/tokens` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [issueRestrictedTokenR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |

## availability-monitors

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/availability-monitors/{monitorId}` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [deleteBackgroundMonitor](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| GET | `/api/v1/availability-monitors` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [listBackgroundMonitors](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| GET | `/api/v1/availability-monitors/{monitorId}/alerts` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [listMonitorAlertEvents](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| GET | `/api/v1/availability-monitors/{monitorId}/history` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [readSelectedSourceHistory](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| PATCH | `/api/v1/availability-monitors/{monitorId}` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [updateBackgroundMonitor](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| POST | `/api/v1/availability-monitors` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [createBackgroundMonitor](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| POST | `/api/v1/availability-monitors/{monitorId}/probe` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [runSelectedSourceProbe](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |

## backups

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/backups` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [listEncryptedBackupsR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| GET | `/api/v1/backups/maintenance` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [getBackupMaintenanceR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| GET | `/api/v1/backups/{backupId}/download` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [downloadAuthenticatedCiphertextR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| POST | `/api/v1/backups` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [createEncryptedBackupTaskR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| POST | `/api/v1/backups/key-challenges` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [createOfflineKeyChallengeTaskR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| POST | `/api/v1/backups/key-confirmation` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [confirmOfflineRecoveryKeyR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| POST | `/api/v1/backups/maintenance/cleanup` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [submitBackupCleanupR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| POST | `/api/v1/backups/maintenance/cleanup-preview` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [previewBackupCleanupR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| POST | `/api/v1/backups/remotes/{remoteId}/verify` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [verifyBackupRemoteR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| POST | `/api/v1/backups/{backupId}/protection` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [protectBackupR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| POST | `/api/v1/backups/{backupId}/remote-upload` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [uploadBackupRemoteR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |
| POST | `/api/v1/backups/{backupId}/verify` | [backup_http.go](../../internal/recovery/backup_http.go) · BackupHTTPPatterns | [verifyEncryptedBackupReceiptR12](../../api/openapi/revisions/v09-r12/backups-controller-tasks-candidate.yaml) |
| PUT | `/api/v1/backups/maintenance/policy` | [backup_maintenance.go](../../apps/controller/server/backup_maintenance.go) · registerBackupMaintenanceRoutes | [configureBackupMaintenanceR29](../../api/openapi/revisions/v09-r29/operations-candidate.yaml) |

## certificates

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/certificates/chain-tasks/{taskId}` | [certificate_install.go](../../apps/controller/server/certificate_install.go) · registerCertificateInstallRoutes | [readNodeCertificateInstallReceiptR15](../../api/openapi/revisions/v09-r15/node-certificate-chain-candidate.yaml) |
| GET | `/api/v1/certificates/cleanup` | [cleanup_http.go](../../internal/certificates/cleanup_http.go) · RegisterCleanupHTTP | [listCertificateCleanupR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| GET | `/api/v1/certificates/csr-tasks/{taskId}` | [certificate_csr.go](../../apps/controller/server/certificate_csr.go) · registerCertificateCSRRoutes | [readCompletedNodeCSRTaskR14](../../api/openapi/revisions/v09-r14/node-csr-candidate.yaml) |
| GET | `/api/v1/certificates/internal/distributions` | [http.go](../../internal/certificates/http.go) · RegisterHTTP | [listInternalCertDistributions](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/certificates/public-profile` | [acme_http.go](../../apps/controller/server/acme_http.go) · registerPublicCertificateRoutes | [publicCertificateProfileR17](../../api/openapi/revisions/v09-r17/public-certificate-tasks-candidate.yaml) |
| GET | `/api/v1/certificates/public-renewals` | [certificate_renewals.go](../../apps/controller/server/certificate_renewals.go) · registerPublicRenewalRoutes | [listPublicRenewalPoliciesR24](../../api/openapi/revisions/v09-r24/public-renewals-candidate.yaml) |
| GET | `/api/v1/certificates/public-renewals/{policyId}/delivery` | [renewal_delivery.go](../../apps/controller/server/renewal_delivery.go) · registerRenewalDeliveryRoutes | [getRenewalDelivery](../../api/openapi/revisions/v09-r31/delivery-candidate.yaml) |
| GET | `/api/v1/certificates/public-tasks/{taskId}` | [acme_http.go](../../apps/controller/server/acme_http.go) · registerPublicCertificateRoutes | [observePublicCertificateOrderR17](../../api/openapi/revisions/v09-r17/public-certificate-tasks-candidate.yaml) |
| POST | `/api/v1/certificates/chain-tasks` | [certificate_install.go](../../apps/controller/server/certificate_install.go) · registerCertificateInstallRoutes | [createNodeCertificateInstallR15](../../api/openapi/revisions/v09-r15/node-certificate-chain-candidate.yaml) |
| POST | `/api/v1/certificates/cleanup/{cleanupId}/cancel` | [cleanup_http.go](../../internal/certificates/cleanup_http.go) · RegisterCleanupHTTP | [cancelCertificateCleanupR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| POST | `/api/v1/certificates/cleanup/{cleanupId}/retry` | [cleanup_http.go](../../internal/certificates/cleanup_http.go) · RegisterCleanupHTTP | [retryCertificateCleanupR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| POST | `/api/v1/certificates/csr-tasks` | [certificate_csr.go](../../apps/controller/server/certificate_csr.go) · registerCertificateCSRRoutes | [createNodeCSRTaskR14](../../api/openapi/revisions/v09-r14/node-csr-candidate.yaml) |
| POST | `/api/v1/certificates/internal/distributions` | [http.go](../../internal/certificates/http.go) · RegisterHTTP | [createInternalCertDistribution](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/certificates/internal/distributions/{distributionId}/revoke` | [http.go](../../internal/certificates/http.go) · RegisterHTTP | [revokeInternalCertDistribution](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/certificates/node-sets` | [certificate_set.go](../../apps/controller/server/certificate_set.go) · registerCertificateSetRoutes | [freezeNodeCertificateSetR16](../../api/openapi/revisions/v09-r16/node-certificate-sets-candidate.yaml) |
| POST | `/api/v1/certificates/public-renewals` | [certificate_renewals.go](../../apps/controller/server/certificate_renewals.go) · registerPublicRenewalRoutes | [configurePublicRenewalPolicyR24](../../api/openapi/revisions/v09-r24/public-renewals-candidate.yaml) |
| POST | `/api/v1/certificates/public-renewals/{policyId}/delivery` | [renewal_delivery.go](../../apps/controller/server/renewal_delivery.go) · registerRenewalDeliveryRoutes | [configureRenewalDelivery](../../api/openapi/revisions/v09-r31/delivery-candidate.yaml) |
| POST | `/api/v1/certificates/public-renewals/{policyId}/delivery/recover` | [renewal_recovery.go](../../apps/controller/server/renewal_recovery.go) · registerRenewalRecoveryRoute | [recoverRenewalDelivery](../../api/openapi/revisions/v09-r31/delivery-candidate.yaml) |
| POST | `/api/v1/certificates/public-tasks` | [acme_http.go](../../apps/controller/server/acme_http.go) · registerPublicCertificateRoutes | [submitPublicCertificateTaskR17](../../api/openapi/revisions/v09-r17/public-certificate-tasks-candidate.yaml) |

## commands

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| POST | `/api/v1/commands/tasks` | [command_tasks.go](../../apps/controller/server/command_tasks.go) · registerCommandRoutes | [submitCommandProfileR20](../../api/openapi/revisions/v09-r20/command-profiles-candidate.yaml) |

## credentials

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/credentials` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listCredentialMetadata08](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/credentials/{credentialId}/references` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [getCredentialReferences08](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/credentials` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [createCredential08](../../api/openapi/g2-merged-candidate.yaml) |

## disaster-recovery

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/disaster-recovery/jobs/{jobId}` | [http.go](../../internal/recovery/http.go) · NewHTTP | [getDisasterRecoveryJob08](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/disaster-recovery/jobs` | [http.go](../../internal/recovery/http.go) · NewHTTP | [createDisasterRecoveryJob08](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/disaster-recovery/preview` | [http.go](../../internal/recovery/http.go) · NewHTTP | [previewDisasterRecovery08](../../api/openapi/g2-merged-candidate.yaml) |

## dns

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/dns/zones/{zoneId}/records/{recordId}` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [deleteDNSRecord](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/dns/accounts` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [listDNSAccounts](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/dns/accounts/{accountId}` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [getDNSAccount](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/dns/accounts/{accountId}/zones` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [listDNSZones](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/dns/providers` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [listDNSProviders](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/dns/zones/{zoneId}/records` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [listDNSRecords](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/dns/accounts/{accountId}` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [updateDNSAccount](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/dns/zones/{zoneId}/records/{recordId}` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [updateDNSRecord](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/dns/accounts` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [createDNSAccount](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/dns/accounts/{accountId}/default` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [setDNSAccountDefaultR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/dns/accounts/{accountId}/test` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [testDNSConnection](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/dns/accounts/{accountId}/zones/sync` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [syncDNSZones](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/dns/provider-tasks` | [dns_execution.go](../../apps/controller/server/dns_execution.go) · registerDNSProviderTaskRoutes | [submitDNSProviderInspectionR17](../../api/openapi/revisions/v09-r17/public-certificate-tasks-candidate.yaml) |
| POST | `/api/v1/dns/record-tasks` | [dns_record_tasks.go](../../apps/controller/server/dns_record_tasks.go) · registerDNSRecordTaskRoutes | [submitDNSRecordChangeR18](../../api/openapi/revisions/v09-r18/dns-record-tasks-candidate.yaml) |
| POST | `/api/v1/dns/zones/{zoneId}/records` | [http.go](../../internal/dns/http.go) · RegisterHTTP | [createDNSRecord](../../api/openapi/g2-merged-candidate.yaml) |

## enrollment-tokens

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| POST | `/api/v1/enrollment-tokens` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [createNodeEnrollmentToken](../../api/openapi/g2-merged-candidate.yaml) |

## enrollments

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/enrollments` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [listNodeEnrollments](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/enrollments/{enrollmentId}/decision` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [decideNodeEnrollment](../../api/openapi/g2-merged-candidate.yaml) |

## files

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/files/distributions/{distributionId}` | [http.go](../../internal/files/http.go) · RegisterHTTP | [getFileDistributionR9](../../api/openapi/revisions/v09-r9/file-distributions-candidate.yaml) |
| GET | `/api/v1/files/resume-candidates/{taskId}` | [file_resume.go](../../apps/controller/server/file_resume.go) · registerFileResumeRoutes | [fileResumeCandidates](../../api/openapi/revisions/v09-r21/file-execution-resume-candidate.yaml) |
| POST | `/api/v1/files/distributions` | [http.go](../../internal/files/http.go) · RegisterHTTP | [createFileDistributionR9](../../api/openapi/revisions/v09-r9/file-distributions-candidate.yaml) |
| POST | `/api/v1/files/distributions/{distributionId}/cancel` | [http.go](../../internal/files/http.go) · RegisterHTTP | [cancelFileDistributionR9](../../api/openapi/revisions/v09-r9/file-distributions-candidate.yaml) |
| POST | `/api/v1/files/distributions/{distributionId}/complete` | [http.go](../../internal/files/http.go) · RegisterHTTP | [submitFileDistributionR9](../../api/openapi/revisions/v09-r9/file-distributions-candidate.yaml) |
| POST | `/api/v1/files/resume-tasks` | [file_resume.go](../../apps/controller/server/file_resume.go) · registerFileResumeRoutes | [createFileResumeTask](../../api/openapi/revisions/v09-r21/file-execution-resume-candidate.yaml) |
| PUT | `/api/v1/files/distributions/{distributionId}/content` | [http.go](../../internal/files/http.go) · RegisterHTTP | [appendFileDistributionR9](../../api/openapi/revisions/v09-r9/file-distributions-candidate.yaml) |

## gateway-provisions

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/gateway-provisions/{jobId}` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [getGatewayProvisionJob](../../api/openapi/g2-merged-candidate.yaml) |

## gateway-routes

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/gateway-routes/{routeId}` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [deleteGatewayRoute](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/gateway-routes` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [listGatewayRoutes](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/gateway-routes/{routeId}` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [updateGatewayRoute](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/gateway-routes` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [createGatewayRoute](../../api/openapi/g2-merged-candidate.yaml) |

## gateways

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/gateways` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [listGateways](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/gateways/{gatewayId}` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [getGateway](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/gateways/{gatewayId}/checks` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [getGatewayChecks](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/gateways/{gatewayId}` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [setGatewayDispatch](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/gateways/{gatewayId}/dispatch` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [patchGatewayDispatchR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/gateways` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [createGatewayProvision](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/gateways/{gatewayId}/provision` | [http.go](../../internal/gateway/http.go) · RegisterHTTP | [retryGatewayProvision](../../api/openapi/g2-merged-candidate.yaml) |

## groups

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/groups/{groupId}` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [deleteNodeGroup](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/groups` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [listNodeGroups](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/groups/classifications` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [listNodeClassificationFacets](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/groups/{groupId}` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [getNodeGroup](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/groups/{groupId}` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [updateNodeGroup](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/groups` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [createNodeGroup](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/groups/{groupId}/members` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [replaceNodeGroupMembers](../../api/openapi/g2-merged-candidate.yaml) |

## health

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/healthz` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerRoutes | [getHealthR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |

## installations

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/installations/catalog` | [installation_catalog.go](../../apps/controller/server/installation_catalog.go) · registerInstallationCatalogRoutes | [getInstallationCatalog](../../api/openapi/revisions/v09-r31/delivery-candidate.yaml) |

## monitoring

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/monitoring/availability-monitors` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [listBackgroundMonitors](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| GET | `/api/v1/monitoring/availability-monitors/{monitorId}/history` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [readSelectedSourceHistory](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| GET | `/api/v1/monitoring/traffic-risks` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [listTrafficRisks08](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/monitoring/availability-monitors/{monitorId}` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [updateBackgroundMonitor](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| POST | `/api/v1/monitoring/availability-monitors` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [createBackgroundMonitor](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |
| POST | `/api/v1/monitoring/availability-monitors/{monitorId}/probe` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [runSelectedSourceProbe](../../api/openapi/revisions/v09-r33/node-probes-candidate.yaml) |

## node-enrollment-tokens

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| POST | `/api/v1/node-enrollment-tokens` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [createNodeEnrollmentToken](../../api/openapi/g2-merged-candidate.yaml) |

## node-enrollments

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/node-enrollments` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [listNodeEnrollments](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/node-enrollments/{enrollmentId}/decision` | [enrollment_management_http.go](../../internal/transport/enrollment_management_http.go) · NewEnrollmentManagementHTTP | [decideNodeEnrollment](../../api/openapi/g2-merged-candidate.yaml) |

## nodes

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/nodes` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [listNodesForTransfer](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/nodes/{nodeId}/capabilities` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [getNodeCapabilitiesG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/nodes/{nodeId}/diagnostics` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [getNodeDiagnostic08](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/nodes/{nodeId}/files/uploads/{uploadId}` | [http.go](../../internal/files/http.go) · RegisterHTTP | [getOwnedUploadR11](../../api/openapi/revisions/v09-r11/upload-policies-candidate.yaml) |
| GET | `/api/v1/nodes/{nodeId}/identity` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [getNodeIdentityG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/nodes/{nodeId}/traffic-quota` | [http.go](../../internal/quota/http.go) · RegisterHTTP | [getObservedTrafficQuota](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| POST | `/api/v1/nodes/{nodeId}/files/uploads` | [http.go](../../internal/files/http.go) · RegisterHTTP | [createPolicyUploadR11](../../api/openapi/revisions/v09-r11/upload-policies-candidate.yaml) |
| POST | `/api/v1/nodes/{nodeId}/files/uploads/{uploadId}/complete` | [http.go](../../internal/files/http.go) · RegisterHTTP | [submitOwnedUploadTaskR11](../../api/openapi/revisions/v09-r11/upload-policies-candidate.yaml) |
| POST | `/api/v1/nodes/{nodeId}/revoke` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [revokeNodeIdentityG2](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/nodes/{nodeId}/suspend` | [http.go](../../internal/nodes/http.go) · RegisterHTTP | [suspendNodeG2](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content` | [http.go](../../internal/files/http.go) · RegisterHTTP | [appendOwnedUploadContentR11](../../api/openapi/revisions/v09-r11/upload-policies-candidate.yaml) |
| PUT | `/api/v1/nodes/{nodeId}/traffic-quota` | [http.go](../../internal/quota/http.go) · RegisterHTTP | [putObservedTrafficQuota](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |

## notifications

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/notifications/channels` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [listBoundNotificationChannels](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| GET | `/api/v1/notifications/channels/{channel}` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [getNotificationChannel](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/notifications/deliveries` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [listTaskNotificationDeliveries](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| GET | `/api/v1/notifications/settings` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [getNotificationSettings](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/notifications/channels/{channel}/credentials` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [bindNotificationCredentialRevision](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| POST | `/api/v1/notifications/channels/{channel}/test` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [queueNotificationTest](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| POST | `/api/v1/notifications/deliveries/{deliveryId}/retry` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [retryKnownFailedNotification](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| PUT | `/api/v1/notifications/channels/{channel}` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [putNotificationChannelConfiguration](../../api/openapi/revisions/v09-r32/observability-candidate.yaml) |
| PUT | `/api/v1/notifications/settings` | [http.go](../../internal/notifications/http.go) · RegisterHTTP | [putNotificationSettings](../../api/openapi/g2-merged-candidate.yaml) |

## operations

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/operations/incidents` | [operations_incidents.go](../../apps/controller/server/operations_incidents.go) · registerOperatorIncidentRoutes | [listOperatorIncidentsR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |
| GET | `/api/v1/operations/incidents/{incidentId}` | [operations_incidents.go](../../apps/controller/server/operations_incidents.go) · registerOperatorIncidentRoutes | [getOperatorIncidentR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |
| GET | `/api/v1/operations/incidents/{incidentId}/evidence` | [incident_closure.go](../../apps/controller/server/incident_closure.go) · registerIncidentClosureRoutes | [listOperatorIncidentEvidenceR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |
| POST | `/api/v1/operations/incidents/sync` | [operations_incidents.go](../../apps/controller/server/operations_incidents.go) · registerOperatorIncidentRoutes | [syncOperatorIncidentsR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |
| POST | `/api/v1/operations/incidents/{incidentId}/actions` | [operations_incidents.go](../../apps/controller/server/operations_incidents.go) · registerOperatorIncidentRoutes | [decideOperatorIncidentR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |
| POST | `/api/v1/operations/incidents/{incidentId}/verify-backup` | [incident_closure.go](../../apps/controller/server/incident_closure.go) · registerIncidentClosureRoutes | [verifyOperatorIncidentBackupR30](../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml) |

## permissions

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/permissions` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listPermissions](../../api/openapi/g2-merged-candidate.yaml) |

## plugin-operations

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/plugin-operations/{operationId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [getPluginOperation](../../api/openapi/g2-merged-candidate.yaml) |

## plugins

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/plugins/xray-manager/users/{userId}` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayDeleteUser](../../api/openapi/xray-candidate.yaml) |
| DELETE | `/api/v1/plugins/{pluginId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [uninstallPlugin](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/plugins` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [listInstalledPlugins](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/plugins/catalog` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [listPluginCatalog](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/plugins/repository` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [getPluginRepository](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [getXrayManagerStatusR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| GET | `/api/v1/plugins/xray-manager/capabilities` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayGetCapabilities](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/jobs/{jobId}` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayGetJobLink](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/nodes` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayListNodes](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/nodes/{nodeId}/configurations` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayListConfigRevisions](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/nodes/{nodeId}/instance` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayGetInstance](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/templates` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayListTemplates](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/traffic` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayGetTrafficSeries](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/traffic/settings` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayGetTrafficSettings](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/xray-manager/users` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayListUsers](../../api/openapi/xray-candidate.yaml) |
| GET | `/api/v1/plugins/{pluginId}` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [getInstalledPlugin](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/plugins/xray-manager/users/{userId}` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayPatchUser](../../api/openapi/xray-candidate.yaml) |
| PATCH | `/api/v1/plugins/{pluginId}/activation` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [patchPluginActivationR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/plugins/catalog/refresh` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [refreshPluginCatalog](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/configurations/validate` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayValidateConfiguration](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/deployments` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayCreateDeployment](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/deployments/preview` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayPreviewDeployment](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/installations` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayStartInstallation](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/installations/preview` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayPreviewInstall](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/nodes/{nodeId}/configurations/rollback` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayRollbackConfiguration](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/nodes/{nodeId}/instance/actions` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayInstanceAction](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/users` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayCreateUser](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/xray-manager/users/{userId}/client-export` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayExportClientConnection](../../api/openapi/xray-candidate.yaml) |
| POST | `/api/v1/plugins/{pluginId}/install` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [installPlugin](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/plugins/{pluginId}/update` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [updatePlugin](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/plugins/repository` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [putPluginRepository](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/plugins/xray-manager/traffic/settings` | [xray_http.go](../../internal/plugins/xray_http.go) · NewXrayDeclarativeHTTP | [xrayPutTrafficSettings](../../api/openapi/xray-candidate.yaml) |
| PUT | `/api/v1/plugins/{pluginId}/activation` | [composition_routes.go](../../apps/controller/server/composition_routes.go) · registerPluginRoutes | [setPluginActivation](../../api/openapi/g2-merged-candidate.yaml) |

## publications

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/publications` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [listPublicationsR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| GET | `/api/v1/publications/{publicationId}` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [getPublicationR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| GET | `/api/v1/publications/{publicationId}/dns-tasks` | [publication_dns_http.go](../../apps/controller/server/publication_dns_http.go) · registerPublicationDNSRoutes | [publicationDNSObservationsR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |
| POST | `/api/v1/publications` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [createPublicationR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| POST | `/api/v1/publications/preview` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [previewPublication08](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/publications/{publicationId}/cancel` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [cancelPublicationR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/publications/{publicationId}/rollback` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [compensatePublicationDNSR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |
| POST | `/api/v1/publications/{publicationId}/rollback-all` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [reverseCompensatePublicationR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |
| POST | `/api/v1/publications/{publicationId}/run` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [continuePublicationDNSR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |

## publishing

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/publishing/publications` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [listPublicationsR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| GET | `/api/v1/publishing/publications/{publicationId}` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [getPublicationR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| POST | `/api/v1/publishing/preview` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [previewPublication08](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/publishing/publications` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [createPublicationR7](../../api/openapi/revisions/v09-r7/services-candidate.yaml) |
| POST | `/api/v1/publishing/publications/{publicationId}/cancel` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [cancelPublicationR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/publishing/publications/{publicationId}/rollback` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [compensatePublicationDNSR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |
| POST | `/api/v1/publishing/publications/{publicationId}/rollback-all` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [reverseCompensatePublicationR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |
| POST | `/api/v1/publishing/publications/{publicationId}/run` | [http.go](../../internal/publishing/http.go) · RegisterHTTP | [continuePublicationDNSR19](../../api/openapi/revisions/v09-r19/publication-dns-tasks-candidate.yaml) |

## resource-grants

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/resource-grants/{grantId}` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [revokeResourceGrant](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/resource-grants` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listResourceGrants](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/resource-grants` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [createResourceGrant](../../api/openapi/g2-merged-candidate.yaml) |

## roles

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/roles` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listRoles](../../api/openapi/g2-merged-candidate.yaml) |

## service-groups

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| DELETE | `/api/v1/service-groups/{serviceGroupId}` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [deleteServiceGroup](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/service-groups` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [listServiceGroups](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/service-groups/{serviceGroupId}` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [getServiceGroup](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/service-groups/{serviceGroupId}/dispatch-preview` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [previewServiceGroupDispatch](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/service-groups/{serviceGroupId}` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [patchServiceGroup](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/service-groups/{serviceGroupId}/members/{nodeId}/scheduling` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [setServiceGroupMemberScheduling](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/service-groups` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [createServiceGroup](../../api/openapi/g2-merged-candidate.yaml) |
| PUT | `/api/v1/service-groups/{serviceGroupId}/members` | [http.go](../../internal/servicegroups/http.go) · RegisterHTTP | [replaceServiceGroupMembers](../../api/openapi/g2-merged-candidate.yaml) |

## system

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/system/controller-update-receipts/{taskId}` | [resident_updates.go](../../apps/controller/server/resident_updates.go) · registerResidentUpdateRoutes | [readResidentControllerReceipt](../../api/openapi/revisions/v09-r22/resident-updates-candidate.yaml) |
| GET | `/api/v1/system/recovery-authority` | [recovery_authority.go](../../apps/controller/server/recovery_authority.go) · registerRecoveryAuthorityRoutes | [readCurrentRecoveryAuthority](../../api/openapi/revisions/v09-r23/recovery-authority-candidate.yaml) |
| GET | `/api/v1/system/updates/jobs` | [http.go](../../internal/updates/http.go) · NewHTTP | [listOwnedUpdateTasksR7](../../api/openapi/revisions/v09-r7/updates-candidate.yaml) |
| GET | `/api/v1/system/updates/jobs/{jobId}` | [http.go](../../internal/updates/http.go) · NewHTTP | [getOwnedUpdateTaskR7](../../api/openapi/revisions/v09-r7/updates-candidate.yaml) |
| GET | `/api/v1/system/updates/source` | [release_source.go](../../apps/controller/server/release_source.go) · registerReleaseSourceRoutes | [getTrustedReleaseSourceR13](../../api/openapi/revisions/v09-r13/release-source-candidate.yaml) |
| POST | `/api/v1/system/agent-fleet-updates` | [resident_updates.go](../../apps/controller/server/resident_updates.go) · registerResidentUpdateRoutes | [submitResidentAgentFleetUpdate](../../api/openapi/revisions/v09-r22/resident-updates-candidate.yaml) |
| POST | `/api/v1/system/controller-update-tasks` | [resident_updates.go](../../apps/controller/server/resident_updates.go) · registerResidentUpdateRoutes | [submitResidentControllerUpdate](../../api/openapi/revisions/v09-r22/resident-updates-candidate.yaml) |
| POST | `/api/v1/system/updates/acquire` | [release_source.go](../../apps/controller/server/release_source.go) · registerReleaseSourceRoutes | [acquireSignedAgentReleaseR13](../../api/openapi/revisions/v09-r13/release-source-candidate.yaml) |
| POST | `/api/v1/system/updates/apply` | [http.go](../../internal/updates/http.go) · NewHTTP | [applyApprovedUpdateR7](../../api/openapi/revisions/v09-r7/updates-candidate.yaml) |
| POST | `/api/v1/system/updates/check` | [http.go](../../internal/updates/http.go) · NewHTTP | [checkControllerUpdate](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/system/updates/jobs/{jobId}/rollback` | [http.go](../../internal/updates/http.go) · NewHTTP | [rollbackControllerUpdate](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/system/updates/plan` | [http.go](../../internal/updates/http.go) · NewHTTP | [planControllerUpdate](../../api/openapi/g2-merged-candidate.yaml) |

## tasks

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/tasks` | [http.go](../../internal/tasks/http.go) · Register | [listTasksG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/tasks/{jobId}` | [http.go](../../internal/tasks/http.go) · Register | [getBatchTaskJob](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/tasks/{jobId}/events` | [http.go](../../internal/tasks/http.go) · Register | [streamTaskEventsG2](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/tasks/{jobId}/executions` | [http.go](../../internal/tasks/http.go) · Register | [listTaskExecutionsG2](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/tasks` | [http.go](../../internal/tasks/http.go) · Register | [createBatchTask](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/tasks/preview` | [http.go](../../internal/tasks/http.go) · Register | [previewBatchTask](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/tasks/{jobId}/approve` | [http.go](../../internal/tasks/http.go) · Register | [approveTaskG2](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/tasks/{jobId}/cancel` | [http.go](../../internal/tasks/http.go) · Register | [cancelTaskG2](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/tasks/{jobId}/resume` | [http.go](../../internal/tasks/http.go) · Register | [resumeTaskG2](../../api/openapi/g2-merged-candidate.yaml) |

## traffic-risks

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/traffic-risks` | [http.go](../../internal/monitoring/http.go) · RegisterHTTP | [listTrafficRisks08](../../api/openapi/g2-merged-candidate.yaml) |

## transfers

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/transfers` | [http.go](../../internal/files/http.go) · RegisterHTTP | [listTransfers](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/transfers/{transferId}` | [http.go](../../internal/files/http.go) · RegisterHTTP | [getTransfer](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/transfers` | [http.go](../../internal/files/http.go) · RegisterHTTP | [createTransfer](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/transfers/{transferId}/cancel` | [http.go](../../internal/files/http.go) · RegisterHTTP | [cancelTransfer](../../api/openapi/g2-merged-candidate.yaml) |

## users

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/users` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [listUsers](../../api/openapi/g2-merged-candidate.yaml) |
| GET | `/api/v1/users/{userId}` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [getUser](../../api/openapi/g2-merged-candidate.yaml) |
| PATCH | `/api/v1/users/{userId}` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [patchUser](../../api/openapi/g2-merged-candidate.yaml) |
| POST | `/api/v1/users` | [http.go](../../internal/auth/http.go) · NewIdentityHTTP | [createUser](../../api/openapi/g2-merged-candidate.yaml) |

## workflows

| 方法 | 路径 | 源码声明位置 | 最近候选参考 |
|---|---|---|---|
| GET | `/api/v1/workflows` | [http.go](../../internal/workflows/http.go) · RegisterHTTP | [listWorkflowsR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| GET | `/api/v1/workflows/{workflowId}` | [http.go](../../internal/workflows/http.go) · RegisterHTTP | [getWorkflowR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| PATCH | `/api/v1/workflows/{workflowId}` | [http.go](../../internal/workflows/http.go) · RegisterHTTP | [patchWorkflowR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/workflows` | [http.go](../../internal/workflows/http.go) · RegisterHTTP | [createWorkflowR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
| POST | `/api/v1/workflows/{workflowId}/runs` | [http.go](../../internal/workflows/http.go) · RegisterHTTP | [triggerWorkflowR34](../../api/openapi/revisions/v09-r34/runtime-additions-candidate.json) |
