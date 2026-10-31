package tasks_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
)

func TestUnknownDispatchIsTerminalAndNotRetried(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	f.dispatcher.setResult(tasks.DispatchResult{Unknown: true, Reason: "write timed out"})
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	f.run(t)
	final := f.get(t, task.ID)
	if final.Status != tasks.StatusUnknown {
		t.Fatalf("status = %s", final.Status)
	}
	before := len(f.dispatcher.commands())
	if before != 1 {
		t.Fatalf("commands = %d", before)
	}
	f.run(t)
	if len(f.dispatcher.commands()) != before {
		t.Fatal("unknown task was retried")
	}
}

func TestOfflineWaitExpiresAfterTTL(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	f.nodes.online["n1"] = false
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	f.run(t)
	if f.get(t, task.ID).Status != tasks.StatusQueued {
		t.Fatalf("status = %s", f.get(t, task.ID).Status)
	}
	if len(f.dispatcher.commands()) != 0 {
		t.Fatal("offline node was dispatched")
	}
	f.clock.Advance(16 * time.Minute)
	f.run(t)
	if f.get(t, task.ID).Status != tasks.StatusExpired {
		t.Fatalf("status = %s", f.get(t, task.ID).Status)
	}
}

func TestRecoveryMarksInFlightUnknownWithoutRetry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	f.run(t)
	if f.get(t, task.ID).Status != tasks.StatusRunning {
		t.Fatal("task did not start")
	}
	secondDispatcher := newDispatcher()
	second, err := tasks.NewService(tasks.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer,
		Dispatcher: secondDispatcher, Nodes: f.nodes, Now: f.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := second.Recover(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, %v", recovered, err)
	}
	if secondDispatcher.commands() != nil && len(secondDispatcher.commands()) != 0 {
		t.Fatal("recovery re-dispatched")
	}
	if err := second.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(secondDispatcher.commands()) != 0 {
		t.Fatal("recovered task was retried")
	}
	recoveredTask, err := second.Get(context.Background(), task.ID)
	if err != nil || recoveredTask.Status != tasks.StatusUnknown {
		t.Fatalf("recovered task = %+v, %v", recoveredTask, err)
	}
}

func TestBatchFailureBlocksLaterBatches(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2", "n3", "n4", "n5", "n6")
	task := f.submit(t, tasks.SubmitRequest{
		TargetNodeIDs: []string{"n1", "n2", "n3", "n4", "n5", "n6"},
	})
	f.run(t)
	if len(f.dispatcher.commands()) != 5 {
		t.Fatalf("first batch commands = %d", len(f.dispatcher.commands()))
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultFailed, "patch failed"); err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []string{"n2", "n3", "n4", "n5"} {
		if _, err := f.service.RecordResult(context.Background(), task.ID, nodeID, tasks.NodeResultSucceeded, "ok"); err != nil {
			t.Fatal(err)
		}
	}
	final := f.get(t, task.ID)
	if final.Status != tasks.StatusPartialFailure {
		t.Fatalf("status = %s", final.Status)
	}
	for _, target := range final.Targets {
		if target.NodeID == "n6" && target.State != tasks.TargetCanceled {
			t.Fatalf("later batch target = %s", target.State)
		}
	}
	before := len(f.dispatcher.commands())
	f.run(t)
	if len(f.dispatcher.commands()) != before {
		t.Fatal("blocked task dispatched a later batch")
	}
}

func TestDuplicateResultIsIdempotentButConflictingIsRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	f.run(t)
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatalf("duplicate result: %v", err)
	}
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultFailed, "late failure"); !errors.Is(err, tasks.ErrDuplicateResult) {
		t.Fatalf("conflicting result err = %v", err)
	}
	logs, err := f.service.Logs(context.Background(), task.ID, 0, 100)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %+v, %v", logs, err)
	}
}

func TestLogRedaction(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"password=hunter2",
		"token: abc123",
		"Authorization: Bearer xyz.abc",
		"curl --password hunter2 https://example.com",
	} {
		output := tasks.Redact(input)
		for _, secret := range []string{"hunter2", "abc123", "xyz.abc"} {
			if strings.Contains(output, secret) {
				t.Fatalf("secret %q survived redaction of %q -> %q", secret, input, output)
			}
		}
	}
	if got := tasks.Redact("line\r\nnext"); strings.ContainsAny(got, "\r") {
		t.Fatalf("control characters kept: %q", got)
	}
}

func TestSensitiveResultMessageIsRedactedBeforeStorage(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	f.run(t)
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultFailed, "password=hunter2 failed"); err != nil {
		t.Fatal(err)
	}
	logs, err := f.service.Logs(context.Background(), task.ID, 0, 100)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %+v, %v", logs, err)
	}
	if strings.Contains(logs[0].Message, "hunter2") {
		t.Fatalf("stored log leaked secret: %q", logs[0].Message)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: tasks.ActionSubmit})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestConcurrentRunOnceDispatchesOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.service.RunOnce(context.Background())
		}()
	}
	wg.Wait()
	if got := len(f.dispatcher.commands()); got != 2 {
		t.Fatalf("dispatched %d commands, want 2", got)
	}
	if f.get(t, task.ID).Status != tasks.StatusRunning {
		t.Fatalf("status = %s", f.get(t, task.ID).Status)
	}
}

func TestApprovalExpiryIsRecheckedBeforeLaterBatch(t *testing.T) {
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
	f.run(t)
	if _, err := f.service.RecordResult(context.Background(), task.ID, "n1", tasks.NodeResultSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(31 * time.Minute)
	f.run(t)
	final := f.get(t, task.ID)
	if final.Status != tasks.StatusFailed || final.FailureReason != "approval_invalid" {
		t.Fatalf("final = %+v", final)
	}
	if got := len(f.dispatcher.commands()); got != 1 {
		t.Fatalf("second batch dispatched after approval expiry: %d", got)
	}
}
