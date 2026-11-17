//go:build postgres

package storage_test

import (
	"context"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
)

func TestPostgresMigrations(t *testing.T) {
	ctx := context.Background()
	db, _ := maintenanceDatabase(t)
	migrator, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	report, err := migrator.Apply(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) == 0 {
		t.Fatal("no migrations applied")
	}
	versions, err := storage.AppliedVersions(ctx, db)
	if err != nil || len(versions) != len(migrator.Versions()) {
		t.Fatalf("versions=%d err=%v", len(versions), err)
	}
}
