// Package storage owns SQLite connection setup, WAL, transaction and migration
// infrastructure. Business schemas and repositories belong to their owning modules;
// this package never imports a domain package. It is the only package allowed to
// import database/sql, so it exposes narrow Queryer/Tx abstractions instead.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"database/sql"

	_ "modernc.org/sqlite"
)

var (
	ErrClosed           = errors.New("storage: database is closed")
	ErrInvalidConfig    = errors.New("storage: invalid configuration")
	ErrNotWAL           = errors.New("storage: database is not in WAL mode")
	ErrCorrupt          = errors.New("storage: database integrity check failed")
	ErrBackupExists     = errors.New("storage: backup destination already exists")
	ErrMigrationDrift   = errors.New("storage: applied migration checksum changed")
	ErrUnknownMigration = errors.New("storage: database has a migration that is not registered")
)

// Config controls one SQLite database handle. Zero values select the defaults:
// busy timeout 5s, four pooled connections and an integrity check at startup.
type Config struct {
	Path string

	// BusyTimeout bounds how long SQLite waits for another process' write lock.
	BusyTimeout time.Duration

	// MaxOpenConns bounds pooled connections. Writers are additionally serialized
	// in-process by the handle so concurrent callers queue instead of failing BUSY.
	MaxOpenConns int

	// ReadOnly opens the file without WAL/pragma writes. Migrations must not run
	// on a read-only handle.
	ReadOnly bool

	// SkipIntegrityCheck disables the startup PRAGMA quick_check. It is intended
	// only for large production databases where startup time matters; corrupt
	// files then fail on first use instead of at Open.
	SkipIntegrityCheck bool
}

func (c Config) normalized() (Config, error) {
	if strings.TrimSpace(c.Path) == "" {
		return Config{}, fmt.Errorf("%w: path is required", ErrInvalidConfig)
	}
	if c.BusyTimeout == 0 {
		c.BusyTimeout = 5 * time.Second
	}
	if c.BusyTimeout < 0 {
		return Config{}, fmt.Errorf("%w: busy timeout must be positive", ErrInvalidConfig)
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 4
	}
	if c.MaxOpenConns < 1 {
		return Config{}, fmt.Errorf("%w: max open connections must be positive", ErrInvalidConfig)
	}
	return c, nil
}

// DB is a SQLite handle. It is safe for concurrent use. All writes inside one
// process are serialized through InTx so callers observe queueing rather than
// SQLITE_BUSY; reads use the connection pool.
type DB struct {
	db   *sql.DB
	path string

	writer chan struct{}
	once   sync.Once
}

// Open prepares the database file (creating parent directories), applies the
// connection pragmas and verifies the result. A corrupt file is rejected here
// unless Config.SkipIntegrityCheck is set.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve path: %w", err)
	}
	if !cfg.ReadOnly {
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, fmt.Errorf("storage: create database directory: %w", err)
		}
	}
	dsn := buildDSN(abs, cfg)
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.MaxOpenConns)
	pool.SetConnMaxLifetime(0)

	handle := &DB{db: pool, path: abs, writer: make(chan struct{}, 1)}
	if err := handle.verify(ctx, cfg); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return handle, nil
}

func (db *DB) verify(ctx context.Context, cfg Config) error {
	if err := db.db.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: ping: %w", err)
	}
	if !cfg.ReadOnly {
		var mode string
		if err := db.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			return fmt.Errorf("storage: read journal mode: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			return fmt.Errorf("%w: got %q", ErrNotWAL, mode)
		}
	}
	if cfg.SkipIntegrityCheck {
		return nil
	}
	var result string
	if err := db.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("storage: integrity check: %w", err)
	}
	if !strings.EqualFold(result, "ok") {
		return fmt.Errorf("%w: %s", ErrCorrupt, result)
	}
	return nil
}

func buildDSN(abs string, cfg Config) string {
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}
	q := url.Values{}
	busyMS := cfg.BusyTimeout.Milliseconds()
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyMS))
	q.Add("_pragma", "foreign_keys(1)")
	if cfg.ReadOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Path returns the absolute database file path.
func (db *DB) Path() string { return db.path }

// Ping checks that the handle can still reach the file.
func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.db == nil {
		return ErrClosed
	}
	return db.db.PingContext(ctx)
}

// Close releases pooled connections. Callers must not use the handle afterwards.
func (db *DB) Close() error {
	if db == nil || db.db == nil {
		return nil
	}
	var err error
	db.once.Do(func() { err = db.db.Close() })
	return err
}

// Exec runs one statement outside a transaction. Multi-statement writes should
// use InTx so a partial failure rolls back.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	if db == nil || db.db == nil {
		return nil, ErrClosed
	}
	return db.db.ExecContext(ctx, query, args...)
}

func (db *DB) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	if db == nil || db.db == nil {
		return nil, ErrClosed
	}
	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (db *DB) QueryRow(ctx context.Context, query string, args ...any) Row {
	if db == nil || db.db == nil {
		return errorRow{ErrClosed}
	}
	return db.db.QueryRowContext(ctx, query, args...)
}

// Queryer is the read/write surface repositories implement against. Both *DB and
// Tx satisfy it, so one repository works inside or outside a transaction.
type Queryer interface {
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

var _ Queryer = (*DB)(nil)

// Result mirrors database/sql.Result without exposing database/sql.
type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

// Row mirrors *sql.Row. Scan reports sql.ErrNoRows for an empty result; use
// IsNotFound to test for it without importing database/sql.
type Row interface {
	Scan(dest ...any) error
}

// Rows mirrors *sql.Rows.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

// NullString scans nullable text without importing database/sql. It is a valid
// sql.Scanner by method shape.
type NullString struct {
	String string
	Valid  bool
}

func (n *NullString) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		n.String, n.Valid = "", false
		return nil
	case string:
		n.String, n.Valid = v, true
		return nil
	case []byte:
		n.String, n.Valid = string(v), true
		return nil
	default:
		return fmt.Errorf("storage: cannot scan %T into NullString", value)
	}
}

// NullTime stores a nullable RFC3339Nano UTC timestamp.
type NullTime struct {
	Time  time.Time
	Valid bool
}

func (n *NullTime) Scan(value any) error {
	var text NullString
	if err := text.Scan(value); err != nil {
		return err
	}
	if !text.Valid || text.String == "" {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	parsed, err := DecodeTime(text.String)
	if err != nil {
		return err
	}
	n.Time, n.Valid = parsed, true
	return nil
}

// EncodeTime stores a canonical UTC RFC3339Nano timestamp. Zero becomes NULL.
func EncodeTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// EncodeNullString maps the empty string to SQL NULL where an absent value is
// meaningful; callers that need to distinguish empty from NULL should not use it.
func EncodeNullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// DecodeTime parses a stored timestamp. Empty text yields the zero time.
func DecodeTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: parse timestamp: %w", err)
	}
	return parsed.UTC(), nil
}
