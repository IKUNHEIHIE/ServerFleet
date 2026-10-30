package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func approvalFixture(t *testing.T) (*ApprovalService, *identityTestStore, *time.Time, LoginSession, LoginSession, *PreparedApprovalPlan, *uint64) {
	t.Helper()
	identity, store, now := identityFixture(t)
	identity.resources = func(_ context.Context, _, id string) ([]string, error) { return []string{id}, nil }
	admin := identityLogin(t, identity)
	user, err := identity.CreateUser(context.Background(), admin.Secret, admin.CSRF, UserCreation{Username: "operator", Password: identityTestPassword, RoleIDs: []string{"operator"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{admin.Identity.SubjectID, user.UserID} {
		if _, err = identity.CreateResourceGrant(context.Background(), admin.Secret, admin.CSRF, GrantRequest{PrincipalID: subject, ResourceType: "node", ResourceID: "node-a", Permissions: []string{"tasks.submit", "tasks.approve"}}); err != nil {
			t.Fatal(err)
		}
	}
	operator, err := identity.Login(context.Background(), "operator", identityTestPassword, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	plan := &PreparedApprovalPlan{ID: "server-plan-01", SubjectID: user.UserID, ActionID: "tasks.submit", Digest: strings.Repeat("a", 64), Source: "ai", AISessionID: "ai-session-1", AIScopeRevision: 1, NodeIDs: []string{"node-a"}, RootShell: true, ReviewSummary: []byte(`{"action":"reviewed synthetic root plan"}`), ExpiresAt: now.Add(10 * time.Minute)}
	scopeRevision := uint64(1)
	service, err := NewApprovalService(identity, func(_ context.Context, id string) (PreparedApprovalPlan, error) {
		if id != plan.ID {
			return PreparedApprovalPlan{}, errors.New("plan not found")
		}
		return clonePreparedPlan(*plan), nil
	}, func(_ context.Context, p PreparedApprovalPlan) error {
		if p.AIScopeRevision != scopeRevision {
			return ErrDenied
		}
		return nil
	}, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, now, admin, operator, plan, &scopeRevision
}
func consumeApprovalTestTx(ctx context.Context, s *ApprovalService, store *identityTestStore, id, subject, digest string, nodes []string, changes ...func(*ApprovalTaskBinding)) (string, error) {
	var approver string
	err := store.Update(ctx, func(raw []byte) ([]byte, error) {
		local := bytes.Clone(raw)
		var state identityState
		if json.Unmarshal(raw, &state) != nil {
			return nil, ErrIdentityUnavailable
		}
		plan := state.Approvals[id].Plan
		// Explicit task fixture; production must bind ACTUAL task fields, not
		// echo approval metadata to manufacture a match.
		binding := ApprovalTaskBinding{TaskID: "fixture-task-1", SubjectID: subject, ActionID: plan.ActionID, Digest: digest, Source: plan.Source, PluginID: plan.PluginID, AISessionID: plan.AISessionID, AIScopeRevision: plan.AIScopeRevision, NodeIDs: slices.Clone(nodes)}
		for _, change := range changes {
			change(&binding)
		}
		tx := IdentitySQLStatements{Read: func(context.Context, string, ...any) ([]byte, bool, error) { return bytes.Clone(local), true, nil }, Exec: func(_ context.Context, _ string, args ...any) (int64, error) {
			local = bytes.Clone(args[0].([]byte))
			return 1, nil
		}}
		var err error
		approver, err = s.ConsumeTx(ctx, tx, id, binding)
		if err != nil {
			return nil, err
		}
		return local, nil
	})
	if err != nil {
		return "", err
	}
	return approver, nil
}
func TestApprovalHumanAdminPlanBindingSingleUseAndAtomicRollback(t *testing.T) {
	ctx := context.Background()
	s, store, _, admin, operator, plan, scope := approvalFixture(t)
	pending, err := s.Request(ctx, operator.Secret, operator.CSRF, plan.ID)
	if err != nil || pending.State != "pending" {
		t.Fatal(pending, err)
	}
	again, err := s.Request(ctx, operator.Secret, operator.CSRF, plan.ID)
	if err != nil || again.ID != pending.ID {
		t.Fatal(again, err)
	}
	plan.Digest = strings.Repeat("b", 64)
	if _, err = s.Request(ctx, operator.Secret, operator.CSRF, plan.ID); err != ErrRevisionConflict {
		t.Fatal("modified plan reuses approval", err)
	}
	plan.Digest = strings.Repeat("a", 64)
	if _, err = s.Decide(ctx, operator.Secret, operator.CSRF, pending.ID, 1, true); err != ErrDenied {
		t.Fatal("AI root self-approved by nonadmin", err)
	}
	approved, err := s.Decide(ctx, admin.Secret, admin.CSRF, pending.ID, 1, true)
	if err != nil || approved.State != "approved" {
		t.Fatal(approved, err)
	}
	if _, err = s.Decide(ctx, admin.Secret, admin.CSRF, pending.ID, 1, true); err != ErrDenied {
		t.Fatal("decision replay", err)
	}
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, strings.Repeat("f", 64), plan.NodeIDs); err != ErrDenied {
		t.Fatal("changed task digest", err)
	}
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, []string{"node-b"}); err != ErrDenied {
		t.Fatal("expanded target", err)
	}
	for _, change := range []func(*ApprovalTaskBinding){func(b *ApprovalTaskBinding) { b.Source = "web" }, func(b *ApprovalTaskBinding) { b.AISessionID = "other-session" }, func(b *ApprovalTaskBinding) { b.AIScopeRevision++ }, func(b *ApprovalTaskBinding) { b.PluginID = "xray-manager" }, func(b *ApprovalTaskBinding) { b.ActionID = "other-action" }} {
		if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs, change); err != ErrDenied {
			t.Fatal("task metadata changed under same digest", err)
		}
	}
	*scope = 2
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs); err != ErrDenied {
		t.Fatal("revoked AI scope", err)
	}
	*scope = 1
	store.failCommit = true
	if id, err := consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs); err == nil || id != "" {
		t.Fatal("failed outer transaction delivered approver", id, err)
	}
	store.failCommit = false
	view, err := s.Get(ctx, operator.Secret, pending.ID)
	if err != nil || view.State != "approved" {
		t.Fatal("outer rollback consumed approval", view, err)
	}
	approver, err := consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs)
	if err != nil || approver != admin.Identity.SubjectID {
		t.Fatal(approver, err)
	}
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs); err != ErrDenied {
		t.Fatal("approval replay consumed twice", err)
	}
	var persisted identityState
	if json.Unmarshal(store.data, &persisted) != nil || persisted.Approvals[pending.ID].View.State != "consumed" {
		t.Fatal("consumption missing")
	}
}
func TestApprovalExpiryPermissionEpochAndCSRF(t *testing.T) {
	ctx := context.Background()
	s, store, now, admin, operator, plan, _ := approvalFixture(t)
	if _, err := s.Request(ctx, operator.Secret, strings.Repeat("A", 43), plan.ID); err != ErrDenied {
		t.Fatal("CSRF bypass", err)
	}
	pending, err := s.Request(ctx, operator.Secret, operator.CSRF, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, admin.Secret, admin.CSRF, pending.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5 * time.Minute)
	if view, err := s.Get(ctx, operator.Secret, pending.ID); err != nil || view.State != "expired" {
		t.Fatal(view, err)
	}
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs); err != ErrDenied {
		t.Fatal("expired approval", err)
	}
	*now = now.Add(-time.Second)
	if _, err = s.Get(ctx, operator.Secret, pending.ID); err != ErrIdentityUnavailable {
		t.Fatal("observed expired approval revived by clock", err)
	}
	s, store, _, admin, operator, plan, _ = approvalFixture(t)
	pending, err = s.Request(ctx, operator.Secret, operator.CSRF, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, admin.Secret, admin.CSRF, pending.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	if err = s.identity.ChangePassword(ctx, admin.Secret, admin.CSRF, identityTestPassword, "new-long-test-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = consumeApprovalTestTx(ctx, s, store, pending.ID, operator.Identity.SubjectID, plan.Digest, plan.NodeIDs); err != ErrDenied {
		t.Fatal("stale approver epoch", err)
	}
}
