package auth_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

func preparedScope(t *testing.T, subject string, p *auth.NodePolicy) *auth.NodeReadScope {
	t.Helper()
	scope, err := auth.PrepareNodeReadScope(context.Background(), subject, func(context.Context) (*auth.NodePolicy, error) { return p, nil })
	if err != nil || scope == nil {
		t.Fatalf("prepare: got %v, %v", scope, err)
	}
	return scope
}

func TestReadScopePreflightWithoutInventory(t *testing.T) {
	calls := 0
	scope, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", func(context.Context) (*auth.NodePolicy, error) {
		calls++
		return policy(), nil
	})
	if err != nil || scope == nil || calls != 1 {
		t.Fatalf("prepare: scope=%v, err=%v, reads=%d", scope, err, calls)
	}
	for i := 0; i < 3; i++ {
		if err := scope.CheckNode(context.Background(), "synthetic-A"); err != nil {
			t.Fatal(err)
		}
		assertPolicyError(t, scope.CheckNode(context.Background(), "synthetic-B"), auth.ErrDenied, auth.Denied)
	}
	if calls != 1 {
		t.Fatal("scope retained/refreshed its policy reader")
	}
	// Preparation does not weaken the existing target-based Check contract.
	assertPolicyError(t, auth.EvaluateNodePolicy(context.Background(), auth.Check{SubjectID: "reader-A", Action: auth.NodeReadAction}, policy()), auth.ErrInvalidCheck, auth.InvalidCheck)
}

func TestReadScopeKnownSubjectWithZeroGrants(t *testing.T) {
	for name, p := range map[string]*auth.NodePolicy{
		"empty_inventory_and_nil_grants":   {ReadGrants: map[string]map[string]struct{}{"known": nil}},
		"empty_inventory_and_empty_grants": {ReadGrants: map[string]map[string]struct{}{"known": {}}},
		"known_inventory_without_grants":   {KnownNodes: set("synthetic-A"), ReadGrants: map[string]map[string]struct{}{"known": {}}},
	} {
		t.Run(name, func(t *testing.T) {
			scope := preparedScope(t, "known", p)
			for _, node := range []string{"synthetic-A", "unknown", "all", "admin", "nodes.write", "ai.root.shell", "plugins.execute"} {
				assertPolicyError(t, scope.CheckNode(context.Background(), node), auth.ErrDenied, auth.Denied)
			}
		})
	}
}

func TestReadScopeBadSubjectsDoNotReadPolicy(t *testing.T) {
	for _, subject := range []string{"", " ", " reader-A", "reader-A ", "reader\tA", "reader\x00A", "*", "reader-*", "reader?", "[reader]", string([]byte{0xff})} {
		t.Run(fmt.Sprintf("%q", subject), func(t *testing.T) {
			calls := 0
			scope, err := auth.PrepareNodeReadScope(context.Background(), subject, func(context.Context) (*auth.NodePolicy, error) { calls++; return policy(), nil })
			if scope != nil || calls != 0 {
				t.Fatalf("invalid subject produced scope or read policy: reads=%d", calls)
			}
			assertPolicyError(t, err, auth.ErrInvalidCheck, auth.InvalidCheck)
		})
	}
}

func TestReadScopeUnknownSubjectsFailEvenWithEmptyInventory(t *testing.T) {
	for name, p := range map[string]*auth.NodePolicy{
		"empty_policy": {}, "empty_grants": {ReadGrants: map[string]map[string]struct{}{}}, "another_subject": {ReadGrants: map[string]map[string]struct{}{"known": nil}},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			scope, err := auth.PrepareNodeReadScope(context.Background(), "unknown-admin", func(context.Context) (*auth.NodePolicy, error) { calls++; return p, nil })
			if scope != nil || calls != 1 {
				t.Fatalf("got scope=%v, reads=%d", scope, calls)
			}
			assertPolicyError(t, err, auth.ErrDenied, auth.Denied)
		})
	}
}

func TestReadScopeMissingFailedOrMalformedPolicy(t *testing.T) {
	for name, reader := range map[string]auth.PolicyReader{
		"nil_reader": nil,
		"nil_policy": func(context.Context) (*auth.NodePolicy, error) { return nil, nil },
		"source_error": func(context.Context) (*auth.NodePolicy, error) {
			return nil, errors.New("synthetic-sensitive-reader-error")
		},
		"valid_policy_and_error": func(context.Context) (*auth.NodePolicy, error) { return policy(), auth.ErrDenied },
	} {
		t.Run(name, func(t *testing.T) {
			scope, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", reader)
			if scope != nil {
				t.Fatal("failed source returned usable scope")
			}
			assertPolicyError(t, err, auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
		})
	}
	for name, mutate := range map[string]func(*auth.NodePolicy){
		"empty_known_id":           func(p *auth.NodePolicy) { p.KnownNodes[""] = struct{}{} },
		"wildcard_known_id":        func(p *auth.NodePolicy) { p.KnownNodes["*"] = struct{}{} },
		"bad_subject":              func(p *auth.NodePolicy) { p.ReadGrants["bad subject"] = nil },
		"wildcard_subject":         func(p *auth.NodePolicy) { p.ReadGrants["*"] = nil },
		"bad_grant":                func(p *auth.NodePolicy) { p.ReadGrants["reader-A"] = set("node?") },
		"dangling_grant":           func(p *auth.NodePolicy) { delete(p.KnownNodes, "synthetic-A") },
		"unrelated_bad_grant":      func(p *auth.NodePolicy) { p.ReadGrants["other"] = set("unknown-node") },
		"empty_subject_bad_policy": func(p *auth.NodePolicy) { p.ReadGrants["empty-subject"] = set("unknown-node") },
	} {
		t.Run(name, func(t *testing.T) {
			p := policy()
			mutate(p)
			calls := 0
			scope, err := auth.PrepareNodeReadScope(context.Background(), "empty-subject", func(context.Context) (*auth.NodePolicy, error) { calls++; return p, nil })
			if scope != nil || calls != 1 {
				t.Fatal("malformed policy produced scope or wrong read count")
			}
			assertPolicyError(t, err, auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
		})
	}
}

func TestReadScopeCopiesSourceAndNewPreparationObservesRevocation(t *testing.T) {
	current := policy()
	before := policy()
	calls := 0
	var sourceErr error
	reader := func(context.Context) (*auth.NodePolicy, error) { calls++; return current, sourceErr }
	first, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", reader)
	if err != nil || !reflect.DeepEqual(current, before) {
		t.Fatalf("prepare failed or mutated source: %v", err)
	}
	// Mutate every source set/outer map after preparation. No live source alias may survive.
	delete(current.KnownNodes, "synthetic-A")
	current.KnownNodes["added-node"] = struct{}{}
	delete(current.ReadGrants["reader-A"], "synthetic-A")
	current.ReadGrants["reader-A"]["synthetic-B"] = struct{}{}
	current.ReadGrants["reader-A"]["added-node"] = struct{}{}
	current.ReadGrants = map[string]map[string]struct{}{"reader-A": set("synthetic-B", "added-node")}
	if err := first.CheckNode(context.Background(), "synthetic-A"); err != nil {
		t.Fatalf("source mutation changed old scope: %v", err)
	}
	for _, node := range []string{"synthetic-B", "added-node"} {
		assertPolicyError(t, first.CheckNode(context.Background(), node), auth.ErrDenied, auth.Denied)
	}
	second, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", reader)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicyError(t, second.CheckNode(context.Background(), "synthetic-A"), auth.ErrDenied, auth.Denied)
	if err := second.CheckNode(context.Background(), "synthetic-B"); err != nil {
		t.Fatal(err)
	}
	// A failed new request cannot fall back to the old scope.
	sourceErr = errors.New("synthetic-source-failure-after-allow")
	third, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", reader)
	if third != nil || calls != 3 {
		t.Fatal("failed preparation reused scope or policy read count changed")
	}
	assertPolicyError(t, err, auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	if err := first.CheckNode(context.Background(), "synthetic-A"); err != nil {
		t.Fatalf("captured scope is a snapshot, not a live revocation checker: %v", err)
	}
}

func TestReadScopeValueCopiesAndPrivateCollection(t *testing.T) {
	scope := preparedScope(t, "reader-A", policy())
	copyOfScope := *scope
	*scope = auth.NodeReadScope{}
	assertPolicyError(t, scope.CheckNode(context.Background(), "synthetic-A"), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	if err := copyOfScope.CheckNode(context.Background(), "synthetic-A"); err != nil {
		t.Fatalf("resetting one value changed another scope: %v", err)
	}
	typ := reflect.TypeOf(auth.NodeReadScope{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Fatal("scope exposes mutable state")
		}
	}
	// CheckNode is the sole public method; no map/slice accessor or mutator is exposed.
	ptr := reflect.TypeOf((*auth.NodeReadScope)(nil))
	if ptr.NumMethod() != 1 || ptr.Method(0).Name != "CheckNode" {
		t.Fatal("unexpected public collection accessor or capability")
	}
}

func TestReadScopeZeroNilBadAndCrossSubjectTargetsFailClosed(t *testing.T) {
	for _, scope := range []*auth.NodeReadScope{nil, {}} {
		assertPolicyError(t, scope.CheckNode(context.Background(), "synthetic-A"), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	}
	scope := preparedScope(t, "reader-A", policy())
	for _, node := range []string{"", " ", " synthetic-A", "synthetic-A ", "a\x00b", "*", "synthetic-*", "node?", "[ab]", string([]byte{0xff})} {
		assertPolicyError(t, scope.CheckNode(context.Background(), node), auth.ErrInvalidCheck, auth.InvalidCheck)
	}
	for _, node := range []string{"synthetic-B", "unregistered-node", "all", "nodes.write", "ai.root.shell", "plugins.execute"} {
		assertPolicyError(t, scope.CheckNode(context.Background(), node), auth.ErrDenied, auth.Denied)
	}
	other := preparedScope(t, "reader-B", policy())
	assertPolicyError(t, other.CheckNode(context.Background(), "synthetic-A"), auth.ErrDenied, auth.Denied)
}

func TestReadScopeCancellationBeforePreparationAndCheck(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	prepared := preparedScope(t, "reader-A", policy())
	for _, ctx := range []context.Context{canceled, expired} {
		calls := 0
		scope, err := auth.PrepareNodeReadScope(ctx, "reader-A", func(context.Context) (*auth.NodePolicy, error) { calls++; return policy(), nil })
		if scope != nil || err != ctx.Err() || calls != 0 {
			t.Fatalf("canceled prepare returned scope, read policy or lost cause: %v", err)
		}
		for _, scope := range []*auth.NodeReadScope{nil, {}, prepared} {
			for _, node := range []string{"synthetic-A", "synthetic-B", "*"} {
				if err := scope.CheckNode(ctx, node); err != ctx.Err() {
					t.Fatalf("check lost context priority: %v", err)
				}
			}
		}
	}
	//lint:ignore SA1012 Deliberately pass nil context to verify fail-closed input validation.
	scope, err := auth.PrepareNodeReadScope(nil, "reader-A", nil)
	if scope != nil {
		t.Fatal("nil context produced a scope")
	}
	assertPolicyError(t, err, auth.ErrInvalidCheck, auth.InvalidCheck)
	//lint:ignore SA1012 Deliberately pass nil context to verify fail-closed input validation.
	assertPolicyError(t, prepared.CheckNode(nil, "synthetic-A"), auth.ErrInvalidCheck, auth.InvalidCheck)
}

func TestReadScopeFinalPolicyReadCancellationAndWrappedSourceErrors(t *testing.T) {
	for _, snapshot := range []*auth.NodePolicy{policy(), nil, {}} {
		for _, sourceErr := range []error{nil, errors.New("synthetic-sensitive-source-error"), auth.ErrDenied} {
			ctx, cancel := context.WithCancel(context.Background())
			scope, err := auth.PrepareNodeReadScope(ctx, "reader-A", func(context.Context) (*auth.NodePolicy, error) { cancel(); return snapshot, sourceErr })
			cancel()
			if scope != nil || err != context.Canceled {
				t.Fatalf("final read cancellation lost: %v", err)
			}
		}
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	reads := 0
	scope, err := auth.PrepareNodeReadScope(ctx, "reader-A", func(ctx context.Context) (*auth.NodePolicy, error) {
		reads++
		<-ctx.Done()
		return policy(), nil
	})
	if scope != nil || err != context.DeadlineExceeded || reads != 1 {
		t.Fatalf("final read deadline lost: scope=%v, err=%v, reads=%d", scope, err, reads)
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		scope, err := auth.PrepareNodeReadScope(context.Background(), "reader-A", func(context.Context) (*auth.NodePolicy, error) {
			return policy(), fmt.Errorf("synthetic-sensitive-source: %w", cause)
		})
		if scope != nil || err != cause || !errors.Is(err, cause) {
			t.Fatalf("wrapped context cause lost or retained source text: %v", err)
		}
	}
}

// scopeCancelOnObservation triggers a real cancellation at a chosen Err check.
// Calibrating against a completed call covers every observed phase (including
// copying and the final return check) without goroutine timing or hard-coded
// implementation counts. The embedded context supplies standard Err/Done semantics.
type scopeCancelOnObservation struct {
	context.Context
	cancel   context.CancelFunc
	calls    int
	cancelAt int
}

func (c *scopeCancelOnObservation) Err() error {
	c.calls++
	if c.calls == c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func TestReadScopeCancellationAtEveryPreparationObservation(t *testing.T) {
	reader := func(context.Context) (*auth.NodePolicy, error) { return policy(), nil }
	probe := &scopeCancelOnObservation{Context: context.Background()}
	if scope, err := auth.PrepareNodeReadScope(probe, "reader-AB", reader); err != nil || scope == nil {
		t.Fatalf("calibration failed: %v", err)
	}
	for observation := 1; observation <= probe.calls; observation++ {
		t.Run(fmt.Sprintf("observation_%d", observation), func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &scopeCancelOnObservation{Context: base, cancel: cancel, cancelAt: observation}
			scope, err := auth.PrepareNodeReadScope(ctx, "reader-AB", reader)
			if scope != nil || err != context.Canceled {
				t.Fatalf("observed cancellation returned usable scope or wrong error: %v", err)
			}
		})
	}
}

func TestReadScopeCancellationAtEveryCheckObservation(t *testing.T) {
	prepared := preparedScope(t, "reader-A", policy())
	for _, scope := range []*auth.NodeReadScope{nil, {}, prepared} {
		for _, node := range []string{"synthetic-A", "synthetic-B", "*"} {
			probe := &scopeCancelOnObservation{Context: context.Background()}
			_ = scope.CheckNode(probe, node)
			for observation := 1; observation <= probe.calls; observation++ {
				base, cancel := context.WithCancel(context.Background())
				ctx := &scopeCancelOnObservation{Context: base, cancel: cancel, cancelAt: observation}
				err := scope.CheckNode(ctx, node)
				cancel()
				if err != context.Canceled {
					t.Fatalf("check returned allow/denial instead of cancellation: %v", err)
				}
			}
		}
	}
}

func TestReadScopeSensitiveErrorsAreNotRetained(t *testing.T) {
	marker := "synthetic-sensitive-reader-subject-node-policy"
	for name, source := range map[string]auth.PolicyReader{
		"source": func(context.Context) (*auth.NodePolicy, error) { return policy(), errors.New(marker) },
		"bad_policy": func(context.Context) (*auth.NodePolicy, error) {
			return &auth.NodePolicy{KnownNodes: set(marker + "*")}, nil
		},
		"unknown_subject": func(context.Context) (*auth.NodePolicy, error) { return policy(), nil },
	} {
		t.Run(name, func(t *testing.T) {
			scope, err := auth.PrepareNodeReadScope(context.Background(), marker, source)
			if scope != nil || err == nil || strings.Contains(fmt.Sprintf("%+v", err), marker) {
				t.Fatalf("sensitive preparation result: %v", err)
			}
			for e := err; e != nil; e = errors.Unwrap(e) {
				if strings.Contains(e.Error(), marker) {
					t.Fatal("sensitive source retained in error chain")
				}
			}
		})
	}
	scope := preparedScope(t, "reader-A", policy())
	for _, node := range []string{marker, marker + "*"} {
		if err := scope.CheckNode(context.Background(), node); err == nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("sensitive node echoed: %v", err)
		}
	}
}

func TestReadScopeConcurrentChecksAfterSourceMutation(t *testing.T) {
	p := policy()
	scope := preparedScope(t, "reader-A", p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := scope.CheckNode(context.Background(), "synthetic-A"); err != nil {
					t.Error(err)
				}
				if !errors.Is(scope.CheckNode(context.Background(), "synthetic-B"), auth.ErrDenied) {
					t.Error("scope expanded during concurrent checks")
				}
			}
		}()
	}
	// These original maps are no longer used by the prepared scope.
	for i := 0; i < 10; i++ {
		delete(p.ReadGrants["reader-A"], "synthetic-A")
		p.ReadGrants["reader-A"]["synthetic-B"] = struct{}{}
		delete(p.KnownNodes, "synthetic-A")
	}
	wg.Wait()
}
