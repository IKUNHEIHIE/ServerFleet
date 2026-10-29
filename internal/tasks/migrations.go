package tasks

import "serverfleet.local/serverfleet/internal/storage"

// Migrations returns the unified task pipeline schema: durable tasks, frozen
// target rows, revision-bound approvals, ordered events for SSE and redacted logs.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0030_tasks",
		Name:    "durable unified tasks",
		Statements: []string{
			`CREATE TABLE tasks (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				submitter_id TEXT NOT NULL,
				action_id TEXT NOT NULL,
				operation_class TEXT NOT NULL,
				params TEXT NOT NULL DEFAULT '{}',
				idempotency_key TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL CHECK (revision > 0),
				status TEXT NOT NULL,
				approval_required INTEGER NOT NULL DEFAULT 0,
				plan_hash TEXT NOT NULL,
				batch_index INTEGER NOT NULL DEFAULT 0,
				cancel_requested INTEGER NOT NULL DEFAULT 0,
				target_type TEXT NOT NULL DEFAULT 'node_ids',
				target_resource_id TEXT NOT NULL DEFAULT '',
				unstarted_deadline TEXT NOT NULL,
				request_id TEXT NOT NULL,
				failure_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				started_at TEXT,
				finished_at TEXT
			)`,
			`CREATE UNIQUE INDEX tasks_idempotency ON tasks (submitter_id, idempotency_key)
				WHERE idempotency_key <> ''`,
			`CREATE INDEX tasks_status ON tasks (status, created_at)`,
			`CREATE TABLE task_targets (
				task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
				node_id TEXT NOT NULL,
				position INTEGER NOT NULL,
				batch_index INTEGER NOT NULL,
				state TEXT NOT NULL,
				result_code TEXT NOT NULL DEFAULT '',
				message TEXT NOT NULL DEFAULT '',
				dispatched_at TEXT,
				finished_at TEXT,
				PRIMARY KEY (task_id, node_id)
			)`,
			`CREATE INDEX task_targets_batch ON task_targets (task_id, batch_index, position)`,
			`CREATE TABLE task_approvals (
				task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
				revision INTEGER NOT NULL,
				approver_id TEXT NOT NULL,
				decision TEXT NOT NULL,
				plan_hash TEXT NOT NULL,
				reason TEXT NOT NULL DEFAULT '',
				decided_at TEXT NOT NULL,
				PRIMARY KEY (task_id, revision)
			)`,
			`CREATE TABLE task_events (
				seq INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
				type TEXT NOT NULL,
				payload TEXT NOT NULL DEFAULT '{}',
				at TEXT NOT NULL
			)`,
			`CREATE INDEX task_events_task ON task_events (task_id, seq)`,
			`CREATE TABLE task_logs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
				node_id TEXT NOT NULL DEFAULT '',
				level TEXT NOT NULL,
				message TEXT NOT NULL,
				at TEXT NOT NULL
			)`,
			`CREATE INDEX task_logs_task ON task_logs (task_id, id)`,
		},
	}}
}
