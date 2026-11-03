package v09_test

import (
	"context"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
)

func TestR1MigrationPreservesExistingPublicationAndAddsOnlyNewVersions(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "upgrade.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var old []storage.Migration
	for _, set := range server.Migrations() {
		for _, migration := range set {
			if migration.Version != "cx/0007_ai_budgets_and_model_audit" && migration.Version != "ds/0081_publishing_targets" && migration.Version != "cx/0008_file_execution_bindings" && migration.Version != "cx/0009_update_journal" && migration.Version != "ds/0062_acme_cleanup" && migration.Version != "ds/0063_cleanup_claim" {
				old = append(old, migration)
			}
		}
	}
	legacy, err := storage.NewMigrator(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO publishing_plans(id,name,domain,service_group_id,expected_group_revision,status,current_stage,created_by,created_at,updated_at) VALUES('old-plan','Existing publication','app.example.com','old-group',1,'queued','dns','owner','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	current, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = current.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = current.Apply(ctx, db); err != nil {
		t.Fatal("migration replay changed history", err)
	}
	versions, err := storage.AppliedVersions(ctx, db)
	if err != nil || len(versions) != 26 {
		t.Fatal(versions, err)
	}
	var name, gateways string
	var revision int
	if err = db.QueryRow(ctx, `SELECT name,gateway_ids,target_revision FROM publishing_plans WHERE id='old-plan'`).Scan(&name, &gateways, &revision); err != nil || name != "Existing publication" || gateways != "[]" || revision != 1 {
		t.Fatal("existing plan changed", name, gateways, revision, err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM cx_ai_generation_budget`).Scan(&count); err != nil || count != 1 {
		t.Fatal("budget schema unavailable", count, err)
	}
}
