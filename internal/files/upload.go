package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

const (
	UploadOpen      = "open"
	UploadCompleted = "completed"
	UploadAborted   = "aborted"
	UploadFailed    = "failed"

	maxChunkBytes = 8 << 20
)

// CreateUpload opens a resumable staging file. It does not contact the node; bytes
// are streamed only on CompleteUpload through the transport port.
func (s *Service) CreateUpload(ctx context.Context, subjectID, requestID, nodeID, destinationPath string, expectedSize *int64, sha256Expected string) (Upload, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Upload{}, err
	}
	if !validToken(subjectID, 128) || !validToken(nodeID, 128) {
		return Upload{}, fmt.Errorf("%w: subject/node", ErrInvalidTransfer)
	}
	if err := ValidateRemotePath(destinationPath); err != nil {
		return Upload{}, err
	}
	if destinationPath == "/" {
		return Upload{}, fmt.Errorf("%w: destination must be a file", ErrInvalidTransfer)
	}
	if expectedSize != nil && (*expectedSize < 0 || *expectedSize > s.maxUploadBytes) {
		return Upload{}, ErrUploadTooLarge
	}
	if sha256Expected != "" && !sha256Pattern.MatchString(sha256Expected) {
		return Upload{}, fmt.Errorf("%w: sha256", ErrInvalidTransfer)
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, nodeID, "file_upload_write"); err != nil {
		return Upload{}, err
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return Upload{}, fmt.Errorf("files: create staging directory: %w", err)
	}
	upload := Upload{
		ID: newID("up"), SubjectID: subjectID, NodeID: nodeID, DestinationPath: destinationPath,
		Status: UploadOpen, ExpectedSize: expectedSize, SHA256Expected: sha256Expected,
		StagingName: newID("stage") + ".part", CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return Upload{}, err
	}
	file, err := os.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Upload{}, fmt.Errorf("files: create staging file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(staging)
		return Upload{}, err
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.CreateUpload(txCtx, tx, upload); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUploadCreate, "success", requestID, []string{nodeID}, ""))
	})
	if err != nil {
		_ = os.Remove(staging)
		return Upload{}, err
	}
	return upload, nil
}

// AppendChunk writes bytes at exactly the stored offset. A mismatched offset is a
// conflict rather than a silent overwrite, so a resumed client can trust the
// reported byte count.
func (s *Service) AppendChunk(ctx context.Context, subjectID, requestID, uploadID string, offset int64, data []byte) (Upload, error) {
	unlock := s.lockUpload(uploadID)
	defer unlock()
	upload, err := s.repo.GetUpload(ctx, uploadID)
	if err != nil {
		return Upload{}, err
	}
	if upload.SubjectID != subjectID {
		return Upload{}, ErrSubjectMismatch
	}
	if upload.Status != UploadOpen {
		return Upload{}, ErrUploadClosed
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_append"); err != nil {
		return Upload{}, err
	}
	if len(data) == 0 || len(data) > maxChunkBytes {
		return Upload{}, fmt.Errorf("%w: chunk size", ErrInvalidTransfer)
	}
	if offset != upload.BytesReceived {
		return Upload{}, ErrOffsetConflict
	}
	next := upload.BytesReceived + int64(len(data))
	if next > s.maxUploadBytes || (upload.ExpectedSize != nil && next > *upload.ExpectedSize) {
		return Upload{}, ErrUploadTooLarge
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return Upload{}, err
	}
	file, err := os.OpenFile(staging, os.O_WRONLY, 0o600)
	if err != nil {
		return Upload{}, fmt.Errorf("files: open staging file: %w", err)
	}
	if _, err := file.WriteAt(data, offset); err != nil {
		_ = file.Close()
		return Upload{}, fmt.Errorf("files: write staging chunk: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Upload{}, err
	}
	if err := file.Close(); err != nil {
		return Upload{}, err
	}
	if err := s.repo.UpdateUploadProgress(ctx, s.db, upload.ID, next); err != nil {
		return Upload{}, err
	}
	upload.BytesReceived = next
	upload.UpdatedAt = s.now()
	return upload, nil
}

// WriteUploadContent streams a request body into the staging file at exactly the
// recorded offset. The body is capped by both the per-upload limit and the
// declared size; an oversized or interrupted write leaves the recorded progress
// unchanged so the client can safely retry the same offset.
func (s *Service) WriteUploadContent(ctx context.Context, subjectID, requestID, uploadID string, offset int64, content io.Reader) (Upload, error) {
	if content == nil {
		return Upload{}, fmt.Errorf("%w: content", ErrInvalidTransfer)
	}
	if !validToken(subjectID, 128) || !validToken(uploadID, 128) {
		return Upload{}, fmt.Errorf("%w: subject/upload", ErrInvalidTransfer)
	}
	unlock := s.lockUpload(uploadID)
	defer unlock()
	upload, err := s.repo.GetUpload(ctx, uploadID)
	if err != nil {
		return Upload{}, err
	}
	if upload.SubjectID != subjectID {
		return Upload{}, ErrSubjectMismatch
	}
	if upload.Status != UploadOpen {
		return Upload{}, ErrUploadClosed
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_content"); err != nil {
		return Upload{}, err
	}
	if offset < 0 || offset != upload.BytesReceived {
		return Upload{}, ErrOffsetConflict
	}
	remaining := s.maxUploadBytes - offset
	if upload.ExpectedSize != nil && *upload.ExpectedSize-offset < remaining {
		remaining = *upload.ExpectedSize - offset
	}
	if remaining < 0 {
		return Upload{}, ErrUploadTooLarge
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return Upload{}, err
	}
	file, err := os.OpenFile(staging, os.O_WRONLY, 0o600)
	if err != nil {
		return Upload{}, fmt.Errorf("files: open staging file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return Upload{}, err
	}
	if info.Size() != offset {
		_ = file.Close()
		return Upload{}, fmt.Errorf("files: staged bytes are inconsistent with the recorded offset")
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return Upload{}, err
	}
	written, copyErr := io.Copy(file, io.LimitReader(content, remaining+1))
	if copyErr != nil {
		_ = file.Truncate(offset)
		_ = file.Close()
		if ctx.Err() != nil {
			return Upload{}, ctx.Err()
		}
		return Upload{}, fmt.Errorf("files: write staging content: %w", copyErr)
	}
	if written > remaining {
		_ = file.Truncate(offset)
		_ = file.Sync()
		_ = file.Close()
		return Upload{}, ErrUploadTooLarge
	}
	if written == 0 {
		_ = file.Close()
		return Upload{}, fmt.Errorf("%w: empty content", ErrInvalidTransfer)
	}
	if err := file.Sync(); err != nil {
		_ = file.Truncate(offset)
		_ = file.Close()
		return Upload{}, err
	}
	next := offset + written
	if err := s.repo.UpdateUploadProgress(ctx, s.db, upload.ID, next); err != nil {
		_ = file.Truncate(offset)
		_ = file.Sync()
		_ = file.Close()
		return Upload{}, err
	}
	if err := file.Close(); err != nil {
		return Upload{}, err
	}
	upload.BytesReceived = next
	upload.UpdatedAt = s.now()
	return upload, nil
}

// CompleteUpload verifies the staged digest and streams the file through the
// transport. A transport failure keeps the upload open for a safe retry because
// the staged bytes are unchanged.
func (s *Service) CompleteUpload(ctx context.Context, subjectID, requestID, uploadID string) (Upload, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Upload{}, err
	}
	unlock := s.lockUpload(uploadID)
	defer unlock()
	upload, err := s.repo.GetUpload(ctx, uploadID)
	if err != nil {
		return Upload{}, err
	}
	if upload.SubjectID != subjectID {
		return Upload{}, ErrSubjectMismatch
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_complete"); err != nil {
		return Upload{}, err
	}
	if upload.Status == UploadCompleted {
		return upload, nil
	}
	if upload.Status != UploadOpen {
		return Upload{}, ErrUploadClosed
	}
	if upload.ExpectedSize != nil && upload.BytesReceived != *upload.ExpectedSize {
		return Upload{}, fmt.Errorf("%w: received %d of %d bytes", ErrInvalidTransfer, upload.BytesReceived, *upload.ExpectedSize)
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return Upload{}, err
	}
	digest, err := fileDigest(staging)
	if err != nil {
		return Upload{}, err
	}
	if upload.SHA256Expected != "" && digest != upload.SHA256Expected {
		_ = os.Remove(staging)
		if statusErr := s.repo.UpdateUploadStatus(ctx, s.db, upload.ID, UploadFailed, digest, "checksum_mismatch"); statusErr != nil {
			return Upload{}, statusErr
		}
		_ = s.audit.Append(ctx, s.event(subjectID, ActionUploadComplete, "failure", requestID, []string{upload.NodeID}, "checksum_mismatch"))
		return Upload{}, ErrChecksumMismatch
	}
	if s.uploads == nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := s.audit.Append(auditCtx, s.event(subjectID, ActionUploadComplete, "failure", requestID, []string{upload.NodeID}, "transport_unavailable")); err != nil {
			return Upload{}, err
		}
		return Upload{}, ErrTransportAbsent
	}
	file, err := os.Open(staging)
	if err != nil {
		return Upload{}, err
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_complete"); err != nil {
		_ = file.Close()
		return Upload{}, err
	}
	pushErr := s.uploads.PushFile(ctx, upload.NodeID, upload.DestinationPath, file, upload.BytesReceived, digest)
	_ = file.Close()
	if pushErr != nil {
		_ = s.repo.UpdateUploadStatus(context.WithoutCancel(ctx), s.db, upload.ID, UploadOpen, digest, "transport_failed")
		_ = s.audit.Append(context.WithoutCancel(ctx), s.event(subjectID, ActionUploadComplete, "failure", requestID, []string{upload.NodeID}, "transport_failed"))
		return Upload{}, ErrTransportFailure
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdateUploadStatus(txCtx, tx, upload.ID, UploadCompleted, digest, ""); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUploadComplete, "success", requestID, []string{upload.NodeID}, ""))
	})
	if err != nil {
		return Upload{}, err
	}
	_ = os.Remove(staging)
	upload.Status = UploadCompleted
	upload.SHA256Actual = digest
	return upload, nil
}

func (s *Service) AbortUpload(ctx context.Context, subjectID, requestID, uploadID string) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	unlock := s.lockUpload(uploadID)
	defer unlock()
	upload, err := s.repo.GetUpload(ctx, uploadID)
	if err != nil {
		return err
	}
	if upload.SubjectID != subjectID {
		return ErrSubjectMismatch
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_abort"); err != nil {
		return err
	}
	if upload.Status != UploadOpen {
		return ErrUploadClosed
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return err
	}
	_ = os.Remove(staging)
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdateUploadStatus(txCtx, tx, upload.ID, UploadAborted, "", ""); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUploadAbort, "success", requestID, []string{upload.NodeID}, ""))
	})
}

func (s *Service) GetUpload(ctx context.Context, subjectID, requestID, uploadID string) (Upload, error) {
	upload, err := s.repo.GetUpload(ctx, uploadID)
	if err != nil {
		return Upload{}, err
	}
	if upload.SubjectID != subjectID {
		return Upload{}, ErrSubjectMismatch
	}
	// Upload status is an owner-bound write workflow, not a remote file read.
	// Content/complete handlers use this precheck before their own write gate.
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_status"); err != nil {
		return Upload{}, err
	}
	return upload, nil
}

// Download streams one authorized remote file into sink. Transport errors are
// replaced with a fixed error so upstream paths or commands are not echoed.
func (s *Service) Download(ctx context.Context, subjectID, requestID, nodeID, remotePath string, sink io.Writer) (int64, error) {
	if err := s.requireRequest(requestID); err != nil {
		return 0, err
	}
	if sink == nil {
		return 0, fmt.Errorf("%w: sink", ErrInvalidTransfer)
	}
	if !validToken(nodeID, 128) {
		return 0, fmt.Errorf("%w: node", ErrInvalidTransfer)
	}
	if err := ValidateRemotePath(remotePath); err != nil {
		return 0, err
	}
	if err := s.authorizeNode(ctx, subjectID, ActionRead, requestID, nodeID, "file_download_read"); err != nil {
		return 0, err
	}
	if s.downloads == nil {
		return 0, ErrTransportAbsent
	}
	written, err := s.downloads.PullFile(ctx, nodeID, remotePath, sink)
	if err != nil {
		_ = s.audit.Append(context.WithoutCancel(ctx), s.event(subjectID, ActionDownload, "failure", requestID, []string{nodeID}, "transport_failed"))
		return written, ErrTransportFailure
	}
	_ = s.audit.Append(ctx, s.event(subjectID, ActionDownload, "success", requestID, []string{nodeID}, ""))
	return written, nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
