package platform_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestWave6IsolatedSQLiteRestoreKeepsQuarantineAndRevocations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root restore executor")
	}
	ctx := context.Background()
	db := testkit.NewDB(t, []storage.Migration{})
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := db.BackupTo(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(bytes)
	intent := recovery.RestoreIntent{SnapshotSHA256: hex.EncodeToString(hash[:]), ControllerID: "new-controller", RootSPKISHA256: strings.Repeat("b", 64), MinTrustGeneration: 3, MinRevocationSequence: 7}
	job := recovery.RestoreJob{ID: "job-one", TaskID: "task-one", BackupID: "backup-one", Intent: intent}
	source := func(context.Context, recovery.RestoreJob) (io.ReadCloser, recovery.Snapshot, error) {
		f, e := os.Open(snapshot)
		return f, recovery.Snapshot{RootSPKISHA256: intent.RootSPKISHA256, TrustGeneration: 1, RevocationSequence: 2, RevokedSerials: []string{"old-revoked"}, Tasks: []recovery.TaskRecord{{ID: "running-task", State: "running", Expires: time.Now().Add(time.Minute)}}}, e
	}
	root := t.TempDir()
	os.Chmod(root, 0700)
	commits := 0
	hooks := recovery.RestoreHooks{Admit: func(context.Context, recovery.RestoreJob, string) error { return nil }, Authority: func(context.Context, recovery.RestoreJob) (recovery.Authority, error) {
		return recovery.Authority{RootSPKISHA256: intent.RootSPKISHA256, TrustGeneration: 3, RevocationSequence: 7, RevokedSerials: []string{"new-revoked"}}, nil
	}, Commit: func(ctx context.Context, path string, plan recovery.Reconciliation) error {
		commits++
		if !plan.InvalidateAllSessions || !plan.InvalidateAllApprovals || !plan.Quarantine || plan.Tasks[0].State != "unknown" || len(plan.RevokedSerials) != 2 {
			t.Error("restored stale execution or revocations")
		}
		return storage.VerifyFile(ctx, path, true)
	}, Verifier: recovery.QuarantineVerifier{Prepare: func(context.Context, recovery.QuarantineHold) (recovery.ReleaseClearance, error) {
		return recovery.ReleaseClearance{}, recovery.ErrQuarantined
	}, Current: func(context.Context, recovery.QuarantineHold, recovery.ReleaseClearance) error {
		return recovery.ErrQuarantined
	}}}
	receipt, err := recovery.RestoreIsolated(ctx, root, job, source, hooks)
	if err != nil || receipt.State != "restored_quarantined" || commits != 1 {
		t.Fatal(receipt, err, commits)
	}
	latch, err := recovery.OpenQuarantine(filepath.Join(root, "restore-"+job.ID), receipt.Hold, hooks.Verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer latch.Close()
	if latch.Check(ctx) != recovery.ErrQuarantined {
		t.Fatal("backup enabled restored Controller")
	}
	if _, err = recovery.RestoreIsolated(ctx, root, job, source, hooks); err == nil || commits != 1 {
		t.Fatal("restore replay")
	}
	job.ID = "job-corrupt"
	job.Intent.SnapshotSHA256 = strings.Repeat("c", 64)
	if _, err = recovery.RestoreIsolated(ctx, root, job, source, hooks); err == nil || commits != 1 {
		t.Fatal("corrupt backup reached commit")
	}
	job.ID = "job-old-epoch"
	job.Intent.SnapshotSHA256 = hex.EncodeToString(hash[:])
	job.Intent.MinTrustGeneration = 1
	hooks.Authority = func(context.Context, recovery.RestoreJob) (recovery.Authority, error) {
		return recovery.Authority{RootSPKISHA256: intent.RootSPKISHA256, TrustGeneration: 1, RevocationSequence: 7, RevokedSerials: []string{"new-revoked"}}, nil
	}
	if _, err = recovery.RestoreIsolated(ctx, root, job, source, hooks); err == nil || commits != 1 {
		t.Fatal("old snapshot trust epoch reached restore commit")
	}
}
