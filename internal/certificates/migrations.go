// Package certificates owns certificate metadata and internal PKI distribution
// records. Private keys never enter this package: only opaque secret-store key
// references are stored, and they are not exposed in read views.
package certificates

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the certificate and distribution schema. Certificate kind
// separates internal PKI records from public ACME records; distribution rows are
// internal-PKI only and reference no cross-domain table.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0061_certificates",
		Name:    "certificate metadata and internal distributions",
		Statements: []string{
			`CREATE TABLE certificates (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('internal', 'acme')),
				common_name TEXT NOT NULL,
				status TEXT NOT NULL
					CHECK (status IN ('pending', 'active', 'renewing', 'expired', 'revoked', 'failed')),
				issuer_ref TEXT NOT NULL DEFAULT '',
				key_ref TEXT NOT NULL,
				serial TEXT NOT NULL,
				not_before TEXT NOT NULL,
				not_after TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				CHECK (common_name NOT LIKE '%PRIVATE KEY%'),
				CHECK (key_ref NOT LIKE '%PRIVATE KEY%')
			)`,
			`CREATE INDEX certificates_kind ON certificates (kind, status)`,
			`CREATE TABLE certificate_distributions (
				id TEXT PRIMARY KEY,
				certificate_id TEXT NOT NULL REFERENCES certificates (id) ON DELETE CASCADE,
				service_group_id TEXT NOT NULL,
				node_id TEXT NOT NULL,
				status TEXT NOT NULL
					CHECK (status IN ('pending', 'submitted', 'active', 'failed', 'revoked')),
				task_id TEXT NOT NULL DEFAULT '',
				error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE (certificate_id, node_id)
			)`,
			`CREATE INDEX certificate_distributions_group ON certificate_distributions (service_group_id, node_id)`,
			`CREATE INDEX certificate_distributions_task ON certificate_distributions (task_id)`,
		},
	}}
}
