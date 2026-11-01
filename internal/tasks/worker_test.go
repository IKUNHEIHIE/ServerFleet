package tasks_test

import (
	"context"
	"serverfleet.local/serverfleet/internal/tasks"
	"sync/atomic"
	"testing"
	"time"
)

type testTaskWorker func(context.Context, tasks.Task) (map[string]tasks.WorkerResult, error)

func (f testTaskWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	return f(ctx, t)
}
func TestDurableUnknownResultsTerminateWithoutReplayingBatch(t *testing.T) {
	f := newFixture(t, "n1", "n2")
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	f.run(t)
	for _, node := range []string{"n1", "n2"} {
		if _, err := f.service.RecordResult(context.Background(), task.ID, node, tasks.NodeResultUnknown, "uncertain effect"); err != nil {
			t.Fatal(err)
		}
	}
	if f.get(t, task.ID).Status != tasks.StatusUnknown {
		t.Fatal("terminal unknown targets left parent running")
	}
	f.run(t)
	if len(f.dispatcher.commands()) != 2 {
		t.Fatal("unknown replayed")
	}
}
func TestRegisteredWorkerReservesBeforeEffectsAndNeverUsesDispatcher(t *testing.T) {
	f := newFixture(t, "n1", "n2")
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := testTaskWorker(func(ctx context.Context, frozen tasks.Task) (map[string]tasks.WorkerResult, error) {
		calls.Add(1)
		persisted, err := f.repo.Get(ctx, frozen.ID)
		if err != nil || persisted.Status != tasks.StatusRunning || persisted.Targets[0].State != tasks.TargetDispatched {
			t.Error("worker ran before durable parent reservation")
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return map[string]tasks.WorkerResult{"n1": {Code: tasks.NodeResultUnknown}, "n2": {Code: tasks.NodeResultUnknown}}, nil
	})
	service, err := tasks.NewService(tasks.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, Nodes: f.nodes, Dispatcher: f.dispatcher, Workers: map[string]tasks.Worker{"host.patch": worker}})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(f.dispatcher.commands()) != 0 {
		t.Fatal("duplicate dispatcher/worker")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for f.get(t, task.ID).Status != tasks.StatusUnknown {
		if time.Now().After(deadline) {
			t.Fatal("worker did not terminate")
		}
		time.Sleep(time.Millisecond)
	}
	// A reconstructed service never replays a persisted dispatch after restart.
	second := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1", "n2"}})
	for _, node := range []string{"n1", "n2"} {
		if _, err = f.repo.MarkDispatched(ctx, f.db, second.ID, node); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.repo.SetStarted(ctx, f.db, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.Transition(ctx, f.db, second.ID, tasks.StatusQueued, tasks.StatusRunning, "", time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	restarted, err := tasks.NewService(tasks.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, Nodes: f.nodes, Dispatcher: f.dispatcher, Workers: map[string]tasks.Worker{"host.patch": worker}})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Get(ctx, second.ID)
	if err != nil || current.Status != tasks.StatusUnknown || calls.Load() != 1 {
		t.Fatal("restart replayed reserved worker", current.Status, err)
	}
}
