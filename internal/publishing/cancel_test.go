package publishing_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/publishing"
)

func TestCancelQueuedPlanIsIdempotent(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	plan, err := f.service.Cancel(context.Background(), "admin", "req-cancel", "plan-1")
	if err != nil || plan.Status != publishing.PlanCanceled {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
	again, err := f.service.Cancel(context.Background(), "admin", "req-cancel-2", "plan-1")
	if err != nil || again.Status != publishing.PlanCanceled {
		t.Fatalf("idempotent cancel = %+v, err = %v", again, err)
	}
	if _, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrPlanCanceled) {
		t.Fatalf("run canceled err = %v", err)
	}
}

func TestCancelRejectsExecutedOrCompletedPlan(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.set(publishing.StageResult{}, errors.New("certificate failed"))
	if _, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("run err = %v", err)
	}
	if _, err := f.service.Cancel(context.Background(), "admin", "req-cancel", "plan-1"); !errors.Is(err, publishing.ErrPlanNotRunnable) {
		t.Fatalf("executed cancel err = %v", err)
	}
	completed := newPublicationFixture(t)
	completed.createPlan(t, "plan-2", "shop.example.com", "")
	if _, err := completed.service.Run(context.Background(), "admin", "req-run", "plan-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := completed.service.Cancel(context.Background(), "admin", "req-cancel", "plan-2"); !errors.Is(err, publishing.ErrPlanCompleted) {
		t.Fatalf("completed cancel err = %v", err)
	}
}

func TestCancelRequiresCurrentPermission(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.authorizer.deny = true
	if _, err := f.service.Cancel(context.Background(), "admin", "req-cancel", "plan-1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("cancel denial err = %v", err)
	}
	plan, _ := f.plan(t, "plan-1")
	if plan.Status != publishing.PlanQueued {
		t.Fatalf("denied cancel changed plan = %+v", plan)
	}
}
