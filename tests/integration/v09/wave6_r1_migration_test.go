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
			if completionOnlyMigration(migration.Version) {
				continue
			}
			if migration.Version != "cx/0007_ai_budgets_and_model_audit" && migration.Version != "ds/0081_publishing_targets" && migration.Version != "cx/0008_file_execution_bindings" && migration.Version != "cx/0009_update_journal" && migration.Version != "ds/0062_acme_cleanup" && migration.Version != "ds/0063_cleanup_claim" && migration.Version != "ds/0042_file_distribution" && migration.Version != "cx/0010_gateway_artifact_bindings" && migration.Version != "ds/0043_upload_policies" {
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
	if err != nil || len(versions) != 50 {
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

// Produce an actual 28-migration database with frozen old SQL. A current Repo
// cannot manufacture legacy columns and thereby hide an upgrade regression.
func TestUploadPolicyMigrationUpgradesActual28WithoutReinterpretingQueuedPlans(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "old28.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var old []storage.Migration
	for _, set := range server.Migrations() {
		for _, m := range set {
			if completionOnlyMigration(m.Version) {
				continue
			}
			if m.Version != "ds/0043_upload_policies" {
				old = append(old, m)
			}
		}
	}
	migrator, err := storage.NewMigrator(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	versions, err := storage.AppliedVersions(ctx, db)
	if err != nil || len(versions) != 28 {
		t.Fatalf("not actual old28: %v %v", versions, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO file_uploads(id,subject_id,node_id,destination_path,status,bytes_received,expected_size,sha256_expected,sha256_actual,error_code,staging_name,task_id,created_at,updated_at) VALUES('old-upload','owner','node','/srv/file','dispatching',7,7,'expected','actual','','legacy.part','old-task','2026-11-08T00:00:00Z','2026-11-08T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	migrator, err = storage.NewMigrator(server.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var policy, actual, task, digest string
	if err = db.QueryRow(ctx, `SELECT conflict_policy,actual_destination,task_id,sha256_actual FROM file_uploads WHERE id='old-upload'`).Scan(&policy, &actual, &task, &digest); err != nil || policy != "overwrite" || actual != "" || task != "old-task" || digest != "actual" {
		t.Fatalf("legacy semantics changed: %q %q %q %q %v", policy, actual, task, digest, err)
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	versions, err = storage.AppliedVersions(ctx, db)
	if err != nil || len(versions) != 50 {
		t.Fatalf("upgrade count: %v %v", versions, err)
	}
}
