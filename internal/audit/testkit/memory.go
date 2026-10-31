// Package testkit contains explicit test-only adapters. Nothing here may be wired into a
// production composition root.
package testkit

import (
	"context"
	"errors"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
)

var ErrValidatorRequired = errors.New("audit testkit: validator is required")

// MemoryRecorder is an append-only in-memory audit.Recorder for isolated tests. It is not
// durable storage, not tamper-proof, and must never replace a real audit sink in production.
type MemoryRecorder struct {
	mu        sync.Mutex
	validator *audit.Validator
	now       func() time.Time
	events    []audit.Event
}

// NewMemoryRecorder requires an explicit validator. A nil clock defaults to time.Now.
func NewMemoryRecorder(validator *audit.Validator, now func() time.Time) (*MemoryRecorder, error) {
	if validator == nil {
		return nil, ErrValidatorRequired
	}
	if now == nil {
		now = time.Now
	}
	return &MemoryRecorder{validator: validator, now: now}, nil
}

// Append validates before writing, never overwrites older events, and writes nothing when the
// context is already canceled or expires before the lock is acquired.
func (r *MemoryRecorder) Append(ctx context.Context, event audit.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.validator.Validate(event, r.now()); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.events = append(r.events, event)
	return nil
}

// Snapshot returns a copy of the recorded events in append order.
func (r *MemoryRecorder) Snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := make([]audit.Event, len(r.events))
	copy(snapshot, r.events)
	return snapshot
}

// Len returns the number of recorded events.
func (r *MemoryRecorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// View returns a read-only projection over the current snapshot.
func (r *MemoryRecorder) View() *audit.View {
	return audit.NewView(r.Snapshot())
}

var _ audit.Recorder = (*MemoryRecorder)(nil)
