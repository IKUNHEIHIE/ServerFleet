package files

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const (
	ActionRead                 = "files.read"
	ActionWrite                = "files.write"
	ActionTransfer             = "files.transfer"
	ActionTransferCreate       = "files.transfer_create"
	ActionTransferSubmitFailed = "files.transfer_submit_failed"
	ActionTransferCancel       = "files.transfer_cancel"
	ActionTransferExecute      = "files.transfer_execute"
	ActionTransferRecover      = "files.transfer_recover"
	ActionUploadCreate         = "files.upload_create"
	ActionUploadComplete       = "files.upload_complete"
	ActionUploadAbort          = "files.upload_abort"
	ActionDownload             = "files.download"
)

func ServiceActions() []string {
	return []string{ActionDownload, ActionRead, ActionTransfer, ActionTransferCancel, ActionTransferCreate,
		ActionTransferExecute, ActionTransferRecover, ActionTransferSubmitFailed, ActionUploadAbort, ActionUploadComplete, ActionUploadCreate, ActionWrite}
}

var (
	ErrMissingAuthorizer = errors.New("files: authorizer is required")
	ErrCancellerAbsent   = errors.New("files: task canceller is not configured")
	ErrTransportFailure  = errors.New("files: node file transport failed")
)

// AuditSink is the durable audit port.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// TransferCommand is the exact payload submitted to the unified task pipeline.
type TransferCommand struct {
	SubjectID         string
	RequestID         string
	SourceNodeID      string
	DestinationNodeID string
	SourcePaths       []string
	DestinationDir    string
	ConflictPolicy    ConflictPolicy
	Checksums         map[string]string
}

type TaskSubmitter interface {
	SubmitTransfer(ctx context.Context, command TransferCommand) (string, error)
}

type TaskCanceller interface {
	CancelTask(ctx context.Context, taskID, subjectID, requestID string) error
}

// TaskReporter is the unified task result seam. The controller passes the real
// tasks.Service; a nil reporter is allowed only in isolated unit composition.
type TaskReporter interface {
	RecordResult(ctx context.Context, taskID, nodeID string, code tasks.NodeResultCode, message string) (tasks.Task, error)
}

// UploadTransport streams staged bytes to an agent. Implemented by CX; no SSH.
type UploadTransport interface {
	PushFile(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) error
}

// DownloadTransport streams a remote file into the caller's writer.
type DownloadTransport interface {
	PullFile(ctx context.Context, nodeID, remotePath string, sink io.Writer) (int64, error)
}

type Options struct {
	DB             *storage.DB
	Repo           *Repo
	Audit          AuditSink
	Authorizer     auth.Authorizer
	Submitter      TaskSubmitter
	Canceller      TaskCanceller
	Uploads        UploadTransport
	Downloads      DownloadTransport
	Coordinator    *TransferCoordinator
	Tasks          TaskReporter
	StagingDir     string
	MaxUploadBytes int64
	Now            func() time.Time
}

type Service struct {
	db             *storage.DB
	repo           *Repo
	audit          AuditSink
	authorizer     auth.Authorizer
	submitter      TaskSubmitter
	canceller      TaskCanceller
	uploads        UploadTransport
	downloads      DownloadTransport
	coordinator    *TransferCoordinator
	tasks          TaskReporter
	stagingDir     string
	maxUploadBytes int64
	now            func() time.Time

	uploadLocks sync.Map
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("files: database is required")
	case options.Repo == nil:
		return nil, errors.New("files: repository is required")
	case options.Audit == nil:
		return nil, errors.New("files: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	case options.StagingDir == "":
		return nil, errors.New("files: staging directory is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	maxUpload := options.MaxUploadBytes
	if maxUpload <= 0 {
		maxUpload = 512 << 20
	}
	return &Service{
		db: options.DB, repo: options.Repo, audit: options.Audit, authorizer: options.Authorizer,
		submitter: options.Submitter, canceller: options.Canceller, uploads: options.Uploads,
		downloads: options.Downloads, coordinator: options.Coordinator, tasks: options.Tasks,
		stagingDir: options.StagingDir, maxUploadBytes: maxUpload, now: now,
	}, nil
}

// CreateTransfer validates both endpoints, authorizes source read and destination
// write separately, then submits exactly one unified task. A dry run validates and
// returns the planned items without writing or submitting anything.
func (s *Service) CreateTransfer(ctx context.Context, input CreateTransferInput) (Transfer, error) {
	if err := s.requireRequest(input.RequestID); err != nil {
		return Transfer{}, err
	}
	transfer, err := buildTransfer(input, s.now())
	if err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, input.SubjectID, ActionRead, input.RequestID, input.SourceNodeID, "file_source_read"); err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, input.SubjectID, ActionWrite, input.RequestID, input.DestinationNodeID, "file_destination_write"); err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, input.SubjectID, ActionTransfer, input.RequestID, input.SourceNodeID, "file_transfer"); err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, input.SubjectID, ActionTransfer, input.RequestID, input.DestinationNodeID, "file_transfer"); err != nil {
		return Transfer{}, err
	}
	if input.DryRun {
		return transfer, nil
	}
	if input.IdempotencyKey != "" {
		if existing, found, err := s.repo.GetTransferByIdempotency(ctx, input.SubjectID, input.IdempotencyKey); err != nil {
			return Transfer{}, err
		} else if found {
			return existing, nil
		}
	}
	if s.submitter == nil {
		return Transfer{}, ErrTaskSubmitter
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.CreateTransfer(txCtx, tx, transfer); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(input.SubjectID, ActionTransferCreate, "success", input.RequestID,
			[]string{input.SourceNodeID, input.DestinationNodeID}, ""))
	})
	if err != nil {
		return Transfer{}, err
	}
	taskID, submitErr := s.submitter.SubmitTransfer(ctx, TransferCommand{
		SubjectID: input.SubjectID, RequestID: input.RequestID,
		SourceNodeID: input.SourceNodeID, DestinationNodeID: input.DestinationNodeID,
		SourcePaths: snapshots(transfer.Items), DestinationDir: input.DestinationDir,
		ConflictPolicy: input.ConflictPolicy, Checksums: input.Checksums,
	})
	if submitErr != nil {
		failure := "task submission failed"
		_ = s.db.InTx(context.WithoutCancel(ctx), func(txCtx context.Context, tx storage.Tx) error {
			if err := s.repo.MarkTransferFailed(txCtx, tx, transfer.ID, failure); err != nil {
				return err
			}
			return s.audit.AppendTx(txCtx, tx, s.event(input.SubjectID, ActionTransferSubmitFailed, "failure", input.RequestID,
				[]string{input.SourceNodeID, input.DestinationNodeID}, "task_submit_failed"))
		})
		return Transfer{}, fmt.Errorf("%w: %v", ErrTaskSubmitter, submitErr)
	}
	if err := s.repo.AttachTask(ctx, s.db, transfer.ID, taskID, TransferQueued); err != nil {
		return Transfer{}, err
	}
	transfer.TaskID = taskID
	return transfer, nil
}

func (s *Service) GetTransfer(ctx context.Context, subjectID, id string) (Transfer, error) {
	transfer, err := s.repo.GetTransfer(ctx, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, subjectID, ActionRead, "req-read", transfer.SourceNodeID, "file_source_read"); err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, "req-read", transfer.DestinationNodeID, "file_destination_write"); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (s *Service) ListVisibleTransfers(ctx context.Context, subjectID string) ([]Transfer, error) {
	transfers, err := s.repo.ListTransfers(ctx, 100)
	if err != nil {
		return nil, err
	}
	out := make([]Transfer, 0, len(transfers))
	for _, transfer := range transfers {
		if err := s.authorizeNode(ctx, subjectID, ActionRead, "req-list", transfer.SourceNodeID, "file_source_read"); err != nil {
			if isPureDenial(err) {
				continue
			}
			return nil, err
		}
		if err := s.authorizeNode(ctx, subjectID, ActionWrite, "req-list", transfer.DestinationNodeID, "file_destination_write"); err != nil {
			if isPureDenial(err) {
				continue
			}
			return nil, err
		}
		out = append(out, transfer)
	}
	return out, nil
}

type ItemUpdate struct {
	SourcePath      string
	DestinationPath string
	Status          ItemStatus
	BytesCopied     int64
	ErrorCode       string
	ErrorMessage    string
}

type TaskUpdate struct {
	Status      TransferStatus
	Progress    float64
	TotalBytes  *int64
	CopiedBytes *int64
	Message     string
	Items       []ItemUpdate
	StartedAt   time.Time
	CompletedAt time.Time
}

// ApplyTaskUpdate reconciles the persisted transfer with a trusted task result.
// A completed task that does not have copied/skipped items for every source is
// downgraded to partial_failure; success is never invented.
func (s *Service) ApplyTaskUpdate(ctx context.Context, taskID string, update TaskUpdate) (Transfer, error) {
	if !update.Status.Valid() {
		return Transfer{}, ErrInvalidTransfer
	}
	transfer, err := s.repo.GetTransferByTask(ctx, taskID)
	if err != nil {
		return Transfer{}, err
	}
	bySource := make(map[string]int, len(transfer.Items))
	for index, item := range transfer.Items {
		bySource[item.SourcePath] = index
	}
	items := append([]TransferItem(nil), transfer.Items...)
	for _, item := range update.Items {
		index, ok := bySource[item.SourcePath]
		if !ok {
			return Transfer{}, fmt.Errorf("%w: unknown source path", ErrInvalidTransfer)
		}
		if !item.Status.Valid() {
			return Transfer{}, fmt.Errorf("%w: item status", ErrInvalidTransfer)
		}
		items[index].DestinationPath = item.DestinationPath
		items[index].Status = item.Status
		items[index].BytesCopied = item.BytesCopied
		items[index].ErrorCode = item.ErrorCode
		items[index].ErrorMessage = tasks.Redact(item.ErrorMessage)
	}
	status := update.Status
	if status == TransferCompleted && !allItemsSuccessful(items) {
		status = TransferPartialFailure
	}
	completed := update.CompletedAt
	if status.Terminal() && completed.IsZero() {
		completed = s.now()
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		return s.repo.ApplyTaskUpdate(txCtx, tx, transfer.ID, status, update.Progress,
			update.TotalBytes, update.CopiedBytes, tasks.Redact(update.Message), items, update.StartedAt, completed)
	})
	if err != nil {
		return Transfer{}, err
	}
	return s.repo.GetTransfer(ctx, transfer.ID)
}

// RecoverTransfers fences transfers left running by a previous controller. It
// never replays uncertain destination writes.
func (s *Service) RecoverTransfers(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrCoordinatorUnavailable
	}
	recovered, err := s.repo.RecoverRunningTransfers(ctx, s.now())
	if err != nil || recovered == 0 {
		return recovered, err
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	event := audit.Event{
		SubjectID: "file-transfer-service", Action: ActionTransferRecover, Result: "success",
		RequestID: "startup-recovery", Source: audit.SourceSystem,
		ReasonCode: "recovered_unknown", At: s.now(),
	}
	if auditErr := s.audit.Append(auditCtx, event); auditErr != nil {
		return recovered, auditErr
	}
	return recovered, nil
}

// ExecuteTransferTask is the unified task pipeline seam for the files.transfer
// action. It runs the cross-node coordinator and persists the reconciled result;
// per-item failures are stored on the transfer while setup errors are returned.
func (s *Service) ExecuteTransferTask(ctx context.Context, taskID string, execution TransferExecution) (Transfer, error) {
	if s == nil || s.coordinator == nil {
		return Transfer{}, ErrCoordinatorUnavailable
	}
	outcome, runErr := s.coordinator.ExecuteTask(ctx, taskID, execution, nil)
	if outcome.TaskID == "" {
		return Transfer{}, runErr
	}
	transfer, applyErr := s.ApplyTaskUpdate(context.WithoutCancel(ctx), taskID, outcome.TaskUpdate())
	if applyErr != nil {
		return Transfer{}, applyErr
	}
	if reportErr := s.reportTaskResult(ctx, outcome); reportErr != nil {
		return transfer, errors.Join(runErr, reportErr)
	}
	return transfer, runErr
}

func (s *Service) reportTaskResult(ctx context.Context, outcome TransferOutcome) error {
	if s.tasks == nil || outcome.SourceNodeID == "" || outcome.DestinationNodeID == "" {
		return nil
	}
	code := tasks.NodeResultUnknown
	message := "file transfer did not complete; inspect item results"
	switch outcome.Status {
	case TransferCompleted:
		code = tasks.NodeResultSucceeded
		message = "file transfer completed"
	case TransferCanceled:
		code = tasks.NodeResultCanceled
		message = "file transfer canceled"
	}
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var reportErr error
	for _, nodeID := range []string{outcome.SourceNodeID, outcome.DestinationNodeID} {
		if _, err := s.tasks.RecordResult(reportCtx, outcome.TaskID, nodeID, code, message); err != nil {
			reportErr = errors.Join(reportErr, err)
		}
	}
	return reportErr
}

func (s *Service) CancelTransfer(ctx context.Context, subjectID, requestID, id string) (Transfer, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Transfer{}, err
	}
	transfer, err := s.repo.GetTransfer(ctx, id)
	if err != nil {
		return Transfer{}, err
	}
	if transfer.Status.Terminal() {
		return Transfer{}, fmt.Errorf("%w: transfer is terminal", ErrInvalidTransfer)
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, transfer.SourceNodeID, "file_source_read"); err != nil {
		return Transfer{}, err
	}
	if err := s.authorizeNode(ctx, subjectID, ActionWrite, requestID, transfer.DestinationNodeID, "file_destination_write"); err != nil {
		return Transfer{}, err
	}
	if transfer.TaskID != "" {
		if s.canceller == nil {
			return Transfer{}, ErrCancellerAbsent
		}
		if err := s.canceller.CancelTask(ctx, transfer.TaskID, subjectID, requestID); err != nil {
			return Transfer{}, err
		}
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		status := TransferCancelling
		if transfer.TaskID == "" {
			status = TransferCanceled
		}
		if err := s.repo.ApplyTaskUpdate(txCtx, tx, transfer.ID, status, transfer.Progress,
			transfer.TotalBytes, nil, "canceled by operator", nil, time.Time{}, s.now()); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionTransferCancel, "success", requestID,
			[]string{transfer.SourceNodeID, transfer.DestinationNodeID}, ""))
	})
	if err != nil {
		return Transfer{}, err
	}
	return s.repo.GetTransfer(ctx, id)
}

// RecoverPending fails transfers whose task submission never completed. They have
// no task ID and therefore no side effects.
func (s *Service) RecoverPending(ctx context.Context) (int, error) {
	transfers, err := s.repo.ListTransfers(ctx, 500)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, transfer := range transfers {
		if transfer.TaskID != "" || transfer.Status.Terminal() {
			continue
		}
		err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			if err := s.repo.MarkTransferFailed(txCtx, tx, transfer.ID, "task submission did not complete"); err != nil {
				return err
			}
			return s.audit.AppendTx(txCtx, tx, s.event(transfer.SubmitterID, ActionTransferSubmitFailed, "failure",
				transfer.RequestID, []string{transfer.SourceNodeID, transfer.DestinationNodeID}, "task_submit_incomplete"))
		})
		if err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func allItemsSuccessful(items []TransferItem) bool {
	for _, item := range items {
		if item.Status != ItemCopied && item.Status != ItemSkipped {
			return false
		}
	}
	return true
}

func buildTransfer(input CreateTransferInput, now time.Time) (Transfer, error) {
	if !validToken(input.SubjectID, 128) || !validToken(input.SourceNodeID, 128) || !validToken(input.DestinationNodeID, 128) {
		return Transfer{}, fmt.Errorf("%w: subject/nodes", ErrInvalidTransfer)
	}
	if input.SourceNodeID == input.DestinationNodeID {
		return Transfer{}, ErrSameNode
	}
	if input.IdempotencyKey != "" && !validToken(input.IdempotencyKey, 128) {
		return Transfer{}, fmt.Errorf("%w: idempotency key", ErrInvalidTransfer)
	}
	if len(input.SourcePaths) == 0 || len(input.SourcePaths) > 500 {
		return Transfer{}, fmt.Errorf("%w: source paths count", ErrInvalidTransfer)
	}
	if !input.ConflictPolicy.Valid() {
		return Transfer{}, fmt.Errorf("%w: conflict policy", ErrInvalidTransfer)
	}
	if input.FollowSymlinks {
		return Transfer{}, ErrSymlinkForbidden
	}
	if err := ValidateDirectoryPath(input.DestinationDir); err != nil {
		return Transfer{}, err
	}
	seen := make(map[string]struct{}, len(input.SourcePaths))
	items := make([]TransferItem, 0, len(input.SourcePaths))
	for index, source := range input.SourcePaths {
		if err := ValidateRemotePath(source); err != nil {
			return Transfer{}, err
		}
		if source == "/" {
			return Transfer{}, fmt.Errorf("%w: source must be a file", ErrInvalidTransfer)
		}
		if _, duplicate := seen[source]; duplicate {
			return Transfer{}, fmt.Errorf("%w: duplicate source", ErrInvalidTransfer)
		}
		seen[source] = struct{}{}
		destination, err := JoinRemotePath(input.DestinationDir, baseName(source))
		if err != nil {
			return Transfer{}, err
		}
		items = append(items, TransferItem{
			Sequence: index, SourcePath: source, DestinationPath: destination, Status: ItemQueued,
		})
	}
	if input.Checksums != nil {
		if len(input.Checksums) > len(items) {
			return Transfer{}, fmt.Errorf("%w: checksums", ErrInvalidTransfer)
		}
		for source, digest := range input.Checksums {
			if _, ok := seen[source]; !ok || !sha256Pattern.MatchString(digest) {
				return Transfer{}, fmt.Errorf("%w: checksum", ErrInvalidTransfer)
			}
		}
	}
	return Transfer{
		ID: newID("xfer"), SubmitterID: input.SubjectID, SourceNodeID: input.SourceNodeID,
		DestinationNodeID: input.DestinationNodeID, DestinationDir: input.DestinationDir,
		ConflictPolicy: input.ConflictPolicy, FollowSymlinks: input.FollowSymlinks,
		DryRun: input.DryRun, Status: TransferQueued, RequestID: input.RequestID,
		IdempotencyKey: input.IdempotencyKey, Items: items, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func snapshots(items []TransferItem) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.SourcePath)
	}
	return paths
}

func (s *Service) authorizeNode(ctx context.Context, subjectID, action, requestID, nodeID, reason string) error {
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: []string{nodeID}}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) {
		denial := audit.Event{
			SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID,
			NodeIDs: []string{nodeID}, Source: audit.SourceWeb, ReasonCode: reason, At: s.now(),
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("files: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb, ReasonCode: reason, At: s.now(),
	}
}

func (s *Service) requireRequest(requestID string) error {
	if requestID == "" || len(requestID) > 128 {
		return ErrRequestIDRequired
	}
	return nil
}

// UploadLimit returns the configured per-upload byte ceiling.
func (s *Service) UploadLimit() int64 {
	if s == nil {
		return 0
	}
	return s.maxUploadBytes
}

// lockUpload serializes staging mutations for one upload so a content write
// cannot race a completion digest or an abort cleanup.
func (s *Service) lockUpload(uploadID string) func() {
	value, _ := s.uploadLocks.LoadOrStore(uploadID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (s *Service) stagingPath(name string) (string, error) {
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return "", ErrInvalidPath
	}
	return filepath.Join(s.stagingDir, name), nil
}

func newID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("files: entropy source failed")
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func validToken(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func isPureDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
