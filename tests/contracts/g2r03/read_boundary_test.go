package g2r03_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, c auth.Check) error { return f(ctx, c) }

func TestActualVisibleFiltersOnlyDenied(t *testing.T) {
	policy := &auth.NodePolicy{KnownNodes: map[string]struct{}{"a": {}, "b": {}}, ReadGrants: map[string]map[string]struct{}{"subject": {"a": {}}}}
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) { return policy, nil })
	got, err := nodes.Visible(context.Background(), a, "subject", []nodes.Node{{ID: "a"}, {ID: "b"}})
	if err != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %v, %v; want only authorized a", got, err)
	}
}

func TestActualVisibleAbortsWithoutPartialList(t *testing.T) {
	for _, cause := range []error{auth.ErrPolicyUnavailable, auth.ErrInvalidCheck, context.Canceled, context.DeadlineExceeded, errors.New("unknown failure")} {
		t.Run(cause.Error(), func(t *testing.T) {
			a := authorizerFunc(func(_ context.Context, c auth.Check) error {
				if c.NodeIDs[0] == "a" {
					return nil
				}
				return errors.Join(auth.ErrDenied, cause)
			})
			got, err := nodes.Visible(context.Background(), a, "subject", []nodes.Node{{ID: "a"}, {ID: "b"}})
			if got != nil || !errors.Is(err, cause) {
				t.Fatalf("got %v, %v; want nil and fatal cause", got, err)
			}
		})
	}
}

func TestActualEmptyInventoryDoesNotReadPolicy(t *testing.T) {
	reads := 0
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		reads++
		return nil, auth.ErrPolicyUnavailable
	})
	got, err := nodes.Visible(context.Background(), a, "unverified-subject", nil)
	if err != nil || len(got) != 0 || reads != 0 {
		t.Fatalf("baseline changed: got %v, %v, reads=%d", got, err, reads)
	}
	// This records a missing endpoint preflight, not an approved HTTP success.
	t.Log("LIMITATION: empty inventory bypasses policy reads; future HTTP must preflight trusted subject/policy")
}

func TestActualPolicyIsReadPerNodeNotOneListSnapshot(t *testing.T) {
	reads := 0
	a := auth.NewNodePolicyAuthorizer(func(context.Context) (*auth.NodePolicy, error) {
		reads++
		id := "a"
		if reads > 1 {
			id = "b"
		}
		return &auth.NodePolicy{KnownNodes: map[string]struct{}{"a": {}, "b": {}}, ReadGrants: map[string]map[string]struct{}{"subject": {id: {}}}}, nil
	})
	got, err := nodes.Visible(context.Background(), a, "subject", []nodes.Node{{ID: "a"}, {ID: "b"}})
	if err != nil || len(got) != 2 || reads != 2 {
		t.Fatalf("baseline changed: got %v, %v, reads=%d", got, err, reads)
	}
	t.Log("LIMITATION: per-node current checks do not establish an atomic list-wide policy snapshot")
}
