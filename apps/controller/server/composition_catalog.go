package server

import (
	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/plugins"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
	"serverfleet.local/serverfleet/internal/workflows"
	"sort"
)

// Migrations returns the complete ordered migration sets across DS and CX domains.
func Migrations() [][]storage.Migration {
	cxMigrations := []storage.Migration{
		{
			Version:    "cx/0001_auth_identity",
			Name:       "persistent identity, session, RBAC and credential state",
			Statements: auth.IdentitySchemaStatements(),
		},
		{
			Version:    "cx/0002_agent_enrollment",
			Name:       "persistent agent enrollment state",
			Statements: transport.EnrollmentSchemaStatements(),
		},
		{
			Version:    "cx/0003_plugin_lifecycle",
			Name:       "persistent plugin lifecycle state",
			Statements: plugins.LifecycleSchemaStatements(),
		},
		{
			Version:    "cx/0004_plugin_history",
			Name:       "persistent plugin history state",
			Statements: plugins.HistorySchemaStatements(),
		},
		{
			Version:    "cx/0005_ai_sessions",
			Name:       "persistent AI session state",
			Statements: ai.SessionSchemaStatements(),
		},
		{
			Version:    "cx/0006_workflows",
			Name:       "persistent workflow state",
			Statements: workflows.WorkflowSchemaStatements(),
		},
		aiR1Migration("cx/0007_ai_budgets_and_model_audit"),
	}
	cxMigrations = append(cxMigrations, GatewayArtifactMigrations()...)
	return [][]storage.Migration{
		cxMigrations,
		nodes.Migrations(),
		servicegroups.Migrations(),
		tasks.Migrations(),
		files.Migrations(),
		{{Version: "cx/0009_update_journal", Name: "review candidate installed provenance and audited receipts", Statements: updates.InstallJournalSchemaStatements()}},
		{files.ExecutionBindingsMigration()},
		monitoring.Migrations(),
		notifications.Migrations(),
		quota.Migrations(),
		dns.Migrations(),
		certificates.Migrations(),
		certificates.CleanupMigrations(),
		certificates.CleanupClaimMigrations(),
		certificates.PublicOrderMigrations(),
		certificates.PublicRenewalMigrations(),
		gateway.Migrations(),
		publishing.Migrations(),
		publishing.TargetMigrations(),
		publishing.DNSBatchMigrations(),
		recovery.Migrations(),
		recovery.OffsiteMigrations(),
		tasks.IncidentMigrations(),
		tasks.IncidentEvidenceMigrations(),
		certificates.RenewalDeliveryMigrations(),
		publishing.CertificateReplacementMigrations(),
		transport.RecoveryMigrations(),
		transport.CertificateCSRMigrations(),
		transport.CertificateChainMigrations(),
		transport.CertificateSetMigrations(),
		recovery.ReconciliationMigrations(),
		recovery.ResidentAuthorityMigrations(),
		updates.AcquisitionMigrations(),
		audit.Migrations(),
	}
}

// AuditActions returns the deduplicated catalog of audit actions across all domains.
func AuditActions() []string {
	raw := []string{
		audit.ActionRetention,
		"auth.setup",
		"auth.bootstrap",
		"auth.login",
		"auth.user.create",
		"auth.user.patch",
		"auth.session.revoke",
		"auth.grant.create",
		"auth.grant.revoke",
		"auth.password.change",
		"auth.http.denied",
		"auth.recovery.invalidate",
		"recovery.reconcile",
		"recovery.activate",
		"auth.token.issue",
		"auth.token.revoke",
		"auth.approval.request",
		"auth.approval.approve",
		"auth.approval.deny",
		"auth.approval.consume",
		"credentials.create",
		"credentials.rotation.stage",
		"credentials.rotate",
		"credentials.revoke",
		"credentials.use.intent",
		"credentials.use.complete",
		"tasks.command",
		transport.CommandCapability, transport.ProbeCapability,
		"host.patch",
		"host.root",
		"files.transfer",
		files.ActionResume,
		transport.ResidentUpdateAction, transport.ControllerUpdateAction,
		updates.ActionInstall,
		updates.ActionAcquireRelease,
		"ai.budget.configure",
		"ai.budget.reserve",
		"gateway.provision",
		transport.CertificateCSRAction,
		transport.CertificateInstallAction,
		"certificates.node_sets.create",
		actionPublicCertificateIssue,
		actionPublicRenewalConfigure,
		actionDNSProviderInspect,
		actionDNSRecordApply,
		actionPublicationDNSApply, actionPublicationDNSRollback,
		"gateway.nginx.site_apply",
		"gateway.nginx.site_remove",
		"enrollment.issue",
		"enrollment.token.issue",
		"enrollment.request",
		"enrollment.approved",
		"enrollment.rejected",
		"enrollment.decide",
		"enrollment.list",
		"agent.revoke",
		"agent.identity.renew",
		"agent.identity.activate",
		"ai.sessions.create",
		tasks.ActionIncidentManage,
		recovery.ActionBackupPolicy, recovery.ActionBackupProtect, recovery.ActionBackupRemoteVerify,
		"ai.runs.create",
		"ai.runs.advance",
		"ai.tools.submitted",
		"ai.tools.observed",
		"ai.sessions.read",
		"ai.sessions.grant",
		"ai.sessions.revoke",
		"ai.sessions.policy",
		"ai.sessions.close",
		"ai.messages.create",
		"ai.messages.read",
		"ai.messages.generate",
		"ai.messages.completed",
		"ai.messages.unknown",
		"ai.events.read",
		"ai.tools.assess",
		"ai.tools.execute",
		"ai.approvals.create",
		"ai.approvals.approve",
		"ai.approvals.deny",
		"ai.approvals.consume",
		"ai.models.read",
		"ai.models.update",
		"plugins.install",
		"plugins.install.intent",
		"plugins.upgrade",
		"plugins.upgrade.intent",
		"plugins.enable",
		"plugins.enable.intent",
		"plugins.disable",
		"plugins.disable.intent",
		"plugins.uninstall",
		"plugins.uninstall.intent",
		"plugins.invoke",
		"plugins.history.save",
		"plugins.history.list",
		"plugins.history.restore-source",
		"workflows.save",
		"workflows.trigger",
		"workflows.dispatch",
		"workflows.task.submit",
		"workflows.invalidated",
		"workflows.paused_unknown",
		"workflows.failed",
		"workflows.complete",
	}
	for _, list := range [][]string{
		nodes.ServiceActions(),
		servicegroups.ServiceActions(),
		tasks.ServiceActions(),
		files.ServiceActions(),
		monitoring.ServiceActions(),
		notifications.ServiceActions(),
		quota.ServiceActions(),
		dns.ServiceActions(),
		certificates.ServiceActions(),
		gateway.ServiceActions(),
		publishing.ServiceActions(),
		recovery.BackupAuditActions(),
	} {
		raw = append(raw, list...)
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, action := range raw {
		if _, exists := seen[action]; !exists {
			seen[action] = struct{}{}
			out = append(out, action)
		}
	}
	sort.Strings(out)
	return out
}

// ExtraPermissions returns the domain permissions registered beyond auth's built-in 5 keys.
func ExtraPermissions() []auth.PermissionDefinition {
	return []auth.PermissionDefinition{
		{Key: actionPublicCertificateIssue, Description: "Issue public certificates for approved node-local CSR tasks", ResourceType: "node", AdminOnly: true},
		{Key: actionPublicRenewalConfigure, Description: "Configure owner-scoped public certificate renewal approval cycles", ResourceType: "node", AdminOnly: true},
		{Key: transport.CertificateInstallAction, Description: "Install verified public chains using node-owned website keys", ResourceType: "node", AdminOnly: true},
		{Key: transport.CertificateCSRAction, Description: "Create node-local website keys and export only a public CSR", ResourceType: "node", AdminOnly: true},
		{Key: certificates.ActionCleanupRead, Description: "Read owned cleanup records", ResourceType: "node", ReadOnly: true, AdminOnly: true},
		{Key: certificates.ActionCleanupRetry, Description: "Retry owned cleanup records", ResourceType: "node", AdminOnly: true},
		{Key: certificates.ActionCleanupCancel, Description: "Cancel owned cleanup records", ResourceType: "node", AdminOnly: true},
		{Key: certificates.ActionCleanupRecord, Description: "Record owned cleanup failures", ResourceType: "node", AdminOnly: true},
		{Key: updates.ActionInstall, Description: "Install verified releases on granted owned targets", ResourceType: "node", AdminOnly: true},
		{Key: nodes.ActionNodesWrite, Description: "Modify granted nodes", ResourceType: "node"},
		{Key: nodes.ActionNodeCreate, Description: "Create nodes", ResourceType: "node"},
		{Key: nodes.ActionNodesSuspend, Description: "Suspend granted nodes", ResourceType: "node"},
		{Key: nodes.ActionNodesRevoke, Description: "Revoke granted nodes", ResourceType: "node", AdminOnly: true},
		{Key: nodes.ActionGroupsWrite, Description: "Manage node groups on granted nodes", ResourceType: "node"},
		{Key: servicegroups.ActionRead, Description: "Read service groups on granted nodes", ResourceType: "node", ReadOnly: true},
		{Key: servicegroups.ActionWrite, Description: "Modify service groups on granted nodes", ResourceType: "node"},
		{Key: tasks.ActionCancel, Description: "Cancel tasks on granted nodes", ResourceType: "node"},
		{Key: tasks.ActionRevise, Description: "Revise unstarted tasks on granted nodes", ResourceType: "node"},
		{Key: tasks.ActionResume, Description: "Resume unstarted queued tasks on granted nodes", ResourceType: "node"},
		{Key: transport.ResidentUpdateAction, Description: "Install approved signed remote Agent releases", ResourceType: "node", AdminOnly: true},
		{Key: transport.ProbeCapability, Description: "Probe public services from granted nodes", ResourceType: "node", ReadOnly: true},
		{Key: transport.CommandCapability, Description: "Execute approved node executable profiles", ResourceType: "node"},
		{Key: "tasks.command", Description: "Execute standard commands on granted nodes", ResourceType: "node"},
		{Key: "host.patch", Description: "Execute controlled host changes on granted nodes", ResourceType: "node"},
		{Key: "host.root", Description: "Execute root-level host changes on granted nodes", ResourceType: "node", AdminOnly: true},
		{Key: files.ActionRead, Description: "Read files on granted nodes", ResourceType: "node", ReadOnly: true},
		{Key: files.ActionWrite, Description: "Write files on granted nodes", ResourceType: "node"},
		{Key: files.ActionResume, Description: "Resume verified unpublished file prefixes with approval", ResourceType: "node"},
		{Key: files.ActionTransfer, Description: "Transfer files across granted nodes", ResourceType: "node"},
		{Key: monitoring.ActionRead, Description: "Read availability monitors and traffic risks", ResourceType: "node", ReadOnly: true},
		{Key: monitoring.ActionWrite, Description: "Create and update availability monitors", ResourceType: "node"},
		{Key: quota.ActionRead, Description: "Read node traffic quota and usage", ResourceType: "node", ReadOnly: true},
		{Key: quota.ActionWrite, Description: "Configure node traffic quota", ResourceType: "node"},
		{Key: certificates.ActionCertificateRead, Description: "Read internal certificate distributions", ResourceType: "node", ReadOnly: true},
		{Key: certificates.ActionDistributionCreate, Description: "Create internal certificate distributions on granted nodes", ResourceType: "node"},
		{Key: certificates.ActionCertificateRenew, Description: "Renew internal certificates on granted nodes", ResourceType: "node"},
		{Key: certificates.ActionCertificateRevoke, Description: "Revoke internal certificates on granted nodes", ResourceType: "node"},
		{Key: certificates.ActionCertificateRotate, Description: "Rotate internal certificates on granted nodes", ResourceType: "node"},
		{Key: publishing.ActionRead, Description: "Read service publication plans and previews", ResourceType: "node", ReadOnly: true},
		{Key: publishing.ActionWrite, Description: "Create and manage service publication plans", ResourceType: "node"},
		{Key: publishing.ActionCreate, Description: "Create staged service publication plans", ResourceType: "node"},
		{Key: gateway.ActionRead, Description: "Read gateways and routes on granted nodes", ResourceType: "node", ReadOnly: true},
		{Key: gateway.ActionWrite, Description: "Manage gateways and routes on granted nodes", ResourceType: "node"},
		{Key: gateway.ActionProvision, Description: "Provision gateway runtime on granted nodes", ResourceType: "node"},
		{Key: gateway.NginxActionApply, Description: "Apply managed nginx sites on granted gateway nodes", ResourceType: "node"},
		{Key: gateway.NginxActionRemove, Description: "Remove managed nginx sites on granted gateway nodes", ResourceType: "node"},
		{Key: dns.ActionAccountsRead, Description: "Read DNS provider accounts", ResourceType: "node", ReadOnly: true},
		{Key: dns.ActionZonesRead, Description: "Read DNS zones", ResourceType: "node", ReadOnly: true},
		{Key: dns.ActionRecordsRead, Description: "Read DNS records", ResourceType: "node", ReadOnly: true},
		{Key: dns.ActionAccountCreate, Description: "Create DNS provider accounts", ResourceType: "node", AdminOnly: true},
		{Key: dns.ActionAccountUpdate, Description: "Update DNS provider accounts", ResourceType: "node", AdminOnly: true},
		{Key: dns.ActionAccountTest, Description: "Test DNS provider accounts", ResourceType: "node"},
		{Key: dns.ActionAccountDefault, Description: "Set default DNS provider account", ResourceType: "node", AdminOnly: true},
		{Key: dns.ActionZoneCreate, Description: "Sync DNS zones", ResourceType: "node"},
		{Key: dns.ActionRecordCreate, Description: "Create DNS records", ResourceType: "node"},
		{Key: dns.ActionRecordUpdate, Description: "Update DNS records", ResourceType: "node"},
		{Key: dns.ActionRecordDelete, Description: "Delete DNS records", ResourceType: "node"},
		{Key: "notifications.read", Description: "Read notification channels and delivery history", ResourceType: "node", ReadOnly: true},
		{Key: notifications.ActionWrite, Description: "Configure notification channels and settings", ResourceType: "node", AdminOnly: true},
		{Key: notifications.ActionChannel, Description: "Update notification channel configuration", ResourceType: "node", AdminOnly: true},
		{Key: notifications.ActionSecret, Description: "Rotate notification channel credentials", ResourceType: "node", AdminOnly: true},
		{Key: notifications.ActionSettings, Description: "Update notification event routing settings", ResourceType: "node", AdminOnly: true},
		{Key: notifications.ActionTest, Description: "Send test notification", ResourceType: "node"},
		{Key: notifications.ActionSend, Description: "Dispatch notification events", ResourceType: "node"},
		{Key: notifications.ActionRetry, Description: "Retry failed notification deliveries", ResourceType: "node"},
		{Key: workflows.ActionRead, Description: "Read own automation workflows", ResourceType: "node", ReadOnly: true},
		{Key: workflows.ActionWrite, Description: "Create, update and run automation workflows", ResourceType: "node"},
	}
}

// New opens and migrates the SQLite WAL database and wires all Controller services and HTTP routes.
