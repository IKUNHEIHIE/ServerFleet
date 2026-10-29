package tasks

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownResultCode = errors.New("tasks: unknown node result code")
	ErrDuplicateResult   = errors.New("tasks: duplicate node result")
	ErrOutOfBatchResult  = errors.New("tasks: result outside the current batch")
	ErrInvalidBatchIndex = errors.New("tasks: invalid batch index")
)

// NodeResultCode is a trusted terminal or in-flight result classification. Only
// NodeResultSucceeded is success evidence; ACK, zero exit codes and truncated output are not
// represented and must not be inferable from this type.
type NodeResultCode string

const (
	NodeResultSucceeded NodeResultCode = "succeeded"
	NodeResultFailed    NodeResultCode = "failed"
	NodeResultCanceled  NodeResultCode = "canceled"
	NodeResultTimedOut  NodeResultCode = "timed_out"
	NodeResultUnknown   NodeResultCode = "unknown"
	NodeResultRunning   NodeResultCode = "running"
)

// Valid reports whether c is a supported result classification.
func (c NodeResultCode) Valid() bool {
	switch c {
	case NodeResultSucceeded, NodeResultFailed, NodeResultCanceled, NodeResultTimedOut,
		NodeResultUnknown, NodeResultRunning:
		return true
	default:
		return false
	}
}

// NodeResult links one target to its trusted result classification.
type NodeResult struct {
	NodeID string
	Code   NodeResultCode
}

// BatchOutcome is the pure sequential gate between batches. Only BatchOutcomeAdvance permits
// starting the next batch; Complete means the last batch succeeded; Blocked means a failure,
// cancellation or timeout forbids starting any later batch; Incomplete means results are
// missing or not all targets have reliable success evidence and no later batch may start.
type BatchOutcome string

const (
	BatchOutcomeAdvance    BatchOutcome = "advance"
	BatchOutcomeComplete   BatchOutcome = "complete"
	BatchOutcomeBlocked    BatchOutcome = "blocked"
	BatchOutcomeIncomplete BatchOutcome = "incomplete"
)

// EvaluateBatch evaluates the current batch against trusted per-target results. It rejects
// duplicate, out-of-batch and unknown-code results. It never retries, cancels or rolls back a
// running batch; it only answers whether the next batch satisfies the sequential gate.
// The function is stateless: callers must evaluate batches in order, only after Advance, and
// supply results for the current execution; earlier failures and stale results are not
// detectable from this value alone.
func (p BatchPlan) EvaluateBatch(batchIndex int, results []NodeResult) (BatchOutcome, error) {
	if batchIndex < 0 || batchIndex >= len(p.batches) {
		return "", fmt.Errorf("%w: index %d", ErrInvalidBatchIndex, batchIndex)
	}
	batch := p.batches[batchIndex]
	inBatch := make(map[string]struct{}, len(batch))
	for _, id := range batch {
		inBatch[id] = struct{}{}
	}

	seen := make(map[string]struct{}, len(results))
	failed := false
	allSucceeded := true
	for index, result := range results {
		if !result.Code.Valid() {
			return "", fmt.Errorf("%w: result index %d", ErrUnknownResultCode, index)
		}
		if _, ok := inBatch[result.NodeID]; !ok {
			return "", fmt.Errorf("%w: result index %d", ErrOutOfBatchResult, index)
		}
		if _, duplicate := seen[result.NodeID]; duplicate {
			return "", fmt.Errorf("%w: result index %d", ErrDuplicateResult, index)
		}
		seen[result.NodeID] = struct{}{}

		switch result.Code {
		case NodeResultFailed, NodeResultCanceled, NodeResultTimedOut:
			failed = true
		case NodeResultSucceeded:
		default:
			allSucceeded = false
		}
	}

	if failed {
		return BatchOutcomeBlocked, nil
	}
	if len(seen) != len(batch) || !allSucceeded {
		return BatchOutcomeIncomplete, nil
	}
	if batchIndex+1 < len(p.batches) {
		return BatchOutcomeAdvance, nil
	}
	return BatchOutcomeComplete, nil
}
