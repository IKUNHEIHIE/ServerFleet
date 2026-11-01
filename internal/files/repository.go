package files

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("files: database is required")
	}
	return &Repo{db: db}, nil
}

const transferSelect = `SELECT id, task_id, submitter_id, source_node_id, destination_node_id,
	destination_dir, conflict_policy, follow_symlinks, dry_run, status, progress, total_bytes,
	copied_bytes, error_message, request_id, idempotency_key, created_at, updated_at, started_at, completed_at
	FROM file_transfers`

func (r *Repo) CreateTransfer(ctx context.Context, q storage.Queryer, transfer Transfer) error {
	_, err := q.Exec(ctx, `INSERT INTO file_transfers
		(id, task_id, submitter_id, source_node_id, destination_node_id, destination_dir,
		 conflict_policy, follow_symlinks, dry_run, status, progress, total_bytes, copied_bytes,
		 error_message, request_id, idempotency_key, created_at, updated_at, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, '', ?, ?, ?, ?, NULL, NULL)`,
		transfer.ID, transfer.TaskID, transfer.SubmitterID, transfer.SourceNodeID,
		transfer.DestinationNodeID, transfer.DestinationDir, string(transfer.ConflictPolicy),
		boolInt(transfer.FollowSymlinks), boolInt(transfer.DryRun), string(transfer.Status),
		transfer.Progress, transfer.RequestID, transfer.IdempotencyKey, encodeTime(transfer.CreatedAt), encodeTime(transfer.UpdatedAt))
	if err != nil {
		return err
	}
	for _, item := range transfer.Items {
		if _, err := q.Exec(ctx, `INSERT INTO file_transfer_items
			(transfer_id, sequence, source_path, destination_path, status, bytes_copied, error_code, error_message)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			transfer.ID, item.Sequence, item.SourcePath, item.DestinationPath, string(item.Status),
			item.BytesCopied, item.ErrorCode, item.ErrorMessage); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) GetTransfer(ctx context.Context, id string) (Transfer, error) {
	transfer, err := scanTransfer(r.db.QueryRow(ctx, transferSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Transfer{}, ErrTransferNotFound
	}
	if err != nil {
		return Transfer{}, err
	}
	items, err := transferItems(ctx, r.db, id)
	if err != nil {
		return Transfer{}, err
	}
	transfer.Items = items
	return transfer, nil
}

func (r *Repo) GetTransferByTask(ctx context.Context, taskID string) (Transfer, error) {
	transfer, err := scanTransfer(r.db.QueryRow(ctx, transferSelect+" WHERE task_id = ?", taskID))
	if storage.IsNotFound(err) {
		return Transfer{}, ErrTransferNotFound
	}
	if err != nil {
		return Transfer{}, err
	}
	items, err := transferItems(ctx, r.db, transfer.ID)
	if err != nil {
		return Transfer{}, err
	}
	transfer.Items = items
	return transfer, nil
}

func (r *Repo) GetTransferByIdempotency(ctx context.Context, submitterID, key string) (Transfer, bool, error) {
	if key == "" {
		return Transfer{}, false, nil
	}
	transfer, err := scanTransfer(r.db.QueryRow(ctx, transferSelect+" WHERE submitter_id = ? AND idempotency_key = ?", submitterID, key))
	if storage.IsNotFound(err) {
		return Transfer{}, false, nil
	}
	if err != nil {
		return Transfer{}, false, err
	}
	items, err := transferItems(ctx, r.db, transfer.ID)
	if err != nil {
		return Transfer{}, false, err
	}
	transfer.Items = items
	return transfer, true, nil
}

func (r *Repo) ListTransfers(ctx context.Context, limit int) ([]Transfer, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, transferSelect+" ORDER BY created_at DESC, id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	transfers := make([]Transfer, 0)
	for rows.Next() {
		transfer, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range transfers {
		items, err := transferItems(ctx, r.db, transfers[index].ID)
		if err != nil {
			return nil, err
		}
		transfers[index].Items = items
	}
	return transfers, nil
}

func (r *Repo) AttachTask(ctx context.Context, q storage.Queryer, id, taskID string, status TransferStatus) error {
	_, err := q.Exec(ctx, "UPDATE file_transfers SET task_id = ?, status = ?, updated_at = ? WHERE id = ?",
		taskID, string(status), encodeTime(time.Now().UTC()), id)
	return err
}

func (r *Repo) MarkTransferFailed(ctx context.Context, q storage.Queryer, id, message string) error {
	now := time.Now().UTC()
	_, err := q.Exec(ctx, `UPDATE file_transfers SET status = ?, error_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND completed_at IS NULL`, string(TransferFailed), message, encodeTime(now), encodeTime(now), id)
	return err
}

// ApplyTaskUpdate stores the trusted task-level result. The caller computes the
// consistent terminal status from item results; this method never upgrades a
// task answer into success by itself.
func (r *Repo) ApplyTaskUpdate(ctx context.Context, q storage.Queryer, id string, status TransferStatus, progress float64, total, copied *int64, message string, items []TransferItem, started, completed time.Time) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	_, err := q.Exec(ctx, `UPDATE file_transfers SET status = ?, progress = ?, total_bytes = COALESCE(?, total_bytes),
		copied_bytes = COALESCE(?, copied_bytes), error_message = ?, started_at = COALESCE(started_at, ?),
		completed_at = ?, updated_at = ? WHERE id = ?`,
		string(status), progress, nullableInt(total), nullableInt(copied), message,
		encodeTime(started), encodeTime(completed), encodeTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := q.Exec(ctx, `UPDATE file_transfer_items SET destination_path = ?, status = ?,
			bytes_copied = ?, error_code = ?, error_message = ? WHERE transfer_id = ? AND sequence = ?`,
			item.DestinationPath, string(item.Status), item.BytesCopied, item.ErrorCode,
			item.ErrorMessage, id, item.Sequence); err != nil {
			return err
		}
	}
	return nil
}

func transferItems(ctx context.Context, q storage.Queryer, transferID string) ([]TransferItem, error) {
	rows, err := q.Query(ctx, `SELECT sequence, source_path, destination_path, status, bytes_copied,
		error_code, error_message FROM file_transfer_items WHERE transfer_id = ? ORDER BY sequence`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TransferItem, 0)
	for rows.Next() {
		var (
			item   TransferItem
			status string
		)
		if err := rows.Scan(&item.Sequence, &item.SourcePath, &item.DestinationPath, &status,
			&item.BytesCopied, &item.ErrorCode, &item.ErrorMessage); err != nil {
			return nil, err
		}
		item.Status = ItemStatus(status)
		if !item.Status.Valid() {
			return nil, fmt.Errorf("files: stored item has invalid status %q", status)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanTransfer(row interface{ Scan(...any) error }) (Transfer, error) {
	var (
		transfer           Transfer
		policy, status     string
		follow, dry        int
		total              *int64
		created, updated   string
		started, completed storage.NullString
	)
	if err := row.Scan(&transfer.ID, &transfer.TaskID, &transfer.SubmitterID, &transfer.SourceNodeID,
		&transfer.DestinationNodeID, &transfer.DestinationDir, &policy, &follow, &dry, &status,
		&transfer.Progress, &total, &transfer.CopiedBytes, &transfer.ErrorMessage, &transfer.RequestID,
		&transfer.IdempotencyKey, &created, &updated, &started, &completed); err != nil {
		return Transfer{}, err
	}
	transfer.ConflictPolicy = ConflictPolicy(policy)
	if !transfer.ConflictPolicy.Valid() {
		return Transfer{}, fmt.Errorf("files: stored transfer has invalid conflict policy")
	}
	transfer.Status = TransferStatus(status)
	if !transfer.Status.Valid() {
		return Transfer{}, fmt.Errorf("files: stored transfer has invalid status")
	}
	transfer.FollowSymlinks = follow != 0
	transfer.DryRun = dry != 0
	transfer.TotalBytes = total
	var err error
	if transfer.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Transfer{}, err
	}
	if transfer.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Transfer{}, err
	}
	if started.Valid {
		if transfer.StartedAt, err = storage.DecodeTime(started.String); err != nil {
			return Transfer{}, err
		}
	}
	if completed.Valid {
		if transfer.CompletedAt, err = storage.DecodeTime(completed.String); err != nil {
			return Transfer{}, err
		}
	}
	return transfer, nil
}

func (r *Repo) CreateUpload(ctx context.Context, q storage.Queryer, upload Upload) error {
	_, err := q.Exec(ctx, `INSERT INTO file_uploads
		(id, subject_id, node_id, destination_path, status, bytes_received, expected_size,
		 sha256_expected, sha256_actual, error_code, staging_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', '', ?, ?, ?)`,
		upload.ID, upload.SubjectID, upload.NodeID, upload.DestinationPath, upload.Status,
		nullableInt(upload.ExpectedSize), upload.SHA256Expected, upload.StagingName,
		encodeTime(upload.CreatedAt), encodeTime(upload.UpdatedAt))
	return err
}

func (r *Repo) GetUpload(ctx context.Context, id string) (Upload, error) {
	var (
		upload           Upload
		expected         *int64
		created, updated string
	)
	err := r.db.QueryRow(ctx, `SELECT id, subject_id, node_id, destination_path, status, bytes_received,
		expected_size, sha256_expected, sha256_actual, error_code, staging_name, created_at, updated_at
		FROM file_uploads WHERE id = ?`, id).Scan(&upload.ID, &upload.SubjectID, &upload.NodeID,
		&upload.DestinationPath, &upload.Status, &upload.BytesReceived, &expected,
		&upload.SHA256Expected, &upload.SHA256Actual, &upload.ErrorCode, &upload.StagingName,
		&created, &updated)
	if storage.IsNotFound(err) {
		return Upload{}, ErrUploadNotFound
	}
	if err != nil {
		return Upload{}, err
	}
	upload.ExpectedSize = expected
	if upload.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Upload{}, err
	}
	if upload.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Upload{}, err
	}
	return upload, nil
}

func (r *Repo) UpdateUploadProgress(ctx context.Context, q storage.Queryer, id string, bytesReceived int64) error {
	_, err := q.Exec(ctx, "UPDATE file_uploads SET bytes_received = ?, updated_at = ? WHERE id = ?",
		bytesReceived, encodeTime(time.Now().UTC()), id)
	return err
}

func (r *Repo) UpdateUploadStatus(ctx context.Context, q storage.Queryer, id, status, actualSHA, errorCode string) error {
	_, err := q.Exec(ctx, `UPDATE file_uploads SET status = ?, sha256_actual = ?, error_code = ?, updated_at = ?
		WHERE id = ?`, status, actualSHA, errorCode, encodeTime(time.Now().UTC()), id)
	return err
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
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

// ClaimTransfer atomically moves one queued transfer into running. Only the
// winner may execute; a second caller gets executed=false and must not open a
// second byte channel.
func (r *Repo) ClaimTransfer(ctx context.Context, taskID string, now time.Time) (Transfer, bool, error) {
	result, err := r.db.Exec(ctx, `UPDATE file_transfers SET status = ?, started_at = COALESCE(started_at, ?),
		updated_at = ? WHERE task_id = ? AND status = ?`,
		string(TransferRunning), encodeTime(now), encodeTime(now), taskID, string(TransferQueued))
	if err != nil {
		return Transfer{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Transfer{}, false, err
	}
	transfer, err := r.GetTransferByTask(ctx, taskID)
	if err != nil {
		return Transfer{}, false, err
	}
	return transfer, affected == 1, nil
}

// RecoverRunningTransfers conservatively fences transfers whose controller died
// while they were running. Copied items remain evidence; queued/copying items
// become failed with an unknown-result code and are never replayed automatically.
func (r *Repo) RecoverRunningTransfers(ctx context.Context, now time.Time) (int, error) {
	recovered := 0
	err := r.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		rows, err := tx.Query(txCtx, `SELECT id FROM file_transfers WHERE status = ?`, string(TransferRunning))
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			result, err := tx.Exec(txCtx, `UPDATE file_transfers SET
				status = CASE WHEN copied_bytes > 0 THEN ? ELSE ? END,
				error_message = ?, completed_at = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				string(TransferPartialFailure), string(TransferFailed),
				"controller restarted during transfer; result unknown",
				encodeTime(now), encodeTime(now), id, string(TransferRunning))
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				continue
			}
			if _, err := tx.Exec(txCtx, `UPDATE file_transfer_items SET status = ?, error_code = ?, error_message = ?
				WHERE transfer_id = ? AND status IN (?, ?)`,
				string(ItemFailed), "controller_restarted_unknown", "result unknown after controller restart",
				id, string(ItemQueued), string(ItemCopying)); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

// SaveProgress persists a non-terminal snapshot so a restart cannot report queued
// work as completed or lose which items already reached the destination.
func (r *Repo) SaveProgress(ctx context.Context, id string, progress float64, total, copied *int64, message string, items []TransferItem, started time.Time) error {
	return r.ApplyTaskUpdate(ctx, r.db, id, TransferRunning, progress, total, copied, message, items, started, time.Time{})
}
