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
