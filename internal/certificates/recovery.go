package certificates

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// InvalidateRestoredCleanupTx prevents old pending DNS deletions from being
// retried against providers after an offline restore. Reconcile explicitly.
func InvalidateRestoredCleanupTx(ctx context.Context, tx storage.Tx, now time.Time) error {
	if ctx == nil || tx == nil || now.IsZero() {
		return ErrCleanupState
	}
	_, err := tx.Exec(ctx, `UPDATE certificate_dns_cleanup SET state='unknown',last_error_code='offline_restore_unknown',next_attempt_at=NULL,claim_token='',lease_until=NULL,updated_at=? WHERE state IN ('pending','retrying','failed')`, storage.EncodeTime(now))
	return err
}
