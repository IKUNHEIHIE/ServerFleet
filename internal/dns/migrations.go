// Package dns owns Cloudflare account references, zones and DNS records. Provider
// access goes through an injected port; this package never stores API tokens.
package dns

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the DNS schema. Account, zone and record rows are validated
// by this package rather than by cross-domain foreign keys.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0060_dns",
		Name:    "cloudflare account references, zones and records",
		Statements: []string{
			`CREATE TABLE dns_accounts (
				id TEXT PRIMARY KEY,
				provider_code TEXT NOT NULL DEFAULT 'cloudflare'
					CHECK (provider_code = 'cloudflare'),
				display_name TEXT NOT NULL,
				note TEXT NOT NULL DEFAULT '',
				enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
				is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
				credential_ref TEXT NOT NULL DEFAULT '',
				credential_status TEXT NOT NULL DEFAULT 'not_configured'
					CHECK (credential_status IN ('not_configured', 'pending_verification', 'valid', 'invalid', 'missing_permissions')),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE UNIQUE INDEX dns_accounts_default ON dns_accounts (provider_code)
				WHERE is_default = 1`,
			`CREATE TABLE dns_zones (
				id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				provider_zone_id TEXT NOT NULL,
				name TEXT NOT NULL COLLATE NOCASE,
				status TEXT NOT NULL DEFAULT 'unknown',
				synced_at TEXT NOT NULL,
				UNIQUE (account_id, provider_zone_id),
				UNIQUE (account_id, name)
			)`,
			`CREATE INDEX dns_zones_account ON dns_zones (account_id, name)`,
			`CREATE TABLE dns_records (
				id TEXT PRIMARY KEY,
				zone_id TEXT NOT NULL,
				type TEXT NOT NULL
					CHECK (type IN ('A', 'AAAA', 'CNAME', 'TXT', 'MX')),
				name TEXT NOT NULL COLLATE NOCASE,
				content TEXT NOT NULL,
				ttl INTEGER NOT NULL DEFAULT 0 CHECK (ttl >= 0 AND ttl <= 86400),
				proxied INTEGER NOT NULL DEFAULT 0 CHECK (proxied IN (0, 1)),
				priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0 AND priority <= 65535),
				modified_at TEXT NOT NULL,
				UNIQUE (zone_id, type, name, content)
			)`,
			`CREATE INDEX dns_records_zone ON dns_records (zone_id, name)`,
		},
	}}
}
