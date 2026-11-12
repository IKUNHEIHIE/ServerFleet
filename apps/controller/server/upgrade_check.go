package server

import (
	"context"
	"fmt"
	"path/filepath"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

func credentialDirectory(database, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	absolute, e := filepath.Abs(database)
	if e != nil {
		return "", e
	}
	return filepath.Join(filepath.Dir(absolute), "credential-keys"), nil
}
func preflightExistingIdentity(ctx context.Context, db *storage.DB, directory string) (auth.IdentityUpgradeReport, error) {
	var count int
	if e := db.QueryRow(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cx_auth_identity'").Scan(&count); e != nil {
		return auth.IdentityUpgradeReport{}, fmt.Errorf("controller: identity preflight database unavailable: %w", e)
	}
	if count == 0 {
		return auth.IdentityUpgradeReport{}, nil
	}
	store, e := newIdentityStore(db, nil)
	if e != nil {
		return auth.IdentityUpgradeReport{}, e
	}
	return auth.CheckIdentityUpgrade(ctx, store, directory)
}
func CheckUpgrade(ctx context.Context, database, keyDirectory string) (auth.IdentityUpgradeReport, error) {
	db, e := storage.Open(ctx, storage.Config{Path: database, ReadOnly: true})
	if e != nil {
		return auth.IdentityUpgradeReport{}, e
	}
	defer db.Close()
	m, e := storage.NewMigrator(Migrations()...)
	if e != nil {
		return auth.IdentityUpgradeReport{}, e
	}
	if e = m.VerifyApplied(ctx, db, false); e != nil {
		return auth.IdentityUpgradeReport{}, fmt.Errorf("controller: MIGRATION_LEDGER_INCOMPATIBLE: %w", e)
	}
	directory, e := credentialDirectory(database, keyDirectory)
	if e != nil {
		return auth.IdentityUpgradeReport{}, e
	}
	return preflightExistingIdentity(ctx, db, directory)
}
