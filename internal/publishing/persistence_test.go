package publishing_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/internal/publishing"
)

func TestPersistenceRestartResumesWithoutRerunningSucceededStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "publishing-restart.db")
	ports := defaultPublicationPorts()
	first := newPublicationFixtureAt(t, path, ports, nil)
	first.createPlan(t, "plan-1", "shop.example.com", "")
	first.ports.cert.set(publishing.StageResult{}, errors.New("certificate provider unavailable"))
	if _, err := first.service.Run(ctx, "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("first run err = %v", err)
	}
	firstPlan, firstStages := first.plan(t, "plan-1")
	if firstPlan.Status != publishing.PlanPartialFailure {
		t.Fatalf("first plan = %+v", firstPlan)
	}
	if cert := stageByKind(t, firstStages, publishing.StageCertificate); cert.Status != publishing.StageFailed {
		t.Fatalf("first certificate = %+v", cert)
	}
	if err := first.db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newPublicationFixtureAt(t, path, ports, nil)
	plan, stages := second.plan(t, "plan-1")
	if plan.Status != publishing.PlanPartialFailure || plan.CreatedAt.IsZero() {
		t.Fatalf("reloaded plan = %+v", plan)
	}
	if dns := stageByKind(t, stages, publishing.StageDNS); dns.Status != publishing.StageSucceeded || dns.Attempts != 1 {
		t.Fatalf("reloaded dns = %+v", dns)
	}
	if cert := stageByKind(t, stages, publishing.StageCertificate); cert.Status != publishing.StageFailed || cert.Attempts != 1 {
		t.Fatalf("reloaded certificate = %+v", cert)
	}
	if gateway := stageByKind(t, stages, publishing.StageGateway); gateway.Status != publishing.StagePending || gateway.Attempts != 0 {
		t.Fatalf("reloaded gateway = %+v", gateway)
	}
	second.ports.cert.setBeforePublish(func() {
		_, current := second.plan(t, "plan-1")
		dns := stageByKind(t, current, publishing.StageDNS)
		cert := stageByKind(t, current, publishing.StageCertificate)
		if dns.Status != publishing.StageSucceeded || dns.Attempts != 1 {
			t.Fatalf("dns during resumed certificate = %+v", dns)
		}
		if cert.Status != publishing.StageRunning || cert.Attempts != 2 {
			t.Fatalf("certificate during resume = %+v", cert)
		}
	})
	second.ports.cert.set(publishing.StageResult{Summary: "certificate resumed"}, nil)
	plan, err := second.service.Run(ctx, "admin", "req-resume", "plan-1")
	if err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if plan.Status != publishing.PlanCompleted {
		t.Fatalf("resumed plan = %+v", plan)
	}
	if dnsCount, _ := second.ports.dns.counts(); dnsCount != 1 {
		t.Fatalf("succeeded dns reran after restart: %d", dnsCount)
	}
	if certCount, _ := second.ports.cert.counts(); certCount != 2 {
		t.Fatalf("certificate attempts = %d", certCount)
	}
	if gatewayCount, _ := second.ports.gateway.counts(); gatewayCount != 1 {
		t.Fatalf("gateway attempts = %d", gatewayCount)
	}
	_, stages = second.plan(t, "plan-1")
	if cert := stageByKind(t, stages, publishing.StageCertificate); cert.Status != publishing.StageSucceeded ||
		cert.Attempts != 2 || cert.ErrorCode != "" || cert.ResultSummary != "certificate resumed" {
		t.Fatalf("resumed certificate = %+v", cert)
	}
	if gateway := stageByKind(t, stages, publishing.StageGateway); gateway.Status != publishing.StageSucceeded {
		t.Fatalf("gateway = %+v", gateway)
	}
	if got := second.auditCount(t, publishing.ActionRun, "failure"); got != 1 {
		t.Fatalf("durable failure audit = %d", got)
	}
	if got := second.auditCount(t, publishing.ActionRun, "success"); got != 1 {
		t.Fatalf("durable success audit = %d", got)
	}
}
