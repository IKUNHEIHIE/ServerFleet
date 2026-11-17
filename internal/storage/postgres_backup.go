package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PostgreSQL backups retain the portable SQLite recovery format. Schema comes
// only from the verified migration registry; rows share one repeatable snapshot.
// Connection secrets and live database files never enter the archive.
func (db *DB) backupPostgres(ctx context.Context, destination string) error {
	m := db.snapshotMigrator.Load()
	if m == nil {
		return errors.New("storage: backup requires a verified complete migration registry")
	}
	dir, err := os.MkdirTemp(filepath.Dir(destination), ".pg-snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "snapshot.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	snapshot, err := Open(ctx, Config{Path: path})
	if err != nil {
		return err
	}
	defer snapshot.Close()
	if _, err = m.Apply(ctx, snapshot); err != nil {
		return err
	}
	source, err := db.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return errors.New("storage: begin postgres snapshot failed")
	}
	defer source.Rollback()
	if _, err = source.ExecContext(ctx, "SET LOCAL search_path = public, pg_catalog"); err != nil {
		return err
	}
	records, err := source.QueryContext(ctx, "SELECT version,checksum FROM public.schema_migrations")
	if err != nil {
		return err
	}
	count := 0
	for records.Next() {
		var v, c string
		if err = records.Scan(&v, &c); err != nil {
			break
		}
		migration, ok := m.byVersion[v]
		if !ok || migration.checksum() != c {
			err = ErrMigrationDrift
			break
		}
		count++
	}
	readErr := records.Err()
	records.Close()
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if count != len(m.byVersion) {
		return ErrUnknownMigration
	}
	tables, err := postgresTables(ctx, source)
	if err != nil {
		return err
	}
	expected, err := sqliteTables(ctx, snapshot.db)
	if err != nil || !slices.Equal(tables, expected) {
		return errors.New("storage: snapshot table set differs from registered schema")
	}
	target, err := snapshot.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer target.Close()
	if _, err = target.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range tables {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+identifier(table)); err != nil {
			return err
		}
	}
	for _, table := range tables {
		cols, err := sqliteColumns(ctx, tx, table)
		if err != nil {
			return err
		}
		rows, err := source.QueryContext(ctx, "SELECT * FROM public."+identifier(table)+" LIMIT 0")
		if err != nil {
			return err
		}
		actual, err := rows.Columns()
		rows.Close()
		if err != nil || !slices.Equal(actual, cols) {
			return errors.New("storage: snapshot columns differ from registered schema")
		}
		if err = copyDatabaseRows(ctx, source, tx, table, cols, dialectSQLite); err != nil {
			return err
		}
	}
	if err = copyPostgresSequences(ctx, source, tx, tables); err != nil {
		return err
	}
	check, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	bad := check.Next()
	checkErr := check.Err()
	check.Close()
	if bad || checkErr != nil {
		return errors.New("storage: snapshot foreign key check failed")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = source.Commit(); err != nil {
		return err
	}
	if err = target.Close(); err != nil {
		return err
	}
	if err = snapshot.Close(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = VerifyFile(ctx, path, true); err != nil {
		return err
	}
	// Atomic publication refuses to replace an existing file or symlink.
	if err = os.Link(path, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrBackupExists
		}
		return err
	}
	parent, err := os.Open(filepath.Dir(destination))
	if err != nil {
		_ = os.Remove(destination)
		return err
	}
	syncErr = parent.Sync()
	closeErr = parent.Close()
	if syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.New("storage: backup directory sync failed")
	}
	return nil
}

func identifier(s string) string { q := string(rune(34)); return q + strings.ReplaceAll(s, q, q+q) + q }

type sqlRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func postgresTables(ctx context.Context, q sqlRowsQueryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname='public' ORDER BY tablename")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func sqliteTables(ctx context.Context, q sqlRowsQueryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func sqliteColumns(ctx context.Context, q sqlRowsQueryer, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+identifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
func copyDatabaseRows(ctx context.Context, source sqlRowsQueryer, target *sql.Tx, table string, cols []string, d dialect) error {
	names := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		names[i] = identifier(c)
		marks[i] = "?"
	}
	query := "SELECT " + strings.Join(names, ",") + " FROM " + identifier(table)
	if d == dialectSQLite {
		query = "SELECT " + strings.Join(names, ",") + " FROM public." + identifier(table)
	}
	rows, err := source.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	stmt, err := target.PrepareContext(ctx, d.prepare("INSERT INTO "+identifier(table)+"("+strings.Join(names, ",")+") VALUES("+strings.Join(marks, ",")+")"))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for rows.Next() {
		vals := make([]any, len(cols))
		pointers := make([]any, len(cols))
		for i := range vals {
			pointers[i] = &vals[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return err
		}
		// database/sql may represent an empty bytea as a typed nil slice.
		// Normalize slices to preserve empty BLOB versus SQL NULL across drivers.
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				copyOf := make([]byte, len(b))
				copy(copyOf, b)
				vals[i] = copyOf
			}
		}
		if _, err = stmt.ExecContext(ctx, vals...); err != nil {
			return fmt.Errorf("storage: copy snapshot table %s failed", table)
		}
	}
	return rows.Err()
}
func copyPostgresSequences(ctx context.Context, source *sql.Tx, target *sql.Tx, tables []string) error {
	var count int
	if err := target.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name='sqlite_sequence'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	for _, table := range tables {
		var ddl string
		if err := target.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&ddl); err != nil {
			return err
		}
		if !strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
			continue
		}
		cols, err := sqliteColumns(ctx, target, table)
		if err != nil {
			return err
		}
		for _, col := range cols {
			var seq sql.NullString
			if err = source.QueryRowContext(ctx, "SELECT pg_catalog.pg_get_serial_sequence($1,$2)", "public."+identifier(table), col).Scan(&seq); err != nil {
				return err
			}
			if !seq.Valid {
				continue
			}
			var value int64
			// Sequence values are deliberately outside MVCC: retaining the high-water
			// mark can skip IDs but must never reissue an allocated identifier.
			if err = source.QueryRowContext(ctx, "SELECT last_value FROM "+seq.String).Scan(&value); err != nil {
				return err
			}
			result, err := target.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name=?", value, table)
			if err != nil {
				return err
			}
			changed, _ := result.RowsAffected()
			if changed == 0 {
				if _, err = target.ExecContext(ctx, "INSERT INTO sqlite_sequence(name,seq) VALUES(?,?)", table, value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
