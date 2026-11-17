//go:build postgres

package storage_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/updates"
)

func maintenanceDatabase(t *testing.T) (*storage.DB, storage.Config) {
	t.Helper()
	dsn := os.Getenv("SERVERFLEET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL test connection is required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("connect isolated test cluster:", err)
	}
	var exists bool
	if err = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='maintenance_owner')").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		if _, err = admin.Exec(ctx, "CREATE ROLE maintenance_owner LOGIN"); err != nil {
			t.Fatal(err)
		}
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "maintenance_" + hex.EncodeToString(nonce[:])
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+name+" OWNER maintenance_owner TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.Password != "" {
		password := "'" + strings.ReplaceAll(config.Password, "'", "''") + "'"
		if _, err = admin.Exec(ctx, "ALTER ROLE maintenance_owner PASSWORD "+password); err != nil {
			t.Fatal(err)
		}
	}
	config.Database = name
	config.User = "maintenance_owner"
	u := url.URL{Scheme: "postgres", User: url.UserPassword("maintenance_owner", config.Password), Path: "/" + name}
	q := u.Query()
	q.Set("host", config.Host)
	q.Set("port", strconv.Itoa(int(config.Port)))
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	cfg := storage.Config{Driver: "postgres", DSN: u.String(), Path: filepath.Join(t.TempDir(), "restored.db")}
	db, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var superuser bool
	if err = db.QueryRow(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&superuser); err != nil || superuser {
		t.Fatal("test database owner must not be a superuser", err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close(context.Background())
	})
	return db, cfg
}
func maintenanceMigrator(t *testing.T) *storage.Migrator {
	t.Helper()
	sets := server.Migrations()
	sets = append(sets, []storage.Migration{{Version: "zz/9999_maintenance_probe", Name: "portable maintenance probe", Statements: []string{
		"CREATE TABLE maintenance_probe(id INTEGER PRIMARY KEY AUTOINCREMENT,value TEXT NOT NULL,payload BLOB,nullable TEXT,wide INTEGER NOT NULL DEFAULT 0,parent_id INTEGER REFERENCES maintenance_probe(id),CHECK(wide>=0))",
	}}})
	m, err := storage.NewMigrator(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestPostgresPortableBackupRestore(t *testing.T) {
	ctx := context.Background()
	db, cfg := maintenanceDatabase(t)
	m := maintenanceMigrator(t)
	if _, err := m.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(id,value,payload,wide) VALUES(1,?,?,?)", "测试 ' quotes", []byte{0, 1, 255}, int64(9000000000)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(id,value,payload,parent_id) VALUES(2,'child',?,1)", []byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "SELECT setval('maintenance_probe_id_seq',10000000000,true)"); err != nil {
		t.Fatal(err)
	}
	var sourceEmpty int
	if err := db.QueryRow(ctx, "SELECT octet_length(payload) FROM maintenance_probe WHERE id=2").Scan(&sourceEmpty); err != nil || sourceEmpty != 0 {
		t.Fatal("source empty blob premise failed", err)
	}
	before, err := updates.SchemaFingerprint(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "portable.db")
	if err = db.BackupTo(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err = db.BackupTo(ctx, path); !errors.Is(err, storage.ErrBackupExists) {
		t.Fatal("overwrote backup", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("snapshot permissions", err)
	}
	portable, err := storage.Open(ctx, storage.Config{Path: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer portable.Close()
	if err = m.VerifyApplied(ctx, portable, true); err != nil {
		t.Fatal(err)
	}
	var sequence int64
	if err = portable.QueryRow(ctx, "SELECT seq FROM sqlite_sequence WHERE name='maintenance_probe'").Scan(&sequence); err != nil || sequence != 10000000000 {
		t.Fatal("lost sequence watermark", sequence, err)
	}
	dest, destCfg := maintenanceDatabase(t)
	if err = storage.RestorePostgres(ctx, path, destCfg, m); err != nil {
		t.Fatal(err)
	}
	var value string
	var payload []byte
	var nullable storage.NullString
	var wide int64
	if err = dest.QueryRow(ctx, "SELECT value,payload,nullable,wide FROM maintenance_probe WHERE id=1").Scan(&value, &payload, &nullable, &wide); err != nil {
		t.Fatal(err)
	}
	if value != "测试 ' quotes" || hex.EncodeToString(payload) != "0001ff" || nullable.Valid || wide != 9000000000 {
		t.Fatal("round trip changed data", value, payload, nullable, wide)
	}
	var empty int
	if err = dest.QueryRow(ctx, "SELECT octet_length(payload) FROM maintenance_probe WHERE id=2").Scan(&empty); err != nil || empty != 0 {
		t.Fatal("empty blob changed", err)
	}
	if err = dest.QueryRow(ctx, "INSERT INTO maintenance_probe(value) VALUES('after restore') RETURNING id").Scan(&sequence); err != nil || sequence != 10000000001 {
		t.Fatal("sequence reused", sequence, err)
	}
	if _, err = dest.Exec(ctx, "INSERT INTO maintenance_probe(value,parent_id) VALUES('invalid',9999)"); err == nil {
		t.Fatal("foreign key not enforced")
	}
	if err = storage.RestorePostgres(ctx, path, destCfg, m); err == nil {
		t.Fatal("nonempty destination accepted")
	}
	var n int
	if err = dest.QueryRow(ctx, "SELECT count(*) FROM maintenance_probe").Scan(&n); err != nil || n != 3 {
		t.Fatal("existing target changed", n, err)
	}
	after, err := updates.SchemaFingerprint(ctx, db)
	if err != nil || after != before {
		t.Fatal("backup modified source schema", err)
	}
	cfg.ReadOnly = true
	readOnly, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err = readOnly.Exec(ctx, "DELETE FROM maintenance_probe"); err == nil {
		t.Fatal("read-only PostgreSQL handle permitted writes")
	}
}

func TestPostgresRestoreRollsBackInvalidReferences(t *testing.T) {
	ctx := context.Background()
	db, _ := maintenanceDatabase(t)
	m := maintenanceMigrator(t)
	if _, err := m.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(id,value) VALUES(1,'parent')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(id,value,parent_id) VALUES(2,'child',1)"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "invalid.db")
	if err := db.BackupTo(ctx, path); err != nil {
		t.Fatal(err)
	}
	broken, err := storage.Open(ctx, storage.Config{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broken.Exec(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err = broken.Exec(ctx, "DELETE FROM maintenance_probe WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	broken.Close()
	target, cfg := maintenanceDatabase(t)
	if err = storage.RestorePostgres(ctx, path, cfg, m); err == nil {
		t.Fatal("corrupt foreign key accepted")
	}
	var count int
	if err = target.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed restore left partial schema/data", count, err)
	}
}

func TestPostgresFingerprintTracksDDLOnly(t *testing.T) {
	ctx := context.Background()
	db, _ := maintenanceDatabase(t)
	m := maintenanceMigrator(t)
	if _, err := m.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	fingerprint := func() string {
		t.Helper()
		s, err := updates.SchemaFingerprint(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	initial := fingerprint()
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(value) VALUES('row')"); err != nil {
		t.Fatal(err)
	}
	if got := fingerprint(); got != initial {
		t.Fatal("row/sequence value changed schema fingerprint")
	}
	for _, ddl := range []string{
		"ALTER TABLE maintenance_probe ADD COLUMN extra TEXT",
		"CREATE INDEX maintenance_probe_extra ON maintenance_probe(extra)",
		"ALTER TABLE maintenance_probe ADD CONSTRAINT value_nonempty CHECK(value<>'')",
		"ALTER SEQUENCE maintenance_probe_id_seq INCREMENT BY 2",
		"CREATE RULE maintenance_no_update AS ON UPDATE TO maintenance_probe DO INSTEAD NOTHING",
	} {
		before := fingerprint()
		if _, err := db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		if got := fingerprint(); got == before {
			t.Fatal("DDL did not change fingerprint", ddl)
		}
	}
}

func TestPostgresBackupRejectsDriftAndCancellation(t *testing.T) {
	ctx := context.Background()
	db, _ := maintenanceDatabase(t)
	m := maintenanceMigrator(t)
	if _, err := m.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE maintenance_probe ADD COLUMN unregistered TEXT"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rejected.db")
	if err := db.BackupTo(ctx, path); err == nil {
		t.Fatal("unregistered column silently lost")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed backup published")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.BackupTo(cancelled, path); err == nil {
		t.Fatal("cancellation ignored")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 0 {
		t.Fatal("plaintext temporary snapshot left behind", err)
	}
}

func TestPostgresSupervisorConnectionBinding(t *testing.T) {
	db, cfg := maintenanceDatabase(t)
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte(cfg.DSN), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(cfg.DSN))
	c := updates.ResidentSupervisorConfig{Database: cfg.Path, DatabaseDriver: "postgres", DatabaseDSNFile: path, DatabaseDSNSHA256: hex.EncodeToString(digest[:])}
	actual, err := c.DatabaseConfig(false)
	if err != nil || !db.MatchesConfig(actual) {
		t.Fatal("supervisor storage mismatch", err)
	}
	reader, err := updates.OpenResidentDatabase(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	c.DatabaseDSNSHA256 = strings.Repeat("0", 64)
	if _, err = c.DatabaseConfig(false); err == nil {
		t.Fatal("unmatched DSN fingerprint accepted")
	}
	c.DatabaseDriver = ""
	if _, err = c.DatabaseConfig(false); err == nil {
		t.Fatal("PostgreSQL credentials silently accepted by SQLite supervisor")
	}
}

// Paired values change atomically during the export. A per-table/read-committed
// copy could combine generations; the portable snapshot must keep them equal.
func TestPostgresBackupConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	db, _ := maintenanceDatabase(t)
	m := maintenanceMigrator(t)
	if _, err := m.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO maintenance_probe(id,value,wide) SELECT n,'padding',0 FROM generate_series(1,4000) n"); err != nil {
		t.Fatal(err)
	}
	writerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan error, 1)
	go func() {
		defer wg.Done()
		var generation int64
		for writerCtx.Err() == nil {
			generation++
			_, err := db.Exec(writerCtx, "UPDATE maintenance_probe SET wide=? WHERE id IN (1,4000)", generation)
			if err != nil && writerCtx.Err() == nil {
				done <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	defer func() { cancel(); wg.Wait() }()
	path := filepath.Join(t.TempDir(), "concurrent.db")
	if err := db.BackupTo(ctx, path); err != nil {
		t.Fatal(err)
	}
	cancel()
	wg.Wait()
	select {
	case err := <-done:
		t.Fatal(err)
	default:
	}
	snapshot, err := storage.Open(ctx, storage.Config{Path: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var distinct int
	if err = snapshot.QueryRow(ctx, "SELECT count(DISTINCT wide) FROM maintenance_probe WHERE id IN (1,4000)").Scan(&distinct); err != nil || distinct != 1 {
		t.Fatal("snapshot mixed committed generations", distinct, err)
	}
}
