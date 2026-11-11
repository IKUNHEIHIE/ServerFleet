// Package monitoring owns HTTP/TCP/TLS service probes, maintenance windows and
// up/down transitions. All network access is delegated to an injected Prober and
// all alerts are delegated to an injected Notifier.
package monitoring

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the monitoring schema.
func Migrations() []storage.Migration {
	return append([]storage.Migration{{
		Version: "ds/0050_monitoring",
		Name:    "monitors and probe checks",
		Statements: []string{
			`CREATE TABLE monitors (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				kind TEXT NOT NULL CHECK (kind IN ('http', 'tcp', 'tls')),
				target TEXT NOT NULL,
				interval_seconds INTEGER NOT NULL
					CHECK (interval_seconds >= 1 AND interval_seconds <= 604800),
				timeout_seconds INTEGER NOT NULL
					CHECK (timeout_seconds >= 1 AND timeout_seconds <= 300),
				enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
				maintenance_until TEXT,
				state TEXT NOT NULL DEFAULT 'unknown'
					CHECK (state IN ('unknown', 'up', 'down')),
				consecutive_failures INTEGER NOT NULL DEFAULT 0
					CHECK (consecutive_failures >= 0),
				last_checked_at TEXT,
				last_error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX monitors_due ON monitors (enabled, last_checked_at)`,
			`CREATE TABLE monitor_checks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				monitor_id TEXT NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
				status TEXT NOT NULL CHECK (status IN ('unknown', 'up', 'down')),
				latency_ns INTEGER CHECK (latency_ns IS NULL OR latency_ns >= 0),
				status_code INTEGER
					CHECK (status_code IS NULL OR (status_code >= 100 AND status_code <= 599)),
				error TEXT NOT NULL DEFAULT '',
				checked_at TEXT NOT NULL
			)`,
			`CREATE INDEX monitor_checks_monitor
				ON monitor_checks (monitor_id, checked_at DESC, id DESC)`,
		},
	}}, AutomationMigrations()...)
}
