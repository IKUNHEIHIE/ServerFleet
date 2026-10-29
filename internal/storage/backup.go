package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BackupTo writes a consistent standalone snapshot of the live database using
// VACUUM INTO. The destination must not exist; its parent directory is created.
// The snapshot is verified with quick_check before the call returns. WAL content
// visible to the snapshot transaction is included, so no separate -wal file is
// needed for recovery.
func (db *DB) BackupTo(ctx context.Context, destination string) error {
	if db == nil || db.db == nil {
		return ErrClosed
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("storage: resolve backup path: %w", err)
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("%w: %s", ErrBackupExists, abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("storage: create backup directory: %w", err)
	}
	if _, err := db.Exec(ctx, "VACUUM INTO ?", abs); err != nil {
		return fmt.Errorf("storage: backup: %w", err)
	}
	if err := VerifyFile(ctx, abs, true); err != nil {
		_ = os.Remove(abs)
		return fmt.Errorf("storage: verify backup: %w", err)
	}
	return nil
}

// VerifyFile opens a database file read-only and runs PRAGMA quick_check. When
// requireLedger is true the file must also contain the migration ledger, which
// rejects arbitrary SQLite files as restore candidates.
func VerifyFile(ctx context.Context, path string, requireLedger bool) error {
	handle, err := Open(ctx, Config{Path: path, ReadOnly: true})
	if err != nil {
		return err
	}
	defer handle.Close()
	if requireLedger {
		var count int
		if err := handle.QueryRow(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&count); err != nil {
			return fmt.Errorf("storage: inspect backup ledger: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("storage: backup has no migration ledger")
		}
	}
	return nil
}

// ChecksumFile returns the SHA-256 hex digest of a file.
func ChecksumFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// RestoreFile installs a verified standalone snapshot as a new database file.
// The caller must stop the Controller and move the live database aside first: this
// function refuses to overwrite an existing destination so a failed restore can
// never silently destroy the current data. The copy is verified before it is
// renamed into place.
func RestoreFile(ctx context.Context, snapshot, destination string) (err error) {
	srcAbs, err := filepath.Abs(snapshot)
	if err != nil {
		return fmt.Errorf("storage: resolve snapshot path: %w", err)
	}
	dstAbs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("storage: resolve restore path: %w", err)
	}
	if _, err := os.Stat(dstAbs); err == nil {
		return fmt.Errorf("%w: %s", ErrBackupExists, dstAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(srcAbs + "-wal"); err == nil {
		return fmt.Errorf("storage: snapshot has a WAL sidecar; use a VACUUM INTO backup")
	}
	if err := VerifyFile(ctx, srcAbs, true); err != nil {
		return fmt.Errorf("storage: verify snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o700); err != nil {
		return fmt.Errorf("storage: create restore directory: %w", err)
	}
	temp := dstAbs + ".restore-tmp"
	_ = os.Remove(temp)
	defer func() {
		if err != nil {
			_ = os.Remove(temp)
		}
	}()
	if err := copyFile(srcAbs, temp); err != nil {
		return err
	}
	if err := VerifyFile(ctx, temp, true); err != nil {
		return fmt.Errorf("storage: verify restored copy: %w", err)
	}
	if err := os.Rename(temp, dstAbs); err != nil {
		return fmt.Errorf("storage: install restored file: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
