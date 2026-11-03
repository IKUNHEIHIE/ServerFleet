package files

import (
	"context"
	"fmt"
	"os"

	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
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
// task, persists the verified staging digest as the upload's actual checksum
// evidence and moves it to dispatching. Called by CompleteUpload under the
// upload lock; the caller has already verified the subject, node write
// authorization and the staging digest. The unified task submission writes its
// own linked tasks.submit audit record, so no additional audit action is
// introduced here.
func (s *Service) SubmitUploadTask(ctx context.Context, upload Upload, taskID, verifiedSHA string) error {
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		moved, err := s.repo.BindUploadTask(txCtx, tx, upload.ID, taskID, verifiedSHA)
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

// TaskOutcomeReader reads the trusted unified task state for upload
// reconciliation. Implemented by the trusted program assembly; a nil or
// non-implementing submitter leaves reconciliation inert (uploads stay
// dispatching until a worker outcome or operator action).
type TaskOutcomeReader interface {
	ReadTaskOutcome(ctx context.Context, taskID string) (tasks.Task, error)
}

func (s *Service) taskOutcomeReader() TaskOutcomeReader {
	if reader, ok := s.submitter.(TaskOutcomeReader); ok {
		return reader
	}
	return nil
}

// uploadTerminalFromTask maps a terminal unified task outcome to the honest
// upload terminal state. A terminal task without a matching terminal upload is
// positive evidence that the worker never applied its outcome; the mapping
// never claims success for an uncertain execution.
func uploadTerminalFromTask(t tasks.Task) (status, errorCode string, ok bool) {
	switch t.Status {
	case tasks.StatusCompleted:
		return UploadCompleted, "", true
	case tasks.StatusCanceled:
		return UploadCancelled, "cancelled_by_task", true
	case tasks.StatusExpired:
		return UploadFailed, "task_expired", true
	case tasks.StatusFailed, tasks.StatusPartialFailure:
		// Partial success is never claimed as a completed upload.
		return UploadFailed, "task_failed", true
	case tasks.StatusUnknown:
		return UploadFailed, UploadErrorAgentUnknown, true
	default:
		return "", "", false
	}
}

// ReconcileUploadTasks converges every dispatching upload whose unified task
// already reached a terminal state without the worker applying an upload
// outcome (controller crash, queued expiry, rejection, direct task-interface
// cancellation). Reads that fail to provide positive task evidence leave the
// upload untouched; convergence itself is the guarded exactly-once terminal
// transition, so it never overwrites a worker outcome racing in parallel.
func (s *Service) ReconcileUploadTasks(ctx context.Context) (int, error) {
	reader := s.taskOutcomeReader()
	if reader == nil {
		return 0, nil
	}
	ids, err := s.repo.ListDispatchingUploads(ctx)
	if err != nil {
		return 0, err
	}
	converged := 0
	for _, id := range ids {
		upload, err := s.repo.GetUpload(ctx, id)
		if err != nil || upload.Status != UploadDispatching || upload.TaskID == "" {
			continue
		}
		task, err := reader.ReadTaskOutcome(ctx, upload.TaskID)
		if err != nil || !task.Status.Terminal() {
			// No positive terminal evidence: leave the upload dispatching.
			continue
		}
		if status, errorCode, ok := uploadTerminalFromTask(task); ok {
			if _, err := s.ApplyUploadTaskUpdate(ctx, upload.TaskID, UploadTaskUpdate{
				Status: status, ErrorCode: errorCode, RequestID: task.RequestID,
			}); err != nil {
				return converged, err
			}
			converged++
		}
	}
	return converged, nil
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
		// The verified staging digest was persisted at dispatch; the worker
		// streams exactly those bytes and the agent-side session verifies the
		// same digest before the write is accepted. It remains the upload's
		// actual checksum evidence even when the client supplied none.
		if upload.SHA256Actual != "" {
			actual = upload.SHA256Actual
		} else {
			actual = upload.SHA256Expected
		}
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
		requestID := update.RequestID
		if requestID == "" {
			// Terminal outcome reported without a request context (review
			// probes, reconciliation); the linked task id remains in the event.
			requestID = "upload-task-outcome"
		}
		event := s.event(upload.SubjectID, action, result, requestID, []string{upload.NodeID}, reason)
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
// the task cancel stops the worker run. For tasks that never issued a write
// frame (no binding, or a binding still before the agent opened the
// destination) the upload converges to cancelled here, using positive
// persisted evidence only; a binding query failure keeps the upload
// dispatching rather than guessing. A stream already past the open keeps the
// worker's own evidence-based terminal outcome. Aborting an open upload stays
// on the existing direct path.
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
	// Convergence needs positive evidence that no write frame was issued.
	// A binding that is absent or still reserved/offered/accepted means the
	// agent never opened the destination file; only then is a known
	// cancelled terminal honest. Infrastructure errors carry no evidence.
	state, stateErr := s.repo.TaskBindingState(ctx, upload.TaskID)
	if stateErr != nil || state == "opened" || state == "done" {
		// Unknown or in-flight: the worker (or startup reconciliation)
		// applies the terminal outcome with its own evidence.
		return s.repo.GetUpload(ctx, uploadID)
	}
	converged, err := s.ApplyUploadTaskUpdate(ctx, upload.TaskID, UploadTaskUpdate{
		Status: UploadCancelled, ErrorCode: "cancelled_before_write", RequestID: requestID,
	})
	if err != nil {
		return Upload{}, err
	}
	_ = converged
	return s.repo.GetUpload(ctx, uploadID)
}
