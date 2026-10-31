package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type installFixture struct {
	dir       string
	s         *SlotInstaller
	staged    *StagedArtifact
	policy    ReleasePolicy
	intent    InstallIntent
	body, old []byte
	calls     []string
	hooks     InstallHooks
	started   time.Time
}

func installTestFixture(t *testing.T) *installFixture {
	t.Helper()
	stager, raw, sig, body, policy := stagingFixture(t)
	staged, err := stager.Stage(context.Background(), raw, sig, "test-key", bytes.NewReader(body), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staged.Close() })
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte("previous signed-release fixture; not an executable")
	hash := sha256.Sum256(old)
	previous := ReleaseManifest{1, "agent", "0.8.0", 1, "linux", "amd64", hex.EncodeToString(hash[:]), int64(len(old))}
	if err = os.WriteFile(filepath.Join(dir, "active"), old, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := NewSlotInstaller(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f := &installFixture{dir: dir, s: s, staged: staged, policy: policy, intent: InstallIntent{TaskID: "task-1", ExecutionID: "exec-1", Previous: previous, AllowRollback: true}, body: body, old: old}
	t.Cleanup(func() {
		if t.Failed() {
			entries, _ := filepath.Glob(filepath.Join(dir, "update-exec-1", "*.json"))
			for i := range entries {
				entries[i] = filepath.Base(entries[i])
			}
			t.Logf("durable markers=%v; service double calls=%v", entries, f.calls)
		}
	})
	// Service/authorization/audit hooks below are explicit doubles. Only the
	// signed staging, bounded copies, switch/backup, journal and restart reads
	// are real filesystem behavior. No service manager or migration is claimed.
	f.hooks = InstallHooks{
		Gate: func(_ context.Context, _ InstallIntent, _ ReleaseManifest, _ string) error { return nil },
		Stop: func(_ context.Context, m ReleaseManifest) error {
			f.calls = append(f.calls, "stop-"+m.Version)
			return nil
		},
		Start: func(_ context.Context, m ReleaseManifest) error {
			f.calls = append(f.calls, "start-"+m.Version)
			f.started = time.Now()
			return nil
		},
		Health: func(_ context.Context, m ReleaseManifest) (HealthEvidence, error) {
			f.calls = append(f.calls, "health-"+m.Version)
			return HealthEvidence{f.intent.ExecutionID, m.Component, m.Version, m.SHA256, "process-123", f.started, time.Now()}, nil
		},
		RollbackSafe: func(context.Context, ReleaseManifest, ReleaseManifest) error { return nil },
		Commit: func(_ context.Context, r InstallReceipt) (string, error) {
			f.calls = append(f.calls, "commit-"+r.State)
			return "audit-1", nil
		},
	}
	return f
}

func (f *installFixture) apply() (InstallReceipt, error) {
	return f.s.Apply(context.Background(), f.staged, f.policy, f.intent, f.hooks)
}
func (f *installFixture) active(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "active"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sameInstallReceipt(a, b InstallReceipt) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestInstallRealFilesReceiptReopenAndPermanentExecutionFence(t *testing.T) {
	f := installTestFixture(t)
	r, err := f.apply()
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "installed" || r.Current != f.staged.Manifest() || r.AuditRecordID != "audit-1" || !bytes.Equal(f.active(t), f.body) {
		t.Fatal("wrong committed outcome", r)
	}
	if !reflect.DeepEqual(f.calls, []string{"stop-0.8.0", "start-0.9.0", "health-0.9.0", "commit-installed"}) {
		t.Fatal(f.calls)
	}
	if _, err = os.Stat(filepath.Join(f.dir, "update-lock.json")); !os.IsNotExist(err) {
		t.Fatal("terminal lock not released", err)
	}
	backup, err := os.ReadFile(filepath.Join(f.dir, "update-exec-1", "previous"))
	if err != nil || !bytes.Equal(backup, f.old) {
		t.Fatal("old file lost", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSlotInstaller(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Receipt("exec-1")
	if err != nil || !sameInstallReceipt(got, r) {
		t.Fatal("commit receipt not persistent", err, got)
	}
	// Reusing the identity must never start a service again, even with another
	// object; a later valid update also cannot replace the old journal.
	before := len(f.calls)
	if _, err = reopened.Apply(context.Background(), f.staged, f.policy, f.intent, f.hooks); err == nil || len(f.calls) != before {
		t.Fatal("update replayed", err)
	}
}

func TestInstallKnownUnhealthyRollsBackOnlyExactBackup(t *testing.T) {
	f := installTestFixture(t)
	health := f.hooks.Health
	f.hooks.Health = func(ctx context.Context, m ReleaseManifest) (HealthEvidence, error) {
		if m.Sequence == 2 {
			f.calls = append(f.calls, "unhealthy-new")
			return HealthEvidence{}, ErrHealthUnhealthy
		}
		return health(ctx, m)
	}
	r, err := f.apply()
	if err != nil || r.State != "rolled_back" || r.Current != f.intent.Previous || r.Attempted.Sequence != 2 || !bytes.Equal(f.active(t), f.old) {
		t.Fatal("rollback falsely reported", r, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"stop-0.8.0", "start-0.9.0", "unhealthy-new", "stop-0.9.0", "start-0.8.0", "health-0.8.0", "commit-rolled_back"}) {
		t.Fatal(f.calls)
	}
	if got, err := f.s.Receipt("exec-1"); err != nil || !sameInstallReceipt(got, r) {
		t.Fatal(err, got)
	}
}

func TestInstallUnknownStagesPersistAndDoNotRetryAfterReopen(t *testing.T) {
	for _, phase := range []string{"stage", "stopping", "stopped", "switching", "switched", "starting", "health", "committing"} {
		t.Run(phase, func(t *testing.T) {
			f := installTestFixture(t)
			hit := false
			f.hooks.Gate = func(_ context.Context, _ InstallIntent, _ ReleaseManifest, p string) error {
				if p == phase {
					hit = true
					return errors.New("revoked")
				}
				return nil
			}
			if _, err := f.apply(); err != ErrApplyUnknown || !hit {
				t.Fatal("interrupted stage succeeded", err)
			}
			before := len(f.calls)
			if err := f.s.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := NewSlotInstaller(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err = r.Receipt("exec-1"); err != ErrApplyUnknown {
				t.Fatal("missing receipt projected as success", err)
			}
			if _, err = r.Apply(context.Background(), f.staged, f.policy, f.intent, f.hooks); err == nil || len(f.calls) != before {
				t.Fatal("unknown update retried", err)
			}
		})
	}
}

func TestInstallMissingDependenciesAndTamperFailBeforeEffects(t *testing.T) {
	for _, name := range []string{"gate", "stop", "start", "health", "commit", "rollback-safe", "old-hash", "bad-id", "floor", "relative"} {
		t.Run(name, func(t *testing.T) {
			f := installTestFixture(t)
			switch name {
			case "gate":
				f.hooks.Gate = nil
			case "stop":
				f.hooks.Stop = nil
			case "start":
				f.hooks.Start = nil
			case "health":
				f.hooks.Health = nil
			case "commit":
				f.hooks.Commit = nil
			case "rollback-safe":
				f.hooks.RollbackSafe = nil
			case "old-hash":
				f.intent.Previous.SHA256 = f.staged.Manifest().SHA256
			case "bad-id":
				f.intent.ExecutionID = "../other"
			case "floor":
				f.policy.MinimumSequence = 2
			case "relative":
				if _, err := NewSlotInstaller("relative"); err != ErrArtifactRejected {
					t.Fatal(err)
				}
				return
			}
			if _, err := f.apply(); err != ErrArtifactRejected || len(f.calls) != 0 || !bytes.Equal(f.active(t), f.old) {
				t.Fatal("preflight changed service", err, f.calls)
			}
			if _, err := os.Stat(filepath.Join(f.dir, "update-lock.json")); !os.IsNotExist(err) {
				t.Fatal("rejected before reservation but left lock", err)
			}
		})
	}
}

func TestInstallAmbiguousEffectsHealthAndCommitNeverBecomeSuccess(t *testing.T) {
	for _, name := range []string{"stop-error", "start-error", "health-timeout", "health-identity", "stale-health", "no-rollback", "rollback-stop", "rollback-health", "audit-error", "audit-panic", "empty-audit"} {
		t.Run(name, func(t *testing.T) {
			f := installTestFixture(t)
			originalHealth := f.hooks.Health
			switch name {
			case "stop-error":
				f.hooks.Stop = func(context.Context, ReleaseManifest) error { return errors.New("ambiguous stop") }
			case "start-error":
				f.hooks.Start = func(context.Context, ReleaseManifest) error { return errors.New("ambiguous start") }
			case "health-timeout":
				f.hooks.Health = func(context.Context, ReleaseManifest) (HealthEvidence, error) {
					return HealthEvidence{}, context.DeadlineExceeded
				}
			case "health-identity":
				f.hooks.Health = func(ctx context.Context, m ReleaseManifest) (HealthEvidence, error) {
					h, e := originalHealth(ctx, m)
					h.SHA256 = f.intent.Previous.SHA256
					return h, e
				}
			case "stale-health":
				f.hooks.Health = func(ctx context.Context, m ReleaseManifest) (HealthEvidence, error) {
					h, e := originalHealth(ctx, m)
					h.StartedAt = time.Now().Add(-time.Hour)
					return h, e
				}
			case "no-rollback":
				f.intent.AllowRollback = false
				fallthrough
			case "rollback-stop", "rollback-health":
				f.hooks.Health = func(context.Context, ReleaseManifest) (HealthEvidence, error) {
					return HealthEvidence{}, ErrHealthUnhealthy
				}
				if name == "rollback-stop" {
					f.hooks.Stop = func(_ context.Context, m ReleaseManifest) error {
						if m.Sequence == 2 {
							return errors.New("stop unknown")
						}
						return nil
					}
				}
			case "audit-error":
				f.hooks.Commit = func(context.Context, InstallReceipt) (string, error) { return "", errors.New("commit outcome unknown") }
			case "audit-panic":
				f.hooks.Commit = func(context.Context, InstallReceipt) (string, error) { panic("synthetic failure") }
			case "empty-audit":
				f.hooks.Commit = func(context.Context, InstallReceipt) (string, error) { return "", nil }
			}
			if _, err := f.apply(); err != ErrApplyUnknown {
				t.Fatal("ambiguous operation succeeded", err)
			}
			if _, err := f.s.Receipt("exec-1"); err != ErrApplyUnknown {
				t.Fatal("false receipt", err)
			}
			if _, err := os.Stat(filepath.Join(f.dir, "update-lock.json")); err != nil {
				t.Fatal("unknown fence removed", err)
			}
		})
	}
}

func TestInstallExclusiveReservationAcrossOpenHandles(t *testing.T) {
	f := installTestFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.hooks.Gate = func(_ context.Context, _ InstallIntent, _ ReleaseManifest, phase string) error {
		if phase == "stage" {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { _, err := f.apply(); done <- err }()
	<-entered
	other, err := NewSlotInstaller(f.dir)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer other.Close()
	second := installTestFixture(t)
	intent := f.intent
	intent.ExecutionID = "exec-2"
	if _, err = other.Apply(context.Background(), second.staged, second.policy, intent, f.hooks); err != ErrUpdateBusy {
		close(release)
		t.Fatal("second handle acquired reservation", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInstallCorruptReceiptAndRollbackBackupStayUnknown(t *testing.T) {
	t.Run("receipt", func(t *testing.T) {
		f := installTestFixture(t)
		if _, err := f.apply(); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(f.dir, "update-exec-1", "receipt.json")
		if err := os.WriteFile(file, []byte(`{"State":"installed"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Receipt("exec-1"); err != ErrApplyUnknown {
			t.Fatal("corrupt receipt accepted", err)
		}
	})
	t.Run("backup", func(t *testing.T) {
		f := installTestFixture(t)
		f.hooks.Health = func(context.Context, ReleaseManifest) (HealthEvidence, error) {
			file := filepath.Join(f.dir, "update-exec-1", "previous")
			if err := os.Chmod(file, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("damaged backup"), 0600); err != nil {
				t.Fatal(err)
			}
			return HealthEvidence{}, ErrHealthUnhealthy
		}
		if _, err := f.apply(); err != ErrApplyUnknown || !bytes.Equal(f.active(t), f.body) {
			t.Fatal("damaged backup replaced candidate", err)
		}
	})
}

func TestInstallRollbackCompatibilityDeniedBeforeOldBinaryStarts(t *testing.T) {
	f := installTestFixture(t)
	f.hooks.Health = func(context.Context, ReleaseManifest) (HealthEvidence, error) {
		return HealthEvidence{}, ErrHealthUnhealthy
	}
	f.hooks.RollbackSafe = func(context.Context, ReleaseManifest, ReleaseManifest) error {
		return errors.New("schema no longer compatible")
	}
	if _, err := f.apply(); err != ErrApplyUnknown || !bytes.Equal(f.active(t), f.body) || !reflect.DeepEqual(f.calls, []string{"stop-0.8.0", "start-0.9.0"}) {
		t.Fatal("incompatible rollback ran old service", err, f.calls)
	}
}
