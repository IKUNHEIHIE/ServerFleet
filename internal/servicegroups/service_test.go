package servicegroups_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

type guardFunc func(context.Context, string) error

func (f guardFunc) CheckDelete(ctx context.Context, id string) error { return f(ctx, id) }

type fakeEligibility struct {
	gateway bool
	health  map[string]bool
}

func (f fakeEligibility) GatewayEligible(context.Context, string) (bool, error) {
	return f.gateway, nil
}

func (f fakeEligibility) NodeHealthy(_ context.Context, nodeID string) (bool, string, error) {
	if healthy, ok := f.health[nodeID]; ok {
		return healthy, "", nil
	}
	return false, "unhealthy", nil
}

func policyReader(subject string, ids []string) auth.PolicyReader {
	policy := &auth.NodePolicy{KnownNodes: map[string]struct{}{}, ReadGrants: map[string]map[string]struct{}{}}
	grant := map[string]struct{}{}
	for _, id := range ids {
		policy.KnownNodes[id] = struct{}{}
		grant[id] = struct{}{}
	}
	policy.ReadGrants[subject] = grant
	return func(context.Context) (*auth.NodePolicy, error) { return policy, nil }
}

type fixture struct {
	db      *storage.DB
	nodes   *nodes.Repo
	repo    *servicegroups.Repo
	audit   *audit.SQLRecorder
	service *servicegroups.Service
}

func newFixture(t *testing.T, authorizer auth.Authorizer, eligibility servicegroups.Eligibility, guard servicegroups.DeleteGuard) fixture {
	t.Helper()
	db := testkit.NewDB(t, nodes.Migrations(), servicegroups.Migrations(), audit.Migrations())
	nodeRepo, err := nodes.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"n1", "n2"} {
		if _, err := nodeRepo.Create(context.Background(), db, nodes.NodeCreateInput{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := servicegroups.NewRepo(db, nodeRepo)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	actions := append(servicegroups.ServiceActions(), servicegroups.ActionWrite)
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(func(context.Context, auth.Check) error { return nil })
	}
	service, err := servicegroups.NewService(servicegroups.Options{
		DB: db, Repo: repo, Nodes: nodeRepo, Audit: recorder,
		Authorizer: authorizer, ReadPolicy: policyReader("admin", []string{"n1", "n2"}),
		Eligibility: eligibility, DeleteGuard: guard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{db: db, nodes: nodeRepo, repo: repo, audit: recorder, service: service}
}

func validInput(id, name string) servicegroups.Input {
	return servicegroups.Input{
		ID: id, Name: name, Description: "web tier",
		Service:       servicegroups.ServiceConfig{Port: 8080, Protocol: "http", HealthCheck: "/health"},
		LoadBalancing: servicegroups.LoadBalancing{Enabled: true, Strategy: "round_robin"},
		MemberNodeIDs: []string{"n1", "n2"},
	}
}

func TestServiceGroupPersistence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	created, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || len(created.Members) != 2 {
		t.Fatalf("created = %+v", created)
	}
	got, err := h.service.GetVisible(ctx, "admin", "sg1")
	if err != nil || got.Name != "Web" || got.Service.Port != 8080 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: servicegroups.ActionCreate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	snapshot, err := h.service.VisibleSnapshot(ctx, "admin", "sg1")
	if err != nil || snapshot.Revision != 1 || len(snapshot.Members) != 2 {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	if _, err := h.service.Create(ctx, "admin", "req-2", validInput("sg1", "Duplicate Name")); !errors.Is(err, servicegroups.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
}

func TestServiceGroupRevisionAndUnknownMembers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	updated := validInput("sg1", "Web Updated")
	updated.MemberNodeIDs = []string{"n1"}
	if _, err := h.service.Update(ctx, "admin", "req-2", "sg1", 1, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Update(ctx, "admin", "req-3", "sg1", 1, updated); !errors.Is(err, servicegroups.ErrRevisionConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	if _, err := h.service.ReplaceMembers(ctx, "admin", "req-4", "sg1", 2, []string{"n1", "missing"}); !errors.Is(err, servicegroups.ErrUnknownMember) {
		t.Fatalf("unknown member err = %v", err)
	}
	record, err := h.repo.Get(ctx, "sg1")
	if err != nil || len(record.Members) != 2 || record.Revision != 2 {
		t.Fatalf("membership changed by config update: %+v, %v", record, err)
	}
	if _, err := h.service.ReplaceMembers(ctx, "admin", "req-5", "sg1", 2, []string{"n2"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := h.service.VisibleSnapshot(ctx, "admin", "sg1")
	if err != nil || len(snapshot.Members) != 1 || snapshot.Members[0] != "n2" || snapshot.Revision != 3 {
		t.Fatalf("snapshot after replace = %+v, %v", snapshot, err)
	}
}

func TestServiceGroupVisibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	visibleOnlyOne := policyReader("u1", []string{"n1"})
	service, err := servicegroups.NewService(servicegroups.Options{
		DB: h.db, Repo: h.repo, Nodes: h.nodes, Audit: h.audit,
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		ReadPolicy: visibleOnlyOne,
	})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := service.ListVisible(ctx, "u1"); err != nil || len(list) != 0 {
		t.Fatalf("hidden group leaked: %+v, %v", list, err)
	}
	if _, err := service.GetVisible(ctx, "u1", "sg1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want denied", err)
	}
	if _, err := service.ListVisible(ctx, "unknown"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("unknown subject err = %v", err)
	}
}

func TestServiceGroupDeniedWriteAudited(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }), nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-deny", validInput("sg1", "Web")); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.repo.Get(ctx, "sg1"); !errors.Is(err, servicegroups.ErrNotFound) {
		t.Fatalf("denied write mutated storage: %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: servicegroups.ActionWrite, Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestSetSchedulingAdvancesRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	member, revision, err := h.service.SetScheduling(ctx, "admin", "req-2", "sg1", "n1", true, "maintenance")
	if err != nil || !member.Paused || revision != 2 {
		t.Fatalf("scheduling = %+v rev=%d err=%v", member, revision, err)
	}
	if _, _, err := h.repo.SetScheduling(ctx, h.db, "sg1", "n1", 1, false, ""); !errors.Is(err, servicegroups.ErrRevisionConflict) {
		t.Fatalf("stale scheduling err = %v", err)
	}
}

func TestDispatchPreviewFailsClosedWithoutEligibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	preview, err := h.service.Preview(ctx, "admin", "sg1")
	if err != nil {
		t.Fatal(err)
	}
	if preview.GatewayEligible || len(preview.EligibleNodeIDs) != 0 {
		t.Fatalf("preview without eligibility = %+v", preview)
	}
	for _, excluded := range preview.ExcludedNodes {
		if excluded.Reason != "gateway_unverified" {
			t.Fatalf("reason = %q", excluded.Reason)
		}
	}
}

func TestDispatchPreviewRequiresOnlineHealthyAndGateway(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, fakeEligibility{gateway: true, health: map[string]bool{"n1": true, "n2": false}}, nil)
	now := time.Now().UTC()
	if _, _, err := h.nodes.ApplyAgentUpdate(ctx, h.db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.nodes.ApplyAgentUpdate(ctx, h.db, nodes.AgentUpdate{
		NodeID: "n2", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	preview, err := h.service.Preview(ctx, "admin", "sg1")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.GatewayEligible || len(preview.EligibleNodeIDs) != 1 || preview.EligibleNodeIDs[0] != "n1" {
		t.Fatalf("preview = %+v", preview)
	}
	if len(preview.ExcludedNodes) != 1 || preview.ExcludedNodes[0].NodeID != "n2" || preview.ExcludedNodes[0].Reason != "unhealthy" {
		t.Fatalf("excluded = %+v", preview.ExcludedNodes)
	}
}

func TestDeleteRequiresGuardAndRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, nil)
	if _, err := h.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Delete(ctx, "admin", "req-2", "sg1", 1); !errors.Is(err, servicegroups.ErrDeleteGuardUnavailable) {
		t.Fatalf("nil guard err = %v", err)
	}
	blocked := newFixture(t, nil, nil, guardFunc(func(context.Context, string) error { return errors.New("referenced by gateway") }))
	if _, err := blocked.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	if err := blocked.service.Delete(ctx, "admin", "req-2", "sg1", 1); err == nil {
		t.Fatal("guard error did not block delete")
	}
	allowed := newFixture(t, nil, nil, guardFunc(func(context.Context, string) error { return nil }))
	if _, err := allowed.service.Create(ctx, "admin", "req-1", validInput("sg1", "Web")); err != nil {
		t.Fatal(err)
	}
	if err := allowed.service.Delete(ctx, "admin", "req-2", "sg1", 2); !errors.Is(err, servicegroups.ErrRevisionConflict) {
		t.Fatalf("stale delete err = %v", err)
	}
	if err := allowed.service.Delete(ctx, "admin", "req-2", "sg1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := allowed.repo.Get(ctx, "sg1"); !errors.Is(err, servicegroups.ErrNotFound) {
		t.Fatalf("group still present: %v", err)
	}
}
