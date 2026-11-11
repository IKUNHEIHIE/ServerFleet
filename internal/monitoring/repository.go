package monitoring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists monitors and probe checks. It never authorizes callers and never
// performs a probe.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("monitoring: database is required")
	}
	return &Repo{db: db}, nil
}

const monitorSelect = `SELECT id, name, kind, target, interval_seconds, timeout_seconds,
	enabled, maintenance_until, state, consecutive_failures, last_checked_at,
	last_error, created_at, updated_at FROM monitors`

func (r *Repo) Create(ctx context.Context, q storage.Queryer, monitor Monitor) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO monitors
		(id, name, kind, target, interval_seconds, timeout_seconds, enabled,
		 maintenance_until, state, consecutive_failures, last_checked_at, last_error,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		monitor.ID, monitor.Name, string(monitor.Kind), monitor.Target,
		monitor.IntervalSeconds, monitor.TimeoutSeconds, boolInt(monitor.Enabled),
		nullableTime(monitor.MaintenanceUntil), string(monitor.State),
		monitor.ConsecutiveFailures, nullableTime(monitor.LastCheckedAt),
		monitor.LastError, storage.EncodeTime(monitor.CreatedAt), storage.EncodeTime(monitor.UpdatedAt))
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return ErrMonitorExists
		}
		return err
	}
	_, err = q.Exec(ctx, `INSERT OR IGNORE INTO monitor_automation(monitor_id,owner_id,revision,observation_revision) VALUES(?,'',0,0)`, monitor.ID)
	return err
}

func (r *Repo) Get(ctx context.Context, id string) (Monitor, error) {
	if r == nil || r.db == nil {
		return Monitor{}, ErrServiceNotConfigured
	}
	monitor, err := scanMonitor(r.db.QueryRow(ctx, monitorSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Monitor{}, ErrMonitorNotFound
	}
	if err == nil {
		err = r.hydrateAutomation(ctx, &monitor)
	}
	return monitor, err
}

func (r *Repo) List(ctx context.Context) ([]Monitor, error) {
	if r == nil || r.db == nil {
		return nil, ErrServiceNotConfigured
	}
	rows, err := r.db.Query(ctx, monitorSelect+" ORDER BY id LIMIT 1001")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monitors := make([]Monitor, 0)
	for rows.Next() {
		monitor, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, monitor)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(monitors) > 1000 {
		return nil, ErrInvalidLimit
	}
	for i := range monitors {
		if err = r.hydrateAutomation(ctx, &monitors[i]); err != nil {
			return nil, err
		}
	}
	return monitors, nil
}

func (r *Repo) Update(ctx context.Context, q storage.Queryer, monitor Monitor) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	outcome, err := q.Exec(ctx, `UPDATE monitors SET
		name = ?, kind = ?, target = ?, interval_seconds = ?, timeout_seconds = ?,
		enabled = ?, maintenance_until = ?, updated_at = ? WHERE id = ?`,
		monitor.Name, string(monitor.Kind), monitor.Target, monitor.IntervalSeconds,
		monitor.TimeoutSeconds, boolInt(monitor.Enabled), nullableTime(monitor.MaintenanceUntil),
		storage.EncodeTime(monitor.UpdatedAt), monitor.ID)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrMonitorNotFound
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, q storage.Queryer, id string) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	outcome, err := q.Exec(ctx, "DELETE FROM monitors WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrMonitorNotFound
	}
	return nil
}

// RecordCheck inserts one sample and updates the derived monitor state in the
// caller transaction. Maintenance callers pass the unchanged state/failure count.
func (r *Repo) RecordCheck(ctx context.Context, q storage.Queryer, check Check, state State, failures int, lastError string, checkedAt time.Time) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	var latency, statusCode any
	if check.Latency != nil {
		latency = check.Latency.Nanoseconds()
	}
	if check.StatusCode != nil {
		statusCode = *check.StatusCode
	}
	if _, err := q.Exec(ctx, `INSERT INTO monitor_checks
		(monitor_id, status, latency_ns, status_code, error, checked_at,source_node_id,task_id)
		VALUES (?, ?, ?, ?, ?, ?,?,?)`,
		check.MonitorID, string(check.Status), latency, statusCode, check.Error,
		storage.EncodeTime(check.CheckedAt), check.SourceNodeID, check.TaskID); err != nil {
		return err
	}
	outcome, err := q.Exec(ctx, `UPDATE monitors SET
		state = ?, consecutive_failures = ?, last_checked_at = ?, last_error = ?,
		updated_at = ? WHERE id = ?`,
		string(state), failures, storage.EncodeTime(checkedAt), lastError,
		storage.EncodeTime(checkedAt), check.MonitorID)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrMonitorNotFound
	}
	return nil
}

func (r *Repo) ListChecks(ctx context.Context, monitorID string, limit int) ([]Check, error) {
	if r == nil || r.db == nil {
		return nil, ErrServiceNotConfigured
	}
	normalized, err := validateCheckLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, monitor_id, status, latency_ns, status_code,
		error, checked_at,source_node_id,task_id FROM monitor_checks WHERE monitor_id = ?
		ORDER BY checked_at DESC, id DESC LIMIT ?`, monitorID, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := make([]Check, 0, normalized)
	for rows.Next() {
		check, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func scanMonitor(row storage.Row) (Monitor, error) {
	var (
		monitor                     Monitor
		kind, state                 string
		enabled                     int
		maintenance, lastChecked    storage.NullString
		createdAt, updatedAt        string
		storedID, name, target      string
		interval, timeout, failures int
		lastError                   string
	)
	if err := row.Scan(&storedID, &name, &kind, &target, &interval, &timeout, &enabled,
		&maintenance, &state, &failures, &lastChecked, &lastError, &createdAt, &updatedAt); err != nil {
		return Monitor{}, err
	}
	monitor = Monitor{
		ID: storedID, Name: name, Kind: Kind(kind), Target: target,
		IntervalSeconds: interval, TimeoutSeconds: timeout, Enabled: enabled == 1,
		State: State(state), ConsecutiveFailures: failures, LastError: lastError,
	}
	if !monitor.Kind.Valid() || !monitor.State.Valid() ||
		monitor.IntervalSeconds < 1 || monitor.IntervalSeconds > 604800 ||
		monitor.TimeoutSeconds < 1 || monitor.TimeoutSeconds > 300 ||
		monitor.ConsecutiveFailures < 0 {
		return Monitor{}, fmt.Errorf("monitoring: stored monitor %q has invalid fields", storedID)
	}
	if maintenance.Valid {
		parsed, err := storage.DecodeTime(maintenance.String)
		if err != nil {
			return Monitor{}, err
		}
		monitor.MaintenanceUntil = parsed
	}
	if lastChecked.Valid {
		parsed, err := storage.DecodeTime(lastChecked.String)
		if err != nil {
			return Monitor{}, err
		}
		monitor.LastCheckedAt = parsed
	}
	created, err := storage.DecodeTime(createdAt)
	if err != nil {
		return Monitor{}, err
	}
	updated, err := storage.DecodeTime(updatedAt)
	if err != nil {
		return Monitor{}, err
	}
	monitor.CreatedAt = created
	monitor.UpdatedAt = updated
	return monitor, nil
}

func scanCheck(row storage.Row) (Check, error) {
	var (
		check      Check
		status     string
		latency    *int64
		statusCode *int64
		checkedAt  string
	)
	if err := row.Scan(&check.ID, &check.MonitorID, &status, &latency, &statusCode,
		&check.Error, &checkedAt, &check.SourceNodeID, &check.TaskID); err != nil {
		return Check{}, err
	}
	check.Status = State(status)
	if !check.Status.Valid() {
		return Check{}, fmt.Errorf("monitoring: stored check %d has invalid status", check.ID)
	}
	if latency != nil {
		if *latency < 0 {
			return Check{}, fmt.Errorf("monitoring: stored check %d has negative latency", check.ID)
		}
		duration := time.Duration(*latency)
		check.Latency = &duration
	}
	if statusCode != nil {
		code := int(*statusCode)
		check.StatusCode = &code
	}
	parsed, err := storage.DecodeTime(checkedAt)
	if err != nil {
		return Check{}, err
	}
	check.CheckedAt = parsed
	return check, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return storage.EncodeTime(value)
}
