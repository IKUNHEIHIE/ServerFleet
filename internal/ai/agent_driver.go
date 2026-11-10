package ai

import (
	"context"
	"errors"
	"time"
)

// Drive is explicitly authorized by the authenticated user's run/continue
// request. It retains only that live in-memory reauthentication callback, never
// persists cookies, constructs a system actor or restores authority on restart.
func (l *AgentLoop) Drive(ctx context.Context, session, id string, expected uint64) (AgentRun, error) {
	r, e := l.Get(ctx, session, id)
	if e != nil {
		return AgentRun{}, e
	}
	if r.Revision != expected {
		return AgentRun{}, ErrScopeConflict
	}
	if agentTerminal(r.State) {
		return r, nil
	}
	if !l.sessions.now().Before(r.ExpiresAt) {
		return l.Advance(ctx, session, id, expected)
	}
	l.workerMu.Lock()
	if _, ok := l.workers[id]; ok {
		l.workerMu.Unlock()
		r.DriverActive = true
		return r, nil
	}
	if l.closed || len(l.workers) >= 16 {
		l.workerMu.Unlock()
		return AgentRun{}, ErrSessionUnavailable
	}
	bounded, cancel := context.WithDeadline(context.WithoutCancel(ctx), r.ExpiresAt)
	l.workers[id] = cancel
	l.wg.Add(1)
	l.workerMu.Unlock()
	go func() {
		defer l.wg.Done()
		defer cancel()
		defer func() { l.workerMu.Lock(); delete(l.workers, id); l.workerMu.Unlock() }()
		defer func() {
			panicked := recover() != nil
			if bounded.Err() == nil && !panicked {
				return
			}
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			a, e := l.read(cleanup, session, id)
			if e == nil && !agentTerminal(a.View.State) {
				// Graceful shutdown pauses known durable waits. It must not
				// manufacture user authority on restart or replay an in-flight I/O.
				if !panicked && errors.Is(bounded.Err(), context.Canceled) && a.View.State != "thinking" && a.View.State != "submitting" {
					return
				}
				state := "failed"
				if panicked || a.View.State == "thinking" || a.View.State == "submitting" || a.View.State == "waiting_task" || a.View.State == "canceling" {
					state = "unknown"
				}
				_, _ = l.stop(cleanup, a, state, "driver_interrupted_or_deadline_expired")
			}
		}()
		for bounded.Err() == nil {
			current, e := l.Get(bounded, session, id)
			if e != nil || agentTerminal(current.State) {
				return
			}
			next, e := l.Advance(bounded, session, id, current.Revision)
			if e != nil {
				if errors.Is(e, ErrScopeConflict) {
					t := time.NewTimer(100 * time.Millisecond)
					select {
					case <-bounded.Done():
						t.Stop()
						return
					case <-t.C:
						continue
					}
				}
				return
			}
			if agentTerminal(next.State) {
				return
			}
			delay := 100 * time.Millisecond
			if next.State == "waiting_approval" || next.State == "waiting_task" || next.State == "canceling" {
				delay = 2 * time.Second
			}
			t := time.NewTimer(delay)
			select {
			case <-bounded.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()
	r.DriverActive = true
	return r, nil
}
func (l *AgentLoop) Close() {
	if l == nil {
		return
	}
	l.workerMu.Lock()
	l.closed = true
	for _, cancel := range l.workers {
		cancel()
	}
	l.workerMu.Unlock()
	l.wg.Wait()
}
func (l *AgentLoop) interrupt(id string) {
	l.workerMu.Lock()
	cancel := l.workers[id]
	l.workerMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
