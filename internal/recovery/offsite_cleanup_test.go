package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
	"testing"
	"time"
)

type offsiteFixture struct {
	m    *BackupMaintenance
	task map[string]tasks.Task
	root string
}

func newOffsiteFixture(t *testing.T) *offsiteFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDB(t, Migrations(), OffsiteMigrations(), audit.Migrations())
	validator, e := audit.NewValidator(append(BackupAuditActions(), ActionBackupPolicy, ActionBackupProtect, ActionBackupRemoteVerify), audit.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	recorder, e := audit.NewSQLRecorder(db, validator, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	rootDir := t.TempDir()
	stageDir := t.TempDir()
	for _, d := range []string{rootDir, stageDir} {
		if e = os.Chmod(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	root, e := os.OpenRoot(rootDir)
	if e != nil {
		t.Fatal(e)
	}
	stage, e := os.OpenRoot(stageDir)
	if e != nil {
		t.Fatal(e)
	}
	f := &offsiteFixture{task: map[string]tasks.Task{}, root: rootDir}
	manager := &BackupManager{root: root, stage: stage, o: BackupDeploymentOptions{DB: db, Audit: recorder, AuthorityID: "controller", Authorize: func(_ context.Context, s string) error {
		if s != "admin" {
			return ErrBackupUnavailable
		}
		return nil
	}, RuntimeCurrent: func(context.Context) error { return nil }, TaskCurrent: func(_ context.Context, id, plan string) (tasks.Task, error) {
		v, ok := f.task[id]
		if !ok || v.PlanHash != plan {
			return v, ErrBackupUnavailable
		}
		return v, nil
	}}}
	t.Cleanup(func() { manager.Close() })
	f.m, e = NewBackupMaintenance(BackupMaintenanceOptions{Manager: manager, CredentialCurrent: func(context.Context, string, BackupDestination) error { return nil }, Borrow: func(context.Context, tasks.Task, BackupDestination, func(context.Context, []byte) error) error {
		return ErrRemoteRejected
	}, ReadTask: func(_ context.Context, id string) (tasks.Task, error) {
		v, ok := f.task[id]
		if !ok {
			return v, ErrBackupUnavailable
		}
		return v, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.m.Configure(ctx, "admin", "configure", 0, BackupPolicy{IntervalHours: 24, LocalDays: 1, LocalKeep: 1, RemoteDays: 1, RemoteKeep: 1})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *offsiteFixture) add(t *testing.T, id string, days int, status tasks.TaskStatus) {
	t.Helper()
	content := []byte("age-encryption.org/v1\nsynthetic-ciphertext-" + id)
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	now := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if e := os.WriteFile(filepath.Join(f.root, id+".age"), content, 0600); e != nil {
		t.Fatal(e)
	}
	_, e := f.m.manager.o.DB.Exec(context.Background(), `INSERT INTO cx_recovery_backups(id,task_id,kind,status,cipher_name,sha256,bytes,created_at,updated_at,subject_id,recipient_fingerprint,authority_id,root_spki,trust_generation) VALUES(?,?,'full','ready',?,?,?,?,?,'admin','recipient','controller',?,1)`, id, "source-"+id, id+".age", digest, len(content), storage.EncodeTime(now), storage.EncodeTime(now), strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	f.task["source-"+id] = tasks.Task{ID: "source-" + id, Status: status, SubmitterID: "admin", ActionID: ActionCreateBackup}
}
func TestOffsiteRetentionProtectsValidMinimumPinsReadersAndFrozenPlan(t *testing.T) {
	f := newOffsiteFixture(t)
	ctx := context.Background()
	f.add(t, "unknown-newest", 1, tasks.StatusUnknown)
	f.add(t, "keep", 2, tasks.StatusCompleted)
	f.add(t, "pinned", 3, tasks.StatusCompleted)
	f.add(t, "old", 4, tasks.StatusCompleted)
	if _, e := f.m.Protect(ctx, "admin", "pin", "pinned", 0, true, "synthetic protected copy"); e != nil {
		t.Fatal(e)
	}
	if e := f.m.manager.stage.Mkdir("old", 0700); e != nil {
		t.Fatal(e)
	}
	file, _, e := f.m.manager.OpenCipher(ctx, "admin", "download", "old")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := f.m.PreviewCleanup(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range plan.Candidates {
		if c.Scope == "local" {
			t.Fatal("active download or minimum/pin protection ignored", c)
		}
	}
	file.Close()
	plan, e = f.m.PreviewCleanup(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	if len(plan.Candidates) != 2 || plan.Candidates[0].Scope != "staging" || plan.Candidates[1].Scope != "local" || plan.Candidates[1].BackupID != "old" {
		t.Fatal(plan.Candidates)
	}
	params, e := f.m.TaskParameters(ctx, "admin", ActionBackupRetention, plan.Digest)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(params)
	task := tasks.Task{ID: "cleanup", SubmitterID: "admin", RequestID: "cleanup-request", PlanHash: "frozen", ActionID: ActionBackupRetention, TargetType: tasks.TargetTypeController, TargetResourceID: "controller", Params: string(raw), Source: "web", ApprovalRequired: true, Status: tasks.StatusRunning}
	f.task[task.ID] = task
	// A frozen plan must be rechecked after protection changes, not blindly deleted.
	if _, e = f.m.Protect(ctx, "admin", "pin-after-preview", "old", 0, true, "pin after preview"); e != nil {
		t.Fatal(e)
	}
	result, e := f.m.Execute(ctx, task)
	if e != nil || result["controller"].Code != tasks.NodeResultSucceeded {
		t.Fatal(result, e)
	}
	if _, e = os.Stat(filepath.Join(f.root, "old.age")); e != nil {
		t.Fatal("pinned frozen candidate removed", e)
	}
	// A fresh independent approval can delete only the newly eligible exact file.
	if _, e = f.m.Protect(ctx, "admin", "unpin", "old", 1, false, "release after evidence"); e != nil {
		t.Fatal(e)
	}
	plan, e = f.m.PreviewCleanup(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	params, e = f.m.TaskParameters(ctx, "admin", ActionBackupRetention, plan.Digest)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(params)
	task.ID = "cleanup-fresh"
	task.Params = string(raw)
	task.PlanHash = "fresh-plan"
	f.task[task.ID] = task
	result, e = f.m.Execute(ctx, task)
	if e != nil || result["controller"].Code != tasks.NodeResultSucceeded {
		t.Fatal(result, e)
	}
	if _, e = os.Stat(filepath.Join(f.root, "old.age")); !os.IsNotExist(e) {
		t.Fatal("eligible ciphertext retained", e)
	}
	if _, e = f.m.manager.stage.Lstat("old"); !os.IsNotExist(e) {
		t.Fatal("eligible stage retained", e)
	}
	if _, _, e = f.m.manager.OpenCipher(ctx, "admin", "late-download", "old"); e == nil {
		t.Fatal("deleted file admitted")
	}
	if result, e = f.m.Execute(ctx, task); e != nil || result["controller"].Code != tasks.NodeResultUnknown {
		t.Fatal("cleanup replayed", result, e)
	}
	receipts, e := f.m.CleanupReceipts(ctx, "admin")
	if e != nil || len(receipts) != 2 {
		t.Fatal(receipts, e)
	}
}
func TestOffsiteUnknownRemoteCannotProtectLocalOrReplayAfterRestart(t *testing.T) {
	f := newOffsiteFixture(t)
	ctx := context.Background()
	f.add(t, "keep", 2, tasks.StatusCompleted)
	f.add(t, "old", 4, tasks.StatusCompleted)
	p, e := f.m.Policy(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	p.RequireVerifiedRemote = true
	p.Destination = &BackupDestination{Kind: "webdav", Endpoint: "https://backup.example.com", CredentialRef: "synthetic-ref", CredentialRevision: 1}
	p, e = f.m.Configure(ctx, "admin", "remote-policy", p.Revision, p)
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.m.manager.Get(ctx, "admin", "old")
	if e != nil {
		t.Fatal(e)
	}
	f.task["unknown-upload"] = tasks.Task{ID: "unknown-upload", ActionID: ActionBackupUpload, Status: tasks.StatusUnknown}
	r := BackupRemoteRecord{ID: "remote-synthetic", BackupID: b.ID, TaskID: "unknown-upload", VerifiedTaskID: "unknown-upload", State: "verified", Destination: *p.Destination, Object: RemoteObject{Key: "synthetic.age", SHA256: b.SHA256, Bytes: b.Bytes, ETag: `"synthetic"`}}
	raw, _ := json.Marshal(r)
	if _, e = f.m.manager.o.DB.Exec(ctx, `INSERT INTO cx_backup_remote_objects VALUES(?,?,?,?,?)`, r.ID, b.ID, destinationHash(r.Destination), r.State, raw); e != nil {
		t.Fatal(e)
	}
	plan, e := f.m.PreviewCleanup(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	if len(plan.Candidates) != 0 {
		t.Fatal("UNKNOWN remote receipt trusted", plan)
	}
	r.State = "uploading"
	if e = f.m.saveRemote(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e = NewBackupMaintenance(f.m.o); e != nil {
		t.Fatal(e)
	}
	r, e = f.m.remote(ctx, r.ID)
	if e != nil || r.State != "unknown" {
		t.Fatal(r, e)
	}
	if e = f.m.manager.o.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error { return InvalidateRestoredBackupPolicyTx(c, tx) }); e != nil {
		t.Fatal(e)
	}
	restored, e := f.m.Policy(ctx, "admin")
	if e != nil || restored.AutoUpload || restored.ScheduleCleanup || restored.ScheduledBackups || restored.Revision <= p.Revision {
		t.Fatal(restored, e)
	}
}

func TestOffsiteMinimumCountDoesNotTrustMissingOrCorruptLocalMetadata(t *testing.T) {
	f := newOffsiteFixture(t)
	ctx := context.Background()
	f.add(t, "missing", 2, tasks.StatusCompleted)
	f.add(t, "corrupt", 3, tasks.StatusCompleted)
	f.add(t, "last-real-copy", 4, tasks.StatusCompleted)
	if e := os.Remove(filepath.Join(f.root, "missing.age")); e != nil {
		t.Fatal(e)
	}
	content, e := os.ReadFile(filepath.Join(f.root, "corrupt.age"))
	if e != nil {
		t.Fatal(e)
	}
	content[len(content)-1] ^= 1
	if e = os.WriteFile(filepath.Join(f.root, "corrupt.age"), content, 0600); e != nil {
		t.Fatal(e)
	}
	plan, e := f.m.PreviewCleanup(ctx, "admin")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range plan.Candidates {
		if c.Scope == "local" {
			t.Fatal("metadata counted as a surviving real copy", c)
		}
	}
}
