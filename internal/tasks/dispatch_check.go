package tasks

import "context"

// CheckDispatch is ONLY the last-mile offer-admission gate for an exact
// pending node in an approved queued/running frozen plan. It does not admit
// file uploads, child offers, execution results, controller-local workers, or
// arbitrary queued work and does not weaken CheckExecution's running rule.
func (s *Service) CheckDispatch(ctx context.Context, id, plan, nodeID string) (Task, error) {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return Task{}, ErrInvalidTask
	}
	if s.runtimeGate != nil {
		if err := s.runtimeGate(ctx); err != nil {
			return Task{}, err
		}
	}
	task, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if task.TargetType == TargetTypeController || task.Status != StatusQueued && task.Status != StatusRunning || task.CancelRequested || task.PlanHash != plan || nodeID == "" || task.Status == StatusQueued && task.StartedAt.IsZero() && !s.now().Before(task.UnstartedDeadline) {
		return Task{}, ErrInvalidStatus
	}
	pending := false
	for _, target := range task.Targets {
		if target.NodeID == nodeID && target.State == TargetPending {
			pending = true
			break
		}
	}
	if !pending {
		return Task{}, ErrInvalidTask
	}
	if err = s.authorizeForTask(ctx, task, task.SubmitterID, task.ActionID, task.RequestID, []string{nodeID}, "task_offer_current"); err != nil {
		return Task{}, err
	}
	if task.ApprovalRequired {
		if err = s.checkApproval(ctx, task, []string{nodeID}); err != nil {
			return Task{}, err
		}
	}
	return task, nil
}
