package updates

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// Candidate statements only; the integration owner assigns a NEW migration.
// No current installation is fabricated from a browser or an old backup.
func InstallJournalSchemaStatements() []string {
	return []string{
		`CREATE TABLE cx_update_installed(component TEXT PRIMARY KEY,payload BLOB NOT NULL,sequence_floor INTEGER NOT NULL)`,
		`CREATE TABLE cx_update_receipts(execution_id TEXT PRIMARY KEY,task_id TEXT NOT NULL,subject_id TEXT NOT NULL,payload BLOB NOT NULL)`,
	}
}

type InstallAuditSink interface {
	AppendTx(context.Context, storage.Tx, audit.Event) error
}
type SQLInstallJournal struct {
	db      *storage.DB
	audit   InstallAuditSink
	current func(context.Context, string, string, string) error
}

func NewSQLInstallJournal(db *storage.DB, sink InstallAuditSink, current func(context.Context, string, string, string) error) (*SQLInstallJournal, error) {
	if db == nil || sink == nil || current == nil {
		return nil, ErrArtifactRejected
	}
	return &SQLInstallJournal{db, sink, current}, nil
}

// Commit is the InstallHooks adapter. Provenance, maximum ATTEMPTED sequence,
// receipt and audit share one transaction, including a known rollback. Missing
// installed provenance fails closed. A lost commit ack is never retried here.
func (j *SQLInstallJournal) Commit(ctx context.Context, subject, planHash string, r InstallReceipt) (string, error) {
	if j == nil || !installID(subject) || !validPlanDigest(planHash) || !installID(r.Intent.ExecutionID) || !installID(r.Intent.TaskID) || !installedManifest(r.Current) || !installedManifest(r.Attempted) || !installedManifest(r.Intent.Previous) || r.Attempted.Sequence <= r.Intent.Previous.Sequence || r.Current.Component != r.Attempted.Component || r.State != "installed" && r.State != "rolled_back" || r.State == "installed" && r.Current != r.Attempted || r.State == "rolled_back" && (!r.Intent.AllowRollback || r.Current != r.Intent.Previous) || !validHealth(r.Health, r.Current, r.Intent.ExecutionID, r.Health.StartedAt) {
		return "", ErrArtifactRejected
	}
	id := "update-" + r.Intent.ExecutionID
	if !installID(id) {
		return "", ErrArtifactRejected
	}
	err := j.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if j.current(ctx, r.Intent.TaskID, planHash, subject) != nil {
			return ErrArtifactRejected
		}
		var raw []byte
		var floor uint64
		var previous ReleaseManifest
		if tx.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed WHERE component=? AND length(payload)<=4096`, r.Current.Component).Scan(&raw, &floor) != nil || json.Unmarshal(raw, &previous) != nil || previous != r.Intent.Previous || floor < previous.Sequence || floor >= r.Attempted.Sequence {
			return ErrApplyUnknown
		}
		next, _ := json.Marshal(r.Current)
		r.AuditRecordID = id
		receipt, _ := json.Marshal(r)
		if _, e := tx.Exec(ctx, `INSERT INTO cx_update_receipts VALUES(?,?,?,?)`, r.Intent.ExecutionID, r.Intent.TaskID, subject, receipt); e != nil {
			return e
		}
		result, e := tx.Exec(ctx, `UPDATE cx_update_installed SET payload=?,sequence_floor=? WHERE component=? AND sequence_floor=?`, next, r.Attempted.Sequence, r.Current.Component, floor)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return ErrApplyUnknown
		}
		return j.audit.AppendTx(ctx, tx, audit.Event{SubjectID: subject, Action: ActionInstall, Result: "success", RequestID: id, TaskID: r.Intent.TaskID, Source: audit.SourceSystem, ReasonCode: r.State, At: time.Now().UTC()})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
