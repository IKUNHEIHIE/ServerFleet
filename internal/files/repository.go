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
	if upload.ConflictPolicy == "" {
		upload.ConflictPolicy = ConflictOverwrite
	}
	if !upload.ConflictPolicy.Valid() {
		return ErrInvalidTransfer
	}

	_, err := q.Exec(ctx, `INSERT INTO file_uploads
                (id, subject_id, node_id, destination_path, status, bytes_received, expected_size,
                 sha256_expected, sha256_actual, error_code, staging_name, created_at, updated_at, conflict_policy)
                VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', '', ?, ?, ?, ?)`,
		upload.ID, upload.SubjectID, upload.NodeID, upload.DestinationPath, upload.Status,
		nullableInt(upload.ExpectedSize), upload.SHA256Expected, upload.StagingName,
		encodeTime(upload.CreatedAt), encodeTime(upload.UpdatedAt), upload.ConflictPolicy)
	return err
}

func (r *Repo) GetUpload(ctx context.Context, id string) (Upload, error) {
	var (
		upload           Upload
		expected         *int64
		created, updated string
	)
	err := r.db.QueryRow(ctx, `SELECT id, subject_id, node_id, destination_path, status, bytes_received,
                expected_size, sha256_expected, sha256_actual, error_code, staging_name, task_id, created_at, updated_at, conflict_policy, actual_destination
                FROM file_uploads WHERE id = ?`, id).Scan(&upload.ID, &upload.SubjectID, &upload.NodeID,
		&upload.DestinationPath, &upload.Status, &upload.BytesReceived, &expected,
		&upload.SHA256Expected, &upload.SHA256Actual, &upload.ErrorCode, &upload.StagingName, &upload.TaskID,
		&created, &updated, &upload.ConflictPolicy, &upload.ActualDestination)
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

// BindUploadTask atomically moves an open upload into dispatching under exactly
// one unified task and persists the verified staging digest as the upload's
// actual checksum evidence. The guarded WHERE clause makes double dispatch
// impossible.
func (r *Repo) BindUploadTask(ctx context.Context, q storage.Queryer, id, taskID, verifiedSHA string) (bool, error) {
	res, err := q.Exec(ctx, `UPDATE file_uploads SET task_id = ?, sha256_actual = ?, status = ?, updated_at = ?
                WHERE id = ? AND status = ? AND task_id = ''`, taskID, verifiedSHA, UploadDispatching,
		encodeTime(time.Now().UTC()), id, UploadOpen)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
}

// GetUploadByTask resolves the upload bound to one unified task.
func (r *Repo) GetUploadByTask(ctx context.Context, taskID string) (Upload, error) {
	var (
		upload           Upload
		expected         *int64
		created, updated string
	)
	err := r.db.QueryRow(ctx, `SELECT id, subject_id, node_id, destination_path, status, bytes_received,
                expected_size, sha256_expected, sha256_actual, error_code, staging_name, task_id, created_at, updated_at, conflict_policy, actual_destination
                FROM file_uploads WHERE task_id = ?`, taskID).Scan(&upload.ID, &upload.SubjectID, &upload.NodeID,
		&upload.DestinationPath, &upload.Status, &upload.BytesReceived, &expected,
		&upload.SHA256Expected, &upload.SHA256Actual, &upload.ErrorCode, &upload.StagingName, &upload.TaskID,
		&created, &updated, &upload.ConflictPolicy, &upload.ActualDestination)
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

// ListDispatchingUploads returns the ids of uploads still awaiting a trusted
// unified task outcome. Used by startup and lazy reconciliation only.
func (r *Repo) ListDispatchingUploads(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM file_uploads WHERE status = ?`, UploadDispatching)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// TaskBindingState returns the persisted execution binding state for one task.
// An empty state with a nil error is positive evidence that no binding (and
// therefore no write frame) exists; a non-nil error is infrastructure failure
// and must never be treated as absence.
func (r *Repo) TaskBindingState(ctx context.Context, taskID string) (string, error) {
	var state *string
	if err := r.db.QueryRow(ctx, `SELECT json_extract(payload,'$.State') FROM cx_file_execution_bindings WHERE task_id=? LIMIT 1`, taskID).Scan(&state); err != nil {
		if storage.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if state == nil {
		return "", nil
	}
	return *state, nil
}

// FinishUploadTask applies the trusted terminal task outcome exactly once. A
// transition out of dispatching succeeds only for the first caller; later
// calls with the same terminal state are idempotent no-ops.
func (r *Repo) FinishUploadTask(ctx context.Context, q storage.Queryer, id, status, actualSHA, errorCode string, destination ...string) (bool, error) {
	actualPath := ""
	if len(destination) > 1 {
		return false, ErrInvalidTransfer
	}
	if len(destination) == 1 {
		actualPath = destination[0]
	}
	switch status {
	case UploadCompleted, UploadSkipped, UploadFailed, UploadCancelled, UploadAborted:
	default:
		return false, ErrInvalidTransfer
	}
	res, err := q.Exec(ctx, `UPDATE file_uploads SET status = ?, sha256_actual = ?, error_code = ?, updated_at = ?, actual_destination = ?
                WHERE id = ? AND status = ?`, status, actualSHA, errorCode, encodeTime(time.Now().UTC()), actualPath, id, UploadDispatching)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
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
		rows, err := tx.Query(txCtx, `SELECT id FROM file_transfers WHERE status IN (?,?)`, string(TransferRunning), string(TransferCancelling))
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
                                WHERE id = ? AND status IN (?,?)`,
				string(TransferPartialFailure), string(TransferFailed),
				"controller restarted during transfer; result unknown",
				encodeTime(now), encodeTime(now), id, string(TransferRunning), string(TransferCancelling))
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

const distributionSelect = `SELECT id, subject_id, node_id, destination_path, status, bytes_received,
        expected_size, sha256_expected, sha256_actual, error_code, staging_name, task_id, created_at, updated_at
        FROM file_distributions`
const distributionTargetSelect = `SELECT distribution_id, sequence, node_id, write_path, status, bytes, sha256,
        error_code, error_message, updated_at FROM file_distribution_targets`

// CreateDistribution inserts the staging header row with its frozen target list.
func (r *Repo) CreateDistribution(ctx context.Context, q storage.Queryer, distribution Distribution) error {
	_, err := q.Exec(ctx, `INSERT INTO file_distributions
			(id, subject_id, node_id, destination_path, status, bytes_received, expected_size,
			 sha256_expected, sha256_actual, error_code, staging_name, task_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', '', ?, '', ?, ?)`,
		distribution.ID, distribution.SubjectID, distribution.NodeID, distribution.DestinationPath, distribution.Status,
		nullableInt(distribution.ExpectedSize), distribution.SHA256Expected, distribution.StagingName,
		encodeTime(distribution.CreatedAt), encodeTime(distribution.UpdatedAt))
	if err != nil {
		return err
	}
	for _, target := range distribution.Targets {
		if _, err := q.Exec(ctx, `INSERT INTO file_distribution_targets
                        (distribution_id, sequence, node_id, write_path, status, bytes, sha256, error_code, error_message, updated_at)
                        VALUES (?, ?, ?, ?, ?, 0, '', '', '', ?)`,
			distribution.ID, target.Sequence, target.NodeID, target.WritePath, target.Status,
			encodeTime(target.UpdatedAt)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) GetDistribution(ctx context.Context, id string) (Distribution, error) {
	var (
		distribution     Distribution
		expected         *int64
		created, updated string
	)
	err := r.db.QueryRow(ctx, distributionSelect+` WHERE id = ?`, id).Scan(
		&distribution.ID, &distribution.SubjectID, &distribution.NodeID, &distribution.DestinationPath,
		&distribution.Status, &distribution.BytesReceived, &expected, &distribution.SHA256Expected,
		&distribution.SHA256Actual, &distribution.ErrorCode, &distribution.StagingName, &distribution.TaskID,
		&created, &updated)
	if storage.IsNotFound(err) {
		return Distribution{}, ErrDistributionNotFound
	}
	if err != nil {
		return Distribution{}, err
	}
	distribution.ExpectedSize = expected
	if distribution.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Distribution{}, err
	}
	if distribution.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Distribution{}, err
	}
	distribution.Targets, err = r.ListDistributionTargets(ctx, id)
	return distribution, err
}

func (r *Repo) GetDistributionByTask(ctx context.Context, taskID string) (Distribution, error) {
	var (
		distribution     Distribution
		expected         *int64
		created, updated string
	)
	err := r.db.QueryRow(ctx, distributionSelect+` WHERE task_id = ?`, taskID).Scan(
		&distribution.ID, &distribution.SubjectID, &distribution.NodeID, &distribution.DestinationPath,
		&distribution.Status, &distribution.BytesReceived, &expected, &distribution.SHA256Expected,
		&distribution.SHA256Actual, &distribution.ErrorCode, &distribution.StagingName, &distribution.TaskID,
		&created, &updated)
	if storage.IsNotFound(err) {
		return Distribution{}, ErrDistributionNotFound
	}
	if err != nil {
		return Distribution{}, err
	}
	distribution.ExpectedSize = expected
	if distribution.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Distribution{}, err
	}
	if distribution.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Distribution{}, err
	}
	distribution.Targets, err = r.ListDistributionTargets(ctx, distribution.ID)
	return distribution, err
}

func (r *Repo) ListDistributionTargets(ctx context.Context, distributionID string) ([]DistributionTarget, error) {
	rows, err := r.db.Query(ctx, distributionTargetSelect+` WHERE distribution_id = ? ORDER BY sequence`, distributionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []DistributionTarget
	for rows.Next() {
		var (
			target  DistributionTarget
			updated string
		)
		var distributionID string
		if err := rows.Scan(&distributionID, &target.Sequence, &target.NodeID, &target.WritePath,
			&target.Status, &target.Bytes, &target.SHA256, &target.ErrorCode, &target.ErrorMessage, &updated); err != nil {
			return nil, err
		}
		if target.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (r *Repo) UpdateDistributionProgress(ctx context.Context, q storage.Queryer, id string, received int64) error {
	_, err := q.Exec(ctx, `UPDATE file_distributions SET bytes_received = ?, updated_at = ? WHERE id = ? AND status = ?`,
		received, encodeTime(time.Now().UTC()), id, DistributionOpen)
	return err
}

// BindDistributionTask durably moves a verified staging distribution into
// dispatching under exactly one task id. Idempotent for the same task.
func (r *Repo) BindDistributionTask(ctx context.Context, q storage.Queryer, id, taskID, verifiedSHA string) (bool, error) {
	res, err := q.Exec(ctx, `UPDATE file_distributions SET status = ?, task_id = ?, sha256_actual = ?, updated_at = ?
                WHERE id = ? AND ((status = ? AND task_id = '') OR (status = ? AND task_id = ?))`,
		DistributionDispatching, taskID, verifiedSHA, encodeTime(time.Now().UTC()),
		id, DistributionOpen, DistributionDispatching, taskID)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
}

// FinishDistributionTarget persists one per-target terminal outcome exactly
// once. Non-terminal pending/dispatching rows converge; terminal rows are
// idempotent no-ops so replays cannot overwrite another honest result.
func (r *Repo) FinishDistribution(ctx context.Context, q storage.Queryer, id, status, actualSHA, errorCode string) (bool, error) {
	switch status {
	case TargetStateCompleted, TargetStateFailed, TargetStateCancelled, TargetStateUnknown:
	default:
		return false, ErrInvalidTargets
	}
	res, err := q.Exec(ctx, `UPDATE file_distribution_targets SET status = ?, bytes = ?, sha256 = ?, error_code = ?,
                error_message = ?, updated_at = ? WHERE distribution_id = ? AND node_id = ? AND status IN (?, ?)`,
		status, bytes, sha, errorCode, errorMessage, encodeTime(time.Now().UTC()),
		id, nodeID, TargetStatePending, TargetStateDispatching)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
}

// SetDistributionTargetState atomically claims a pending target. A dispatching
// or terminal row cannot be claimed again, including after reconciliation.
func (r *Repo) SetDistributionTargetState(ctx context.Context, id, nodeID, status string) (bool, error) {
	if status != TargetStateDispatching {
		return false, ErrInvalidTargets
	}
	res, err := r.db.Exec(ctx, `UPDATE file_distribution_targets SET status = ?, updated_at = ?
                WHERE distribution_id = ? AND node_id = ? AND status = ?
                AND EXISTS (SELECT 1 FROM file_distributions WHERE id = ? AND status = ?)`,
		status, encodeTime(time.Now().UTC()), id, nodeID, TargetStatePending, id, DistributionDispatching)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
}

// FailOpenDistribution rejects unsubmitted staging without touching a bound
// task. The caller commits its failure audit in the same transaction.
func (r *Repo) FailOpenDistribution(ctx context.Context, q storage.Queryer, id, actualSHA, errorCode string) (bool, error) {
	res, err := q.Exec(ctx, `UPDATE file_distributions SET status = ?, sha256_actual = ?, error_code = ?, updated_at = ?
                WHERE id = ? AND status = ? AND task_id = ''`,
		DistributionFailed, actualSHA, errorCode, encodeTime(time.Now().UTC()), id, DistributionOpen)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved == 1, err
}

// FinishDistribution atomically converges the distribution terminal state
// from dispatching. The first caller wins; later calls are no-ops.
func (r *Repo) FinishDistribution(ctx context.Context, q storage.Queryer, id, status, actualSHA, errorCode string, destination ...string) (bool, error) {
	actualPath := ""
	if len(destination) > 1 {
		return false, ErrInvalidTransfer
	}
	if len(destination) == 1 {
		actualPath = destination[0]
	}
	switch status {
	case DistributionCompleted, DistributionPartial, DistributionFailed, DistributionCancelled, DistributionAborted:
	default:
		return false, ErrInvalidTransfer
	}
	res, err := q.Exec(ctx, `UPDATE file_distributions SET status = ?, sha256_actual = ?, error_code = ?, updated_at = ?
                WHERE id = ? AND status = ?`,
		status, actualSHA, errorCode, encodeTime(time.Now().UTC()), id, DistributionDispatching)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	return moved > 0, err
}

// AbortDistributionRow converges an OPEN staging distribution to aborted.
// Only the open state can be aborted; dispatching distributions must go
// through the unified cancellation path instead.
func (r *Repo) AbortDistributionRow(ctx context.Context, q storage.Queryer, id string) error {
	res, err := q.Exec(ctx, `UPDATE file_distributions SET status = ?, error_code = 'aborted', updated_at = ?
                WHERE id = ? AND status = ?`, DistributionAborted, encodeTime(time.Now().UTC()), id, DistributionOpen)
	if err != nil {
		return err
	}
	moved, err := res.RowsAffected()
	if err == nil && moved == 0 {
		return ErrDistributionClosed
	}
	return err
}

func (r *Repo) ListDispatchingDistributions(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM file_distributions WHERE status = ?`, DistributionDispatching)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FreezeDistributionTargetPaths persists the worker-mapped per-node write
// paths before the unified task is submitted, so the frozen rows and the task
// parameters agree exactly from the first frame.
func (r *Repo) FreezeDistributionTargetPaths(ctx context.Context, id string, paths map[string]string) error {
	for node, writePath := range paths {
		res, err := r.db.Exec(ctx, `UPDATE file_distribution_targets SET write_path = ?, updated_at = ?
			WHERE distribution_id = ? AND node_id = ? AND write_path = ''`,
			writePath, encodeTime(time.Now().UTC()), id, node)
		if err != nil {
			return err
		}
		moved, err := res.RowsAffected()
		if err == nil && moved == 0 {
			return ErrInvalidTargets
		}
	}
	return nil
}
