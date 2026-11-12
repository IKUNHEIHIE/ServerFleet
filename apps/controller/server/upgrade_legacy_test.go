package server

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"testing"
)

func TestLegacy49MigrationIdentityAndOriginalKeyringUpgradeTo50(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database := filepath.Join(directory, "legacy.db")
	keys := filepath.Join(directory, "credential-keys")
	groups := [][]storage.Migration{}
	for _, group := range Migrations() {
		old := []storage.Migration{}
		for _, m := range group {
			if m.Version != "ds/0050_node_diagnostics" {
				old = append(old, m)
			}
		}
		if len(old) > 0 {
			groups = append(groups, old)
		}
	}
	db, e := storage.Open(ctx, storage.Config{Path: database})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	migrator, e := storage.NewMigrator(groups...)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrator.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	store, e := newIdentityStore(db, nil)
	if e != nil {
		t.Fatal(e)
	}
	identity, _ := auth.NewIdentityService(store)
	if e = auth.EnsureCredentialKeyDirectory(keys); e != nil {
		t.Fatal(e)
	}
	if _, e = auth.NewPersistentCredentialService(ctx, identity, keys, nil, nil); e != nil {
		t.Fatal(e)
	}
	setup, e := identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = identity.Bootstrap(ctx, setup, "admin", "synthetic-legacy-password"); e != nil {
		t.Fatal(e)
	}
	login, e := identity.Login(ctx, "admin", "synthetic-legacy-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Update(ctx, func(raw []byte) ([]byte, error) {
		var state map[string]any
		if e := json.Unmarshal(raw, &state); e != nil {
			return nil, e
		}
		state["Version"] = 1
		delete(state, "EnrollmentReadGrants")
		return json.Marshal(state)
	}); e != nil {
		t.Fatal(e)
	}
	var before []byte
	_ = store.View(ctx, func(raw []byte) error { before = append([]byte{}, raw...); return nil })
	report, e := CheckUpgrade(ctx, database, keys)
	if e != nil || !report.Initialized || !report.CredentialsVerified || report.Version != 1 {
		t.Fatal(report, e)
	}
	_ = store.View(ctx, func(raw []byte) error {
		if !bytes.Equal(before, raw) {
			t.Fatal("read-only preflight mutated old identity")
		}
		return nil
	})
	versions, e := storage.AppliedVersions(ctx, db)
	if e != nil || len(versions) != 49 {
		t.Fatal("preflight applied migrations", len(versions), e)
	}
	_ = db.Close()
	upgraded, e := New(ctx, Config{DBPath: database, Origin: "https://fleet.test", CredentialKeyDir: keys})
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	versions, e = storage.AppliedVersions(ctx, upgraded.DB)
	if e != nil || len(versions) != 50 {
		t.Fatal(len(versions), e)
	}
	current, e := upgraded.Identity.Authenticate(ctx, login.Secret, "", false)
	if e != nil || current.SubjectID != login.Identity.SubjectID {
		t.Fatal("upgrade lost existing identity/session", e)
	}
}
