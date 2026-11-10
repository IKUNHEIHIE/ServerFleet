package files

import (
	"bytes"
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// ExecutionBinding is one immutable node/item reservation. Offer contains only
// generated task parameters, never chunks/secrets. Observation binds the original
// authenticated connection; reconnect cannot adopt or replay this reservation.
type ExecutionBinding struct {
	ExecutionID, TaskID, TransferID, SubjectID, NodeID, Direction, ParentPlan, Observation string
	Sequence                                                                               int
	Offer                                                                                  []byte
	State, Result, SHA256                                                                  string
	Size, Offset                                                                           uint64
	Complete                                                                               bool
}

// Candidate only: the integration owner registers this NEW immutable migration.
func ExecutionBindingsMigration() storage.Migration {
	return storage.Migration{Version: "cx/0008_file_execution_bindings", Name: "immutable per-item file execution reservations", Statements: []string{
		`CREATE TABLE cx_file_execution_bindings(execution_id TEXT PRIMARY KEY,task_id TEXT NOT NULL,transfer_id TEXT NOT NULL,node_id TEXT NOT NULL,item_sequence INTEGER NOT NULL,payload BLOB NOT NULL,UNIQUE(task_id,node_id,item_sequence))`,
		`CREATE INDEX cx_file_execution_task ON cx_file_execution_bindings(task_id)`,
	}}
}
func validExecutionBinding(b ExecutionBinding) bool {
	if b.Offset > b.Size || b.SHA256 != "" && !sha256Pattern.MatchString(b.SHA256) {
		return false
	}
	switch b.State {
	case "reserved", "offered", "accepted", "opened", "done":
	default:
		return false
	}
	switch b.Result {
	case "", "EXECUTION_RESULT_SUCCEEDED", "EXECUTION_RESULT_FAILED", "EXECUTION_RESULT_CANCELED", "EXECUTION_RESULT_TIMED_OUT", "EXECUTION_RESULT_UNKNOWN":
	default:
		return false
	}
	if (b.State == "done") != (b.Result != "") {
		return false
	}
	return validChannelID(b.ExecutionID) && validChannelID(b.TaskID) && validChannelID(b.TransferID) && validChannelID(b.SubjectID) && validChannelID(b.NodeID) && sha256Pattern.MatchString(b.ParentPlan) && (b.Direction == "read" || b.Direction == "write") && b.Sequence >= 0 && b.Sequence < 16 && len(b.Observation) > 0 && len(b.Observation) < 2048 && len(b.Offer) > 0 && len(b.Offer) <= 8192 && b.Size <= DefaultChannelFileBytes
}
func (r *Repo) ReserveExecution(ctx context.Context, b ExecutionBinding, sink AuditSink) error {
	if !validExecutionBinding(b) || b.State != "reserved" || sink == nil {
		return ErrTransferExecution
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return r.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO cx_file_execution_bindings(execution_id,task_id,transfer_id,node_id,item_sequence,payload) VALUES(?,?,?,?,?,?)`, b.ExecutionID, b.TaskID, b.TransferID, b.NodeID, b.Sequence, raw); err != nil {
			return err
		}
		return sink.AppendTx(ctx, tx, bindingAudit(b, "reserved"))
	})
}
func bindingAudit(b ExecutionBinding, phase string) audit.Event {
	return audit.Event{SubjectID: b.SubjectID, Action: ActionTransferExecute, Result: "success", RequestID: b.ExecutionID, TaskID: b.TaskID, NodeIDs: []string{b.NodeID}, Source: audit.SourceSystem, ReasonCode: phase, At: time.Now().UTC()}
}
func readExecution(ctx context.Context, q storage.Queryer, id string) (ExecutionBinding, error) {
	var raw []byte
	var b ExecutionBinding
	if err := q.QueryRow(ctx, `SELECT payload FROM cx_file_execution_bindings WHERE execution_id=? AND length(payload)<=16384`, id).Scan(&raw); err != nil {
		return b, err
	}
	if json.Unmarshal(raw, &b) != nil || !validExecutionBinding(b) || b.ExecutionID != id {
		return b, ErrTransferExecution
	}
	return b, nil
}
func (r *Repo) Execution(ctx context.Context, id string) (ExecutionBinding, error) {
	return readExecution(ctx, r.db, id)
}
func (r *Repo) ObserveExecution(ctx context.Context, id, phase string, sink AuditSink, fn func(*ExecutionBinding) error) error {
	if sink == nil || fn == nil {
		return ErrTransferExecution
	}
	return r.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		b, err := readExecution(ctx, tx, id)
		if err != nil {
			return err
		}
		before, _ := json.Marshal(b)
		immutable := b
		if err = fn(&b); err != nil {
			return err
		}
		if !validExecutionBinding(b) {
			return ErrTransferExecution
		}
		immutable.State = b.State
		immutable.Result = b.Result
		immutable.Size = b.Size
		immutable.Offset = b.Offset
		immutable.SHA256 = b.SHA256
		immutable.Complete = b.Complete
		fixed, _ := json.Marshal(immutable)
		after, _ := json.Marshal(b)
		if !bytes.Equal(fixed, after) {
			return ErrTransferIdentity
		}
		if bytes.Equal(before, after) {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE cx_file_execution_bindings SET payload=? WHERE execution_id=?`, after, id); err != nil {
			return err
		}
		return sink.AppendTx(ctx, tx, bindingAudit(b, phase))
	})
}

// ExecutionsForTask is a bounded owned read projection, not a caller-supplied
// SQL view. Metadata contains no transferred file bytes.
func (r *Repo) ExecutionsForTask(ctx context.Context, taskID string) ([]ExecutionBinding, error) {
	if !validChannelID(taskID) {
		return nil, ErrTransferIdentity
	}
	rows, err := r.db.Query(ctx, `SELECT execution_id FROM cx_file_execution_bindings WHERE task_id=? ORDER BY node_id,item_sequence LIMIT 257`, taskID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) > 256 {
		return nil, ErrTransferExecution
	}
	out := make([]ExecutionBinding, 0, len(ids))
	for _, id := range ids {
		binding, e := r.Execution(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, binding)
	}
	return out, nil
}
