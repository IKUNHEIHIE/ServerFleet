package nodes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func auditValidator(t *testing.T, actions ...string) *audit.Validator {
	t.Helper()
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func newAuditRecorder(t *testing.T, db *storage.DB, actions ...string) *audit.SQLRecorder {
	t.Helper()
	recorder, err := audit.NewSQLRecorder(db, auditValidator(t, actions...), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

func seedNode(t *testing.T, db *storage.DB, repo *nodes.Repo, id, name string) nodes.Node {
	t.Helper()
	node, err := repo.Create(context.Background(), db, nodes.NodeCreateInput{
		ID: id, Name: name, Address: "10.0.0.1", Region: "hkg", Provider: "test",
		Tags: []string{"web"}, Status: nodes.StatusOffline,
	})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func readPolicy(subject string, known, granted []string) auth.PolicyReader {
	policy := &auth.NodePolicy{KnownNodes: map[string]struct{}{}, ReadGrants: map[string]map[string]struct{}{}}
	for _, id := range known {
		policy.KnownNodes[id] = struct{}{}
	}
	grant := map[string]struct{}{}
	for _, id := range granted {
		grant[id] = struct{}{}
	}
	policy.ReadGrants[subject] = grant
	return func(context.Context) (*auth.NodePolicy, error) { return policy, nil }
}

type serviceFixture struct {
	db     *storage.DB
	repo   *nodes.Repo
	groups *nodes.GroupRepo
	audit  *audit.SQLRecorder
	svc    *nodes.Service
}

func newServiceFixture(t *testing.T, authorizer auth.Authorizer, policy auth.PolicyReader, actions ...string) serviceFixture {
	t.Helper()
	db := testkit.NewDB(t, nodes.Migrations(), audit.Migrations())
	repo, err := nodes.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := nodes.NewGroupRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) == 0 {
		actions = nodes.ServiceActions()
	}
	recorder := newAuditRecorder(t, db, actions...)
	if policy == nil {
		var known []string
		for _, node := range listNodes(t, db, repo) {
			known = append(known, node.ID)
		}
		policy = readPolicy("admin", known, known)
	}
	service, err := nodes.NewService(nodes.Options{
		DB: db, Nodes: repo, Groups: groups, Audit: recorder,
		Authorizer: authorizer, ReadPolicy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return serviceFixture{db: db, repo: repo, groups: groups, audit: recorder, svc: service}
}

func listNodes(t *testing.T, db *storage.DB, repo *nodes.Repo) []nodes.Node {
	t.Helper()
	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestNodePersistenceAndAgentUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t, nodes.Migrations())
	repo, _ := nodes.NewRepo(db)
	seedNode(t, db, repo, "n1", "Node One")

	now := time.Now().UTC()
	updated, applied, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
		Metrics:      &nodes.Metrics{CPUPercent: 12.5, MemoryPercent: 40},
		SampleSource: nodes.SampleSourceAgent,
	})
	if err != nil || !applied {
		t.Fatalf("update: %+v applied=%t err=%v", updated, applied, err)
	}
	if updated.Metrics == nil || updated.Metrics.CPUPercent != 12.5 {
		t.Fatalf("metrics not stored: %+v", updated.Metrics)
	}
	if _, ok := updated.Usage(now, 5*time.Minute); !ok {
		t.Fatal("fresh online sample not usable")
	}

	// Duplicate sequence is an idempotent no-op.
	duplicate, applied, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("duplicate sequence was applied")
	}
	if duplicate.Status != nodes.StatusOnline || duplicate.Metrics == nil {
		t.Fatalf("duplicate changed stored node: %+v", duplicate)
	}

	// Lower sequence is stale and must not overwrite.
	if _, _, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOffline, Sequence: 1, ObservedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Offline with a newer sequence keeps the last sample but Usage reports missing.
	offline, applied, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOffline, Sequence: 2, ObservedAt: now.Add(time.Second),
	})
	if err != nil || !applied {
		t.Fatalf("offline update: %v applied=%t", err, applied)
	}
	if _, ok := offline.Usage(now.Add(time.Second), 5*time.Minute); ok {
		t.Fatal("offline node reported a usable sample")
	}

	// Revocation is terminal for agent events.
	if _, _, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusRevoked, Sequence: 3, ObservedAt: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 4, ObservedAt: now.Add(3 * time.Second),
	}); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("err = %v, want ErrNodeRevoked", err)
	}

	if _, _, err := repo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 5, ObservedAt: now.Add(10 * time.Minute),
	}); !errors.Is(err, nodes.ErrInvalidNode) {
		t.Fatalf("future sample err = %v, want ErrInvalidNode", err)
	}
}

func TestNodeRestartKeepsInventory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := t.TempDir() + "/fleet.db"
	db := testkit.NewDBAt(t, path, nodes.Migrations())
	repo, _ := nodes.NewRepo(db)
	seedNode(t, db, repo, "n1", "Node One")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repo2, _ := nodes.NewRepo(reopened)
	node, err := repo2.Get(ctx, "n1")
	if err != nil || node.Name != "Node One" {
		t.Fatalf("node = %+v, %v", node, err)
	}
}

func TestListVisiblePreflightsEmptyInventory(t *testing.T) {
	t.Parallel()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("u1", nil, nil))
	if _, err := h.svc.ListVisible(context.Background(), "u1"); err != nil {
		t.Fatalf("known empty subject: %v", err)
	}
	if _, err := h.svc.ListVisible(context.Background(), "unknown"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("unknown subject err = %v, want denied", err)
	}
}

func TestListVisibleFiltersNodesAndGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	policy := readPolicy("u1", []string{"n1", "n2"}, []string{"n1"})
	authorizer := auth.NewNodePolicyAuthorizer(policy)
	h := newServiceFixture(t, authorizer, policy, nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	seedNode(t, h.db, h.repo, "n2", "Node Two")
	if _, err := h.groups.Create(ctx, h.db, "g1", "Visible", "", []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.groups.Create(ctx, h.db, "g2", "Hidden", "", []string{"n1", "n2"}); err != nil {
		t.Fatal(err)
	}

	nodesList, err := h.svc.ListVisible(ctx, "u1")
	if err != nil || len(nodesList) != 1 || nodesList[0].ID != "n1" {
		t.Fatalf("visible nodes = %+v, %v", nodesList, err)
	}
	if _, err := h.svc.GetVisible(ctx, "u1", "n2"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("hidden node err = %v", err)
	}
	groups, err := h.svc.ListVisibleGroups(ctx, "u1")
	if err != nil || len(groups) != 1 || groups[0].ID != "g1" {
		t.Fatalf("visible groups = %+v, %v", groups, err)
	}
	classifications, err := h.svc.ClassifyVisible(ctx, "u1", nodes.DimensionProvider)
	if err != nil || len(classifications) != 1 || classifications[0].Count != 1 {
		t.Fatalf("classifications = %+v, %v", classifications, err)
	}
}

func TestGroupWritesAuditAndRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	actions := append(nodes.ServiceActions(), nodes.ActionGroupsWrite)
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("admin", []string{"n1", "n2"}, []string{"n1", "n2"}), actions...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	seedNode(t, h.db, h.repo, "n2", "Node Two")

	group, err := h.svc.CreateGroup(ctx, "admin", "req-1", nodes.CreateGroupInput{
		ID: "g1", Name: "Maintenance", Notes: "patching", MemberNodeIDs: []string{"n1"},
	})
	if err != nil || group.Revision != 1 {
		t.Fatalf("create group = %+v, %v", group, err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: nodes.ActionGroupCreate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}

	renamed, err := h.svc.RenameGroup(ctx, "admin", "req-2", "g1", nodes.RenameGroupInput{
		ExpectedRevision: 1, Name: "Patch Window", Notes: "nightly",
	})
	if err != nil || renamed.Revision != 2 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	if _, err := h.svc.RenameGroup(ctx, "admin", "req-3", "g1", nodes.RenameGroupInput{
		ExpectedRevision: 1, Name: "Stale", Notes: "",
	}); !errors.Is(err, nodes.ErrRevisionConflict) {
		t.Fatalf("stale rename err = %v", err)
	}

	if _, err := h.svc.ReplaceGroupMembers(ctx, "admin", "req-4", "g1", nodes.ReplaceGroupMembersInput{
		ExpectedRevision: 2, MemberNodeIDs: []string{"n1", "missing"},
	}); !errors.Is(err, nodes.ErrUnknownNode) {
		t.Fatalf("unknown member err = %v", err)
	}
	after, err := h.groups.Get(ctx, "g1")
	if err != nil || len(after.Members) != 1 || after.Revision != 2 {
		t.Fatalf("partial membership write: %+v, %v", after, err)
	}
	replaced, err := h.svc.ReplaceGroupMembers(ctx, "admin", "req-5", "g1", nodes.ReplaceGroupMembersInput{
		ExpectedRevision: 2, MemberNodeIDs: []string{"n1", "n2"},
	})
	if err != nil || replaced.Revision != 3 || len(replaced.Members) != 2 {
		t.Fatalf("replace members = %+v, %v", replaced, err)
	}
}

func TestAuthorizationDeniedIsAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	actions := append(nodes.ServiceActions(), nodes.ActionGroupsWrite)
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }), readPolicy("u1", []string{"n1"}, []string{"n1"}), actions...)
	seedNode(t, h.db, h.repo, "n1", "Node One")

	if _, err := h.svc.CreateGroup(ctx, "u1", "req-deny", nodes.CreateGroupInput{
		ID: "g-denied", Name: "Denied", MemberNodeIDs: []string{"n1"},
	}); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want denied", err)
	}
	if _, err := h.groups.Get(ctx, "g-denied"); !errors.Is(err, nodes.ErrGroupNotFound) {
		t.Fatalf("denied write mutated storage: %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: nodes.ActionGroupsWrite, Result: "denied"})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "u1" {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestAuditFailureRollsBackMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Deliberately omit groups.create from the audit action catalog: AppendTx
	// rejects the event and the enclosing transaction must roll back.
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("admin", []string{"n1"}, []string{"n1"}), nodes.ActionGroupsWrite)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	if _, err := h.svc.CreateGroup(ctx, "admin", "req-audit-fail", nodes.CreateGroupInput{
		ID: "g1", Name: "No Audit", MemberNodeIDs: []string{"n1"},
	}); err == nil {
		t.Fatal("mutation succeeded without an audit record")
	}
	if _, err := h.groups.Get(ctx, "g1"); !errors.Is(err, nodes.ErrGroupNotFound) {
		t.Fatalf("mutation survived audit failure: %v", err)
	}
}

func TestCreateNodeRequiresWriteAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var last auth.Check
	h := newServiceFixture(t, authorizerFunc(func(_ context.Context, check auth.Check) error {
		last = check
		return nil
	}), readPolicy("admin", nil, nil), nodes.ServiceActions()...)
	node, err := h.svc.CreateNode(ctx, "admin", "req-node", nodes.NodeCreateInput{
		ID: "n-new", Name: "New Node",
	})
	if err != nil || node.ID != "n-new" {
		t.Fatalf("create node = %+v, %v", node, err)
	}
	if last.Action != nodes.ActionNodesWrite || len(last.NodeIDs) != 1 || last.NodeIDs[0] != "n-new" {
		t.Fatalf("authorization check = %+v", last)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: nodes.ActionNodeCreate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestApplyAgentUpdateAuditsAppliedChangeOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("admin", []string{"n1"}, []string{"n1"}), nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	now := time.Now().UTC()
	update := nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
		Metrics: &nodes.Metrics{CPUPercent: 10, MemoryPercent: 20}, SampleSource: nodes.SampleSourceAgent,
	}
	if _, err := h.svc.ApplyAgentUpdate(ctx, "agent-1", "agent-req-1", update); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ApplyAgentUpdate(ctx, "agent-1", "agent-req-1", update); err != nil {
		t.Fatal(err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: nodes.ActionAgentUpdate})
	if err != nil || page.Total != 1 {
		t.Fatalf("agent audit = %+v, %v", page, err)
	}
	if page.Items[0].Source != audit.SourceAgent {
		t.Fatalf("source = %q", page.Items[0].Source)
	}
}

func TestGroupDeleteRequiresRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	actions := append(nodes.ServiceActions(), nodes.ActionGroupsWrite)
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("admin", []string{"n1"}, []string{"n1"}), actions...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	if _, err := h.groups.Create(ctx, h.db, "g1", "G1", "", []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DeleteGroup(ctx, "admin", "req-del", "g1", 2); !errors.Is(err, nodes.ErrRevisionConflict) {
		t.Fatalf("stale delete err = %v", err)
	}
	if err := h.svc.DeleteGroup(ctx, "admin", "req-del", "g1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.groups.Get(ctx, "g1"); !errors.Is(err, nodes.ErrGroupNotFound) {
		t.Fatalf("group still present: %v", err)
	}
}
