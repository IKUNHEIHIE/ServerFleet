package nodes_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

type mockAuthorizer struct {
	allowed  map[string]bool
	failures map[string]error
	checks   []auth.Check
}

func allowOnly(ids ...string) *mockAuthorizer {
	m := &mockAuthorizer{allowed: map[string]bool{}, failures: map[string]error{}}
	for _, id := range ids {
		m.allowed[id] = true
	}
	return m
}

func (m *mockAuthorizer) Authorize(_ context.Context, check auth.Check) error {
	m.checks = append(m.checks, check)
	if len(check.NodeIDs) != 1 {
		return errors.New("unexpected node batch")
	}
	if err, ok := m.failures[check.NodeIDs[0]]; ok {
		return err
	}
	if !m.allowed[check.NodeIDs[0]] {
		return auth.ErrDenied
	}
	return nil
}

type sequenceAuthorizer struct {
	results []error
	calls   int
}

func (s *sequenceAuthorizer) Authorize(context.Context, auth.Check) error {
	if s.calls >= len(s.results) {
		return nil
	}
	err := s.results[s.calls]
	s.calls++
	return err
}

func policyFor(t *testing.T, subject string, granted ...string) *auth.NodePolicy {
	t.Helper()
	known := make(map[string]struct{})
	for _, node := range loadFixture(t) {
		known[node.ID] = struct{}{}
	}
	grants := make(map[string]struct{}, len(granted))
	for _, id := range granted {
		grants[id] = struct{}{}
	}
	return &auth.NodePolicy{
		KnownNodes: known,
		ReadGrants: map[string]map[string]struct{}{subject: grants},
	}
}

func TestVisibleFiltersToAllowedNodes(t *testing.T) {
	fixture := loadFixture(t)
	authorizer := allowOnly("node-online-a", "node-offline-a")

	got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture)
	if err != nil {
		t.Fatalf("Visible returned error: %v", err)
	}
	if want := []string{"node-online-a", "node-offline-a"}; !slices.Equal(ids(got), want) {
		t.Fatalf("Visible ids = %v, want %v", ids(got), want)
	}
	if len(authorizer.checks) != len(fixture) {
		t.Fatalf("authorizer saw %d checks, want %d", len(authorizer.checks), len(fixture))
	}
	for i, check := range authorizer.checks {
		if check.SubjectID != "user-1" {
			t.Errorf("check %d subject = %q, want user-1", i, check.SubjectID)
		}
		if check.Action != "nodes.read" {
			t.Errorf("check %d action = %q, want nodes.read", i, check.Action)
		}
		if len(check.NodeIDs) != 1 {
			t.Errorf("check %d targets = %v, want one", i, check.NodeIDs)
		}
	}
}

func TestVisibleDeniesByDefault(t *testing.T) {
	fixture := loadFixture(t)
	got, err := nodes.Visible(context.Background(), allowOnly(), "user-2", fixture)
	if err != nil {
		t.Fatalf("Visible returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Visible ids = %v, want none", ids(got))
	}
}

func TestVisibleFailsClosedWithoutAuthorizerOrSubject(t *testing.T) {
	fixture := loadFixture(t)
	if _, err := nodes.Visible(context.Background(), nil, "user-1", fixture); !errors.Is(err, nodes.ErrMissingAuthorizer) {
		t.Fatalf("nil authorizer error = %v, want ErrMissingAuthorizer", err)
	}
	if _, err := nodes.Visible(context.Background(), allowOnly(), "   ", fixture); !errors.Is(err, nodes.ErrMissingSubject) {
		t.Fatalf("blank subject error = %v, want ErrMissingSubject", err)
	}

	authorizer := allowOnly("node-online-a")
	got, err := nodes.Visible(context.Background(), authorizer, " user-1 ", fixture[:1])
	if !errors.Is(err, nodes.ErrInvalidSubject) {
		t.Fatalf("padded subject error = %v, want ErrInvalidSubject", err)
	}
	if got != nil {
		t.Fatalf("padded subject returned %v, want nil", ids(got))
	}
	if len(authorizer.checks) != 0 {
		t.Fatalf("padded subject was forwarded to the authorizer %d times", len(authorizer.checks))
	}
}

func TestVisibleAbortsOnNonDeniedAuthorizationErrors(t *testing.T) {
	fixture := loadFixture(t)[:3]
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "policy unavailable", err: auth.ErrPolicyUnavailable},
		{name: "invalid check", err: auth.ErrInvalidCheck},
		{name: "unknown error", err: errors.New("authorization backend down")},
		{name: "wrapped policy unavailable", err: fmt.Errorf("policy store: %w", auth.ErrPolicyUnavailable)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &sequenceAuthorizer{results: []error{nil, tc.err, nil}}
			got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if got != nil {
				t.Fatalf("Visible returned partial list %v with error", ids(got))
			}
		})
	}
}

func TestVisibleAbortsOnFirstLastAndMidFailures(t *testing.T) {
	fixture := loadFixture(t)[:3]
	for _, tc := range []struct {
		name      string
		nodeCount int
		results   []error
		want      error
	}{
		{
			name:      "first node",
			nodeCount: 1,
			results:   []error{auth.ErrPolicyUnavailable},
			want:      auth.ErrPolicyUnavailable,
		},
		{
			name:      "last node after an allowed node",
			nodeCount: 2,
			results:   []error{nil, auth.ErrInvalidCheck},
			want:      auth.ErrInvalidCheck,
		},
		{
			name:      "middle after an allowed node",
			nodeCount: 3,
			results:   []error{nil, auth.ErrPolicyUnavailable, nil},
			want:      auth.ErrPolicyUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &sequenceAuthorizer{results: tc.results}
			got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture[:tc.nodeCount])
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if got != nil {
				t.Fatalf("Visible returned partial list %v with error", ids(got))
			}
		})
	}
}

func TestVisibleFiltersWrappedDeniedWithoutAborting(t *testing.T) {
	authorizer := &sequenceAuthorizer{results: []error{
		nil,
		fmt.Errorf("authorization layer: %w", auth.ErrDenied),
		nil,
	}}
	got, err := nodes.Visible(context.Background(), authorizer, "user-1", loadFixture(t)[:3])
	if err != nil {
		t.Fatalf("wrapped denial aborted the read: %v", err)
	}
	if want := []string{"node-online-a", "node-offline-a"}; !slices.Equal(ids(got), want) {
		t.Fatalf("Visible ids = %v, want %v", ids(got), want)
	}
}

func TestVisibleStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authorizer := allowOnly("node-online-a")
	if _, err := nodes.Visible(ctx, authorizer, "user-1", loadFixture(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(authorizer.checks) != 0 {
		t.Fatalf("authorizer was called %d times after cancellation", len(authorizer.checks))
	}
}

func TestVisiblePropagatesCancellationOrDeadlineFromFinalCheck(t *testing.T) {
	fixture := loadFixture(t)[:1]
	for _, tc := range []struct {
		name string
		auth func(context.Context, context.CancelFunc) auth.Authorizer
		want error
	}{
		{
			name: "cancel during final check",
			auth: func(_ context.Context, cancel context.CancelFunc) auth.Authorizer {
				return authorizerFunc(func(context.Context, auth.Check) error {
					cancel()
					return context.Canceled
				})
			},
			want: context.Canceled,
		},
		{
			name: "cancel during final check with nil authorizer error",
			auth: func(_ context.Context, cancel context.CancelFunc) auth.Authorizer {
				return authorizerFunc(func(context.Context, auth.Check) error {
					cancel()
					return nil
				})
			},
			want: context.Canceled,
		},
		{
			name: "wrapped deadline during final check",
			auth: func(context.Context, context.CancelFunc) auth.Authorizer {
				return authorizerFunc(func(context.Context, auth.Check) error {
					return fmt.Errorf("authorization failed: %w", context.DeadlineExceeded)
				})
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, err := nodes.Visible(ctx, tc.auth(ctx, cancel), "user-1", fixture)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if got != nil {
				t.Fatalf("Visible returned %v together with an error, want nil", ids(got))
			}
		})
	}
}

func TestVisiblePropagatesExpiredDeadlineDuringFinalCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	authorizer := authorizerFunc(func(ctx context.Context, _ auth.Check) error {
		<-ctx.Done()
		return ctx.Err()
	})
	got, err := nodes.Visible(ctx, authorizer, "user-1", loadFixture(t)[:1])
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if got != nil {
		t.Fatalf("Visible returned %v together with an error, want nil", ids(got))
	}
}

func TestVisibleWithRealNodePolicyAuthorizerMixedGrants(t *testing.T) {
	policy := policyFor(t, "user-1", "node-online-a", "node-offline-a")
	authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		return policy, nil
	})
	got, err := nodes.Visible(context.Background(), authorizer, "user-1", loadFixture(t))
	if err != nil {
		t.Fatalf("Visible returned error: %v", err)
	}
	if want := []string{"node-online-a", "node-offline-a"}; !slices.Equal(ids(got), want) {
		t.Fatalf("Visible ids = %v, want %v", ids(got), want)
	}
}

func TestVisibleWithRealNodePolicyAuthorizerAllDenied(t *testing.T) {
	policy := policyFor(t, "user-1")
	authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		return policy, nil
	})
	got, err := nodes.Visible(context.Background(), authorizer, "user-1", loadFixture(t))
	if err != nil {
		t.Fatalf("Visible returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Visible ids = %v, want none", ids(got))
	}
}

func TestVisibleWithRealNodePolicyAuthorizerPolicyUnavailableAborts(t *testing.T) {
	policy := policyFor(t, "user-1", "node-online-a", "node-online-b")
	fixture := loadFixture(t)[:2]

	t.Run("first node", func(t *testing.T) {
		authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
			return nil, errors.New("policy store down")
		})
		got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture[:1])
		if !errors.Is(err, auth.ErrPolicyUnavailable) {
			t.Fatalf("error = %v, want ErrPolicyUnavailable", err)
		}
		if got != nil {
			t.Fatalf("Visible returned partial list %v", ids(got))
		}
	})

	t.Run("after an allowed node", func(t *testing.T) {
		calls := 0
		authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
			calls++
			if calls == 1 {
				return policy, nil
			}
			return nil, errors.New("policy store down")
		})
		got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture)
		if !errors.Is(err, auth.ErrPolicyUnavailable) {
			t.Fatalf("error = %v, want ErrPolicyUnavailable", err)
		}
		if got != nil {
			t.Fatalf("Visible returned partial list %v", ids(got))
		}
		if calls != 2 {
			t.Fatalf("policy reader calls = %d, want 2", calls)
		}
	})
}

func TestVisibleWithRealNodePolicyAuthorizerInvalidCheckAborts(t *testing.T) {
	authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		t.Fatal("policy reader must not be called for an invalid check")
		return nil, nil
	})
	got, err := nodes.Visible(context.Background(), authorizer, "user-1", []nodes.Node{{ID: "bad*id"}})
	if !errors.Is(err, auth.ErrInvalidCheck) {
		t.Fatalf("error = %v, want ErrInvalidCheck", err)
	}
	if got != nil {
		t.Fatalf("Visible returned partial list %v", ids(got))
	}
}

func TestVisibleWithRealNodePolicyAuthorizerRevocationTakesEffect(t *testing.T) {
	policy := policyFor(t, "user-1", "node-online-a")
	authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		return policy, nil
	})
	fixture := loadFixture(t)

	buckets, err := nodes.VisibleClassify(context.Background(), authorizer, "user-1", fixture, nodes.DimensionRegion)
	if err != nil {
		t.Fatalf("first VisibleClassify returned error: %v", err)
	}
	want := []nodes.Classification{{Dimension: nodes.DimensionRegion, Value: "cn-north", Count: 1}}
	if !slices.Equal(buckets, want) {
		t.Fatalf("first buckets = %+v, want %+v", buckets, want)
	}

	policy = policyFor(t, "user-1")
	buckets, err = nodes.VisibleClassify(context.Background(), authorizer, "user-1", fixture, nodes.DimensionRegion)
	if err != nil {
		t.Fatalf("post-revocation VisibleClassify returned error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("post-revocation buckets = %+v, want none", buckets)
	}
	visible, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture)
	if err != nil {
		t.Fatalf("post-revocation Visible returned error: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("post-revocation visible ids = %v, want none", ids(visible))
	}
}

func TestVisibleWithRealNodePolicyAuthorizerReaderCancellation(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			authorizer := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
				return nil, fmt.Errorf("policy reader: %w", want)
			})
			got, err := nodes.Visible(context.Background(), authorizer, "user-1", loadFixture(t)[:1])
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if got != nil {
				t.Fatalf("Visible returned partial list %v", ids(got))
			}
		})
	}
}

func TestVisibleWrappersAbortOnPolicyFailure(t *testing.T) {
	fixture := loadFixture(t)[:2]
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "policy unavailable", err: auth.ErrPolicyUnavailable},
		{name: "invalid check", err: auth.ErrInvalidCheck},
		{name: "unknown error", err: errors.New("authorization backend down")},
	} {
		t.Run("VisibleFilter/"+failure.name, func(t *testing.T) {
			authorizer := &sequenceAuthorizer{results: []error{nil, failure.err}}
			got, err := nodes.VisibleFilter(context.Background(), authorizer, "user-1", fixture, nodes.NodeQuery{})
			if !errors.Is(err, failure.err) {
				t.Fatalf("error = %v, want %v", err, failure.err)
			}
			if got != nil {
				t.Fatalf("VisibleFilter returned partial list %v", ids(got))
			}
		})
		t.Run("VisibleClassify/"+failure.name, func(t *testing.T) {
			authorizer := &sequenceAuthorizer{results: []error{nil, failure.err}}
			buckets, err := nodes.VisibleClassify(context.Background(), authorizer, "user-1", fixture, nodes.DimensionRegion)
			if !errors.Is(err, failure.err) {
				t.Fatalf("error = %v, want %v", err, failure.err)
			}
			if buckets != nil {
				t.Fatalf("VisibleClassify returned partial buckets %+v", buckets)
			}
		})
	}
}

func TestVisibleWrappersPropagateFinalCheckCancellation(t *testing.T) {
	fixture := loadFixture(t)[:1]
	for _, tc := range []struct {
		name string
		call func(context.Context, auth.Authorizer) error
	}{
		{
			name: "VisibleFilter",
			call: func(ctx context.Context, authorizer auth.Authorizer) error {
				_, err := nodes.VisibleFilter(ctx, authorizer, "user-1", fixture, nodes.NodeQuery{})
				return err
			},
		},
		{
			name: "VisibleClassify",
			call: func(ctx context.Context, authorizer auth.Authorizer) error {
				_, err := nodes.VisibleClassify(ctx, authorizer, "user-1", fixture, nodes.DimensionRegion)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			authorizer := authorizerFunc(func(context.Context, auth.Check) error {
				cancel()
				return context.Canceled
			})
			if err := tc.call(ctx, authorizer); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestVisibleClassifyScopesCounts(t *testing.T) {
	fixture := loadFixture(t)
	regions, err := nodes.VisibleClassify(
		context.Background(), allowOnly("node-online-a"), "user-1", fixture, nodes.DimensionRegion,
	)
	if err != nil {
		t.Fatalf("VisibleClassify returned error: %v", err)
	}
	want := []nodes.Classification{{Dimension: nodes.DimensionRegion, Value: "cn-north", Count: 1}}
	if !slices.Equal(regions, want) {
		t.Fatalf("visible region buckets = %+v, want %+v", regions, want)
	}
}

func TestVisibleClassifyPropagatesScopeFailure(t *testing.T) {
	_, err := nodes.VisibleClassify(
		context.Background(), nil, "user-1", loadFixture(t), nodes.DimensionRegion,
	)
	if !errors.Is(err, nodes.ErrMissingAuthorizer) {
		t.Fatalf("error = %v, want ErrMissingAuthorizer", err)
	}
}

func TestVisibleFilterScopesResults(t *testing.T) {
	fixture := loadFixture(t)
	got, err := nodes.VisibleFilter(
		context.Background(),
		allowOnly("node-online-a", "node-offline-a"),
		"user-1",
		fixture,
		nodes.NodeQuery{Statuses: []nodes.Status{nodes.StatusOnline}},
	)
	if err != nil {
		t.Fatalf("VisibleFilter returned error: %v", err)
	}
	if want := []string{"node-online-a"}; !slices.Equal(ids(got), want) {
		t.Fatalf("VisibleFilter ids = %v, want %v", ids(got), want)
	}
}
