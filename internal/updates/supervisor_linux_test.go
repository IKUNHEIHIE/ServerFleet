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
	"testing"
	"time"
)

func TestOwnedSupervisorChild(t *testing.T) {
	if os.Getenv("SF_TEST_SUPERVISOR_CHILD") != "1" {
		t.Skip("private subprocess helper only")
	}
	raw, _ := json.Marshal(struct {
		PID            int
		Nonce, Version string
	}{os.Getpid(), os.Getenv("SF_SUPERVISOR_NONCE"), "1.0.0"})
	if os.WriteFile(os.Getenv("SF_TEST_SUPERVISOR_READY"), raw, 0600) != nil {
		os.Exit(2)
	}
	for {
		time.Sleep(time.Second)
	}
}
func TestOwnedSupervisorActualProcessSignedUpdateHealthAndRollback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root owned process profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	previous := ReleaseManifest{Format: 1, Component: "agent", Version: "1.0.0", Sequence: 1, OS: "linux", Architecture: runtime.GOARCH, SHA256: hex.EncodeToString(hash[:]), Bytes: int64(len(body))}
	if err = os.WriteFile(filepath.Join(dir, "active"), body, 0700); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(dir, "ready.json")
	supervisor, err := NewOwnedProcessSupervisor(OwnedSupervisorConfig{Directory: dir, ExecutionID: "owned-execution", Arguments: []string{"-test.run=^TestOwnedSupervisorChild$"}, Environment: []string{"SF_TEST_SUPERVISOR_CHILD=1", "SF_TEST_SUPERVISOR_READY=" + readyPath}, Gate: func(context.Context, ReleaseManifest, string) error { return nil }, Ready: func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
		for {
			raw, e := os.ReadFile(readyPath)
			var ready struct {
				PID            int
				Nonce, Version string
			}
			if e == nil && json.Unmarshal(raw, &ready) == nil && ready.PID == p.PID && ready.Nonce == p.Nonce {
				if ready.Version != m.Version {
					return ErrHealthUnhealthy
				}
				return nil
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
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := supervisor.Close(stop); e != nil {
			t.Error("owned child cleanup failed", e)
		}
	})
	if err = supervisor.Start(ctx, previous); err != nil {
		t.Fatal(err)
	}
	first, err := supervisor.Health(ctx, previous)
	if err != nil || first.ProcessID == "" {
		t.Fatal(first, err)
	}
	// A new controller object after restart cannot adopt a PID or start a
	// duplicate while the durable local reservation remains unresolved.
	restartedConfig := supervisor.cfg
	restartedConfig.ExecutionID = "restarted-controller"
	restarted, err := NewOwnedProcessSupervisor(restartedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Start(ctx, previous) != ErrUpdateBusy {
		t.Fatal("restart duplicated an unresolved owned process")
	}
	if err = restarted.Close(ctx); err != nil {
		t.Fatal(err)
	}
	wrong := previous
	wrong.Version = "9.9.9"
	if supervisor.Stop(ctx, wrong) == nil {
		t.Fatal("stopped an unmatched release")
	}
	staging := t.TempDir()
	os.Chmod(staging, 0700)
	stager, err := NewArtifactStager(staging)
	if err != nil {
		t.Fatal(err)
	}
	defer stager.Close()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	next := previous
	next.Sequence = 2
	next.Version = "2.0.0"
	raw, _ := json.Marshal(next)
	sig := ed25519.Sign(key, append([]byte("serverfleet.release.v1\x00"), raw...))
	policy := ReleasePolicy{Component: "agent", Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"private-test": pub}}
	staged, err := stager.Stage(ctx, raw, sig, "private-test", bytes.NewReader(body), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	installer, err := NewSlotInstaller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer installer.Close()
	receipt, err := installer.Apply(ctx, staged, policy, InstallIntent{TaskID: "owned-task", ExecutionID: "owned-execution", Previous: previous, AllowRollback: true}, InstallHooks{Gate: func(context.Context, InstallIntent, ReleaseManifest, string) error { return nil }, Start: supervisor.Start, Stop: supervisor.Stop, Health: supervisor.Health, RollbackSafe: func(context.Context, ReleaseManifest, ReleaseManifest) error { return nil }, Commit: func(context.Context, InstallReceipt) (string, error) { return "synthetic-atomic-audit", nil }})
	if err != nil || receipt.State != "rolled_back" || receipt.Attempted.Version != "2.0.0" || receipt.Current.Version != "1.0.0" || receipt.Health.ProcessID == first.ProcessID {
		t.Fatal("signed actual process rollback failed", receipt.State, err)
	}
	if _, err = supervisor.Health(ctx, previous); err != nil {
		t.Fatal("rollback process not healthy", err)
	}
}
