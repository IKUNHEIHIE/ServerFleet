package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

// Distribution states. DistributionDispatching marks a verified staging
// distribution bound to one unified task; the task worker owns the per-target
// frame-authorized pushes and the terminal states, exactly like uploads.
const (
	DistributionOpen        = "open"
	DistributionDispatching = "dispatching"
	// DistributionPartial is an honest terminal: at least one target succeeded
	// AND at least one target failed/was cancelled/stayed unknown. Successful
	// targets are never replayed to "improve" the batch.
	DistributionPartial   = "partial"
	DistributionCompleted = "completed"
	DistributionFailed    = "failed"
	DistributionCancelled = "cancelled"
	DistributionAborted   = "aborted"
)

// Per-target execution states inside one distribution.
const (
	TargetStatePending     = "pending"
	TargetStateDispatching = "dispatching"
	TargetStateCompleted   = "completed"
	TargetStateFailed      = "failed"
	TargetStateCancelled   = "cancelled"
	TargetStateUnknown     = "unknown"
)

const (
	// MaxDistributionTargets bounds one frozen distribution plan.
	MaxDistributionTargets = 16

	ActionDistributionCreate   = "files.distribution_create"
	ActionDistributionComplete = "files.distribution_complete"
	ActionDistributionCancel   = "files.distribution_cancel"
)

// Distribution is a controller staging file destined for several nodes under
// one frozen plan. NodeID is the staging anchor used for staging-time
// authorization bookkeeping; the authoritative target identities live in
// Targets and are frozen at creation.
type Distribution struct {
	ID              string
	SubjectID       string
	NodeID          string
	DestinationPath string
	Status          string
	BytesReceived   int64
	ExpectedSize    *int64
	SHA256Expected  string
	SHA256Actual    string
	ErrorCode       string
	StagingName     string
	TaskID          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Targets         []DistributionTarget
}

// DistributionTarget persists one frozen target identity and its honest
// per-target outcome. ExecutionID identity is covered by the execution
// binding rows (and childExecutionID(task, node, 0)); Bytes/SHA persist the
// per-target delivered evidence.
type DistributionTarget struct {
	Sequence     int
	NodeID       string
	WritePath    string
	Status       string
	Bytes        int64
	SHA256       string
	ErrorCode    string
	ErrorMessage string
	UpdatedAt    time.Time
}

// DistributionCommand is the frozen distribution dispatch plan submitted to
// the unified task queue. Staging bytes never enter the task parameters.
type DistributionCommand struct {
	SubjectID, RequestID, DistributionID string
	TargetNodes                          []string
	DestinationPath, SHA256Expected      string
	Size                                 int64
}

// DistributionTaskSubmitter is implemented by the trusted program assembly
// when the controlled agent file channel is configured.
type DistributionTaskSubmitter interface {
	SubmitDistribution(ctx context.Context, command DistributionCommand) (string, error)
}

// DistributionTaskUpdate is the trusted per-target terminal outcome reported
// by the task worker. It is the only path out of DistributionDispatching.
type DistributionTaskUpdate struct {
	TargetNode   string
	Status       string
	Bytes        int64
	SHA256       string
	ErrorCode    string
	ErrorMessage string
	RequestID    string
}

var (
	ErrDistributionNotFound = errors.New("files: distribution not found")
	ErrDistributionClosed   = errors.New("files: distribution is not open")
	ErrInvalidTargets       = errors.New("files: invalid distribution targets")
	// ErrAgentOffline marks a frame or target whose node is not currently
	// online; it is an honest per-target failure, never a batch failure.
	ErrAgentOffline = errors.New("files: agent node is offline")
)

// distributionTaskSubmitter returns the trusted assembly port when wired.
func (s *Service) distributionTaskSubmitter() DistributionTaskSubmitter {
	if submitter, ok := s.submitter.(DistributionTaskSubmitter); ok && submitter != nil {
		return submitter
	}
	return nil
}

// CreateDistribution opens a resumable staging file for several destination
// nodes. It does not contact any node; every write frame flows only through
// the unified task worker after CompleteDistribution. The target list is
// frozen here: later service-group or grant changes can never add targets to
// an existing plan, and each execution frame re-checks CURRENT permissions on
// the frozen targets.
func (s *Service) CreateDistribution(ctx context.Context, subjectID, requestID string, targetNodes []string, destinationPath string, expectedSize *int64, sha256Expected string) (Distribution, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Distribution{}, err
	}
	if !validToken(subjectID, 128) {
		return Distribution{}, fmt.Errorf("%w: subject", ErrInvalidTransfer)
	}
	if len(targetNodes) == 0 || len(targetNodes) > MaxDistributionTargets {
		return Distribution{}, ErrInvalidTargets
	}
	seen := make(map[string]bool, len(targetNodes))
	frozen := make([]string, 0, len(targetNodes))
	for _, node := range targetNodes {
		if !validToken(node, 128) || seen[node] {
			return Distribution{}, ErrInvalidTargets
		}
		seen[node] = true
		frozen = append(frozen, node)
	}
	if err := ValidateRemotePath(destinationPath); err != nil {
		return Distribution{}, err
	}
	if destinationPath == "/" {
		return Distribution{}, fmt.Errorf("%w: destination must be a file", ErrInvalidTransfer)
	}
	if expectedSize != nil && (*expectedSize < 0 || *expectedSize > s.maxUploadBytes) {
		return Distribution{}, ErrUploadTooLarge
	}
	if sha256Expected != "" && !sha256Pattern.MatchString(sha256Expected) {
		return Distribution{}, ErrChecksumMismatch
	}
	// Fail closed up front: the subject must currently hold files.write on
	// every frozen target; execution re-checks each frame independently.
	for _, node := range frozen {
		if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, node, "distribution_create"); err != nil {
			return Distribution{}, err
		}
	}
	if s.stagingDir == "" {
		return Distribution{}, ErrInvalidTransfer
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return Distribution{}, fmt.Errorf("files: create staging directory: %w", err)
	}
	now := s.now().UTC()
	stagingName := newID("dst") + ".part"
	staging, err := s.stagingPath(stagingName)
	if err != nil {
		return Distribution{}, err
	}
	file, err := os.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Distribution{}, fmt.Errorf("files: create staging file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(staging)
		return Distribution{}, err
	}
	distribution := Distribution{
		ID: newID("dist"), SubjectID: subjectID, NodeID: frozen[0],
		DestinationPath: destinationPath, Status: DistributionOpen,
		ExpectedSize: expectedSize, SHA256Expected: sha256Expected,
		StagingName: stagingName, CreatedAt: now, UpdatedAt: now,
	}
	distribution.Targets = frozenTargets(frozen, now)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.CreateDistribution(txCtx, tx, distribution); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionDistributionCreate, "success", requestID, frozen, ""))
	})
	if err != nil {
		if path, pathErr := s.stagingPath(stagingName); pathErr == nil {
			_ = os.Remove(path)
		}
		return Distribution{}, err
	}
	return distribution, nil
}

func frozenTargets(nodes []string, now time.Time) []DistributionTarget {
	targets := make([]DistributionTarget, 0, len(nodes))
	for i, node := range nodes {
		targets = append(targets, DistributionTarget{Sequence: i, NodeID: node, Status: TargetStatePending, UpdatedAt: now})
	}
	return targets
}

// GetDistribution returns the distribution and reconciles a dispatching plan
// lazily against the persisted per-target task results, mirroring GetUpload.
func (s *Service) GetDistribution(ctx context.Context, subjectID, requestID, distributionID string) (Distribution, error) {
	distribution, err := s.repo.GetDistribution(ctx, distributionID)
	if err != nil {
		return Distribution{}, err
	}
	if distribution.SubjectID != subjectID {
		return Distribution{}, ErrSubjectMismatch
	}
	if distribution.Status == DistributionDispatching {
		distribution = s.reconcileDistribution(ctx, distribution)
	}
	return distribution, nil
}

// WriteDistributionContent appends staging bytes at the exact offset. The
// frozen target list is not re-authorized per chunk; CURRENT permissions are
// enforced per frame by the task worker, so a mid-staging revocation fails
// that target at execution without silently expanding or shrinking the plan.
func (s *Service) WriteDistributionContent(ctx context.Context, subjectID, requestID, distributionID string, offset int64, content io.Reader) (Distribution, error) {
	if content == nil {
		return Distribution{}, fmt.Errorf("%w: content", ErrInvalidTransfer)
	}
	unlock := s.lockUpload(distributionID)
	defer unlock()
	distribution, err := s.repo.GetDistribution(ctx, distributionID)
	if err != nil {
		return Distribution{}, err
	}
	if distribution.SubjectID != subjectID {
		return Distribution{}, ErrSubjectMismatch
	}
	if distribution.Status != DistributionOpen {
		return Distribution{}, ErrDistributionClosed
	}
	if offset < 0 || offset != distribution.BytesReceived {
		return Distribution{}, ErrOffsetConflict
	}
	remaining := s.maxUploadBytes - offset
	if distribution.ExpectedSize != nil && *distribution.ExpectedSize-offset < remaining {
		remaining = *distribution.ExpectedSize - offset
	}
	if remaining < 0 {
		return Distribution{}, ErrUploadTooLarge
	}
	file, err := s.openStagingAppend(distribution.StagingName, offset)
	if err != nil {
		return Distribution{}, err
	}
	written, copyErr := io.Copy(file, io.LimitReader(content, remaining+1))
	if copyErr != nil {
		_ = file.Truncate(offset)
		_ = file.Close()
		if ctx.Err() != nil {
			return Distribution{}, ctx.Err()
		}
		return Distribution{}, fmt.Errorf("files: write staging content: %w", copyErr)
	}
	if written > remaining {
		_ = file.Truncate(offset)
		_ = file.Sync()
		_ = file.Close()
		return Distribution{}, ErrUploadTooLarge
	}
	if written == 0 {
		_ = file.Close()
		return Distribution{}, fmt.Errorf("%w: empty content", ErrInvalidTransfer)
	}
	if err := file.Sync(); err != nil {
		_ = file.Truncate(offset)
		_ = file.Close()
		return Distribution{}, err
	}
	next := offset + written
	if err := s.repo.UpdateDistributionProgress(ctx, s.db, distribution.ID, next); err != nil {
		_ = file.Truncate(offset)
		_ = file.Sync()
		_ = file.Close()
		return Distribution{}, err
	}
	if err := file.Close(); err != nil {
		return Distribution{}, err
	}
	distribution.BytesReceived = next
	distribution.UpdatedAt = s.now()
	return distribution, nil
}

// CompleteDistribution verifies the staged digest and moves the frozen plan
// into the sole unified task queue. No transport claim is made here; the
// worker owns the frame-authorized pushes and the per-target outcomes.
func (s *Service) CompleteDistribution(ctx context.Context, subjectID, requestID, distributionID string) (Distribution, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Distribution{}, err
	}
	unlock := s.lockUpload(distributionID)
	defer unlock()
	distribution, err := s.repo.GetDistribution(ctx, distributionID)
	if err != nil {
		return Distribution{}, err
	}
	if distribution.SubjectID != subjectID {
		return Distribution{}, ErrSubjectMismatch
	}
	for _, node := range distributionTargetNodes(distribution) {
		if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, node, "distribution_complete"); err != nil {
			return Distribution{}, err
		}
	}
	if distribution.Status == DistributionCompleted || distribution.Status == DistributionPartial {
		return distribution, nil
	}
	if distribution.Status != DistributionOpen {
		return Distribution{}, ErrDistributionClosed
	}
	if distribution.ExpectedSize != nil && distribution.BytesReceived != *distribution.ExpectedSize {
		return Distribution{}, fmt.Errorf("%w: received %d of %d bytes", ErrInvalidTransfer, distribution.BytesReceived, *distribution.ExpectedSize)
	}
	staging, err := s.stagingPath(distribution.StagingName)
	if err != nil {
		return Distribution{}, err
	}
	digest, err := s.stagingDigest(distribution.StagingName, distribution.BytesReceived)
	if err != nil {
		return Distribution{}, err
	}
	if distribution.SHA256Expected != "" && digest != distribution.SHA256Expected {
		err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			moved, err := s.repo.FailOpenDistribution(txCtx, tx, distribution.ID, digest, "checksum_mismatch")
			if err != nil {
				return err
			}
			if !moved {
				return ErrDistributionClosed
			}
			return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionDistributionComplete, "failure", requestID, distributionTargetNodes(distribution), "checksum_mismatch"))
		})
		if err != nil {
			return Distribution{}, err
		}
		// Retain staging if the durable rejection/audit did not commit. After a
		// committed rejection it can never be submitted or replayed.
		_ = os.Remove(staging)
		return Distribution{}, ErrChecksumMismatch
	}
	submitter := s.distributionTaskSubmitter()
	if submitter == nil {
		return Distribution{}, ErrTaskSubmitter
	}
	taskID, submitErr := submitter.SubmitDistribution(ctx, DistributionCommand{
		SubjectID: subjectID, RequestID: requestID, DistributionID: distribution.ID,
		TargetNodes: distributionTargetNodes(distribution), DestinationPath: distribution.DestinationPath,
		SHA256Expected: digest, Size: distribution.BytesReceived,
	})
	if submitErr != nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.audit.Append(auditCtx, s.event(subjectID, ActionTransferSubmitFailed, "failure", requestID, distributionTargetNodes(distribution), "distribution_submit_failed"))
		return Distribution{}, fmt.Errorf("%w: %v", ErrTaskSubmitter, submitErr)
	}
	if err := s.BindDistributionTask(ctx, distribution, taskID, digest); err != nil {
		return Distribution{}, err
	}
	distribution.Status = DistributionDispatching
	distribution.TaskID = taskID
	distribution.SHA256Actual = digest
	return distribution, nil
}

// BindDistributionTask durably binds a verified staging distribution to its
// unified task in a separate transaction after submission, exactly like
// SubmitUploadTask: no child effects before the exact linkage exists.
func (s *Service) BindDistributionTask(ctx context.Context, distribution Distribution, taskID, verifiedSHA string) error {
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		bound, err := s.repo.BindDistributionTask(txCtx, tx, distribution.ID, taskID, verifiedSHA)
		if err != nil {
			return err
		}
		if !bound {
			return ErrDistributionNotFound
		}
		return nil
	})
}

// ApplyDistributionTaskUpdate persists one trusted per-target terminal
// outcome and aggregates the distribution terminal state from the persisted
// target rows. Successful targets keep their results even when other targets
// fail, are cancelled or stay unknown.
func (s *Service) ApplyDistributionTaskUpdate(ctx context.Context, taskID string, update DistributionTaskUpdate) (bool, error) {
	distribution, err := s.repo.GetDistributionByTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	if distribution.Status != DistributionDispatching {
		return true, nil
	}
	applied, err := s.repo.FinishDistributionTarget(ctx, s.db, distribution.ID, update.TargetNode, update.Status, update.Bytes, update.SHA256, update.ErrorCode, update.ErrorMessage)
	if err != nil || !applied {
		return applied, err
	}
	if update.RequestID != "" {
		_ = s.audit.Append(ctx, s.event(distribution.SubjectID, ActionDistributionComplete, "distribution_target_"+update.Status, update.RequestID, []string{update.TargetNode}, update.ErrorCode))
	}
	// Aggregate a terminal distribution state once every frozen target row is
	// terminal; unknown stays a valid non-terminal honest state.
	aggregated, err := s.aggregateDistribution(ctx, distribution)
	return aggregated, err
}

func (s *Service) aggregateDistribution(ctx context.Context, distribution Distribution) (bool, error) {
	targets, err := s.repo.ListDistributionTargets(ctx, distribution.ID)
	if err != nil {
		return false, err
	}
	if len(targets) == 0 {
		return false, ErrInvalidTargets
	}
	pending, succeeded, failed, cancelled, unknown := 0, 0, 0, 0, 0
	for _, target := range targets {
		switch target.Status {
		case TargetStatePending, TargetStateDispatching:
			pending++
		case TargetStateCompleted:
			succeeded++
		case TargetStateFailed:
			failed++
		case TargetStateCancelled:
			cancelled++
		case TargetStateUnknown:
			unknown++
		default:
			return false, ErrInvalidTargets
		}
	}
	if pending > 0 || unknown > 0 {
		return false, nil
	}
	status, errorCode := DistributionFailed, "distribution_failed"
	switch {
	case succeeded == len(targets):
		status, errorCode = DistributionCompleted, ""
	case succeeded > 0:
		status, errorCode = DistributionPartial, "partial_target_failures"
	case cancelled == len(targets):
		status, errorCode = DistributionCancelled, "cancelled"
	case cancelled > 0:
		status, errorCode = DistributionPartial, "partial_target_failures"
	}
	return s.repo.FinishDistribution(ctx, s.db, distribution.ID, status, distribution.SHA256Actual, errorCode)
}

// CancelDistribution moves a dispatching distribution into the unified
// cancellation path, per target. Targets that never received a write frame
// converge to cancelled; targets with delivered evidence keep the worker's
// own honest terminal outcome.
func (s *Service) CancelDistribution(ctx context.Context, subjectID, requestID, distributionID string) (Distribution, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Distribution{}, err
	}
	unlock := s.lockUpload(distributionID)
	defer unlock()
	distribution, err := s.repo.GetDistribution(ctx, distributionID)
	if err != nil {
		return Distribution{}, err
	}
	if distribution.SubjectID != subjectID {
		return Distribution{}, ErrSubjectMismatch
	}
	switch distribution.Status {
	case DistributionOpen:
		if err := s.AbortDistribution(ctx, subjectID, requestID, distributionID); err != nil {
			return Distribution{}, err
		}
		return s.repo.GetDistribution(ctx, distributionID)
	case DistributionDispatching:
	default:
		return distribution, nil
	}
	if s.canceller == nil {
		return Distribution{}, ErrCancellerAbsent
	}
	if err := s.canceller.CancelTask(ctx, distribution.TaskID, subjectID, requestID); err != nil {
		return Distribution{}, err
	}
	return s.repo.GetDistribution(ctx, distributionID)
}

// AbortDistribution removes the staging bytes of an open distribution.
func (s *Service) AbortDistribution(ctx context.Context, subjectID, requestID, distributionID string) error {
	distribution, err := s.repo.GetDistribution(ctx, distributionID)
	if err != nil {
		return err
	}
	if distribution.SubjectID != subjectID {
		return ErrSubjectMismatch
	}
	if distribution.Status != DistributionOpen {
		return ErrDistributionClosed
	}
	staging, err := s.stagingPath(distribution.StagingName)
	if err != nil {
		return err
	}
	_ = os.Remove(staging)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.AbortDistributionRow(txCtx, tx, distribution.ID); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionDistributionCancel, "success", requestID, distributionTargetNodes(distribution), "aborted"))
	})
	return err
}

// ReconcileDistributionTasks converges dispatching distributions after a
// restart from the persisted per-target task results, without replaying any
// side effect.
func (s *Service) ReconcileDistributionTasks(ctx context.Context) (int, error) {
	ids, err := s.repo.ListDispatchingDistributions(ctx)
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for _, id := range ids {
		distribution, err := s.repo.GetDistribution(ctx, id)
		if err != nil {
			continue
		}
		if s.reconcileDistribution(ctx, distribution).Status != DistributionDispatching {
			reconciled++
		}
	}
	return reconciled, nil
}

// reconcileDistribution converges per-target rows from the unified task's
// persisted per-target results and then aggregates the terminal state.
func (s *Service) reconcileDistribution(ctx context.Context, distribution Distribution) Distribution {
	if distribution.TaskID == "" {
		return distribution
	}
	reader := s.taskOutcomeReader()
	if reader == nil {
		return distribution
	}
	task, err := reader.ReadTaskOutcome(ctx, distribution.TaskID)
	if err != nil {
		return distribution
	}
	byNode := make(map[string]tasks.Target, len(task.Targets))
	for _, target := range task.Targets {
		byNode[target.NodeID] = target
	}
	for i := range distribution.Targets {
		target := &distribution.Targets[i]
		if target.Status != TargetStatePending && target.Status != TargetStateDispatching {
			continue
		}
		result, ok := byNode[target.NodeID]
		if !ok {
			continue
		}
		if result.Code == tasks.NodeResultUnknown && !task.Status.Terminal() {
			continue
		}
		if result.Code == "" && task.Status.Terminal() {
			// A task that never dispatched its targets (queued cancellation,
			// expiry or rejection) still carries an honest task-level terminal
			// outcome every frozen row converges to.
			switch task.Status {
			case tasks.StatusCanceled:
				result.Code = tasks.NodeResultCanceled
				result.Message = "task cancelled before dispatch"
			case tasks.StatusExpired:
				result.Code = tasks.NodeResultTimedOut
				result.Message = "offline wait expired"
			case tasks.StatusFailed:
				result.Code = tasks.NodeResultFailed
				result.Message = "task failed: " + task.FailureReason
			}
		}
		switch result.Code {
		case tasks.NodeResultSucceeded:
			_, _ = s.repo.FinishDistributionTarget(ctx, s.db, distribution.ID, target.NodeID, TargetStateCompleted, 0, distribution.SHA256Actual, "", "")
		case tasks.NodeResultFailed:
			_, _ = s.repo.FinishDistributionTarget(ctx, s.db, distribution.ID, target.NodeID, TargetStateFailed, 0, "", "task_failed", result.Message)
		case tasks.NodeResultCanceled:
			_, _ = s.repo.FinishDistributionTarget(ctx, s.db, distribution.ID, target.NodeID, TargetStateCancelled, 0, "", "task_cancelled", result.Message)
		case tasks.NodeResultTimedOut:
			_, _ = s.repo.FinishDistributionTarget(ctx, s.db, distribution.ID, target.NodeID, TargetStateFailed, 0, "", "task_expired", result.Message)
		default:
			// Unknown/empty stays honest and non-terminal; no replay.
		}
	}
	// Aggregate from durable target rows. A worker may finish another target
	// after this call's initial snapshot, so never combine a new parent status
	// with that older target list or report an ignored target write as success.
	_, _ = s.aggregateDistribution(ctx, distribution)
	if current, err := s.repo.GetDistribution(ctx, distribution.ID); err == nil {
		return current
	}
	// A read failure leaves the original conservative snapshot unchanged.
	return distribution
}

func distributionTargetNodes(distribution Distribution) []string {
	nodes := make([]string, 0, len(distribution.Targets))
	for _, target := range distribution.Targets {
		nodes = append(nodes, target.NodeID)
	}
	return nodes
}

// DistributionStaging opens the verified staging file bound to a dispatched
// distribution task for worker streaming. It refuses anything but an exact
// dispatching binding with byte-consistent staging.
func (s *Service) DistributionStaging(ctx context.Context, taskID string) (*os.File, Distribution, error) {
	distribution, err := s.repo.GetDistributionByTask(ctx, taskID)
	if err != nil {
		return nil, Distribution{}, err
	}
	if distribution.Status != DistributionDispatching || distribution.TaskID != taskID {
		return nil, Distribution{}, ErrDistributionClosed
	}
	staging, err := s.stagingPath(distribution.StagingName)
	if err != nil {
		return nil, Distribution{}, err
	}
	file, err := os.OpenFile(staging, os.O_RDONLY, 0o600)
	if err != nil {
		return nil, Distribution{}, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != distribution.BytesReceived {
		_ = file.Close()
		return nil, Distribution{}, fmt.Errorf("files: staged bytes are inconsistent with the recorded size")
	}
	return file, distribution, nil
}
