package certificates

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
)

type cleanupExecutionKey struct{}
type cleanupExecution struct {
	journal *CleanupJournal
	attempt cleanupAttempt
	system  bool
}

// CurrentExecution exposes only the owned record after checking the SAME live
// journal claim, lease, current owner authorization and cancellation. The
// private token never leaves this package or becomes an HTTP/credential field.
func (j *CleanupJournal) CurrentExecution(ctx context.Context) (CleanupRecord, error) {
	if ctx == nil || ctx.Err() != nil || j == nil {
		return CleanupRecord{}, ErrCleanupUnavailable
	}
	execution, ok := ctx.Value(cleanupExecutionKey{}).(cleanupExecution)
	if !ok || execution.journal != j {
		return CleanupRecord{}, ErrCleanupUnavailable
	}
	active, err := j.claimStillActive(ctx, execution.attempt)
	if err != nil || !active {
		return CleanupRecord{}, ErrCleanupBusy
	}
	var lease string
	if err = j.db.QueryRow(ctx, `SELECT lease_until FROM certificate_dns_cleanup WHERE id=? AND claim_token=? AND state='retrying'`, execution.attempt.recordID, execution.attempt.token).Scan(&lease); err != nil {
		return CleanupRecord{}, ErrCleanupBusy
	}
	until, e := storage.DecodeTime(lease)
	if e != nil || !j.now().Before(until) {
		return CleanupRecord{}, ErrCleanupBusy
	}
	if err = j.authorizeAttempt(ctx, execution.attempt.subjectID, execution.attempt.requestID, execution.system); err != nil {
		return CleanupRecord{}, err
	}
	record, err := j.get(ctx, execution.attempt.recordID)
	if err != nil || record.SubjectID != execution.attempt.subjectID || record.State != CleanupRetrying {
		return CleanupRecord{}, ErrCleanupBusy
	}
	return record, nil
}
