package updates

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

func TestSQLiteSchemaFingerprintDetectsDDLChangesAndReadsInsideWriteTransaction(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "fingerprint.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, `CREATE TABLE fingerprint_probe(id INTEGER PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	first, err := SQLiteSchemaFingerprint(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	// DDL can change while PRAGMA user_version stays identical; the fingerprint
	// must still differ, otherwise a trusted install would accept new columns.
	if _, err := db.Exec(ctx, `ALTER TABLE fingerprint_probe ADD COLUMN extra BLOB`); err != nil {
		t.Fatal(err)
	}
	second, err := SQLiteSchemaFingerprint(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err := db.QueryRow(ctx, `PRAGMA user_version`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != version {
		t.Fatal("test premise broken: user_version changed")
	}
	if first == second {
		t.Fatal("DDL change did not change the fingerprint")
	}

	// The product live/schema gate runs from inside the journal transaction. A
	// pool read there must not deadlock on the in-process writer lock.
	txCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- db.InTx(txCtx, func(ctx context.Context, tx storage.Tx) error {
			if _, err := tx.Exec(ctx, `CREATE TABLE fingerprint_tx(value TEXT)`); err != nil {
				return err
			}
			_, err := SQLiteSchemaFingerprint(ctx, db)
			return err
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("fingerprint inside write transaction:", err)
		}
	case <-txCtx.Done():
		t.Fatal("schema fingerprint deadlocked inside InTx")
	}
	third, err := SQLiteSchemaFingerprint(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("committed DDL was not reflected after the transaction")
	}
}
