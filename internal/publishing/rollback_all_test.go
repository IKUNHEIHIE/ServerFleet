package publishing_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/publishing"
)

func TestRollbackAllCompensatesInReverseOrder(t *testing.T) {
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	plan, err := f.service.RollbackAll(ctx, "admin", "req-rollback-all", "plan-1")
	if err != nil {
		t.Fatalf("RollbackAll: %v", err)
	}
	if plan.Status != publishing.PlanCanceled {
		t.Fatalf("plan = %+v", plan)
	}
	order := f.ports.rollbackLog.snapshot()
	if len(order) != 3 || order[0] != publishing.StageGateway || order[1] != publishing.StageCertificate || order[2] != publishing.StageDNS {
		t.Fatalf("rollback order = %v", order)
	}
	_, stages := f.plan(t, "plan-1")
	for _, stage := range stages {
		if stage.Status != publishing.StageRolledBack {
			t.Fatalf("stage = %+v", stage)
		}
		if stage.ResultSummary == "" {
			t.Fatalf("rollback rewrote summary: %+v", stage)
		}
	}
	for name, stage := range map[string]*fakeStage{
		"dns":  f.ports.dns.fakeStage,
		"cert": f.ports.cert.fakeStage,
		"gw":   f.ports.gateway.fakeStage,
	} {
		publishes, rollbacks := stage.counts()
		if publishes != 1 || rollbacks != 1 {
			t.Fatalf("%s publishes/rollbacks = %d/%d", name, publishes, rollbacks)
		}
	}
	if got := f.auditCount(t, publishing.ActionRollbackAll, "success"); got != 1 {
		t.Fatalf("success audit = %d", got)
	}
	if _, err := f.service.RollbackAll(ctx, "admin", "req-rollback-again", "plan-1"); err != nil {
		t.Fatalf("idempotent RollbackAll: %v", err)
	}
	if len(f.ports.rollbackLog.snapshot()) != 3 {
		t.Fatal("idempotent RollbackAll repeated compensation")
	}
	if _, err := f.service.Run(ctx, "admin", "req-run-canceled", "plan-1"); !errors.Is(err, publishing.ErrPlanCanceled) {
		t.Fatalf("run after rollback = %v", err)
	}
}

func TestRollbackAllStopsAtFirstFailureAndResumes(t *testing.T) {
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.ports.cert.setRollbackError(errors.New("certificate compensation failed"))
	plan, err := f.service.RollbackAll(ctx, "admin", "req-rollback-all", "plan-1")
	if !errors.Is(err, publishing.ErrRollbackFailed) {
		t.Fatalf("err = %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure || plan.CurrentStage != publishing.StageCertificate {
		t.Fatalf("plan = %+v", plan)
	}
	_, stages := f.plan(t, "plan-1")
	if stageByKind(t, stages, publishing.StageGateway).Status != publishing.StageRolledBack {
		t.Fatal("gateway was not compensated first")
	}
	if stageByKind(t, stages, publishing.StageCertificate).Status != publishing.StageSucceeded {
		t.Fatal("failed compensation changed the certificate stage")
	}
	if stageByKind(t, stages, publishing.StageDNS).Status != publishing.StageSucceeded {
		t.Fatal("DNS was compensated while a downstream stage remained live")
	}
	if order := f.ports.rollbackLog.snapshot(); len(order) != 2 || order[0] != publishing.StageGateway || order[1] != publishing.StageCertificate {
		t.Fatalf("rollback order = %v", order)
	}
	if _, rollbacks := f.ports.dns.counts(); rollbacks != 0 {
		t.Fatalf("dns rollbacks = %d", rollbacks)
	}
	if got := f.auditCount(t, publishing.ActionRollbackAll, "failure"); got != 1 {
		t.Fatalf("failure audit = %d", got)
	}

	f.ports.cert.setRollbackError(nil)
	plan, err = f.service.RollbackAll(ctx, "admin", "req-rollback-resume", "plan-1")
	if err != nil || plan.Status != publishing.PlanCanceled {
		t.Fatalf("resumed plan = %+v, err = %v", plan, err)
	}
	order := f.ports.rollbackLog.snapshot()
	if len(order) != 4 || order[2] != publishing.StageCertificate || order[3] != publishing.StageDNS {
		t.Fatalf("resumed order = %v", order)
	}
}

func TestRollbackAllPartialPlanUsesReverseExecutedOrder(t *testing.T) {
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.set(publishing.StageResult{}, errors.New("certificate failed"))
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("Run err = %v", err)
	}
	plan, err := f.service.RollbackAll(ctx, "admin", "req-rollback-all", "plan-1")
	if err != nil || plan.Status != publishing.PlanCanceled {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
	order := f.ports.rollbackLog.snapshot()
	if len(order) != 2 || order[0] != publishing.StageCertificate || order[1] != publishing.StageDNS {
		t.Fatalf("rollback order = %v", order)
	}
	_, stages := f.plan(t, "plan-1")
	if stageByKind(t, stages, publishing.StageGateway).Status != publishing.StagePending {
		t.Fatal("never-executed gateway stage was compensated")
	}
}

func TestRollbackAllAuthorizationAndRunningStage(t *testing.T) {
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.authorizer.deny = true
	if _, err := f.service.RollbackAll(ctx, "admin", "req-denied", "plan-1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied err = %v", err)
	}
	if len(f.ports.rollbackLog.snapshot()) != 0 {
		t.Fatal("denied rollback called a provider")
	}
	f.authorizer.deny = false

	empty := newPublicationFixture(t)
	empty.createPlan(t, "plan-2", "shop.example.com", "")
	if _, err := empty.service.RollbackAll(ctx, "admin", "req-none", "plan-2"); !errors.Is(err, publishing.ErrStageNotRollable) {
		t.Fatalf("no executed stages err = %v", err)
	}
	_, stages := empty.plan(t, "plan-2")
	running := stageByKind(t, stages, publishing.StageDNS)
	running.Status = publishing.StageRunning
	if err := empty.repo.UpdateStage(ctx, empty.db, running); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.service.RollbackAll(ctx, "admin", "req-running", "plan-2"); !errors.Is(err, publishing.ErrStageAlreadyRunning) {
		t.Fatalf("running stage err = %v", err)
	}
}
