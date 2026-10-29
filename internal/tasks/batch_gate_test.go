package tasks_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"serverfleet.local/serverfleet/internal/tasks"
)

func mustBatchPlan(t *testing.T, class tasks.OperationClass, count int) tasks.BatchPlan {
	t.Helper()
	plan, err := tasks.NewDefaultBatchPlan(mustSnapshot(t, makeIDs(count)...), class)
	if err != nil {
		t.Fatalf("NewDefaultBatchPlan returned error: %v", err)
	}
	return plan
}

func successResults(batch []string) []tasks.NodeResult {
	results := make([]tasks.NodeResult, len(batch))
	for index, nodeID := range batch {
		results[index] = tasks.NodeResult{NodeID: nodeID, Code: tasks.NodeResultSucceeded}
	}
	return results
}

func TestEvaluateBatchAdvanceAndComplete(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class tasks.OperationClass
		count int
	}{
		{name: "standard", class: tasks.OperationClassStandard, count: 6},
		{name: "root", class: tasks.OperationClassRoot, count: 6},
		{name: "modifying", class: tasks.OperationClassModifying, count: 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := mustBatchPlan(t, tc.class, tc.count)
			batches := plan.Batches()
			for index, batch := range batches {
				outcome, err := plan.EvaluateBatch(index, successResults(batch))
				if err != nil {
					t.Fatalf("batch %d returned error: %v", index, err)
				}
				want := tasks.BatchOutcomeAdvance
				if index == len(batches)-1 {
					want = tasks.BatchOutcomeComplete
				}
				if outcome != want {
					t.Fatalf("batch %d outcome = %v, want %v", index, outcome, want)
				}
			}
		})
	}
}

func TestEvaluateBatchFailureBlocksLaterBatches(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 11)
	batches := plan.Batches()
	for batchIndex, batch := range batches {
		for _, code := range []tasks.NodeResultCode{
			tasks.NodeResultFailed,
			tasks.NodeResultCanceled,
			tasks.NodeResultTimedOut,
		} {
			results := successResults(batch)
			results[len(results)-1].Code = code
			outcome, err := plan.EvaluateBatch(batchIndex, results)
			if err != nil {
				t.Fatalf("batch %d code %v returned error: %v", batchIndex, code, err)
			}
			if outcome != tasks.BatchOutcomeBlocked {
				t.Fatalf("batch %d code %v outcome = %v, want blocked", batchIndex, code, outcome)
			}
		}
	}
}

func TestSequentialDriverStopsAfterFailedBatch(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 11)
	for _, failIndex := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("fail batch %d", failIndex), func(t *testing.T) {
			batches := plan.Batches()
			evaluated := 0
			var last tasks.BatchOutcome
			for index, batch := range batches {
				results := successResults(batch)
				if index == failIndex {
					results[0].Code = tasks.NodeResultFailed
				}
				outcome, err := plan.EvaluateBatch(index, results)
				if err != nil {
					t.Fatalf("batch %d returned error: %v", index, err)
				}
				evaluated++
				last = outcome
				if outcome != tasks.BatchOutcomeAdvance {
					break
				}
			}
			if evaluated != failIndex+1 {
				t.Fatalf("evaluated %d batches after failure at %d, want %d", evaluated, failIndex, failIndex+1)
			}
			if last != tasks.BatchOutcomeBlocked {
				t.Fatalf("last outcome = %v, want blocked", last)
			}
		})
	}
}

func TestSequentialDriverCompletesAllBatches(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 11)
	batches := plan.Batches()
	evaluated := 0
	var last tasks.BatchOutcome
	for index, batch := range batches {
		outcome, err := plan.EvaluateBatch(index, successResults(batch))
		if err != nil {
			t.Fatalf("batch %d returned error: %v", index, err)
		}
		evaluated++
		last = outcome
		if outcome != tasks.BatchOutcomeAdvance {
			break
		}
	}
	if evaluated != len(batches) {
		t.Fatalf("evaluated %d batches, want %d", evaluated, len(batches))
	}
	if last != tasks.BatchOutcomeComplete {
		t.Fatalf("last outcome = %v, want complete", last)
	}
}

func TestEvaluateBatchSuccessThenFailureIsBlocked(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 6)
	batch := plan.Batches()[1]
	results := successResults(batch)
	results[0].Code = tasks.NodeResultSucceeded
	results[1].Code = tasks.NodeResultFailed
	outcome, err := plan.EvaluateBatch(1, results)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != tasks.BatchOutcomeBlocked {
		t.Fatalf("outcome = %v, want blocked", outcome)
	}
}

func TestEvaluateBatchIncompleteResults(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	batch := plan.Batches()[0]
	allSuccess := successResults(batch)

	missing := allSuccess[:len(allSuccess)-1]
	empty := []tasks.NodeResult{}

	running := successResults(batch)
	running[len(running)-1].Code = tasks.NodeResultRunning

	unknown := successResults(batch)
	unknown[len(unknown)-1].Code = tasks.NodeResultUnknown

	mixed := successResults(batch)
	mixed[0].Code = tasks.NodeResultSucceeded
	mixed[1].Code = tasks.NodeResultRunning
	mixed[2].Code = tasks.NodeResultUnknown

	for _, tc := range []struct {
		name    string
		results []tasks.NodeResult
	}{
		{name: "missing target", results: missing},
		{name: "empty results", results: empty},
		{name: "running target", results: running},
		{name: "unknown result", results: unknown},
		{name: "mixed success running unknown", results: mixed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := plan.EvaluateBatch(0, tc.results)
			if err != nil {
				t.Fatalf("EvaluateBatch returned error: %v", err)
			}
			if outcome != tasks.BatchOutcomeIncomplete {
				t.Fatalf("outcome = %v, want incomplete", outcome)
			}
		})
	}
}

func TestEvaluateBatchRejectsInvalidResultsAndIndex(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	batches := plan.Batches()
	batch0 := batches[0]
	batch1 := batches[1]

	duplicate := append(successResults(batch0), tasks.NodeResult{NodeID: batch0[0], Code: tasks.NodeResultSucceeded})
	if outcome, err := plan.EvaluateBatch(0, duplicate); !errors.Is(err, tasks.ErrDuplicateResult) {
		t.Fatalf("duplicate outcome/error = %q/%v, want ErrDuplicateResult", outcome, err)
	}

	outOfBatch := append(successResults(batch0), tasks.NodeResult{NodeID: batch1[0], Code: tasks.NodeResultSucceeded})
	if outcome, err := plan.EvaluateBatch(0, outOfBatch); !errors.Is(err, tasks.ErrOutOfBatchResult) {
		t.Fatalf("out-of-batch outcome/error = %q/%v, want ErrOutOfBatchResult", outcome, err)
	}

	unknownCode := successResults(batch0)
	unknownCode[0].Code = tasks.NodeResultCode("acknowledged")
	if outcome, err := plan.EvaluateBatch(0, unknownCode); !errors.Is(err, tasks.ErrUnknownResultCode) {
		t.Fatalf("unknown code outcome/error = %q/%v, want ErrUnknownResultCode", outcome, err)
	}

	exitCode := successResults(batch0)
	exitCode[0].Code = tasks.NodeResultCode("exit_code_zero")
	if _, err := plan.EvaluateBatch(0, exitCode); !errors.Is(err, tasks.ErrUnknownResultCode) {
		t.Fatalf("exit-code inference error = %v, want ErrUnknownResultCode", err)
	}

	for _, index := range []int{-1, plan.BatchCount()} {
		if outcome, err := plan.EvaluateBatch(index, nil); !errors.Is(err, tasks.ErrInvalidBatchIndex) {
			t.Fatalf("index %d outcome/error = %q/%v, want ErrInvalidBatchIndex", index, outcome, err)
		}
	}
	if _, err := (tasks.BatchPlan{}).EvaluateBatch(0, nil); !errors.Is(err, tasks.ErrInvalidBatchIndex) {
		t.Fatalf("zero plan error = %v, want ErrInvalidBatchIndex", err)
	}
}

func TestEvaluateBatchDoesNotMutateInputs(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	batchesBefore := plan.Batches()
	results := successResults(batchesBefore[0])
	resultsBefore := slices.Clone(results)

	outcome, err := plan.EvaluateBatch(0, results)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != tasks.BatchOutcomeAdvance {
		t.Fatalf("outcome = %v, want advance", outcome)
	}
	if !slices.Equal(results, resultsBefore) {
		t.Fatalf("results mutated: %v", results)
	}
	if !slices.EqualFunc(plan.Batches(), batchesBefore, func(a, b []string) bool {
		return slices.Equal(a, b)
	}) {
		t.Fatalf("plan mutated: %v", plan.Batches())
	}
}

func TestNodeResultCodeValid(t *testing.T) {
	for _, code := range []tasks.NodeResultCode{
		tasks.NodeResultSucceeded,
		tasks.NodeResultFailed,
		tasks.NodeResultCanceled,
		tasks.NodeResultTimedOut,
		tasks.NodeResultUnknown,
		tasks.NodeResultRunning,
	} {
		if !code.Valid() {
			t.Errorf("%q should be valid", code)
		}
	}
	for _, code := range []tasks.NodeResultCode{"", "acknowledged", "exit_code_zero", "partial"} {
		if code.Valid() {
			t.Errorf("%q should be invalid", code)
		}
	}
}
