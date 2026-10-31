package services_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, auth.Check) error { return nil }

type recordingDispatcher struct {
	mu       sync.Mutex
	commands []tasks.Command
}

func (d *recordingDispatcher) Dispatch(_ context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, command)
	return tasks.DispatchResult{Accepted: true}, nil
}

func (d *recordingDispatcher) Cancel(context.Context, tasks.CancelCommand) error { return nil }

type onlineLookup struct{}

func (onlineLookup) Online(context.Context, string) (bool, error) { return true, nil }

// TestAuthorizedApprovalTaskPipeline drives the real nodes, tasks and audit
// persistence with a test-only identity/transport boundary. It is a backend
// simulation, not a real Agent or Controller end-to-end run.
func TestAuthorizedApprovalTaskPipeline(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBAt(t, t.TempDir()+"/pipeline.db", v09Migrations()...)
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	actions := append(append(nodes.ServiceActions(), tasks.ServiceActions()...), "host.root")
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	nodeRepo, _ := nodes.NewRepo(db)
	groupRepo, _ := nodes.NewGroupRepo(db)
	policy := &auth.NodePolicy{
		KnownNodes: map[string]struct{}{"n1": {}},
		ReadGrants: map[string]map[string]struct{}{"user-1": {"n1": {}}},
	}
	readPolicy := func(context.Context) (*auth.NodePolicy, error) { return policy, nil }
	nodeService, err := nodes.NewService(nodes.Options{
		DB: db, Nodes: nodeRepo, Groups: groupRepo, Audit: recorder,
		Authorizer: allowAuthorizer{}, ReadPolicy: readPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &recordingDispatcher{}
	taskRepo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	taskService, err := tasks.NewService(tasks.Options{
		DB: db, Repo: taskRepo, Audit: recorder, Authorizer: allowAuthorizer{},
		Dispatcher: dispatcher, Nodes: onlineLookup{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodeService.CreateNode(ctx, "user-1", "req-node", nodes.NodeCreateInput{
		ID: "n1", Name: "Node One", Status: nodes.StatusOffline,
	}); err != nil {
		t.Fatal(err)
	}
	task, err := taskService.Submit(ctx, tasks.SubmitRequest{
		SubjectID: "user-1", RequestID: "req-task", Name: "root patch", ActionID: "host.root",
		OperationClass: tasks.OperationClassRoot, TargetNodeIDs: []string{"n1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusAwaitingApproval {
		t.Fatalf("initial status = %s", task.Status)
	}
	if err := taskService.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dispatcher.commands) != 0 {
		t.Fatal("unapproved task dispatched")
	}
	if _, err := taskService.Decide(ctx, task.ID, 1, tasks.Decision{
		ApproverID: "admin-2", RequestID: "req-approve", Approve: true, Reason: "reviewed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := taskService.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dispatcher.commands) != 1 {
		t.Fatalf("commands = %d", len(dispatcher.commands))
	}
	final, err := taskService.RecordResult(ctx, task.ID, "n1", tasks.NodeResultSucceeded, "patched")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("final = %s", final.Status)
	}
	auditPage, err := recorder.Query(ctx, audit.Query{TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if auditPage.Total < 2 {
		t.Fatalf("task audit rows = %d", auditPage.Total)
	}
	submits, err := recorder.Query(ctx, audit.Query{Action: tasks.ActionSubmit})
	if err != nil || submits.Total != 1 {
		t.Fatalf("submit audit = %+v, %v", submits, err)
	}
	approvals, err := recorder.Query(ctx, audit.Query{Action: tasks.ActionApprove, Result: "allowed"})
	if err != nil || approvals.Total != 1 {
		t.Fatalf("approval audit = %+v, %v", approvals, err)
	}
	eventList, err := taskService.Events(ctx, task.ID, 0, 100)
	if err != nil || len(eventList) < 4 {
		t.Fatalf("events = %+v, %v", eventList, err)
	}
}
