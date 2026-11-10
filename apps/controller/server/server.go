// Package server composes the persistent Controller domains, SQLite WAL storage,
// transactional audit sink, browser session/CSRF boundary, and REST/SSE handlers.
package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
)

// Config configures the composed Controller management runtime.
type Config struct {
	WebDirectory              string
	DBPath                    string
	CredentialKeyDir          string
	AIGenerationQuota         ai.GenerationQuota
	LocalAIProvider           *ai.LocalTLSProviderConfig
	Origin                    string
	StagingDir                string
	Dispatcher                tasks.Dispatcher
	DNSProvider               dns.Provider
	ACMEDeployment            *ACMEDeployment
	GatewayChecker            gateway.Checker
	MonitoringProber          monitoring.Prober
	MonitoringAllowedHosts    []string
	CertificateIssuer         certificates.Issuer
	CertificateCleanupCleaner certificates.DNSRecordCleaner
	DisableCleanupRetries     bool
	CleanupRetryInterval      time.Duration
	PublishingBridge          publishing.LocalBridgeConfig
	UploadTransport           files.UploadTransport
	DownloadTransport         files.DownloadTransport
	SMTPSender                notifications.SMTPSender
	QQSender                  notifications.QQSender
	Now                       func() time.Time

	// Agent transport composition. Enrollment and the Agent WSS link stay
	// unmounted unless the local PKI issuer and exact authority are supplied.
	AgentAuthority   string
	AgentServerName  string
	EnrollmentIssuer *transport.CertificateIssuer
	AgentFrameBytes  int
	AgentFileGate    transport.FileFrameGate
	FileExecution    *FileExecutionConfig
	// GatewayDeployment is the trusted startup document for Agent-side nginx
	// roots and certificate install paths. Nil keeps the gateway chain closed.
	GatewayDeployment *GatewayDeployment

	// Optional signed plugin package trust. Empty maps keep installation closed.
	PluginTrustKeys         map[string]ed25519.PublicKey
	PluginTrustSources      map[string]bool
	PluginTrustCapabilities map[string]bool

	// BackupDir defaults to a private directory beside the SQLite database.
	BackupDir             string
	BackupDeployment      *BackupDeployment
	RecoveryCleanup       func() error
	RecoveryLatch         *recovery.QuarantineLatch
	ResidentSupervisor    *updates.ResidentSupervisorConfig
	BuildVersion          string
	UpdateProduct         *updates.ProductDeploymentOptions
	ReleaseSource         *ReleaseSourceDeployment
	ControllerAuthorityID string
	UpdateExecution       *updates.TaskInstallWorkerOptions
	UpdateResolvePlan     func(context.Context, updates.ApplyRequest) (updates.FrozenInstallPlan, error)
	UpdateJobs            func(context.Context, updates.Principal) (updates.JobPage, error)
	UpdateJob             func(context.Context, updates.Principal, string) (updates.ApplyResult, error)
	UpdateCheck           func(context.Context, string) (updates.CheckResult, error)
	UpdatePlan            func(context.Context, string) (updates.Plan, error)
	UpdateApply           func(context.Context, updates.ApplyRequest) (updates.ApplyResult, error)
	RecoveryExecute       func(context.Context, recovery.Plan) (string, error)
	PluginRuntime         plugins.AdmittedRuntime
	XrayTaskHost          *plugins.XrayTaskHost
}

// Server holds the wired Controller database, services, and HTTP router.
type Server struct {
	residentHealthGate func(context.Context) error
	recoveryCleanup    func() error
	residentSupervisor *updates.ResidentSupervisorConfig
	buildVersion       string
	dbPath             string
	ProductUpdates     updates.ProductDeployment
	ReleaseAcquirer    *updates.ReleaseAcquirer
	PublicCertificates *publicCertificateExecution
	runtimeGate        func(context.Context) error
	DB                 *storage.DB
	Audit              *audit.SQLRecorder
	IdentityStore      *auth.SQLIdentityStore
	Identity           *auth.IdentityService
	Credentials        *auth.CredentialService
	IdentityHTTP       *auth.IdentityHTTP
	NodesRepo          *nodes.Repo
	GroupsRepo         *nodes.GroupRepo
	ServiceGroupsRepo  *servicegroups.Repo
	MonitoringRepo     *monitoring.Repo
	QuotaRepo          *quota.Repo
	CertificatesRepo   *certificates.Repo
	PublishingRepo     *publishing.Repo
	Nodes              *nodes.Service
	ServiceGroups      *servicegroups.Service
	TasksRepo          *tasks.Repo
	Tasks              *tasks.Service
	Files              *files.Service
	FilesTransport     *files.LocalStagingTransport
	DNS                *dns.Service
	Notifications      *notifications.Service
	Gateway            *gateway.Service
	Monitoring         *monitoring.Service
	Quota              *quota.Service
	Certificates       *certificates.Service
	CertificateCleanup *certificates.CleanupJournal
	cleanupEnabled     bool
	cleanupInterval    time.Duration
	Publishing         *publishing.Service

	Enrollment           *transport.EnrollmentService
	EnrollmentHTTP       *transport.EnrollmentHTTP
	EnrollmentManagement *transport.EnrollmentManagementHTTP
	Links                *transport.ControllerLinks
	AgentFiles           *transport.FileClient
	FileWorker           *FileExecutionWorker
	AgentLinkHTTP        *transport.AgentLinkHTTP

	AI               *ai.SessionService
	AIBudget         *ai.PersistentGenerationBudget
	AIConversation   *ai.ConversationService
	AIRuntime        *ai.BYOKRuntime
	AIModelsHTTP     *ai.ModelSettingsHTTP
	AIGenerationHTTP *ai.GenerationHTTP
	AIManagement     *ai.ManagementHTTP
	AIMessagesHTTP   *ai.MessagesHTTP
	AIEventsHTTP     *ai.MessageEventsHTTP

	Plugins          *plugins.LifecycleManager
	PluginRepository plugins.RepositoryStore
	PluginOperations plugins.OperationStore
	PluginVault      *plugins.PackageVault
	PluginHTTP       *plugins.ManagementHTTP
	XrayHTTP         *plugins.XrayDeclarativeHTTP

	UpdatesHTTP          *updates.HTTP
	RecoveryHTTP         *recovery.HTTP
	Backups              *recovery.BackupManager
	BackupHTTP           *recovery.BackupHTTP
	backupAutomaticOwner string

	Workflows     *workflows.Engine
	workflowState *workflowRuntime

	agentBridge *agentLinkBridge
	mux         *http.ServeMux
	console     http.Handler
	now         func() time.Time
	stagingDir  string
	backupDir   string

	runCancel context.CancelFunc
	runDone   chan struct{}
}

type requestIDContextKey struct{}

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
		transport.CommandCapability,
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
		"ai.sessions.create",
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
func New(ctx context.Context, cfg Config) (*Server, error) {
	runtimeGate, guardErr := recovery.RuntimeGuard(ctx, cfg.DBPath, cfg.RecoveryLatch)
	if guardErr != nil {
		return nil, guardErr
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return nil, errors.New("controller: database path is required")
	}
	if strings.TrimSpace(cfg.Origin) == "" {
		return nil, errors.New("controller: browser origin is required")
	}
	var console http.Handler
	if cfg.WebDirectory != "" {
		var err error
		console, err = LoadConsole(cfg.WebDirectory)
		if err != nil {
			return nil, err
		}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	stagingDir := cfg.StagingDir
	if strings.TrimSpace(stagingDir) == "" {
		stagingDir = filepath.Join(filepath.Dir(cfg.DBPath), "staging")
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("controller: create staging directory: %w", err)
	}

	db, err := storage.Open(ctx, storage.Config{Path: cfg.DBPath})
	if err != nil {
		return nil, err
	}
	var s *Server
	cleanup := func(cause error) (*Server, error) {
		if s != nil && s.AIRuntime != nil {
			s.AIRuntime.Close()
		}
		if s != nil && s.PluginVault != nil {
			_ = s.PluginVault.Close()
			s.PluginVault = nil
		}
		if s != nil && s.ProductUpdates != nil {
			_ = s.ProductUpdates.Close()
		}
		if s != nil && s.PublicCertificates != nil {
			s.PublicCertificates.Close()
		}
		if s != nil && s.ReleaseAcquirer != nil {
			_ = s.ReleaseAcquirer.Close()
		}
		if s != nil && s.Backups != nil {
			_ = s.Backups.Close()
		}
		_ = db.Close()
		return nil, cause
	}

	migrator, err := storage.NewMigrator(Migrations()...)
	if err != nil {
		return cleanup(err)
	}
	if _, err := migrator.Apply(ctx, db); err != nil {
		return cleanup(err)
	}

	var expectedResident *recovery.ResidentAuthority
	if cfg.ResidentSupervisor != nil {
		c := cfg.ResidentSupervisor
		expectedResident = &recovery.ResidentAuthority{AuthorityID: cfg.ControllerAuthorityID, PolicySHA256: updates.ResidentPolicySHA(*c), Directory: c.Directory, Mode: "owned"}
		if os.Getenv("SF_RESIDENT_SUPERVISOR_POLICY") != expectedResident.PolicySHA256 {
			return cleanup(recovery.ErrQuarantined)
		}
		if err := db.InTx(ctx, func(txctx context.Context, tx storage.Tx) error {
			return recovery.BindResidentAuthorityTx(txctx, tx, *expectedResident, false)
		}); err != nil {
			return cleanup(err)
		}
	}
	runtimeGate = recovery.ResidentAuthorityGate(db, expectedResident, os.Getenv("SF_RESIDENT_SUPERVISOR_POLICY"), runtimeGate)
	if runtimeGate(ctx) != nil {
		return cleanup(recovery.ErrQuarantined)
	}
	authorityGate := runtimeGate
	if cfg.ResidentSupervisor != nil {
		if cfg.UpdateProduct != nil {
			return cleanup(errors.New("controller: use independent Agent supervisors with Controller self supervision; legacy Controller-owned Agent update slot is a separate deployment mode"))
		}
		runtimeGate = recovery.ResidentMaintenanceGate(cfg.ResidentSupervisor.Directory, runtimeGate)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(AuditActions(), limits)
	if err != nil {
		return cleanup(err)
	}
	auditRecorder, err := audit.NewSQLRecorder(db, validator, now)
	if err != nil {
		return cleanup(err)
	}

	nodesRepo, err := nodes.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	groupsRepo, err := nodes.NewGroupRepo(db)
	if err != nil {
		return cleanup(err)
	}
	serviceGroupsRepo, err := servicegroups.NewRepo(db, nodesRepo)
	if err != nil {
		return cleanup(err)
	}
	tasksRepo, err := tasks.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	filesRepo, err := files.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	dnsRepo, err := dns.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	notificationsRepo, err := notifications.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	gatewayRepo, err := gateway.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	monitoringRepo, err := monitoring.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	quotaRepo, err := quota.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	certificatesRepo, err := certificates.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}
	publishingRepo, err := publishing.NewRepo(db)
	if err != nil {
		return cleanup(err)
	}

	identityStore, err := newIdentityStore(db, auditRecorder)
	if err != nil {
		return cleanup(err)
	}
	resolver := func(ctx context.Context, kind, id string) ([]string, error) {
		switch kind {
		case "node":
			node, err := nodesRepo.Get(ctx, id)
			if storage.IsNotFound(err) {
				return nil, auth.ErrInvalidCheck
			}
			if err != nil {
				return nil, err
			}
			return []string{node.ID}, nil
		case "node_group":
			group, err := groupsRepo.Get(ctx, id)
			if storage.IsNotFound(err) {
				return nil, auth.ErrInvalidCheck
			}
			if err != nil {
				return nil, err
			}
			return append([]string(nil), group.Members...), nil
		case "service_group":
			group, err := serviceGroupsRepo.Get(ctx, id)
			if storage.IsNotFound(err) {
				return nil, auth.ErrInvalidCheck
			}
			if err != nil {
				return nil, err
			}
			members := make([]string, 0, len(group.Members))
			for _, m := range group.Members {
				members = append(members, m.NodeID)
			}
			return members, nil
		default:
			return nil, auth.ErrInvalidCheck
		}
	}
	identityService, err := auth.NewIdentityServiceWithResources(identityStore, resolver, ExtraPermissions())
	if err != nil {
		return cleanup(err)
	}
	var aiConversation *ai.ConversationService
	var publicCertificates *publicCertificateExecution
	keyDir := cfg.CredentialKeyDir
	if keyDir == "" {
		absoluteDB, pathErr := filepath.Abs(cfg.DBPath)
		if pathErr != nil {
			return cleanup(pathErr)
		}
		keyDir = filepath.Join(filepath.Dir(absoluteDB), "credential-keys")
	}
	if err := auth.EnsureCredentialKeyDirectory(keyDir); err != nil {
		return cleanup(err)
	}
	credentialsService, err := auth.NewPersistentCredentialService(
		ctx, identityService, keyDir,
		func(ctx context.Context, op auth.CredentialOperation) error {
			if publicCertificates != nil && op.Module == "dns" {
				return publicCertificates.credentialGate(ctx, op)
			}
			if aiConversation == nil || op.Module != "ai" || op.Source != "ai" {
				return auth.ErrDenied
			}
			return aiConversation.CheckCredentialOperation(ctx, op)
		},
		nil, // No registered reference reader: report unavailable, never a false empty list.
	)
	if err != nil {
		return cleanup(err)
	}
	identityHTTP, err := auth.NewIdentityHTTP(identityService, auth.IdentityHTTPConfig{
		Origin:                 cfg.Origin,
		Credentials:            credentialsService,
		EnableRestrictedTokens: true,
	})
	if err != nil {
		return cleanup(err)
	}

	s = &Server{
		residentHealthGate: authorityGate,
		recoveryCleanup:    cfg.RecoveryCleanup,
		buildVersion:       cfg.BuildVersion, dbPath: cfg.DBPath,
		runtimeGate:       runtimeGate,
		DB:                db,
		Audit:             auditRecorder,
		IdentityStore:     identityStore,
		Identity:          identityService,
		Credentials:       credentialsService,
		IdentityHTTP:      identityHTTP,
		NodesRepo:         nodesRepo,
		GroupsRepo:        groupsRepo,
		ServiceGroupsRepo: serviceGroupsRepo,
		MonitoringRepo:    monitoringRepo,
		QuotaRepo:         quotaRepo,
		CertificatesRepo:  certificatesRepo,
		PublishingRepo:    publishingRepo,
		now:               now,
		stagingDir:        stagingDir,
	}

	authorizer := &composedAuthorizer{identity: identityService}
	readPolicy := identityService.NodeReadPolicySnapshot

	nodesService, err := nodes.NewService(nodes.Options{
		DB:         db,
		Nodes:      nodesRepo,
		Groups:     groupsRepo,
		Audit:      auditRecorder,
		Authorizer: authorizer,
		ReadPolicy: readPolicy,
		Now:        now,
	})
	if err != nil {
		return cleanup(err)
	}
	s.Nodes = nodesService
	var gatewayChain *gatewayWiring
	if cfg.GatewayDeployment != nil {
		gatewayChain, err = newGatewayWiring(ctx, db, cfg.GatewayDeployment)
		if err != nil {
			return cleanup(err)
		}
		if cfg.FileExecution == nil || cfg.EnrollmentIssuer == nil || cfg.AgentAuthority == "" {
			return cleanup(ErrGatewayDeployment)
		}
	}
	if cfg.FileExecution != nil {
		if cfg.AgentFileGate != nil || cfg.EnrollmentIssuer == nil || cfg.AgentAuthority == "" {
			return cleanup(files.ErrTransferExecution)
		}
		s.FileWorker, err = NewFileExecutionWorker(s, filesRepo, authorizer, *cfg.FileExecution)
		if err != nil {
			return cleanup(err)
		}
		cfg.AgentFileGate = s.FileWorker.Gate
	}
	if gatewayChain != nil {
		if cfg.AgentFileGate == nil {
			return cleanup(ErrGatewayDeployment)
		}
		cfg.AgentFileGate = composeGatewayFileGate(gatewayChain.artifactGate, cfg.AgentFileGate)
	}
	agentBridge, err := s.setupAgentTransport(cfg, nodesService, tasksRepo)
	if err != nil {
		return cleanup(err)
	}
	if gatewayChain != nil {
		if agentBridge == nil || s.AgentFiles == nil || cfg.FileExecution == nil {
			return cleanup(ErrGatewayDeployment)
		}
		channel, e := NewGatewayWSSChannel(s.AgentFiles)
		if e != nil {
			return cleanup(e)
		}
		gatewayChain.uploader, e = NewGatewayChannelUploaderWithMapper(channel, cfg.FileExecution.Paths, db, gatewayChain.stage)
		if e != nil {
			return cleanup(e)
		}
		gatewayChain.dispatcher, e = newGatewayTaskDispatcher(gatewayExecutionConfig{
			Repo: tasksRepo, DB: db, Plans: publishingRepo, Artifacts: gatewayChain.uploader,
			Next: agentBridge, Authorizer: authorizer, Nodes: nodesRepo,
			Registry: gatewayChain.registry, Now: now,
		})
		if e != nil {
			return cleanup(e)
		}
		if e := gatewayChain.stage.Bind(gatewayChain.dispatcher); e != nil {
			return cleanup(e)
		}
	}

	eligibility := &composedEligibility{nodes: nodesRepo, gateways: gatewayRepo, db: db, now: now}
	deleteGuard := &composedDeleteGuard{gateways: gatewayRepo, db: db}
	serviceGroupsService, err := servicegroups.NewService(servicegroups.Options{
		DB:          db,
		Repo:        serviceGroupsRepo,
		Nodes:       nodesRepo,
		Audit:       auditRecorder,
		Authorizer:  authorizer,
		ReadPolicy:  readPolicy,
		Eligibility: eligibility,
		DeleteGuard: deleteGuard,
		Now:         now,
	})
	if err != nil {
		return cleanup(err)
	}

	nodeLookup := &composedNodeLookup{nodes: nodesRepo, now: now}
	dispatcher := cfg.Dispatcher
	if gatewayChain != nil {
		if dispatcher != nil {
			return cleanup(errors.New("controller: gateway wiring cannot share a supplied dispatcher"))
		}
		dispatcher = gatewayChain.dispatcher
	} else if dispatcher == nil && agentBridge != nil {
		dispatcher = agentBridge
	}
	if cfg.ACMEDeployment != nil {
		if cfg.DNSProvider != nil {
			return cleanup(certificates.ErrACMEConfig)
		}
		publicCertificates, err = s.newPublicCertificateExecution(ctx, *cfg.ACMEDeployment)
		if err != nil {
			return cleanup(err)
		}
		s.PublicCertificates = publicCertificates
		cfg.DNSProvider = publicCertificates.provider
	}
	workers := map[string]tasks.Worker{}
	if publicCertificates != nil {
		workers[actionPublicCertificateIssue] = publicCertificates
	}
	if s.FileWorker != nil {
		workers[files.ActionTransfer] = s.FileWorker
		workers[files.ActionResume] = s.FileWorker
		agentBridge.fileWorker = s.FileWorker
	}
	if cfg.UpdateProduct != nil {
		if cfg.UpdateExecution != nil || cfg.UpdateApply != nil || cfg.UpdateResolvePlan != nil || cfg.UpdatePlan != nil || cfg.UpdateCheck != nil {
			return cleanup(updates.ErrArtifactRejected)
		}
		s.ProductUpdates, err = s.NewOwnedProductUpdates(*cfg.UpdateProduct)
		if err != nil {
			return cleanup(err)
		}
		cfg.UpdateExecution = s.ProductUpdates.WorkerOptions()
		cfg.UpdateResolvePlan = s.ProductUpdates.ResolvePlan
		cfg.UpdatePlan = s.ProductUpdates.Plan
		cfg.UpdateCheck = s.ProductUpdates.Check
	}
	if cfg.UpdateExecution != nil {
		if cfg.UpdateResolvePlan == nil || cfg.UpdateApply != nil {
			return cleanup(updates.ErrArtifactRejected)
		}
		options := *cfg.UpdateExecution
		options.Current = func(ctx context.Context, id, plan string) (tasks.Task, error) {
			return s.Tasks.CheckExecution(ctx, id, plan)
		}
		options.Administrator = s.Identity.AuthorizeAdministrator
		worker, e := updates.NewTaskInstallWorker(options)
		if e != nil {
			return cleanup(e)
		}
		workers[updates.ActionInstall] = worker
		projection, e := updates.NewTaskJobProjection(tasksRepo, func(ctx context.Context, p updates.Principal, t tasks.Task) error {
			return (&composedTaskReadGate{identity: s.Identity, tasks: tasksRepo}).AuthorizeTaskRead(ctx, p.SubjectID, t.ID)
		})
		if e != nil {
			return cleanup(e)
		}
		cfg.UpdateJobs, cfg.UpdateJob = projection.Jobs, projection.Job
		cfg.UpdateApply = func(ctx context.Context, r updates.ApplyRequest) (updates.ApplyResult, error) {
			if !r.Principal.Administrator || s.Identity.AuthorizeAdministrator(ctx, r.Principal.SubjectID) != nil {
				return updates.ApplyResult{}, updates.ErrArtifactRejected
			}
			p, e := cfg.UpdateResolvePlan(ctx, r)
			if e != nil || p.ID != r.PlanID || p.SubjectID != r.Principal.SubjectID || r.Confirmation != p.Attempted.Version || r.BackupMode != "required" {
				return updates.ApplyResult{}, updates.ErrArtifactRejected
			}
			request, e := p.TaskRequest(r.Principal.RequestID)
			if e != nil {
				return updates.ApplyResult{}, e
			}
			t, e := s.Tasks.Submit(ctx, request)
			if e != nil {
				return updates.ApplyResult{}, e
			}
			return updates.ApplyResult{ID: t.ID, TaskID: t.ID, Status: string(t.Status), TargetVersion: p.Attempted.Version}, nil
		}
	}
	controllerExecution, err := s.setupEncryptedBackups(ctx, cfg, workers)
	if err != nil {
		return cleanup(err)
	}
	releaseControllerExecution, err := s.setupReleaseSource(ctx, cfg, workers)
	if err != nil {
		return cleanup(err)
	}
	controllerExecution, err = mergeControllerExecutions(controllerExecution, releaseControllerExecution)
	if err != nil {
		return cleanup(err)
	}
	if publicCertificates != nil {
		dnsExecution, e := publicCertificates.inspectionExecution(cfg.ControllerAuthorityID, workers)
		if e != nil {
			return cleanup(e)
		}
		controllerExecution, err = mergeControllerExecutions(controllerExecution, dnsExecution)
		if err != nil {
			return cleanup(err)
		}
	}
	residentExecution, residentErr := s.setupResidentController(cfg.ResidentSupervisor, cfg.ControllerAuthorityID, workers)
	if residentErr != nil {
		return cleanup(residentErr)
	}
	controllerExecution, err = mergeControllerExecutions(controllerExecution, residentExecution)
	if err != nil {
		return cleanup(err)
	}
	tasksService, err := tasks.NewService(tasks.Options{
		ControllerExecution: controllerExecution,
		RuntimeGate:         runtimeGate,
		Workers:             workers,
		DB:                  db,
		Repo:                tasksRepo,
		Audit:               auditRecorder,
		Authorizer:          authorizer,
		Dispatcher:          dispatcher,
		Nodes:               nodeLookup,
		Now:                 now,
	})
	if err != nil {
		return cleanup(err)
	}
	s.TasksRepo = tasksRepo
	s.Tasks = tasksService
	if agentBridge != nil {
		agentBridge.tasks = tasksService
	}
	if err := s.setupWorkflows(); err != nil {
		return cleanup(err)
	}
	if err := s.setupAI(cfg); err != nil {
		return cleanup(err)
	}
	aiConversation = s.AIConversation
	if err := s.setupPlugins(cfg); err != nil {
		return cleanup(err)
	}
	if err := s.setupUpdatesRecovery(cfg); err != nil {
		return cleanup(err)
	}

	taskBridge := &composedTaskBridge{tasks: tasksService}
	uploads := cfg.UploadTransport
	if localTransport, ok := uploads.(*files.LocalStagingTransport); ok {
		s.FilesTransport = localTransport // Explicit local test adapter only.
	}
	downloads := cfg.DownloadTransport
	if downloads == nil {
		downloads, _ = uploads.(files.DownloadTransport)
	}
	var fileSubmitter files.TaskSubmitter = taskBridge
	var validateTransfer func(context.Context, files.TransferCommand) error
	if s.FileWorker != nil {
		fileSubmitter = s.FileWorker
		validateTransfer = s.FileWorker.ValidateTransfer
	}
	filesService, err := files.NewService(files.Options{
		ValidateTransfer: validateTransfer,
		DB:               db,
		Repo:             filesRepo,
		Audit:            auditRecorder,
		Authorizer:       authorizer,
		Submitter:        fileSubmitter,
		Canceller:        taskBridge,
		Uploads:          uploads,
		Downloads:        downloads,
		StagingDir:       stagingDir,
		Now:              now,
	})
	if err != nil {
		return cleanup(err)
	}

	dnsService, err := dns.NewService(dns.Options{
		DB:         db,
		Repo:       dnsRepo,
		Audit:      auditRecorder,
		Authorizer: authorizer,
		Provider:   cfg.DNSProvider,
		Now:        now,
	})
	if err != nil {
		return cleanup(err)
	}

	notificationsService, err := notifications.NewService(notifications.Options{
		DB:         db,
		Repo:       notificationsRepo,
		Audit:      auditRecorder,
		Authorizer: authorizer,
		SMTPSender: cfg.SMTPSender,
		QQSender:   cfg.QQSender,
		Now:        now,
	})
	if err != nil {
		return cleanup(err)
	}

	sgLookup := &composedServiceGroupLookup{repo: serviceGroupsRepo}
	routeChecker := cfg.GatewayChecker
	if routeChecker == nil && gatewayChain != nil {
		routeChecker, err = newGatewayRouteChecker(s, tasksService, publishingRepo, nodesRepo, serviceGroupsRepo, gatewayRepo, authorizer, gatewayChain.deployment.Gateways)
		if err != nil {
			return cleanup(err)
		}
	}
	gatewayService, err := gateway.NewService(gateway.Options{
		DB:            db,
		Repo:          gatewayRepo,
		Nodes:         nodeLookup,
		ServiceGroups: sgLookup,
		Checker:       routeChecker,
		Tasks:         taskBridge,
		Audit:         auditRecorder,
		Authorizer:    authorizer,
		Now:           now,
	})
	if err != nil {
		return cleanup(err)
	}

	prober := cfg.MonitoringProber
	if prober == nil {
		prober, err = monitoring.NewNetProber(monitoring.NetProberConfig{
			AllowedHosts: cfg.MonitoringAllowedHosts,
		})
		if err != nil {
			return cleanup(err)
		}
	}
	monitoringService, err := monitoring.NewService(monitoring.Options{
		DB:           db,
		Repo:         monitoringRepo,
		Audit:        auditRecorder,
		Authorizer:   authorizer,
		Prober:       prober,
		AllowedHosts: cfg.MonitoringAllowedHosts,
		Now:          now,
	})
	if err != nil {
		return cleanup(err)
	}

	quotaService, err := quota.NewService(quota.Options{
		DB:         db,
		Repo:       quotaRepo,
		Nodes:      nodesRepo,
		Audit:      auditRecorder,
		Authorizer: authorizer,
		Notifier:   notificationsService,
		Now:        now,
	})
	if err != nil {
		return cleanup(err)
	}

	certificatesService, err := certificates.NewService(certificates.Options{
		DB:             db,
		Repo:           certificatesRepo,
		Audit:          auditRecorder,
		Authorizer:     authorizer,
		Issuer:         cfg.CertificateIssuer,
		Tasks:          &composedCertTaskBridge{tasks: tasksService},
		GroupRevisions: sgLookup,
		Now:            now,
	})
	if err != nil {
		return cleanup(err)
	}

	productionBridge := publishing.LocalBridge{}
	if gatewayChain != nil {
		if e := gatewayChain.validate(ctx, gatewayRepo, db, cfg.FileExecution.Paths); e != nil {
			return cleanup(e)
		}
		composed, _, e := NewProductionPublishingBridge(ProductionPublishingConfig{
			Plans: publishingRepo, DNS: dnsService, Certificates: certificatesService,
			Gateways: gatewayService, Nodes: nodesRepo, ServiceGroups: serviceGroupsRepo,
			Tasks: tasksService, TasksRepo: tasksRepo, DB: db,
			Artifacts: gatewayChain.uploader, NextDispatcher: agentBridge, Authorizer: authorizer,
			Dispatcher: gatewayChain.dispatcher, Registry: gatewayChain.registry,
			CertificatePath: gatewayChain.deployment.Certificates, NodeCertificateRoots: gatewayChain.deployment.CertificateRoots, GatewayPaths: gatewayChain.deployment.Gateways,
			Now: now,
		})
		if e != nil {
			return cleanup(e)
		}
		if publicCertificates != nil {
			composed.DNS = publicationDNSStage{manager: publicCertificates}
		}
		productionBridge = composed
		observer, e := NewGatewayArtifactObserver(db, gatewayChain.dispatcher)
		if e != nil {
			return cleanup(e)
		}
		agentBridge.gatewayChild = observer
	}

	cleaner := cfg.CertificateCleanupCleaner
	if cleaner == nil {
		cleaner, err = NewPublicationCleanupCleaner(dnsService)
		if err != nil {
			return cleanup(err)
		}
	}
	s.cleanupEnabled = !cfg.DisableCleanupRetries
	s.cleanupInterval = cfg.CleanupRetryInterval
	if s.cleanupInterval == 0 {
		s.cleanupInterval = 30 * time.Second
	}
	if s.cleanupInterval < time.Second || s.cleanupInterval > time.Hour {
		return cleanup(errors.New("controller: cleanup retry interval must be between one second and one hour"))
	}
	if publicCertificates != nil {
		cleaner = publicCertificateCleaner{manager: publicCertificates, next: cleaner}
	}
	s.CertificateCleanup, err = certificates.NewCleanupJournal(certificates.CleanupJournalOptions{DB: db, Audit: auditRecorder, Authorizer: authorizer, Cleaner: cleaner, Now: now})
	if err != nil {
		return cleanup(err)
	}
	pubBridge := publishing.NewLocalBridge(cfg.PublishingBridge)
	if gatewayChain != nil {
		pubBridge = productionBridge
	}
	publishingService, err := publishing.NewService(publishing.Options{
		DB:           db,
		Repo:         publishingRepo,
		Groups:       sgLookup,
		DNS:          pubBridge.DNS,
		Certificates: pubBridge.Certificates,
		Gateway:      pubBridge.Gateway,
		Audit:        auditRecorder,
		Authorizer:   authorizer,
		Now:          now,
	})
	if err != nil {
		return cleanup(err)
	}

	s.ServiceGroups = serviceGroupsService
	s.Files = filesService
	if s.FileWorker != nil {
		if _, err := s.Files.RecoverTransfers(ctx); err != nil {
			return cleanup(err)
		}
	}
	if _, err := s.Tasks.Recover(ctx); err != nil {
		return cleanup(err)
	}
	s.DNS = dnsService
	s.Notifications = notificationsService
	s.Gateway = gatewayService
	s.Monitoring = monitoringService
	s.Quota = quotaService
	s.Certificates = certificatesService
	s.Publishing = publishingService
	s.mux = http.NewServeMux()
	if err := s.registerRoutes(); err != nil {
		return cleanup(err)
	}
	s.console = console
	if s.ProductUpdates != nil {
		if err := s.ProductUpdates.Start(ctx); err != nil {
			return cleanup(err)
		}
	}
	s.startTaskScheduler()
	return s, nil
}

func (s *Server) startTaskScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	s.runCancel = cancel
	s.runDone = make(chan struct{})
	go func() {
		defer close(s.runDone)
		var workers sync.WaitGroup
		workers.Add(1)
		go func() { defer workers.Done(); _ = s.Tasks.Run(ctx) }()
		if s.cleanupEnabled {
			workers.Add(1)
			go func() { defer workers.Done(); s.runCertificateCleanup(ctx) }()
		}
		if s.Backups != nil && s.backupAutomaticOwner != "" {
			workers.Add(1)
			go func() { defer workers.Done(); s.runBackupSchedule(ctx) }()
		}
		if s.PublicCertificates != nil {
			workers.Add(1)
			go func() { defer workers.Done(); s.runPublicRenewalSchedule(ctx) }()
		}
		workers.Wait()
	}()
}

// Close stops the task scheduler loop before closing the database handle.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	if s.Links != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.Links.Close(closeCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	if s.runCancel != nil {
		s.runCancel()
		select {
		case <-s.runDone:
		case <-time.After(10 * time.Second):
			return errors.New("controller: task scheduler did not stop")
		}
		s.runCancel = nil
	}
	if s.AIRuntime != nil {
		s.AIRuntime.Close()
	}
	if s.PluginVault != nil {
		_ = s.PluginVault.Close()
		s.PluginVault = nil
	}
	if s.ProductUpdates != nil {
		if err := s.ProductUpdates.Close(); err != nil {
			return err
		}
		s.ProductUpdates = nil
	}
	if s.PublicCertificates != nil {
		s.PublicCertificates.Close()
		s.PublicCertificates = nil
	}
	if s.ReleaseAcquirer != nil {
		_ = s.ReleaseAcquirer.Close()
		s.ReleaseAcquirer = nil
	}
	if s.Backups != nil {
		_ = s.Backups.Close()
		s.Backups = nil
	}
	if s.DB == nil {
		return nil
	}
	err := s.DB.Close()
	if s.recoveryCleanup != nil {
		if e := s.recoveryCleanup(); err == nil {
			err = e
		}
		s.recoveryCleanup = nil
	}
	return err
}

// ServeHTTP sets security headers and a request ID before dispatching to the composed mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	reqID := newRequestID()
	w.Header().Set("X-Request-ID", reqID)
	ctx := context.WithValue(r.Context(), requestIDContextKey{}, reqID)
	// Private read-only readiness for the actual owning parent. Business APIs,
	// workers and browser access keep the maintenance gate during replacement.
	if r.Method == "GET" && r.URL.Path == "/internal/resident-ready" && r.URL.RawQuery == "" && s.residentSupervisor != nil {
		nonce := os.Getenv("SF_SUPERVISOR_NONCE")
		if len(nonce) == 32 && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-SF-Resident-Nonce")), []byte(nonce)) == 1 {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"state": "owned_process_ready"})
			})
			recovery.RuntimeHTTP(handler, s.residentHealthGate).ServeHTTP(w, r.WithContext(ctx))
			return
		}
	}
	next := http.Handler(s.mux)
	if s.console != nil && (r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/favicon.ico" || strings.HasPrefix(r.URL.Path, "/assets/")) {
		next = s.console
	}
	recovery.RuntimeHTTP(next, s.runtimeGate).ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) registerRoutes() error {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "stage": "v0.9-wave5"})
	})

	for _, pattern := range []string{
		"/api/v1/auth/",
		"/api/v1/users",
		"/api/v1/users/",
		"/api/v1/roles",
		"/api/v1/permissions",
		"/api/v1/resource-grants",
		"/api/v1/resource-grants/",
		"/api/v1/credentials",
		"/api/v1/credentials/",
	} {
		s.mux.Handle(pattern, s.IdentityHTTP)
	}

	s.registerEnrollmentRoutes()
	s.registerAIRoutes()
	if err := s.RegisterAIBudgetRoutes(); err != nil {
		return err
	}
	s.registerPluginRoutes()
	s.registerUpdateRecoveryRoutes()
	s.registerReleaseSourceRoutes()
	s.registerCertificateCSRRoutes()
	s.registerCertificateInstallRoutes()
	s.registerCertificateSetRoutes()
	s.registerPublicCertificateRoutes()
	s.registerCommandRoutes()
	s.registerResidentUpdateRoutes()
	s.registerRecoveryAuthorityRoutes()
	s.registerFileResumeRoutes()
	s.registerPublicationDNSRoutes()
	if s.AgentLinkHTTP != nil {
		s.mux.Handle("/api/v1/agent/link", s.AgentLinkHTTP)
	} else {
		s.mux.HandleFunc("/api/v1/agent/link", agentTransportUnavailable)
	}
	if s.EnrollmentHTTP != nil {
		s.mux.Handle("/api/v1/agent/enroll", s.EnrollmentHTTP)
	} else {
		s.mux.HandleFunc("/api/v1/agent/enroll", agentTransportUnavailable)
	}

	if err := nodes.RegisterHTTP(s.mux, s.Nodes, func(r *http.Request) (nodes.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return nodes.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := servicegroups.RegisterHTTP(s.mux, s.ServiceGroups, func(r *http.Request) (servicegroups.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return servicegroups.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := tasks.Register(
		s.mux,
		s.Tasks,
		func(r *http.Request) (tasks.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return tasks.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedGroupResolver{groups: s.GroupsRepo, serviceGroups: s.ServiceGroupsRepo},
		resolveOperationClass,
		&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo},
	); err != nil {
		return err
	}
	if err := files.RegisterHTTP(s.mux, s.Files, func(r *http.Request) (files.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return files.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := audit.Register(
		s.mux,
		s.Audit,
		func(r *http.Request) (audit.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return audit.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedAuditReadGate{identity: s.Identity},
	); err != nil {
		return err
	}
	if err := dns.RegisterHTTP(s.mux, s.DNS, func(r *http.Request) (dns.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return dns.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := notifications.RegisterHTTP(
		s.mux,
		s.Notifications,
		func(r *http.Request) (notifications.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return notifications.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedNotificationsReadGate{identity: s.Identity},
	); err != nil {
		return err
	}
	if err := gateway.RegisterHTTP(s.mux, s.Gateway, func(r *http.Request) (gateway.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return gateway.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := monitoring.RegisterHTTP(
		s.mux,
		s.Monitoring,
		&composedTrafficRiskSource{quota: s.Quota},
		func(r *http.Request) (monitoring.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return monitoring.Principal{SubjectID: sub, RequestID: reqID}, err
		},
	); err != nil {
		return err
	}
	if err := quota.RegisterHTTP(s.mux, s.Quota, func(r *http.Request) (quota.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return quota.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := certificates.RegisterHTTP(
		s.mux,
		s.Certificates,
		&composedServiceGroupLookup{repo: s.ServiceGroupsRepo},
		func(r *http.Request) (certificates.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return certificates.Principal{SubjectID: sub, RequestID: reqID}, err
		},
	); err != nil {
		return err
	}
	if err := certificates.RegisterCleanupHTTP(s.mux, s.CertificateCleanup, func(r *http.Request) (certificates.CleanupPrincipal, error) {
		sub, id, err := s.authenticate(r)
		return certificates.CleanupPrincipal{SubjectID: sub, RequestID: id}, err
	}); err != nil {
		return err
	}
	if err := publishing.RegisterHTTP(s.mux, s.Publishing, func(r *http.Request) (publishing.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return publishing.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := s.registerWorkflowRoutes(); err != nil {
		return err
	}
	return nil
}

func (s *Server) authenticate(r *http.Request) (string, string, error) {
	identity, requestID, err := s.authenticateIdentity(r)
	if err != nil {
		return "", "", err
	}
	return identity.SubjectID, requestID, nil
}

func (s *Server) authenticateIdentity(r *http.Request) (auth.Identity, string, error) {
	identity, err := s.IdentityHTTP.AuthenticateRequest(r)
	if err != nil {
		return auth.Identity{}, "", err
	}
	reqID, _ := r.Context().Value(requestIDContextKey{}).(string)
	if reqID == "" {
		reqID = newRequestID()
	}
	return identity, reqID, nil
}

func (s *Server) registerEnrollmentRoutes() {
	patterns := []string{
		"/api/v1/enrollment-tokens",
		"/api/v1/node-enrollment-tokens",
		"/api/v1/enrollments",
		"/api/v1/enrollments/",
		"/api/v1/node-enrollments",
		"/api/v1/node-enrollments/",
	}
	for _, pattern := range patterns {
		if s.EnrollmentManagement != nil {
			s.mux.Handle(pattern, s.EnrollmentManagement)
			continue
		}
		s.mux.HandleFunc(pattern, agentTransportUnavailable)
	}
}

func (s *Server) registerAIRoutes() {
	if s.AIManagement != nil {
		for _, pattern := range []string{
			"GET /api/v1/ai/security-policy",
			"POST /api/v1/ai/sessions",
			"GET /api/v1/ai/sessions/{sessionId}",
			"GET /api/v1/ai/sessions/{sessionId}/approval-policy",
			"PATCH /api/v1/ai/sessions/{sessionId}/approval-policy",
			"POST /api/v1/ai/sessions/{sessionId}/grants",
			"DELETE /api/v1/ai/sessions/{sessionId}/grants/{grantId}",
			"POST /api/v1/ai/sessions/{sessionId}/approvals",
			"POST /api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision",
			"POST /api/v1/ai/tools/execute",
			"POST /api/v1/ai/sessions/{sessionId}/resource-recommendations",
		} {
			s.mux.Handle(pattern, s.AIManagement)
		}
	}
	if s.AIModelsHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/model-settings", s.AIModelsHTTP)
		s.mux.Handle("PUT /api/v1/ai/model-settings", s.AIModelsHTTP)
	}
	if s.AIGenerationHTTP != nil {
		s.mux.Handle("POST /api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate", s.AIGenerationHTTP)
	}
	if s.AIMessagesHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/sessions/{sessionId}/messages", s.AIMessagesHTTP)
		s.mux.Handle("POST /api/v1/ai/sessions/{sessionId}/messages", s.AIMessagesHTTP)
	}
	if s.AIEventsHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/sessions/{sessionId}/events", s.AIEventsHTTP)
	}
}

func (s *Server) registerPluginRoutes() {
	if s.PluginHTTP != nil {
		for _, pattern := range []string{
			"GET /api/v1/plugins",
			"GET /api/v1/plugins/repository",
			"PUT /api/v1/plugins/repository",
			"GET /api/v1/plugins/catalog",
			"POST /api/v1/plugins/catalog/refresh",
			"POST /api/v1/plugins/{pluginId}/install",
			"PATCH /api/v1/plugins/{pluginId}/activation",
			"PUT /api/v1/plugins/{pluginId}/activation",
			"POST /api/v1/plugins/{pluginId}/update",
			"DELETE /api/v1/plugins/{pluginId}",
			"GET /api/v1/plugins/{pluginId}",
			"GET /api/v1/plugin-operations/{operationId}",
		} {
			s.mux.Handle(pattern, s.PluginHTTP)
		}
	}
	if s.XrayHTTP != nil {
		for _, pattern := range s.XrayHTTP.Patterns() {
			s.mux.Handle(pattern, s.XrayHTTP)
		}
	}
}

func agentTransportUnavailable(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(http.StatusServiceUnavailable),
		"status": http.StatusServiceUnavailable, "code": "AGENT_TRANSPORT_UNAVAILABLE",
		"requestId": newRequestID(),
	})
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "req-fallback"
	}
	return "req-" + hex.EncodeToString(raw[:])
}

func newIdentityStore(db *storage.DB, recorder *audit.SQLRecorder) (*auth.SQLIdentityStore, error) {
	statements := func(q storage.Queryer) auth.IdentitySQLStatements {
		return auth.IdentitySQLStatements{
			Read: func(ctx context.Context, query string, args ...any) ([]byte, bool, error) {
				var payload []byte
				var initialized bool
				err := q.QueryRow(ctx, query, args...).Scan(&payload, &initialized)
				return payload, initialized, err
			},
			Exec: func(ctx context.Context, query string, args ...any) (int64, error) {
				result, err := q.Exec(ctx, query, args...)
				if err != nil {
					return 0, err
				}
				return result.RowsAffected()
			},
		}
	}
	backend := auth.IdentitySQLBackend{
		Read: statements(db).Read,
		InTx: func(ctx context.Context, fn func(auth.IdentitySQLStatements) error) error {
			return db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				ops := statements(tx)
				ops.AppendAudit = func(auditCtx context.Context, events []auth.IdentityAuditEvent) error {
					for _, event := range events {
						source := audit.Source(event.Source)
						if source == "" {
							source = audit.SourceWeb
						}
						reqID := event.RequestID
						if reqID == "" {
							reqID = newRequestID()
						}
						if err := recorder.AppendTx(auditCtx, tx, audit.Event{
							SubjectID: event.SubjectID,
							Action:    event.Action,
							Result:    event.Result,
							RequestID: reqID,
							Source:    source,
							At:        event.At,
							NodeIDs:   event.NodeIDs,
						}); err != nil {
							return err
						}
					}
					return nil
				}
				return fn(ops)
			})
		},
	}
	backend.InTxContext = func(ctx context.Context, fn func(context.Context, auth.IdentitySQLStatements) error) error {
		return backend.InTx(ctx, func(ops auth.IdentitySQLStatements) error {
			return fn(ctx, ops)
		})
	}
	return auth.NewSQLIdentityStore(backend)
}

type composedAuthorizer struct {
	identity *auth.IdentityService
}

func (a *composedAuthorizer) Authorize(ctx context.Context, check auth.Check) error {
	if a == nil || a.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	if len(check.NodeIDs) == 0 {
		if check.Action == nodes.ActionGroupsWrite ||
			strings.HasPrefix(check.Action, "dns.") ||
			strings.HasPrefix(check.Action, "notifications.") ||
			strings.HasPrefix(check.Action, "monitoring.") ||
			check.Action == certificates.ActionCertificateRead ||
			strings.HasPrefix(check.Action, "certificates.cleanup.") ||
			check.Action == publishing.ActionRead {
			return a.identity.AuthorizeSubjectAction(ctx, check.SubjectID, check.Action)
		}
		return auth.ErrInvalidCheck
	}
	err := a.identity.Authorize(ctx, check)
	if errors.Is(err, auth.ErrDenied) && (check.Action == "tasks.command" || check.Action == "host.patch") {
		return a.identity.Authorize(ctx, auth.Check{
			SubjectID: check.SubjectID,
			Action:    tasks.ActionSubmit,
			NodeIDs:   check.NodeIDs,
		})
	}
	return err
}

var _ auth.Authorizer = (*composedAuthorizer)(nil)

type composedTaskReadGate struct {
	identity *auth.IdentityService
	tasks    *tasks.Repo
}

func (g *composedTaskReadGate) AuthorizeTaskRead(ctx context.Context, subjectID, taskID string) error {
	if g == nil || g.identity == nil || g.tasks == nil {
		return auth.ErrPolicyUnavailable
	}
	if err := g.identity.AuthorizeSubjectAction(ctx, subjectID, "tasks.read"); err != nil {
		return err
	}
	if taskID == "" {
		return nil
	}
	task, err := g.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.TargetType == tasks.TargetTypeController {
		return g.identity.AuthorizeAdministrator(ctx, subjectID)
	}
	if len(task.Targets) == 0 {
		return nil
	}
	targetIDs := make([]string, 0, len(task.Targets))
	for _, target := range task.Targets {
		targetIDs = append(targetIDs, target.NodeID)
	}
	return g.identity.Authorize(ctx, auth.Check{
		SubjectID: subjectID,
		Action:    "tasks.read",
		NodeIDs:   targetIDs,
	})
}

type composedAuditReadGate struct {
	identity *auth.IdentityService
}

func (g *composedAuditReadGate) AuthorizeAuditRead(ctx context.Context, subjectID string) error {
	if g == nil || g.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	return g.identity.AuthorizeSubjectAction(ctx, subjectID, "audit.read")
}

type composedNotificationsReadGate struct {
	identity *auth.IdentityService
}

func (g *composedNotificationsReadGate) AuthorizeNotificationsRead(ctx context.Context, subjectID string) error {
	if g == nil || g.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	return g.identity.AuthorizeSubjectAction(ctx, subjectID, "notifications.read")
}

type composedGroupResolver struct {
	groups        *nodes.GroupRepo
	serviceGroups *servicegroups.Repo
}

func (r *composedGroupResolver) ResolveTargets(ctx context.Context, targetType, groupID string) ([]string, error) {
	switch targetType {
	case "node_group":
		group, err := r.groups.Get(ctx, groupID)
		if err != nil {
			return nil, err
		}
		return append([]string(nil), group.Members...), nil
	case "service_group":
		group, err := r.serviceGroups.Get(ctx, groupID)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(group.Members))
		for _, m := range group.Members {
			out = append(out, m.NodeID)
		}
		return out, nil
	default:
		return nil, tasks.ErrInvalidTask
	}
}

func resolveOperationClass(actionID string) (tasks.OperationClass, error) {
	if strings.TrimSpace(actionID) == "" || strings.ContainsAny(actionID, " \t\r\n:") {
		return "", tasks.ErrUnknownOperationClass
	}
	switch actionID {
	case tasks.ActionSubmit, "tasks.command", files.ActionTransfer:
		return tasks.OperationClassStandard, nil
	case "host.root":
		return tasks.OperationClassRoot, nil
	default:
		return tasks.OperationClassModifying, nil
	}
}

type composedNodeLookup struct {
	nodes *nodes.Repo
	now   func() time.Time
}

func (l *composedNodeLookup) Online(ctx context.Context, nodeID string) (bool, error) {
	node, err := l.nodes.Get(ctx, nodeID)
	if err != nil {
		return false, err
	}
	return node.Status == nodes.StatusOnline, nil
}

func (l *composedNodeLookup) LookupNode(ctx context.Context, nodeID string) (gateway.NodeRef, bool, error) {
	node, err := l.nodes.Get(ctx, nodeID)
	if storage.IsNotFound(err) {
		return gateway.NodeRef{}, false, nil
	}
	if err != nil {
		return gateway.NodeRef{}, false, err
	}
	return gateway.NodeRef{
		ID:     node.ID,
		Online: node.Status == nodes.StatusOnline,
	}, true, nil
}

type composedServiceGroupLookup struct {
	repo *servicegroups.Repo
}

func (l *composedServiceGroupLookup) LookupServiceGroup(ctx context.Context, serviceGroupID string) (gateway.ServiceGroupRef, bool, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if storage.IsNotFound(err) {
		return gateway.ServiceGroupRef{}, false, nil
	}
	if err != nil {
		return gateway.ServiceGroupRef{}, false, err
	}
	return gateway.ServiceGroupRef{
		ID:       group.ID,
		Revision: group.Revision,
	}, true, nil
}

func (l *composedServiceGroupLookup) Revision(ctx context.Context, serviceGroupID string) (uint64, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if err != nil {
		return 0, err
	}
	return group.Revision, nil
}

func (l *composedServiceGroupLookup) Members(ctx context.Context, serviceGroupID string) ([]string, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(group.Members))
	for _, m := range group.Members {
		out = append(out, m.NodeID)
	}
	return out, nil
}

func (l *composedServiceGroupLookup) LookupGroup(ctx context.Context, serviceGroupID string) (publishing.GroupRef, bool, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if storage.IsNotFound(err) {
		return publishing.GroupRef{}, false, nil
	}
	if err != nil {
		return publishing.GroupRef{}, false, err
	}
	nodeIDs := make([]string, 0, len(group.Members))
	for _, m := range group.Members {
		nodeIDs = append(nodeIDs, m.NodeID)
	}
	return publishing.GroupRef{
		ID:       group.ID,
		Revision: group.Revision,
		NodeIDs:  nodeIDs,
	}, true, nil
}

type composedTrafficRiskSource struct {
	quota *quota.Service
}

func (s *composedTrafficRiskSource) ListTrafficRisks(ctx context.Context, subjectID string) ([]monitoring.TrafficRisk, error) {
	if s == nil || s.quota == nil {
		return nil, errors.New("quota: traffic risk source is unavailable")
	}
	items, err := s.quota.ListTrafficRisks(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]monitoring.TrafficRisk, 0, len(items))
	for _, item := range items {
		out = append(out, monitoring.TrafficRisk{
			NodeID:    item.NodeID,
			Kind:      item.Kind,
			Severity:  item.Severity,
			Estimated: item.Estimated,
		})
	}
	return out, nil
}

type composedCertTaskBridge struct {
	tasks *tasks.Service
}

func (b *composedCertTaskBridge) Submit(ctx context.Context, req certificates.DistributionTask) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      req.SubjectID,
		RequestID:      newRequestID(),
		Name:           "certificate distribution " + req.CertificateID,
		ActionID:       certificates.ActionDistributionCreate,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  req.NodeIDs,
		Params: map[string]string{
			"certificate_id":   req.CertificateID,
			"service_group_id": req.ServiceGroupID,
		},
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

type composedTaskBridge struct {
	tasks *tasks.Service
}

func (b *composedTaskBridge) SubmitTransfer(ctx context.Context, command files.TransferCommand) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      command.SubjectID,
		RequestID:      command.RequestID,
		Name:           "file transfer",
		ActionID:       files.ActionTransfer,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  []string{command.SourceNodeID, command.DestinationNodeID},
		Params: map[string]string{
			"source_node":     command.SourceNodeID,
			"dest_node":       command.DestinationNodeID,
			"dest_dir":        command.DestinationDir,
			"conflict_policy": string(command.ConflictPolicy),
		},
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func (b *composedTaskBridge) CancelTask(ctx context.Context, taskID, subjectID, requestID string) error {
	_, err := b.tasks.Cancel(ctx, taskID, subjectID, requestID)
	return err
}

func (b *composedTaskBridge) Submit(ctx context.Context, request gateway.TaskRequest) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      request.SubjectID,
		RequestID:      request.RequestID,
		Name:           "gateway provision " + request.ResourceID,
		ActionID:       request.ActionID,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  request.NodeIDs,
		Params:         request.Params,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

type composedEligibility struct {
	nodes    *nodes.Repo
	gateways *gateway.Repo
	db       *storage.DB
	now      func() time.Time
}

func (e *composedEligibility) GatewayEligible(ctx context.Context, _ string) (bool, error) {
	list, err := e.gateways.ListGateways(ctx, e.db)
	if err != nil {
		return false, err
	}
	for _, gw := range list {
		node, err := e.nodes.Get(ctx, gw.NodeID)
		if err != nil {
			continue
		}
		if gw.SchedulingEligible(node.Status == nodes.StatusOnline) {
			return true, nil
		}
	}
	return false, nil
}

func (e *composedEligibility) NodeHealthy(ctx context.Context, nodeID string) (bool, string, error) {
	node, err := e.nodes.Get(ctx, nodeID)
	if err != nil {
		return false, "node_not_found", err
	}
	if node.Status != nodes.StatusOnline {
		return false, "node_offline", nil
	}
	return true, "online", nil
}

type composedDeleteGuard struct {
	gateways *gateway.Repo
	db       *storage.DB
}

func (g *composedDeleteGuard) CheckDelete(ctx context.Context, serviceGroupID string) error {
	routes, err := g.gateways.ListRoutes(ctx, g.db, "")
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ServiceGroupID == serviceGroupID {
			return errors.New("servicegroups: service group is still referenced by gateway routes")
		}
	}
	return nil
}
