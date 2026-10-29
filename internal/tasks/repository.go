package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists tasks, targets, approvals, events and logs. It never authorizes
// callers; the Service layer validates and audits before using it.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("tasks: database is required")
	}
	return &Repo{db: db}, nil
}

const taskSelect = `SELECT id, name, submitter_id, action_id, operation_class, params,
	idempotency_key, revision, status, approval_required, plan_hash, batch_index,
	cancel_requested, target_type, target_resource_id, unstarted_deadline, request_id,
	failure_reason, created_at, updated_at, started_at, finished_at FROM tasks`

func (r *Repo) Create(ctx context.Context, q storage.Queryer, task Task, targets []Target) error {
	if len(targets) == 0 {
		return fmt.Errorf("%w: targets", ErrInvalidTask)
	}
	_, err := q.Exec(ctx, `INSERT INTO tasks
		(id, name, submitter_id, action_id, operation_class, params, idempotency_key,
		 revision, status, approval_required, plan_hash, batch_index, cancel_requested,
		 target_type, target_resource_id, unstarted_deadline, request_id, failure_reason,
		 created_at, updated_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, '', ?, ?, NULL, NULL)`,
		task.ID, task.Name, task.SubmitterID, task.ActionID, string(task.OperationClass),
		task.Params, task.IdempotencyKey, task.Revision, string(task.Status),
		boolInt(task.ApprovalRequired), task.PlanHash, task.BatchIndex, task.TargetType,
		task.TargetResourceID, encodeTime(task.UnstartedDeadline), task.RequestID,
		encodeTime(task.CreatedAt), encodeTime(task.UpdatedAt))
	if err != nil {
		return err
	}
	return insertTargets(ctx, q, task.ID, targets)
}

func insertTargets(ctx context.Context, q storage.Queryer, taskID string, targets []Target) error {
	for _, target := range targets {
		if _, err := q.Exec(ctx, `INSERT INTO task_targets
			(task_id, node_id, position, batch_index, state, result_code, message, dispatched_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
			taskID, target.NodeID, target.Position, target.BatchIndex, string(target.State),
			string(target.Code), target.Message); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) Get(ctx context.Context, id string) (Task, error) {
	return r.GetTx(ctx, r.db, id)
}

func (r *Repo) GetTx(ctx context.Context, q storage.Queryer, id string) (Task, error) {
	task, err := scanTask(q.QueryRow(ctx, taskSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, err
	}
	targets, err := TaskTargets(ctx, q, id)
	if err != nil {
		return Task{}, err
	}
	task.Targets = targets
	return task, nil
}

func (r *Repo) GetByIdempotencyKey(ctx context.Context, submitterID, key string) (Task, bool, error) {
	if key == "" {
		return Task{}, false, nil
	}
	task, err := scanTask(r.db.QueryRow(ctx, taskSelect+" WHERE submitter_id = ? AND idempotency_key = ?", submitterID, key))
	if storage.IsNotFound(err) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	targets, err := TaskTargets(ctx, r.db, task.ID)
	if err != nil {
		return Task{}, false, err
	}
	task.Targets = targets
	return task, true, nil
}

func TaskTargets(ctx context.Context, q storage.Queryer, taskID string) ([]Target, error) {
	rows, err := q.Query(ctx, `SELECT node_id, position, batch_index, state, result_code, message,
		dispatched_at, finished_at FROM task_targets WHERE task_id = ? ORDER BY batch_index, position`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]Target, 0)
	for rows.Next() {
		var (
			target      Target
			state, code string
			dispatched  storage.NullString
			finished    storage.NullString
		)
		if err := rows.Scan(&target.NodeID, &target.Position, &target.BatchIndex, &state, &code,
			&target.Message, &dispatched, &finished); err != nil {
			return nil, err
		}
		target.State = TargetState(state)
		if !target.State.Valid() {
			return nil, fmt.Errorf("tasks: stored target has invalid state %q", state)
		}
		target.Code = NodeResultCode(code)
		if code != "" && !target.Code.Valid() {
			return nil, fmt.Errorf("tasks: stored target has invalid code %q", code)
		}
		var err error
		if dispatched.Valid {
			if target.DispatchedAt, err = storage.DecodeTime(dispatched.String); err != nil {
				return nil, err
			}
		}
		if finished.Valid {
			if target.FinishedAt, err = storage.DecodeTime(finished.String); err != nil {
				return nil, err
			}
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s TargetState) Valid() bool {
	switch s {
	case TargetPending, TargetDispatched, TargetSucceeded, TargetFailed, TargetCanceled, TargetTimedOut, TargetUnknown:
		return true
	default:
		return false
	}
}

func (r *Repo) ListActive(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `SELECT id FROM tasks
		WHERE status IN (?, ?, ?) ORDER BY created_at, id LIMIT ?`,
		string(StatusQueued), string(StatusRunning), string(StatusCancelling), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repo) ListRunningOrCancelling(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, "SELECT id FROM tasks WHERE status IN (?, ?) ORDER BY created_at, id",
		string(StatusRunning), string(StatusCancelling))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Transition moves a task from one status to another. It returns false when the
// expected status no longer holds; no partial fields are updated.
func (r *Repo) Transition(ctx context.Context, q storage.Queryer, id string, from, to TaskStatus, reason string, started, finished time.Time) (bool, error) {
	outcome, err := q.Exec(ctx, `UPDATE tasks SET status = ?, failure_reason = ?, updated_at = ?,
		started_at = COALESCE(started_at, ?), finished_at = ?
		WHERE id = ? AND status = ?`,
		string(to), reason, encodeTime(time.Now().UTC()), encodeTime(started), encodeTime(finished),
		id, string(from))
	if err != nil {
		return false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *Repo) SetStarted(ctx context.Context, q storage.Queryer, id string) error {
	_, err := q.Exec(ctx, "UPDATE tasks SET started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ? AND started_at IS NULL",
		encodeTime(time.Now().UTC()), encodeTime(time.Now().UTC()), id)
	return err
}

func (r *Repo) SetBatchIndex(ctx context.Context, q storage.Queryer, id string, index int) error {
	_, err := q.Exec(ctx, "UPDATE tasks SET batch_index = ?, updated_at = ? WHERE id = ?",
		index, encodeTime(time.Now().UTC()), id)
	return err
}

func (r *Repo) SetCancelRequested(ctx context.Context, q storage.Queryer, id string) error {
	_, err := q.Exec(ctx, "UPDATE tasks SET cancel_requested = 1, updated_at = ? WHERE id = ?",
		encodeTime(time.Now().UTC()), id)
	return err
}

// Finalize writes a terminal task status exactly once. It reports false when the
// task was already finalized, so concurrent scheduler/cancel paths cannot flip a
// terminal state.
func (r *Repo) Finalize(ctx context.Context, q storage.Queryer, id string, status TaskStatus, reason string, finished time.Time) (bool, error) {
	if !status.Terminal() {
		return false, fmt.Errorf("%w: finalize status", ErrInvalidTask)
	}
	outcome, err := q.Exec(ctx, `UPDATE tasks SET status = ?, failure_reason = ?, finished_at = ?, updated_at = ?
		WHERE id = ? AND finished_at IS NULL`,
		string(status), reason, encodeTime(finished), encodeTime(time.Now().UTC()), id)
	if err != nil {
		return false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *Repo) MarkDispatched(ctx context.Context, q storage.Queryer, taskID, nodeID string) (bool, error) {
	outcome, err := q.Exec(ctx, `UPDATE task_targets SET state = ?, dispatched_at = ?
		WHERE task_id = ? AND node_id = ? AND state = ?`,
		string(TargetDispatched), encodeTime(time.Now().UTC()), taskID, nodeID, string(TargetPending))
	if err != nil {
		return false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// RecordTargetResult stores one terminal trusted result. A repeated identical
// result is an idempotent no-op; a different result after terminal is rejected.
func (r *Repo) RecordTargetResult(ctx context.Context, q storage.Queryer, taskID, nodeID string, code NodeResultCode, message string) (bool, error) {
	var currentState, currentCode string
	err := q.QueryRow(ctx, "SELECT state, result_code FROM task_targets WHERE task_id = ? AND node_id = ?",
		taskID, nodeID).Scan(&currentState, &currentCode)
	if storage.IsNotFound(err) {
		return false, ErrTargetNotFound
	}
	if err != nil {
		return false, err
	}
	state := TargetState(currentState)
	if state.Terminal() {
		if currentCode == string(code) {
			return false, nil
		}
		return false, ErrDuplicateResult
	}
	outcome, err := q.Exec(ctx, `UPDATE task_targets SET state = ?, result_code = ?, message = ?, finished_at = ?
		WHERE task_id = ? AND node_id = ? AND state IN (?, ?)`,
		string(code), string(code), message, encodeTime(time.Now().UTC()),
		taskID, nodeID, string(TargetPending), string(TargetDispatched))
	if err != nil {
		return false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *Repo) CancelPendingTargets(ctx context.Context, q storage.Queryer, taskID string) (int, error) {
	outcome, err := q.Exec(ctx, `UPDATE task_targets SET state = ?, message = 'canceled before dispatch', finished_at = ?
		WHERE task_id = ? AND state = ?`,
		string(TargetCanceled), encodeTime(time.Now().UTC()), taskID, string(TargetPending))
	if err != nil {
		return 0, err
	}
	affected, err := outcome.RowsAffected()
	return int(affected), err
}

func (r *Repo) InsertApproval(ctx context.Context, q storage.Queryer, approval Approval) error {
	_, err := q.Exec(ctx, `INSERT INTO task_approvals
		(task_id, revision, approver_id, decision, plan_hash, reason, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		approval.TaskID, approval.Revision, approval.ApproverID, approval.Decision,
		approval.PlanHash, approval.Reason, encodeTime(approval.DecidedAt))
	return err
}

func (r *Repo) Approval(ctx context.Context, q storage.Queryer, taskID string, revision uint64) (Approval, error) {
	var (
		approval    Approval
		revisionVal int64
		decided     string
	)
	err := q.QueryRow(ctx, `SELECT task_id, revision, approver_id, decision, plan_hash, reason, decided_at
		FROM task_approvals WHERE task_id = ? AND revision = ?`, taskID, revision).Scan(
		&approval.TaskID, &revisionVal, &approval.ApproverID, &approval.Decision,
		&approval.PlanHash, &approval.Reason, &decided)
	if storage.IsNotFound(err) {
		return Approval{}, ErrApprovalInvalid
	}
	if err != nil {
		return Approval{}, err
	}
	approval.Revision = uint64(revisionVal)
	approval.DecidedAt, err = storage.DecodeTime(decided)
	return approval, err
}

func (r *Repo) DeleteApprovals(ctx context.Context, q storage.Queryer, taskID string) error {
	_, err := q.Exec(ctx, "DELETE FROM task_approvals WHERE task_id = ?", taskID)
	return err
}

func (r *Repo) Revise(ctx context.Context, q storage.Queryer, task Task, targets []Target) error {
	_, err := q.Exec(ctx, `UPDATE tasks SET params = ?, revision = ?, status = ?, plan_hash = ?,
		batch_index = 0, unstarted_deadline = ?, updated_at = ? WHERE id = ?`,
		task.Params, task.Revision, string(task.Status), task.PlanHash,
		encodeTime(task.UnstartedDeadline), encodeTime(time.Now().UTC()), task.ID)
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, "DELETE FROM task_targets WHERE task_id = ?", task.ID); err != nil {
		return err
	}
	if err := insertTargets(ctx, q, task.ID, targets); err != nil {
		return err
	}
	return r.DeleteApprovals(ctx, q, task.ID)
}

func (r *Repo) AppendEvent(ctx context.Context, q storage.Queryer, taskID, eventType string, payload map[string]any, at time.Time) error {
	encoded := []byte("{}")
	if payload != nil {
		var err error
		encoded, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("tasks: encode event: %w", err)
		}
	}
	_, err := q.Exec(ctx, "INSERT INTO task_events (task_id, type, payload, at) VALUES (?, ?, ?, ?)",
		taskID, eventType, string(encoded), encodeTime(at))
	return err
}

// Events returns ordered events after afterSeq. A non-zero cursor older than the
// oldest retained event returns ErrCursorExpired instead of silently skipping.
func (r *Repo) Events(ctx context.Context, taskID string, afterSeq int64, limit int) ([]Event, error) {
	if afterSeq < 0 {
		return nil, fmt.Errorf("%w: cursor", ErrInvalidTask)
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		return nil, fmt.Errorf("%w: limit", ErrInvalidTask)
	}
	var minSeq *int64
	if err := r.db.QueryRow(ctx, "SELECT MIN(seq) FROM task_events WHERE task_id = ?", taskID).Scan(&minSeq); err != nil {
		if !storage.IsNotFound(err) {
			return nil, err
		}
	}
	if minSeq != nil && afterSeq > 0 && afterSeq < *minSeq-1 {
		return nil, ErrCursorExpired
	}
	rows, err := r.db.Query(ctx, `SELECT seq, task_id, type, payload, at FROM task_events
		WHERE task_id = ? AND seq > ? ORDER BY seq LIMIT ?`, taskID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var (
			event   Event
			payload string
			at      string
		)
		if err := rows.Scan(&event.Seq, &event.TaskID, &event.Type, &payload, &at); err != nil {
			return nil, err
		}
		event.Payload = []byte(payload)
		if event.At, err = storage.DecodeTime(at); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *Repo) AppendLog(ctx context.Context, q storage.Queryer, entry LogEntry) error {
	_, err := q.Exec(ctx, "INSERT INTO task_logs (task_id, node_id, level, message, at) VALUES (?, ?, ?, ?, ?)",
		entry.TaskID, entry.NodeID, entry.Level, entry.Message, encodeTime(entry.At))
	return err
}

func (r *Repo) Logs(ctx context.Context, taskID string, afterID int64, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		return nil, fmt.Errorf("%w: limit", ErrInvalidTask)
	}
	rows, err := r.db.Query(ctx, `SELECT id, task_id, node_id, level, message, at FROM task_logs
		WHERE task_id = ? AND id > ? ORDER BY id LIMIT ?`, taskID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]LogEntry, 0)
	for rows.Next() {
		var (
			entry LogEntry
			at    string
		)
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.NodeID, &entry.Level, &entry.Message, &at); err != nil {
			return nil, err
		}
		if entry.At, err = storage.DecodeTime(at); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var (
		task                            Task
		class, status                   string
		revision, batchIndex            int64
		approvalRequired, cancelRequest int
		deadline, created, updated      string
		started, finished               storage.NullString
	)
	if err := row.Scan(&task.ID, &task.Name, &task.SubmitterID, &task.ActionID, &class,
		&task.Params, &task.IdempotencyKey, &revision, &status, &approvalRequired,
		&task.PlanHash, &batchIndex, &cancelRequest, &task.TargetType, &task.TargetResourceID,
		&deadline, &task.RequestID, &task.FailureReason, &created, &updated, &started, &finished); err != nil {
		return Task{}, err
	}
	task.OperationClass = OperationClass(class)
	if !task.OperationClass.Valid() {
		return Task{}, fmt.Errorf("tasks: stored task has invalid class %q", class)
	}
	task.Status = TaskStatus(status)
	if !task.Status.Valid() {
		return Task{}, fmt.Errorf("tasks: stored task has invalid status %q", status)
	}
	if revision < 1 || batchIndex < 0 {
		return Task{}, fmt.Errorf("tasks: stored task has invalid revision/batch")
	}
	task.Revision = uint64(revision)
	task.BatchIndex = int(batchIndex)
	task.ApprovalRequired = approvalRequired != 0
	task.CancelRequested = cancelRequest != 0
	var err error
	if task.UnstartedDeadline, err = storage.DecodeTime(deadline); err != nil {
		return Task{}, err
	}
	if task.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Task{}, err
	}
	if task.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Task{}, err
	}
	if started.Valid {
		if task.StartedAt, err = storage.DecodeTime(started.String); err != nil {
			return Task{}, err
		}
	}
	if finished.Valid {
		if task.FinishedAt, err = storage.DecodeTime(finished.String); err != nil {
			return Task{}, err
		}
	}
	return task, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func encodeTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
