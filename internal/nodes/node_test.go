package nodes_test

import (
	"math"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/nodes"
)

func TestUsageRequiresOnlineNodeAndFreshSample(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	sample := &nodes.Metrics{CPUPercent: 10, MemoryPercent: 20, ObservedAt: now.Add(-1 * time.Minute)}

	for _, tc := range []struct {
		name   string
		node   nodes.Node
		maxAge time.Duration
		wantOK bool
	}{
		{
			name:   "online fresh sample",
			node:   nodes.Node{ID: "a", Status: nodes.StatusOnline, Metrics: sample},
			maxAge: 5 * time.Minute,
			wantOK: true,
		},
		{
			name:   "offline keeps missing metrics",
			node:   nodes.Node{ID: "b", Status: nodes.StatusOffline, Metrics: sample},
			maxAge: 5 * time.Minute,
			wantOK: false,
		},
		{
			name:   "online without sample",
			node:   nodes.Node{ID: "c", Status: nodes.StatusOnline},
			maxAge: 5 * time.Minute,
			wantOK: false,
		},
		{
			name: "online sample without observation time",
			node: nodes.Node{ID: "d", Status: nodes.StatusOnline, Metrics: &nodes.Metrics{
				CPUPercent: 10, MemoryPercent: 20,
			}},
			maxAge: 5 * time.Minute,
			wantOK: false,
		},
		{
			name: "future observation is not trusted",
			node: nodes.Node{ID: "e", Status: nodes.StatusOnline, Metrics: &nodes.Metrics{
				CPUPercent: 10, MemoryPercent: 20, ObservedAt: now.Add(time.Minute),
			}},
			maxAge: 5 * time.Minute,
			wantOK: false,
		},
		{
			name:   "stale sample",
			node:   nodes.Node{ID: "f", Status: nodes.StatusOnline, Metrics: &nodes.Metrics{ObservedAt: now.Add(-10 * time.Minute)}},
			maxAge: 5 * time.Minute,
			wantOK: false,
		},
		{
			name:   "non-positive max age",
			node:   nodes.Node{ID: "g", Status: nodes.StatusOnline, Metrics: sample},
			maxAge: 0,
			wantOK: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.node.Usage(now, tc.maxAge)
			if ok != tc.wantOK {
				t.Fatalf("Usage ok = %t, want %t", ok, tc.wantOK)
			}
			if ok && (got.CPUPercent != tc.node.Metrics.CPUPercent ||
				got.MemoryPercent != tc.node.Metrics.MemoryPercent ||
				!got.ObservedAt.Equal(tc.node.Metrics.ObservedAt)) {
				t.Fatalf("Usage = %+v, want the raw sample %+v", got, *tc.node.Metrics)
			}
			if !ok && (got.CPUPercent != 0 || got.MemoryPercent != 0) {
				t.Fatalf("Usage returned values for missing sample: %+v", got)
			}
		})
	}
}

func TestUsageDoesNotFakeZeroForOfflineFixtureNode(t *testing.T) {
	fixture := loadFixture(t)
	offline := findNode(t, fixture, "node-offline-b")
	if offline.Status != nodes.StatusOffline || offline.Metrics == nil {
		t.Fatalf("fixture precondition changed: %+v", offline)
	}
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	if usage, ok := offline.Usage(now, 24*time.Hour); ok {
		t.Fatalf("offline node reported usage %+v, want missing", usage)
	}
}

func TestUsageRejectsNonFiniteMetrics(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	observed := now.Add(-1 * time.Minute)
	for _, tc := range []struct {
		name string
		cpu  float64
		mem  float64
	}{
		{name: "cpu NaN", cpu: math.NaN(), mem: 20},
		{name: "cpu positive infinity", cpu: math.Inf(1), mem: 20},
		{name: "cpu negative infinity", cpu: math.Inf(-1), mem: 20},
		{name: "memory NaN", cpu: 10, mem: math.NaN()},
		{name: "memory positive infinity", cpu: 10, mem: math.Inf(1)},
		{name: "memory negative infinity", cpu: 10, mem: math.Inf(-1)},
		{name: "both non-finite", cpu: math.NaN(), mem: math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := nodes.Node{ID: "a", Status: nodes.StatusOnline, Metrics: &nodes.Metrics{
				CPUPercent: tc.cpu, MemoryPercent: tc.mem, ObservedAt: observed,
			}}
			if usage, ok := node.Usage(now, 5*time.Minute); ok {
				t.Fatalf("Usage accepted non-finite sample %+v", usage)
			}
		})
	}
}

func TestUsageAcceptsRealZeroPercentSample(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	sample := &nodes.Metrics{CPUPercent: 0, MemoryPercent: 0, ObservedAt: now.Add(-1 * time.Minute)}
	node := nodes.Node{ID: "a", Status: nodes.StatusOnline, Metrics: sample}
	got, ok := node.Usage(now, 5*time.Minute)
	if !ok {
		t.Fatal("Usage rejected a fresh zero-percent sample")
	}
	if got.CPUPercent != 0 || got.MemoryPercent != 0 || !got.ObservedAt.Equal(sample.ObservedAt) {
		t.Fatalf("Usage = %+v, want the real zero sample %+v", got, *sample)
	}
}

func TestStatusValid(t *testing.T) {
	for _, status := range []nodes.Status{
		nodes.StatusOnline, nodes.StatusOffline, nodes.StatusSuspended,
		nodes.StatusRevoked, nodes.StatusUnknown,
	} {
		if !status.Valid() {
			t.Errorf("%q should be valid", status)
		}
	}
	if nodes.Status("bogus").Valid() {
		t.Error("unknown status reported valid")
	}
}
