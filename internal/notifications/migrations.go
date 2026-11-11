// Package notifications owns outbound SMTP and QQ Bot channels. It stores only
// opaque secret references: credential values live in the CX secret store and are
// never accepted, persisted, returned or audited by this package.
package notifications

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the notification schema. Provider calls are made through
// injected ports; this package performs no network I/O.
func Migrations() []storage.Migration {
	return append([]storage.Migration{{
		Version: "ds/0051_notifications",
		Name:    "notification channels, settings and deliveries",
		Statements: []string{
			`CREATE TABLE notification_channels (
				channel TEXT PRIMARY KEY CHECK (channel IN ('smtp', 'qqbot')),
				enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
				smtp_host TEXT NOT NULL DEFAULT '',
				smtp_port INTEGER NOT NULL DEFAULT 0
					CHECK (smtp_port >= 0 AND smtp_port <= 65535),
				smtp_security TEXT NOT NULL DEFAULT '',
				smtp_sender TEXT NOT NULL DEFAULT '',
				smtp_recipient TEXT NOT NULL DEFAULT '',
				qq_app_id TEXT NOT NULL DEFAULT '',
				qq_target_type TEXT NOT NULL DEFAULT '',
				qq_target_id TEXT NOT NULL DEFAULT '',
				secret_ref TEXT NOT NULL DEFAULT '',
				credential_status TEXT NOT NULL DEFAULT 'not_configured'
					CHECK (credential_status IN ('not_configured', 'configured', 'invalid')),
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE notification_settings (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				smtp_fallback_on_qq_failure INTEGER NOT NULL DEFAULT 1
					CHECK (smtp_fallback_on_qq_failure IN (0, 1)),
				max_retry_attempts INTEGER NOT NULL DEFAULT 2
					CHECK (max_retry_attempts >= 0 AND max_retry_attempts <= 5),
				deduplication_window_seconds INTEGER NOT NULL DEFAULT 300
					CHECK (deduplication_window_seconds >= 0 AND deduplication_window_seconds <= 86400),
				updated_at TEXT NOT NULL
			)`,
			`INSERT INTO notification_settings
				(id, smtp_fallback_on_qq_failure, max_retry_attempts,
				 deduplication_window_seconds, updated_at)
				VALUES (1, 1, 2, 300, '1970-01-01T00:00:00Z')`,
			`CREATE TABLE notification_deliveries (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				channel TEXT NOT NULL CHECK (channel IN ('smtp', 'qqbot')),
				status TEXT NOT NULL CHECK (status IN (
					'queued', 'submitted', 'provider_accepted', 'rejected',
					'rate_limited', 'failed', 'delivery_unknown')),
				provider_code TEXT NOT NULL DEFAULT '',
				message TEXT NOT NULL DEFAULT '',
				dedupe_key TEXT NOT NULL DEFAULT '',
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX notification_deliveries_dedupe
				ON notification_deliveries (dedupe_key, created_at DESC)
				WHERE dedupe_key <> ''`,
			`CREATE INDEX notification_deliveries_created
				ON notification_deliveries (created_at DESC, id DESC)`,
		},
	}}, TaskDeliveryMigrations()...)
}
