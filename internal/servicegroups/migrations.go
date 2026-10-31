// Package servicegroups owns the business service group resource: members,
// scheduling pause and dispatch eligibility. It is deliberately separate from
// ordinary node groups and never edits node inventory directly.
package servicegroups

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the service group schema. Member rows reference only the
// group table; node existence is validated through the nodes owner port.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0021_servicegroups",
		Name:    "service groups and members",
		Statements: []string{
			`CREATE TABLE service_groups (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL CHECK (revision > 0),
				service_port INTEGER NOT NULL DEFAULT 0,
				service_protocol TEXT NOT NULL DEFAULT '',
				health_check TEXT NOT NULL DEFAULT '',
				lb_enabled INTEGER NOT NULL DEFAULT 0,
				lb_strategy TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE UNIQUE INDEX service_groups_name ON service_groups (name COLLATE NOCASE)`,
			`CREATE TABLE service_group_members (
				group_id TEXT NOT NULL REFERENCES service_groups (id) ON DELETE CASCADE,
				node_id TEXT NOT NULL,
				paused INTEGER NOT NULL DEFAULT 0,
				pause_reason TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (group_id, node_id)
			)`,
			`CREATE INDEX service_group_members_node ON service_group_members (node_id)`,
		},
	}}
}
