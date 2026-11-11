// Package quota owns traffic periods, per-node monthly quota policy, expiry
// estimates and reminder selection. Traffic values are accepted only from trusted
// callers and are never synthesized; exceeding a quota never blocks networking.
package quota

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the traffic, quota policy and reminder schema.
func Migrations() []storage.Migration {
	return append([]storage.Migration{{
		Version: "ds/0052_quota",
		Name:    "traffic periods, quota policy and reminders",
		Statements: []string{
			`CREATE TABLE traffic_periods (
				node_id TEXT NOT NULL,
				period_start TEXT NOT NULL,
				period_end TEXT NOT NULL,
				bytes_in REAL NOT NULL CHECK (bytes_in >= 0),
				bytes_out REAL NOT NULL CHECK (bytes_out >= 0),
				source TEXT NOT NULL CHECK (source IN (
					'agent_estimate', 'provider_billing', 'manual_correction')),
				updated_at TEXT NOT NULL,
				PRIMARY KEY (node_id, period_start)
			)`,
			`CREATE INDEX traffic_periods_node
				ON traffic_periods (node_id, period_start DESC)`,
			`CREATE TABLE traffic_samples (
				node_id TEXT NOT NULL,
				period_start TEXT NOT NULL,
				observed_at TEXT NOT NULL,
				bytes_in REAL NOT NULL CHECK (bytes_in >= 0),
				bytes_out REAL NOT NULL CHECK (bytes_out >= 0),
				source TEXT NOT NULL CHECK (source IN (
					'agent_estimate', 'provider_billing', 'manual_correction')),
				recorded_at TEXT NOT NULL,
				PRIMARY KEY (node_id, period_start, observed_at)
			)`,
			`CREATE INDEX traffic_samples_period
				ON traffic_samples (node_id, period_start, observed_at DESC)`,
			`CREATE TABLE quota_policies (
				node_id TEXT PRIMARY KEY,
				monthly_limit_bytes INTEGER NOT NULL CHECK (monthly_limit_bytes > 0),
				warning_percent INTEGER NOT NULL
					CHECK (warning_percent >= 1 AND warning_percent <= 100),
				expiry_at TEXT,
				estimated INTEGER NOT NULL DEFAULT 0 CHECK (estimated IN (0, 1)),
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE quota_reminders (
				dedupe_key TEXT PRIMARY KEY,
				node_id TEXT NOT NULL,
				period_start TEXT NOT NULL,
				threshold TEXT NOT NULL,
				sent_at TEXT NOT NULL
			)`,
		},
	}}, AutomationMigrations()...)
}
