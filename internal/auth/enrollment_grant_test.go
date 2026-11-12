package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnrollmentReadGrantRevocationSurvivesReceiptPruning(t *testing.T) {
	ctx := context.Background()
	store := &identityTestStore{}
	identity, e := NewIdentityServiceWithResources(store, func(context.Context, string, string) ([]string, error) { return []string{"node-one"}, nil }, nil)
	if e != nil {
		t.Fatal(e)
	}
	setup, e := identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = identity.Bootstrap(ctx, setup, "admin", identityTestPassword); e != nil {
		t.Fatal(e)
	}
	login := identityLogin(t, identity)
	input := GrantRequest{PrincipalID: login.Identity.SubjectID, ResourceType: "node", ResourceID: "node-one", Permissions: []string{"nodes.read"}}
	grant, e := identity.CreateEnrollmentReadGrant(ctx, login.Secret, login.CSRF, input, "enrollment-permanent-one")
	if e != nil {
		t.Fatal(e)
	}
	if e = identity.RevokeResourceGrant(ctx, login.Secret, login.CSRF, grant.GrantID); e != nil {
		t.Fatal(e)
	}
	if e = identity.update(ctx, func(st *identityState, _ time.Time) error { st.Receipts = map[string]identityReceipt{}; return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e = identity.CreateEnrollmentReadGrant(ctx, login.Secret, login.CSRF, input, "enrollment-permanent-one"); !errors.Is(e, ErrDenied) {
		t.Fatal("pruned receipt resurrected enrollment grant", e)
	}
}
