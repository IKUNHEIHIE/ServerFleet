package tasks

import "errors"

const (
	// DefaultStandardBatchSize is the approved default maximum for ordinary tasks.
	DefaultStandardBatchSize = 5
	// DefaultStagedFirstBatchSize is the approved first batch size for root/modifying tasks.
	DefaultStagedFirstBatchSize = 1
	// DefaultStagedBatchSize is the approved maximum for later staged batches.
	DefaultStagedBatchSize = 5
)

var ErrUnknownOperationClass = errors.New("tasks: unknown operation class")

// OperationClass must come from the trusted action registry or task service, not from an AI
// or client claim. It determines batch shape only; it does not prove permissions or approval.
type OperationClass string

const (
	OperationClassStandard  OperationClass = "standard"
	OperationClassRoot      OperationClass = "root"
	OperationClassModifying OperationClass = "modifying"
)

// Valid reports whether c is a supported trusted operation class.
func (c OperationClass) Valid() bool {
	switch c {
	case OperationClassStandard, OperationClassRoot, OperationClassModifying:
		return true
	default:
		return false
	}
}

// BatchPlan is an immutable ordered partition of a frozen target snapshot. The zero value has
// no batches and must not be used; create plans with NewDefaultBatchPlan.
type BatchPlan struct {
	batches [][]string
}

// NewDefaultBatchPlan partitions the snapshot with approved defaults: ordinary tasks use
// batches of at most 5; root/modifying tasks use a first batch of 1 followed by batches of at
// most 5. Empty snapshots and unknown classes are rejected. This plan does not dispatch or
// authorize anything.
func NewDefaultBatchPlan(snapshot TargetSnapshot, class OperationClass) (BatchPlan, error) {
	if !class.Valid() {
		return BatchPlan{}, ErrUnknownOperationClass
	}
	ids := snapshot.IDs()
	if len(ids) == 0 {
		return BatchPlan{}, ErrEmptyTargets
	}

	var batches [][]string
	switch class {
	case OperationClassStandard:
		batches = partition(ids, 0, DefaultStandardBatchSize)
	default:
		batches = partition(ids, DefaultStagedFirstBatchSize, DefaultStagedBatchSize)
	}
	return BatchPlan{batches: batches}, nil
}

// partition splits ids into firstSize (when positive) followed by chunks of size at most size.
func partition(ids []string, firstSize, size int) [][]string {
	batches := make([][]string, 0, (len(ids)+size-1)/size)
	start := 0
	if firstSize > 0 {
		batches = append(batches, ids[:firstSize])
		start = firstSize
	}
	for start < len(ids) {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		batches = append(batches, ids[start:end])
		start = end
	}
	return batches
}

// Batches returns a deep copy of the ordered batches. Callers can mutate the returned slices
// without changing the plan.
func (p BatchPlan) Batches() [][]string {
	copied := make([][]string, len(p.batches))
	for index, batch := range p.batches {
		copied[index] = make([]string, len(batch))
		copy(copied[index], batch)
	}
	return copied
}

// BatchCount returns the number of planned batches.
func (p BatchPlan) BatchCount() int {
	return len(p.batches)
}
