package storage

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// RestorePostgres imports a verified portable snapshot into an EMPTY PostgreSQL
// database owned by the operator. Schema, rows and FK validation commit together.
// It never truncates existing data, disables server-wide triggers, or grants
// recovery authority. Offline reconciliation/fencing remain mandatory.
func RestorePostgres(ctx context.Context, snapshot string, cfg Config, m *Migrator) error {
	if ctx == nil || m == nil || cfg.Driver != "postgres" || cfg.ReadOnly {
		return ErrInvalidConfig
	}
	if err := VerifyFile(ctx, snapshot, true); err != nil {
		return err
	}
	source, err := Open(ctx, Config{Path: snapshot, ReadOnly: true})
	if err != nil {
		return err
	}
	defer source.Close()
	if err = m.VerifyApplied(ctx, source, true); err != nil {
		return err
	}
	from, err := source.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer from.Rollback()
	target, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer target.Close()
	tx, err := target.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_catalog.pg_try_advisory_xact_lock(1936094001)").Scan(&locked); err != nil || !locked {
		return errors.New("storage: restore destination busy")
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL search_path = public, pg_catalog"); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public') + (SELECT count(*) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public')").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("storage: restore requires an empty PostgreSQL public schema")
	}
	if _, err = tx.ExecContext(ctx, dialectPostgres.prepare(migrationsTable)); err != nil {
		return err
	}
	for _, migration := range m.migrations {
		for _, statement := range migration.Statements {
			if _, err = tx.ExecContext(ctx, dialectPostgres.prepare(statement)); err != nil {
				return errors.New("storage: restore schema creation failed")
			}
		}
	}
	tables, err := postgresTables(ctx, tx)
	if err != nil {
		return err
	}
	expected, err := sqliteTables(ctx, from)
	if err != nil || !slices.Equal(tables, expected) {
		return errors.New("storage: restore table set differs from registered schema")
	}
	// Recreate FKs only after all rows have loaded; ADD CONSTRAINT validates every
	// row. This needs table-owner privileges, never session_replication_role.
	rows, err := tx.QueryContext(ctx, "SELECT c.relname,k.conname,pg_catalog.pg_get_constraintdef(k.oid) FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND k.contype='f' ORDER BY c.relname,k.conname")
	if err != nil {
		return err
	}
	var fks [][3]string
	for rows.Next() {
		var fk [3]string
		if err = rows.Scan(&fk[0], &fk[1], &fk[2]); err != nil {
			break
		}
		fks = append(fks, fk)
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	for _, fk := range fks {
		if _, err = tx.ExecContext(ctx, "ALTER TABLE "+identifier(fk[0])+" DROP CONSTRAINT "+identifier(fk[1])); err != nil {
			return err
		}
	}
	for _, table := range tables {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+identifier(table)); err != nil {
			return err
		}
	}
	for _, table := range tables {
		cols, err := sqliteColumns(ctx, from, table)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT * FROM "+identifier(table)+" LIMIT 0")
		if err != nil {
			return err
		}
		actual, err := rows.Columns()
		rows.Close()
		if err != nil || !slices.Equal(cols, actual) {
			return errors.New("storage: restore columns differ from registered schema")
		}
		if err = copyDatabaseRows(ctx, from, tx, table, cols, dialectPostgres); err != nil {
			return err
		}
	}
	for _, fk := range fks {
		if _, err = tx.ExecContext(ctx, "ALTER TABLE "+identifier(fk[0])+" ADD CONSTRAINT "+identifier(fk[1])+" "+fk[2]); err != nil {
			return errors.New("storage: restored foreign key validation failed")
		}
	}
	// Restore both current row maxima and any higher allocated portable sequence.
	rows, err = tx.QueryContext(ctx, "SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public' AND column_default LIKE 'nextval(%' ORDER BY table_name,column_name")
	if err != nil {
		return err
	}
	var seqs [][2]string
	for rows.Next() {
		var s [2]string
		if err = rows.Scan(&s[0], &s[1]); err != nil {
			break
		}
		seqs = append(seqs, s)
	}
	readErr = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	for _, s := range seqs {
		var value int64
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX("+identifier(s[1])+"),0) FROM "+identifier(s[0])).Scan(&value); err != nil {
			return err
		}
		var allocated int64
		e := from.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name=?", s[0]).Scan(&allocated)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if allocated > value {
			value = allocated
		}
		if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.setval(pg_catalog.pg_get_serial_sequence($1,$2),GREATEST($3::bigint,1),$3::bigint>0)", "public."+identifier(s[0]), s[1], value); err != nil {
			return err
		}
	}
	if err = from.Commit(); err != nil {
		return err
	}
	return tx.Commit()
}

// ReadDSNFile accepts only a small private regular file. Error text deliberately
// omits contents, parser errors, and connection strings.
func ReadDSNFile(path string) (string, error) {
	raw, err := readPrivateDSN(path)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" || strings.ContainsRune(dsn, 0) {
		return "", ErrInvalidConfig
	}
	return dsn, nil
}
