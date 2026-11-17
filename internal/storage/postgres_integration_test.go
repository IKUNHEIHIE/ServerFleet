//go:build postgres

package storage_test

import (
	"context"
	"os"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
)

func TestPostgresMigrations(t *testing.T) {
	dsn := os.Getenv("SERVERFLEET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("SERVERFLEET_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Driver: "postgres", DSN: dsn, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
