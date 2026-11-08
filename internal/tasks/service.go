package tasks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionSubmit           = "tasks.submit"
	ActionApprove          = "tasks.approve"
	ActionRevise           = "tasks.revise"
	ActionCancel           = "tasks.cancel"
	ActionResume           = "tasks.resume"
	ActionDispatchDenied   = "tasks.dispatch_denied"
	ActionFinished         = "tasks.finished"
	ActionApprovalConsumed = "tasks.approval_consumed"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionApprovalConsumed, ActionApprove, ActionCancel, ActionDispatchDenied,
		ActionFinished, ActionResume, ActionRevise, ActionSubmit}
}

// AuditSink is the durable audit port.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// DispatchResult classifies the transport answer. Accepted means the command was
// handed to the agent channel; Unknown means it may or may not have executed.
type DispatchResult struct {
	Accepted bool
	Unknown  bool
	Reason   string
}

// Command is the only execution payload this package emits. The transport owner
// converts it to the agent Protobuf frame; no other execution path is permitted.
type Command struct {
	TaskID   string
	NodeID   string
	ActionID string
	Params   json.RawMessage
}

// CancelCommand asks the transport to interrupt one dispatched target.
type CancelCommand struct {
	TaskID string
	NodeID string
}

// Dispatcher is implemented by the CX transport adapter.
type Dispatcher interface {
	Dispatch(ctx context.Context, command Command) (DispatchResult, error)
	Cancel(ctx context.Context, command CancelCommand) error
}

// ApprovalConsumer atomically consumes an external approval bound to the subject,
// plan digest and target snapshot. It MUST use the provided transaction so a
// failed submission rolls the consumption back. It returns the approver identity.
type ApprovalConsumer interface {
	ConsumeApproval(ctx context.Context, tx storage.Tx, approvalID, subjectID, planDigest string, nodeIDs []string) (string, error)
}

// NodeLookup reports trusted node availability. A nil lookup makes dispatch fail
// closed rather than assuming nodes are online.
type NodeLookup interface {
	Online(ctx context.Context, nodeID string) (bool, error)
}

// Options configures the unified task service.
type Options struct {
	ControllerExecution *ControllerExecution
	DB                  *storage.DB
	Repo                *Repo
	Audit               AuditSink
	Authorizer          auth.Authorizer
	Dispatcher          Dispatcher
	Workers             map[string]Worker
	RuntimeGate         func(context.Context) error
	Approvals           ApprovalConsumer
	Nodes               NodeLookup
	Now                 func() time.Time
	ApprovalTTL         time.Duration
	UnstartedTTL        time.Duration
	Interval            time.Duration
	Concurrency         int
	RequireApproval     func(OperationClass) bool
	OnError             func(error)
}

type Service struct {
	controller      *ControllerExecution
	runtimeGate     func(context.Context) error
	db              *storage.DB
	repo            *Repo
	audit           AuditSink
	authorizer      auth.Authorizer
	dispatcher      Dispatcher
	approvals       ApprovalConsumer
	nodes           NodeLookup
	now             func() time.Time
	approvalTTL     time.Duration
	unstartedTTL    time.Duration
	interval        time.Duration
	concurrency     int
	requireApproval func(OperationClass) bool
	onError         func(error)

	runMu      sync.Mutex
	taskLocks  sync.Map
	workers    map[string]Worker
	workerMu   sync.Mutex
	workerRuns map[string]*workerRun
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("tasks: database is required")
	case options.Repo == nil:
		return nil, errors.New("tasks: repository is required")
	case options.Audit == nil:
		return nil, errors.New("tasks: audit sink is required")
	case options.Authorizer == nil:
		return nil, errors.New("tasks: authorizer is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	approvalTTL := options.ApprovalTTL
	if approvalTTL == 0 {
		approvalTTL = 30 * time.Minute
	}
	unstartedTTL := options.UnstartedTTL
	if unstartedTTL == 0 {
		unstartedTTL = DefaultUnstartedOfflineWait
	}
	interval := options.Interval
	if interval == 0 {
		interval = 2 * time.Second
	}
	concurrency := options.Concurrency
	if concurrency == 0 {
		concurrency = DefaultStandardBatchSize
	}
	require := options.RequireApproval
	if require == nil {
		require = func(class OperationClass) bool {
			return class == OperationClassRoot || class == OperationClassModifying
		}
	}
	onError := options.OnError
	if onError == nil {
		onError = func(error) {}
	}
	workers := make(map[string]Worker, len(options.Workers))
	for action, worker := range options.Workers {
		if !validToken(action, 128) || worker == nil {
			return nil, ErrInvalidTask
		}
		workers[action] = worker
	}
	controller, err := cloneControllerExecution(options.ControllerExecution, workers)
	if err != nil {
		return nil, err
	}
	return &Service{
		runtimeGate: options.RuntimeGate, controller: controller,
		db: options.DB, repo: options.Repo, audit: options.Audit, authorizer: options.Authorizer,
		dispatcher: options.Dispatcher, approvals: options.Approvals, nodes: options.Nodes, now: now,
		approvalTTL: approvalTTL, unstartedTTL: unstartedTTL, interval: interval,
		concurrency: concurrency, requireApproval: require, onError: onError,
		workers: workers, workerRuns: map[string]*workerRun{},
	}, nil
}

// SubmitRequest is a trusted, already authenticated submission. TargetNodeIDs is
// the exact frozen snapshot; group resolution happens before Submit.
type SubmitRequest struct {
	SubjectID        string
	RequestID        string
	Name             string
	ActionID         string
	OperationClass   OperationClass
	TargetType       string
	TargetResourceID string
	TargetNodeIDs    []string
	Params           map[string]string
	CanonicalParams  []byte
	IdempotencyKey   string
	RequireApproval  bool
	ScopeRevision    uint64
	Source           audit.Source
	PluginID         string
	ApprovalID       string
}

// Submit persists a task and its frozen target snapshot. Authorization runs
// before any write; the audit record is written in the same transaction.
func (s *Service) Submit(ctx context.Context, request SubmitRequest) (Task, error) {
	if err := s.requireRequest(request.RequestID); err != nil {
		return Task{}, err
	}
	if !validToken(request.SubjectID, 128) || !validToken(request.ActionID, 128) {
		return Task{}, fmt.Errorf("%w: subject/action", ErrInvalidTask)
	}
	if !validLabel(request.Name, 128) {
		return Task{}, fmt.Errorf("%w: name", ErrInvalidTask)
	}
	if !request.OperationClass.Valid() {
		return Task{}, ErrUnknownOperationClass
	}
	snapshot, err := NewTargetSnapshot(request.TargetNodeIDs)
	if err != nil {
		return Task{}, err
	}
	targetType := request.TargetType
	if targetType == "" {
		targetType = "node_ids"
	}
	switch targetType {
	case TargetTypeController:
		if ctx.Value(controllerAdmissionKey{}) != s || s.controller == nil || request.TargetResourceID != s.controller.AuthorityID || snapshot.Len() != 1 || snapshot.IDs()[0] != s.controller.AuthorityID || request.ScopeRevision != 0 || request.PluginID != "" || request.ApprovalID != "" || request.OperationClass == OperationClassRoot {
			return Task{}, ErrInvalidTask
		}
	case "node_ids":
		if request.TargetResourceID != "" {
			return Task{}, fmt.Errorf("%w: target resource id", ErrInvalidTask)
		}
	case "node_group", "service_group":
		if !validToken(request.TargetResourceID, 128) {
			return Task{}, fmt.Errorf("%w: target resource id", ErrInvalidTask)
		}
	default:
		return Task{}, fmt.Errorf("%w: target type", ErrInvalidTask)
	}
	params, err := paramsFromSubmit(request)
	if err != nil {
		return Task{}, err
	}
	if request.IdempotencyKey != "" && !validToken(request.IdempotencyKey, 128) {
		return Task{}, fmt.Errorf("%w: idempotency key", ErrInvalidTask)
	}
	source := request.Source
	if source == "" {
		source = audit.SourceWeb
	}
	if !knownAuditSource(source) {
		return Task{}, fmt.Errorf("%w: source", ErrInvalidTask)
	}
	if request.PluginID != "" && !validToken(request.PluginID, 128) {
		return Task{}, fmt.Errorf("%w: plugin id", ErrInvalidTask)
	}
	if source == audit.SourcePlugin && request.PluginID == "" {
		return Task{}, fmt.Errorf("%w: plugin id is required", ErrInvalidTask)
	}
	admission := Task{ActionID: request.ActionID, TargetType: targetType, TargetResourceID: request.TargetResourceID, ScopeRevision: request.ScopeRevision, PluginID: request.PluginID, Source: string(source), OperationClass: request.OperationClass, Targets: []Target{{NodeID: snapshot.IDs()[0]}}}
	if targetType != TargetTypeController {
		admission.Targets = nil
	}
	if err := s.authorizeForTask(ctx, admission, request.SubjectID, request.ActionID, request.RequestID, snapshot.IDs(), "task_submit"); err != nil {
		return Task{}, err
	}
	if existing, found, err := s.repo.GetByIdempotencyKey(ctx, request.SubjectID, request.IdempotencyKey); err != nil {
		return Task{}, err
	} else if found {
		if existing.ActionID != request.ActionID || existing.OperationClass != request.OperationClass ||
			existing.Params != params || existing.TargetType != targetType || existing.TargetResourceID != request.TargetResourceID || !sameTargetSet(existing.Targets, snapshot.IDs()) {
			return Task{}, ErrIdempotencyMismatch
		}
		return existing, nil
	}

	now := s.now().UTC()
	plan, err := NewDefaultBatchPlan(snapshot, request.OperationClass)
	if err != nil {
		return Task{}, err
	}
	targets := buildTargets(snapshot.IDs(), plan)
	approvalRequired := request.RequireApproval || s.requireApproval(request.OperationClass)
	if targetType == TargetTypeController {
		rule, err := s.controllerRule(admission)
		if err != nil {
			return Task{}, err
		}
		approvalRequired = approvalRequired || rule.RequireApproval
	}
	status := StatusQueued
	if approvalRequired {
		status = StatusAwaitingApproval
	}
	externalApproval := request.ApprovalID != ""
	if externalApproval {
		if s.approvals == nil {
			return Task{}, ErrApprovalConsumerAbsent
		}
		approvalRequired = true
		status = StatusQueued
	}
	task := Task{
		ID: newTaskID(), Name: request.Name, SubmitterID: request.SubjectID,
		ActionID: request.ActionID, OperationClass: request.OperationClass, Params: params,
		IdempotencyKey: request.IdempotencyKey, Revision: 1, Status: status,
		ApprovalRequired: approvalRequired, BatchIndex: 0, TargetType: targetType,
		TargetResourceID:  request.TargetResourceID,
		UnstartedDeadline: now.Add(s.unstartedTTL), RequestID: request.RequestID,
		ScopeRevision: request.ScopeRevision, PluginID: request.PluginID, Source: string(source),
		ApprovalRef: request.ApprovalID, CreatedAt: now, UpdatedAt: now, Targets: targets,
	}
	task.PlanHash = planHash(task)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Create(txCtx, tx, task, targets); err != nil {
			if storage.IsUniqueViolation(err) {
				// Concurrent duplicate submission: return the winner outside.
				return errDuplicateSubmit
			}
			return err
		}
		eventType := "created"
		if approvalRequired {
			eventType = "approval_required"
		}
		if err := s.repo.AppendEvent(txCtx, tx, task.ID, eventType, map[string]any{
			"status": string(status), "revision": task.Revision,
		}, now); err != nil {
			return err
		}
		if externalApproval {
			approver, err := s.approvals.ConsumeApproval(txCtx, tx, request.ApprovalID, request.SubjectID, task.PlanHash, snapshot.IDs())
			if err != nil {
				return err
			}
			if !validToken(approver, 128) || approver == request.SubjectID {
				return ErrSelfApproval
			}
			if err := s.repo.InsertApproval(txCtx, tx, Approval{
				TaskID: task.ID, Revision: task.Revision, ApproverID: approver,
				Decision: "approved", PlanHash: task.PlanHash, Reason: "external approval",
				DecidedAt: now,
			}); err != nil {
				return err
			}
			if err := s.repo.AppendEvent(txCtx, tx, task.ID, "approval_consumed", map[string]any{
				"revision": task.Revision, "approver": approver,
			}, now); err != nil {
				return err
			}
			consumed := s.event(approver, ActionApprovalConsumed, "success", request.RequestID, snapshot.IDs(), "")
			consumed.TaskID = task.ID
			if err := s.audit.AppendTx(txCtx, tx, consumed); err != nil {
				return err
			}
		}
		event := s.event(request.SubjectID, ActionSubmit, "success", request.RequestID, auditSnapshotIDs(targetType, snapshot.IDs()), "")
		event.Source = source
		event.TaskID = task.ID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if errors.Is(err, errDuplicateSubmit) {
		existing, found, lookupErr := s.repo.GetByIdempotencyKey(ctx, request.SubjectID, request.IdempotencyKey)
		if lookupErr != nil {
			return Task{}, lookupErr
		}
		if found {
			return existing, nil
		}
		return Task{}, ErrInvalidTask
	}
	if err != nil {
		return Task{}, err
	}
	task.Targets = targets
	return task, nil
}

var errDuplicateSubmit = errors.New("tasks: duplicate idempotent submission")

// Decision is one human/automation approval decision.
type Decision struct {
	ApproverID string
	RequestID  string
	Approve    bool
	Reason     string
}

// Decide records a revision-bound approval or rejection. Self-approval is
// rejected; a stale revision never reaches execution.
func (s *Service) Decide(ctx context.Context, taskID string, expectedRevision uint64, decision Decision) (Task, error) {
	if err := s.requireRequest(decision.RequestID); err != nil {
		return Task{}, err
	}
	if !validToken(decision.ApproverID, 128) {
		return Task{}, fmt.Errorf("%w: approver", ErrInvalidTask)
	}
	if decision.Reason != "" && !validLabel(decision.Reason, 300) {
		return Task{}, fmt.Errorf("%w: reason", ErrInvalidTask)
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if task.Status != StatusAwaitingApproval {
		return Task{}, fmt.Errorf("%w: task is %s", ErrInvalidStatus, task.Status)
	}
	if task.Revision != expectedRevision {
		return Task{}, ErrApprovalInvalid
	}
	if decision.ApproverID == task.SubmitterID {
		return Task{}, ErrSelfApproval
	}
	targetIDs := taskTargetIDs(task)
	if err := s.authorizeForTask(ctx, task, decision.ApproverID, ActionApprove, decision.RequestID, targetIDs, "task_approve"); err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	outcomeStatus := StatusQueued
	record := "allowed"
	approvalDecision := "approved"
	eventType := "approval_granted"
	if !decision.Approve {
		outcomeStatus = StatusFailed
		record = "denied"
		approvalDecision = "rejected"
		eventType = "approval_rejected"
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.InsertApproval(txCtx, tx, Approval{
			TaskID: taskID, Revision: task.Revision, ApproverID: decision.ApproverID,
			Decision: approvalDecision, PlanHash: task.PlanHash, Reason: decision.Reason, DecidedAt: now,
		}); err != nil {
			return err
		}
		finished := time.Time{}
		if !decision.Approve {
			finished = now
		}
		moved, err := s.repo.Transition(txCtx, tx, taskID, StatusAwaitingApproval, outcomeStatus, decision.Reason, time.Time{}, finished)
		if err != nil {
			return err
		}
		if !moved {
			return ErrInvalidStatus
		}
		if err := s.repo.AppendEvent(txCtx, tx, taskID, eventType, map[string]any{
			"revision": task.Revision, "approver": decision.ApproverID,
		}, now); err != nil {
			return err
		}
		reasonCode := decision.Reason
		if reasonCode != "" && !validToken(reasonCode, audit.DefaultLimits().MaxReasonCodeBytes) {
			reasonCode = approvalDecision
		}
		event := s.event(decision.ApproverID, ActionApprove, record, decision.RequestID, taskAuditIDs(task), reasonCode)
		event.TaskID = taskID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, taskID)
}

// ReviseRequest replaces the frozen snapshot and params, advances the revision and
// invalidates every prior approval.
type ReviseRequest struct {
	SubjectID        string
	RequestID        string
	ExpectedRevision uint64
	TargetNodeIDs    []string
	Params           map[string]string
}

func (s *Service) Revise(ctx context.Context, taskID string, request ReviseRequest) (Task, error) {
	if err := s.requireRequest(request.RequestID); err != nil {
		return Task{}, err
	}
	if !validToken(request.SubjectID, 128) {
		return Task{}, fmt.Errorf("%w: subject", ErrInvalidTask)
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if task.TargetType == TargetTypeController {
		return Task{}, ErrInvalidTask
	}
	if task.Revision != request.ExpectedRevision {
		return Task{}, ErrInvalidStatus
	}
	if task.Status != StatusAwaitingApproval && task.Status != StatusQueued {
		return Task{}, fmt.Errorf("%w: cannot revise %s", ErrInvalidStatus, task.Status)
	}
	if !task.StartedAt.IsZero() {
		return Task{}, fmt.Errorf("%w: task already started", ErrInvalidStatus)
	}
	snapshot, err := NewTargetSnapshot(request.TargetNodeIDs)
	if err != nil {
		return Task{}, err
	}
	params, err := normalizeParams(request.Params)
	if err != nil {
		return Task{}, err
	}
	if err := s.authorize(ctx, request.SubjectID, ActionRevise, request.RequestID, snapshot.IDs(), "task_revise"); err != nil {
		return Task{}, err
	}
	plan, err := NewDefaultBatchPlan(snapshot, task.OperationClass)
	if err != nil {
		return Task{}, err
	}
	targets := buildTargets(snapshot.IDs(), plan)
	task.Revision++
	task.Params = params
	task.Targets = targets
	task.PlanHash = planHash(task)
	task.BatchIndex = 0
	task.UnstartedDeadline = s.now().UTC().Add(s.unstartedTTL)
	if task.ApprovalRequired {
		task.Status = StatusAwaitingApproval
	} else {
		task.Status = StatusQueued
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Revise(txCtx, tx, task, targets); err != nil {
			return err
		}
		if err := s.repo.AppendEvent(txCtx, tx, taskID, "revised", map[string]any{
			"revision": task.Revision, "targets": snapshot.Len(),
		}, s.now()); err != nil {
			return err
		}
		event := s.event(request.SubjectID, ActionRevise, "success", request.RequestID, snapshot.IDs(), "")
		event.TaskID = taskID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if err != nil {
		return Task{}, err
	}
	task.Targets = targets
	return task, nil
}

// Resume re-arms a queued task that has not dispatched anything: it re-checks
// authorization and approval, then extends the offline wait and advances the
// revision. Terminal, running or cancelling tasks are never resumed, and no
// target state is changed.
func (s *Service) Resume(ctx context.Context, taskID, subjectID, requestID string, expectedRevision uint64, reason string) (Task, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Task{}, err
	}
	if reason != "" && !validLabel(reason, 300) {
		return Task{}, fmt.Errorf("%w: reason", ErrInvalidTask)
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return Task{}, ErrInvalidStatus
	}
	if task.Status.Terminal() {
		return Task{}, ErrTaskTerminal
	}
	if task.Status != StatusQueued || !task.StartedAt.IsZero() || task.CancelRequested {
		return Task{}, fmt.Errorf("%w: only an unstarted queued task can be resumed", ErrInvalidStatus)
	}
	if task.ApprovalRequired {
		if err := s.checkApproval(ctx, task); err != nil {
			return Task{}, err
		}
	}
	if task.TargetType == TargetTypeController {
		return Task{}, ErrInvalidTask
	}
	targetIDs := taskTargetIDs(task)
	if err := s.authorizeForTask(ctx, task, subjectID, task.ActionID, requestID, targetIDs, "task_resume"); err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	deadline := now.Add(s.unstartedTTL)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		moved, err := s.repo.ResumeUnstarted(txCtx, tx, taskID, task.Revision, deadline, now)
		if err != nil {
			return err
		}
		if !moved {
			return ErrInvalidStatus
		}
		if err := s.repo.AppendEvent(txCtx, tx, taskID, "resumed", map[string]any{
			"revision": task.Revision + 1, "expectedRevision": task.Revision,
		}, now); err != nil {
			return err
		}
		event := s.event(subjectID, ActionResume, "success", requestID, taskAuditIDs(task), "resumed")
		event.TaskID = taskID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, taskID)
}

// Cancel stops an unstarted task immediately and asks the transport to interrupt
// dispatched targets. Unknown transport answers leave the task cancelling rather
// than claiming the side effects stopped.
func (s *Service) Cancel(ctx context.Context, taskID, subjectID, requestID string, expectedRevision ...uint64) (Task, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Task{}, err
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if len(expectedRevision) > 0 && expectedRevision[0] > 0 && task.Revision != expectedRevision[0] {
		return Task{}, ErrInvalidStatus
	}
	if task.Status.Terminal() {
		return Task{}, ErrTaskTerminal
	}
	targetIDs := taskTargetIDs(task)
	if err := s.authorizeForTask(ctx, task, subjectID, ActionCancel, requestID, targetIDs, "task_cancel"); err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	dispatched := make([]string, 0)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.SetCancelRequested(txCtx, tx, taskID); err != nil {
			return err
		}
		if _, err := s.repo.CancelPendingTargets(txCtx, tx, taskID); err != nil {
			return err
		}
		targets, err := TaskTargets(txCtx, tx, taskID)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if target.State == TargetDispatched {
				dispatched = append(dispatched, target.NodeID)
			}
		}
		status := StatusCancelling
		if len(dispatched) == 0 {
			status = StatusCanceled
		}
		moved, err := s.repo.Transition(txCtx, tx, taskID, task.Status, status, "canceled by operator", time.Time{}, terminalTime(status, now))
		if err != nil {
			return err
		}
		if !moved {
			return ErrInvalidStatus
		}
		if err := s.repo.AppendEvent(txCtx, tx, taskID, "cancel_requested", map[string]any{
			"dispatched": len(dispatched),
		}, now); err != nil {
			return err
		}
		event := s.event(subjectID, ActionCancel, "success", requestID, taskAuditIDs(task), "")
		event.TaskID = taskID
		return s.audit.AppendTx(txCtx, tx, event)
	})
	if err != nil {
		return Task{}, err
	}
	if s.workers[task.ActionID] != nil {
		s.workerMu.Lock()
		if run := s.workerRuns[taskID]; run != nil {
			run.cancel()
		}
		s.workerMu.Unlock()
		return s.repo.Get(ctx, taskID)
	}
	for _, nodeID := range dispatched {
		if s.dispatcher == nil {
			break
		}
		if err := s.dispatcher.Cancel(ctx, CancelCommand{TaskID: taskID, NodeID: nodeID}); err != nil {
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = s.repo.AppendEvent(cancelCtx, s.db, taskID, "cancel_transport_error", map[string]any{
				"nodeId": nodeID,
			}, s.now())
			cancel()
		}
	}
	return s.repo.Get(ctx, taskID)
}

// RecordResult stores one trusted terminal result and advances the task. Unknown
// results are terminal and are never retried automatically.
func (s *Service) RecordResult(ctx context.Context, taskID, nodeID string, code NodeResultCode, message string) (Task, error) {
	if !terminalResult(code) {
		return Task{}, fmt.Errorf("%w: result code", ErrInvalidTask)
	}
	redacted := Redact(message)
	if len(redacted) > 4096 {
		redacted = redacted[:4096]
	}
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if task.Status.Terminal() {
		return Task{}, ErrTaskTerminal
	}
	now := s.now().UTC()
	logNodeID := nodeID
	targetKey := "nodeId"
	if task.TargetType == TargetTypeController {
		logNodeID = ""
		targetKey = "controllerId"
	}
	applied := false
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		var err error
		applied, err = s.repo.RecordTargetResult(txCtx, tx, taskID, nodeID, code, redacted)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
		if err := s.repo.AppendLog(txCtx, tx, LogEntry{
			TaskID: taskID, NodeID: logNodeID, Level: logLevelFor(code), Message: redacted, At: now,
		}); err != nil {
			return err
		}
		return s.repo.AppendEvent(txCtx, tx, taskID, "target_result", map[string]any{
			targetKey: nodeID, "code": string(code),
		}, now)
	})
	if err != nil {
		return Task{}, err
	}
	if !applied {
		return s.repo.Get(ctx, taskID)
	}
	if err := s.advance(ctx, taskID); err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, taskID)
}

// AppendLog adds a redacted transport-side log line to a task.
func (s *Service) AppendLog(ctx context.Context, taskID, nodeID, level, message string) error {
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("%w: log level", ErrInvalidTask)
	}
	if _, err := s.repo.Get(ctx, taskID); err != nil {
		return err
	}
	redacted := Redact(message)
	if len(redacted) > 4096 {
		redacted = redacted[:4096]
	}
	return s.repo.AppendLog(ctx, s.db, LogEntry{
		TaskID: taskID, NodeID: nodeID, Level: level, Message: redacted, At: s.now(),
	})
}

func (s *Service) Get(ctx context.Context, taskID string) (Task, error) {
	return s.repo.Get(ctx, taskID)
}

func (s *Service) Events(ctx context.Context, taskID string, afterSeq int64, limit int) ([]Event, error) {
	if _, err := s.repo.Get(ctx, taskID); err != nil {
		return nil, err
	}
	return s.repo.Events(ctx, taskID, afterSeq, limit)
}

func (s *Service) Logs(ctx context.Context, taskID string, afterID int64, limit int) ([]LogEntry, error) {
	if _, err := s.repo.Get(ctx, taskID); err != nil {
		return nil, err
	}
	return s.repo.Logs(ctx, taskID, afterID, limit)
}

// Preview checks each explicit target against current authorization without
// writing anything. Group targets require a resolver implemented by composition.
func (s *Service) Preview(ctx context.Context, subjectID, actionID string, class OperationClass, nodeIDs []string) (TargetSnapshot, []string, []string, bool, error) {
	snapshot, err := NewTargetSnapshot(nodeIDs)
	if err != nil {
		return TargetSnapshot{}, nil, nil, false, err
	}
	if !class.Valid() {
		return TargetSnapshot{}, nil, nil, false, ErrUnknownOperationClass
	}
	eligible := make([]string, 0, snapshot.Len())
	denied := make([]string, 0)
	for _, nodeID := range snapshot.IDs() {
		check := auth.Check{SubjectID: subjectID, Action: actionID, NodeIDs: []string{nodeID}}
		if err := s.authorizer.Authorize(ctx, check); err != nil {
			if isPureDenial(err) {
				denied = append(denied, nodeID)
				continue
			}
			return TargetSnapshot{}, nil, nil, false, err
		}
		eligible = append(eligible, nodeID)
	}
	return snapshot, eligible, denied, s.requireApproval(class), nil
}

func (s *Service) lockTask(taskID string) func() {
	value, _ := s.taskLocks.LoadOrStore(taskID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func sameTargetSet(targets []Target, ids []string) bool {
	if len(targets) != len(ids) {
		return false
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	for _, target := range targets {
		if _, ok := want[target.NodeID]; !ok {
			return false
		}
	}
	return true
}

func paramsFromSubmit(request SubmitRequest) (string, error) {
	if request.CanonicalParams != nil {
		if request.Params != nil {
			return "", fmt.Errorf("%w: provide params or canonical params, not both", ErrInvalidTask)
		}
		var decoded map[string]string
		if err := json.Unmarshal(request.CanonicalParams, &decoded); err != nil {
			return "", fmt.Errorf("%w: canonical params", ErrInvalidTask)
		}
		return normalizeParams(decoded)
	}
	return normalizeParams(request.Params)
}

func knownAuditSource(source audit.Source) bool {
	switch source {
	case audit.SourceWeb, audit.SourceAI, audit.SourcePlugin, audit.SourceAgent, audit.SourceSystem:
		return true
	default:
		return false
	}
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("tasks: service is not configured")
	}
	if requestID == "" || len(requestID) > 128 {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID string, nodeIDs []string, reason string) error {
	if s.authorizer == nil {
		return fmt.Errorf("%w: authorizer", ErrInvalidTask)
	}
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: append([]string(nil), nodeIDs...)}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) {
		denial := audit.Event{
			SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID,
			NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
			ReasonCode: reason, At: s.now(),
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("tasks: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
		ReasonCode: reason, At: s.now(),
	}
}

func buildTargets(ids []string, plan BatchPlan) []Target {
	targets := make([]Target, 0, len(ids))
	position := 0
	for batchIndex, batch := range plan.Batches() {
		for _, nodeID := range batch {
			targets = append(targets, Target{
				NodeID: nodeID, Position: position, BatchIndex: batchIndex, State: TargetPending,
			})
			position++
		}
	}
	return targets
}

func taskTargetIDs(task Task) []string {
	ids := make([]string, 0, len(task.Targets))
	for _, target := range task.Targets {
		ids = append(ids, target.NodeID)
	}
	sort.Strings(ids)
	return ids
}

func taskSnapshot(task Task) (TargetSnapshot, error) {
	ordered := append([]Target(nil), task.Targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })
	ids := make([]string, 0, len(ordered))
	for _, target := range ordered {
		ids = append(ids, target.NodeID)
	}
	return NewTargetSnapshot(ids)
}

func planHash(task Task) string {
	snapshot, err := taskSnapshot(task)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	hash.Write([]byte(task.ActionID))
	hash.Write([]byte{0})
	hash.Write([]byte(task.OperationClass))
	hash.Write([]byte{0})
	hash.Write([]byte(fmt.Sprintf("%d", task.Revision)))
	hash.Write([]byte{0})
	hash.Write([]byte(task.Params))
	hash.Write([]byte{0})
	for _, id := range snapshot.IDs() {
		hash.Write([]byte(id))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func terminalResult(code NodeResultCode) bool {
	switch code {
	case NodeResultSucceeded, NodeResultFailed, NodeResultCanceled, NodeResultTimedOut, NodeResultUnknown:
		return true
	default:
		return false
	}
}

func logLevelFor(code NodeResultCode) string {
	switch code {
	case NodeResultSucceeded:
		return "info"
	case NodeResultCanceled:
		return "warn"
	default:
		return "error"
	}
}

func terminalTime(status TaskStatus, now time.Time) time.Time {
	if status.Terminal() {
		return now
	}
	return time.Time{}
}

func normalizeParams(params map[string]string) (string, error) {
	if len(params) > 32 {
		return "", fmt.Errorf("%w: too many params", ErrInvalidTask)
	}
	clean := make(map[string]string, len(params))
	for key, value := range params {
		if !validToken(key, 64) {
			return "", fmt.Errorf("%w: param key", ErrInvalidTask)
		}
		if sensitiveKey(key) {
			return "", fmt.Errorf("%w: params must reference secrets by id", ErrInvalidTask)
		}
		if len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return "", fmt.Errorf("%w: param value", ErrInvalidTask)
		}
		clean[key] = value
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	if len(encoded) > 32*1024 {
		return "", fmt.Errorf("%w: params too large", ErrInvalidTask)
	}
	return string(encoded), nil
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range []string{"password", "passwd", "secret", "token", "apikey", "api_key", "private_key", "authorization"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func validLabel(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	return validLabel(value, max) && !strings.ContainsFunc(value, unicode.IsSpace)
}

func newTaskID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("tasks: entropy source failed: " + err.Error())
	}
	return "tsk_" + hex.EncodeToString(buffer)
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
