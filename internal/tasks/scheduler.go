package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Run starts the scheduler loop until ctx is canceled. Effects are only applied
// through a new execution of RunOnce; no goroutine is shared across services.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.onError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce advances every active task at most once. It is safe to call
// concurrently, but callers normally use a single loop.
func (s *Service) RunOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ids, err := s.repo.ListActive(ctx, 100)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.process(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) process(ctx context.Context, taskID string) error {
	unlock := s.lockTask(taskID)
	defer unlock()
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status.Terminal() || task.Status == StatusAwaitingApproval {
		return nil
	}
	if task.CancelRequested {
		if !hasDispatched(task) {
			_, err := s.finalize(ctx, task, StatusCanceled, "canceled by operator")
			return err
		}
		return nil
	}
	now := s.now().UTC()
	if task.Status == StatusQueued && task.StartedAt.IsZero() && now.After(task.UnstartedDeadline) {
		_, err := s.finalize(ctx, task, StatusExpired, "offline wait expired")
		return err
	}
	if task.ApprovalRequired {
		if err := s.checkApproval(ctx, task); err != nil {
			_, finalErr := s.finalize(ctx, task, StatusFailed, "approval_invalid")
			if finalErr != nil {
				return finalErr
			}
			return nil
		}
	}
	batch := batchTargets(task, task.BatchIndex)
	if len(batch) == 0 {
		return errors.New("tasks: task has no targets for its current batch")
	}
	unfinished := make([]Target, 0, len(batch))
	for _, target := range batch {
		if !target.State.Terminal() {
			unfinished = append(unfinished, target)
		}
	}
	if len(unfinished) == 0 {
		return s.advance(ctx, taskID)
	}
	dispatched := false
	pending := make([]Target, 0, len(unfinished))
	for _, target := range unfinished {
		if target.State == TargetDispatched {
			dispatched = true
			continue
		}
		if target.State == TargetPending {
			pending = append(pending, target)
		}
	}
	if dispatched {
		if err := s.ensureRunning(ctx, task); err != nil {
			return err
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if s.nodes == nil {
		_, err := s.finalize(ctx, task, StatusFailed, "node_lookup_unavailable")
		return err
	}
	for _, target := range pending {
		online, err := s.nodes.Online(ctx, target.NodeID)
		if err != nil {
			return err
		}
		if !online {
			if task.StartedAt.IsZero() && now.After(task.UnstartedDeadline) {
				_, finalErr := s.finalize(ctx, task, StatusExpired, "offline wait expired")
				return finalErr
			}
			return nil
		}
	}
	pendingIDs := make([]string, 0, len(pending))
	for _, target := range pending {
		pendingIDs = append(pendingIDs, target.NodeID)
	}
	if err := s.authorize(ctx, task.SubmitterID, task.ActionID, task.RequestID, pendingIDs, "task_execute"); err != nil {
		if isPureDenial(err) {
			if eventErr := s.repo.AppendEvent(ctx, s.db, task.ID, "dispatch_denied", map[string]any{
				"reason": "authorization_revoked",
			}, s.now()); eventErr != nil {
				return eventErr
			}
			_, finalErr := s.finalize(ctx, task, StatusFailed, "authorization_revoked")
			if finalErr != nil {
				return finalErr
			}
			return nil
		}
		return err
	}
	return s.dispatch(ctx, task, pending)
}

func (s *Service) dispatch(ctx context.Context, task Task, pending []Target) error {
	if s.dispatcher == nil {
		_, err := s.finalize(ctx, task, StatusFailed, "dispatcher_unavailable")
		return err
	}
	type outcome struct {
		target Target
		result DispatchResult
		err    error
	}
	limit := s.concurrency
	if limit > len(pending) {
		limit = len(pending)
	}
	semaphore := make(chan struct{}, limit)
	results := make(chan outcome, len(pending))
	params := json.RawMessage(task.Params)
	for _, target := range pending {
		target := target
		semaphore <- struct{}{}
		go func() {
			defer func() { <-semaphore }()
			result, err := s.dispatcher.Dispatch(ctx, Command{
				TaskID: task.ID, NodeID: target.NodeID, ActionID: task.ActionID, Params: params,
			})
			results <- outcome{target: target, result: result, err: err}
		}()
	}
	collected := make([]outcome, 0, len(pending))
	for range pending {
		collected = append(collected, <-results)
	}
	now := s.now().UTC()
	unknown := false
	accepted := false
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		for _, item := range collected {
			if item.err != nil || item.result.Unknown {
				unknown = true
				if _, err := s.repo.RecordTargetResult(txCtx, tx, task.ID, item.target.NodeID, NodeResultUnknown, "transport result unknown"); err != nil {
					return err
				}
				if err := s.repo.AppendEvent(txCtx, tx, task.ID, "target_unknown", map[string]any{
					"nodeId": item.target.NodeID,
				}, now); err != nil {
					return err
				}
				continue
			}
			if !item.result.Accepted {
				if _, err := s.repo.RecordTargetResult(txCtx, tx, task.ID, item.target.NodeID, NodeResultFailed, Redact(item.result.Reason)); err != nil {
					return err
				}
				if err := s.repo.AppendEvent(txCtx, tx, task.ID, "target_rejected", map[string]any{
					"nodeId": item.target.NodeID,
				}, now); err != nil {
					return err
				}
				continue
			}
			accepted = true
			moved, err := s.repo.MarkDispatched(txCtx, tx, task.ID, item.target.NodeID)
			if err != nil {
				return err
			}
			if !moved {
				continue
			}
			if err := s.repo.AppendEvent(txCtx, tx, task.ID, "target_dispatched", map[string]any{
				"nodeId": item.target.NodeID, "batch": task.BatchIndex,
			}, now); err != nil {
				return err
			}
		}
		if accepted {
			if err := s.repo.SetStarted(txCtx, tx, task.ID); err != nil {
				return err
			}
			if task.Status == StatusQueued {
				if _, err := s.repo.Transition(txCtx, tx, task.ID, StatusQueued, StatusRunning, "", time.Time{}, time.Time{}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if unknown {
		_, err := s.finalize(ctx, task, StatusUnknown, "transport result unknown")
		return err
	}
	return s.advance(ctx, task.ID)
}

// advance evaluates the current batch and either moves to the next batch, keeps
// waiting for results, or finalizes the task. It never retries a dispatched or
// unknown side effect.
func (s *Service) advance(ctx context.Context, taskID string) error {
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status.Terminal() {
		return nil
	}
	if task.CancelRequested && !hasDispatched(task) {
		_, err := s.finalize(ctx, task, StatusCanceled, "canceled by operator")
		return err
	}
	batch := batchTargets(task, task.BatchIndex)
	for _, target := range batch {
		if !target.State.Terminal() {
			return nil
		}
	}
	snapshot, err := taskSnapshot(task)
	if err != nil {
		return err
	}
	plan, err := NewDefaultBatchPlan(snapshot, task.OperationClass)
	if err != nil {
		return err
	}
	results := make([]NodeResult, 0, len(batch))
	for _, target := range batch {
		code := codeForState(target)
		if code == "" {
			return fmt.Errorf("tasks: target %s has no trustworthy result", target.NodeID)
		}
		results = append(results, NodeResult{NodeID: target.NodeID, Code: code})
	}
	outcome, err := plan.EvaluateBatch(task.BatchIndex, results)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	switch outcome {
	case BatchOutcomeAdvance:
		err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			if err := s.repo.SetBatchIndex(txCtx, tx, task.ID, task.BatchIndex+1); err != nil {
				return err
			}
			moved, err := s.repo.Transition(txCtx, tx, task.ID, task.Status, StatusQueued, "", time.Time{}, time.Time{})
			if err != nil {
				return err
			}
			if !moved {
				return ErrInvalidStatus
			}
			return s.repo.AppendEvent(txCtx, tx, task.ID, "batch_advanced", map[string]any{
				"from": task.BatchIndex, "to": task.BatchIndex + 1,
			}, now)
		})
		return err
	case BatchOutcomeComplete, BatchOutcomeBlocked:
		_, err := s.finalize(ctx, task, "", "")
		return err
	case BatchOutcomeIncomplete:
		if err := s.ensureRunning(ctx, task); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("tasks: unknown batch outcome %q", outcome)
	}
}

// finalize writes one terminal status derived from all target results unless a
// forced status (expired) is supplied. Pending targets are canceled so nothing
// new can dispatch after a terminal decision.
func (s *Service) finalize(ctx context.Context, task Task, forced TaskStatus, reason string) (Task, error) {
	current, err := s.repo.Get(ctx, task.ID)
	if err != nil {
		return Task{}, err
	}
	if current.Status.Terminal() {
		return current, nil
	}
	status := forced
	if status == "" {
		status, reason = deriveStatus(current)
	}
	now := s.now().UTC()
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := s.repo.CancelPendingTargets(txCtx, tx, current.ID); err != nil {
			return err
		}
		moved, err := s.repo.Finalize(txCtx, tx, current.ID, status, reason, now)
		if err != nil {
			return err
		}
		if !moved {
			return nil
		}
		return s.repo.AppendEvent(txCtx, tx, current.ID, string(status), map[string]any{
			"status": string(status), "reason": reason,
		}, now)
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, current.ID)
}

func deriveStatus(task Task) (TaskStatus, string) {
	unknown, succeeded, failed, canceled, timedOut, pending := 0, 0, 0, 0, 0, 0
	for _, target := range task.Targets {
		switch target.State {
		case TargetUnknown:
			unknown++
		case TargetSucceeded:
			succeeded++
		case TargetFailed:
			failed++
		case TargetCanceled:
			canceled++
		case TargetTimedOut:
			timedOut++
		case TargetPending, TargetDispatched:
			pending++
		}
	}
	if task.CancelRequested {
		return StatusCanceled, "canceled by operator"
	}
	if unknown > 0 {
		return StatusUnknown, "one or more transport results are unknown"
	}
	if pending > 0 {
		if succeeded > 0 {
			return StatusPartialFailure, "later batches were not started"
		}
		return StatusFailed, "task ended with targets not started"
	}
	total := succeeded + failed + canceled + timedOut
	switch {
	case total > 0 && succeeded == total:
		return StatusCompleted, ""
	case succeeded > 0:
		return StatusPartialFailure, "some targets failed"
	case timedOut > 0:
		return StatusFailed, "one or more targets timed out"
	default:
		return StatusFailed, "no target succeeded"
	}
}

func (s *Service) ensureRunning(ctx context.Context, task Task) error {
	if task.Status == StatusRunning {
		return nil
	}
	if task.Status != StatusQueued {
		return nil
	}
	if _, err := s.repo.Transition(ctx, s.db, task.ID, StatusQueued, StatusRunning, "", time.Time{}, time.Time{}); err != nil {
		return err
	}
	return s.repo.SetStarted(ctx, s.db, task.ID)
}

func (s *Service) checkApproval(ctx context.Context, task Task) error {
	approval, err := s.repo.Approval(ctx, s.db, task.ID, task.Revision)
	if err != nil {
		return ErrApprovalInvalid
	}
	if !approval.Approved() || approval.PlanHash != task.PlanHash {
		return ErrApprovalInvalid
	}
	if approval.ApproverID == task.SubmitterID {
		return ErrSelfApproval
	}
	if s.approvalTTL > 0 && s.now().UTC().After(approval.DecidedAt.Add(s.approvalTTL)) {
		return ErrApprovalInvalid
	}
	return nil
}

// Recover must be called once at startup. Any target that was dispatched when the
// Controller stopped has an unknown outcome and is never retried automatically;
// the task becomes unknown (or canceled when cancellation was already requested
// and nothing was in flight).
func (s *Service) Recover(ctx context.Context) (int, error) {
	ids, err := s.repo.ListRunningOrCancelling(ctx)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, id := range ids {
		unlock := s.lockTask(id)
		task, err := s.repo.Get(ctx, id)
		if err != nil {
			unlock()
			return recovered, err
		}
		inFlight := false
		err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			for _, target := range task.Targets {
				if target.State == TargetDispatched {
					inFlight = true
					if _, err := s.repo.RecordTargetResult(txCtx, tx, id, target.NodeID, NodeResultUnknown, "controller restarted with in-flight command"); err != nil {
						return err
					}
				}
			}
			if inFlight {
				moved, err := s.repo.Finalize(txCtx, tx, id, StatusUnknown, "controller restart: in-flight result unknown", s.now().UTC())
				if err != nil {
					return err
				}
				if moved {
					return s.repo.AppendEvent(txCtx, tx, id, "recovered", map[string]any{
						"status": string(StatusUnknown), "inFlight": true,
					}, s.now())
				}
				return nil
			}
			if task.CancelRequested {
				moved, err := s.repo.Finalize(txCtx, tx, id, StatusCanceled, "canceled by operator", s.now().UTC())
				if err != nil {
					return err
				}
				if moved {
					return s.repo.AppendEvent(txCtx, tx, id, "recovered", map[string]any{
						"status": string(StatusCanceled), "inFlight": false,
					}, s.now())
				}
				return nil
			}
			// Nothing was in flight: queued/pending work has no side effects yet and
			// can safely continue through the normal scheduler.
			return nil
		})
		unlock()
		if err != nil {
			return recovered, err
		}
		if inFlight || task.CancelRequested {
			recovered++
		}
	}
	return recovered, nil
}

func batchTargets(task Task, batchIndex int) []Target {
	targets := make([]Target, 0)
	for _, target := range task.Targets {
		if target.BatchIndex == batchIndex {
			targets = append(targets, target)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Position < targets[j].Position })
	return targets
}

func hasDispatched(task Task) bool {
	for _, target := range task.Targets {
		if target.State == TargetDispatched {
			return true
		}
	}
	return false
}

func codeForState(target Target) NodeResultCode {
	switch target.State {
	case TargetSucceeded:
		return NodeResultSucceeded
	case TargetFailed:
		return NodeResultFailed
	case TargetCanceled:
		return NodeResultCanceled
	case TargetTimedOut:
		return NodeResultTimedOut
	case TargetUnknown:
		return NodeResultUnknown
	default:
		return ""
	}
}
