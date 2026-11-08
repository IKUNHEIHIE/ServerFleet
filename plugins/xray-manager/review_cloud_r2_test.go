package xraymanager

import (
	"math"
	"testing"
	"time"
)

// 主控首审 V09-CLOUD-REVIEW-R2-01 的独立回归（W3/W4）。
//
// 三条原样回归直接来自审查正文，必须 PASS；其余为本轮新增的负向覆盖：
// 段不匹配、缺来源、计数器倒退、窗口重放、80% 溢出，以及“改完计划再重算
// digest”这种 digest 字符串比较挡不住的漂移。

func TestReviewR2QuotaResetRoundTripPreservesUsedBytes(t *testing.T) {
	policy := quotaPolicy()
	known := QuotaBaseline{SegmentID: "seg-1"}
	usage := QuotaUsage{Available: true, Uplink: 100, Downlink: 50, SegmentID: "seg-1", At: at(4)}
	before, err := EvaluateQuota(policy, known, usage, at(4))
	if err != nil {
		t.Fatal(err)
	}
	reset, err := PlanProcessReset(resetRequest(), Baseline{Generation: 1, SegmentID: "seg-1"}, known, usage)
	if err != nil {
		t.Fatal(err)
	}
	after, err := EvaluateQuota(policy, reset.Baseline, QuotaUsage{Available: true, Uplink: 1, SegmentID: "seg-2", At: at(5)}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	if reset.AccountingUnknown || after.Unknown || after.UsedBytes != before.UsedBytes+1 {
		t.Fatalf("known reset loses accounting: before=%+v reset=%+v after=%+v", before, reset, after)
	}
}

func TestReviewR2QuotaRolloverChargesAllNewWindowBytes(t *testing.T) {
	policy := quotaPolicy()
	policy.ResetOnWindow = true
	policy.Period = 24 * time.Hour
	known := QuotaBaseline{Uplink: 10, ConsumedBytes: 800, WindowStart: at(1).Add(-48 * time.Hour), SegmentID: "seg-1"}
	next, err := WindowRollover(policy, known, QuotaUsage{Available: true, Uplink: 100, SegmentID: "seg-1"}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	after, err := EvaluateQuota(policy, next, QuotaUsage{Available: true, Uplink: 120, SegmentID: "seg-1"}, at(6))
	if err != nil {
		t.Fatal(err)
	}
	if after.Unknown || after.UsedBytes != 20 {
		t.Fatalf("new window should charge 20, got %+v (baseline=%+v)", after, next)
	}
}

func TestReviewR2BatchApprovalRejectsChangedPolicy(t *testing.T) {
	p, err := PlanBatch([]string{"n1"}, []NodeTarget{{NodeID: "n1", Online: true, Architecture: "amd64"}}, BatchPolicy{Mode: BatchNormal, Concurrency: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	approval := BatchApproval{Digest: p.Digest, Targets: []string{"n1"}, ApprovedBy: "admin", ApprovalID: "approval-1"}
	p.Policy.Concurrency = 16
	if err := ApproveBatch(p, approval); err == nil {
		t.Fatal("approval for old digest accepted changed concurrency=16")
	}
}

// ---- W3 补充负向 ----

func TestReviewR2QuotaResetRefusesWithoutZeroing(t *testing.T) {
	request := resetRequest()

	// 缺来源：不能清零，也不能把基线当成已冻结。
	unknownSource := QuotaBaseline{Uplink: 40, Downlink: 10, ConsumedBytes: 900, SegmentID: "seg-1"}
	outcome, err := PlanProcessReset(request, Baseline{Generation: 1}, unknownSource, QuotaUsage{Available: false})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.AccountingUnknown || outcome.Reason != "quota_source_unavailable_before_reset" {
		t.Fatalf("missing source must stay unknown: %+v", outcome)
	}
	if outcome.Baseline.ConsumedBytes != 900 || outcome.Baseline.Uplink != 40 {
		t.Fatalf("missing source must not zero the baseline: %+v", outcome.Baseline)
	}

	// 段不匹配：观察值不属于当前基线所在段。
	mismatch := QuotaBaseline{Uplink: 40, ConsumedBytes: 900, SegmentID: "seg-1"}
	outcome, err = PlanProcessReset(request, Baseline{Generation: 1}, mismatch,
		QuotaUsage{Available: true, Uplink: 60, SegmentID: "seg-OTHER"})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.AccountingUnknown || outcome.Reason != "quota_segment_mismatch_before_reset" {
		t.Fatalf("segment mismatch must stay unknown: %+v", outcome)
	}
	if outcome.Baseline.ConsumedBytes != 900 {
		t.Fatalf("segment mismatch must not rebase accounting: %+v", outcome.Baseline)
	}

	// 计数器倒退（进程已被重置但没记录基线）：未知，不是 0。
	backwards := QuotaBaseline{Uplink: 500, ConsumedBytes: 100, SegmentID: "seg-1"}
	outcome, err = PlanProcessReset(request, Baseline{Generation: 1}, backwards,
		QuotaUsage{Available: true, Uplink: 10, SegmentID: "seg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.AccountingUnknown || outcome.Reason != "quota_counter_reset_before_reset" {
		t.Fatalf("backwards counters must stay unknown: %+v", outcome)
	}
	if outcome.Baseline.ConsumedBytes != 100 || outcome.Baseline.Uplink != 500 {
		t.Fatalf("backwards counters must not rebase accounting: %+v", outcome.Baseline)
	}
}

func TestReviewR2QuotaDirectionIsHonouredOnReset(t *testing.T) {
	// uplink 方向只结算上行：下行增量不得被吞进基线。
	known := QuotaBaseline{Uplink: 10, Downlink: 10, ConsumedBytes: 5, SegmentID: "seg-1"}
	request := resetRequest()
	request.Direction = QuotaUplink
	outcome, err := PlanProcessReset(request, Baseline{Generation: 1}, known,
		QuotaUsage{Available: true, Uplink: 30, Downlink: 90, SegmentID: "seg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.AccountingUnknown {
		t.Fatalf("uplink reset must stay known: %+v", outcome)
	}
	if outcome.Baseline.ConsumedBytes != 25 {
		t.Fatalf("uplink reset must carry 5+20: %+v", outcome.Baseline)
	}
	policy := quotaPolicy()
	policy.Direction = QuotaUplink
	after, err := EvaluateQuota(policy, outcome.Baseline, QuotaUsage{Available: true, Uplink: 4, Downlink: 1000, SegmentID: "seg-2"}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	if after.UsedBytes != 29 {
		t.Fatalf("uplink usage must be 25+4, got %+v", after)
	}
}

func TestReviewR2QuotaRolloverRefusals(t *testing.T) {
	policy := quotaPolicy()
	policy.ResetOnWindow = true
	policy.Period = 24 * time.Hour
	known := QuotaBaseline{Uplink: 100, Downlink: 100, ConsumedBytes: 500, WindowStart: at(1).Add(-48 * time.Hour), SegmentID: "seg-1"}

	if next, err := WindowRollover(policy, known, QuotaUsage{Available: false}, at(5)); err == nil {
		t.Fatalf("rollover without a source must be refused, got %+v", next)
	}
	if next, err := WindowRollover(policy, known, QuotaUsage{Available: true, Uplink: 1, SegmentID: "seg-2"}, at(5)); err == nil {
		t.Fatalf("rollover across segments must be refused, got %+v", next)
	}
	// 重放/未到期：窗口还差一天。
	fresh := QuotaBaseline{Uplink: 100, Downlink: 100, ConsumedBytes: 500, WindowStart: at(1), SegmentID: "seg-1"}
	if _, err := WindowRollover(policy, fresh, QuotaUsage{Available: true, Uplink: 120, SegmentID: "seg-1"}, at(5)); err == nil {
		t.Fatal("not yet elapsed rollover must be refused")
	}
	// ResetOnWindow 未开启时不得自行换窗。
	policy.ResetOnWindow = false
	if _, err := WindowRollover(policy, known, QuotaUsage{Available: true, Uplink: 120, SegmentID: "seg-1"}, at(5)); err == nil {
		t.Fatal("rollover without ResetOnWindow must be refused")
	}
}

func TestReviewR2QuotaSegmentMismatchAndWarningOverflow(t *testing.T) {
	policy := quotaPolicy()
	state, err := EvaluateQuota(policy, QuotaBaseline{Uplink: 10, SegmentID: "seg-1"},
		QuotaUsage{Available: true, Uplink: 20, SegmentID: "seg-2"}, at(2))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Unknown || state.Reason != "quota_segment_mismatch" {
		t.Fatalf("segment mismatch must be unknown: %+v", state)
	}

	// 80% 阈值不能因 used*100 溢出而误报。
	huge := policy
	huge.LimitBytes = math.MaxUint64
	state, err = EvaluateQuota(huge, QuotaBaseline{}, QuotaUsage{Available: true, Uplink: math.MaxUint64 / 2}, at(2))
	if err != nil {
		t.Fatal(err)
	}
	if state.Unknown || state.Warning {
		t.Fatalf("half of MaxUint64 must not warn: %+v", state)
	}
	nearLimit := QuotaUsage{Available: true, Uplink: math.MaxUint64 - 1}
	state, err = EvaluateQuota(huge, QuotaBaseline{}, nearLimit, at(2))
	if err != nil {
		t.Fatal(err)
	}
	if state.Exhausted || !state.Warning {
		t.Fatalf("near-limit usage must warn without overflow: %+v", state)
	}
}

// ---- W4 补充负向 ----

func TestReviewR2BatchApprovalRejectsMutatedPlan(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1", "n-2"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatal(err)
	}
	approval := BatchApproval{Digest: plan.Digest, Targets: plan.Targets, ApprovedBy: "admin-1", ApprovalID: "ap-1"}
	if err := ApproveBatch(plan, approval); err != nil {
		t.Fatalf("baseline approval must pass: %v", err)
	}

	mutations := map[string]func(*BatchPlan){
		"concurrency":    func(p *BatchPlan) { p.Policy.Concurrency = 16 },
		"batch size":     func(p *BatchPlan) { p.Policy.BatchSize = 1 },
		"failure budget": func(p *BatchPlan) { p.Policy.MaxFailures = 9 },
		"health stop":    func(p *BatchPlan) { p.Policy.StopOnHealthFailure = false },
		"extra target":   func(p *BatchPlan) { p.Targets = append(p.Targets, "n-3") },
		"dropped wave":   func(p *BatchPlan) { p.Waves = p.Waves[:len(p.Waves)-1] },
		"eligibility":    func(p *BatchPlan) { p.Eligible = false },
		"adoption flag":  func(p *BatchPlan) { p.RequiresAdoption = true },
	}
	for name, mutate := range mutations {
		drifted := plan
		drifted.Targets = append([]string(nil), plan.Targets...)
		drifted.Waves = append([]PlannedWave(nil), plan.Waves...)
		mutate(&drifted)
		if err := ApproveBatch(drifted, approval); err == nil {
			t.Fatalf("%s drift was accepted with the old digest", name)
		}
	}
}

func TestReviewR2BatchApprovalVerifiesWavesEvenWithRecomputedDigest(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1", "n-2"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatal(err)
	}
	// digest 是无密钥的 SHA-256：能改计划的人也能重算它，所以必须再验证结构。
	dropped := plan
	dropped.Targets = append([]string(nil), plan.Targets...)
	dropped.Waves = nil
	digest, err := digestPlan(dropped)
	if err != nil {
		t.Fatal(err)
	}
	dropped.Digest = digest
	approval := BatchApproval{Digest: digest, Targets: dropped.Targets, ApprovedBy: "admin-1", ApprovalID: "ap-1"}
	if err := ApproveBatch(dropped, approval); err == nil {
		t.Fatal("plan whose waves no longer match its targets must be rejected")
	}

	// 同样的原则适用于失败预算变化后重算 digest：策略仍然合法，但波次结构必须自洽。
	badEligibility := plan
	badEligibility.Targets = append([]string(nil), plan.Targets...)
	badEligibility.Issues = []TargetIssue{{NodeID: "n-1", Code: "node_offline", Message: "x"}}
	digest, err = digestPlan(badEligibility)
	if err != nil {
		t.Fatal(err)
	}
	badEligibility.Digest = digest
	if err := ApproveBatch(badEligibility, BatchApproval{Digest: digest, Targets: badEligibility.Targets, ApprovedBy: "admin-1", ApprovalID: "ap-1"}); err == nil {
		t.Fatal("eligibility that contradicts the issues must be rejected")
	}
}

func TestReviewR2BatchApprovalRejectsPastedIdentityStrings(t *testing.T) {
	plan, err := PlanBatch([]string{"n-1"}, batchNodes(), batchPolicy(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApproveBatch(plan, BatchApproval{Digest: plan.Digest, Targets: plan.Targets, ApprovedBy: "same", ApprovalID: "same"}); err == nil {
		t.Fatal("approver and approval id must not be the same token")
	}
	if err := ApproveBatch(plan, BatchApproval{Digest: plan.Digest, Targets: plan.Targets, ApprovedBy: "  ", ApprovalID: "ap-1"}); err == nil {
		t.Fatal("blank approver must be rejected")
	}
	// 目标集合顺序无关，但内容必须完全一致。
	if err := ApproveBatch(plan, BatchApproval{Digest: plan.Digest, Targets: []string{"n-1", "n-1"}, ApprovedBy: "admin-1", ApprovalID: "ap-1"}); err == nil {
		t.Fatal("duplicated approval targets must be rejected")
	}
}
