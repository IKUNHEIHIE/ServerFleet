package monitoring

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
)

func NodeProbeMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0087_node_probe_sources", Name: "node source monitoring grants and task receipt provenance", Statements: []string{
		`ALTER TABLE monitor_automation ADD COLUMN source_node_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_automation ADD COLUMN last_probe_unknown INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE monitor_automation ADD COLUMN last_probe_task_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_checks ADD COLUMN source_node_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_checks ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE monitor_probe_runs(run_key TEXT PRIMARY KEY,task_id TEXT NOT NULL DEFAULT '',payload BLOB NOT NULL CHECK(length(payload)<=16384))`,
		`CREATE UNIQUE INDEX monitor_probe_task_id ON monitor_probe_runs(task_id) WHERE task_id<>''`,
	}}}
}

// ProbeSnapshot is a trusted composition port, never exposed without HTTP auth.
func (s *Service) ProbeSnapshot(ctx context.Context, id, owner string, revision, observation uint64, source string) (Monitor, error) {
	m, e := s.repo.Get(ctx, id)
	if e != nil {
		return m, e
	}
	if m.AutomationOwnerID != owner || m.AutomationRevision != revision || m.ObservationRevision != observation || m.SourceNodeID != source {
		return m, ErrObservationChanged
	}
	if s.automationCurrent != nil {
		if e = s.automationCurrent(ctx, m); e != nil {
			return m, e
		}
	}
	return m, nil
}
func (s *Service) validateSource(ctx context.Context, actor, node string) error {
	if node == "" {
		return nil
	}
	if s.sourceValidator == nil || s.remoteProber == nil {
		return ErrProberUnavailable
	}
	return s.sourceValidator(ctx, actor, node)
}
