package ai

import (
	"context"
	"time"
)

// A typed extension of the sole audit transaction, not a second audit service.
// No migration number is assigned here. Composition registers this schema and
// calls AppendModelSettingsAudit from SessionSQLStatements.AppendModelAudit on
// the SAME DS Tx as the session aggregate and general audit event.
func ModelSettingsAuditSchemaStatements() []string {
	return []string{`CREATE TABLE cx_ai_model_audit(id INTEGER PRIMARY KEY AUTOINCREMENT,subject_id TEXT NOT NULL,profile_id TEXT NOT NULL,revision INTEGER NOT NULL,action TEXT NOT NULL,at TEXT NOT NULL,UNIQUE(subject_id,profile_id,revision,action))`}
}
func AppendModelSettingsAudit(ctx context.Context, exec func(context.Context, string, ...any) (int64, error), events []ModelSettingsAuditEvent) error {
	if ctx == nil || exec == nil || len(events) > 10000 {
		return ErrSessionUnavailable
	}
	for _, e := range events {
		if !exactID(e.SubjectID) || !exactID(e.ProfileID) || e.Revision == 0 || e.Action != "ai.models.update" || e.At.IsZero() {
			return ErrSessionUnavailable
		}
		rows, err := exec(ctx, `INSERT INTO cx_ai_model_audit(subject_id,profile_id,revision,action,at) VALUES(?,?,?,?,?)`, e.SubjectID, e.ProfileID, e.Revision, e.Action, e.At.UTC().Format(time.RFC3339Nano))
		if err != nil || rows != 1 {
			return ErrSessionUnavailable
		}
	}
	return ctx.Err()
}
