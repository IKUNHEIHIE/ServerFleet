package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradePreflightRetainsLegacyStateAndDiagnosesMissingOrWrongKeys(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	_ = os.Chmod(directory, 0700)
	store := &identityTestStore{}
	identity, _ := NewIdentityService(store)
	credentials, e := NewPersistentCredentialService(ctx, identity, directory, func(context.Context, CredentialOperation) error { return nil }, nil)
	if e != nil {
		t.Fatal(e)
	}
	token, e := identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = identity.Bootstrap(ctx, token, "admin", identityTestPassword); e != nil {
		t.Fatal(e)
	}
	session := identityLogin(t, identity)
	if _, e = credentials.Create(ctx, session.Secret, session.CSRF, credentialInput("synthetic-upgrade-secret")); e != nil {
		t.Fatal(e)
	}
	var before []byte
	_ = store.View(ctx, func(raw []byte) error { before = append([]byte{}, raw...); return nil })
	report, e := CheckIdentityUpgrade(ctx, store, directory)
	if e != nil || !report.Initialized || !report.CredentialsVerified {
		t.Fatal(report, e)
	}
	_ = store.View(ctx, func(raw []byte) error {
		if string(raw) != string(before) {
			t.Fatal("preflight changed payload")
		}
		return nil
	})
	filename := filepath.Join(directory, "credential-keys.json")
	original, e := os.ReadFile(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(original)
	_ = os.Remove(filename)
	_, e = CheckIdentityUpgrade(ctx, store, directory)
	var issue *UpgradeError
	if !errors.As(e, &issue) || issue.Code != "CREDENTIAL_KEYRING_MISSING" {
		t.Fatal("missing-key diagnostic", e)
	}
	if _, e = os.Stat(filename); !os.IsNotExist(e) {
		t.Fatal("preflight regenerated missing keys")
	}
	_ = os.WriteFile(filename, original, 0644)
	_, e = CheckIdentityUpgrade(ctx, store, directory)
	if !errors.As(e, &issue) || issue.Code != "CREDENTIAL_KEYRING_PERMISSIONS" {
		t.Fatal("permissions diagnostic", e)
	}
	_ = os.Chmod(filename, 0600)
	var ring CredentialKeyring
	if json.Unmarshal(original, &ring) != nil {
		t.Fatal("fixture")
	}
	for id := range ring.Keys {
		ring.Keys[id] = make([]byte, 32)
	}
	wrong, _ := json.Marshal(ring)
	_ = os.WriteFile(filename, wrong, 0600)
	_, e = CheckIdentityUpgrade(ctx, store, directory)
	if !errors.As(e, &issue) || issue.Code != "CREDENTIAL_KEY_BINDING_OR_CIPHERTEXT_INVALID" {
		t.Fatal("wrong-key diagnostic", e)
	}
	_ = os.WriteFile(filename, original, 0600)
	if _, e = CheckIdentityUpgrade(ctx, store, directory); e != nil {
		t.Fatal("original keys did not restore compatibility", e)
	}
}
