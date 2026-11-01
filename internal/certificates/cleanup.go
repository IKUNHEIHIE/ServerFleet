package certificates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

var (
	ErrCleanupNotFound    = errors.New("certificates: cleanup record not found")
	ErrCleanupState       = errors.New("certificates: cleanup state does not allow this action")
	ErrCleanupExhausted   = errors.New("certificates: cleanup retry budget exhausted")
	ErrCleanupUnavailable = errors.New("certificates: cleanup executor is not configured")
)

// CleanupState is the durable retry state of one DNS-01 challenge record.
type CleanupState string

const (
	CleanupPending  CleanupState = "pending"
	CleanupRetrying CleanupState = "retrying"
	CleanupResolved CleanupState = "resolved"
	CleanupCanceled CleanupState = "canceled"
	CleanupFailed   CleanupState = "failed"
)

// CleanupRecord is the durable identity and retry state of a challenge record.
// It never contains provider credentials.
type CleanupRecord struct {
	ID            int64
	SubjectID     string
	RequestID     string
	PlanID        string
	Domain        string
	FQDN          string
	RecordID      string
	RecordValue   string
	State         CleanupState
	Attempts      int
	LastErrorCode string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DNSRecordCleaner deletes exactly one journal-owned challenge record. The
// implementation must verify the expected value/name and use the current
// authorization; it must not resemble a generic provider API.
type DNSRecordCleaner interface {
	DeleteChallengeRecord(ctx context.Context, subjectID, requestID, fqdn, recordID, expectedValue string) error
}

type CleanupJournalOptions struct {
	DB          *storage.DB
	Audit       AuditSink
	Authorizer  auth.Authorizer
	Cleaner     DNSRecordCleaner
	Now         func() time.Time
	MaxAttempts int
	Backoff     time.Duration
}

type CleanupJournal struct {
	db          *storage.DB
	audit       AuditSink
	authorizer  auth.Authorizer
	cleaner     DNSRecordCleaner
	now         func() time.Time
	maxAttempts int
	backoff     time.Duration
}

func NewCleanupJournal(options CleanupJournalOptions) (*CleanupJournal, error) {
	if options.DB == nil || options.Audit == nil || options.Authorizer == nil {
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if maxAttempts > 20 {
		maxAttempts = 20
	}
	backoff := options.Backoff
	if backoff <= 0 {
		backoff = time.Minute
	}
	return &CleanupJournal{
		db: options.DB, audit: options.Audit, authorizer: options.Authorizer, cleaner: options.Cleaner,
		now: now, maxAttempts: maxAttempts, backoff: backoff,
	}, nil
}

// RecordFailure persists the cleanup identities returned by ACME for a failed
// issuance. Repeated identical records are idempotent and remain pending.
func (j *CleanupJournal) RecordFailure(ctx context.Context, subjectID, requestID, planID, domain string, records []ACMEDNS01Record) ([]CleanupRecord, error) {
	if j == nil {
		return nil, ErrCleanupUnavailable
	}
	if !validCleanupText(subjectID, 128) || !validCleanupText(requestID, 128) || !validCleanupText(domain, 253) {
		return nil, ErrInvalidInput
	}
	if len(records) == 0 || len(records) > 16 {
		return nil, ErrInvalidInput
	}
	if err := j.authorize(ctx, subjectID, ActionCleanupRecord, requestID, "cleanup_record"); err != nil {
		return nil, err
	}
	now := j.now().UTC()
	var saved []CleanupRecord
	err := j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		for _, record := range records {
			if !validCleanupText(record.FQDN, 255) || !validCleanupText(record.RecordID, 256) ||
				!validCleanupText(record.Value, 255) {
				return ErrInvalidInput
			}
			_, err := tx.Exec(txCtx, `INSERT INTO certificate_dns_cleanup
				(subject_id, request_id, plan_id, domain, fqdn, record_id, record_value,
				 state, attempts, last_error_code, next_attempt_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '', ?, ?, ?)
				ON CONFLICT (fqdn, record_id) DO NOTHING`,
				subjectID, requestID, planID, domain, record.FQDN, record.RecordID, record.Value,
				string(CleanupPending), storage.EncodeTime(now), storage.EncodeTime(now), storage.EncodeTime(now))
			if err != nil {
				return err
			}
			row := tx.QueryRow(txCtx, cleanupSelect+" WHERE fqdn = ? AND record_id = ?", record.FQDN, record.RecordID)
			stored, err := scanCleanup(row)
			if err != nil {
				return err
			}
			saved = append(saved, stored)
		}
		return j.audit.AppendTx(txCtx, tx, j.event(subjectID, ActionCleanupRecord, "success", requestID, "cleanup_recorded"))
	})
	return saved, err
}

// List returns cleanup records visible to the current subject.
func (j *CleanupJournal) List(ctx context.Context, subjectID string) ([]CleanupRecord, error) {
	if j == nil {
		return nil, ErrCleanupUnavailable
	}
	if err := j.authorize(ctx, subjectID, ActionCleanupRead, "", "cleanup_read"); err != nil {
		return nil, err
	}
	rows, err := j.db.Query(ctx, cleanupSelect+" WHERE subject_id = ? ORDER BY created_at DESC, id DESC", subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]CleanupRecord, 0)
	for rows.Next() {
		record, err := scanCleanup(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// Retry runs one admin-authorized cleanup attempt. The provider call happens
// inside the current authorization window and is bounded by MaxAttempts.
func (j *CleanupJournal) Retry(ctx context.Context, subjectID, requestID string, id int64) (CleanupRecord, error) {
	if j == nil {
		return CleanupRecord{}, ErrCleanupUnavailable
	}
	if j.cleaner == nil {
		return CleanupRecord{}, ErrCleanupUnavailable
	}
	if err := j.authorize(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_retry"); err != nil {
		return CleanupRecord{}, err
	}
	record, err := j.get(ctx, id)
	if err != nil {
		return CleanupRecord{}, err
	}
	if record.SubjectID != subjectID {
		return CleanupRecord{}, ErrCleanupNotFound
	}
	switch record.State {
	case CleanupResolved, CleanupCanceled:
		return CleanupRecord{}, ErrCleanupState
	case CleanupFailed:
		return CleanupRecord{}, ErrCleanupExhausted
	}
	if record.Attempts >= j.maxAttempts {
		_ = j.setFinal(ctx, subjectID, requestID, record, CleanupFailed, "retry_budget_exhausted")
		return CleanupRecord{}, ErrCleanupExhausted
	}
	return j.execute(ctx, subjectID, requestID, record, false)
}

// Cancel stops further automatic cleanup. The record is not deleted; a later
// explicit admin action may still retry it only if it was not resolved.
func (j *CleanupJournal) Cancel(ctx context.Context, subjectID, requestID string, id int64) (CleanupRecord, error) {
	if j == nil {
		return CleanupRecord{}, ErrCleanupUnavailable
	}
	if err := j.authorize(ctx, subjectID, ActionCleanupCancel, requestID, "cleanup_cancel"); err != nil {
		return CleanupRecord{}, err
	}
	record, err := j.get(ctx, id)
	if err != nil {
		return CleanupRecord{}, err
	}
	if record.SubjectID != subjectID {
		return CleanupRecord{}, ErrCleanupNotFound
	}
	if record.State == CleanupResolved {
		return CleanupRecord{}, ErrCleanupState
	}
	if err := j.setState(ctx, subjectID, requestID, record, CleanupCanceled, "canceled", false); err != nil {
		return CleanupRecord{}, err
	}
	return j.get(ctx, id)
}

// RetryDue runs at most limit due attempts using the dedicated cleanup service
// subject. The composition must grant that subject certificates.cleanup.retry.
func (j *CleanupJournal) RetryDue(ctx context.Context, limit int) (int, error) {
	if j == nil || j.cleaner == nil {
		return 0, ErrCleanupUnavailable
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	now := j.now().UTC()
	rows, err := j.db.Query(ctx, cleanupSelect+` WHERE state IN (?, ?) AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY COALESCE(next_attempt_at, created_at), id LIMIT ?`,
		string(CleanupPending), string(CleanupRetrying), storage.EncodeTime(now), limit)
	if err != nil {
		return 0, err
	}
	var records []CleanupRecord
	for rows.Next() {
		record, err := scanCleanup(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	attempted := 0
	for _, record := range records {
		if record.Attempts >= j.maxAttempts {
			_ = j.setFinal(ctx, record.SubjectID, "cleanup-service", record, CleanupFailed, "retry_budget_exhausted")
			continue
		}
		if err := j.authorize(ctx, "cleanup-service", ActionCleanupRetry, "cleanup-service", "cleanup_due"); err != nil {
			continue
		}
		if _, err := j.execute(ctx, record.SubjectID, "cleanup-service", record, true); err == nil {
			attempted++
		}
	}
	return attempted, nil
}

func (j *CleanupJournal) execute(ctx context.Context, subjectID, requestID string, record CleanupRecord, system bool) (CleanupRecord, error) {
	if err := j.setStateAt(ctx, subjectID, requestID, record, CleanupRetrying, "", time.Time{}, false, ActionCleanupRetry); err != nil {
		return CleanupRecord{}, err
	}
	cleanerSubject, cleanerRequest := subjectID, requestID
	if system {
		cleanerSubject, cleanerRequest = "cleanup-service", "cleanup-service"
	}
	err := j.cleaner.DeleteChallengeRecord(ctx, cleanerSubject, cleanerRequest, record.FQDN, record.RecordID, record.RecordValue)
	if err != nil {
		code := "provider_delete_failed"
		if ctx.Err() != nil {
			code = "canceled"
		}
		next := j.now().UTC().Add(j.backoff * time.Duration(record.Attempts+1))
		state := CleanupRetrying
		if record.Attempts+1 >= j.maxAttempts {
			state = CleanupFailed
			next = time.Time{}
		}
		if setErr := j.setStateAt(ctx, subjectID, requestID, record, state, code, next, true, ActionCleanupRetry); setErr != nil {
			return CleanupRecord{}, setErr
		}
		if system {
			return CleanupRecord{}, err
		}
		return j.get(ctx, record.ID)
	}
	if err := j.setStateAt(ctx, subjectID, requestID, record, CleanupResolved, "resolved", time.Time{}, true, ActionCleanupRetry); err != nil {
		return CleanupRecord{}, err
	}
	return j.get(ctx, record.ID)
}

func (j *CleanupJournal) setFinal(ctx context.Context, subjectID, requestID string, record CleanupRecord, state CleanupState, code string) error {
	return j.setStateAt(ctx, subjectID, requestID, record, state, code, time.Time{}, false, ActionCleanupRetry)
}

func (j *CleanupJournal) setState(ctx context.Context, subjectID, requestID string, record CleanupRecord, state CleanupState, code string, bump bool) error {
	return j.setStateAt(ctx, subjectID, requestID, record, state, code, time.Time{}, bump, ActionCleanupCancel)
}

func (j *CleanupJournal) setStateAt(ctx context.Context, subjectID, requestID string, record CleanupRecord, state CleanupState, code string, next time.Time, bump bool, action string) error {
	now := j.now().UTC()
	attempts := record.Attempts
	if bump {
		attempts++
	}
	err := j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup SET state = ?, attempts = ?, last_error_code = ?,
			next_attempt_at = ?, updated_at = ? WHERE id = ? AND subject_id = ?`,
			string(state), attempts, code, nullableCleanupTime(next), storage.EncodeTime(now), record.ID, record.SubjectID); err != nil {
			return err
		}
		result := "success"
		if state == CleanupFailed || state == CleanupRetrying {
			result = "failure"
		}
		return j.audit.AppendTx(txCtx, tx, j.event(subjectID, action, result, requestID, string(state)))
	})
	return err
}

func (j *CleanupJournal) get(ctx context.Context, id int64) (CleanupRecord, error) {
	record, err := scanCleanup(j.db.QueryRow(ctx, cleanupSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return CleanupRecord{}, ErrCleanupNotFound
	}
	return record, err
}

func (j *CleanupJournal) authorize(ctx context.Context, subjectID, action, requestID, reason string) error {
	if !validCleanupText(subjectID, 128) {
		return ErrInvalidInput
	}
	err := j.authorizer.Authorize(ctx, auth.Check{SubjectID: subjectID, Action: action})
	if err == nil {
		return nil
	}
	if isCleanupDenial(err) {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = j.audit.Append(auditCtx, j.event(subjectID, action, "denied", requestID, reason))
	}
	return err
}

func (j *CleanupJournal) event(subjectID, action, result, requestID, reason string) audit.Event {
	return audit.Event{SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		Source: audit.SourceWeb, ReasonCode: reason, At: j.now()}
}

const cleanupSelect = `SELECT id, subject_id, request_id, plan_id, domain, fqdn, record_id, record_value,
	state, attempts, last_error_code, next_attempt_at, created_at, updated_at FROM certificate_dns_cleanup`

func scanCleanup(row interface{ Scan(...any) error }) (CleanupRecord, error) {
	var (
		record                  CleanupRecord
		state, created, updated string
		nextTime                storage.NullString
	)
	if err := row.Scan(&record.ID, &record.SubjectID, &record.RequestID, &record.PlanID, &record.Domain,
		&record.FQDN, &record.RecordID, &record.RecordValue, &state, &record.Attempts,
		&record.LastErrorCode, &nextTime, &created, &updated); err != nil {
		return CleanupRecord{}, err
	}
	record.State = CleanupState(state)
	switch record.State {
	case CleanupPending, CleanupRetrying, CleanupResolved, CleanupCanceled, CleanupFailed:
	default:
		return CleanupRecord{}, fmt.Errorf("certificates: stored cleanup has invalid state")
	}
	var err error
	if record.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return CleanupRecord{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return CleanupRecord{}, err
	}
	if nextTime.Valid {
		if record.NextAttemptAt, err = storage.DecodeTime(nextTime.String); err != nil {
			return CleanupRecord{}, err
		}
	}
	return record, nil
}

func nullableCleanupTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func validCleanupText(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func isCleanupDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
