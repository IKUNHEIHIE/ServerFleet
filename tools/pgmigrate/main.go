// Command pgmigrate copies a stopped ServerFleet SQLite database into an
// already-migrated PostgreSQL database using COPY. It is an offline operator
// tool: the Controller must be stopped, and the PostgreSQL DSN must use a
// superuser so session_replication_role can defer foreign-key checks.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite"
)

func main() {
	sqlitePath := flag.String("sqlite", "", "existing SQLite database path (must be stopped)")
	pgDSN := flag.String("pg", "", "PostgreSQL DSN (superuser)")
	tableFilter := flag.String("table", "", "optional single table to copy")
	flag.Parse()
	if *sqlitePath == "" || *pgDSN == "" {
		fmt.Fprintln(os.Stderr, "-sqlite and -pg are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if err := run(ctx, *sqlitePath, *pgDSN, *tableFilter); err != nil {
		fmt.Fprintln(os.Stderr, "pgmigrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, sqlitePath, pgDSN, only string) error {
	raw, err := sql.Open("sqlite", "file:"+sqlitePath)
	if err != nil {
		return err
	}
	defer raw.Close()
	conn, err := pgx.Connect(ctx, pgDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		return fmt.Errorf("enable replica session role: %w", err)
	}
	tables, err := sqliteTables(ctx, raw)
	if err != nil {
		return err
	}
	if only != "" {
		tables = []string{only}
	}
	if len(tables) == 0 {
		return fmt.Errorf("no tables found in %s", sqlitePath)
	}
	if err := truncateAll(ctx, conn, tables); err != nil {
		return err
	}
	total := int64(0)
	for _, table := range tables {
		columns, err := sqliteColumns(ctx, raw, table)
		if err != nil {
			return err
		}
		count, err := copyTable(ctx, raw, conn, table, columns)
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		total += count
		fmt.Printf("%-48s %8d rows\n", table, count)
	}
	if err := fixSequences(ctx, conn); err != nil {
		return err
	}
	fmt.Printf("copied %d rows across %d tables\n", total, len(tables))
	return nil
}

func sqliteTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func sqliteColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+strings.ReplaceAll(table, `"`, `""`)+`")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func truncateAll(ctx context.Context, conn *pgx.Conn, tables []string) error {
	if _, err := conn.Exec(ctx, "TRUNCATE TABLE "+quoteTableList(tables)+" RESTART IDENTITY CASCADE"); err != nil {
		return fmt.Errorf("truncate target tables: %w", err)
	}
	return nil
}

func copyTable(ctx context.Context, src *sql.DB, dst *pgx.Conn, table string, columns []string) (int64, error) {
	selectSQL := "SELECT " + quoteColumnList(columns) + " FROM \"" + strings.ReplaceAll(table, `"`, `""`) + "\""
	rows, err := src.QueryContext(ctx, selectSQL)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	bytea, err := byteaColumns(ctx, dst, table, columns)
	if err != nil {
		return 0, err
	}
	source := &copyRows{rows: rows, columns: len(columns), bytea: bytea}
	return dst.CopyFrom(ctx, pgx.Identifier{table}, columns, source)
}

type copyRows struct {
	rows    *sql.Rows
	columns int
	bytea   map[int]bool
}

func byteaColumns(ctx context.Context, conn *pgx.Conn, table string, columns []string) (map[int]bool, error) {
	rows, err := conn.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND data_type='bytea'`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bytea := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		bytea[name] = true
	}
	out := map[int]bool{}
	for i, name := range columns {
		if bytea[name] {
			out[i] = true
		}
	}
	return out, rows.Err()
}

func (c *copyRows) Next() bool { return c.rows.Next() }

func (c *copyRows) Values() ([]any, error) {
	values := make([]any, c.columns)
	dest := make([]any, c.columns)
	for i := range values {
		dest[i] = &values[i]
	}
	if err := c.rows.Scan(dest...); err != nil {
		return nil, err
	}
	for i, value := range values {
		switch typed := value.(type) {
		case []byte:
			copied := make([]byte, len(typed))
			copy(copied, typed)
			if c.bytea[i] {
				values[i] = copied
			} else {
				values[i] = string(copied)
			}
		case string:
			if c.bytea[i] {
				values[i] = []byte(typed)
			} else {
				values[i] = typed
			}
		}
	}
	return values, nil
}

func (c *copyRows) Err() error { return c.rows.Err() }

func fixSequences(ctx context.Context, conn *pgx.Conn) error {
	rows, err := conn.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='public' AND column_default LIKE 'nextval(%' ORDER BY table_name, column_name`)
	if err != nil {
		return err
	}
	type seq struct{ table, column string }
	var list []seq
	for rows.Next() {
		var item seq
		if err := rows.Scan(&item.table, &item.column); err != nil {
			rows.Close()
			return err
		}
		list = append(list, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range list {
		statement := fmt.Sprintf("SELECT setval(pg_get_serial_sequence('%s','%s'), COALESCE(MAX(%s),0)+1, false) FROM %s",
			item.table, item.column, quoteIdent(item.column), quoteIdent(item.table))
		if _, err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("fix sequence %s.%s: %w", item.table, item.column, err)
		}
	}
	return nil
}

func quoteIdent(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func quoteColumnList(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = quoteIdent(value)
	}
	return strings.Join(parts, ", ")
}

func quoteTableList(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = quoteIdent(value)
	}
	return strings.Join(parts, ", ")
}
