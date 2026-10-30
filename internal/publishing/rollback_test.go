package publishing_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/publishing"
)

func TestRollbackPartialUntilEveryExecutedStageRolledBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	plan, err := f.service.Rollback(ctx, "admin", "req-rollback-dns", "plan-1", publishing.StageDNS)
	if err != nil {
		t.Fatalf("rollback dns: %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("partial rollback plan = %+v", plan)
	}
	_, stages := f.plan(t, "plan-1")
	dns := stageByKind(t, stages, publishing.StageDNS)
	if dns.Status != publishing.StageRolledBack || dns.ResultSummary != "dns ok" {
		t.Fatalf("dns after rollback = %+v", dns)
	}
	if strings.Contains(strings.ToLower(dns.ResultSummary), "cache") {
		t.Fatalf("rollback rewrote the summary as an instant cache claim: %q", dns.ResultSummary)
	}
	if cert := stageByKind(t, stages, publishing.StageCertificate); cert.Status != publishing.StageSucceeded {
		t.Fatalf("cert after dns rollback = %+v", cert)
	}
	if gateway := stageByKind(t, stages, publishing.StageGateway); gateway.Status != publishing.StageSucceeded {
		t.Fatalf("gateway after dns rollback = %+v", gateway)
	}
	if publishes, rollbacks := f.ports.dns.counts(); publishes != 1 || rollbacks != 1 {
		t.Fatalf("dns publishes/rollbacks = %d/%d", publishes, rollbacks)
	}
	call := f.ports.dns.lastRollback()
	if call.PlanID != "plan-1" || call.Result.Summary != "dns ok" {
		t.Fatalf("rollback call = %+v", call)
	}
	if got := f.auditCount(t, publishing.ActionRollback, "partial"); got != 1 {
		t.Fatalf("partial audit = %d", got)
	}

	plan, err = f.service.Rollback(ctx, "admin", "req-rollback-cert", "plan-1", publishing.StageCertificate)
	if err != nil {
		t.Fatalf("rollback certificate: %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("second partial plan = %+v", plan)
	}
	if got := f.auditCount(t, publishing.ActionRollback, "partial"); got != 2 {
		t.Fatalf("second partial audit = %d", got)
	}

	plan, err = f.service.Rollback(ctx, "admin", "req-rollback-gateway", "plan-1", publishing.StageGateway)
	if err != nil {
		t.Fatalf("rollback gateway: %v", err)
	}
	if plan.Status != publishing.PlanCanceled {
		t.Fatalf("final rollback plan = %+v", plan)
	}
	_, stages = f.plan(t, "plan-1")
	for _, stage := range stages {
		if stage.Status != publishing.StageRolledBack {
			t.Fatalf("stage after complete rollback = %+v", stage)
		}
	}
	if got := f.auditCount(t, publishing.ActionRollback, "success"); got != 1 {
		t.Fatalf("complete rollback audit = %d", got)
	}

	plan, err = f.service.Rollback(ctx, "admin", "req-rollback-again", "plan-1", publishing.StageDNS)
	if err != nil || plan.Status != publishing.PlanCanceled {
		t.Fatalf("idempotent rollback = %+v, %v", plan, err)
	}
	if _, rollbacks := f.ports.dns.counts(); rollbacks != 1 {
		t.Fatalf("idempotent rollback re-ran compensation: %d", rollbacks)
	}
	if _, err := f.service.Run(ctx, "admin", "req-run-canceled", "plan-1"); !errors.Is(err, publishing.ErrPlanCanceled) {
		t.Fatalf("run canceled err = %v", err)
	}
}

func TestRollbackFailedStageThenEarlierSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.set(publishing.StageResult{}, errors.New("certificate failed"))
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("Run err = %v", err)
	}
	plan, err := f.service.Rollback(ctx, "admin", "req-rollback-cert", "plan-1", publishing.StageCertificate)
	if err != nil {
		t.Fatalf("rollback failed stage: %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("plan after failed-stage rollback = %+v", plan)
	}
	_, stages := f.plan(t, "plan-1")
	cert := stageByKind(t, stages, publishing.StageCertificate)
	if cert.Status != publishing.StageRolledBack {
		t.Fatalf("cert after rollback = %+v", cert)
	}
	call := f.ports.cert.lastRollback()
	if call.PlanID != "plan-1" || call.Result.Summary != "" {
		t.Fatalf("failed-stage rollback call = %+v", call)
	}
	if gateway := stageByKind(t, stages, publishing.StageGateway); gateway.Status != publishing.StagePending {
		t.Fatalf("gateway after rollback = %+v", gateway)
	}
	plan, err = f.service.Rollback(ctx, "admin", "req-rollback-dns", "plan-1", publishing.StageDNS)
	if err != nil {
		t.Fatalf("rollback dns: %v", err)
	}
	if plan.Status != publishing.PlanCanceled {
		t.Fatalf("plan after all executed rolled back = %+v", plan)
	}
	_, stages = f.plan(t, "plan-1")
	if dns := stageByKind(t, stages, publishing.StageDNS); dns.Status != publishing.StageRolledBack {
		t.Fatalf("dns = %+v", dns)
	}
	if gateway := stageByKind(t, stages, publishing.StageGateway); gateway.Status != publishing.StagePending {
		t.Fatalf("pending gateway should stay pending = %+v", gateway)
	}
	if got := f.auditCount(t, publishing.ActionRollback, "success"); got != 1 {
		t.Fatalf("complete rollback audit = %d", got)
	}
}

func TestRollbackFailureLeavesStageAndPlanUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.ports.cert.setRollbackError(errors.New("compensation token=secret"))
	if _, err := f.service.Rollback(ctx, "admin", "req-rollback", "plan-1", publishing.StageCertificate); !errors.Is(err, publishing.ErrRollbackFailed) {
		t.Fatalf("rollback failure err = %v", err)
	}
	plan, stages := f.plan(t, "plan-1")
	if plan.Status != publishing.PlanCompleted {
		t.Fatalf("failed rollback changed plan = %+v", plan)
	}
	if cert := stageByKind(t, stages, publishing.StageCertificate); cert.Status != publishing.StageSucceeded {
		t.Fatalf("failed rollback changed stage = %+v", cert)
	}
	if publishes, rollbacks := f.ports.cert.counts(); publishes != 1 || rollbacks != 1 {
		t.Fatalf("cert publishes/rollbacks = %d/%d", publishes, rollbacks)
	}
	if got := f.auditCount(t, publishing.ActionRollback, "failure"); got != 1 {
		t.Fatalf("rollback failure audit = %d", got)
	}
	f.ports.cert.setRollbackError(nil)
	plan, err := f.service.Rollback(ctx, "admin", "req-rollback-ok", "plan-1", publishing.StageCertificate)
	if err != nil {
		t.Fatalf("retry rollback: %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("retry rollback plan = %+v", plan)
	}
	_, stages = f.plan(t, "plan-1")
	if cert := stageByKind(t, stages, publishing.StageCertificate); cert.Status != publishing.StageRolledBack {
		t.Fatalf("retried cert = %+v", cert)
	}
}

func TestRollbackRejectsPendingAndUnknownStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Rollback(ctx, "admin", "req-pending", "plan-1", publishing.StageDNS); !errors.Is(err, publishing.ErrStageNotRollable) {
		t.Fatalf("pending rollback err = %v", err)
	}
	if _, err := f.service.Rollback(ctx, "admin", "req-unknown", "plan-1", publishing.StageKind("bogus")); !errors.Is(err, publishing.ErrInvalidStage) {
		t.Fatalf("unknown stage err = %v", err)
	}
	if dns, _ := f.ports.dns.counts(); dns != 0 {
		t.Fatalf("pending rollback called publisher: %d", dns)
	}
	plan, stages := f.plan(t, "plan-1")
	if plan.Status != publishing.PlanQueued {
		t.Fatalf("plan = %+v", plan)
	}
	for _, stage := range stages {
		if stage.Status != publishing.StagePending {
			t.Fatalf("stage = %+v", stage)
		}
	}
	if got := f.auditCount(t, publishing.ActionRollback, "partial"); got != 0 {
		t.Fatalf("rollback audit = %d", got)
	}
}
