package certificates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	ErrCleanupBusy        = errors.New("certificates: cleanup attempt is already running")
	ErrCleanupRetryable   = errors.New("certificates: delete is proven unapplied and may be retried")
	ErrCleanupUnknown     = errors.New("certificates: cleanup outcome is unknown and needs reconciliation")
)

// CleanupState is the durable retry state of one DNS-01 challenge record.
type CleanupState string

const (
	CleanupPending  CleanupState = "pending"
	CleanupRetrying CleanupState = "retrying"
	CleanupResolved CleanupState = "resolved"
	CleanupCanceled CleanupState = "canceled"
	CleanupFailed   CleanupState = "failed"
	// CleanupUnknown means an attempt was claimed but its provider outcome was
	// never recorded (process death, lease expiry or cancellation during I/O).
	// It is never replayed automatically: only an explicit operator decision may
	// move it.
	CleanupUnknown CleanupState = "unknown"
)

// CleanupRecord is the durable identity and retry state of a challenge record.
// It never contains provider credentials. claimToken and leaseUntil are private
// execution bookkeeping; read projections do not expose them.
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
	claimToken    string
	leaseUntil    time.Time
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
	Lease       time.Duration
}

type CleanupJournal struct {
	db          *storage.DB
	audit       AuditSink
	authorizer  auth.Authorizer
	cleaner     DNSRecordCleaner
	now         func() time.Time
	maxAttempts int
	backoff     time.Duration
	lease       time.Duration
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
	lease := options.Lease
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	if lease > 15*time.Minute {
		lease = 15 * time.Minute
	}
	return &CleanupJournal{
		db: options.DB, audit: options.Audit, authorizer: options.Authorizer, cleaner: options.Cleaner,
		now: now, maxAttempts: maxAttempts, backoff: backoff, lease: lease,
	}, nil
}

// RecordFailure persists the cleanup identities returned by ACME for a failed
// issuance. Repeated identical records are idempotent and remain pending.
func (j *CleanupJournal) RecordFailure(ctx context.Context, subjectID, requestID, planID, domain string, records []ACMEDNS01Record) ([]CleanupRecord, error) {
	return j.RecordFailureOutcome(ctx, subjectID, requestID, planID, domain, records, false)
}

// RecordFailureOutcome retains proven-unapplied cleanup as pending, and an
// ambiguous provider deletion as UNKNOWN. Unknown effects are never retried.
func (j *CleanupJournal) RecordFailureOutcome(ctx context.Context, subjectID, requestID, planID, domain string, records []ACMEDNS01Record, unknown bool) ([]CleanupRecord, error) {
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
	var initialState = CleanupPending
	var nextAttempt any = storage.EncodeTime(now)
	if unknown {
		initialState = CleanupUnknown
		nextAttempt = nil
	}
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
				string(initialState), nextAttempt, storage.EncodeTime(now), storage.EncodeTime(now))
			if err != nil {
				return err
			}
			row := tx.QueryRow(txCtx, cleanupSelect+" WHERE fqdn = ? AND record_id = ?", record.FQDN, record.RecordID)
			stored, err := scanCleanup(row)
			if err != nil {
				return err
			}
			if stored.SubjectID != subjectID || stored.PlanID != planID || stored.RecordValue != record.Value || stored.Domain != domain {
				return ErrInvalidInput
			}
			if unknown && (stored.State == CleanupPending || stored.State == CleanupUnknown) {
				if _, err = tx.Exec(txCtx, `UPDATE certificate_dns_cleanup SET state='unknown',last_error_code='provider_outcome_unknown',next_attempt_at=NULL,updated_at=? WHERE id=? AND state IN ('pending','unknown')`, storage.EncodeTime(now), stored.ID); err != nil {
					return err
				}
				stored.State = CleanupUnknown
				stored.LastErrorCode = "provider_outcome_unknown"
			}
			saved = append(saved, stored)
		}
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, subjectID, ActionCleanupRecord, "success", requestID, "cleanup_recorded"))
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
	if err := j.retryable(record); err != nil {
		return CleanupRecord{}, err
	}
	now := j.now().UTC()
	if record.claimToken != "" {
		if record.leaseUntil.After(now) {
			j.auditDenied(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_busy")
			return CleanupRecord{}, ErrCleanupBusy
		}
		// A previous process died while holding the claim. The provider may or
		// may not have deleted the record, so never replay it automatically.
		if _, err := j.expireClaim(ctx, subjectID, requestID, record, "provider_outcome_unknown"); err != nil {
			return CleanupRecord{}, err
		}
		return CleanupRecord{}, ErrCleanupUnknown
	}
	if record.Attempts >= j.maxAttempts {
		if err := j.setFinal(ctx, subjectID, requestID, record, CleanupFailed, "retry_budget_exhausted", ActionCleanupRetry); err != nil {
			return CleanupRecord{}, err
		}
		return CleanupRecord{}, ErrCleanupExhausted
	}
	return j.execute(ctx, subjectID, requestID, record, false)
}

func (j *CleanupJournal) retryable(record CleanupRecord) error {
	switch record.State {
	case CleanupResolved, CleanupCanceled:
		return ErrCleanupState
	case CleanupFailed:
		return ErrCleanupExhausted
	case CleanupUnknown:
		return ErrCleanupUnknown
	case CleanupPending, CleanupRetrying:
		return nil
	default:
		return ErrCleanupState
	}
}

// Cancel stops further automatic cleanup. A cancel racing an in-flight attempt
// wins: the attempt's later completion may not rewrite the canceled state.
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
	if record.State == CleanupCanceled {
		return record, nil
	}
	now := j.now().UTC()
	applied := false
	err = j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		result, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, last_error_code = 'canceled', next_attempt_at = NULL,
				claim_token = '', lease_until = NULL, updated_at = ?
			WHERE id = ? AND subject_id = ? AND state IN ('pending','retrying','failed','unknown')`,
			string(CleanupCanceled), storage.EncodeTime(now), record.ID, record.SubjectID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		applied = true
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, subjectID, ActionCleanupCancel, "success", requestID, "cleanup_cancel_success"))
	})
	if err != nil {
		return CleanupRecord{}, err
	}
	if !applied {
		current, err := j.get(ctx, id)
		if err != nil {
			return CleanupRecord{}, err
		}
		if current.State == CleanupCanceled {
			return current, nil
		}
		return CleanupRecord{}, ErrCleanupState
	}
	return j.get(ctx, id)
}

// RetryDue runs at most limit due attempts on behalf of each original owner,
// rechecking current cleanup/DNS authorization. No synthetic principal is granted
// execution rights. Expired claims first become unknown and are never replayed.
type cleanupBackgroundKey struct{}

func (j *CleanupJournal) RetryDue(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		return 0, ErrCleanupUnavailable
	}
	ctx = context.WithValue(ctx, cleanupBackgroundKey{}, true)
	if j == nil || j.cleaner == nil {
		return 0, ErrCleanupUnavailable
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if _, err := j.RecoverStaleClaims(ctx, limit); err != nil {
		return 0, err
	}
	now := j.now().UTC()
	rows, err := j.db.Query(ctx, cleanupSelect+` WHERE state IN (?, ?) AND claim_token = ''
		AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
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
			_ = j.setFinal(ctx, record.SubjectID, fmt.Sprintf("cleanup-due-%d", record.ID), record, CleanupFailed, "retry_budget_exhausted", ActionCleanupRetry)
			continue
		}
		if err := j.authorize(ctx, record.SubjectID, ActionCleanupRetry, fmt.Sprintf("cleanup-due-%d", record.ID), "cleanup_due"); err != nil {
			continue
		}
		if _, err := j.execute(ctx, record.SubjectID, fmt.Sprintf("cleanup-due-%d", record.ID), record, true); err == nil {
			attempted++
		}
	}
	return attempted, nil
}

// RecoverStaleClaims marks every expired claim as unknown. It never calls the
// provider: a crashed process cannot prove whether its delete request reached
// the provider, so replaying would risk deleting a re-created record.
func (j *CleanupJournal) RecoverStaleClaims(ctx context.Context, limit int) (int, error) {
	if j == nil || ctx == nil {
		return 0, ErrCleanupUnavailable
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	// Internal lease fencing performs no provider I/O and must still quarantine
	// expired claims when the original owner has since lost permission.
	ctx = context.WithValue(ctx, cleanupBackgroundKey{}, true)
	now := j.now().UTC()
	rows, err := j.db.Query(ctx, cleanupSelect+` WHERE claim_token <> ''
		AND lease_until IS NOT NULL AND lease_until <= ? ORDER BY lease_until, id LIMIT ?`,
		storage.EncodeTime(now), limit)
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
	recovered := 0
	for _, record := range records {
		applied, err := j.expireClaim(ctx, "cleanup-service", "cleanup-service", record, "provider_outcome_unknown")
		if err != nil {
			return recovered, err
		}
		if applied {
			recovered++
		}
	}
	return recovered, nil
}

func (j *CleanupJournal) execute(ctx context.Context, subjectID, requestID string, record CleanupRecord, system bool) (CleanupRecord, error) {
	attempt, err := j.claim(ctx, subjectID, requestID, record)
	if err != nil {
		return CleanupRecord{}, err
	}
	if err := j.authorizeAttempt(ctx, subjectID, requestID, system); err != nil {
		j.releaseClaim(ctx, attempt, "authorization_revoked")
		return CleanupRecord{}, err
	}
	active, err := j.claimStillActive(ctx, attempt)
	if err != nil {
		j.releaseClaim(ctx, attempt, "claim_check_failed")
		return CleanupRecord{}, err
	}
	if !active {
		j.auditDenied(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_superseded")
		return CleanupRecord{}, ErrCleanupBusy
	}
	cleanerSubject, cleanerRequest := subjectID, requestID
	executionCtx := context.WithValue(ctx, cleanupExecutionKey{}, cleanupExecution{journal: j, attempt: attempt, system: system})
	deleteErr := j.cleaner.DeleteChallengeRecord(executionCtx, cleanerSubject, cleanerRequest, record.FQDN, record.RecordID, record.RecordValue)
	// The outcome is persisted even if authorization was revoked during I/O;
	// the second check only records the revocation instead of hiding it.
	j.authorizeAfterIO(ctx, subjectID, requestID, system)
	state, code, next, result := j.cleanupOutcome(attempt, deleteErr, ctx.Err())
	finished, applied, err := j.finishClaim(ctx, attempt, state, code, next, result)
	if err != nil {
		return CleanupRecord{}, err
	}
	if !applied {
		j.auditDenied(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_superseded")
		return finished, ErrCleanupState
	}
	if system && deleteErr != nil {
		return finished, deleteErr
	}
	return finished, nil
}

func (j *CleanupJournal) cleanupOutcome(attempt cleanupAttempt, deleteErr, ctxErr error) (CleanupState, string, time.Time, string) {
	if deleteErr == nil {
		return CleanupResolved, "resolved", time.Time{}, "success"
	}
	if ctxErr != nil || errors.Is(deleteErr, context.Canceled) || errors.Is(deleteErr, context.DeadlineExceeded) {
		return CleanupUnknown, "provider_outcome_unknown", time.Time{}, "failure"
	}
	if errors.Is(deleteErr, ErrCleanupNotFound) {
		return CleanupFailed, "record_identity_mismatch", time.Time{}, "failure"
	}
	if !errors.Is(deleteErr, ErrCleanupRetryable) {
		return CleanupUnknown, "provider_outcome_unknown", time.Time{}, "failure"
	}
	if attempt.attempts >= j.maxAttempts {
		return CleanupFailed, "provider_delete_failed", time.Time{}, "failure"
	}
	return CleanupRetrying, "provider_delete_failed", j.now().UTC().Add(j.backoff * time.Duration(attempt.attempts)), "failure"
}

type cleanupAttempt struct {
	token            string
	recordID         int64
	subjectID        string
	requestID        string
	previousState    CleanupState
	previousAttempts int
	previousNext     time.Time
	attempts         int
}

func (j *CleanupJournal) claim(ctx context.Context, subjectID, requestID string, record CleanupRecord) (cleanupAttempt, error) {
	token, err := newCleanupClaimToken()
	if err != nil {
		return cleanupAttempt{}, err
	}
	now := j.now().UTC()
	attempt := cleanupAttempt{
		token: token, recordID: record.ID, subjectID: subjectID, requestID: requestID,
		previousState: record.State, previousAttempts: record.Attempts, previousNext: record.NextAttemptAt,
		attempts: record.Attempts + 1,
	}
	applied := false
	err = j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		result, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, attempts = ?, claim_token = ?, lease_until = ?,
				last_error_code = '', next_attempt_at = NULL, updated_at = ?
			WHERE id = ? AND state IN ('pending','retrying') AND attempts = ?
				AND (claim_token = '' OR lease_until IS NULL OR lease_until <= ?)`,
			string(CleanupRetrying), attempt.attempts, token, storage.EncodeTime(now.Add(j.lease)),
			storage.EncodeTime(now), record.ID, record.Attempts, storage.EncodeTime(now))
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		applied = true
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, subjectID, ActionCleanupRetry, "success", requestID, "cleanup_claimed"))
	})
	if err != nil {
		return cleanupAttempt{}, err
	}
	if !applied {
		j.auditDenied(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_busy")
		return cleanupAttempt{}, j.claimConflict(ctx, record.ID)
	}
	return attempt, nil
}

func (j *CleanupJournal) claimConflict(ctx context.Context, id int64) error {
	current, err := j.get(ctx, id)
	if err != nil {
		return err
	}
	switch current.State {
	case CleanupResolved, CleanupCanceled:
		return ErrCleanupState
	case CleanupFailed:
		return ErrCleanupExhausted
	case CleanupUnknown:
		return ErrCleanupUnknown
	default:
		return ErrCleanupBusy
	}
}

func (j *CleanupJournal) claimStillActive(ctx context.Context, attempt cleanupAttempt) (bool, error) {
	var token, state string
	err := j.db.QueryRow(ctx, `SELECT claim_token, state FROM certificate_dns_cleanup WHERE id = ?`, attempt.recordID).
		Scan(&token, &state)
	if storage.IsNotFound(err) {
		return false, ErrCleanupNotFound
	}
	if err != nil {
		return false, err
	}
	return token == attempt.token && CleanupState(state) == CleanupRetrying, nil
}

// finishClaim persists the provider outcome only while this exact claim still
// owns the record. A cancel (or a stolen lease) therefore cannot be overwritten.
func (j *CleanupJournal) finishClaim(ctx context.Context, attempt cleanupAttempt, state CleanupState, code string, next time.Time, result string) (CleanupRecord, bool, error) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := j.now().UTC()
	applied := false
	err := j.db.InTx(writeCtx, func(txCtx context.Context, tx storage.Tx) error {
		update, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, attempts = ?, last_error_code = ?, next_attempt_at = ?,
				claim_token = '', lease_until = NULL, updated_at = ?
			WHERE id = ? AND state = ? AND claim_token = ?`,
			string(state), attempt.attempts, code, nullableCleanupTime(next), storage.EncodeTime(now),
			attempt.recordID, string(CleanupRetrying), attempt.token)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		applied = true
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, attempt.subjectID, ActionCleanupRetry, result, attempt.requestID, string(state)))
	})
	if err != nil {
		return CleanupRecord{}, false, err
	}
	record, err := j.get(writeCtx, attempt.recordID)
	if err != nil {
		return CleanupRecord{}, false, err
	}
	return record, applied, nil
}

func (j *CleanupJournal) releaseClaim(ctx context.Context, attempt cleanupAttempt, code string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := j.now().UTC()
	_ = j.db.InTx(writeCtx, func(txCtx context.Context, tx storage.Tx) error {
		update, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, attempts = ?, last_error_code = ?, next_attempt_at = ?,
				claim_token = '', lease_until = NULL, updated_at = ?
			WHERE id = ? AND state = ? AND claim_token = ?`,
			string(attempt.previousState), attempt.previousAttempts, code,
			nullableCleanupTime(attempt.previousNext), storage.EncodeTime(now),
			attempt.recordID, string(CleanupRetrying), attempt.token)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, attempt.subjectID, ActionCleanupRetry, "failure", attempt.requestID, code))
	})
}

// expireClaim converts one stale claim to unknown without contacting a provider.
func (j *CleanupJournal) expireClaim(ctx context.Context, subjectID, requestID string, record CleanupRecord, code string) (bool, error) {
	now := j.now().UTC()
	applied := false
	err := j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		update, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, last_error_code = ?, next_attempt_at = NULL,
				claim_token = '', lease_until = NULL, updated_at = ?
			WHERE id = ? AND state = ? AND claim_token = ?`,
			string(CleanupUnknown), code, storage.EncodeTime(now),
			record.ID, string(CleanupRetrying), record.claimToken)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		applied = true
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, subjectID, ActionCleanupRetry, "failure", requestID, code))
	})
	return applied, err
}

func (j *CleanupJournal) authorizeAttempt(ctx context.Context, subjectID, requestID string, system bool) error {
	return j.authorize(ctx, subjectID, ActionCleanupRetry, requestID, "cleanup_current_authorization")
}

func (j *CleanupJournal) authorizeAfterIO(ctx context.Context, subjectID, requestID string, system bool) {
	authCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = j.authorize(authCtx, subjectID, ActionCleanupRetry, requestID, "cleanup_post_io")
}

func (j *CleanupJournal) setFinal(ctx context.Context, subjectID, requestID string, record CleanupRecord, state CleanupState, code, action string) error {
	now := j.now().UTC()
	applied := false
	err := j.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		update, err := tx.Exec(txCtx, `UPDATE certificate_dns_cleanup
			SET state = ?, last_error_code = ?, next_attempt_at = NULL, updated_at = ?
			WHERE id = ? AND subject_id = ? AND claim_token = '' AND state IN ('pending','retrying')`,
			string(state), code, storage.EncodeTime(now), record.ID, record.SubjectID)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return nil
		}
		applied = true
		return j.audit.AppendTx(txCtx, tx, j.event(ctx, subjectID, action, "failure", requestID, code))
	})
	if err != nil {
		return err
	}
	if !applied {
		return ErrCleanupState
	}
	return nil
}

func (j *CleanupJournal) get(ctx context.Context, id int64) (CleanupRecord, error) {
	record, err := scanCleanup(j.db.QueryRow(ctx, cleanupSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return CleanupRecord{}, ErrCleanupNotFound
	}
	return record, err
}

func (j *CleanupJournal) auditDenied(ctx context.Context, subjectID, action, requestID, reason string) {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = j.audit.Append(auditCtx, j.event(ctx, subjectID, action, "denied", requestID, reason))
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
		_ = j.audit.Append(auditCtx, j.event(ctx, subjectID, action, "denied", requestID, reason))
	}
	return err
}

func (j *CleanupJournal) event(ctx context.Context, subjectID, action, result, requestID, reason string) audit.Event {
	source := audit.SourceWeb
	if background, _ := ctx.Value(cleanupBackgroundKey{}).(bool); background {
		source = audit.SourceSystem
	}
	return audit.Event{SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		Source: source, ReasonCode: reason, At: j.now()}
}

const cleanupSelect = `SELECT id, subject_id, request_id, plan_id, domain, fqdn, record_id, record_value,
	state, attempts, last_error_code, next_attempt_at, created_at, updated_at, claim_token, lease_until
	FROM certificate_dns_cleanup`

func scanCleanup(row interface{ Scan(...any) error }) (CleanupRecord, error) {
	var (
		record                  CleanupRecord
		state, created, updated string
		nextTime, leaseTime     storage.NullString
	)
	if err := row.Scan(&record.ID, &record.SubjectID, &record.RequestID, &record.PlanID, &record.Domain,
		&record.FQDN, &record.RecordID, &record.RecordValue, &state, &record.Attempts,
		&record.LastErrorCode, &nextTime, &created, &updated, &record.claimToken, &leaseTime); err != nil {
		return CleanupRecord{}, err
	}
	record.State = CleanupState(state)
	switch record.State {
	case CleanupPending, CleanupRetrying, CleanupResolved, CleanupCanceled, CleanupFailed, CleanupUnknown:
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
	if leaseTime.Valid {
		if record.leaseUntil, err = storage.DecodeTime(leaseTime.String); err != nil {
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

func newCleanupClaimToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("certificates: cleanup claim token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
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
