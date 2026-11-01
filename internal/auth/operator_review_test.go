package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestR1OperatorGrantsRemainExactForURLSafeSubjectIDs(t *testing.T) {
	s, store, _ := identityFixture(t)
	ctx := context.Background()
	managed, err := NewIdentityServiceWithResources(store, func(context.Context, string, string) ([]string, error) { return []string{"node-one"}, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	managed.now = s.now
	s = managed
	admin := identityLogin(t, s)
	user, err := s.CreateUser(ctx, admin.Secret, admin.CSRF, UserCreation{"operator-probe", "Probe", identityTestPassword, []string{"operator"}, true})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.CreateResourceGrant(ctx, admin.Secret, admin.CSRF, GrantRequest{user.UserID, "node", "node-one", []string{"tasks.read", "tasks.approve"}, nil})
	if err != nil {
		t.Fatal(err)
	}
	previous := user.UserID
	for _, prefix := range []string{"a", "_", "-", "a_b-", "-A_z_"} {
		id := prefix + strings.Repeat("a", 42-len(prefix)) + "A"
		if err := s.update(ctx, func(st *identityState, _ time.Time) error {
			u := st.Users[previous]
			delete(st.Users, previous)
			u.ID = id
			st.Users[id] = u
			g := st.Grants[grant.GrantID]
			g.Value.PrincipalID = id
			st.Grants[grant.GrantID] = g
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		previous = id
		for _, action := range []string{"tasks.read", "tasks.approve"} {
			if err := s.AuthorizeSubjectAction(ctx, id, action); err != nil {
				t.Fatalf("role denied %q: %v", prefix, err)
			}
			if err := s.Authorize(ctx, Check{SubjectID: id, Action: action, NodeIDs: []string{"node-one"}}); err != nil {
				t.Fatalf("grant denied %q: %v", prefix, err)
			}
			if err := s.Authorize(ctx, Check{SubjectID: id, Action: action, NodeIDs: []string{"node-two"}}); err == nil {
				t.Fatal("grant escaped its node")
			}
		}
	}
	if err := s.RevokeResourceGrant(ctx, admin.Secret, admin.CSRF, grant.GrantID); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, Check{SubjectID: previous, Action: "tasks.read", NodeIDs: []string{"node-one"}}); err == nil {
		t.Fatal("revoked operator grant remained usable")
	}
}
