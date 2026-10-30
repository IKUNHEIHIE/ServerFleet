package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func privateQuarantineDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestQuarantineRejectsNonPrivateLinuxDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux directory permission check")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if latch, err := BeginQuarantine(context.Background(), dir, restoreIntent(), QuarantineVerifier{}); err != ErrRecoveryRejected || latch != nil {
		t.Fatal("public restore-control directory admitted", err)
	}
}

func restoreIntent() RestoreIntent {
	return RestoreIntent{strings.Repeat("a", 64), "new-controller", strings.Repeat("b", 64), 4, 7}
}

// This verifier is an explicit double, not real offline authority/fencing.
func clearanceFixture(h QuarantineHold) ReleaseClearance {
	return ReleaseClearance{h.ID, h.SnapshotSHA256, h.ControllerID, h.RootSPKISHA256, h.MinTrustGeneration, h.MinRevocationSequence, strings.Repeat("c", 64), strings.Repeat("d", 64), "audit-reconcile"}
}
func TestQuarantineDurableAndLiveVerifierRequired(t *testing.T) {
	ctx := context.Background()
	dir := privateQuarantineDirectory(t)
	l, err := BeginQuarantine(ctx, dir, restoreIntent(), QuarantineVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	hold := l.Hold()
	if err = l.Check(ctx); err != ErrQuarantined {
		t.Fatal("new restore was runnable", err)
	}
	if err = l.Release(ctx); err != ErrQuarantined {
		t.Fatal("absent authority cleared quarantine", err)
	}
	_ = l.Close()
	l, err = BeginQuarantine(ctx, dir, restoreIntent(), QuarantineVerifier{})
	if err != nil || l.Hold() != hold {
		t.Fatal("same intent lost permanent hold", err)
	}
	_ = l.Close()
	current := true
	preparations := 0
	verifier := QuarantineVerifier{
		Prepare: func(_ context.Context, h QuarantineHold) (ReleaseClearance, error) {
			preparations++
			return clearanceFixture(h), nil
		},
		Current: func(context.Context, QuarantineHold, ReleaseClearance) error {
			if !current {
				return errors.New("authority revoked or reconciliation no longer current")
			}
			return nil
		},
	}
	l, err = OpenQuarantine(dir, hold, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.Release(ctx); err != nil || preparations != 1 {
		t.Fatal("repeated reconciliation", preparations, err)
	}
	if err = l.Check(ctx); err != nil {
		t.Fatal(err)
	}
	current = false
	if err = l.Check(ctx); err != ErrQuarantined {
		t.Fatal("cached clearance survived current authority denial", err)
	}
	_ = l.Close()
	l, err = OpenQuarantine(dir, hold, QuarantineVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err = l.Check(ctx); err != ErrQuarantined {
		t.Fatal("restart trusted receipt without authority", err)
	}
	if _, err = BeginQuarantine(ctx, dir, restoreIntent(), verifier); err != ErrRecoveryRejected {
		t.Fatal("reused released restore episode", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "hold.json")); err != nil {
		t.Fatal("release deleted the durable hold", err)
	}
}

func TestQuarantineRejectsMisbindingStaleAndAmbiguousRecords(t *testing.T) {
	ctx := context.Background()
	for _, mutate := range []func(*ReleaseClearance){
		func(c *ReleaseClearance) { c.ControllerID = "old-controller" },
		func(c *ReleaseClearance) { c.HoldID = strings.Repeat("e", 64) },
		func(c *ReleaseClearance) { c.SnapshotSHA256 = strings.Repeat("f", 64) },
		func(c *ReleaseClearance) { c.RootSPKISHA256 = strings.Repeat("a", 64) },
		func(c *ReleaseClearance) { c.TrustGeneration-- },
		func(c *ReleaseClearance) { c.RevocationSequence-- },
		func(c *ReleaseClearance) { c.FencingEvidenceSHA256 = "claimed-fenced" },
		func(c *ReleaseClearance) { c.ReconciledStateSHA256 = "" },
		func(c *ReleaseClearance) { c.AuditRecordID = "" },
	} {
		l, err := BeginQuarantine(ctx, privateQuarantineDirectory(t), restoreIntent(), QuarantineVerifier{
			Prepare: func(_ context.Context, h QuarantineHold) (ReleaseClearance, error) {
				c := clearanceFixture(h)
				mutate(&c)
				return c, nil
			},
			Current: func(context.Context, QuarantineHold, ReleaseClearance) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if err = l.Release(ctx); err != ErrQuarantined {
			t.Fatal("invalid receipt cleared hold", err)
		}
		_ = l.Close()
	}
	for _, corrupt := range []string{"", "{}", "{", strings.Repeat("a", 4097)} {
		dir := privateQuarantineDirectory(t)
		l, err := BeginQuarantine(ctx, dir, restoreIntent(), QuarantineVerifier{})
		if err != nil {
			t.Fatal(err)
		}
		h := l.Hold()
		if err := os.WriteFile(filepath.Join(dir, "hold.json"), []byte(corrupt), 0600); err != nil {
			t.Fatal(err)
		}
		if err := l.Check(ctx); err != ErrQuarantined {
			t.Fatal("damaged hold accepted", err)
		}
		_ = l.Close()
		if _, err = OpenQuarantine(dir, h, QuarantineVerifier{}); err != ErrRecoveryRejected {
			t.Fatal("damaged hold reopened", err)
		}
		if _, err = BeginQuarantine(ctx, dir, restoreIntent(), QuarantineVerifier{}); err != ErrRecoveryRejected {
			t.Fatal("damaged hold silently repaired", err)
		}
	}
}

func TestQuarantineCancellationPanicAndPartialReleaseDeny(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"canceled", "panic", "partial", "duplicate-json"} {
		dir := privateQuarantineDirectory(t)
		called := false
		verifier := QuarantineVerifier{
			Prepare: func(_ context.Context, h QuarantineHold) (ReleaseClearance, error) {
				called = true
				return clearanceFixture(h), nil
			},
			Current: func(context.Context, QuarantineHold, ReleaseClearance) error { panic("must not expose verifier panic") },
		}
		l, err := BeginQuarantine(ctx, dir, restoreIntent(), verifier)
		if err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithCancel(ctx)
		if mode == "canceled" {
			cancel()
		}
		if mode == "partial" || mode == "duplicate-json" {
			content := "{"
			if mode == "duplicate-json" {
				content = `{"HoldID":"first","HoldID":"second"}`
			}
			if err = os.WriteFile(filepath.Join(dir, "release.json"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err = l.Release(bounded); err != ErrQuarantined {
			t.Fatal(mode, err)
		}
		if called != (mode == "panic") {
			t.Fatal("ambiguous state invoked reconciliation", mode, called)
		}
		if err = l.Check(ctx); err != ErrQuarantined {
			t.Fatal(mode, err)
		}
		cancel()
		_ = l.Close()
	}
}
