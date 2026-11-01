package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
)

const ActionTransferExecute = "files.transfer_execute"

var (
	ErrExecutionTerminal = errors.New("files: transfer execution is already terminal")
	ErrTransferIdentity  = errors.New("files: transfer execution identity mismatch")
	ErrSpoolUnavailable  = errors.New("files: transfer spool directory is not configured")
)

// TransferExecution is the trusted task linkage. ExecutionID is the CX execution
// identifier when the task pipeline exposes one; when empty the coordinator uses a
// stable task/transfer-derived id so retries still dedupe at the agent.
type TransferExecution struct {
	SubjectID   string
	RequestID   string
	ExecutionID string
}

// NodeTransport is the pair of ports the coordinator uses for one node. The
// controller composition builds it from the active WSS+mTLS file sessions; no
// other execution path is permitted.
type NodeTransport struct {
	Upload   UploadTransport
	Download DownloadTransport
}

// NodeFileTransports resolves a node-scoped transport at execution time.
type NodeFileTransports interface {
	ForNode(ctx context.Context, nodeID string) (NodeTransport, error)
}

// NodeFileTransportsFunc adapts a function to NodeFileTransports.
type NodeFileTransportsFunc func(ctx context.Context, nodeID string) (NodeTransport, error)

func (f NodeFileTransportsFunc) ForNode(ctx context.Context, nodeID string) (NodeTransport, error) {
	return f(ctx, nodeID)
}

// TransferOutcome is the per-item result the controller reports through the
// unified task pipeline. It is never a substitute for the task's own status.
type TransferOutcome struct {
	TaskID      string
	Status      TransferStatus
	Items       []ItemUpdate
	TotalBytes  int64
	CopiedBytes int64
	Message     string
	StartedAt   time.Time
	CompletedAt time.Time
}

func (o TransferOutcome) TaskUpdate() TaskUpdate {
	total := o.TotalBytes
	copied := o.CopiedBytes
	progress := 0.0
	if total > 0 {
		progress = float64(copied) / float64(total)
		if progress > 1 {
			progress = 1
		}
	}
	return TaskUpdate{
		Status: o.Status, Progress: progress,
		TotalBytes: &total, CopiedBytes: &copied,
		Message: o.Message, Items: o.Items,
		StartedAt: o.StartedAt, CompletedAt: o.CompletedAt,
	}
}

// CoordinationProgress receives a snapshot after every item. It must not block
// the transfer for long.
type CoordinationProgress func(TransferOutcome)

// CoordinatorOptions configures the cross-node byte path. All ports are required;
// a missing transport or authorizer fails the execution instead of falling back to
// a local staging directory.
type CoordinatorOptions struct {
	Repo         *Repo
	Transports   NodeFileTransports
	Authorizer   auth.Authorizer
	Audit        AuditSink
	SpoolDir     string
	MaxFileBytes int64
	Now          func() time.Time
}

// TransferCoordinator pulls a source file through the controller and pushes it to
// the destination node. It is invoked by the unified task pipeline, never by an
// HTTP handler and never directly against an agent.
type TransferCoordinator struct {
	repo       *Repo
	transports NodeFileTransports
	authorizer auth.Authorizer
	audit      AuditSink
	spoolDir   string
	maxFile    int64
	now        func() time.Time
}

func NewTransferCoordinator(options CoordinatorOptions) (*TransferCoordinator, error) {
	switch {
	case options.Repo == nil:
		return nil, errors.New("files: coordinator repository is required")
	case options.Transports == nil:
		return nil, ErrTransportAbsent
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	case options.Audit == nil:
		return nil, errors.New("files: coordinator audit sink is required")
	case options.SpoolDir == "":
		return nil, ErrSpoolUnavailable
	}
	maxFile := options.MaxFileBytes
	if maxFile <= 0 {
		maxFile = DefaultChannelFileBytes
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &TransferCoordinator{
		repo: options.Repo, transports: options.Transports, authorizer: options.Authorizer,
		audit: options.Audit, spoolDir: options.SpoolDir, maxFile: maxFile, now: now,
	}, nil
}

// ExecuteTask runs one files.transfer task. The transfer row is loaded from the
// task id; item paths and endpoints never come from the caller. Per-item failures
// are reported in the outcome, while setup, authorization and cancellation errors
// are returned as errors.
func (c *TransferCoordinator) ExecuteTask(ctx context.Context, taskID string, execution TransferExecution, progress CoordinationProgress) (TransferOutcome, error) {
	if c == nil {
		return TransferOutcome{}, ErrCoordinatorUnavailable
	}
	if !validToken(taskID, 128) || !validToken(execution.SubjectID, 128) || execution.RequestID == "" {
		return TransferOutcome{}, ErrTransferIdentity
	}
	transfer, err := c.repo.GetTransferByTask(ctx, taskID)
	if err != nil {
		return TransferOutcome{}, err
	}
	if transfer.SubmitterID != execution.SubjectID {
		return TransferOutcome{}, ErrTransferIdentity
	}
	if transfer.Status.Terminal() {
		return TransferOutcome{}, ErrExecutionTerminal
	}
	if transfer.DryRun {
		return TransferOutcome{}, fmt.Errorf("%w: dry-run transfer cannot execute", ErrInvalidTransfer)
	}
	if err := c.authorizeExecution(ctx, execution, transfer); err != nil {
		return TransferOutcome{}, err
	}
	if err := os.MkdirAll(c.spoolDir, 0o700); err != nil {
		return TransferOutcome{}, fmt.Errorf("%w: %v", ErrSpoolUnavailable, err)
	}
	executionID := execution.ExecutionID
	if executionID == "" {
		executionID = "task_" + taskID + "_" + transfer.ID
	}
	if !validChannelToken(executionID, 160) {
		return TransferOutcome{}, ErrTransferIdentity
	}
	source, err := c.transports.ForNode(ctx, transfer.SourceNodeID)
	if err != nil || source.Download == nil {
		return TransferOutcome{}, fmt.Errorf("%w: source node", ErrTransportAbsent)
	}
	destination, err := c.transports.ForNode(ctx, transfer.DestinationNodeID)
	if err != nil || destination.Upload == nil {
		return TransferOutcome{}, fmt.Errorf("%w: destination node", ErrTransportAbsent)
	}

	outcome := TransferOutcome{
		TaskID: taskID, Status: TransferRunning, StartedAt: c.now(),
		Items: make([]ItemUpdate, 0, len(transfer.Items)),
	}
	aborted := ""
	for index, item := range transfer.Items {
		update := ItemUpdate{
			SourcePath: item.SourcePath, DestinationPath: item.DestinationPath, Status: item.Status,
			BytesCopied: item.BytesCopied, ErrorCode: item.ErrorCode, ErrorMessage: item.ErrorMessage,
		}
		switch item.Status {
		case ItemCopied, ItemSkipped, ItemFailed, ItemCancelled:
			outcome.Items = append(outcome.Items, update)
			if item.Status == ItemCopied || item.Status == ItemSkipped {
				outcome.CopiedBytes += item.BytesCopied
				outcome.TotalBytes += item.BytesCopied
			}
			continue
		}
		if aborted != "" {
			update.Status = ItemCancelled
			update.ErrorCode = aborted
			update.ErrorMessage = "not attempted after a destination failure"
			outcome.Items = append(outcome.Items, update)
			continue
		}
		if err := ctx.Err(); err != nil {
			update.Status = ItemCancelled
			update.ErrorCode = "canceled"
			update.ErrorMessage = "transfer canceled"
			outcome.Items = append(outcome.Items, update)
			for _, remaining := range transfer.Items[index+1:] {
				outcome.Items = append(outcome.Items, ItemUpdate{
					SourcePath: remaining.SourcePath, DestinationPath: remaining.DestinationPath,
					Status: ItemCancelled, ErrorCode: "canceled", ErrorMessage: "transfer canceled",
				})
			}
			outcome.Status = TransferCanceled
			outcome.CompletedAt = c.now()
			outcome.Message = "transfer canceled"
			c.auditOutcome(ctx, execution, transfer, outcome, "canceled")
			if progress != nil {
				progress(outcome)
			}
			return outcome, err
		}
		itemCtx := WithChannelIdentity(ctx, transfer.ID+":"+strconv.Itoa(item.Sequence), executionID)
		update.Status = ItemCopying
		outcome.Items = append(outcome.Items, update)
		if progress != nil {
			progress(outcome)
		}
		copied, destinationPath, resultStatus, err := c.transferItem(itemCtx, c.spoolDir, source.Download, destination.Upload, transfer, item)
		outcome.Items[len(outcome.Items)-1] = ItemUpdate{
			SourcePath: item.SourcePath, DestinationPath: destinationPath, Status: resultStatus,
			BytesCopied: copied, ErrorCode: "", ErrorMessage: "",
		}
		itemSucceeded := err == nil
		if err != nil {
			code := coordinationErrorCode(err)
			outcome.Items[len(outcome.Items)-1].ErrorCode = code
			outcome.Items[len(outcome.Items)-1].ErrorMessage = tasks.Redact(err.Error())
			if code == "channel_disconnected" || code == "agent_unauthorized" || code == "destination_unavailable" {
				aborted = code
			}
		}
		if itemSucceeded {
			outcome.CopiedBytes += copied
			outcome.TotalBytes += copied
		}
		if progress != nil {
			progress(outcome)
		}
	}
	outcome.Status = outcomeStatus(outcome.Items)
	outcome.CompletedAt = c.now()
	if outcome.Status == TransferCompleted {
		outcome.Message = "all files copied"
	} else {
		outcome.Message = "one or more files were not copied"
	}
	c.auditOutcome(ctx, execution, transfer, outcome, string(outcome.Status))
	return outcome, nil
}

func (c *TransferCoordinator) transferItem(ctx context.Context, spoolDir string, download DownloadTransport, upload UploadTransport, transfer Transfer, item TransferItem) (int64, string, ItemStatus, error) {
	spool, err := os.CreateTemp(spoolDir, "spool-*")
	if err != nil {
		return 0, item.DestinationPath, ItemFailed, fmt.Errorf("%w: %v", ErrSpoolUnavailable, err)
	}
	spoolName := spool.Name()
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spoolName)
	}()
	if err := spool.Chmod(0o600); err != nil {
		return 0, item.DestinationPath, ItemFailed, fmt.Errorf("%w: %v", ErrSpoolUnavailable, err)
	}
	hasher := sha256.New()
	sink := io.MultiWriter(spool, hasher)
	written, err := download.PullFile(ctx, transfer.SourceNodeID, item.SourcePath, sink)
	if err != nil {
		return written, item.DestinationPath, ItemFailed, err
	}
	if written < 0 || written > c.maxFile {
		return written, item.DestinationPath, ItemFailed, ErrFileTooLarge
	}
	if err := spool.Sync(); err != nil {
		return written, item.DestinationPath, ItemFailed, fmt.Errorf("%w: %v", ErrSpoolUnavailable, err)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return written, item.DestinationPath, ItemFailed, fmt.Errorf("%w: %v", ErrSpoolUnavailable, err)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	destinationPath := item.DestinationPath
	var result UploadResult
	if policyUpload, ok := upload.(PolicyUploadTransport); ok {
		result, err = policyUpload.PushFileWithPolicy(ctx, transfer.DestinationNodeID, destinationPath, transfer.ConflictPolicy, spool, written, digest)
	} else {
		if transfer.ConflictPolicy != ConflictOverwrite {
			return written, destinationPath, ItemFailed, fmt.Errorf("%w: transport cannot honor conflict policy", ErrInvalidTransfer)
		}
		if rich, ok := upload.(ResultUploadTransport); ok {
			result, err = rich.PushFileResult(ctx, transfer.DestinationNodeID, destinationPath, spool, written, digest)
		} else {
			err = upload.PushFile(ctx, transfer.DestinationNodeID, destinationPath, spool, written, digest)
			result = UploadResult{DestinationPath: destinationPath, Bytes: written, SHA256: digest}
		}
	}
	if err != nil {
		return written, destinationPath, ItemFailed, err
	}
	if result.DestinationPath != "" {
		destinationPath = result.DestinationPath
	}
	if result.Skipped {
		return 0, destinationPath, ItemSkipped, nil
	}
	if result.Bytes != written || result.SHA256 != digest {
		return written, destinationPath, ItemFailed, ErrAgentChecksum
	}
	return written, destinationPath, ItemCopied, nil
}

func (c *TransferCoordinator) authorizeExecution(ctx context.Context, execution TransferExecution, transfer Transfer) error {
	checks := []auth.Check{
		{SubjectID: execution.SubjectID, Action: ActionRead, NodeIDs: []string{transfer.SourceNodeID}},
		{SubjectID: execution.SubjectID, Action: ActionWrite, NodeIDs: []string{transfer.DestinationNodeID}},
		{SubjectID: execution.SubjectID, Action: ActionTransfer, NodeIDs: []string{transfer.SourceNodeID, transfer.DestinationNodeID}},
	}
	for _, check := range checks {
		if err := c.authorizer.Authorize(ctx, check); err != nil {
			if isPureDenial(err) {
				c.auditDenial(ctx, execution, transfer, check.Action)
			}
			return err
		}
	}
	return nil
}

func (c *TransferCoordinator) auditDenial(ctx context.Context, execution TransferExecution, transfer Transfer, action string) {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	event := audit.Event{
		SubjectID: execution.SubjectID, Action: action, Result: "denied",
		RequestID: execution.RequestID, NodeIDs: []string{transfer.SourceNodeID, transfer.DestinationNodeID},
		Source: audit.SourceSystem, ReasonCode: "transfer_execution_denied", At: c.now(), TaskID: transfer.TaskID,
	}
	_ = c.audit.Append(auditCtx, event)
}

func (c *TransferCoordinator) auditOutcome(ctx context.Context, execution TransferExecution, transfer Transfer, outcome TransferOutcome, reason string) {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result := "success"
	if outcome.Status == TransferPartialFailure || outcome.Status == TransferFailed || outcome.Status == TransferCanceled {
		result = "failure"
	}
	_ = c.audit.Append(auditCtx, audit.Event{
		SubjectID: execution.SubjectID, Action: ActionTransferExecute, Result: result,
		RequestID: execution.RequestID, NodeIDs: []string{transfer.SourceNodeID, transfer.DestinationNodeID},
		Source: audit.SourceSystem, ReasonCode: reason, At: c.now(), TaskID: transfer.TaskID,
	})
}

func outcomeStatus(items []ItemUpdate) TransferStatus {
	copied := 0
	failed := 0
	cancelled := 0
	for _, item := range items {
		switch item.Status {
		case ItemCopied, ItemSkipped:
			copied++
		case ItemFailed:
			failed++
		case ItemCancelled:
			cancelled++
		case ItemQueued, ItemCopying:
			failed++
		}
	}
	switch {
	case copied > 0 && failed == 0 && cancelled == 0:
		return TransferCompleted
	case copied > 0:
		return TransferPartialFailure
	default:
		return TransferFailed
	}
}

func coordinationErrorCode(err error) string {
	contextErr := ""
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, ErrChannelCanceled):
		contextErr = "canceled"
	case errors.Is(err, ErrAgentPathRejected):
		contextErr = "path_rejected"
	case errors.Is(err, ErrAgentSymlink):
		contextErr = "symlink_forbidden"
	case errors.Is(err, ErrInsufficientSpace):
		contextErr = "insufficient_space"
	case errors.Is(err, ErrAgentChecksum):
		contextErr = "checksum_mismatch"
	case errors.Is(err, ErrAgentIO):
		contextErr = "agent_io"
	case errors.Is(err, ErrAgentConflict):
		contextErr = "conflict"
	case errors.Is(err, ErrAgentUnauthorized):
		contextErr = "agent_unauthorized"
	case errors.Is(err, ErrChannelDisconnected):
		contextErr = "channel_disconnected"
	case errors.Is(err, ErrChannelUnavailable), errors.Is(err, ErrTransportAbsent):
		contextErr = "destination_unavailable"
	case errors.Is(err, ErrResumeUnsupported):
		contextErr = "resume_unsupported"
	case errors.Is(err, ErrFileTooLarge):
		contextErr = "file_too_large"
	case errors.Is(err, ErrFileSizeMismatch):
		contextErr = "size_mismatch"
	case errors.Is(err, ErrSpoolUnavailable):
		contextErr = "local_spool_failed"
	case errors.Is(err, ErrTransferIdentity):
		contextErr = "unauthorized"
	default:
		contextErr = "transfer_failed"
	}
	return contextErr
}
