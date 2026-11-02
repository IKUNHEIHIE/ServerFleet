package certificates

import "serverfleet.local/serverfleet/internal/storage"

// CleanupClaimMigrations is the candidate DDL for atomic cleanup execution
// claims. The historical ds/0062 table cannot store a claim lease or an unknown
// provider outcome, so this migration rebuilds it instead of editing history.
// It is not registered centrally; the integration owner must accept the number
// before a real cleaner is enabled.
func CleanupClaimMigrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0063_cleanup_claim",
		Name:    "atomic ACME cleanup execution claims",
		Statements: []string{
			`CREATE TABLE certificate_dns_cleanup_claim (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				subject_id TEXT NOT NULL,
				request_id TEXT NOT NULL,
				plan_id TEXT NOT NULL DEFAULT '',
				domain TEXT NOT NULL,
				fqdn TEXT NOT NULL,
				record_id TEXT NOT NULL,
				record_value TEXT NOT NULL,
				state TEXT NOT NULL CHECK (state IN ('pending','retrying','resolved','canceled','failed','unknown')),
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				last_error_code TEXT NOT NULL DEFAULT '',
				next_attempt_at TEXT,
				claim_token TEXT NOT NULL DEFAULT '',
				lease_until TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE (fqdn, record_id)
			)`,
			`INSERT INTO certificate_dns_cleanup_claim
				(id, subject_id, request_id, plan_id, domain, fqdn, record_id, record_value, state,
				 attempts, last_error_code, next_attempt_at, claim_token, lease_until, created_at, updated_at)
				SELECT id, subject_id, request_id, plan_id, domain, fqdn, record_id, record_value, state,
					attempts, last_error_code, next_attempt_at, '', NULL, created_at, updated_at
				FROM certificate_dns_cleanup`,
			`DROP TABLE certificate_dns_cleanup`,
			`ALTER TABLE certificate_dns_cleanup_claim RENAME TO certificate_dns_cleanup`,
			`CREATE INDEX certificate_dns_cleanup_due ON certificate_dns_cleanup (state, next_attempt_at)`,
			`CREATE INDEX certificate_dns_cleanup_claim ON certificate_dns_cleanup (claim_token, lease_until)`,
		},
	}}
}
