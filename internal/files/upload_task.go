package files

import (
	"context"
	"fmt"
	"os"

	"serverfleet.local/serverfleet/internal/storage"
)

// UploadCommand is the frozen upload dispatch plan submitted to the unified
// task queue. Staging bytes never enter the task parameters; the worker
// streams them frame-by-frame through the controlled agent channel.
type UploadCommand struct {
	SubjectID, RequestID, UploadID, NodeID, DestinationPath, SHA256Expected string
	Size                                                                    int64
}

// UploadTaskSubmitter is implemented by the trusted program assembly when the
// controlled agent file channel is configured. A submitter that does not
// implement it keeps the legacy direct transport path for isolated unit
// compositions only; production assembly always implements it.
type UploadTaskSubmitter interface {
	SubmitUpload(ctx context.Context, command UploadCommand) (string, error)
}

// UploadTaskUpdate is the trusted terminal outcome reported by the task
// worker. It is the only path out of UploadDispatching. RequestID carries the
// frozen task's original request id for the terminal audit record.
type UploadTaskUpdate struct {
	Status               string
	ErrorCode, ErrorNote string
	RequestID            string
}

// UploadErrorAgentUnknown marks an execution whose frame outcome is not
// known; the upload record stays honest and is never silently retried.
const UploadErrorAgentUnknown = "agent_unknown"

// SubmitUploadTask binds a verified staging upload to exactly one unified
// task and moves it to dispatching. Called by CompleteUpload under the upload
// lock; the caller has already verified the subject, node write authorization
// and the staging digest. The unified task submission writes its own linked
// tasks.submit audit record, so no additional audit action is introduced here.
func (s *Service) SubmitUploadTask(ctx context.Context, upload Upload, taskID string) error {
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		moved, err := s.repo.BindUploadTask(txCtx, tx, upload.ID, taskID)
		if err != nil {
			return err
		}
		if !moved {
			return ErrUploadClosed
		}
		return nil
	})
}

// uploadTaskSubmitter exposes the configured upload task submitter, if any.
func (s *Service) uploadTaskSubmitter() UploadTaskSubmitter {
	if submitter, ok := s.submitter.(UploadTaskSubmitter); ok {
		return submitter
	}
	return nil
}

// ApplyUploadTaskUpdate reconciles the persisted upload with a trusted task
// result. Exactly one terminal transition leaves dispatching; repeated calls
// with the same terminal state are idempotent. The staging file is removed on
// completed and cancelled outcomes; a failed or unknown execution keeps it
// for operator reconciliation.
func (s *Service) ApplyUploadTaskUpdate(ctx context.Context, taskID string, update UploadTaskUpdate) (Upload, error) {
	upload, err := s.repo.GetUploadByTask(ctx, taskID)
	if err != nil {
		return Upload{}, err
	}
	if upload.Status != UploadDispatching {
		if upload.Status == update.Status {
			return upload, nil
		}
		return Upload{}, fmt.Errorf("%w: upload is %s", ErrUploadClosed, upload.Status)
	}
	var actual, errorCode string
	switch update.Status {
	case UploadCompleted:
		// The worker streams the exact verified staging bytes; the agent-side
		// session verifies the digest before the write is accepted.
		actual = upload.SHA256Expected
	default:
		errorCode = update.ErrorCode
	}
	finished := false
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		moved, err := s.repo.FinishUploadTask(txCtx, tx, upload.ID, update.Status, actual, errorCode)
		if err != nil {
			return err
		}
		finished = moved
		if !moved {
			return nil
		}
		action, result, reason := ActionUploadComplete, "failure", update.ErrorCode
		if update.Status == UploadCompleted {
			result = "success"
		} else if update.Status == UploadCancelled {
			action, result = ActionUploadAbort, "success"
		}
		event := s.event(upload.SubjectID, action, result, update.RequestID, []string{upload.NodeID}, reason)
		event.TaskID = taskID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if err != nil {
		return Upload{}, err
	}
	if !finished {
		// Worker finalized concurrently with identical or different evidence.
		return s.repo.GetUploadByTask(ctx, taskID)
	}
	if update.Status == UploadCompleted || update.Status == UploadCancelled {
		if staging, err := s.stagingPath(upload.StagingName); err == nil {
			_ = os.Remove(staging)
		}
	}
	upload.Status = update.Status
	upload.SHA256Actual = actual
	upload.ErrorCode = errorCode
	return upload, nil
}

// DispatchedStaging opens the staging file bound to a dispatching upload for
// the worker. The binding and status guards make replays across restarts
// observable; the caller re-verifies the digest while streaming.
func (s *Service) DispatchedStaging(ctx context.Context, taskID string) (*os.File, Upload, error) {
	upload, err := s.repo.GetUploadByTask(ctx, taskID)
	if err != nil {
		return nil, Upload{}, err
	}
	if upload.Status != UploadDispatching || upload.TaskID != taskID {
		return nil, Upload{}, ErrUploadClosed
	}
	staging, err := s.stagingPath(upload.StagingName)
	if err != nil {
		return nil, Upload{}, err
	}
	file, err := os.OpenFile(staging, os.O_RDONLY, 0o600)
	if err != nil {
		return nil, Upload{}, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != upload.BytesReceived {
		_ = file.Close()
		return nil, Upload{}, fmt.Errorf("files: staged bytes are inconsistent with the recorded size")
	}
	return file, upload, nil
}

// CancelUpload moves a dispatching upload into the unified cancellation path:
// the task cancel stops the worker run and the worker applies the terminal
// cancelled state with its own evidence. Aborting an open upload stays on the
// existing direct path.
func (s *Service) CancelUpload(ctx context.Context, subjectID, requestID, uploadID string) (Upload, error) {
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
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, upload.NodeID, "file_upload_cancel"); err != nil {
		return Upload{}, err
	}
	if upload.Status != UploadDispatching || upload.TaskID == "" {
		return upload, nil
	}
	if s.canceller == nil {
		return Upload{}, ErrCancellerAbsent
	}
	if err := s.canceller.CancelTask(ctx, upload.TaskID, subjectID, requestID); err != nil {
		return Upload{}, err
	}
	return s.repo.GetUpload(ctx, uploadID)
}
