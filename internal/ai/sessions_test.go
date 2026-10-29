package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type sessionTestStore struct {
	mu   sync.Mutex
	data []byte
	fail bool
}

func (s *sessionTestStore) View(ctx context.Context, fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(bytes.Clone(s.data))
}
func (s *sessionTestStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := fn(bytes.Clone(s.data))
	if err != nil {
		return err
	}
	if s.fail {
		return errors.New("failed commit")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.data = bytes.Clone(next)
	return nil
}
func sessionFixture(t *testing.T) (*SessionService, *sessionTestStore, *SessionActor, *CurrentAuthority, *time.Time) {
	t.Helper()
	store := &sessionTestStore{}
	actor := &SessionActor{SubjectID: "owner", HumanWeb: true, CanFullAccess: true}
	authority := &CurrentAuthority{SubjectID: actor.SubjectID, AllowedNodes: []string{"node-a", "node-b"}, AllowedTools: []string{"nodes.read", "host.modify", "shell.root"}, GroupMembers: map[string][]string{"group-a": {"node-a"}}}
	canonical := func(raw json.RawMessage) (json.RawMessage, error) {
		if string(raw) != "{}" {
			return nil, ErrToolDenied
		}
		return bytes.Clone(raw), nil
	}
	service, err := NewSessionService(store, func(context.Context, string, string) (SessionActor, error) { return *actor, nil }, func(context.Context, string, string) (CurrentAuthority, error) { return *authority, nil }, []RegisteredTool{{ToolDefinition{ID: "nodes.read", ReadOnly: true, AutoAllowed: true}, canonical}, {ToolDefinition{ID: "host.modify"}, canonical}, {ToolDefinition{ID: "shell.root", RootShell: true}, canonical}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	return service, store, actor, authority, &now
}
func TestPersistentAIScopePerActionGroupSnapshotAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, _, actor, current, _ := sessionFixture(t)
	session, err := s.Create(ctx)
	if err != nil || session.ScopeRevision != 1 || len(session.AllowedNodeIDs) != 0 || len(session.ResourceGrants) != 0 || session.ApprovalPolicy.Mode != "per_action" {
		t.Fatal(session, err)
	}
	call := ToolCall{ToolID: "nodes.read", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`), ScopeRevision: 1}
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrToolDenied {
		t.Fatal("new AI session inherited resources", err)
	}
	session, err = s.AddGrant(ctx, session.ID, 1, ResourceGrantInput{"service_group", "group-a", []string{"nodes.read"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = s.SetPolicy(ctx, session.ID, session.ScopeRevision, LowRisk, 0)
	if err != nil {
		t.Fatal(err)
	}
	call.ScopeRevision = session.ScopeRevision
	assessment, err := s.AssessTool(ctx, session.ID, call)
	if err != nil || assessment.RequiresApproval {
		t.Fatal(assessment, err)
	}
	call.ToolID = "host.modify"
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrToolDenied {
		t.Fatal("read scope inherited current user modification rights", err)
	}
	current.GroupMembers["group-a"] = []string{"node-a", "node-b"}
	call.ToolID = "nodes.read"
	call.NodeIDs = []string{"node-b"}
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrToolDenied {
		t.Fatal("new member inherited grant", err)
	}
	if _, err = s.AddGrant(ctx, session.ID, session.ScopeRevision, ResourceGrantInput{"service_group", "group-a", []string{"host.modify"}}); err != ErrScopeConflict {
		t.Fatal("different snapshots merged into cross-action authority", err)
	}
	current.GroupMembers["group-a"] = []string{"node-b"}
	call.NodeIDs = []string{"node-a"}
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrToolDenied {
		t.Fatal("removed member retained grant", err)
	}
	actor.HumanWeb = false
	if _, err = s.AddGrant(ctx, session.ID, session.ScopeRevision, ResourceGrantInput{"node", "node-b", []string{"host.modify"}}); err != ErrToolDenied {
		t.Fatal("model enlarged grants", err)
	}
	actor.HumanWeb = true
	actor.SubjectID = "other"
	if _, err = s.Get(ctx, session.ID); err != ErrToolDenied {
		t.Fatal("cross-subject session read", err)
	}
}
func TestAIScopePolicyExpiryRevisionFailureAndRootApproval(t *testing.T) {
	ctx := context.Background()
	s, store, actor, _, now := sessionFixture(t)
	session, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, err = s.AddGrant(ctx, session.ID, session.ScopeRevision, ResourceGrantInput{"node", "node-a", []string{"nodes.read", "shell.root"}})
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(store.data)
	store.fail = true
	if _, err = s.SetPolicy(ctx, session.ID, session.ScopeRevision, FullAccess, 15); err != ErrSessionUnavailable {
		t.Fatal(err)
	}
	store.fail = false
	if !bytes.Equal(before, store.data) {
		t.Fatal("failed commit changed policy")
	}
	if _, err = s.SetPolicy(ctx, session.ID, session.ScopeRevision, FullAccess, 10); err != ErrToolDenied {
		t.Fatal("unsupported confirmation duration", err)
	}
	actor.CanFullAccess = false
	if _, err = s.SetPolicy(ctx, session.ID, session.ScopeRevision, FullAccess, 15); err != ErrToolDenied {
		t.Fatal(err)
	}
	actor.CanFullAccess = true
	session, err = s.SetPolicy(ctx, session.ID, session.ScopeRevision, FullAccess, 15)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ToolID: "shell.root", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`), ScopeRevision: session.ScopeRevision}
	assessed, err := s.AssessTool(ctx, session.ID, call)
	if err != nil || !assessed.RequiresApproval {
		t.Fatal("full access bypassed root human approval", assessed, err)
	}
	call.ToolID = "nodes.read"
	assessed, err = s.AssessTool(ctx, session.ID, call)
	if err != nil || assessed.RequiresApproval {
		t.Fatal(assessed, err)
	}
	*now = now.Add(15 * time.Minute)
	assessed, err = s.AssessTool(ctx, session.ID, call)
	if err != nil || !assessed.RequiresApproval {
		t.Fatal("expired full mode did not restore every-call", assessed, err)
	}
	*now = now.Add(-time.Second)
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrSessionUnavailable {
		t.Fatal("clock rollback revived full mode", err)
	}
	*now = now.Add(time.Second)
	session, err = s.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err = s.RevokeGrant(ctx, session.ID, session.ResourceGrants[0].GrantID, session.ScopeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssessTool(ctx, session.ID, call); err != ErrToolDenied {
		t.Fatal("revoked grant/stale revision", err)
	}
	session, err = s.Close(ctx, session.ID, session.ScopeRevision)
	if err != nil || session.Status != "closed" {
		t.Fatal(session, err)
	}
	if _, err = s.AddGrant(ctx, session.ID, session.ScopeRevision, ResourceGrantInput{"node", "node-a", []string{"nodes.read"}}); err != ErrToolDenied {
		t.Fatal("closed session reopened", err)
	}
}
