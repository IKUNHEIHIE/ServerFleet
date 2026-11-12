package nodes

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the node inventory and ordinary group schema. Service group
// tables live in the servicegroups package.
func Migrations() []storage.Migration {
	return append([]storage.Migration{{
		Version: "ds/0020_nodes",
		Name:    "node inventory and ordinary groups",
		Statements: []string{
			`CREATE TABLE nodes (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				address TEXT NOT NULL DEFAULT '',
				region TEXT NOT NULL DEFAULT '',
				provider TEXT NOT NULL DEFAULT '',
				country_code TEXT NOT NULL DEFAULT '',
				tags TEXT NOT NULL DEFAULT '[]',
				status TEXT NOT NULL,
				last_seen_at TEXT,
				event_seq INTEGER NOT NULL DEFAULT 0,
				metrics_cpu REAL,
				metrics_memory REAL,
				metrics_observed_at TEXT,
				metrics_source TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX nodes_status ON nodes (status)`,
			`CREATE TABLE node_groups (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				notes TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL CHECK (revision > 0),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE UNIQUE INDEX node_groups_name ON node_groups (name COLLATE NOCASE)`,
			`CREATE TABLE node_group_members (
				group_id TEXT NOT NULL REFERENCES node_groups (id) ON DELETE CASCADE,
				node_id TEXT NOT NULL,
				PRIMARY KEY (group_id, node_id)
			)`,
			`CREATE INDEX node_group_members_node ON node_group_members (node_id)`,
		},
	}}, DiagnosticMigrations()...)
}
