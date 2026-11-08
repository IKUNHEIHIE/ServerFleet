package xraymanager

import (
	"errors"
	"testing"
)

func batchNodes() []NodeTarget {
	return []NodeTarget{
		{NodeID: "n-1", Name: "one", Online: true, Architecture: "linux/amd64"},
		{NodeID: "n-2", Name: "two", Online: true, Architecture: "linux/arm64"},
		{NodeID: "n-3", Name: "three", Online: false, Architecture: "linux/amd64"},
		{NodeID: "n-4", Name: "four", Online: true},
		{NodeID: "n-5", Name: "five", Online: true, Architecture: "linux/amd64", Managed: true},
		{NodeID: "n-6", Name: "six", Online: true, Architecture: "linux/amd64", PortConflict: true},
	}
}

func batchPolicy() BatchPolicy {
	return BatchPolicy{Mode: BatchBatch, BatchSize: 2, Concurrency: 2, MaxFailures: 1, StopOnHealthFailure: true}
}

func TestPlanBatchBuildsOrderedWavesAndStableDigest(t *testing.T) {
	plan, err := PlanBatch([]string{"n-2", "n-1"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Eligible || len(plan.Issues) != 0 {
		t.Fatalf("expected eligible plan: %+v", plan)
	}
	if len(plan.Targets) != 2 || plan.Targets[0] != "n-1" || plan.Targets[1] != "n-2" {
		t.Fatalf("targets must be sorted: %+v", plan.Targets)
	}
	if len(plan.Waves) != 1 || plan.Waves[0].NodeIDs[0] != "n-1" {
		t.Fatalf("unexpected waves: %+v", plan.Waves)
	}
	again, err := PlanBatch([]string{"n-1", "n-2"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan again: %v", err)
	}
	if again.Digest != plan.Digest {
		t.Fatalf("digest must be order independent: %s != %s", again.Digest, plan.Digest)
	}
	drifted, err := PlanBatch([]string{"n-1"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan drifted: %v", err)
	}
	if drifted.Digest == plan.Digest {
		t.Fatal("different targets must not share a digest")
	}
}

func TestPlanBatchReportsEveryIneligibleTarget(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1", "n-3", "n-4", "n-5", "n-6", "n-9"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Eligible {
		t.Fatal("plan with issues must not be eligible")
	}
	codes := map[string]string{}
	for _, issue := range plan.Issues {
		codes[issue.NodeID] = issue.Code
	}
	want := map[string]string{
		"n-3": "node_offline",
		"n-4": "architecture_unknown",
		"n-5": "existing_instance",
		"n-6": "port_conflict",
		"n-9": "unknown_node",
	}
	for id, code := range want {
		if codes[id] != code {
			t.Fatalf("%s: want %s, got %q (all=%v)", id, code, codes[id], codes)
		}
	}
	if !plan.RequiresAdoption {
		t.Fatal("managed instance must require adoption approval")
	}
	if len(plan.Waves) != 1 || plan.Waves[0].NodeIDs[0] != "n-1" {
		t.Fatalf("eligible node must still be planned: %+v", plan.Waves)
	}
}

func TestPlanBatchAdoptionAllowsExistingInstance(t *testing.T) {
	plan, err := PlanBatch([]string{"n-5"}, batchNodes(), batchPolicy(), true)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Eligible || plan.RequiresAdoption {
		t.Fatalf("explicit adoption must clear the block: %+v", plan)
	}
}

func TestPlanBatchRejectsMalformedInput(t *testing.T) {
	bad := []BatchPolicy{
		{Mode: "weird", Concurrency: 1},
		{Mode: BatchBatch, BatchSize: 0, Concurrency: 1},
		{Mode: BatchNormal, BatchSize: 2, Concurrency: 1},
		{Mode: BatchNormal, Concurrency: 0},
		{Mode: BatchNormal, Concurrency: 99},
		{Mode: BatchNormal, Concurrency: 1, MaxFailures: -1},
	}
	for _, policy := range bad {
		if _, err := PlanBatch([]string{"n-1"}, batchNodes(), policy, false); !errors.Is(err, ErrBatchSpec) {
			t.Fatalf("policy %+v: want ErrBatchSpec, got %v", policy, err)
		}
	}
	if _, err := PlanBatch([]string{"n-1", "n-1"}, batchNodes(), batchPolicy(), false); !errors.Is(err, ErrBatchSpec) {
		t.Fatal("duplicate targets must be rejected")
	}
	if _, err := PlanBatch(nil, batchNodes(), batchPolicy(), false); !errors.Is(err, ErrBatchSpec) {
		t.Fatal("empty target list must be rejected")
	}
	if _, err := PlanBatch([]string{"n-1"}, []NodeTarget{{NodeID: "n-1"}, {NodeID: "n-1"}}, batchPolicy(), false); !errors.Is(err, ErrBatchSpec) {
		t.Fatal("duplicate node inventory entries must be rejected")
	}
}

func TestApproveBatchBindsDigestTargetsAndAdoption(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1", "n-2"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	ok := BatchApproval{Digest: plan.Digest, Targets: []string{"n-2", "n-1"}, ApprovedBy: "admin-1", ApprovalID: "ap-1"}
	if err := ApproveBatch(plan, ok); err != nil {
		t.Fatalf("approve: %v", err)
	}
	cases := map[string]BatchApproval{
		"digest drift":   {Digest: "deadbeef", Targets: plan.Targets, ApprovedBy: "admin-1", ApprovalID: "ap-1"},
		"target drift":   {Digest: plan.Digest, Targets: []string{"n-1"}, ApprovedBy: "admin-1", ApprovalID: "ap-1"},
		"extra target":   {Digest: plan.Digest, Targets: []string{"n-1", "n-2", "n-3"}, ApprovedBy: "admin-1", ApprovalID: "ap-1"},
		"no approver":    {Digest: plan.Digest, Targets: plan.Targets, ApprovalID: "ap-1"},
		"no approval id": {Digest: plan.Digest, Targets: plan.Targets, ApprovedBy: "admin-1"},
	}
	for name, approval := range cases {
		if err := ApproveBatch(plan, approval); !errors.Is(err, ErrBatchSpec) {
			t.Fatalf("%s: want ErrBatchSpec, got %v", name, err)
		}
	}

	adoptionPlan, err := PlanBatch([]string{"n-5"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan adoption: %v", err)
	}
	without := BatchApproval{Digest: adoptionPlan.Digest, Targets: adoptionPlan.Targets, ApprovedBy: "admin-1", ApprovalID: "ap-1"}
	if err := ApproveBatch(adoptionPlan, without); !errors.Is(err, ErrBatchSpec) {
		t.Fatal("adoption without consent must be rejected")
	}
	without.AdoptionAgreed = true
	if err := ApproveBatch(adoptionPlan, without); err != nil {
		t.Fatalf("adoption with consent: %v", err)
	}
}

func TestShouldStopHonoursFailureBudgetAndHealth(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if ShouldStop(plan, WaveOutcomes{Failed: 1}, false) {
		t.Fatal("failure equal to budget must not stop")
	}
	if !ShouldStop(plan, WaveOutcomes{Failed: 2}, false) {
		t.Fatal("failure above budget must stop")
	}
	if !ShouldStop(plan, WaveOutcomes{}, true) {
		t.Fatal("health failure must stop when policy requires it")
	}
	relaxed := plan
	relaxed.Policy.StopOnHealthFailure = false
	if ShouldStop(relaxed, WaveOutcomes{}, true) {
		t.Fatal("health failure must not stop when policy allows it")
	}
	if !ShouldStop(BatchPlan{}, WaveOutcomes{}, false) {
		t.Fatal("empty plan must stop")
	}
}
