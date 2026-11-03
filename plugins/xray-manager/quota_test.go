package xraymanager

import (
	"errors"
	"testing"
	"time"
)

func quotaPolicy() QuotaPolicy {
	return QuotaPolicy{
		UserID:      "u-1",
		InstanceID:  "i-1",
		LimitBytes:  1000,
		Direction:   QuotaTotal,
		WindowStart: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
}

func at(hour int) time.Time { return time.Date(2026, 11, 3, hour, 0, 0, 0, time.UTC) }

func TestEvaluateQuotaCountsTotalAndWarns(t *testing.T) {
	state, err := EvaluateQuota(quotaPolicy(), QuotaBaseline{}, QuotaUsage{Available: true, Uplink: 500, Downlink: 300}, at(1))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if state.Unknown || state.Exhausted {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.UsedBytes != 800 || state.RemainingBytes != 200 || !state.Warning {
		t.Fatalf("usage/warning wrong: %+v", state)
	}
}

func TestEvaluateQuotaNeverInventsExhaustionFromMissingSource(t *testing.T) {
	state, err := EvaluateQuota(quotaPolicy(), QuotaBaseline{}, QuotaUsage{Available: false}, at(1))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !state.Unknown || state.Exhausted || state.Reason != "quota_source_unavailable" {
		t.Fatalf("missing source must stay unknown: %+v", state)
	}
}

func TestEvaluateQuotaTreatsCounterResetAsUnknownNotZero(t *testing.T) {
	policy := quotaPolicy()
	baseline := QuotaBaseline{Uplink: 900, Downlink: 0, ConsumedBytes: 900}
	state, err := EvaluateQuota(policy, baseline, QuotaUsage{Available: true, Uplink: 10, Downlink: 10}, at(2))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !state.Unknown || state.Reason != "quota_counter_reset_without_baseline" {
		t.Fatalf("reset without baseline must be unknown: %+v", state)
	}
	if state.Exhausted {
		t.Fatal("reset must not be reported as exhausted")
	}
}

func TestEvaluateQuotaExhaustedAndUnlimited(t *testing.T) {
	policy := quotaPolicy()
	state, err := EvaluateQuota(policy, QuotaBaseline{}, QuotaUsage{Available: true, Uplink: 600, Downlink: 400}, at(3))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !state.Exhausted || state.RemainingBytes != 0 || state.Reason != "quota_exhausted" {
		t.Fatalf("expected exhausted: %+v", state)
	}

	unlimited := policy
	unlimited.LimitBytes = 0
	state, err = EvaluateQuota(unlimited, QuotaBaseline{}, QuotaUsage{Available: true, Uplink: 1 << 40}, at(3))
	if err != nil {
		t.Fatalf("evaluate unlimited: %v", err)
	}
	if state.Exhausted || state.Unknown || state.Reason != "quota_unlimited" {
		t.Fatalf("unlimited policy must not exhaust: %+v", state)
	}
}

func TestEvaluateQuotaRejectsBadPolicy(t *testing.T) {
	policy := quotaPolicy()
	policy.Direction = "both"
	if _, err := EvaluateQuota(policy, QuotaBaseline{}, QuotaUsage{Available: true}, at(1)); !errors.Is(err, ErrQuotaSpec) {
		t.Fatalf("want ErrQuotaSpec, got %v", err)
	}
	if _, err := EvaluateQuota(quotaPolicy(), QuotaBaseline{}, QuotaUsage{Available: true}, time.Time{}); !errors.Is(err, ErrQuotaSpec) {
		t.Fatalf("want ErrQuotaSpec for missing clock, got %v", err)
	}
}

func resetRequest() ProcessResetRequest {
	return ProcessResetRequest{
		InstanceID:    "i-1",
		SegmentID:     "seg-2",
		Generation:    2,
		RequestedAt:   at(4),
		Reason:        "operator reset",
		OperatorID:    "admin-1",
		TaskID:        "task-1",
		SourcePresent: true,
	}
}

func TestPlanProcessResetFreezesConsumedTotals(t *testing.T) {
	known := QuotaBaseline{
		Uplink: 100, Downlink: 50, ConsumedBytes: 120, SegmentID: "seg-1",
		WindowStart: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
	outcome, err := PlanProcessReset(resetRequest(), Baseline{Generation: 1, SegmentID: "seg-1"}, known,
		QuotaUsage{Available: true, Uplink: 40, Downlink: 20, SegmentID: "seg-1", At: at(4)})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if outcome.AccountingUnknown {
		t.Fatalf("accounting must stay known: %+v", outcome)
	}
	if outcome.Baseline.Uplink != 140 || outcome.Baseline.Downlink != 70 || outcome.Baseline.SegmentID != "seg-2" {
		t.Fatalf("baseline not frozen correctly: %+v", outcome.Baseline)
	}
	if outcome.Generation != 2 || outcome.SegmentID != "seg-2" {
		t.Fatalf("generation wrong: %+v", outcome)
	}
}

func TestPlanProcessResetRefusals(t *testing.T) {
	cases := map[string]func(*ProcessResetRequest){
		"missing operator": func(r *ProcessResetRequest) { r.OperatorID = "" },
		"missing task":     func(r *ProcessResetRequest) { r.TaskID = "" },
		"no source":        func(r *ProcessResetRequest) { r.SourcePresent = false },
		"stale generation": func(r *ProcessResetRequest) { r.Generation = 1 },
	}
	for name, mutate := range cases {
		request := resetRequest()
		mutate(&request)
		_, err := PlanProcessReset(request, Baseline{Generation: 1}, QuotaBaseline{}, QuotaUsage{Available: true})
		if !errors.Is(err, ErrProcessResetRefused) {
			t.Fatalf("%s: want ErrProcessResetRefused, got %v", name, err)
		}
	}
}

func TestPlanProcessResetUnknownSourceKeepsAccountingUnknown(t *testing.T) {
	known := QuotaBaseline{Uplink: 10, SegmentID: "seg-1"}
	outcome, err := PlanProcessReset(resetRequest(), Baseline{Generation: 1}, known, QuotaUsage{Available: false})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !outcome.AccountingUnknown || outcome.Reason != "quota_source_unavailable_before_reset" {
		t.Fatalf("accounting must be flagged unknown: %+v", outcome)
	}
	// The already-known baseline is preserved so quota is not reset to zero.
	if outcome.Baseline.Uplink != 10 {
		t.Fatalf("known baseline dropped: %+v", outcome.Baseline)
	}
}

func TestPlanProcessResetSegmentMismatchIsUnknown(t *testing.T) {
	known := QuotaBaseline{Uplink: 10, SegmentID: "seg-9"}
	outcome, err := PlanProcessReset(resetRequest(), Baseline{Generation: 1}, known,
		QuotaUsage{Available: true, Uplink: 5, SegmentID: "seg-1"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !outcome.AccountingUnknown || outcome.Reason != "quota_segment_mismatch_before_reset" {
		t.Fatalf("segment mismatch must be unknown: %+v", outcome)
	}
}

func TestWindowRolloverResetsConsumedButNotLifetime(t *testing.T) {
	policy := quotaPolicy()
	policy.Period = 24 * time.Hour
	policy.ResetOnWindow = true
	baseline := QuotaBaseline{
		Uplink: 10, Downlink: 5, ConsumedBytes: 800,
		WindowStart: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
	next, err := WindowRollover(policy, baseline, QuotaUsage{Available: true, Uplink: 2, Downlink: 3}, at(5))
	if err != nil {
		t.Fatalf("rollover: %v", err)
	}
	if next.ConsumedBytes != 0 {
		t.Fatalf("window consumption must restart: %+v", next)
	}
	if next.Uplink != 12 || next.Downlink != 8 {
		t.Fatalf("lifetime counters must accumulate: %+v", next)
	}

	// Replaying the same rollover time must not grant a second window.
	if _, err := WindowRollover(policy, next, QuotaUsage{Available: true}, at(5)); !errors.Is(err, ErrQuotaSpec) {
		t.Fatalf("want ErrQuotaSpec for non-elapsed window, got %v", err)
	}
}
