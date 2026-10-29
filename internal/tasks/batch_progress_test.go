package tasks_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"serverfleet.local/serverfleet/internal/tasks"
)

func mustStartProgress(t *testing.T, plan tasks.BatchPlan) tasks.BatchProgress {
	t.Helper()
	progress, err := tasks.StartBatchProgress(plan)
	if err != nil {
		t.Fatalf("StartBatchProgress returned error: %v", err)
	}
	return progress
}

func assertSameProgress(t *testing.T, got, want tasks.BatchProgress) {
	t.Helper()
	if got.State() != want.State() ||
		got.CurrentBatchIndex() != want.CurrentBatchIndex() ||
		got.BatchCount() != want.BatchCount() ||
		got.Valid() != want.Valid() ||
		got.Terminal() != want.Terminal() ||
		!slices.Equal(got.CurrentBatchTargets(), want.CurrentBatchTargets()) {
		t.Fatalf("progress changed: got state=%q index=%d valid=%t terminal=%t targets=%v, want state=%q index=%d valid=%t terminal=%t targets=%v",
			got.State(), got.CurrentBatchIndex(), got.Valid(), got.Terminal(), got.CurrentBatchTargets(),
			want.State(), want.CurrentBatchIndex(), want.Valid(), want.Terminal(), want.CurrentBatchTargets())
	}
}

func TestStartBatchProgressRejectsZeroPlan(t *testing.T) {
	if _, err := tasks.StartBatchProgress(tasks.BatchPlan{}); !errors.Is(err, tasks.ErrInvalidBatchPlan) {
		t.Fatalf("zero plan error = %v, want ErrInvalidBatchPlan", err)
	}
}

func TestStartBatchProgressBeginsAtFirstBatchWithCopiedTargets(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 6)
	progress := mustStartProgress(t, plan)

	if !progress.Valid() || progress.Terminal() {
		t.Fatalf("new progress valid/terminal = %t/%t, want true/false", progress.Valid(), progress.Terminal())
	}
	if progress.State() != tasks.BatchProgressWaiting {
		t.Fatalf("state = %q, want waiting", progress.State())
	}
	if progress.CurrentBatchIndex() != 0 {
		t.Fatalf("index = %d, want 0", progress.CurrentBatchIndex())
	}
	if progress.BatchCount() != plan.BatchCount() {
		t.Fatalf("batch count = %d, want %d", progress.BatchCount(), plan.BatchCount())
	}
	want := plan.Batches()[0]
	if got := progress.CurrentBatchTargets(); !slices.Equal(got, want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}

	targets := progress.CurrentBatchTargets()
	targets[0] = "mutated"
	if got := progress.CurrentBatchTargets(); !slices.Equal(got, want) {
		t.Fatalf("targets changed through returned copy: %v", got)
	}
}

func TestBatchProgressMovesSequentiallyAndCompletes(t *testing.T) {
	for _, class := range []struct {
		name  string
		class tasks.OperationClass
	}{
		{name: "standard", class: tasks.OperationClassStandard},
		{name: "root", class: tasks.OperationClassRoot},
		{name: "modifying", class: tasks.OperationClassModifying},
	} {
		t.Run(class.name, func(t *testing.T) {
			plan := mustBatchPlan(t, class.class, 11)
			batches := plan.Batches()
			progress := mustStartProgress(t, plan)
			for index, batch := range batches {
				previous := progress
				next, outcome, err := progress.ApplyBatch(successResults(batch))
				if err != nil {
					t.Fatalf("batch %d returned error: %v", index, err)
				}
				if index < len(batches)-1 {
					if outcome != tasks.BatchOutcomeAdvance {
						t.Fatalf("batch %d outcome = %v, want advance", index, outcome)
					}
					if next.State() != tasks.BatchProgressWaiting || next.CurrentBatchIndex() != index+1 {
						t.Fatalf("batch %d next state/index = %q/%d, want waiting/%d", index, next.State(), next.CurrentBatchIndex(), index+1)
					}
					if got := next.CurrentBatchTargets(); !slices.Equal(got, batches[index+1]) {
						t.Fatalf("next targets = %v, want %v", got, batches[index+1])
					}
				} else {
					if outcome != tasks.BatchOutcomeComplete {
						t.Fatalf("last batch outcome = %v, want complete", outcome)
					}
					if next.State() != tasks.BatchProgressComplete || !next.Terminal() || next.CurrentBatchIndex() != index {
						t.Fatalf("last progress state/index/terminal = %q/%d/%t", next.State(), next.CurrentBatchIndex(), next.Terminal())
					}
					if got := next.CurrentBatchTargets(); !slices.Equal(got, batch) {
						t.Fatalf("last targets = %v, want %v", got, batch)
					}
				}
				if previous.State() != tasks.BatchProgressWaiting || previous.CurrentBatchIndex() != index {
					t.Fatalf("old progress changed: state=%q index=%d", previous.State(), previous.CurrentBatchIndex())
				}
				progress = next
			}
		})
	}
}

func TestBatchProgressSingleBatchCompletes(t *testing.T) {
	for _, class := range []tasks.OperationClass{
		tasks.OperationClassStandard,
		tasks.OperationClassRoot,
		tasks.OperationClassModifying,
	} {
		t.Run(string(class), func(t *testing.T) {
			plan := mustBatchPlan(t, class, 1)
			progress := mustStartProgress(t, plan)
			next, outcome, err := progress.ApplyBatch(successResults(plan.Batches()[0]))
			if err != nil {
				t.Fatal(err)
			}
			if outcome != tasks.BatchOutcomeComplete || next.State() != tasks.BatchProgressComplete || !next.Terminal() {
				t.Fatalf("outcome/state/terminal = %v/%q/%t, want complete/complete/true", outcome, next.State(), next.Terminal())
			}
			if next.CurrentBatchIndex() != 0 {
				t.Fatalf("index = %d, want 0", next.CurrentBatchIndex())
			}
		})
	}
}

func TestBatchProgressIncompleteWaitsInPlace(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	batch := plan.Batches()[0]
	allSuccess := successResults(batch)
	running := successResults(batch)
	running[len(running)-1].Code = tasks.NodeResultRunning
	unknown := successResults(batch)
	unknown[len(unknown)-1].Code = tasks.NodeResultUnknown
	mixed := successResults(batch)
	mixed[0].Code = tasks.NodeResultRunning
	mixed[1].Code = tasks.NodeResultUnknown

	progress := mustStartProgress(t, plan)
	for _, tc := range []struct {
		name    string
		results []tasks.NodeResult
	}{
		{name: "missing target", results: allSuccess[:len(allSuccess)-1]},
		{name: "empty results", results: []tasks.NodeResult{}},
		{name: "running", results: running},
		{name: "unknown", results: unknown},
		{name: "mixed running and unknown", results: mixed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, outcome, err := progress.ApplyBatch(tc.results)
			if err != nil {
				t.Fatalf("ApplyBatch returned error: %v", err)
			}
			if outcome != tasks.BatchOutcomeIncomplete {
				t.Fatalf("outcome = %v, want incomplete", outcome)
			}
			assertSameProgress(t, next, progress)
		})
	}

	next, outcome, err := progress.ApplyBatch(allSuccess)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != tasks.BatchOutcomeAdvance || next.CurrentBatchIndex() != 1 || next.State() != tasks.BatchProgressWaiting {
		t.Fatalf("after evidence outcome/state/index = %v/%q/%d", outcome, next.State(), next.CurrentBatchIndex())
	}
	if progress.CurrentBatchIndex() != 0 {
		t.Fatalf("old progress index = %d, want 0", progress.CurrentBatchIndex())
	}
}

func TestBatchProgressFailureTerminates(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 11)
	batches := plan.Batches()
	for failIndex := range batches {
		for _, code := range []tasks.NodeResultCode{
			tasks.NodeResultFailed,
			tasks.NodeResultCanceled,
			tasks.NodeResultTimedOut,
		} {
			t.Run(fmt.Sprintf("fail batch %d/%s", failIndex, code), func(t *testing.T) {
				progress := mustStartProgress(t, plan)
				for index := 0; index < failIndex; index++ {
					next, outcome, err := progress.ApplyBatch(successResults(batches[index]))
					if err != nil || outcome != tasks.BatchOutcomeAdvance {
						t.Fatalf("prefix batch %d outcome/error = %v/%v", index, outcome, err)
					}
					progress = next
				}
				results := successResults(batches[failIndex])
				results[len(results)-1].Code = code
				blocked, outcome, err := progress.ApplyBatch(results)
				if err != nil {
					t.Fatalf("ApplyBatch returned error: %v", err)
				}
				if outcome != tasks.BatchOutcomeBlocked {
					t.Fatalf("outcome = %v, want blocked", outcome)
				}
				if blocked.State() != tasks.BatchProgressBlocked || !blocked.Terminal() || blocked.CurrentBatchIndex() != failIndex {
					t.Fatalf("blocked state/index/terminal = %q/%d/%t", blocked.State(), blocked.CurrentBatchIndex(), blocked.Terminal())
				}
				if got := blocked.CurrentBatchTargets(); !slices.Equal(got, batches[failIndex]) {
					t.Fatalf("blocked targets = %v, want %v", got, batches[failIndex])
				}

				again, outcome, err := blocked.ApplyBatch(successResults(batches[failIndex]))
				if !errors.Is(err, tasks.ErrProgressTerminal) {
					t.Fatalf("terminal ApplyBatch error = %v, want ErrProgressTerminal", err)
				}
				if outcome != "" {
					t.Fatalf("terminal outcome = %q, want empty", outcome)
				}
				assertSameProgress(t, again, blocked)

				if failIndex+1 < len(batches) {
					reopened, nextOutcome, nextErr := blocked.ApplyBatch(successResults(batches[failIndex+1]))
					if !errors.Is(nextErr, tasks.ErrProgressTerminal) {
						t.Fatalf("terminal next-batch error = %v, want ErrProgressTerminal", nextErr)
					}
					if nextOutcome != "" {
						t.Fatalf("terminal next-batch outcome = %q, want empty", nextOutcome)
					}
					assertSameProgress(t, reopened, blocked)
				}
			})
		}
	}
}

func TestBatchProgressCompleteCannotReopen(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	progress := mustStartProgress(t, plan)
	batches := plan.Batches()
	progress, _, err := progress.ApplyBatch(successResults(batches[0]))
	if err != nil {
		t.Fatal(err)
	}
	completed, outcome, err := progress.ApplyBatch(successResults(batches[1]))
	if err != nil || outcome != tasks.BatchOutcomeComplete {
		t.Fatalf("outcome/error = %v/%v, want complete", outcome, err)
	}
	again, outcome, err := completed.ApplyBatch(successResults(batches[1]))
	if !errors.Is(err, tasks.ErrProgressTerminal) {
		t.Fatalf("terminal error = %v, want ErrProgressTerminal", err)
	}
	if outcome != "" {
		t.Fatalf("terminal outcome = %q, want empty", outcome)
	}
	assertSameProgress(t, again, completed)
}

func TestBatchProgressRejectsInvalidResultsWithoutChanging(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassStandard, 6)
	batches := plan.Batches()
	batch0 := batches[0]
	batch1 := batches[1]
	progress := mustStartProgress(t, plan)

	duplicate := append(successResults(batch0), tasks.NodeResult{NodeID: batch0[0], Code: tasks.NodeResultSucceeded})
	outOfBatch := append(successResults(batch0), tasks.NodeResult{NodeID: batch1[0], Code: tasks.NodeResultSucceeded})
	unknownCode := successResults(batch0)
	unknownCode[0].Code = tasks.NodeResultCode("acknowledged")

	for _, tc := range []struct {
		name    string
		results []tasks.NodeResult
		want    error
	}{
		{name: "duplicate result", results: duplicate, want: tasks.ErrDuplicateResult},
		{name: "out of batch result", results: outOfBatch, want: tasks.ErrOutOfBatchResult},
		{name: "unknown result code", results: unknownCode, want: tasks.ErrUnknownResultCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, outcome, err := progress.ApplyBatch(tc.results)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if outcome != "" {
				t.Fatalf("outcome = %q, want empty", outcome)
			}
			assertSameProgress(t, next, progress)
		})
	}

	advanced, outcome, err := progress.ApplyBatch(successResults(batch0))
	if err != nil || outcome != tasks.BatchOutcomeAdvance {
		t.Fatalf("valid batch outcome/error = %v/%v", outcome, err)
	}
	next, outcome, err := advanced.ApplyBatch(successResults(batch0))
	if !errors.Is(err, tasks.ErrOutOfBatchResult) {
		t.Fatalf("cross-batch old results error = %v, want ErrOutOfBatchResult", err)
	}
	if outcome != "" {
		t.Fatalf("cross-batch outcome = %q, want empty", outcome)
	}
	assertSameProgress(t, next, advanced)
}

func TestZeroBatchProgressRejected(t *testing.T) {
	var zero tasks.BatchProgress
	if zero.Valid() || zero.Terminal() {
		t.Fatalf("zero valid/terminal = %t/%t, want false/false", zero.Valid(), zero.Terminal())
	}
	if zero.State() != "" || zero.CurrentBatchIndex() != 0 || zero.BatchCount() != 0 || zero.CurrentBatchTargets() != nil {
		t.Fatalf("zero accessors state/index/count/targets = %q/%d/%d/%v", zero.State(), zero.CurrentBatchIndex(), zero.BatchCount(), zero.CurrentBatchTargets())
	}
	next, outcome, err := zero.ApplyBatch(nil)
	if !errors.Is(err, tasks.ErrInvalidProgress) {
		t.Fatalf("zero ApplyBatch error = %v, want ErrInvalidProgress", err)
	}
	if outcome != "" || next.Valid() {
		t.Fatalf("zero ApplyBatch outcome/next valid = %q/%t, want empty/false", outcome, next.Valid())
	}
}

func TestBatchProgressCopyAndInputIsolation(t *testing.T) {
	plan := mustBatchPlan(t, tasks.OperationClassRoot, 6)
	progress := mustStartProgress(t, plan)
	results := successResults(plan.Batches()[0])
	resultsBefore := slices.Clone(results)

	next, outcome, err := progress.ApplyBatch(results)
	if err != nil || outcome != tasks.BatchOutcomeAdvance {
		t.Fatalf("outcome/error = %v/%v", outcome, err)
	}
	if !slices.Equal(results, resultsBefore) {
		t.Fatalf("ApplyBatch mutated input results: %v", results)
	}
	if progress.CurrentBatchIndex() != 0 || progress.State() != tasks.BatchProgressWaiting {
		t.Fatalf("old progress changed: state=%q index=%d", progress.State(), progress.CurrentBatchIndex())
	}
	if next.CurrentBatchIndex() != 1 || next.State() != tasks.BatchProgressWaiting {
		t.Fatalf("next progress state/index = %q/%d, want waiting/1", next.State(), next.CurrentBatchIndex())
	}

	targets := next.CurrentBatchTargets()
	targets[0] = "mutated"
	if got := next.CurrentBatchTargets(); got[0] != plan.Batches()[1][0] {
		t.Fatalf("next progress changed through returned targets: %v", got)
	}

	planCopy := plan.Batches()
	planCopy[1][0] = "mutated-plan"
	if got := next.CurrentBatchTargets(); got[0] != plan.Batches()[1][0] {
		t.Fatalf("progress changed through plan copy: %v", got)
	}
}
