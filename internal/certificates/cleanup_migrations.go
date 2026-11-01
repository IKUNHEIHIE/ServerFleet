package certificates

import "serverfleet.local/serverfleet/internal/storage"

// CleanupMigrations is the candidate DDL for durable ACME DNS-01 cleanup
// records. It is not registered in Migrations until the integration owner
// accepts the migration number.
func CleanupMigrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0062_acme_cleanup",
		Name:    "durable ACME DNS-01 cleanup records",
		Statements: []string{
			`CREATE TABLE certificate_dns_cleanup (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				subject_id TEXT NOT NULL,
				request_id TEXT NOT NULL,
				plan_id TEXT NOT NULL DEFAULT '',
				domain TEXT NOT NULL,
				fqdn TEXT NOT NULL,
				record_id TEXT NOT NULL,
				record_value TEXT NOT NULL,
				state TEXT NOT NULL CHECK (state IN ('pending','retrying','resolved','canceled','failed')),
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				last_error_code TEXT NOT NULL DEFAULT '',
				next_attempt_at TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE (fqdn, record_id)
			)`,
			`CREATE INDEX certificate_dns_cleanup_due ON certificate_dns_cleanup (state, next_attempt_at)`,
		},
	}}
}
