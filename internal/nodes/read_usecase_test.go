package nodes_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
)

func usecasePolicy(granted ...string) *auth.NodePolicy {
	grants := make(map[string]struct{}, len(granted))
	for _, id := range granted {
		grants[id] = struct{}{}
	}
	return &auth.NodePolicy{
		KnownNodes: map[string]struct{}{"a": {}, "b": {}, "c": {}},
		ReadGrants: map[string]map[string]struct{}{"reader": grants, "zero": nil},
	}
}

func usecasePolicyReader(granted ...string) auth.PolicyReader {
	p := usecasePolicy(granted...)
	return func(context.Context) (*auth.NodePolicy, error) { return p, nil }
}

func usecaseInventory() []nodes.Node {
	sample := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	return []nodes.Node{
		{ID: "a", Name: "visible alpha", Address: "192.0.2.1", Region: "visible-region", Provider: "visible-provider", Tags: []string{"visible-tag", "shared"}, Status: nodes.StatusOnline, LastSeenAt: sample, Metrics: &nodes.Metrics{CPUPercent: 0, MemoryPercent: 100, ObservedAt: sample}},
		{ID: "b", Name: "hidden-name-marker", Address: "192.0.2.2", Region: "hidden-region-marker", Provider: "hidden-provider", Tags: []string{"hidden-tag-marker"}, Status: nodes.StatusOffline},
		{ID: "c", Name: "visible gamma", Region: "visible-region", Provider: "visible-provider", Status: nodes.StatusUnknown},
	}
}

func usecaseSource(snapshot []nodes.Node) nodes.InventorySource {
	return func(context.Context) ([]nodes.Node, error) { return snapshot, nil }
}

func assertUsecaseFailure(t *testing.T, out []nodes.Node, err, want error) {
	t.Helper()
	if out != nil || !errors.Is(err, want) {
		t.Fatalf("got nodes=%v, err=%v; want nil result and %v", ids(out), err, want)
	}
}

func TestReadInventoryPreflightOrderCountsAndInputOrder(t *testing.T) {
	ctx := context.Background()
	var calls []string
	policy := func(got context.Context) (*auth.NodePolicy, error) {
		if got != ctx {
			t.Fatal("policy context changed")
		}
		calls = append(calls, "policy")
		return usecasePolicy("a", "c"), nil
	}
	snapshot := usecaseInventory()
	snapshot[0], snapshot[2] = snapshot[2], snapshot[0]
	inventory := func(got context.Context) ([]nodes.Node, error) {
		if got != ctx || !slices.Equal(calls, []string{"policy"}) {
			t.Fatal("inventory ran before successful preflight")
		}
		calls = append(calls, "inventory")
		return snapshot, nil
	}
	out, err := nodes.ReadVisibleInventory(ctx, "reader", policy, inventory)
	if err != nil || !slices.Equal(ids(out), []string{"c", "a"}) || !slices.Equal(calls, []string{"policy", "inventory"}) {
		t.Fatalf("out=%v, err=%v, calls=%v", ids(out), err, calls)
	}
}

func TestReadInventoryEmptyAndZeroResourcePreflight(t *testing.T) {
	for _, subject := range []string{"reader", "zero"} {
		for _, snapshot := range [][]nodes.Node{nil, {}, usecaseInventory()} {
			policyReads, inventoryReads := 0, 0
			out, err := nodes.ReadVisibleInventory(context.Background(), subject,
				func(context.Context) (*auth.NodePolicy, error) { policyReads++; return usecasePolicy(), nil },
				func(context.Context) ([]nodes.Node, error) { inventoryReads++; return snapshot, nil })
			if err != nil || out == nil || len(out) != 0 || cap(out) != 0 || policyReads != 1 || inventoryReads != 1 {
				t.Fatalf("empty authorized result or source counts wrong: out=%v, err=%v, reads=%d/%d", out, err, policyReads, inventoryReads)
			}
		}
	}
}

func TestReadInventoryPreflightFailuresNeverReadInventory(t *testing.T) {
	for _, tc := range []struct {
		name, subject string
		policy        *auth.NodePolicy
		sourceErr     error
		want          error
	}{
		{"blank", "", usecasePolicy("a"), nil, auth.ErrInvalidCheck},
		{"padded", " reader", usecasePolicy("a"), nil, auth.ErrInvalidCheck},
		{"wildcard", "reader*", usecasePolicy("a"), nil, auth.ErrInvalidCheck},
		{"unknown_empty_inventory", "unknown", usecasePolicy(), nil, auth.ErrDenied},
		{"unknown_empty_policy", "unknown", &auth.NodePolicy{}, nil, auth.ErrDenied},
		{"nil_policy", "reader", nil, nil, auth.ErrPolicyUnavailable},
		{"read_failure", "reader", usecasePolicy("a"), errors.New("synthetic-sensitive-policy"), auth.ErrPolicyUnavailable},
		{"joined_fault", "reader", usecasePolicy("a"), errors.Join(auth.ErrDenied, auth.ErrPolicyUnavailable), auth.ErrPolicyUnavailable},
		{"dangling_grant", "reader", &auth.NodePolicy{ReadGrants: map[string]map[string]struct{}{"reader": {"missing": {}}}}, nil, auth.ErrPolicyUnavailable},
		{"unrelated_bad_policy", "reader", &auth.NodePolicy{ReadGrants: map[string]map[string]struct{}{"reader": nil, "other": {"missing": {}}}}, nil, auth.ErrPolicyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inventoryReads := 0
			out, err := nodes.ReadVisibleInventory(context.Background(), tc.subject,
				func(context.Context) (*auth.NodePolicy, error) { return tc.policy, tc.sourceErr },
				func(context.Context) ([]nodes.Node, error) { inventoryReads++; return nil, nil })
			assertUsecaseFailure(t, out, err, tc.want)
			if inventoryReads != 0 {
				t.Fatal("failed preflight read inventory")
			}
		})
	}
	out, err := nodes.ReadVisibleInventory(context.Background(), "reader", nil, func(context.Context) ([]nodes.Node, error) {
		t.Fatal("nil policy reader reached inventory")
		return nil, nil
	})
	assertUsecaseFailure(t, out, err, auth.ErrPolicyUnavailable)
}

func TestReadInventoryNilSourceFailsAfterPreflight(t *testing.T) {
	reads := 0
	out, err := nodes.ReadVisibleInventory(context.Background(), "reader", func(context.Context) (*auth.NodePolicy, error) { reads++; return usecasePolicy("a"), nil }, nil)
	assertUsecaseFailure(t, out, err, nodes.ErrInventoryUnavailable)
	if reads != 1 {
		t.Fatal("nil inventory bypassed preflight")
	}
}

func TestReadInventoryOneSnapshotAndFreshCallRevocation(t *testing.T) {
	p := usecasePolicy("a")
	policyReads, inventoryReads := 0, 0
	var policyFailure error
	reader := func(context.Context) (*auth.NodePolicy, error) { policyReads++; return p, policyFailure }
	source := func(context.Context) ([]nodes.Node, error) {
		inventoryReads++
		// Node a remains known; revoke only its grant and replace the outer map.
		delete(p.ReadGrants["reader"], "a")
		p.ReadGrants["reader"]["b"] = struct{}{}
		p.ReadGrants = map[string]map[string]struct{}{"reader": {"b": {}}}
		return usecaseInventory(), nil
	}
	first, err := nodes.ReadVisibleInventory(context.Background(), "reader", reader, source)
	if err != nil || !slices.Equal(ids(first), []string{"a"}) {
		t.Fatalf("policy mutation changed same-call scope: %v, %v", ids(first), err)
	}
	second, err := nodes.ReadVisibleInventory(context.Background(), "reader", reader, source)
	if err != nil || !slices.Equal(ids(second), []string{"b"}) || policyReads != 2 || inventoryReads != 2 {
		t.Fatalf("new call missed revocation: %v, %v, reads=%d/%d", ids(second), err, policyReads, inventoryReads)
	}
	policyFailure = errors.New("synthetic-policy-failure-after-allow")
	third, err := nodes.ReadVisibleInventory(context.Background(), "reader", reader, source)
	assertUsecaseFailure(t, third, err, auth.ErrPolicyUnavailable)
	if policyReads != 3 || inventoryReads != 2 || !slices.Equal(ids(first), []string{"a"}) {
		t.Fatal("failed fresh call used an old scope/list or read inventory")
	}
	policyFailure = nil
	fourth, err := nodes.ReadVisibleInventory(context.Background(), "reader", reader, func(context.Context) ([]nodes.Node, error) {
		inventoryReads++
		return second, errors.New("synthetic-inventory-failure-after-allow")
	})
	assertUsecaseFailure(t, fourth, err, nodes.ErrInventoryUnavailable)
	if policyReads != 4 || inventoryReads != 3 {
		t.Fatal("inventory failure reused an old scope/list or skipped a source")
	}
}

func TestReadInventorySourceErrorsRejectPartialDataAndSanitize(t *testing.T) {
	secret := errors.New("synthetic-sensitive-inventory-source")
	for _, sourceErr := range []error{
		secret, auth.ErrDenied, errors.Join(auth.ErrDenied, secret), errors.Join(auth.ErrDenied, auth.ErrPolicyUnavailable),
		fmt.Errorf("synthetic-sensitive-wrapper: %w", context.Canceled),
		fmt.Errorf("synthetic-sensitive-wrapper: %w", context.DeadlineExceeded),
		errors.Join(auth.ErrDenied, secret, context.Canceled),
	} {
		for _, snapshot := range [][]nodes.Node{nil, usecaseInventory()[:1]} {
			out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a"), func(context.Context) ([]nodes.Node, error) { return snapshot, sourceErr })
			want := nodes.ErrInventoryUnavailable
			if errors.Is(sourceErr, context.Canceled) {
				want = context.Canceled
			} else if errors.Is(sourceErr, context.DeadlineExceeded) {
				want = context.DeadlineExceeded
			}
			assertUsecaseFailure(t, out, err, want)
			if err != want || errors.Unwrap(err) != nil || strings.Contains(err.Error(), "synthetic-sensitive") {
				t.Fatalf("source cause/text retained or misclassified: %v", err)
			}
		}
	}
}

func TestReadInventoryMalformedIDsNeverProducePartialCounts(t *testing.T) {
	for _, bad := range []string{"", " ", " a", "a ", "a\tb", "a\x00b", "*", "a*", "node?", "[ab]", string([]byte{0xff})} {
		t.Run(fmt.Sprintf("id_%q", bad), func(t *testing.T) {
			out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a"), usecaseSource([]nodes.Node{{ID: "a"}, {ID: bad}}))
			assertUsecaseFailure(t, out, err, auth.ErrInvalidCheck)
			if strings.Contains(err.Error(), bad) && len(bad) > 2 {
				t.Fatal("invalid source ID echoed")
			}
		})
	}
	for _, duplicate := range []string{"a", "b", "unregistered"} {
		out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a"), usecaseSource([]nodes.Node{{ID: "a"}, {ID: duplicate}, {ID: duplicate}}))
		assertUsecaseFailure(t, out, err, nodes.ErrInvalidInventory)
	}
}

func TestReadInventoryDeepCopyBothDirections(t *testing.T) {
	for _, direction := range []string{"source_to_output", "output_to_source"} {
		t.Run(direction, func(t *testing.T) {
			snapshot, expected := usecaseInventory(), usecaseInventory()
			out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a", "b", "c"), usecaseSource(snapshot))
			if err != nil || !reflect.DeepEqual(out, expected) || !reflect.DeepEqual(snapshot, expected) {
				t.Fatalf("values changed during read: %v", err)
			}
			if &out[0] == &snapshot[0] || &out[0].Tags[0] == &snapshot[0].Tags[0] || out[0].Metrics == snapshot[0].Metrics {
				t.Fatal("output aliases source slice/tags/metrics")
			}
			if direction == "source_to_output" {
				snapshot[0].ID, snapshot[0].Name, snapshot[0].Tags[0], snapshot[0].Metrics.CPUPercent = "changed", "changed", "changed", 55
				if !reflect.DeepEqual(out, expected) {
					t.Fatal("source mutation changed output")
				}
			} else {
				out[0].ID, out[0].Name, out[0].Tags[0], out[0].Metrics.MemoryPercent = "changed", "changed", "changed", 25
				if !reflect.DeepEqual(snapshot, expected) {
					t.Fatal("output mutation changed source")
				}
			}
		})
	}
	tags, metrics := []string{"shared"}, &nodes.Metrics{CPUPercent: 1}
	snapshot := []nodes.Node{{ID: "a", Tags: tags, Metrics: metrics}, {ID: "c", Tags: tags, Metrics: metrics}, {ID: "b", Tags: []string{}}}
	out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a", "b", "c"), usecaseSource(snapshot))
	if err != nil || out[2].Tags == nil || out[2].Metrics != nil {
		t.Fatalf("empty/nil values not preserved: %v", err)
	}
	out[0].Tags[0], out[0].Metrics.CPUPercent = "changed", 90
	if out[1].Tags[0] != "shared" || out[1].Metrics.CPUPercent != 1 || tags[0] != "shared" || metrics.CPUPercent != 1 {
		t.Fatal("nested source sharing survived cloning")
	}
}

func TestReadInventoryFilterAndCountsOnlyUseAuthorizedSuccess(t *testing.T) {
	out, err := nodes.ReadVisibleInventory(context.Background(), "reader", usecasePolicyReader("a", "c"), usecaseSource(usecaseInventory()))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"hidden-name-marker", "hidden-tag-marker", "192.0.2.2"} {
		filtered, err := nodes.Filter(out, nodes.NodeQuery{Text: text})
		if err != nil || len(filtered) != 0 {
			t.Fatalf("hidden data entered authorized filtering: %v, %v", ids(filtered), err)
		}
	}
	for _, dimension := range []nodes.Dimension{nodes.DimensionRegion, nodes.DimensionTag, nodes.DimensionProvider} {
		buckets, err := nodes.Classify(out, dimension)
		if err != nil {
			t.Fatal(err)
		}
		for _, bucket := range buckets {
			if strings.Contains(bucket.Value, "hidden") || bucket.Count > len(out) {
				t.Fatalf("hidden inventory entered classification: %+v", bucket)
			}
		}
		if dimension == nodes.DimensionRegion && !reflect.DeepEqual(buckets, []nodes.Classification{{Dimension: dimension, Value: "visible-region", Count: 2}}) {
			t.Fatalf("visible region count changed: %+v", buckets)
		}
	}
}

func TestReadInventoryCanceledExpiredAndNilContext(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		policyReads, inventoryReads := 0, 0
		out, err := nodes.ReadVisibleInventory(ctx, "reader",
			func(context.Context) (*auth.NodePolicy, error) { policyReads++; return usecasePolicy("a"), nil },
			func(context.Context) ([]nodes.Node, error) { inventoryReads++; return usecaseInventory(), nil })
		assertUsecaseFailure(t, out, err, ctx.Err())
		if policyReads != 0 || inventoryReads != 0 {
			t.Fatal("pre-canceled request read sources")
		}
	}
	//lint:ignore SA1012 Deliberately pass nil context to verify fail-closed delegation to preflight.
	out, err := nodes.ReadVisibleInventory(nil, "reader", nil, nil)
	assertUsecaseFailure(t, out, err, auth.ErrInvalidCheck)
}

// Switching to a real canceled/expired context at a controlled observation or
// source callback tests exact boundaries without sleeps or scheduler races.
type inventoryObservedContext struct {
	context.Context
	terminal context.Context
	checks   int
	endAt    int
}

func (c *inventoryObservedContext) Err() error {
	c.checks++
	if c.checks == c.endAt {
		c.Context = c.terminal
	}
	return c.Context.Err()
}

func TestReadInventoryCancellationAndDeadlineInsideLastSource(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	for _, terminal := range []context.Context{canceled, expired} {
		for _, phase := range []string{"policy", "inventory"} {
			for _, sourceErr := range []error{nil, errors.New("synthetic-sensitive-source-fault")} {
				ctx := &inventoryObservedContext{Context: context.Background()}
				inventoryReads := 0
				out, err := nodes.ReadVisibleInventory(ctx, "reader",
					func(context.Context) (*auth.NodePolicy, error) {
						if phase == "policy" {
							ctx.Context = terminal
							return usecasePolicy("a"), sourceErr
						}
						return usecasePolicy("a"), nil
					},
					func(context.Context) ([]nodes.Node, error) {
						inventoryReads++
						ctx.Context = terminal
						return usecaseInventory(), sourceErr
					})
				assertUsecaseFailure(t, out, err, terminal.Err())
				if (phase == "policy" && inventoryReads != 0) || (phase == "inventory" && inventoryReads != 1) {
					t.Fatal("cancellation boundary changed source ordering")
				}
			}
		}
	}
}

func TestReadInventoryCancellationAtEveryObservation(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	for name, snapshot := range map[string][]nodes.Node{"empty": nil, "filter_and_copy": usecaseInventory()} {
		probe := &inventoryObservedContext{Context: context.Background()}
		if _, err := nodes.ReadVisibleInventory(probe, "reader", usecasePolicyReader("a", "c"), usecaseSource(snapshot)); err != nil {
			t.Fatal(err)
		}
		// Derive all observation points from an actual successful call, including
		// policy validation, inventory boundary, traversal, tag copy and final return.
		for _, terminal := range []context.Context{canceled, expired} {
			for observation := 1; observation <= probe.checks; observation++ {
				t.Run(fmt.Sprintf("%s/%v/observation_%d", name, terminal.Err(), observation), func(t *testing.T) {
					ctx := &inventoryObservedContext{Context: context.Background(), terminal: terminal, endAt: observation}
					out, err := nodes.ReadVisibleInventory(ctx, "reader", usecasePolicyReader("a", "c"), usecaseSource(snapshot))
					assertUsecaseFailure(t, out, err, terminal.Err())
				})
			}
		}
	}
}
