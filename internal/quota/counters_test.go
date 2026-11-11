package quota_test

import (
	"context"
	"serverfleet.local/serverfleet/internal/quota"
	"strings"
	"testing"
	"time"
)

func TestObservedTrafficBaselinesResetsAndAuthoritativeBilling(t *testing.T) {
	h := newFixture(t, nil, &fakeNotifier{})
	ctx := context.Background()
	h.upsertPolicy(t, "n1", 100000, 80, time.Time{})
	start, _, _ := quota.PolicyPeriod(h.clock.Now(), quota.Policy{})
	sample := quota.CounterSample{NodeID: "n1", BootID: "boot-a", Interfaces: strings.Repeat("a", 64), BytesIn: 1000, BytesOut: 2000, ObservedAt: h.clock.Now()}
	record := func() {
		t.Helper()
		sample.ObservedAt = h.clock.Now()
		if e := h.service.RecordCounter(ctx, sample); e != nil {
			t.Fatal(e)
		}
	}
	record()
	if _, ok, e := h.service.GetUsage(ctx, "admin", "n1", start); e != nil || ok {
		t.Fatal("first sample invented usage", e)
	}
	h.clock.Advance(time.Minute)
	sample.BytesIn += 100
	sample.BytesOut += 200
	record()
	record()
	usage, ok, e := h.service.GetUsage(ctx, "admin", "n1", start)
	if e != nil || !ok || usage.TotalBytes() != 300 {
		t.Fatal("delta or replay", usage, e)
	}
	h.clock.Advance(time.Minute)
	sample.BootID = "boot-b"
	sample.BytesIn = 2
	sample.BytesOut = 3
	record()
	h.clock.Advance(6 * time.Minute)
	sample.BytesIn += 10000
	record()
	h.clock.Advance(time.Minute)
	sample.Interfaces = strings.Repeat("b", 64)
	sample.BytesIn += 100
	record()
	usage, _, _ = h.service.GetUsage(ctx, "admin", "n1", start)
	if usage.TotalBytes() != 300 {
		t.Fatal("reset/gap/interface change added fabricated bytes", usage)
	}
	coverage, ok, e := h.service.CounterCoverage(ctx, "admin", "n1", start)
	if e != nil || !ok || coverage.Samples != 5 || coverage.Gaps != 3 {
		t.Fatal("coverage", coverage, e)
	}
	h.record(t, "n1", 10, 20, quota.SourceProvider)
	h.clock.Advance(time.Minute)
	sample.BytesIn += 100
	record()
	usage, _, _ = h.service.GetUsage(ctx, "admin", "n1", start)
	if usage.Source != quota.SourceProvider || usage.TotalBytes() != 30 {
		t.Fatal("host estimate overwrote provider", usage)
	}
	restarted := buildFixture(t, h.db, h.clock, authorizerFunc(allowAll), &fakeNotifier{})
	h.clock.Advance(time.Minute)
	sample.BytesOut += 50
	sample.ObservedAt = h.clock.Now()
	if e := restarted.service.RecordCounter(ctx, sample); e != nil {
		t.Fatal(e)
	}
	coverage, _, _ = restarted.service.CounterCoverage(ctx, "admin", "n1", start)
	if coverage.Samples != 7 {
		t.Fatal("restart lost baseline", coverage)
	}
}
func TestQuotaPolicyCycleAndReminderSettingsPersist(t *testing.T) {
	h := newFixture(t, nil, &fakeNotifier{})
	ctx := context.Background()
	p, e := h.service.UpsertPolicy(ctx, "admin", "custom-cycle", quota.PolicyInput{NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 70, ResetDay: 5, Timezone: "Asia/Shanghai", Direction: "egress", ThresholdsPct: []int{70, 85, 95}, RemindDays: []int{30, 7, 1}, ExpiryType: "instance"})
	if e != nil {
		t.Fatal(e)
	}
	p, ok, e := h.service.GetPolicy(ctx, "admin", "n1")
	if e != nil || !ok || p.Direction != "egress" || p.ResetDay != 5 || p.Timezone != "Asia/Shanghai" || len(p.ThresholdsPct) != 3 || p.AutomationRevision != 1 {
		t.Fatal("policy", p, e)
	}
	start, end, e := quota.PolicyPeriod(time.Date(2026, 10, 4, 15, 59, 0, 0, time.UTC), p)
	if e != nil || start.Format(time.RFC3339) != "2026-09-04T16:00:00Z" || end.Format(time.RFC3339) != "2026-10-04T16:00:00Z" {
		t.Fatal("local reset boundary", start, end, e)
	}
	next, _, _ := quota.PolicyPeriod(end, p)
	if !next.Equal(end) {
		t.Fatal("reset boundary not inclusive")
	}
	for _, bad := range []quota.PolicyInput{{NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 70, ResetDay: 29}, {NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 70, Timezone: "invalid/zone"}, {NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 70, ThresholdsPct: []int{95, 70}}} {
		if _, e := h.service.UpsertPolicy(ctx, "admin", "bad-cycle", bad); e == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}
