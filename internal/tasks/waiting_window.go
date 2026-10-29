package tasks

import (
	"errors"
	"time"
)

// DefaultUnstartedOfflineWait is the approved default wait for tasks that have not started
// while their target node is offline. It is not a frozen public contract.
const DefaultUnstartedOfflineWait = 15 * time.Minute

var (
	ErrInvalidCreatedAt     = errors.New("tasks: creation time is required")
	ErrInvalidWaitDuration  = errors.New("tasks: wait duration must be positive")
	ErrInvalidReferenceTime = errors.New("tasks: reference time is required")
	ErrInvalidWindow        = errors.New("tasks: invalid wait window")
	ErrUnknownStartState    = errors.New("tasks: unknown start state")
)

// WaitWindow is an explicit creation/deadline pair for an unstarted offline wait.
type WaitWindow struct {
	createdAt time.Time
	deadline  time.Time
}

// NewWaitWindow creates a window from caller-supplied times. Zero creation times and
// non-positive durations are rejected so they cannot turn into an infinite wait.
func NewWaitWindow(createdAt time.Time, wait time.Duration) (WaitWindow, error) {
	if createdAt.IsZero() {
		return WaitWindow{}, ErrInvalidCreatedAt
	}
	if wait <= 0 {
		return WaitWindow{}, ErrInvalidWaitDuration
	}
	return WaitWindow{createdAt: createdAt, deadline: createdAt.Add(wait)}, nil
}

// NewDefaultWaitWindow creates the approved default 15-minute window.
func NewDefaultWaitWindow(createdAt time.Time) (WaitWindow, error) {
	return NewWaitWindow(createdAt, DefaultUnstartedOfflineWait)
}

func (w WaitWindow) CreatedAt() time.Time { return w.createdAt }
func (w WaitWindow) Deadline() time.Time  { return w.deadline }

// StartState distinguishes tasks that have not started from tasks already running.
type StartState string

const (
	StartStateUnstarted StartState = "unstarted"
	StartStateStarted   StartState = "started"
)

// WaitOutcome is the result of applying the wait window.
type WaitOutcome string

const (
	WaitPending       WaitOutcome = "pending"
	WaitExpired       WaitOutcome = "expired"
	WaitNotApplicable WaitOutcome = "not_applicable"
)

// Evaluate applies the window only to unstarted tasks. A valid window yields not_applicable
// for started tasks; they are never reclassified as expired or queued. A zero/invalid window
// is rejected for every state. For unstarted tasks, now before the deadline is pending and
// now at or after the deadline is expired; pending is not an execution permission. The
// reference time must be explicit for every evaluation.
func (w WaitWindow) Evaluate(now time.Time, state StartState) (WaitOutcome, error) {
	if now.IsZero() {
		return "", ErrInvalidReferenceTime
	}
	switch state {
	case StartStateStarted, StartStateUnstarted:
	default:
		return "", ErrUnknownStartState
	}
	if w.createdAt.IsZero() || w.deadline.IsZero() || !w.deadline.After(w.createdAt) {
		return "", ErrInvalidWindow
	}
	if state == StartStateStarted {
		return WaitNotApplicable, nil
	}
	if now.Before(w.deadline) {
		return WaitPending, nil
	}
	return WaitExpired, nil
}
