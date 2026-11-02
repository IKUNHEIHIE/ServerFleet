package updates

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

type productDeployment struct {
	mu         sync.Mutex
	o          ProductDeploymentOptions
	root       *os.Root
	source     *DirectoryReleaseSource
	stager     *ArtifactStager
	installer  *SlotInstaller
	supervisor *OwnedProcessSupervisor
	binding    atomic.Pointer[productBinding]
	closing    atomic.Bool
	installed  ReleaseManifest
}

type productBinding struct {
	task tasks.Task
	plan FrozenInstallPlan
}

func productPrivate(dir string) bool {
	i, e := os.Lstat(dir)
	if e != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSymlink != 0 {
		return false
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && os.Geteuid() != 0
}

func NewProductDeployment(o ProductDeploymentOptions) (ProductDeployment, error) {
	if o.DB == nil || o.Audit == nil || !productPrivate(o.Directory) || !productPrivate(o.Inbox) || !productPrivate(o.StageDir) || !installID(o.NodeID) || !installID(o.KeyID) || o.Initial.Component != "agent" || !installedManifest(o.Initial) || o.Current == nil || o.Administrator == nil || o.RuntimeCurrent == nil || o.NodeCurrent == nil || !filepath.IsAbs(o.IdentityDir) || !filepath.IsAbs(o.RootCertificate) || o.SchemaVersion < 0 || !validPlanDigest(o.SchemaFingerprint) {
		return nil, ErrArtifactRejected
	}
	o.Policy = cloneReleasePolicy(o.Policy)
	p := &productDeployment{o: o}
	var err error
	p.root, err = os.OpenRoot(o.Directory)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			p.Close()
		}
	}()
	p.stager, err = NewArtifactStager(o.StageDir)
	if err != nil {
		return nil, err
	}
	p.source, err = NewDirectoryReleaseSource(o.Inbox, p.stager)
	if err != nil {
		return nil, err
	}
	p.installer, err = NewSlotInstaller(o.Directory)
	if err != nil {
		return nil, err
	}
	// A crash or unknown prior effect never silently starts another product.
	for _, name := range []string{"update-lock.json", "supervisor-reservation.json"} {
		if _, e := p.root.Lstat(name); !errors.Is(e, os.ErrNotExist) {
			return nil, ErrUpdateBusy
		}
	}
	ok = true
	return p, nil
}

func (p *productDeployment) schema(ctx context.Context) error {
	var v int
	if p.o.RuntimeCurrent(ctx) != nil || p.o.DB.QueryRow(ctx, `PRAGMA user_version`).Scan(&v) != nil || v != p.o.SchemaVersion {
		return ErrApplyUnknown
	}
	// user_version alone does not detect a changed DDL. The trusted installer
	// pins the reviewed sqlite_master fingerprint; a mismatch is UNKNOWN here.
	got, e := SQLiteSchemaFingerprint(ctx, p.o.DB)
	if e != nil || got != p.o.SchemaFingerprint {
		return ErrApplyUnknown
	}
	return nil
}

func (p *productDeployment) verifyInstalled(ctx context.Context, m ReleaseManifest) error {
	policy := p.o.Policy
	policy.MinimumSequence = 0
	staged, e := p.source.Stage(ctx, m.Version, p.o.KeyID, policy)
	if e != nil {
		return e
	}
	defer staged.Close()
	if staged.Manifest() != m || p.installer.verifyFile(ctx, "active", m) != nil {
		return ErrArtifactRejected
	}
	return nil
}

func (p *productDeployment) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.supervisor != nil || p.schema(ctx) != nil {
		return ErrArtifactRejected
	}
	var raw []byte
	var floor uint64
	err := p.o.DB.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&raw, &floor)
	if storage.IsNotFound(err) {
		// Establish initial provenance ONLY from matching signed inbox bytes AND
		// the actual active executable, under trusted startup authority + audit.
		if p.verifyInstalled(ctx, p.o.Initial) != nil {
			return ErrArtifactRejected
		}
		raw, _ = json.Marshal(p.o.Initial)
		err = p.o.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			if p.o.RuntimeCurrent(ctx) != nil {
				return ErrArtifactRejected
			}
			if _, e := tx.Exec(ctx, `INSERT INTO cx_update_installed(component,payload,sequence_floor) VALUES('agent',?,?)`, raw, p.o.Initial.Sequence); e != nil {
				return e
			}
			return p.o.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: "local-installer", Action: ActionInstall, Result: "success", RequestID: "initial-product-provenance", Source: audit.SourceSystem, ReasonCode: "verified_initial_provenance", At: time.Now().UTC()})
		})
		floor = p.o.Initial.Sequence
	}
	if err != nil || json.Unmarshal(raw, &p.installed) != nil || !installedManifest(p.installed) || floor < p.installed.Sequence || p.verifyInstalled(ctx, p.installed) != nil {
		return ErrArtifactRejected
	}
	gate := func(ctx context.Context, _ ReleaseManifest, phase string) error {
		if p.closing.Load() {
			// Only the outer lifecycle may stop the process it owns; every other
			// gate (install/health/start) must abort without taking over or killing.
			if phase == "supervisor-stop" {
				return nil
			}
			return ErrApplyUnknown
		}
		b := p.binding.Load()
		if b == nil {
			return p.schema(ctx)
		}
		return p.live(ctx, b.plan)
	}
	var e error
	p.supervisor, e = NewOwnedProcessSupervisor(OwnedSupervisorConfig{
		Directory:   p.o.Directory,
		ExecutionID: "owned-product",
		Arguments:   []string{"-serve", "-state-dir", p.o.IdentityDir, "-controller-url", p.o.ControllerOrigin, "-root-cert", p.o.RootCertificate},
		Environment: []string{"SF_AGENT_HEALTH_FILE=" + filepath.Join(p.o.Directory, "health.json")},
		Gate:        gate,
		Ready:       AgentProductReady(filepath.Join(p.o.Directory, "health.json"), p.o.NodeID, p.o.NodeCurrent),
	})
	if e != nil {
		return e
	}
	return p.supervisor.Start(ctx, p.installed)
}

func (p *productDeployment) live(ctx context.Context, plan FrozenInstallPlan) error {
	if p.schema(ctx) != nil {
		return ErrApplyUnknown
	}
	if plan.SchemaFingerprint != p.o.SchemaFingerprint {
		// A plan approved under another pinned schema may not be replayed.
		return ErrArtifactRejected
	}
	b := p.binding.Load()
	if b == nil || b.plan != plan {
		return ErrArtifactRejected
	}
	t, e := p.o.Current(ctx, b.task.ID, b.task.PlanHash)
	if e != nil || t.SubmitterID != plan.SubjectID || t.Params != b.task.Params || p.o.Administrator(ctx, plan.SubjectID) != nil {
		return ErrArtifactRejected
	}
	return nil
}

func (p *productDeployment) WorkerOptions() *TaskInstallWorkerOptions {
	return &TaskInstallWorkerOptions{Installer: p.installer, Current: p.o.Current, Administrator: p.o.Administrator, BackupCurrent: p.backupCurrent, Resolve: p.resolve}
}

const productBackupMaxBytes = 256 << 20

func (p *productDeployment) backupCurrent(ctx context.Context, plan FrozenInstallPlan) error {
	if p.schema(ctx) != nil || plan.SchemaFingerprint != p.o.SchemaFingerprint {
		return ErrArtifactRejected
	}
	file, e := p.installer.openRegular("backup-"+plan.ID+".db", productBackupMaxBytes)
	if e != nil {
		return ErrArtifactRejected
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > productBackupMaxBytes {
		return ErrArtifactRejected
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(file, info.Size()+1))
	if e != nil || n != info.Size() || hex.EncodeToString(h.Sum(nil)) != plan.BackupSHA256 {
		return ErrArtifactRejected
	}
	return storage.VerifyFile(ctx, filepath.Join(p.o.Directory, "backup-"+plan.ID+".db"), true)
}

func (p *productDeployment) Plan(ctx context.Context, version string) (Plan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	principal, ok := ctx.Value(updatePrincipalKey{}).(Principal)
	if !ok || !principal.Administrator || p.o.Administrator(ctx, principal.SubjectID) != nil || p.supervisor == nil || p.schema(ctx) != nil {
		return Plan{}, ErrArtifactRejected
	}
	var currentRaw []byte
	var floor uint64
	var current ReleaseManifest
	if p.o.DB.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&currentRaw, &floor) != nil || json.Unmarshal(currentRaw, &current) != nil || p.installer.verifyFile(ctx, "active", current) != nil {
		return Plan{}, ErrArtifactRejected
	}
	policy := p.o.Policy
	policy.MinimumSequence = floor
	staged, e := p.source.Stage(ctx, version, p.o.KeyID, policy)
	if e != nil {
		return Plan{}, e
	}
	defer staged.Close()
	// Plans survive restart outside HTTP memory. Each has a distinct verified
	// consistent backup; the browser never supplies backup paths or hashes.
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return Plan{}, e
	}
	id := "product-" + hex.EncodeToString(nonce[:])
	backup := filepath.Join(p.o.Directory, "backup-"+id+".db")
	if p.o.DB.BackupTo(ctx, backup) != nil {
		return Plan{}, ErrArtifactRejected
	}
	if os.Chmod(backup, 0600) != nil {
		return Plan{}, ErrApplyUnknown
	}
	if info, e := os.Lstat(backup); e != nil || !info.Mode().IsRegular() || info.Size() > productBackupMaxBytes {
		return Plan{}, ErrArtifactRejected
	}
	hash, e := storage.ChecksumFile(backup)
	if e != nil {
		return Plan{}, e
	}
	frozen := FrozenInstallPlan{ID: id, SubjectID: principal.SubjectID, NodeID: p.o.NodeID, BackupSHA256: hash, PolicySHA256: ReleasePolicyDigest(policy), SchemaFingerprint: p.o.SchemaFingerprint, Previous: current, Attempted: staged.Manifest(), AllowRollback: true}
	if !frozen.valid() {
		return Plan{}, ErrArtifactRejected
	}
	raw, e := json.Marshal(frozen)
	if e != nil || len(raw) > 1024 {
		return Plan{}, ErrArtifactRejected
	}
	f, e := p.root.OpenFile("plan-"+id+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return Plan{}, e
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || syncStagingDirectory(p.root, ".") != nil {
		return Plan{}, ErrApplyUnknown
	}
	auditErr := p.o.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if p.o.Administrator(ctx, principal.SubjectID) != nil || p.o.RuntimeCurrent(ctx) != nil {
			return ErrArtifactRejected
		}
		return p.o.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: principal.SubjectID, Action: ActionInstall, Result: "success", RequestID: principal.RequestID, Source: audit.SourceWeb, ReasonCode: "plan_prepared", At: time.Now().UTC()})
	})
	if auditErr != nil {
		// A definite revocation has no audit/provenance; remove the unissued plan
		// and its backup. Anything else keeps the files and reports UNKNOWN.
		if errors.Is(auditErr, ErrArtifactRejected) {
			if p.discardPlan(id) == nil {
				return Plan{}, ErrArtifactRejected
			}
		}
		return Plan{}, ErrApplyUnknown
	}
	return Plan{PlanID: id, TargetVersion: version, Eligible: true, BackupRequired: true, PreflightChecks: []PreflightCheck{{Name: "signed artifact and installed provenance", Passed: true}, {Name: "reviewed current schema fingerprint", Passed: true}}}, nil
}

func (p *productDeployment) discardPlan(id string) error {
	if !installID(id) {
		return ErrArtifactRejected
	}
	removeErr := p.root.Remove("plan-" + id + ".json")
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	removeErr = p.root.Remove("backup-" + id + ".db")
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return syncStagingDirectory(p.root, ".")
}

func (p *productDeployment) ResolvePlan(ctx context.Context, r ApplyRequest) (FrozenInstallPlan, error) {
	var frozen FrozenInstallPlan
	if !installID(r.PlanID) || p.o.Administrator(ctx, r.Principal.SubjectID) != nil {
		return frozen, ErrArtifactRejected
	}
	f, e := p.installer.openRegular("plan-"+r.PlanID+".json", 1024)
	if e != nil {
		return frozen, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 1025))
	if e != nil || len(raw) > 1024 || json.Unmarshal(raw, &frozen) != nil || !frozen.valid() || frozen.ID != r.PlanID || frozen.SubjectID != r.Principal.SubjectID || frozen.SchemaFingerprint != p.o.SchemaFingerprint {
		return FrozenInstallPlan{}, ErrArtifactRejected
	}
	return frozen, nil
}

func (p *productDeployment) resolve(ctx context.Context, plan FrozenInstallPlan, intent InstallIntent) (PreparedInstall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	repo, e := tasks.NewRepo(p.o.DB)
	if e != nil {
		return PreparedInstall{}, e
	}
	t, e := repo.Get(ctx, intent.TaskID)
	if e != nil {
		return PreparedInstall{}, e
	}
	if prior := p.binding.Load(); prior != nil {
		old, e := repo.Get(ctx, prior.task.ID)
		if e != nil || !old.Status.Terminal() {
			return PreparedInstall{}, ErrUpdateBusy
		}
	}
	p.binding.Store(&productBinding{t, plan})
	fail := func(err error) (PreparedInstall, error) {
		// Never leave a half-open binding that could pass a later gate.
		if b := p.binding.Load(); b != nil && b.plan == plan && b.task.ID == t.ID {
			p.binding.CompareAndSwap(b, nil)
		}
		return PreparedInstall{}, err
	}
	if p.live(ctx, plan) != nil {
		return fail(ErrArtifactRejected)
	}
	policy := p.o.Policy
	policy.MinimumSequence = plan.Attempted.Sequence - 1
	// Use the actual persisted floor, never assume sequence increments by one.
	if p.o.DB.QueryRow(ctx, `SELECT sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&policy.MinimumSequence) != nil || ReleasePolicyDigest(policy) != plan.PolicySHA256 {
		return fail(ErrArtifactRejected)
	}
	staged, e := p.source.Stage(ctx, plan.Attempted.Version, p.o.KeyID, policy)
	if e != nil {
		return fail(e)
	}
	journal, e := NewSQLInstallJournal(p.o.DB, p.o.Audit, func(ctx context.Context, id, digest, subject string) error {
		if id != t.ID || digest != t.PlanHash || subject != plan.SubjectID {
			return ErrArtifactRejected
		}
		return p.live(ctx, plan)
	})
	if e != nil {
		staged.Close()
		return fail(e)
	}
	return PreparedInstall{PlanID: plan.ID, SubjectID: plan.SubjectID, Staged: staged, Policy: policy, Intent: intent, Hooks: InstallHooks{
		Gate: func(ctx context.Context, _ InstallIntent, _ ReleaseManifest, _ string) error {
			return p.live(ctx, plan)
		},
		Stop:  p.supervisor.Stop,
		Start: p.supervisor.Start,
		Health: func(ctx context.Context, m ReleaseManifest) (HealthEvidence, error) {
			h, e := p.supervisor.Health(ctx, m)
			if e != nil {
				return h, e
			}
			if p.live(ctx, plan) != nil {
				return HealthEvidence{}, ErrApplyUnknown
			}
			h.ExecutionID = intent.ExecutionID
			return h, nil
		},
		RollbackSafe: func(ctx context.Context, _, _ ReleaseManifest) error { return p.schema(ctx) },
		Commit: func(ctx context.Context, r InstallReceipt) (string, error) {
			return journal.Commit(ctx, plan.SubjectID, t.PlanHash, r)
		},
	}}, nil
}

func (p *productDeployment) Check(ctx context.Context, channel string) (CheckResult, error) {
	if p.schema(ctx) != nil {
		return CheckResult{}, ErrArtifactRejected
	}
	var raw []byte
	var current ReleaseManifest
	var floor uint64
	if p.o.DB.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&raw, &floor) != nil || json.Unmarshal(raw, &current) != nil || !installedManifest(current) {
		return CheckResult{}, ErrArtifactRejected
	}
	if p.o.CandidateVersion == current.Version {
		return CheckResult{CurrentVersion: current.Version, LatestVersion: current.Version, Channel: channel, CheckedAt: time.Now().UTC()}, nil
	}
	policy := p.o.Policy
	policy.MinimumSequence = floor
	staged, e := p.source.Stage(ctx, p.o.CandidateVersion, p.o.KeyID, policy)
	if e != nil {
		// Only a verified local candidate is announced; no invented online feed.
		return CheckResult{}, ErrUpdateSourceUnavailable
	}
	defer staged.Close()
	return CheckResult{CurrentVersion: current.Version, LatestVersion: staged.Manifest().Version, Channel: channel, UpdateAvailable: true, SignatureVerified: true, PlatformCompatible: true, CheckedAt: time.Now().UTC()}, nil
}

func (p *productDeployment) Close() error {
	if p == nil {
		return nil
	}
	p.closing.Store(true)
	if p.supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.supervisor.Close(ctx)
		cancel()
		if err != nil {
			return err
		}
	}
	if p.installer != nil {
		p.installer.Close()
	}
	if p.source != nil {
		p.source.Close()
	}
	if p.stager != nil {
		p.stager.Close()
	}
	if p.root != nil {
		return p.root.Close()
	}
	return nil
}
