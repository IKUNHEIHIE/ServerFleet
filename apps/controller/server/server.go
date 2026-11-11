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
	"strings"
	"sync"
	"time"
)

// Config configures the composed Controller management runtime.
type Config struct {
	InstallationCatalog       *InstallationCatalog
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
	notificationAuthority string
	residentHealthGate    func(context.Context) error
	recoveryCleanup       func() error
	residentSupervisor    *updates.ResidentSupervisorConfig
	buildVersion          string
	dbPath                string
	ProductUpdates        updates.ProductDeployment
	ReleaseAcquirer       *updates.ReleaseAcquirer
	PublicCertificates    *publicCertificateExecution
	runtimeGate           func(context.Context) error
	DB                    *storage.DB
	Audit                 *audit.SQLRecorder
	IdentityStore         *auth.SQLIdentityStore
	Identity              *auth.IdentityService
	Credentials           *auth.CredentialService
	IdentityHTTP          *auth.IdentityHTTP
	NodesRepo             *nodes.Repo
	GroupsRepo            *nodes.GroupRepo
	ServiceGroupsRepo     *servicegroups.Repo
	MonitoringRepo        *monitoring.Repo
	QuotaRepo             *quota.Repo
	CertificatesRepo      *certificates.Repo
	PublishingRepo        *publishing.Repo
	Nodes                 *nodes.Service
	ServiceGroups         *servicegroups.Service
	TasksRepo             *tasks.Repo
	Tasks                 *tasks.Service
	Files                 *files.Service
	FilesTransport        *files.LocalStagingTransport
	DNS                   *dns.Service
	Notifications         *notifications.Service
	Gateway               *gateway.Service
	Monitoring            *monitoring.Service
	Quota                 *quota.Service
	Certificates          *certificates.Service
	CertificateCleanup    *certificates.CleanupJournal
	cleanupEnabled        bool
	cleanupInterval       time.Duration
	Publishing            *publishing.Service

	Enrollment           *transport.EnrollmentService
	EnrollmentHTTP       *transport.EnrollmentHTTP
	EnrollmentManagement *transport.EnrollmentManagementHTTP
	Links                *transport.ControllerLinks
	AgentFiles           *transport.FileClient
	FileWorker           *FileExecutionWorker
	AgentLinkHTTP        *transport.AgentLinkHTTP

	AI               *ai.SessionService
	AIAgent          *ai.AgentLoop
	AIAgentHTTP      *ai.AgentHTTP
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
	BackupMaintenance    *recovery.BackupMaintenance
	Incidents            *tasks.IncidentService
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
		if s != nil && s.AIAgent != nil {
			s.AIAgent.Close()
		}
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
			if (op.Module == "smtp" || op.Module == "qqbot") && s != nil {
				return s.notificationCredentialGate(ctx, op)
			}
			if op.Module == "backups" && s != nil {
				return s.backupCredentialGate(ctx, op)
			}
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

	eligibility := &composedEligibility{nodes: nodesRepo, gateways: gatewayRepo, db: db}
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

	nodeLookup := &composedNodeLookup{nodes: nodesRepo}
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
	notificationExecution, e := s.setupNotificationExecution(cfg.ControllerAuthorityID, workers)
	if e != nil {
		return cleanup(e)
	}
	controllerExecution, err = mergeControllerExecutions(controllerExecution, notificationExecution)
	if err != nil {
		return cleanup(err)
	}
	tasksService, err := tasks.NewService(tasks.Options{
		TaskCurrent:         s.currentTask,
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
	if err := s.setupOperatorIncidents(); err != nil {
		return cleanup(err)
	}
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

	smtpSender, qqSender := cfg.SMTPSender, cfg.QQSender
	var notificationDispatch notifications.DeliveryDispatcher
	var prepareNotification notifications.CredentialBindingPreparer = s.prepareNotificationCredential
	var currentNotification notifications.DeliveryAuthority
	if s.notificationAuthority != "" {
		notificationDispatch = s.dispatchNotification
		prepareNotification = s.prepareNotificationCredential
		currentNotification = s.notificationCurrent
		borrower := notifications.SecretBorrowerFunc(s.borrowNotification)
		if smtpSender == nil {
			smtpSender, err = notifications.NewSMTPMailer(notifications.SMTPConfig{Secrets: borrower, Current: s.notificationIOCurrent})
			if err != nil {
				return cleanup(err)
			}
		}
		if qqSender == nil {
			qqSender, err = notifications.NewQQBotSender(notifications.QQConfig{Secrets: borrower, Current: s.notificationIOCurrent})
			if err != nil {
				return cleanup(err)
			}
		}
	}
	notificationsService, err := notifications.NewService(notifications.Options{
		DB:         db,
		Repo:       notificationsRepo,
		Audit:      auditRecorder,
		Authorizer: authorizer,
		SMTPSender: smtpSender,
		QQSender:   qqSender,
		Dispatcher: notificationDispatch, PrepareCredential: prepareNotification, CurrentDelivery: currentNotification,
		Now: now,
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

	var monitorAuthority monitoring.AutomationAuthority
	var quotaAuthority quota.AutomationAuthority
	var alertNotifier monitoring.Notifier = notificationsService
	var quotaNotifier quota.Notifier = notificationsService
	if s.notificationAuthority != "" {
		monitorAuthority = s.monitorAutomationCurrent
		quotaAuthority = s.quotaAutomationCurrent
		alertNotifier = durableAlertAdapter{notificationsService}
		quotaNotifier = durableAlertAdapter{notificationsService}
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
		RemoteProber: nodeProbeClient{s}, SourceValidator: s.validateProbeSource,
		Prober: prober, Notifier: alertNotifier, AutomationCurrent: monitorAuthority,
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
		Notifier:   quotaNotifier, AutomationCurrent: quotaAuthority,
		Now: now,
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
	if agentBridge != nil {
		agentBridge.probeCurrent = s.nodeProbeTaskCurrent
		agentBridge.recordProbe = s.recordNodeProbe
	}
	s.Quota = quotaService
	if agentBridge != nil {
		agentBridge.recordTraffic = s.recordTraffic
	}
	s.Certificates = certificatesService
	s.Publishing = publishingService
	s.mux = http.NewServeMux()
	s.registerInstallationCatalogRoutes(cfg.InstallationCatalog)
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
		if s.Backups != nil {
			workers.Add(1)
			go func() { defer workers.Done(); s.runBackupSchedule(ctx) }()
		}
		if s.Incidents != nil {
			workers.Add(1)
			go func() { defer workers.Done(); s.runOperatorIncidentMonitor(ctx) }()
		}
		if s.PublicCertificates != nil {
			workers.Add(1)
			go func() { defer workers.Done(); s.runPublicRenewalSchedule(ctx) }()
		}
		if s.notificationAuthority != "" {
			workers.Add(3)
			go func() { defer workers.Done(); s.runMonitorAutomation(ctx) }()
			go func() { defer workers.Done(); s.runQuotaAutomation(ctx) }()
			go func() { defer workers.Done(); s.runNotificationDispatch(ctx) }()
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
	if s.AIAgent != nil {
		s.AIAgent.Close()
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

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "req-fallback"
	}
	return "req-" + hex.EncodeToString(raw[:])
}
