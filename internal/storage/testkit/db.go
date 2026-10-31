// Package testkit provides real SQLite handles for tests in other modules. It is
// only imported from _test.go files; production code must call storage.Open.
package testkit

import (
	"context"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/internal/storage"
)

// NewDB opens a file-backed WAL database in a temporary directory, applies the
// supplied migration sets and registers cleanup. A real file (not :memory:) keeps
// WAL, restart and corruption behavior realistic.
func NewDB(t testing.TB, sets ...[]storage.Migration) *storage.DB {
	t.Helper()
	return NewDBAt(t, filepath.Join(t.TempDir(), "fleet.db"), sets...)
}

// NewDBAt opens a database at an explicit path, for restart tests.
func NewDBAt(t testing.TB, path string, sets ...[]storage.Migration) *storage.DB {
	t.Helper()
	handle, err := storage.Open(context.Background(), storage.Config{Path: path})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	if len(sets) > 0 {
		migrator, err := storage.NewMigrator(sets...)
		if err != nil {
			_ = handle.Close()
			t.Fatalf("storage.NewMigrator: %v", err)
		}
		if _, err := migrator.Apply(context.Background(), handle); err != nil {
			_ = handle.Close()
			t.Fatalf("migrator.Apply: %v", err)
		}
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}
