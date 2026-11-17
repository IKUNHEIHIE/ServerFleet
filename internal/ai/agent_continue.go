package ai

import (
	"context"
	"sync"
	"time"
)

func (l *AgentLoop) Continue(ctx context.Context, session, id string, expected uint64, steps, seconds int) (AgentRun, error) {
	if steps < 1 || steps > 24 || seconds < 60 || seconds > 3600 {
		return AgentRun{}, ErrToolDenied
	}
	lock, _ := l.locks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	if !mu.TryLock() {
		return AgentRun{}, ErrScopeConflict
	}
	defer mu.Unlock()
	if _, err := l.sessions.actor(ctx, "ai.runs.advance", session, true); err != nil {
		return AgentRun{}, err
	}
	current, err := l.read(ctx, session, id)
	if err != nil {
		return AgentRun{}, err
	}
	view, err := l.sessions.Get(ctx, session)
	if err != nil {
		return AgentRun{}, err
	}
	if current.View.ScopeRevision != view.ScopeRevision || current.View.Revision != expected || current.View.State != "limit_reached" || current.View.StopReason != "maximum_model_steps_reached" || current.View.MaxSteps+steps > 64 {
		return AgentRun{}, ErrScopeConflict
	}
	l.workerMu.Lock()
	_, running := l.workers[id]
	l.workerMu.Unlock()
	if running {
		return AgentRun{}, ErrScopeConflict
	}
	updated, err := l.change(ctx, session, id, expected, func(run *agentRunRecord, now time.Time) error {
		deadline := now.Add(time.Duration(seconds) * time.Second)
		if deadline.Sub(run.View.CreatedAt) > 24*time.Hour {
			return ErrScopeConflict
		}
		run.View.MaxSteps += steps
		run.View.ExpiresAt = deadline
		run.View.State = "queued"
		run.View.StopReason = ""
		return nil
	})
	return updated.View, err
}
