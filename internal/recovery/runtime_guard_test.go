package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeGuardRequiresBoundExplicitReleaseAndLiveFence(t *testing.T) {
	ctx := context.Background()
	dir := privateQuarantineDirectory(t)
	db := filepath.Join(dir, "restored.db")
	l, err := BeginQuarantine(ctx, dir, restoreIntent(), QuarantineVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	hold := l.Hold()
	l.Close()
	if _, err = RuntimeGuard(ctx, db, nil); err != ErrQuarantined {
		t.Fatal("omitted latch opened restored DB", err)
	}
	current := true
	// Explicit authority/fence DOUBLE. This test proves local admission only.
	l, err = OpenQuarantine(dir, hold, QuarantineVerifier{Prepare: func(_ context.Context, h QuarantineHold) (ReleaseClearance, error) { return clearanceFixture(h), nil }, Current: func(context.Context, QuarantineHold, ReleaseClearance) error {
		if !current {
			return ErrQuarantined
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err = RuntimeGuard(ctx, db, l); err != ErrQuarantined {
		t.Fatal("implicit release", err)
	}
	if err = l.Release(ctx); err != nil {
		t.Fatal(err)
	}
	gate, err := RuntimeGuard(ctx, db, l)
	if err != nil || gate(ctx) != nil {
		t.Fatal(err)
	}
	if _, err = RuntimeGuard(ctx, filepath.Join(t.TempDir(), "restored.db"), l); err != ErrQuarantined {
		t.Fatal("latch reused for different database", err)
	}
	current = false
	if gate(ctx) != ErrQuarantined {
		t.Fatal("cached release bypassed current fence")
	}
}
func TestNormalRuntimeGuardDetectsLaterHold(t *testing.T) {
	ctx := context.Background()
	dir := privateQuarantineDirectory(t)
	gate, err := RuntimeGuard(ctx, filepath.Join(dir, "controller.db"), nil)
	if err != nil || gate(ctx) != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "hold.json"), []byte("partial hold"), 0600); err != nil {
		t.Fatal(err)
	}
	if gate(ctx) != ErrQuarantined {
		t.Fatal("normal profile ignored restore marker")
	}
}
