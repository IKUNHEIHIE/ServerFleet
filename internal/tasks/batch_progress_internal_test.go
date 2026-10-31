package tasks

import (
	"errors"
	"testing"
)

func TestBatchProgressRejectsStructurallyInvalidValues(t *testing.T) {
	snapshot, err := NewTargetSnapshot([]string{"node-1"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewDefaultBatchPlan(snapshot, OperationClassStandard)
	if err != nil {
		t.Fatal(err)
	}

	for _, progress := range []BatchProgress{
		{},
		{plan: plan, index: -1, state: BatchProgressWaiting},
		{plan: plan, index: plan.BatchCount(), state: BatchProgressWaiting},
		{plan: plan, index: 0, state: BatchProgressState("bogus")},
	} {
		if progress.Valid() {
			t.Fatalf("invalid progress reported valid: state=%q index=%d", progress.State(), progress.CurrentBatchIndex())
		}
		if progress.Terminal() {
			t.Fatalf("invalid progress reported terminal: state=%q", progress.State())
		}
		if targets := progress.CurrentBatchTargets(); targets != nil {
			t.Fatalf("invalid progress returned targets: %v", targets)
		}
		if _, outcome, err := progress.ApplyBatch(nil); !errors.Is(err, ErrInvalidProgress) || outcome != "" {
			t.Fatalf("ApplyBatch outcome/error = %q/%v, want empty/ErrInvalidProgress", outcome, err)
		}
	}
}
