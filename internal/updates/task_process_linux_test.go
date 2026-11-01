package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallTaskActualOwnedProcessVersionStorageAndAtomicRollback(t *testing.T) {
	for _, incompatible := range []bool{false, true} {
		name := "compatible_rollback"
		if incompatible {
			name = "incompatible_storage_unknown"
		}
		t.Run(name, func(t *testing.T) { runInstallTaskActualOwnedProcess(t, incompatible) })
	}
}
func runInstallTaskActualOwnedProcess(t *testing.T, incompatible bool) {
	if os.Geteuid() == 0 {
		t.Skip("non-root owned process profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	exe, _ := os.Executable()
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(binary)
	previous := ReleaseManifest{1, "agent", "1.0.0", 1, "linux", runtime.GOARCH, hex.EncodeToString(hash[:]), int64(len(binary))}
	next := previous
	next.Sequence = 2
	next.Version = "2.0.0"
	if err = os.WriteFile(filepath.Join(dir, "active"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	stageDir := t.TempDir()
	os.Chmod(stageDir, 0700)
	stager, err := NewArtifactStager(stageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer stager.Close()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(next)
	policy := ReleasePolicy{Component: "agent", Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"local-review": pub}}
	staged, err := stager.Stage(ctx, raw, ed25519.Sign(key, append([]byte("serverfleet.release.v1\x00"), raw...)), "local-review", bytes.NewReader(binary), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	installer, err := NewSlotInstaller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer installer.Close()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations(), []storage.Migration{{Version: "test/9999_process_journal", Name: "unregistered candidate fixture", Statements: InstallJournalSchemaStatements()}})
	if _, err = db.Exec(ctx, `PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	oldRaw, _ := json.Marshal(previous)
	if _, err = db.Exec(ctx, `INSERT INTO cx_update_installed VALUES(?,?,?)`, previous.Component, oldRaw, 1); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "before-update.db")
	if err = db.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	backupRaw, _ := os.ReadFile(backup)
	backupHash := sha256.Sum256(backupRaw)
	validator, _ := audit.NewValidator(append(tasks.ServiceActions(), ActionInstall), audit.DefaultLimits())
	sink, _ := audit.NewSQLRecorder(db, validator, time.Now)
	repo, _ := tasks.NewRepo(db)
	var service *tasks.Service
	var task tasks.Task
	var supervisor *OwnedProcessSupervisor
	check := func(ctx context.Context) error { _, e := service.CheckExecution(ctx, task.ID, task.PlanHash); return e }
	journal, _ := NewSQLInstallJournal(db, sink, func(ctx context.Context, id, plan, subject string) error {
		current, e := service.CheckExecution(ctx, id, plan)
		if e != nil || current.SubmitterID != subject {
			return ErrArtifactRejected
		}
		return nil
	})
	storageCompatible := func(ctx context.Context) error {
		var version int
		if db.QueryRow(ctx, `PRAGMA user_version`).Scan(&version) != nil || version != 1 {
			return ErrApplyUnknown
		}
		return nil
	}
	worker, err := NewTaskInstallWorker(TaskInstallWorkerOptions{Installer: installer, Current: func(ctx context.Context, id, plan string) (tasks.Task, error) {
		return service.CheckExecution(ctx, id, plan)
	}, Administrator: func(context.Context, string) error { return nil }, BackupCurrent: func(ctx context.Context, p FrozenInstallPlan) error {
		data, e := os.ReadFile(backup)
		sum := sha256.Sum256(data)
		if e != nil || hex.EncodeToString(sum[:]) != p.BackupSHA256 || storage.VerifyFile(ctx, backup, true) != nil {
			return ErrArtifactRejected
		}
		return storageCompatible(ctx)
	}, Resolve: func(_ context.Context, p FrozenInstallPlan, i InstallIntent) (PreparedInstall, error) {
		return PreparedInstall{PlanID: p.ID, SubjectID: p.SubjectID, Staged: staged, Policy: policy, Intent: i, Hooks: InstallHooks{Gate: func(ctx context.Context, _ InstallIntent, _ ReleaseManifest, _ string) error { return check(ctx) }, Stop: supervisor.Stop, Start: supervisor.Start, Health: supervisor.Health, RollbackSafe: func(ctx context.Context, _, _ ReleaseManifest) error { return storageCompatible(ctx) }, Commit: func(ctx context.Context, r InstallReceipt) (string, error) {
			return journal.Commit(ctx, p.SubjectID, task.PlanHash, r)
		}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Role/online/bootstrap/cleanup authorities are explicit doubles. Execution
	// approval/current task, actual process version/PID/image, backup, SQLite
	// storage version, filesystem rollback and same-transaction audit are real.
	service, err = tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: sink, Authorizer: installAuthorizer{}, Nodes: installNodes{}, Workers: map[string]tasks.Worker{ActionInstall: worker}})
	if err != nil {
		t.Fatal(err)
	}
	plan := FrozenInstallPlan{ID: "actual-owned-update", SubjectID: "admin", NodeID: "isolated-node", BackupSHA256: hex.EncodeToString(backupHash[:]), PolicySHA256: ReleasePolicyDigest(policy), Previous: previous, Attempted: next, AllowRollback: true}
	request, err := plan.TaskRequest("actual-update-request")
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	executionHash := sha256.Sum256([]byte(task.ID + "\x00" + task.PlanHash))
	execution := "update-" + hex.EncodeToString(executionHash[:20])
	readyPath := filepath.Join(dir, "ready.json")
	var bootstrap, cleanup atomic.Bool
	bootstrap.Store(true)
	supervisor, err = NewOwnedProcessSupervisor(OwnedSupervisorConfig{Directory: dir, ExecutionID: execution, Arguments: []string{"-test.run=^TestOwnedSupervisorChild$"}, Environment: []string{"SF_TEST_SUPERVISOR_CHILD=1", "SF_TEST_SUPERVISOR_READY=" + readyPath}, Gate: func(ctx context.Context, m ReleaseManifest, phase string) error {
		if bootstrap.Load() && m == previous || cleanup.Load() && phase == "supervisor-stop" {
			return nil
		}
		return check(ctx)
	}, Ready: func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
		for {
			raw, e := os.ReadFile(readyPath)
			var observed struct {
				PID            int
				Nonce, Version string
			}
			if e == nil && json.Unmarshal(raw, &observed) == nil && observed.PID == p.PID && observed.Nonce == p.Nonce {
				if observed.Version != m.Version {
					if incompatible {
						if _, err := db.Exec(ctx, `PRAGMA user_version=2`); err != nil {
							return err
						}
					}
					return ErrHealthUnhealthy
				}
				return storageCompatible(ctx)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup.Store(true)
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := supervisor.Close(stop); e != nil {
			t.Error(e)
		}
	})
	if err = supervisor.Start(ctx, previous); err != nil {
		t.Fatal(err)
	}
	first, err := supervisor.Health(ctx, previous)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.Store(false)
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusAwaitingApproval {
		t.Fatal("update did not await approval")
	}
	task, err = service.Decide(ctx, task.ID, task.Revision, tasks.Decision{ApproverID: "other-admin", RequestID: "actual-update-approval", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		current, e := service.Get(ctx, task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.Status.Terminal() {
			want := tasks.StatusFailed
			if incompatible {
				want = tasks.StatusUnknown
			}
			if current.Status != want {
				t.Fatal("rollback task result", current.Status)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	var committed []byte
	var floor int
	if err = db.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed`).Scan(&committed, &floor); err != nil {
		t.Fatal(err)
	}
	var actual ReleaseManifest
	json.Unmarshal(committed, &actual)
	receipt, err := installer.Receipt(execution)
	if incompatible {
		supervisor.mu.Lock()
		stillCandidate := supervisor.process != nil && supervisor.process.manifest == next
		supervisor.mu.Unlock()
		active, _ := os.ReadFile(filepath.Join(dir, "active"))
		if err == nil || floor != 1 || actual != previous || !bytes.Equal(active, binary) || !stillCandidate {
			t.Fatal("incompatible storage fabricated rollback", floor, actual, err)
		}
		var receipts int
		db.QueryRow(ctx, `SELECT count(*) FROM cx_update_receipts`).Scan(&receipts)
		if receipts != 0 {
			t.Fatal("unknown committed receipt")
		}
		if _, e := os.Stat(filepath.Join(dir, "update-lock.json")); e != nil {
			t.Fatal("unknown lost permanent reservation", e)
		}
		if e := service.RunOnce(ctx); e != nil {
			t.Fatal(e)
		}
		t.Log("actual storage user_version changed to 2: old release never restarted, no journal/audit commit, UNKNOWN remains permanently reserved")
		return
	}
	if err != nil || receipt.State != "rolled_back" || receipt.Current != previous || actual != previous || floor != 2 || receipt.Health.ProcessID == first.ProcessID {
		t.Fatal("actual rollback provenance", receipt, actual, floor, err)
	}
	var audits int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action=? AND task_id=?`, ActionInstall, task.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("missing atomic update audit", audits, err)
	}
	t.Log("approved actual owned process update: real candidate reports wrong version, actual storage permits exact backup rollback, new PID healthy, installed provenance/floor/audit committed together")
}
