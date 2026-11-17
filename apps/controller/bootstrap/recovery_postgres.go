package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
)

// Importing data is deliberately separate from activate: no copied receipt
// grants a running authority or replaces the continuous fence checks.
func restoreReconciledPostgres(ctx context.Context, directory, dsnFile string, stdout io.Writer) error {
	if !filepath.IsAbs(directory) {
		return recovery.ErrRecoveryRejected
	}
	var receipt recovery.ReconciliationReceipt
	if _, err := readOfflineRecord(filepath.Join(directory, "reconciled.json"), &receipt, 16<<10); err != nil {
		return err
	}
	path := filepath.Join(directory, "restored.db")
	source, err := storage.Open(ctx, storage.Config{Path: path, ReadOnly: true})
	if err != nil {
		return err
	}
	actual, err := recovery.ReadReconciliation(ctx, source)
	source.Close()
	if err != nil || actual != receipt {
		return recovery.ErrRecoveryRejected
	}
	dsn, err := storage.ReadDSNFile(dsnFile)
	if err != nil {
		return err
	}
	migrator, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		return err
	}
	if err = storage.RestorePostgres(ctx, path, storage.Config{Path: path, Driver: "postgres", DSN: dsn}, migrator); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"state": "postgres_imported_waiting_actual_fence", "released": false})
}
