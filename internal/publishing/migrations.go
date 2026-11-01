package publishing

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the accepted publication plan schema. DNS, certificate and
// gateway state stay in their owning domains; this schema holds only
// orchestration state.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0080_publishing",
		Name:    "staged publication plans",
		Statements: []string{
			`CREATE TABLE publishing_plans (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				domain TEXT NOT NULL,
				service_group_id TEXT NOT NULL,
				expected_group_revision INTEGER NOT NULL CHECK (expected_group_revision > 0),
				status TEXT NOT NULL CHECK (status IN ('queued','running','completed','partial_failure','failed','canceled')),
				current_stage TEXT NOT NULL CHECK (current_stage IN ('dns','certificate','gateway')),
				created_by TEXT NOT NULL,
				idempotency_key TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE UNIQUE INDEX publishing_plans_idempotency
				ON publishing_plans (created_by, idempotency_key) WHERE idempotency_key <> ''`,
			`CREATE INDEX publishing_plans_status ON publishing_plans (status, updated_at)`,
			`CREATE TABLE publishing_stages (
				plan_id TEXT NOT NULL REFERENCES publishing_plans (id) ON DELETE CASCADE,
				stage TEXT NOT NULL CHECK (stage IN ('dns','certificate','gateway')),
				status TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed','skipped','rolled_back')),
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				result_summary TEXT NOT NULL DEFAULT '',
				error_code TEXT NOT NULL DEFAULT '',
				started_at TEXT,
				finished_at TEXT,
				PRIMARY KEY (plan_id, stage)
			)`,
			`CREATE INDEX publishing_stages_status ON publishing_stages (plan_id, status)`,
		},
	}}
}

// TargetMigrations is the candidate DDL patch for persisted publishing targets.
// It is not registered in Migrations until the integration owner accepts the
// migration number and updates the shared migration-count fixtures.
func TargetMigrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0081_publishing_targets",
		Name:    "publication target ids and target revision",
		Statements: []string{
			`ALTER TABLE publishing_plans ADD COLUMN gateway_ids TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE publishing_plans ADD COLUMN zone_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE publishing_plans ADD COLUMN certificate_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE publishing_plans ADD COLUMN https INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE publishing_plans ADD COLUMN target_revision INTEGER NOT NULL DEFAULT 1`,
		},
	}}
}
