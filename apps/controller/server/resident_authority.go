package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
	"time"
)

// Outside the child lifecycle, but using domain-owned repositories and live
// identity reads. It opens no HTTP server, scheduler or execution fallback.
func ControllerSupervisorHooks(ctx context.Context, c updates.ResidentSupervisorConfig) (updates.ResidentSupervisorHooks, func() error, error) {
	var empty updates.ResidentSupervisorHooks
	if c.Component != "controller" || !transport.FileDigest(c.SchemaSHA256) || !filepath.IsAbs(c.Database) {
		return empty, nil, updates.ErrArtifactRejected
	}
	// Restored authorities require the live restore verifier, not this ordinary
	// profile. Never use a supervisor to bypass a restoration quarantine.
	if _, e := os.Lstat(filepath.Join(filepath.Dir(c.Database), "hold.json")); !errors.Is(e, os.ErrNotExist) {
		return empty, nil, updates.ErrArtifactRejected
	}
	db, e := storage.Open(ctx, storage.Config{Path: c.Database})
	if e != nil {
		return empty, nil, e
	}
	fail := func(e error) (updates.ResidentSupervisorHooks, func() error, error) { db.Close(); return empty, nil, e }
	validator, e := audit.NewValidator(AuditActions(), audit.DefaultLimits())
	if e != nil {
		return fail(e)
	}
	recorder, e := audit.NewSQLRecorder(db, validator, time.Now)
	if e != nil {
		return fail(e)
	}
	store, e := newIdentityStore(db, recorder)
	if e != nil {
		return fail(e)
	}
	identity, e := auth.NewIdentityServiceWithResources(store, func(context.Context, string, string) ([]string, error) { return nil, auth.ErrDenied }, ExtraPermissions())
	if e != nil {
		return fail(e)
	}
	repo, e := tasks.NewRepo(db)
	if e != nil {
		return fail(e)
	}
	admitted := map[string]transport.ResidentUpdateIntent{}
	schema := func(ctx context.Context) error {
		actual, e := updates.SQLiteSchemaFingerprint(ctx, db)
		if e != nil || actual != c.SchemaSHA256 {
			return updates.ErrApplyUnknown
		}
		return nil
	}
	current := func(ctx context.Context, i transport.ResidentUpdateIntent, phase string) error {
		if !i.Valid(time.Now()) || i.NodeID != c.NodeID || schema(ctx) != nil {
			return updates.ErrApplyUnknown
		}
		if _, e := os.Lstat(filepath.Join(filepath.Dir(c.Database), "hold.json")); !errors.Is(e, os.ErrNotExist) {
			return updates.ErrApplyUnknown
		}
		t, e := repo.Get(ctx, i.TaskID)
		if e != nil || t.ActionID != transport.ControllerUpdateAction || t.PlanHash != i.PlanSHA256 || t.Params != i.Parameters || t.TargetType != tasks.TargetTypeController || t.TargetResourceID != c.NodeID || len(t.Targets) != 1 || t.Targets[0].NodeID != c.NodeID || t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" || !t.ApprovalRequired || t.CancelRequested || t.OperationClass != tasks.OperationClassModifying || i.ExecutionID != executionIDFor(t.ID, c.NodeID, t.PlanHash) {
			return updates.ErrApplyUnknown
		}
		approval, e := repo.Approval(ctx, db, t.ID, t.Revision)
		if e != nil || !approval.Approved() || approval.PlanHash != t.PlanHash || identity.AuthorizeAdministrator(ctx, t.SubmitterID) != nil || identity.AuthorizeAdministrator(ctx, approval.ApproverID) != nil {
			return updates.ErrApplyUnknown
		}
		p, e := transport.ParseResidentUpdateParameters([]byte(t.Params))
		if e != nil || p.PolicySHA256 != updates.ResidentPolicySHA(c) {
			return updates.ErrApplyUnknown
		}
		if phase == "admit" {
			if t.Status != tasks.StatusRunning {
				return updates.ErrApplyUnknown
			}
			admitted[i.ExecutionID] = i
			return nil
		}
		// The SAME live installer may finish an already admitted handoff after its
		// child shut down. Restarting the supervisor cannot recreate this authority;
		// no task/execution replay is performed and UNKNOWN history stays UNKNOWN.
		if admitted[i.ExecutionID] != i || (t.Status != tasks.StatusRunning && t.Status != tasks.StatusUnknown) {
			return updates.ErrApplyUnknown
		}
		return nil
	}
	backed := map[string]bool{}
	hooks := updates.ResidentSupervisorHooks{Current: current, BeforeStop: func(ctx context.Context, i transport.ResidentUpdateIntent) error {
		if current(ctx, i, "before-stop") != nil {
			return updates.ErrApplyUnknown
		}
		if backed[i.ExecutionID] {
			return nil
		}
		name := filepath.Join(c.Directory, "database-backup-"+i.ExecutionID+".db")
		if db.BackupTo(ctx, name) != nil || schema(ctx) != nil {
			return updates.ErrApplyUnknown
		}
		backed[i.ExecutionID] = true
		return nil
	}, RollbackSafe: schema, Commit: func(ctx context.Context, i transport.ResidentUpdateIntent, r updates.InstallReceipt) (string, error) {
		if current(ctx, i, "commit") != nil || !backed[i.ExecutionID] {
			return "", updates.ErrApplyUnknown
		}
		id := "resident-" + i.ExecutionID
		e := recorder.Append(ctx, audit.Event{SubjectID: c.NodeID, Action: transport.ControllerUpdateAction, Result: "success", TaskID: i.TaskID, RequestID: id, Source: audit.SourceSystem, ReasonCode: "owned_controller_signed_image_fresh_health_compatible_storage", At: time.Now().UTC()})
		if e != nil {
			return "", e
		}
		return id, nil
	}}
	return hooks, db.Close, nil
}
