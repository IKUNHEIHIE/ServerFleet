package tasks

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// Worker is a program-registered action implementation in the SOLE scheduler.
// It receives the persisted frozen task, not HTTP input. Child protocol events
// must never report a parent result; only this complete operation returns one.
type Worker interface {
	Execute(context.Context, Task) (map[string]WorkerResult, error)
}
type WorkerResult struct {
	Code    NodeResultCode
	Message string
}
type workerRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Service) startWorker(ctx context.Context, task Task, pending []Target, worker Worker) error {
	if s.runtimeGate != nil {
		if err := s.runtimeGate(ctx); err != nil {
			return err
		}
	}
	s.workerMu.Lock()
	full := len(s.workerRuns) >= s.concurrency
	s.workerMu.Unlock()
	if full {
		return nil
	}
	// Composite workers own the entire frozen snapshot, never a partial batch.
	if len(pending) != len(task.Targets) {
		return ErrInvalidTask
	}
	for _, t := range pending {
		if t.BatchIndex != task.BatchIndex {
			return ErrInvalidTask
		}
	}
	err := s.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		for _, t := range pending {
			moved, err := s.repo.MarkDispatched(ctx, tx, task.ID, t.NodeID)
			if err != nil {
				return err
			}
			if !moved {
				return ErrInvalidStatus
			}
		}
		if err := s.repo.SetStarted(ctx, tx, task.ID); err != nil {
			return err
		}
		moved, err := s.repo.Transition(ctx, tx, task.ID, task.Status, StatusRunning, "", time.Time{}, time.Time{})
		if err != nil {
			return err
		}
		if !moved {
			return ErrInvalidStatus
		}
		return s.repo.AppendEvent(ctx, tx, task.ID, "worker_reserved", map[string]any{"action": task.ActionID}, s.now())
	})
	if err != nil {
		return err
	}
	// A lost commit acknowledgement leaves durable dispatched targets. No worker
	// is started on error; the next pass/restart classifies them UNKNOWN.
	current, err := s.repo.Get(ctx, task.ID)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &workerRun{cancel: cancel, done: make(chan struct{})}
	s.workerMu.Lock()
	s.workerRuns[task.ID] = run
	s.workerMu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.workerMu.Lock()
			delete(s.workerRuns, task.ID)
			close(run.done)
			s.workerMu.Unlock()
		}()
		results, runErr := invokeWorker(runCtx, worker, current)
		resultCtx, stop := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
		defer stop()
		for _, target := range current.Targets {
			result, ok := results[target.NodeID]
			if !ok || !terminalResult(result.Code) {
				result = WorkerResult{NodeResultUnknown, "worker result unknown"}
			}
			if runErr != nil && result.Code == NodeResultSucceeded {
				result = WorkerResult{NodeResultUnknown, "worker outcome uncertain"}
			}
			if _, err := s.RecordResult(resultCtx, task.ID, target.NodeID, result.Code, result.Message); err != nil {
				s.onError(err)
			}
		}
	}()
	return nil
}
func invokeWorker(ctx context.Context, w Worker, t Task) (results map[string]WorkerResult, err error) {
	defer func() {
		if recover() != nil {
			results = nil
			err = ErrInvalidStatus
		}
	}()
	return w.Execute(ctx, t)
}

// Restart never replays a dispatched worker, including one interrupted before
// its first child offer. A live worker owns cancellation and bounded finalization.
func (s *Service) reconcileWorker(ctx context.Context, task Task) error {
	s.workerMu.Lock()
	live := s.workerRuns[task.ID] != nil
	s.workerMu.Unlock()
	if live {
		return nil
	}
	return s.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		for _, t := range task.Targets {
			if t.State == TargetDispatched {
				if _, err := s.repo.RecordTargetResult(ctx, tx, task.ID, t.NodeID, NodeResultUnknown, "worker interrupted; reconcile without replay"); err != nil {
					return err
				}
			}
		}
		_, err := s.repo.Finalize(ctx, tx, task.ID, StatusUnknown, "worker interrupted; reconcile without replay", s.now())
		if err != nil {
			return err
		}
		return s.repo.AppendEvent(ctx, tx, task.ID, "unknown", map[string]any{"reason": "worker_interrupted"}, s.now())
	})
}
func (s *Service) stopWorkers() {
	s.workerMu.Lock()
	runs := make([]*workerRun, 0, len(s.workerRuns))
	for _, r := range s.workerRuns {
		r.cancel()
		runs = append(runs, r)
	}
	s.workerMu.Unlock()
	for _, r := range runs {
		<-r.done
	}
}

// CheckExecution rechecks current task/approval/subject authorization on every
// child frame. Unsupported automation/plugin scopes are rejected by the worker
// until an explicitly registered scope gate exists; no implicit AI authority.
func (s *Service) CheckExecution(ctx context.Context, id, plan string) (Task, error) {
	if s.runtimeGate != nil {
		if err := s.runtimeGate(ctx); err != nil {
			return Task{}, err
		}
	}
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if t.Status != StatusRunning || t.CancelRequested || t.PlanHash != plan {
		return Task{}, ErrInvalidStatus
	}
	if t.ApprovalRequired {
		if err = s.checkApproval(ctx, t); err != nil {
			return Task{}, err
		}
	}
	if err = s.authorize(ctx, t.SubmitterID, t.ActionID, t.RequestID, taskTargetIDs(t), "task_execute"); err != nil {
		return Task{}, err
	}
	return t, nil
}
