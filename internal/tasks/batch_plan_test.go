package tasks_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"serverfleet.local/serverfleet/internal/tasks"
)

func mustSnapshot(t *testing.T, ids ...string) tasks.TargetSnapshot {
	t.Helper()
	snapshot, err := tasks.NewTargetSnapshot(ids)
	if err != nil {
		t.Fatalf("NewTargetSnapshot returned error: %v", err)
	}
	return snapshot
}

func makeIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("node-%02d", i)
	}
	return ids
}

func flattenBatches(batches [][]string) []string {
	var flat []string
	for _, batch := range batches {
		flat = append(flat, batch...)
	}
	return flat
}

func TestDefaultBatchPlanSizes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class tasks.OperationClass
		count int
		want  []int
	}{
		{name: "standard 1", class: tasks.OperationClassStandard, count: 1, want: []int{1}},
		{name: "standard 5", class: tasks.OperationClassStandard, count: 5, want: []int{5}},
		{name: "standard 6", class: tasks.OperationClassStandard, count: 6, want: []int{5, 1}},
		{name: "standard 11", class: tasks.OperationClassStandard, count: 11, want: []int{5, 5, 1}},
		{name: "standard 12", class: tasks.OperationClassStandard, count: 12, want: []int{5, 5, 2}},
		{name: "standard 13", class: tasks.OperationClassStandard, count: 13, want: []int{5, 5, 3}},
		{name: "root 1", class: tasks.OperationClassRoot, count: 1, want: []int{1}},
		{name: "root 5", class: tasks.OperationClassRoot, count: 5, want: []int{1, 4}},
		{name: "root 6", class: tasks.OperationClassRoot, count: 6, want: []int{1, 5}},
		{name: "root 11", class: tasks.OperationClassRoot, count: 11, want: []int{1, 5, 5}},
		{name: "root 12", class: tasks.OperationClassRoot, count: 12, want: []int{1, 5, 5, 1}},
		{name: "root 13", class: tasks.OperationClassRoot, count: 13, want: []int{1, 5, 5, 2}},
		{name: "modifying 1", class: tasks.OperationClassModifying, count: 1, want: []int{1}},
		{name: "modifying 5", class: tasks.OperationClassModifying, count: 5, want: []int{1, 4}},
		{name: "modifying 6", class: tasks.OperationClassModifying, count: 6, want: []int{1, 5}},
		{name: "modifying 11", class: tasks.OperationClassModifying, count: 11, want: []int{1, 5, 5}},
		{name: "modifying 12", class: tasks.OperationClassModifying, count: 12, want: []int{1, 5, 5, 1}},
		{name: "modifying 13", class: tasks.OperationClassModifying, count: 13, want: []int{1, 5, 5, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := makeIDs(tc.count)
			slices.Reverse(ids)
			plan, err := tasks.NewDefaultBatchPlan(mustSnapshot(t, ids...), tc.class)
			if err != nil {
				t.Fatalf("NewDefaultBatchPlan returned error: %v", err)
			}
			batches := plan.Batches()
			if plan.BatchCount() != len(tc.want) {
				t.Fatalf("BatchCount = %d, want %d", plan.BatchCount(), len(tc.want))
			}
			gotSizes := make([]int, len(batches))
			for index, batch := range batches {
				gotSizes[index] = len(batch)
			}
			if !slices.Equal(gotSizes, tc.want) {
				t.Fatalf("batch sizes = %v, want %v", gotSizes, tc.want)
			}
			if got := flattenBatches(batches); !slices.Equal(got, ids) {
				t.Fatalf("flattened batches = %v, want ordered targets %v", got, ids)
			}
		})
	}
}

func TestDefaultBatchPlanCopiesOutput(t *testing.T) {
	ids := makeIDs(12)
	slices.Reverse(ids)
	plan, err := tasks.NewDefaultBatchPlan(mustSnapshot(t, ids...), tasks.OperationClassStandard)
	if err != nil {
		t.Fatal(err)
	}

	first := plan.Batches()
	first[0][0] = "mutated"
	first[1] = append(first[1], "extra")
	first[2] = nil

	second := plan.Batches()
	if got := flattenBatches(second); !slices.Equal(got, ids) {
		t.Fatalf("plan changed through returned batches: %v", got)
	}
	if len(second[2]) != 2 {
		t.Fatalf("last batch length = %d, want 2", len(second[2]))
	}
}

func TestNewDefaultBatchPlanRejectsEmptySnapshotAndUnknownClass(t *testing.T) {
	if _, err := tasks.NewDefaultBatchPlan(tasks.TargetSnapshot{}, tasks.OperationClassStandard); !errors.Is(err, tasks.ErrEmptyTargets) {
		t.Fatalf("empty snapshot error = %v, want ErrEmptyTargets", err)
	}
	snapshot := mustSnapshot(t, "node-1")
	for _, class := range []tasks.OperationClass{"", "bogus"} {
		if _, err := tasks.NewDefaultBatchPlan(snapshot, class); !errors.Is(err, tasks.ErrUnknownOperationClass) {
			t.Fatalf("class %q error = %v, want ErrUnknownOperationClass", class, err)
		}
	}
}

func TestDefaultBatchSizes(t *testing.T) {
	if tasks.DefaultStandardBatchSize != 5 ||
		tasks.DefaultStagedFirstBatchSize != 1 ||
		tasks.DefaultStagedBatchSize != 5 {
		t.Fatalf("defaults = %d/%d/%d, want 5/1/5",
			tasks.DefaultStandardBatchSize, tasks.DefaultStagedFirstBatchSize, tasks.DefaultStagedBatchSize)
	}
}
