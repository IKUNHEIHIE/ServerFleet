package tasks_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

type switchAuthorizer struct {
	mu   sync.Mutex
	deny bool
}

func (a *switchAuthorizer) Authorize(context.Context, auth.Check) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deny {
		return auth.ErrDenied
	}
	return nil
}

func (a *switchAuthorizer) setDeny(value bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deny = value
}

type fakeDispatcher struct {
	mu         sync.Mutex
	result     tasks.DispatchResult
	err        error
	dispatched []tasks.Command
	cancels    []tasks.CancelCommand
}

func newDispatcher() *fakeDispatcher {
	return &fakeDispatcher{result: tasks.DispatchResult{Accepted: true}}
}

func (d *fakeDispatcher) Dispatch(_ context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched = append(d.dispatched, command)
	return d.result, d.err
}

func (d *fakeDispatcher) Cancel(_ context.Context, command tasks.CancelCommand) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancels = append(d.cancels, command)
	return nil
}

func (d *fakeDispatcher) setResult(result tasks.DispatchResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.result = result
}

func (d *fakeDispatcher) commands() []tasks.Command {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]tasks.Command(nil), d.dispatched...)
}

func (d *fakeDispatcher) cancelCommands() []tasks.CancelCommand {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]tasks.CancelCommand(nil), d.cancels...)
}

type fakeNodes struct {
	mu     sync.Mutex
	online map[string]bool
	err    error
}

func onlineNodes(ids ...string) *fakeNodes {
	nodes := &fakeNodes{online: map[string]bool{}}
	for _, id := range ids {
		nodes.online[id] = true
	}
	return nodes
}

func (n *fakeNodes) Online(_ context.Context, nodeID string) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return false, n.err
	}
	return n.online[nodeID], nil
}

type fixture struct {
	db         *storage.DB
	repo       *tasks.Repo
	audit      *audit.SQLRecorder
	dispatcher *fakeDispatcher
	authorizer *switchAuthorizer
	nodes      *fakeNodes
	clock      *fakeClock
	service    *tasks.Service
}

func newFixture(t *testing.T, nodeIDs ...string) *fixture {
	t.Helper()
	if len(nodeIDs) == 0 {
		nodeIDs = []string{"n1", "n2", "n3", "n4", "n5", "n6"}
	}
	ctx := context.Background()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations())
	repo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	actions := append(tasks.ServiceActions(), "host.patch", "host.root")
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Now().UTC()}
	dispatcher := newDispatcher()
	authorizer := &switchAuthorizer{}
	nodeLookup := onlineNodes(nodeIDs...)
	service, err := tasks.NewService(tasks.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		Dispatcher: dispatcher, Nodes: nodeLookup, Now: clock.Now,
		ApprovalTTL: 30 * time.Minute, UnstartedTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ctx
	return &fixture{
		db: db, repo: repo, audit: recorder, dispatcher: dispatcher,
		authorizer: authorizer, nodes: nodeLookup, clock: clock, service: service,
	}
}

func (f *fixture) submit(t *testing.T, request tasks.SubmitRequest) tasks.Task {
	t.Helper()
	if request.SubjectID == "" {
		request.SubjectID = "user-1"
	}
	if request.RequestID == "" {
		request.RequestID = "req-submit"
	}
	if request.Name == "" {
		request.Name = "test task"
	}
	if request.ActionID == "" {
		request.ActionID = "host.patch"
	}
	if !request.OperationClass.Valid() {
		request.OperationClass = tasks.OperationClassStandard
	}
	task, err := f.service.Submit(context.Background(), request)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return task
}

func (f *fixture) run(t *testing.T) {
	t.Helper()
	if err := f.service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (f *fixture) get(t *testing.T, id string) tasks.Task {
	t.Helper()
	task, err := f.service.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return task
}

func TestSubmitPersistsAndAudits(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	if task.Status != tasks.StatusQueued || task.Revision != 1 || len(task.Targets) != 2 {
		t.Fatalf("task = %+v", task)
	}
	if task.PlanHash == "" {
		t.Fatal("plan hash is empty")
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: tasks.ActionSubmit})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "user-1" {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	events, err := f.service.Events(context.Background(), task.ID, 0, 100)
	if err != nil || len(events) != 1 || events[0].Type != "created" {
		t.Fatalf("events = %+v, %v", events, err)
	}
}

func TestSubmitIdempotency(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	first := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}, IdempotencyKey: "once"})
	second := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}, IdempotencyKey: "once"})
	if first.ID != second.ID {
		t.Fatalf("idempotent submit created two tasks: %s %s", first.ID, second.ID)
	}
	var count int
	if err := f.db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task count = %d", count)
	}
}

func TestSubmitRejectsSecretParams(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	if _, err := f.service.Submit(context.Background(), tasks.SubmitRequest{
		SubjectID: "user-1", RequestID: "req", Name: "x", ActionID: "host.patch",
		OperationClass: tasks.OperationClassStandard, TargetNodeIDs: []string{"n1"},
		Params: map[string]string{"password": "hunter2"},
	}); err == nil {
		t.Fatal("accepted secret-bearing params")
	}
}

func TestStandardTaskDispatchAndComplete(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	f.run(t)

	running := f.get(t, task.ID)
	if running.Status != tasks.StatusRunning || running.StartedAt.IsZero() {
		t.Fatalf("running = %+v", running)
	}
	commands := f.dispatcher.commands()
	if len(commands) != 2 || commands[0].ActionID != "host.patch" {
		t.Fatalf("commands = %+v", commands)
	}
	for _, target := range running.Targets {
		if target.State != tasks.TargetDispatched {
			t.Fatalf("target state = %s", target.State)
		}
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	completed, err := f.service.RecordResult(context.Background(), task.ID, "n2", tasks.NodeResultSucceeded, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != tasks.StatusCompleted || completed.FinishedAt.IsZero() {
		t.Fatalf("completed = %+v", completed)
	}
	events, err := f.service.Events(context.Background(), task.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 6 {
		t.Fatalf("events = %+v", events)
	}
	cursor := events[len(events)-1].Seq
	more, err := f.service.Events(context.Background(), task.ID, cursor, 100)
	if err != nil || len(more) != 0 {
		t.Fatalf("cursor events = %+v, %v", more, err)
	}
	logs, err := f.service.Logs(context.Background(), task.ID, 0, 100)
	if err != nil || len(logs) != 2 {
		t.Fatalf("logs = %+v, %v", logs, err)
	}
}

func TestRootTaskCanaryAndApproval(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2", "n3")
	task := f.submit(t, tasks.SubmitRequest{
		OperationClass: tasks.OperationClassRoot, ActionID: "host.root",
		TargetNodeIDs: []string{"n1", "n2", "n3"},
	})
	if task.Status != tasks.StatusAwaitingApproval || !task.ApprovalRequired {
		t.Fatalf("task = %+v", task)
	}
	f.run(t)
	if len(f.dispatcher.commands()) != 0 {
		t.Fatal("unapproved root task dispatched")
	}
	if _, err := f.service.Decide(context.Background(), task.ID, 1, tasks.Decision{
		ApproverID: "user-1", RequestID: "req-self", Approve: true,
	}); err != tasks.ErrSelfApproval {
		t.Fatalf("self approval err = %v", err)
	}
	if _, err := f.service.Decide(context.Background(), task.ID, 1, tasks.Decision{
		ApproverID: "admin-2", RequestID: "req-approve", Approve: true,
	}); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	first := f.dispatcher.commands()
	if len(first) != 1 || first[0].NodeID != "n1" {
		t.Fatalf("canary batch = %+v task=%+v", first, f.get(t, task.ID))
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	second := f.dispatcher.commands()
	if len(second) != 3 {
		t.Fatalf("second batch = %+v", second)
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n2", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	final, err := f.service.RecordResult(context.Background(), task.ID, "n3", tasks.NodeResultSucceeded, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("final = %+v", final)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: tasks.ActionApprove})
	if err != nil || page.Total != 1 || page.Items[0].Result != "allowed" {
		t.Fatalf("approval audit = %+v, %v", page, err)
	}
}

func TestReviseInvalidatesApproval(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{
		OperationClass: tasks.OperationClassRoot, ActionID: "host.root",
		TargetNodeIDs: []string{"n1", "n2"},
	})
	if _, err := f.service.Decide(context.Background(), task.ID, 1, tasks.Decision{
		ApproverID: "admin-2", RequestID: "req-approve", Approve: true,
	}); err != nil {
		t.Fatal(err)
	}
	revised, err := f.service.Revise(context.Background(), task.ID, tasks.ReviseRequest{
		SubjectID: "user-1", RequestID: "req-revise", ExpectedRevision: 1,
		TargetNodeIDs: []string{"n1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 2 || revised.Status != tasks.StatusAwaitingApproval || len(revised.Targets) != 1 {
		t.Fatalf("revised = %+v", revised)
	}
	if _, err := f.service.Decide(context.Background(), task.ID, 1, tasks.Decision{
		ApproverID: "admin-2", RequestID: "req-stale", Approve: true,
	}); err != tasks.ErrApprovalInvalid {
		t.Fatalf("stale approval err = %v", err)
	}
	if _, err := f.service.Decide(context.Background(), task.ID, 2, tasks.Decision{
		ApproverID: "admin-2", RequestID: "req-fresh", Approve: true,
	}); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	commands := f.dispatcher.commands()
	if len(commands) != 1 || commands[0].NodeID != "n1" {
		t.Fatalf("revised dispatch = %+v", commands)
	}
}

func TestDispatchDeniedAfterRevocationFailsTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	f.authorizer.setDeny(true)
	f.run(t)
	if len(f.dispatcher.commands()) != 0 {
		t.Fatal("revoked task still dispatched")
	}
	failed := f.get(t, task.ID)
	if failed.Status != tasks.StatusFailed || failed.FailureReason != "authorization_revoked" {
		t.Fatalf("failed = %+v", failed)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: "host.patch", Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestCancelBeforeAndAfterDispatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	first := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}, Name: "pre"})
	canceled, err := f.service.Cancel(context.Background(), first.ID, "user-1", "req-cancel")
	if err != nil || canceled.Status != tasks.StatusCanceled {
		t.Fatalf("cancel before dispatch = %+v, %v", canceled, err)
	}
	if len(f.dispatcher.commands()) != 0 {
		t.Fatal("canceled task dispatched")
	}

	second := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}, Name: "post"})
	f.run(t)
	cancelling, err := f.service.Cancel(context.Background(), second.ID, "user-1", "req-cancel")
	if err != nil || cancelling.Status != tasks.StatusCancelling {
		t.Fatalf("cancel after dispatch = %+v, %v", cancelling, err)
	}
	if len(f.dispatcher.cancelCommands()) != 2 {
		t.Fatalf("cancel commands = %+v", f.dispatcher.cancelCommands())
	}
	if _, err := f.service.RecordResult(context.Background(), second.ID, "n1", tasks.NodeResultCanceled, "stopped"); err != nil {
		t.Fatal(err)
	}
	final, err := f.service.RecordResult(context.Background(), second.ID, "n2", tasks.NodeResultCanceled, "stopped")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != tasks.StatusCanceled {
		t.Fatalf("final = %+v", final)
	}
}
