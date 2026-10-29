package tasks

import "errors"

var (
	ErrInvalidBatchPlan = errors.New("tasks: invalid batch plan")
	ErrInvalidProgress  = errors.New("tasks: invalid batch progress")
	ErrProgressTerminal = errors.New("tasks: batch progress is terminal")
)

// BatchProgressState is the explicit position of a sequential batch progress value.
type BatchProgressState string

const (
	BatchProgressWaiting  BatchProgressState = "waiting"
	BatchProgressComplete BatchProgressState = "complete"
	BatchProgressBlocked  BatchProgressState = "blocked"
)

// BatchProgress is an immutable, copyable pure value that tracks which batch is current.
// It starts at the first batch and only moves to the immediately next batch after Advance.
// It is not a one-time dispatch ticket, execution permission, persisted record or replay
// protection: copying a waiting value and applying the same results again can recompute the
// same transition. A real task service must bind results to the current execution and retain
// the authoritative progress.
type BatchProgress struct {
	plan  BatchPlan
	index int
	state BatchProgressState
}

// StartBatchProgress creates progress at the first batch of a non-zero plan. A zero plan is
// rejected; targets are never interpreted as "all nodes". The plan's batches are copied so
// the progress value does not share its mutable storage with the caller's plan.
func StartBatchProgress(plan BatchPlan) (BatchProgress, error) {
	if plan.BatchCount() == 0 {
		return BatchProgress{}, ErrInvalidBatchPlan
	}
	frozenPlan := BatchPlan{batches: plan.Batches()}
	return BatchProgress{plan: frozenPlan, index: 0, state: BatchProgressWaiting}, nil
}

// State returns the explicit progress state. The zero value returns an empty state.
func (p BatchProgress) State() BatchProgressState { return p.state }

// CurrentBatchIndex returns the current or last-evaluated batch index. It is meaningful only
// when Valid reports true.
func (p BatchProgress) CurrentBatchIndex() int { return p.index }

// BatchCount returns the number of batches in the underlying plan.
func (p BatchProgress) BatchCount() int { return p.plan.BatchCount() }

// CurrentBatchTargets returns a copy of the current (or terminal) batch targets.
func (p BatchProgress) CurrentBatchTargets() []string {
	if !p.Valid() {
		return nil
	}
	batch := p.plan.batches[p.index]
	copied := make([]string, len(batch))
	copy(copied, batch)
	return copied
}

// Valid reports whether p was created by StartBatchProgress or produced by ApplyBatch.
func (p BatchProgress) Valid() bool {
	if p.plan.BatchCount() == 0 || p.index < 0 || p.index >= p.plan.BatchCount() {
		return false
	}
	switch p.state {
	case BatchProgressWaiting, BatchProgressComplete, BatchProgressBlocked:
		return true
	default:
		return false
	}
}

// Terminal reports whether the progress is complete or blocked. Terminal values cannot be
// reopened, changed to another batch, or applied again.
func (p BatchProgress) Terminal() bool {
	return p.Valid() && (p.state == BatchProgressComplete || p.state == BatchProgressBlocked)
}

// ApplyBatch applies trusted results to the current batch and returns the next progress value
// plus the reused BatchOutcome. Incomplete leaves the value waiting on the same batch;
// Advance moves exactly one batch; Blocked and Complete produce terminal values. Invalid
// results return the original value unchanged. This method never dispatches, retries, cancels
// or rolls back work, and a waiting value is not execution permission.
func (p BatchProgress) ApplyBatch(results []NodeResult) (BatchProgress, BatchOutcome, error) {
	if !p.Valid() {
		return p, "", ErrInvalidProgress
	}
	if p.Terminal() {
		return p, "", ErrProgressTerminal
	}

	outcome, err := p.plan.EvaluateBatch(p.index, results)
	if err != nil {
		return p, "", err
	}
	switch outcome {
	case BatchOutcomeIncomplete:
		return p, outcome, nil
	case BatchOutcomeAdvance:
		p.index++
	case BatchOutcomeBlocked:
		p.state = BatchProgressBlocked
	case BatchOutcomeComplete:
		p.state = BatchProgressComplete
	}
	return p, outcome, nil
}
