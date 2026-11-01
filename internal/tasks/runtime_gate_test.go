package tasks_test

import (
	"context"
	"errors"
	"serverfleet.local/serverfleet/internal/tasks"
	"testing"
)

func TestRuntimeFenceStopsActualSchedulerDispatch(t *testing.T) {
	f := newFixture(t, "n1")
	denied := errors.New("restored controller quarantined")
	service, err := tasks.NewService(tasks.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, Nodes: f.nodes, Dispatcher: f.dispatcher, RuntimeGate: func(context.Context) error { return denied }})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	if err = service.RunOnce(context.Background()); !errors.Is(err, denied) || len(f.dispatcher.commands()) != 0 {
		t.Fatal("quarantine dispatched", err)
	}
	if current := f.get(t, task.ID); current.Status != tasks.StatusQueued {
		t.Fatal("quarantine mutated pending task", current.Status)
	}
}
