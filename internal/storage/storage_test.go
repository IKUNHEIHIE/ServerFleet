package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func testMigrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0001_test_items",
		Name:    "test items",
		Statements: []string{
			`CREATE TABLE items (id TEXT PRIMARY KEY, note TEXT, created_at TEXT NOT NULL)`,
		},
	}}
}

func TestOpenUsesWAL(t *testing.T) {
	t.Parallel()
	db := testkit.NewDB(t)
	var mode string
	if err := db.QueryRow(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestRestartKeepsData(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fleet.db")
	ctx := context.Background()
	first := testkit.NewDBAt(t, path, testMigrations())
	if _, err := first.Exec(ctx, "INSERT INTO items (id, note, created_at) VALUES (?, ?, ?)", "n1", "hello", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()
	migrator, _ := storage.NewMigrator(testMigrations())
	report, err := migrator.Apply(ctx, second)
	if err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if len(report.Applied) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("report = %+v, want skipped", report)
	}
	var note string
	if err := second.QueryRow(ctx, "SELECT note FROM items WHERE id = ?", "n1").Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "hello" {
		t.Fatalf("note = %q", note)
	}
}

func TestCorruptFileRejected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite file at all, pad pad pad pad pad pad pad pad pad pad pad"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), storage.Config{Path: path})
	if err == nil {
		db.Close()
		t.Fatal("Open accepted a corrupt file")
	}
}

func TestTxRollbackOnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t, testMigrations())
	boom := errors.New("boom")
	err := db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, "INSERT INTO items (id, created_at) VALUES (?, ?)", "x", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	var count int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

func TestTxCancellationRollsBack(t *testing.T) {
	t.Parallel()
	db := testkit.NewDB(t, testMigrations())
	ctx, cancel := context.WithCancel(context.Background())
	err := db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, "INSERT INTO items (id, created_at) VALUES (?, ?)", "cancelled", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var count int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cancelled transaction left %d rows", count)
	}
}

func TestTxPanicRollsBack(t *testing.T) {
	t.Parallel()
	db := testkit.NewDB(t, testMigrations())
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic did not propagate")
			}
		}()
		_ = db.InTx(context.Background(), func(txCtx context.Context, tx storage.Tx) error {
			if _, err := tx.Exec(txCtx, "INSERT INTO items (id, created_at) VALUES (?, ?)", "panic", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()
	var count int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("panicking transaction left %d rows", count)
	}
}

func TestConcurrentWritersSerialize(t *testing.T) {
	t.Parallel()
	db := testkit.NewDB(t, testMigrations())
	ctx := context.Background()
	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				_, err := tx.Exec(txCtx, "INSERT INTO items (id, created_at) VALUES (?, ?)", "w"+time.Duration(index).String(), time.Now().UTC().Format(time.RFC3339Nano))
				return err
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("writer: %v", err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != writers {
		t.Fatalf("count = %d, want %d", count, writers)
	}
}

func TestMigrationDriftRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t)
	original, _ := storage.NewMigrator(testMigrations())
	if _, err := original.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	changed := testMigrations()
	changed[0].Statements = []string{`CREATE TABLE items (id TEXT PRIMARY KEY, extra TEXT)`}
	edited, _ := storage.NewMigrator(changed)
	if _, err := edited.Apply(ctx, db); !errors.Is(err, storage.ErrMigrationDrift) {
		t.Fatalf("err = %v, want ErrMigrationDrift", err)
	}
}

func TestUnknownMigrationRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t, testMigrations())
	if _, err := db.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES ('ds/9999_future', 'future', 'x', '2026-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	migrator, _ := storage.NewMigrator(testMigrations())
	if _, err := migrator.Apply(ctx, db); !errors.Is(err, storage.ErrUnknownMigration) {
		t.Fatalf("err = %v, want ErrUnknownMigration", err)
	}
}

func TestBackupAndRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t, testMigrations())
	if _, err := db.Exec(ctx, "INSERT INTO items (id, note, created_at) VALUES ('a', 'backed up', ?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := db.BackupTo(ctx, snapshot); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}
	if err := db.BackupTo(ctx, snapshot); !errors.Is(err, storage.ErrBackupExists) {
		t.Fatalf("second backup err = %v, want ErrBackupExists", err)
	}
	restored := filepath.Join(dir, "restored.db")
	if err := storage.RestoreFile(ctx, snapshot, restored); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	restoredDB, err := storage.Open(ctx, storage.Config{Path: restored})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var note string
	if err := restoredDB.QueryRow(ctx, "SELECT note FROM items WHERE id = 'a'").Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "backed up" {
		t.Fatalf("note = %q", note)
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t, testMigrations())
	if _, err := db.Exec(ctx, "INSERT INTO items (id, created_at) VALUES ('r', ?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	readonly, err := storage.Open(ctx, storage.Config{Path: db.Path(), ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if _, err := readonly.Exec(ctx, "INSERT INTO items (id, created_at) VALUES ('w', ?)", time.Now().UTC().Format(time.RFC3339Nano)); err == nil {
		t.Fatal("read-only handle accepted a write")
	}
}

func TestMigratorRejectsBadVersions(t *testing.T) {
	t.Parallel()
	if _, err := storage.NewMigrator([]storage.Migration{{Version: "nope", Name: "x", Statements: []string{"SELECT 1"}}}); err == nil {
		t.Fatal("accepted un-namespaced version")
	}
	if _, err := storage.NewMigrator([]storage.Migration{{Version: "ds/0001_a", Name: "x"}}); err == nil {
		t.Fatal("accepted migration without statements")
	}
}
