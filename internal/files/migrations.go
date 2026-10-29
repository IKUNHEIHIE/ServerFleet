// Package files owns remote file browsing metadata, upload staging and node-to-node
// copy orchestration. Actual node I/O is executed only through injected transport
// ports backed by the CX controlled command channel; this package never opens SSH.
package files

import "serverfleet.local/serverfleet/internal/storage"

func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0040_files",
		Name:    "file transfers, uploads and item results",
		Statements: []string{
			`CREATE TABLE file_transfers (
				id TEXT PRIMARY KEY,
				task_id TEXT NOT NULL DEFAULT '',
				submitter_id TEXT NOT NULL,
				source_node_id TEXT NOT NULL,
				destination_node_id TEXT NOT NULL,
				destination_dir TEXT NOT NULL,
				conflict_policy TEXT NOT NULL,
				follow_symlinks INTEGER NOT NULL DEFAULT 0,
				dry_run INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL,
				progress REAL NOT NULL DEFAULT 0,
				total_bytes INTEGER,
				copied_bytes INTEGER NOT NULL DEFAULT 0,
				error_message TEXT NOT NULL DEFAULT '',
				request_id TEXT NOT NULL,
				idempotency_key TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				started_at TEXT,
				completed_at TEXT
			)`,
			`CREATE INDEX file_transfers_task ON file_transfers (task_id)`,
			`CREATE UNIQUE INDEX file_transfers_idempotency ON file_transfers (submitter_id, idempotency_key)
				WHERE idempotency_key <> ''`,
			`CREATE TABLE file_transfer_items (
				transfer_id TEXT NOT NULL REFERENCES file_transfers (id) ON DELETE CASCADE,
				sequence INTEGER NOT NULL,
				source_path TEXT NOT NULL,
				destination_path TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				bytes_copied INTEGER NOT NULL DEFAULT 0,
				error_code TEXT NOT NULL DEFAULT '',
				error_message TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (transfer_id, sequence)
			)`,
			`CREATE TABLE file_uploads (
				id TEXT PRIMARY KEY,
				subject_id TEXT NOT NULL,
				node_id TEXT NOT NULL,
				destination_path TEXT NOT NULL,
				status TEXT NOT NULL,
				bytes_received INTEGER NOT NULL DEFAULT 0,
				expected_size INTEGER,
				sha256_expected TEXT NOT NULL DEFAULT '',
				sha256_actual TEXT NOT NULL DEFAULT '',
				error_code TEXT NOT NULL DEFAULT '',
				staging_name TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
		},
	}}
}
