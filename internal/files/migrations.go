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
	}, {
		// Allocated by the integration owner after the unchanged ds/0040.
		// Keep adopted SQL bytes intact: the ledger checks raw statement hashes.
		Version: "ds/0041_file_upload_task",
		Name:    "unified upload task binding and dispatch state",
		Statements: []string{
			`ALTER TABLE file_uploads ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX file_uploads_task ON file_uploads (task_id)`,
		},
	}, {
		// Candidate schema for V09-CLOUD-AM-02 multi-target distribution. The
		// final version number is allocated by the integration owner; the exact
		// SQL bytes are delivered as a precise candidate diff in the progress
		// docs and may be renumbered verbatim on adoption.
		Version: "am/0001_file_distribution",
		Name:    "frozen multi-target distribution and per-target results",
		Statements: []string{
			`CREATE TABLE file_distributions (
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
				task_id TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE file_distribution_targets (
				distribution_id TEXT NOT NULL REFERENCES file_distributions (id) ON DELETE CASCADE,
				sequence INTEGER NOT NULL,
				node_id TEXT NOT NULL,
				write_path TEXT NOT NULL,
				status TEXT NOT NULL,
				bytes INTEGER NOT NULL DEFAULT 0,
				sha256 TEXT NOT NULL DEFAULT '',
				error_code TEXT NOT NULL DEFAULT '',
				error_message TEXT NOT NULL DEFAULT '',
				updated_at TEXT NOT NULL,
				PRIMARY KEY (distribution_id, sequence)
			)`,
			`CREATE UNIQUE INDEX file_distribution_nodes ON file_distribution_targets (distribution_id, node_id)`,
			`CREATE INDEX file_distributions_task ON file_distributions (task_id)`,
		},
	}}
}
