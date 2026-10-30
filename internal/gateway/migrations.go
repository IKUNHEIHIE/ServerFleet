package gateway

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the gateway schema. Node and service group rows are never
// copied here; their existence and revisions are checked through owner ports.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0070_gateway",
		Name:    "managed gateways, routes and provisioning",
		Statements: []string{
			`CREATE TABLE gateway_nodes (
				id TEXT PRIMARY KEY,
				node_id TEXT NOT NULL,
				name TEXT NOT NULL,
				region TEXT NOT NULL DEFAULT '',
				country_code TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL CHECK (status IN ('pending','installing','verifying','ready','failed','maintenance')),
				nginx_installed INTEGER NOT NULL DEFAULT 0 CHECK (nginx_installed IN (0,1)),
				checks_passed INTEGER NOT NULL DEFAULT 0 CHECK (checks_passed IN (0,1)),
				enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
				last_error TEXT NOT NULL DEFAULT '',
				nginx_version TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL CHECK (revision > 0),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX gateway_nodes_node ON gateway_nodes (node_id)`,
			`CREATE INDEX gateway_nodes_status ON gateway_nodes (status, enabled)`,
			`CREATE TABLE gateway_routes (
				id TEXT PRIMARY KEY,
				gateway_id TEXT NOT NULL REFERENCES gateway_nodes (id) ON DELETE CASCADE,
				domain TEXT NOT NULL,
				upstream TEXT NOT NULL DEFAULT '',
				service_group_id TEXT NOT NULL DEFAULT '',
				service_group_revision INTEGER NOT NULL DEFAULT 0 CHECK (service_group_revision >= 0),
				https INTEGER NOT NULL DEFAULT 0 CHECK (https IN (0,1)),
				enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
				revision INTEGER NOT NULL CHECK (revision > 0),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				CHECK ((upstream <> '') + (service_group_id <> '') = 1)
			)`,
			`CREATE UNIQUE INDEX gateway_routes_domain ON gateway_routes (domain)`,
			`CREATE INDEX gateway_routes_gateway ON gateway_routes (gateway_id, domain)`,
			`CREATE TABLE gateway_provisions (
				id TEXT PRIMARY KEY,
				gateway_id TEXT NOT NULL REFERENCES gateway_nodes (id) ON DELETE CASCADE,
				task_id TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed')),
				steps TEXT NOT NULL DEFAULT '[]',
				error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				started_at TEXT,
				finished_at TEXT
			)`,
			`CREATE UNIQUE INDEX gateway_provisions_task ON gateway_provisions (task_id)`,
			`CREATE UNIQUE INDEX gateway_provisions_active ON gateway_provisions (gateway_id)
				WHERE status IN ('queued','running')`,
			`CREATE INDEX gateway_provisions_gateway ON gateway_provisions (gateway_id, created_at DESC)`,
		},
	}}
}
