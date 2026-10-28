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

var errDenied = errors.New("denied")

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
		return errDenied
	}
	return nil
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
		if check.Action == "" {
			t.Errorf("check %d has an empty action", i)
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
}

func TestVisibleExcludesNodesWhoseCheckFails(t *testing.T) {
	fixture := loadFixture(t)
	authorizer := allowOnly("node-online-a")
	authorizer.failures["node-online-b"] = errors.New("authorization backend unavailable")

	got, err := nodes.Visible(context.Background(), authorizer, "user-1", fixture)
	if err != nil {
		t.Fatalf("Visible returned error: %v", err)
	}
	if want := []string{"node-online-a"}; !slices.Equal(ids(got), want) {
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
