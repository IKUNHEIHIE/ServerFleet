package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// ActionRetention is the audit action recorded before expired events are removed.
// The validator's action allowlist must include it for PurgeBefore to succeed.
const ActionRetention = "audit.retention"

var (
	ErrRetentionTooRecent = errors.New("audit: retention cutoff must be at least one hour old")
	ErrInvalidBatch       = errors.New("audit: invalid retention batch size")
)

// SQLRecorder is a durable append-only audit sink. It never exposes update or
// delete for individual events; retention is the only removal path and records
// itself first in the same transaction.
type SQLRecorder struct {
	db        *storage.DB
	validator *Validator
	now       func() time.Time
}

// NewSQLRecorder requires a migrated database and an explicit validator. A nil
// clock defaults to time.Now.
func NewSQLRecorder(db *storage.DB, validator *Validator, now func() time.Time) (*SQLRecorder, error) {
	if db == nil {
		return nil, errors.New("audit: database is required")
	}
	if validator == nil {
		return nil, ErrInvalidValidator
	}
	if now == nil {
		now = time.Now
	}
	return &SQLRecorder{db: db, validator: validator, now: now}, nil
}

// Migrations returns this domain's namespaced migration set.
func Migrations() []storage.Migration {
	return []storage.Migration{{
		Version: "ds/0010_audit",
		Name:    "append-only audit events",
		Statements: []string{
			`CREATE TABLE audit_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				subject_id TEXT NOT NULL,
				action TEXT NOT NULL,
				result TEXT NOT NULL,
				request_id TEXT NOT NULL,
				task_id TEXT NOT NULL DEFAULT '',
				node_ids TEXT NOT NULL DEFAULT '[]',
				source TEXT NOT NULL DEFAULT '',
				reason_code TEXT NOT NULL DEFAULT '',
				at TEXT NOT NULL
			)`,
			`CREATE INDEX audit_events_at ON audit_events (at DESC, id DESC)`,
			`CREATE INDEX audit_events_subject ON audit_events (subject_id, at DESC)`,
			`CREATE INDEX audit_events_task ON audit_events (task_id, at DESC)`,
			`CREATE INDEX audit_events_action ON audit_events (action, at DESC)`,
			`CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
				BEGIN SELECT RAISE(ABORT, 'audit events are append-only'); END`,
		},
	}}
}

// Append validates and writes one event in its own transaction.
func (r *SQLRecorder) Append(ctx context.Context, event Event) error {
	if r == nil || r.db == nil {
		return errors.New("audit: recorder is not configured")
	}
	return r.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		return r.AppendTx(txCtx, tx, event)
	})
}

// AppendTx writes the event inside the caller's transaction. Sensitive mutations
// must call this in the same transaction so an audit failure rolls them back.
// Events are append-only; the event ID is assigned by SQLite.
func (r *SQLRecorder) AppendTx(ctx context.Context, tx storage.Tx, event Event) error {
	if r == nil || r.validator == nil {
		return errors.New("audit: recorder is not configured")
	}
	if tx == nil {
		return errors.New("audit: transaction is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.validator.Validate(event, r.now()); err != nil {
		return err
	}
	nodes := event.NodeIDs
	if nodes == nil {
		nodes = []string{}
	}
	encodedNodes, err := json.Marshal(nodes)
	if err != nil {
		return fmt.Errorf("audit: encode nodes: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
		(subject_id, action, result, request_id, task_id, node_ids, source, reason_code, at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SubjectID, event.Action, event.Result, event.RequestID,
		event.TaskID, string(encodedNodes), string(event.Source), event.ReasonCode,
		event.At.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	return nil
}

// StoredPage is a page of fully attributed durable events. Unlike the legacy
// five-field EventView it includes task, node, source and reason metadata, all of
// which are validated structured fields rather than free-form payloads.
type StoredPage struct {
	Items   []Event
	Offset  int
	Limit   int
	Total   int
	HasMore bool
}

// Query returns a durable page ordered by time descending, then insertion order
// descending. It applies the same bounds as the in-memory View.
func (r *SQLRecorder) Query(ctx context.Context, q Query) (StoredPage, error) {
	if r == nil || r.db == nil {
		return StoredPage{}, errors.New("audit: recorder is not configured")
	}
	if q.Offset < 0 {
		return StoredPage{}, invalidQuery("offset must not be negative")
	}
	if q.Limit < 0 {
		return StoredPage{}, invalidQuery("limit must not be negative")
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return StoredPage{}, invalidQuery("limit exceeds maximum")
	}
	if !q.Since.IsZero() && !q.Until.IsZero() && q.Until.Before(q.Since) {
		return StoredPage{}, invalidQuery("until must not be before since")
	}
	if q.Source != "" && !knownSource(q.Source) {
		return StoredPage{}, invalidQuery("unknown source")
	}
	where, args := auditWhere(q)
	page := StoredPage{Offset: q.Offset, Limit: limit}
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&page.Total); err != nil {
		return StoredPage{}, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, subject_id, action, result, request_id, task_id,
		node_ids, source, reason_code, at FROM audit_events`+where+
		" ORDER BY at DESC, id DESC LIMIT ? OFFSET ?", append(args, limit, q.Offset)...)
	if err != nil {
		return StoredPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			event    Event
			eventID  int64
			nodeJSON string
			at       string
		)
		if err := rows.Scan(&eventID, &event.SubjectID, &event.Action, &event.Result, &event.RequestID,
			&event.TaskID, &nodeJSON, &event.Source, &event.ReasonCode, &at); err != nil {
			return StoredPage{}, err
		}
		event.ID = eventID
		nodes := []string{}
		if nodeJSON != "" {
			if err := json.Unmarshal([]byte(nodeJSON), &nodes); err != nil {
				return StoredPage{}, fmt.Errorf("audit: decode stored nodes: %w", err)
			}
		}
		event.NodeIDs = nodes
		parsed, err := storage.DecodeTime(at)
		if err != nil {
			return StoredPage{}, err
		}
		event.At = parsed
		page.Items = append(page.Items, event)
	}
	if err := rows.Err(); err != nil {
		return StoredPage{}, err
	}
	page.HasMore = q.Offset+len(page.Items) < page.Total
	return page, nil
}

func auditWhere(q Query) (string, []any) {
	clauses := make([]string, 0, 7)
	args := make([]any, 0, 7)
	add := func(column, value string) {
		if value == "" {
			return
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, value)
	}
	add("subject_id", q.SubjectID)
	add("action", q.Action)
	add("result", q.Result)
	add("request_id", q.RequestID)
	add("task_id", q.TaskID)
	add("source", string(q.Source))
	if q.ResourceID != "" {
		clauses = append(clauses, "(task_id = ? OR EXISTS (SELECT 1 FROM json_each(audit_events.node_ids) WHERE json_each.value = ?))")
		args = append(args, q.ResourceID, q.ResourceID)
	}
	if !q.Since.IsZero() {
		clauses = append(clauses, "at >= ?")
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	if !q.Until.IsZero() {
		clauses = append(clauses, "at < ?")
		args = append(args, q.Until.UTC().Format(time.RFC3339Nano))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	query := " WHERE "
	for index, clause := range clauses {
		if index > 0 {
			query += " AND "
		}
		query += clause
	}
	return query, args
}

// PurgeBefore removes events older than cutoff in bounded batches after writing a
// retention event. cutoff must be at least one hour in the past to prevent an
// accidental full wipe; batch sizes outside 1..10000 are rejected. The returned
// count includes only removed audit events, not the retention marker.
func (r *SQLRecorder) PurgeBefore(ctx context.Context, cutoff time.Time, actor, requestID string, batch int) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("audit: recorder is not configured")
	}
	if cutoff.IsZero() {
		return 0, fmt.Errorf("%w: cutoff is required", ErrInvalidQuery)
	}
	now := r.now()
	if cutoff.After(now.Add(-time.Hour)) {
		return 0, ErrRetentionTooRecent
	}
	if batch < 1 || batch > 10000 {
		return 0, ErrInvalidBatch
	}
	removed := 0
	err := r.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		marker := Event{
			SubjectID:  actor,
			Action:     ActionRetention,
			Result:     "success",
			RequestID:  requestID,
			Source:     SourceSystem,
			ReasonCode: "retention",
			At:         now,
		}
		if err := r.AppendTx(txCtx, tx, marker); err != nil {
			return err
		}
		for {
			if err := txCtx.Err(); err != nil {
				return err
			}
			outcome, err := tx.Exec(txCtx, `DELETE FROM audit_events WHERE id IN (
				SELECT id FROM audit_events WHERE at < ? ORDER BY id LIMIT ?)`,
				cutoff.UTC().Format(time.RFC3339Nano), batch)
			if err != nil {
				return err
			}
			affected, err := outcome.RowsAffected()
			if err != nil {
				return err
			}
			removed += int(affected)
			if affected < int64(batch) {
				return nil
			}
		}
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// Count returns the number of stored events.
func (r *SQLRecorder) Count(ctx context.Context) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("audit: recorder is not configured")
	}
	var count int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&count)
	return count, err
}

var _ Recorder = (*SQLRecorder)(nil)

var ErrEventNotFound = errors.New("audit: event not found")

// Get returns one durable event by its numeric id.
func (r *SQLRecorder) Get(ctx context.Context, id int64) (Event, error) {
	if r == nil || r.db == nil {
		return Event{}, errors.New("audit: recorder is not configured")
	}
	var (
		event    Event
		nodeJSON string
		at       string
	)
	err := r.db.QueryRow(ctx, `SELECT subject_id, action, result, request_id, task_id, node_ids,
		source, reason_code, at FROM audit_events WHERE id = ?`, id).Scan(
		&event.SubjectID, &event.Action, &event.Result, &event.RequestID, &event.TaskID,
		&nodeJSON, &event.Source, &event.ReasonCode, &at)
	if storage.IsNotFound(err) {
		return Event{}, ErrEventNotFound
	}
	if err != nil {
		return Event{}, err
	}
	nodes := []string{}
	if nodeJSON != "" {
		if err := json.Unmarshal([]byte(nodeJSON), &nodes); err != nil {
			return Event{}, fmt.Errorf("audit: decode stored nodes: %w", err)
		}
	}
	event.ID = id
	event.NodeIDs = nodes
	event.At, err = storage.DecodeTime(at)
	if err != nil {
		return Event{}, err
	}
	return event, nil
}
