package recovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/storage"
)

// Owner metadata is tested separately from the real process/TLS proof. These
// tests do not claim a fenced process or a successful disaster exercise.
func TestResidentAuthorityCannotOmitBindingOrRunAfterExternalFence(t *testing.T) {
	ctx := context.Background()
	dir := privateQuarantineDirectory(t)
	db, e := storage.Open(ctx, storage.Config{Path: filepath.Join(dir, "control.db")})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	migrator, e := storage.NewMigrator(ResidentAuthorityMigrations())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrator.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	binding := ResidentAuthority{AuthorityID: "old-controller", PolicySHA256: strings.Repeat("a", 64), Directory: dir, Mode: "owned"}
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return BindResidentAuthorityTx(ctx, tx, binding, false)
	}); e != nil {
		t.Fatal(e)
	}
	base := func(context.Context) error { return nil }
	for _, gate := range []func(context.Context) error{ResidentAuthorityGate(db, nil, "", base), ResidentAuthorityGate(db, &binding, strings.Repeat("b", 64), base)} {
		if gate(ctx) != ErrQuarantined {
			t.Fatal("unbound runtime admitted")
		}
	}
	gate := ResidentAuthorityGate(db, &binding, binding.PolicySHA256, base)
	if e = gate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "fenced-intent.json"), []byte("incomplete"), 0600); e != nil {
		t.Fatal(e)
	}
	if gate(ctx) != ErrQuarantined {
		t.Fatal("fence intent did not immediately stop authority")
	}
	changed := binding
	changed.Directory = filepath.Join(dir, "different")
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return BindResidentAuthorityTx(ctx, tx, changed, false)
	}); e != ErrRecoveryRejected {
		t.Fatal("binding silently redirected", e)
	}
}
func TestRestoredResidentRequiresExplicitActivationAndPreservesReceipt(t *testing.T) {
	ctx := context.Background()
	dir := privateQuarantineDirectory(t)
	db, e := storage.Open(ctx, storage.Config{Path: filepath.Join(dir, "control.db")})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m, e := storage.NewMigrator(ResidentAuthorityMigrations())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return SuspendRestoredResidentAuthorityTx(ctx, tx, "old-controller", "new-controller")
	}); e != nil {
		t.Fatal(e)
	}
	b := ResidentAuthority{AuthorityID: "new-controller", PolicySHA256: strings.Repeat("a", 64), Directory: dir, Mode: "owned"}
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return BindResidentAuthorityTx(ctx, tx, b, false) }); e != ErrRecoveryRejected {
		t.Fatal("ordinary bootstrap cleared restored isolation", e)
	}
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := BindResidentAuthorityTx(ctx, tx, b, true); e != nil {
			return e
		}
		return RecordResidentActivationTx(ctx, tx, `{"verified-by-test-owner":"not-physical-evidence"}`)
	}); e != nil {
		t.Fatal(e)
	}
	raw, e := ReadResidentActivation(ctx, db)
	if e != nil || raw == "" {
		t.Fatal("committed receipt missing", e)
	}
	if e = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return RecordResidentActivationTx(ctx, tx, `{}`) }); e == nil {
		t.Fatal("activation receipt overwritten")
	}
}
