package tasks_test

import (
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/tasks"
)

func TestSupervisorAndSchedulerApprovalBindingCannotOutliveOrMoveDecision(t *testing.T) {
	now := time.Now().UTC()
	task := tasks.Task{ID: "task", Revision: 2, PlanHash: "frozen-plan", SubmitterID: "operator"}
	approval := tasks.Approval{TaskID: task.ID, Revision: task.Revision, PlanHash: task.PlanHash, ApproverID: "independent-admin", Decision: "approved", DecidedAt: now.Add(-time.Minute)}
	if !approval.CurrentFor(task, now, tasks.DefaultApprovalTTL) {
		t.Fatal("current independently approved plan rejected")
	}
	for _, change := range []func(*tasks.Approval){
		func(a *tasks.Approval) { a.DecidedAt = now.Add(-tasks.DefaultApprovalTTL - time.Second) },
		func(a *tasks.Approval) { a.DecidedAt = now.Add(time.Second) },
		func(a *tasks.Approval) { a.DecidedAt = time.Time{} },
		func(a *tasks.Approval) { a.ApproverID = task.SubmitterID },
		func(a *tasks.Approval) { a.ApproverID = "" },
		func(a *tasks.Approval) { a.TaskID = "different-task" },
		func(a *tasks.Approval) { a.Revision-- },
		func(a *tasks.Approval) { a.PlanHash = "changed-plan" },
		func(a *tasks.Approval) { a.Decision = "rejected" },
	} {
		a := approval
		change(&a)
		if a.CurrentFor(task, now, tasks.DefaultApprovalTTL) {
			t.Fatalf("unsafe offline approval accepted: %+v", a)
		}
	}
}
