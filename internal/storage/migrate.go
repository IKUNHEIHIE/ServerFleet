package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Migration is one ordered schema change owned by a domain. Version must be
// globally unique and namespaced, for example "ds/0001_nodes". Each statement is
// expected to be a single SQL statement; multiple statements in one string are
// rejected by SQLite drivers in inconsistent ways.
type Migration struct {
	Version    string
	Name       string
	Statements []string
}

// MigrationReport lists versions applied by this call (existing ones are skipped).
type MigrationReport struct {
	Applied []string
	Skipped []string
}

// Migrator is an immutable, ordered migration set. It never mutates the database
// outside an immediate transaction and refuses to run when an applied version is
// missing from the registry (downgrade) or its checksum changed (edited history).
type Migrator struct {
	migrations []Migration
	byVersion  map[string]Migration
}

// NewMigrator validates and sorts the union of one or more domain migration sets.
func NewMigrator(sets ...[]Migration) (*Migrator, error) {
	byVersion := map[string]Migration{}
	all := make([]Migration, 0)
	for _, set := range sets {
		for _, migration := range set {
			if err := validateMigration(migration); err != nil {
				return nil, err
			}
			if _, exists := byVersion[migration.Version]; exists {
				return nil, fmt.Errorf("storage: duplicate migration version %q", migration.Version)
			}
			byVersion[migration.Version] = migration
			all = append(all, migration)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Version < all[j].Version })
	return &Migrator{migrations: all, byVersion: byVersion}, nil
}

func validateMigration(migration Migration) error {
	version := migration.Version
	if version == "" || strings.TrimSpace(version) != version {
		return fmt.Errorf("%w: migration version is required", ErrInvalidConfig)
	}
	slash := strings.Index(version, "/")
	if slash <= 0 || slash == len(version)-1 {
		return fmt.Errorf("%w: migration version %q must be namespaced like ds/0001_name", ErrInvalidConfig, version)
	}
	for _, r := range version {
		if !(r == '/' || r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("%w: migration version %q has unsupported characters", ErrInvalidConfig, version)
		}
	}
	if strings.TrimSpace(migration.Name) == "" {
		return fmt.Errorf("%w: migration %q needs a name", ErrInvalidConfig, version)
	}
	if len(migration.Statements) == 0 {
		return fmt.Errorf("%w: migration %q has no statements", ErrInvalidConfig, version)
	}
	for index, statement := range migration.Statements {
		if strings.TrimSpace(statement) == "" {
			return fmt.Errorf("%w: migration %q statement %d is empty", ErrInvalidConfig, version, index)
		}
	}
	return nil
}

// Versions returns the registered versions in apply order.
func (m *Migrator) Versions() []string {
	if m == nil {
		return nil
	}
	versions := make([]string, len(m.migrations))
	for index, migration := range m.migrations {
		versions[index] = migration.Version
	}
	return versions
}

const migrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	checksum TEXT NOT NULL,
	applied_at TEXT NOT NULL
)`

// Apply brings the database to the registry's schema version. It is safe to call
// on every start: applied migrations are verified and skipped.
func (m *Migrator) Apply(ctx context.Context, db *DB) (MigrationReport, error) {
	var report MigrationReport
	if m == nil {
		return report, fmt.Errorf("%w: migrator is required", ErrInvalidConfig)
	}
	if db == nil {
		return report, ErrClosed
	}
	if ctx == nil {
		return report, fmt.Errorf("%w: context is required", ErrInvalidConfig)
	}
	if _, err := db.Exec(ctx, migrationsTable); err != nil {
		return report, fmt.Errorf("storage: create migration ledger: %w", err)
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return report, err
	}
	for version, record := range applied {
		registered, ok := m.byVersion[version]
		if !ok {
			return report, fmt.Errorf("%w: %s", ErrUnknownMigration, version)
		}
		if registered.checksum() != record {
			return report, fmt.Errorf("%w: %s", ErrMigrationDrift, version)
		}
	}
	for _, migration := range m.migrations {
		if _, done := applied[migration.Version]; done {
			report.Skipped = append(report.Skipped, migration.Version)
			continue
		}
		ran, err := m.applyOne(ctx, db, migration)
		if err != nil {
			return report, err
		}
		if ran {
			report.Applied = append(report.Applied, migration.Version)
			continue
		}
		report.Skipped = append(report.Skipped, migration.Version)
	}
	return report, nil
}

func (m *Migrator) applyOne(ctx context.Context, db *DB, migration Migration) (bool, error) {
	applied := false
	err := db.InTx(ctx, func(txCtx context.Context, tx Tx) error {
		var existing string
		err := tx.QueryRow(txCtx, "SELECT checksum FROM schema_migrations WHERE version = ?", migration.Version).Scan(&existing)
		switch {
		case err == nil:
			if existing != migration.checksum() {
				return fmt.Errorf("%w: %s", ErrMigrationDrift, migration.Version)
			}
			return nil
		case !IsNotFound(err):
			return err
		}
		for index, statement := range migration.Statements {
			if _, err := tx.Exec(txCtx, statement); err != nil {
				return fmt.Errorf("storage: migration %s statement %d: %w", migration.Version, index, err)
			}
		}
		_, err = tx.Exec(txCtx,
			"INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
			migration.Version, migration.Name, migration.checksum(), nowUTC())
		if err != nil {
			return fmt.Errorf("storage: record migration %s: %w", migration.Version, err)
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func appliedMigrations(ctx context.Context, db *DB) (map[string]string, error) {
	rows, err := db.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("storage: read migration ledger: %w", err)
	}
	defer rows.Close()
	applied := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

// AppliedVersions returns the sorted migration versions recorded in the database.
func AppliedVersions(ctx context.Context, db *DB) ([]string, error) {
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(applied))
	for version := range applied {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions, nil
}

func (m Migration) checksum() string {
	hash := sha256.New()
	for _, statement := range m.Statements {
		hash.Write([]byte(statement))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// IsNotFound reports whether err is an empty-row result without importing
// database/sql in domain packages.
func IsNotFound(err error) bool {
	return errors.Is(err, errNoRows)
}

// VerifyApplied performs read-only ledger verification without creating tables
// or applying migrations. Offline bundle validation must not mutate a snapshot.
func (m *Migrator) VerifyApplied(ctx context.Context, db *DB, requireComplete bool) error {
	if m == nil || db == nil || ctx == nil {
		return ErrInvalidConfig
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	for version, checksum := range applied {
		registered, ok := m.byVersion[version]
		if !ok {
			return ErrUnknownMigration
		}
		if registered.checksum() != checksum {
			return ErrMigrationDrift
		}
	}
	if requireComplete && len(applied) != len(m.byVersion) {
		return ErrUnknownMigration
	}
	return nil
}
