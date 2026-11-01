package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestPersistentCredentialRestartRotationAndFailureClosed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux owner and mode checks")
	}
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store := &identityTestStore{}
	identity, _ := NewIdentityService(store)
	gate := func(context.Context, CredentialOperation) error { return nil }
	first, err := NewPersistentCredentialService(ctx, identity, dir, gate, nil)
	if err != nil {
		t.Fatal(err)
	}
	setup, _ := identity.PrepareSetup(ctx)
	if err = identity.Bootstrap(ctx, setup, "admin", identityTestPassword); err != nil {
		t.Fatal(err)
	}
	session := identityLogin(t, identity)
	meta, err := first.Create(ctx, session.Secret, session.CSRF, credentialInput("synthetic-restart-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err = RotateCredentialMasterKey(dir); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPersistentCredentialService(ctx, identity, dir, gate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.cipher.active == first.cipher.active {
		t.Fatal("rotation did not change key")
	}
	var retained []byte
	err = restarted.UseForService(ctx, credentialOp(session, meta, "after-restart", "use"), func(_ context.Context, secret []byte) error {
		if string(secret) != "synthetic-restart-secret" {
			t.Error("lost ciphertext after restart")
		}
		retained = secret
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range retained {
		if b != 0 {
			t.Fatal("borrow buffer not cleared")
		}
	}
	path := filepath.Join(dir, "credential-keys.json")
	raw, _ := os.ReadFile(path)
	defer clear(raw)
	for _, mode := range []os.FileMode{0644, 0660} {
		os.Chmod(path, mode)
		if _, err = NewPersistentCredentialService(ctx, identity, dir, gate, nil); err == nil {
			t.Fatal("accepted public key file")
		}
	}
	os.Chmod(path, 0600)
	os.WriteFile(path, []byte(`{"version":99,"active":"unknown","keys":{}}`), 0600)
	if _, err = NewPersistentCredentialService(ctx, identity, dir, gate, nil); err == nil {
		t.Fatal("accepted unknown version")
	}
	os.Remove(path)
	if _, err = NewPersistentCredentialService(ctx, identity, dir, gate, nil); err == nil {
		t.Fatal("regenerated lost key for old DB")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created replacement")
	}
	os.WriteFile(path, raw, 0600)
	if _, err = NewPersistentCredentialService(ctx, identity, dir, gate, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentCredentialConcurrentInitialization(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux owner and mode checks")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Separate pristine stores exercise filesystem publication, not just the
			// in-memory transaction mutex.
			s, _ := NewIdentityService(&identityTestStore{})
			v, err := NewPersistentCredentialService(context.Background(), s, dir, nil, nil)
			if err != nil {
				failures <- err
				return
			}
			ids <- v.cipher.active
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var active string
	for id := range ids {
		if active != "" && active != id {
			t.Fatal("racing startup replaced key")
		}
		active = id
	}
	if active == "" {
		t.Fatal("no initialized service")
	}
}

func TestPersistentCredentialRejectDirectoryAndSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux owner and mode checks")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0755)
	s, _ := NewIdentityService(&identityTestStore{})
	if _, err := NewPersistentCredentialService(context.Background(), s, dir, nil, nil); err == nil {
		t.Fatal("public directory")
	}
	os.Chmod(dir, 0700)
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("unchanged"), 0600)
	os.Symlink(outside, filepath.Join(dir, "credential-keys.json"))
	if _, err := NewPersistentCredentialService(context.Background(), s, dir, nil, nil); err == nil {
		t.Fatal("symlink key")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "unchanged" {
		t.Fatal("touched external file")
	}
}
