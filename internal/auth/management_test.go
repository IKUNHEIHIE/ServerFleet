package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBootstrapAdminGetsCurrentNodeScopeWhileOtherAdminsStayExplicit(t *testing.T) {
	base, store, _ := identityFixture(t)
	ctx := context.Background()
	managed, err := NewIdentityServiceWithResources(store, func(context.Context, string, string) ([]string, error) {
		return []string{"node-a", "node-b"}, nil
	}, []PermissionDefinition{
		{"host.patch", "Controlled host change", "node", false, false},
		{"nodes.write", "Modify nodes", "node", false, false},
		{"nodes.suspend", "Suspend nodes", "node", false, false},
		{"nodes.revoke", "Archive nodes", "node", false, true},
		{"gateway.read", "Read gateways", "node", true, false},
		{"gateway.write", "Manage gateways", "node", false, false},
	})
	if err != nil {
		t.Fatal(err)
	}
	managed.SetBootstrapNodeLister(func(context.Context) ([]string, error) {
		return []string{"node-a", "node-b"}, nil
	})
	managed.now = base.now
	admin := identityLogin(t, managed)
	ordinary, err := managed.CreateUser(ctx, admin.Secret, admin.CSRF, UserCreation{"administrator-two", "Other administrator", identityTestPassword, []string{"administrator"}, true})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{NodeReadAction, "nodes.write", "nodes.suspend", "nodes.revoke", "gateway.read", "gateway.write"} {
		if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: action, NodeIDs: []string{"node-a", "node-b"}}); err != nil {
			t.Fatalf("bootstrap administrator denied %s: %v", action, err)
		}
		if err := managed.Authorize(ctx, Check{SubjectID: ordinary.UserID, Action: action, NodeIDs: []string{"node-a"}}); err != ErrDenied {
			t.Fatalf("ordinary administrator implicitly gained %s: %v", action, err)
		}
	}
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: "tasks.approve", NodeIDs: []string{"node-a"}}); err != nil {
		t.Fatalf("bootstrap administrator denied role-authorized approval permission: %v", err)
	}
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: "unregistered.action", NodeIDs: []string{"node-a"}}); err == nil {
		t.Fatal("bootstrap administrator bypassed permission catalog")
	}
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: "nodes.write", NodeIDs: []string{"new-node"}}); err != nil {
		t.Fatalf("bootstrap administrator could not create a new node: %v", err)
	}
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: "host.patch", NodeIDs: []string{"node-a"}}); err != nil {
		t.Fatalf("bootstrap administrator could not use catalog-authorized node action: %v", err)
	}
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: "unregistered.action", NodeIDs: []string{"node-a"}}); err == nil {
		t.Fatal("bootstrap administrator bypassed permission catalog")
	}
	policy, err := managed.NodeReadPolicySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.ReadGrants[admin.Identity.SubjectID]) != 2 || len(policy.ReadGrants[ordinary.UserID]) != 0 {
		t.Fatalf("unexpected read scope: %#v", policy.ReadGrants)
	}
	managed.SetBootstrapNodeLister(func(context.Context) ([]string, error) { return nil, errors.New("inventory unavailable") })
	if err := managed.Authorize(ctx, Check{SubjectID: admin.Identity.SubjectID, Action: NodeReadAction, NodeIDs: []string{"node-a"}}); err != ErrPolicyUnavailable {
		t.Fatalf("bootstrap inventory failure did not fail closed: %v", err)
	}
}

func TestIdentityRBACExactGrantsGroupSnapshotAndRevocation(t *testing.T) {
	s, m, now := identityFixture(t)
	ctx := context.Background()
	members := []string{"node-a", "node-b"}
	sourceError := false
	resolver := func(context.Context, string, string) ([]string, error) {
		if sourceError {
			return nil, errors.New("private backend details")
		}
		return append([]string(nil), members...), nil
	}
	managed, err := NewIdentityServiceWithResources(m, resolver, []PermissionDefinition{{"host.patch", "Controlled change", "node", false, false}})
	if err != nil {
		t.Fatal(err)
	}
	managed.now = s.now
	s = managed
	admin := identityLogin(t, s)
	user, err := s.CreateUser(ctx, admin.Secret, admin.CSRF, UserCreation{"reader", "Read only", identityTestPassword, []string{"reader"}, true})
	if err != nil {
		t.Fatal(err)
	}
	check := Check{SubjectID: admin.Identity.SubjectID, Action: NodeReadAction, NodeIDs: []string{"node-a"}}
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("admin got implicit nodes", err)
	}
	check.SubjectID = user.UserID
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("zero grants accepted", err)
	}
	expires := now.Add(time.Minute)
	grant, err := s.CreateResourceGrant(ctx, admin.Secret, admin.CSRF, GrantRequest{user.UserID, "service_group", "group-1", []string{NodeReadAction, "host.patch"}, &expires})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Authorize(ctx, check); err != nil {
		t.Fatal(err)
	}
	check.Action = "host.patch"
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("reader modified node", err)
	}
	check.Action = NodeReadAction
	members = append(members, "node-c")
	check.NodeIDs = []string{"node-c"}
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("new member inherited old grant", err)
	}
	members = []string{"node-b", "node-c"}
	check.NodeIDs = []string{"node-a"}
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("removed member retained grant", err)
	}
	check.NodeIDs = []string{"node-b"}
	sourceError = true
	if err = s.Authorize(ctx, check); err != ErrPolicyUnavailable {
		t.Fatal("source failure allowed/echoed", err)
	}
	sourceError = false
	policy, err := s.NodeReadPolicy(ctx, user.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := policy.ReadGrants[user.UserID]["node-b"]; !ok {
		t.Fatal("fresh policy missing member")
	}
	*now = now.Add(time.Minute)
	if err = s.Authorize(ctx, check); err != ErrDenied {
		t.Fatal("expired grant accepted", err)
	}
	if err = s.RevokeResourceGrant(ctx, admin.Secret, admin.CSRF, grant.GrantID); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityUserRevisionDemotionEpochAndLastAdmin(t *testing.T) {
	s, _, _ := identityFixture(t)
	ctx := context.Background()
	admin := identityLogin(t, s)
	user, err := s.CreateUser(ctx, admin.Secret, admin.CSRF, UserCreation{"operator", "", identityTestPassword, []string{"operator"}, true})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := s.Login(ctx, "operator", identityTestPassword, "other-ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListUsers(ctx, operator.Secret); err != ErrDenied {
		t.Fatal("non-admin managed identities", err)
	}
	roles := []string{"reader"}
	if _, err = s.PatchUser(ctx, admin.Secret, admin.CSRF, user.UserID, user.Revision+1, UserChange{RoleIDs: &roles}); err != ErrRevisionConflict {
		t.Fatal(err)
	}
	updated, err := s.PatchUser(ctx, admin.Secret, admin.CSRF, user.UserID, user.Revision, UserChange{RoleIDs: &roles})
	if err != nil || updated.Revision != 2 {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, operator.Secret, "", false); err != ErrCredential {
		t.Fatal("role downgrade retained session", err)
	}
	disabled := false
	users, err := s.ListUsers(ctx, admin.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var adminRevision uint64
	for _, u := range users {
		if u.UserID == admin.Identity.SubjectID {
			adminRevision = u.Revision
		}
	}
	if _, err = s.PatchUser(ctx, admin.Secret, admin.CSRF, admin.Identity.SubjectID, adminRevision, UserChange{Enabled: &disabled}); err != ErrDenied {
		t.Fatal("disabled last admin", err)
	}
}

func TestIdentityLoginSwitchRevokesBrowserSessionAtomically(t *testing.T) {
	s, m, _ := identityFixture(t)
	ctx := context.Background()
	first := identityLogin(t, s)
	_, err := s.CreateUser(ctx, first.Secret, first.CSRF, UserCreation{"other", "", identityTestPassword, []string{"reader"}, true})
	if err != nil {
		t.Fatal(err)
	}
	m.failCommit = true
	if _, err = s.LoginReplacing(ctx, "other", identityTestPassword, "ip", first.Secret); err != ErrIdentityUnavailable {
		t.Fatal(err)
	}
	m.failCommit = false
	if _, err = s.Authenticate(ctx, first.Secret, "", false); err != nil {
		t.Fatal("failed login revoked old session", err)
	}
	second, err := s.LoginReplacing(ctx, "other", identityTestPassword, "ip", first.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, first.Secret, "", false); err != ErrCredential {
		t.Fatal("old browser session still active", err)
	}
	profile, csrf, err := s.CurrentIdentity(ctx, second.Secret)
	if err != nil || profile.UserID != second.Identity.SubjectID || csrf != second.CSRF {
		t.Fatal(err)
	}
	list, err := s.ListSessions(ctx, second.Secret)
	if err != nil || len(list) != 1 || !list[0].Current {
		t.Fatal(list, err)
	}
}
