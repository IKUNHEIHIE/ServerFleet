package v09_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"testing"
)

// The accepted SQL is frozen independently from current files.Migrations.
// Rebuilding both old and new schemas from current definitions masks history drift.
//
//go:embed testdata/ds0040-files-adopted.json
var adoptedFileMigrationJSON []byte

func TestCloudUploadMigrationPreservesAdoptedFileHistory(t *testing.T) {
	var fixture struct {
		SourceCommit string
		Checksum     string
		Migration    storage.Migration
	}
	if err := json.Unmarshal(adoptedFileMigrationJSON, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SourceCommit != "1ca79072232bfdcee2b9f4582ac020d4430d0c2b" {
		t.Fatal("unexpected historical source", fixture.SourceCommit)
	}
	var old []storage.Migration
	for _, set := range server.Migrations() {
		for _, m := range set {
			if completionOnlyMigration(m.Version) {
				continue
			}
			if m.Version == "ds/0043_upload_policies" || m.Version == "ds/0041_file_upload_task" || m.Version == "ds/0042_file_distribution" || m.Version == "cx/0010_gateway_artifact_bindings" {
				continue
			}
			if m.Version == fixture.Migration.Version {
				m = fixture.Migration
			}
			old = append(old, m)
		}
	}
	if len(old) != 25 {
		t.Fatalf("adopted migration count=%d, want 25", len(old))
	}
	db := testkit.NewDB(t, old)
	ctx := context.Background()
	var before string
	if err := db.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, fixture.Migration.Version).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != fixture.Checksum {
		t.Fatal("historical fixture checksum mismatch", before, fixture.Checksum)
	}
	if _, err := db.Exec(ctx, `INSERT INTO file_uploads(id,subject_id,node_id,destination_path,status,staging_name,created_at,updated_at) VALUES('adopted-upload','owner','node-one','/srv/file.bin','open','adopted-stage','2026-11-02T00:00:00Z','2026-11-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	current, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := current.Apply(ctx, db); err != nil {
			t.Fatalf("upgrade/reapply %d rejected adopted database: %v", i, err)
		}
	}
	var after, taskID, status string
	if err := db.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, fixture.Migration.Version).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("adopted history rewritten", before, after)
	}
	if err := db.QueryRow(ctx, `SELECT task_id,status FROM file_uploads WHERE id='adopted-upload'`).Scan(&taskID, &status); err != nil {
		t.Fatal(err)
	}
	if taskID != "" || status != "open" {
		t.Fatalf("old upload changed: task=%q status=%q", taskID, status)
	}
	versions, err := storage.AppliedVersions(ctx, db)
	if err != nil || len(versions) != 48 {
		t.Fatal("new migration count", versions, err)
	}
}
