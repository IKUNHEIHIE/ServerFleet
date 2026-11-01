package auth

import (
	"context"
	"os"
	"runtime"
	"testing"
)

func TestWave6RevokedCredentialDoesNotPreventRestart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux private-key store")
	}
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	identity, err := NewIdentityService(&identityTestStore{})
	if err != nil {
		t.Fatal(err)
	}
	gate := func(context.Context, CredentialOperation) error { return nil }
	vault, err := NewPersistentCredentialService(ctx, identity, dir, gate, nil)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.Bootstrap(ctx, setup, "admin", identityTestPassword); err != nil {
		t.Fatal(err)
	}
	session := identityLogin(t, identity)
	meta, err := vault.Create(ctx, session.Secret, session.CSRF, credentialInput("synthetic-revocation-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Revoke(ctx, credentialOp(session, meta, "revoke-before-restart", "revoke")); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPersistentCredentialService(ctx, identity, dir, gate, nil)
	if err != nil {
		t.Fatalf("revoking a credential made Controller restart fail: %v", err)
	}
	if err = restarted.UseForService(ctx, credentialOp(session, meta, "use-after-restart", "use"), func(context.Context, []byte) error {
		t.Fatal("revoked secret was lent after restart")
		return nil
	}); err == nil {
		t.Fatal("revoked credential accepted")
	}
}
