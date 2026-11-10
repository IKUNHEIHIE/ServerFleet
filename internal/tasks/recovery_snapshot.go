package tasks

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

type RecoveryTaskRecord struct {
	ID, State string
	Expires   time.Time
}

func ReadRecoveryTasks(ctx context.Context, db storage.Queryer) ([]RecoveryTaskRecord, error) {
	rows, err := db.Query(ctx, `SELECT id,status,unstarted_deadline FROM tasks ORDER BY id LIMIT 100001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecoveryTaskRecord{}
	for rows.Next() {
		var record RecoveryTaskRecord
		var expires string
		if err := rows.Scan(&record.ID, &record.State, &expires); err != nil {
			return nil, err
		}
		parsed, err := storage.DecodeTime(expires)
		if err != nil {
			return nil, err
		}
		record.Expires = parsed
		out = append(out, record)
		if len(out) > 100000 {
			return nil, ErrInvalidTask
		}
	}
	return out, rows.Err()
}

// InvalidateRestoredTasksTx preserves terminal facts while ensuring NO task
// from an old backup is eligible for dispatch, automatic resume or old approval.
// This belongs only in the coordinator's offline quarantine transaction.
func InvalidateRestoredTasksTx(ctx context.Context, tx storage.Tx, now time.Time) error {
	if ctx == nil || tx == nil || now.IsZero() {
		return ErrInvalidTask
	}
	// In-flight targets remain unknown. Pending targets are also quarantined:
	// absence of an old snapshot receipt cannot prove no post-snapshot side effect.
	if _, err := tx.Exec(ctx, `UPDATE task_targets SET state='unknown',result_code='unknown',message='offline_restore_no_replay',finished_at=? WHERE task_id IN (SELECT id FROM tasks WHERE status IN ('queued','awaiting_approval','running','cancelling')) AND state IN ('pending','dispatched')`, storage.EncodeTime(now)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tasks SET status=CASE WHEN status IN ('queued','awaiting_approval') AND unstarted_deadline<=? THEN 'expired' ELSE 'unknown' END,cancel_requested=1,failure_reason='offline_restore_no_replay',updated_at=?,finished_at=? WHERE status IN ('queued','awaiting_approval','running','cancelling')`, storage.EncodeTime(now), storage.EncodeTime(now), storage.EncodeTime(now)); err != nil {
		return err
	}
	// Keep approval history, but make every old approval unusable. New revisions
	// require a new explicit task; no restored approval can authorize execution.
	_, err := tx.Exec(ctx, `UPDATE task_approvals SET decision='denied',reason='offline_restore_approval_invalidated' WHERE decision='approved'`)
	return err
}
