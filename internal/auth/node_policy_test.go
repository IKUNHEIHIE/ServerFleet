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

func set(ids ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func policy() *auth.NodePolicy {
	return &auth.NodePolicy{
		KnownNodes: set("synthetic-A", "synthetic-B", "synthetic-C"),
		ReadGrants: map[string]map[string]struct{}{
			"reader-A": set("synthetic-A"), "reader-B": set("synthetic-B"),
			"reader-AB": set("synthetic-A", "synthetic-B"), "empty-subject": nil,
		},
	}
}

func check() auth.Check {
	return auth.Check{SubjectID: "reader-A", Action: auth.NodeReadAction, NodeIDs: []string{"synthetic-A"}}
}

func assertPolicyError(t *testing.T, err, category error, kind auth.PolicyErrorKind) {
	t.Helper()
	var detail *auth.PolicyError
	if !errors.Is(err, category) || !errors.As(err, &detail) || detail.Kind != kind || detail.Field == "" {
		t.Fatalf("got %v, want category %v / kind %s / fixed field", err, category, kind)
	}
	for _, other := range []error{auth.ErrDenied, auth.ErrInvalidCheck, auth.ErrPolicyUnavailable, context.Canceled, context.DeadlineExceeded} {
		if other != category && errors.Is(err, other) {
			t.Fatalf("error %v also misclassified as %v", err, other)
		}
	}
}

func evaluateBoth(t *testing.T, c auth.Check, p *auth.NodePolicy, category error, kind auth.PolicyErrorKind) {
	t.Helper()
	for name, evaluate := range map[string]func() error{
		"pure": func() error { return auth.EvaluateNodePolicy(context.Background(), c, p) },
		"port": func() error {
			a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return p, nil })
			return a.Authorize(context.Background(), c)
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := evaluate()
			if category == nil {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			assertPolicyError(t, err, category, kind)
		})
	}
}

func TestExplicitNodeReadGrants(t *testing.T) {
	p := policy()
	evaluateBoth(t, check(), p, nil, "")
	c := check()
	c.SubjectID = "reader-AB"
	c.NodeIDs = []string{"synthetic-B", "synthetic-A"}
	evaluateBoth(t, c, p, nil, "")
}

func TestDefaultDenialsAndWholeRequestScope(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*auth.Check)
	}{
		{"unknown_subject", func(c *auth.Check) { c.SubjectID = "unregistered-admin" }},
		{"subject_without_grants", func(c *auth.Check) { c.SubjectID = "empty-subject" }},
		{"unknown_action", func(c *auth.Check) { c.Action = "nodes.unregistered" }},
		{"node_write_not_supported", func(c *auth.Check) { c.Action = "nodes.write" }},
		{"ai_root_not_supported", func(c *auth.Check) { c.Action = "ai.root.shell" }},
		{"plugin_not_supported", func(c *auth.Check) { c.Action = "plugins.execute" }},
		{"unknown_node", func(c *auth.Check) { c.NodeIDs = []string{"unregistered-node"} }},
		{"ungranted_known_node", func(c *auth.Check) { c.NodeIDs = []string{"synthetic-C"} }},
		{"cross_subject_node", func(c *auth.Check) { c.NodeIDs = []string{"synthetic-B"} }},
		{"allowed_then_denied", func(c *auth.Check) { c.NodeIDs = []string{"synthetic-A", "synthetic-B"} }},
		{"denied_then_allowed", func(c *auth.Check) { c.NodeIDs = []string{"synthetic-B", "synthetic-A"} }},
		{"allowed_then_unknown", func(c *auth.Check) { c.NodeIDs = []string{"synthetic-A", "unknown"} }},
		{"no_literal_all_alias", func(c *auth.Check) { c.NodeIDs = []string{"all"} }},
		{"no_group_expansion", func(c *auth.Check) { c.NodeIDs = []string{"service-group:synthetic-A"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := check()
			tc.mutate(&c)
			evaluateBoth(t, c, policy(), auth.ErrDenied, auth.Denied)
		})
	}
	for _, p := range []*auth.NodePolicy{{}, {KnownNodes: set("synthetic-A")}, {ReadGrants: map[string]map[string]struct{}{"reader-A": nil}}} {
		evaluateBoth(t, check(), p, auth.ErrDenied, auth.Denied)
	}
	evaluateBoth(t, check(), nil, auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
}

func TestInvalidChecksAndNoWildcardExpansion(t *testing.T) {
	for _, value := range []string{"", " ", "*", "reader-*", "node?", "[ab]", "n\x00", "x\ny", "node A", string([]byte{0xff})} {
		for _, field := range []string{"subject", "action", "target"} {
			t.Run(fmt.Sprintf("%s/%q", field, value), func(t *testing.T) {
				c := check()
				switch field {
				case "subject":
					c.SubjectID = value
				case "action":
					c.Action = value
				case "target":
					c.NodeIDs = []string{value}
				}
				evaluateBoth(t, c, policy(), auth.ErrInvalidCheck, auth.InvalidCheck)
			})
		}
	}
	for _, targets := range [][]string{nil, {}, {"synthetic-A", ""}, {"synthetic-A", "synthetic-A"}, {"synthetic-A", "*"}} {
		c := check()
		c.NodeIDs = targets
		evaluateBoth(t, c, policy(), auth.ErrInvalidCheck, auth.InvalidCheck)
	}
	// No normalization turns a malformed identifier into a granted one.
	for _, value := range []string{" reader-A", "reader-A ", "reader-A\t"} {
		c := check()
		c.SubjectID = value
		evaluateBoth(t, c, policy(), auth.ErrInvalidCheck, auth.InvalidCheck)
	}
}

func TestInvalidTrustedPolicyNeverGrants(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*auth.NodePolicy)
	}{
		{"empty_node", func(p *auth.NodePolicy) { p.KnownNodes[""] = struct{}{} }},
		{"wildcard_node", func(p *auth.NodePolicy) { p.KnownNodes["*"] = struct{}{} }},
		{"invalid_subject", func(p *auth.NodePolicy) { p.ReadGrants[" "] = set("synthetic-A") }},
		{"wildcard_subject", func(p *auth.NodePolicy) { p.ReadGrants["*"] = set("synthetic-A") }},
		{"wildcard_grant", func(p *auth.NodePolicy) { p.ReadGrants["reader-A"] = set("*") }},
		{"missing_known_node", func(p *auth.NodePolicy) { delete(p.KnownNodes, "synthetic-A") }},
		{"unrelated_bad_grant", func(p *auth.NodePolicy) { p.ReadGrants["other"] = set("unknown") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := policy()
			tc.mutate(p)
			evaluateBoth(t, check(), p, auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
		})
	}
}

func TestMissingOrFailedPolicyReader(t *testing.T) {
	var nilReceiver *auth.NodePolicyAuthorizer
	for _, a := range []*auth.NodePolicyAuthorizer{nilReceiver, {}, auth.NewNodePolicyAuthorizer(nil)} {
		assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	}
	for _, sourceErr := range []error{errors.New("synthetic secret database path"), auth.ErrDenied} {
		a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return policy(), sourceErr })
		assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	}
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return nil, nil })
	assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
}

func TestRevocationAndReaderFailureAfterPriorAllow(t *testing.T) {
	current := policy()
	calls := 0
	var sourceErr error
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { calls++; return current, sourceErr })
	if err := a.Authorize(context.Background(), check()); err != nil {
		t.Fatal(err)
	}
	current = policy()
	delete(current.ReadGrants["reader-A"], "synthetic-A")
	assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrDenied, auth.Denied)
	current = policy()
	if err := a.Authorize(context.Background(), check()); err != nil {
		t.Fatal(err)
	}
	current = policy()
	delete(current.ReadGrants, "reader-A")
	assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrDenied, auth.Denied)
	current = nil
	assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	current = policy()
	sourceErr = errors.New("synthetic source failure")
	assertPolicyError(t, a.Authorize(context.Background(), check()), auth.ErrPolicyUnavailable, auth.PolicyUnavailable)
	if calls != 6 {
		t.Fatalf("cached or extra reads: got %d, want 6", calls)
	}
	// Pure calls likewise use the explicit snapshot, with no hidden grant cache.
	current = policy()
	if err := auth.EvaluateNodePolicy(context.Background(), check(), current); err != nil {
		t.Fatal(err)
	}
	current = policy()
	delete(current.ReadGrants, "reader-A")
	assertPolicyError(t, auth.EvaluateNodePolicy(context.Background(), check(), current), auth.ErrDenied, auth.Denied)
}

func TestCancellationAndDeadlineBeforePolicyRead(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		calls := 0
		a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { calls++; return policy(), nil })
		for _, err := range []error{a.Authorize(ctx, check()), auth.EvaluateNodePolicy(ctx, check(), policy())} {
			if err != ctx.Err() {
				t.Fatalf("got %v, want exact ctx error %v", err, ctx.Err())
			}
		}
		if calls != 0 {
			t.Fatal("canceled request read policy")
		}
	}
	//lint:ignore SA1012 Deliberately pass malformed nil context to test fail-closed rejection.
	adapterErr := auth.NewNodePolicyAuthorizer(nil).Authorize(nil, check())
	//lint:ignore SA1012 Deliberately pass malformed nil context to test fail-closed rejection.
	pureErr := auth.EvaluateNodePolicy(nil, check(), policy())
	for _, err := range []error{adapterErr, pureErr} {
		assertPolicyError(t, err, auth.ErrInvalidCheck, auth.InvalidCheck)
	}
}

func TestCancellationDuringFinalPolicyReadHasPriority(t *testing.T) {
	for _, sourceErr := range []error{nil, errors.New("synthetic failure"), auth.ErrDenied} {
		for _, snapshot := range []*auth.NodePolicy{policy(), nil, {}} {
			ctx, cancel := context.WithCancel(context.Background())
			a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { cancel(); return snapshot, sourceErr })
			if err := a.Authorize(ctx, check()); err != context.Canceled {
				t.Fatalf("lost cancellation: %v", err)
			}
			cancel()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	a := auth.NewNodePolicyAuthorizer(func(ctx context.Context) (*auth.NodePolicy, error) { <-ctx.Done(); return policy(), nil })
	if err := a.Authorize(ctx, check()); err != context.DeadlineExceeded {
		t.Fatalf("lost final read deadline: %v", err)
	}
}

func TestWrappedSourceContextErrorsPreserveClassification(t *testing.T) {
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
			return policy(), fmt.Errorf("synthetic-secret: %w", ctxErr)
		})
		err := a.Authorize(context.Background(), check())
		if err != ctxErr {
			t.Fatalf("got %v, want normalized %v", err, ctxErr)
		}
	}
}

func TestUntrustedCheckCannotExpandPolicyOrMutateDuringRead(t *testing.T) {
	requested := check()
	requested.NodeIDs = []string{"synthetic-B"}
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		requested.NodeIDs[0] = "synthetic-A"
		return policy(), nil
	})
	// The adapter evaluates its detached original B request, not the later A edit.
	assertPolicyError(t, a.Authorize(context.Background(), requested), auth.ErrDenied, auth.Denied)
	requested = check()
	requested.NodeIDs = []string{"synthetic-A", "synthetic-B"}
	evaluateBoth(t, requested, policy(), auth.ErrDenied, auth.Denied)
}

func TestInputPolicyAndCheckRemainUnchanged(t *testing.T) {
	p := policy()
	before := policy()
	c := check()
	beforeTargets := append([]string(nil), c.NodeIDs...)
	evaluateBoth(t, c, p, nil, "")
	if !reflect.DeepEqual(p, before) || !reflect.DeepEqual(c.NodeIDs, beforeTargets) {
		t.Fatal("input mutated")
	}
}

func TestErrorClassificationAndNoSensitiveValues(t *testing.T) {
	marker := "synthetic-sensitive-marker"
	c := check()
	c.SubjectID = marker
	err := auth.EvaluateNodePolicy(context.Background(), c, policy())
	assertPolicyError(t, fmt.Errorf("safe wrapper: %w", err), auth.ErrDenied, auth.Denied)
	for _, candidate := range []error{
		err,
		auth.EvaluateNodePolicy(context.Background(), auth.Check{SubjectID: marker + "*"}, policy()),
		auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return nil, errors.New(marker) }).Authorize(context.Background(), check()),
	} {
		if candidate == nil || strings.Contains(candidate.Error(), marker) || strings.Contains(candidate.Error(), "synthetic-A") {
			t.Fatalf("unsafe error: %v", candidate)
		}
	}
}

func TestInvalidOrUnsupportedRequestDoesNotReadPolicy(t *testing.T) {
	calls := 0
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { calls++; return policy(), nil })
	assertPolicyError(t, a.Authorize(context.Background(), auth.Check{}), auth.ErrInvalidCheck, auth.InvalidCheck)
	c := check()
	c.Action = "root.shell"
	assertPolicyError(t, a.Authorize(context.Background(), c), auth.ErrDenied, auth.Denied)
	if calls != 0 {
		t.Fatal("invalid/unscoped request read policy")
	}
}

func TestConcurrentReadOnlyChecks(t *testing.T) {
	immutable := policy()
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return immutable, nil })
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Authorize(context.Background(), check()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
