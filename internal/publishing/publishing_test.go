package publishing_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/publishing"
)

func TestNewServiceRequiresAuthorizer(t *testing.T) {
	t.Parallel()
	f := newPublicationFixture(t)
	_, err := publishing.NewService(publishing.Options{
		DB: f.db, Repo: f.repo, Groups: f.ports.groups, DNS: f.ports.dns,
		Certificates: f.ports.cert, Gateway: f.ports.gateway, Audit: f.recorder,
	})
	if !errors.Is(err, publishing.ErrMissingAuthorizer) {
		t.Fatalf("err = %v, want ErrMissingAuthorizer", err)
	}
}

func TestCreatePlanPersistsOrderedStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	plan, err := f.service.Create(ctx, "admin", "req-create", publishing.CreateInput{
		ID: "plan-1", Name: "Web", Domain: "Shop.Example.COM", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plan.Status != publishing.PlanQueued || plan.CurrentStage != publishing.StageDNS || plan.Domain != "shop.example.com" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.CreatedBy != "admin" || plan.IdempotencyKey != "idem-1" {
		t.Fatalf("plan metadata = %+v", plan)
	}
	stored, stages := f.plan(t, "plan-1")
	if stored.Status != publishing.PlanQueued {
		t.Fatalf("stored plan = %+v", stored)
	}
	expected := publishing.StageKinds()
	if len(stages) != len(expected) {
		t.Fatalf("stages = %+v", stages)
	}
	for index, stage := range stages {
		if stage.Kind != expected[index] || stage.Status != publishing.StagePending || stage.Attempts != 0 {
			t.Fatalf("stage %d = %+v", index, stage)
		}
	}
	if got := f.auditCount(t, publishing.ActionCreate, "success"); got != 1 {
		t.Fatalf("create audit = %d", got)
	}

	repeated, err := f.service.Create(ctx, "admin", "req-again", publishing.CreateInput{
		ID: "plan-2", Name: "Web", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, IdempotencyKey: "idem-1",
	})
	if err != nil || repeated.ID != "plan-1" {
		t.Fatalf("idempotent create = %+v, %v", repeated, err)
	}
	if _, _, err := f.repo.Get(ctx, "plan-2"); !errors.Is(err, publishing.ErrPlanNotFound) {
		t.Fatalf("idempotent create inserted another plan: %v", err)
	}
	if got := f.auditCount(t, publishing.ActionCreate, "success"); got != 1 {
		t.Fatalf("idempotent create audit = %d", got)
	}
	if _, err := f.service.Create(ctx, "admin", "req-duplicate-id", publishing.CreateInput{
		ID: "plan-1", Name: "Other", Domain: "other.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, IdempotencyKey: "idem-other",
	}); !errors.Is(err, publishing.ErrPlanExists) {
		t.Fatalf("duplicate id err = %v", err)
	}
	if _, err := f.service.Create(ctx, "admin", "req-revision", publishing.CreateInput{
		ID: "plan-3", Name: "Web", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 2, IdempotencyKey: "idem-2",
	}); !errors.Is(err, publishing.ErrRevisionConflict) {
		t.Fatalf("revision conflict err = %v", err)
	}
	if _, _, err := f.repo.Get(ctx, "plan-3"); !errors.Is(err, publishing.ErrPlanNotFound) {
		t.Fatalf("revision conflict persisted plan: %v", err)
	}
	if _, err := f.service.Create(ctx, "admin", "req-missing-group", publishing.CreateInput{
		ID: "plan-4", Name: "Web", Domain: "shop.example.com", ServiceGroupID: "missing",
		ExpectedGroupRevision: 3,
	}); !errors.Is(err, publishing.ErrServiceGroupNotFound) {
		t.Fatalf("missing group err = %v", err)
	}
	f.ports.groups.setError(errors.New("group backend token=secret"))
	if _, err := f.service.Create(ctx, "admin", "req-group-error", publishing.CreateInput{
		ID: "plan-6", Name: "Web", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3,
	}); !errors.Is(err, publishing.ErrGroupLookupFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("group lookup err = %v", err)
	}
	f.ports.groups.setError(nil)
	if _, err := f.service.Create(ctx, "admin", "req-bad-domain", publishing.CreateInput{
		ID: "plan-5", Name: "Web", Domain: "https://shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3,
	}); !errors.Is(err, publishing.ErrInvalidPlan) {
		t.Fatalf("bad domain err = %v", err)
	}
	gotPlan, gotStages, err := f.service.Get(ctx, "admin", "plan-1")
	if err != nil || gotPlan.ID != "plan-1" || len(gotStages) != 3 {
		t.Fatalf("Get = %+v, %+v, %v", gotPlan, gotStages, err)
	}
}

func TestCreatePlanDeniedAudited(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixtureAt(t, filepath.Join(t.TempDir(), "denied.db"), nil, &switchAuthorizer{deny: true})
	if _, err := f.service.Create(ctx, "admin", "req-denied", publishing.CreateInput{
		ID: "plan-1", Name: "Web", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3,
	}); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied err = %v", err)
	}
	if _, _, err := f.repo.Get(ctx, "plan-1"); !errors.Is(err, publishing.ErrPlanNotFound) {
		t.Fatalf("denied create persisted plan: %v", err)
	}
	if got := f.auditCount(t, publishing.ActionWrite, "denied"); got != 1 {
		t.Fatalf("denial audit = %d", got)
	}
}

func TestRunOrdersStagesAndPersistsBeforeNext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.setBeforePublish(func() {
		plan, stages := f.plan(t, "plan-1")
		if plan.Status != publishing.PlanRunning || plan.CurrentStage != publishing.StageCertificate {
			t.Fatalf("plan during certificate = %+v", plan)
		}
		dns := stageByKind(t, stages, publishing.StageDNS)
		cert := stageByKind(t, stages, publishing.StageCertificate)
		if dns.Status != publishing.StageSucceeded || cert.Status != publishing.StageRunning || cert.Attempts != 1 {
			t.Fatalf("stages before certificate = %+v", stages)
		}
	})
	plan, err := f.service.Run(ctx, "admin", "req-run", "plan-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.Status != publishing.PlanCompleted || plan.CurrentStage != publishing.StageGateway {
		t.Fatalf("plan after run = %+v", plan)
	}
	f.verifyPublished(t, "plan-1")
	if got := f.ports.callLog.snapshot(); len(got) != 3 ||
		got[0] != publishing.StageDNS || got[1] != publishing.StageCertificate || got[2] != publishing.StageGateway {
		t.Fatalf("call order = %+v", got)
	}
	dnsRequest, ok := f.ports.dns.lastRequest().(publishing.DNSPublishRequest)
	if !ok || dnsRequest.PlanID != "plan-1" || dnsRequest.Domain != "shop.example.com" ||
		dnsRequest.ServiceGroupID != "sg1" || dnsRequest.ExpectedGroupRevision != 3 {
		t.Fatalf("dns request = %+v", dnsRequest)
	}
	if got := f.auditCount(t, publishing.ActionRun, "success"); got != 1 {
		t.Fatalf("run audit = %d", got)
	}
	if got := f.auditCount(t, publishing.ActionStage, "success"); got != 7 {
		t.Fatalf("stage audit = %d", got)
	}
	dnsPublishes, _ := f.ports.dns.counts()
	if _, err := f.service.Run(ctx, "admin", "req-run-again", "plan-1"); !errors.Is(err, publishing.ErrPlanCompleted) {
		t.Fatalf("completed rerun err = %v", err)
	}
	again, _ := f.ports.dns.counts()
	if again != dnsPublishes {
		t.Fatalf("completed plan reran dns: %d -> %d", dnsPublishes, again)
	}
}

func TestRunFailureStopsAndRetryResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.set(publishing.StageResult{}, errors.New("provider exploded token=secret"))
	plan, err := f.service.Run(ctx, "admin", "req-run", "plan-1")
	if !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("Run err = %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure || plan.CurrentStage != publishing.StageCertificate {
		t.Fatalf("failed plan = %+v", plan)
	}
	_, stages := f.plan(t, "plan-1")
	dns := stageByKind(t, stages, publishing.StageDNS)
	cert := stageByKind(t, stages, publishing.StageCertificate)
	gateway := stageByKind(t, stages, publishing.StageGateway)
	if dns.Status != publishing.StageSucceeded || dns.Attempts != 1 {
		t.Fatalf("dns = %+v", dns)
	}
	if cert.Status != publishing.StageFailed || cert.Attempts != 1 || cert.ErrorCode != publishing.ErrorCodePublishError || cert.ResultSummary != "" {
		t.Fatalf("cert = %+v", cert)
	}
	if gateway.Status != publishing.StagePending || gateway.Attempts != 0 {
		t.Fatalf("gateway = %+v", gateway)
	}
	if got := f.ports.callLog.snapshot(); len(got) != 2 || got[0] != publishing.StageDNS || got[1] != publishing.StageCertificate {
		t.Fatalf("call order after failure = %+v", got)
	}
	gatewayCount, _ := f.ports.gateway.counts()
	if gatewayCount != 0 {
		t.Fatalf("gateway ran after failure: %d", gatewayCount)
	}
	if got := f.auditCount(t, publishing.ActionRun, "failure"); got != 1 {
		t.Fatalf("failure audit = %d", got)
	}

	f.ports.cert.set(publishing.StageResult{Summary: "certificate retry ok"}, nil)
	plan, err = f.service.Run(ctx, "admin", "req-retry", "plan-1")
	if err != nil {
		t.Fatalf("retry Run: %v", err)
	}
	if plan.Status != publishing.PlanCompleted {
		t.Fatalf("retry plan = %+v", plan)
	}
	dnsCount, _ := f.ports.dns.counts()
	certCount, _ := f.ports.cert.counts()
	gatewayCount, _ = f.ports.gateway.counts()
	if dnsCount != 1 || certCount != 2 || gatewayCount != 1 {
		t.Fatalf("publish counts dns=%d cert=%d gateway=%d", dnsCount, certCount, gatewayCount)
	}
	_, stages = f.plan(t, "plan-1")
	dns = stageByKind(t, stages, publishing.StageDNS)
	cert = stageByKind(t, stages, publishing.StageCertificate)
	gateway = stageByKind(t, stages, publishing.StageGateway)
	if dns.Attempts != 1 || cert.Attempts != 2 || gateway.Attempts != 1 {
		t.Fatalf("attempts dns=%d cert=%d gateway=%d", dns.Attempts, cert.Attempts, gateway.Attempts)
	}
	if cert.Status != publishing.StageSucceeded || cert.ErrorCode != "" || cert.ResultSummary != "certificate retry ok" {
		t.Fatalf("retried cert = %+v", cert)
	}
	if got := f.auditCount(t, publishing.ActionRun, "success"); got != 1 {
		t.Fatalf("success audit = %d", got)
	}
	if got := f.auditCount(t, publishing.ActionRun, "failure"); got != 1 {
		t.Fatalf("failure audit after retry = %d", got)
	}
}

func TestRunRejectsEmptyStageResult(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.dns.set(publishing.StageResult{}, nil)
	plan, err := f.service.Run(ctx, "admin", "req-run", "plan-1")
	if !errors.Is(err, publishing.ErrStageFailed) || !errors.Is(err, publishing.ErrInvalidStageResult) {
		t.Fatalf("invalid result err = %v", err)
	}
	if plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("plan = %+v", plan)
	}
	_, stages := f.plan(t, "plan-1")
	dns := stageByKind(t, stages, publishing.StageDNS)
	cert := stageByKind(t, stages, publishing.StageCertificate)
	gateway := stageByKind(t, stages, publishing.StageGateway)
	if dns.Status != publishing.StageFailed || dns.ErrorCode != publishing.ErrorCodeInvalidResult || dns.ResultSummary != "" {
		t.Fatalf("dns = %+v", dns)
	}
	if cert.Status != publishing.StagePending || gateway.Status != publishing.StagePending {
		t.Fatalf("later stages = %+v", stages)
	}
	if count, _ := f.ports.cert.counts(); count != 0 {
		t.Fatalf("certificate ran after invalid dns result: %d", count)
	}
	f.ports.dns.set(publishing.StageResult{Summary: "dns ok"}, nil)
	if _, err := f.service.Run(ctx, "admin", "req-retry", "plan-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	_, stages = f.plan(t, "plan-1")
	dns = stageByKind(t, stages, publishing.StageDNS)
	if dns.Attempts != 2 || dns.Status != publishing.StageSucceeded {
		t.Fatalf("retried dns = %+v", dns)
	}
}

func TestStageResultSummaryRedacted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.dns.set(publishing.StageResult{
		Summary: "record created token=supersecret Authorization: Bearer authsecret",
	}, nil)
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, stages := f.plan(t, "plan-1")
	dns := stageByKind(t, stages, publishing.StageDNS)
	if dns.Status != publishing.StageSucceeded {
		t.Fatalf("dns = %+v", dns)
	}
	if strings.Contains(dns.ResultSummary, "supersecret") || strings.Contains(dns.ResultSummary, "authsecret") {
		t.Fatalf("summary was not redacted: %q", dns.ResultSummary)
	}
	if !strings.Contains(dns.ResultSummary, "[redacted]") {
		t.Fatalf("summary = %q", dns.ResultSummary)
	}
}

func TestRunRevisionConflictDoesNotPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.groups.setRevision("sg1", 4)
	plan, err := f.service.Run(ctx, "admin", "req-run", "plan-1")
	if !errors.Is(err, publishing.ErrRevisionConflict) {
		t.Fatalf("err = %v", err)
	}
	if plan.Status != publishing.PlanQueued {
		t.Fatalf("plan = %+v", plan)
	}
	if dns, _ := f.ports.dns.counts(); dns != 0 {
		t.Fatalf("dns published on revision conflict: %d", dns)
	}
	if got := f.auditCount(t, publishing.ActionRun, "success"); got != 0 {
		t.Fatalf("run audit = %d", got)
	}
}

func TestRunRejectsAlreadyRunningStage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	_, stages := f.plan(t, "plan-1")
	dns := stageByKind(t, stages, publishing.StageDNS)
	dns.Status = publishing.StageRunning
	dns.Attempts = 1
	dns.StartedAt = f.clock.Now()
	if err := f.repo.UpdateStage(ctx, f.db, dns); err != nil {
		t.Fatalf("UpdateStage: %v", err)
	}
	if _, err := f.service.Run(ctx, "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageAlreadyRunning) {
		t.Fatalf("err = %v", err)
	}
	if count, _ := f.ports.dns.counts(); count != 0 {
		t.Fatalf("running stage republished: %d", count)
	}
}

func TestRunAuthDenialAudited(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	denied := newPublicationService(t, f, &switchAuthorizer{deny: true})
	if _, err := denied.Run(ctx, "admin", "req-denied-run", "plan-1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied run err = %v", err)
	}
	if _, err := denied.Rollback(ctx, "admin", "req-denied-rollback", "plan-1", publishing.StageDNS); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied rollback err = %v", err)
	}
	if _, _, err := denied.Get(ctx, "admin", "plan-1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied get err = %v", err)
	}
	if got := f.auditCount(t, publishing.ActionWrite, "denied"); got != 2 {
		t.Fatalf("denial audit = %d", got)
	}
	if dns, _ := f.ports.dns.counts(); dns != 0 {
		t.Fatalf("denied run published dns: %d", dns)
	}
	plan, stages := f.plan(t, "plan-1")
	if plan.Status != publishing.PlanQueued {
		t.Fatalf("denied run changed plan: %+v", plan)
	}
	for _, stage := range stages {
		if stage.Status != publishing.StagePending {
			t.Fatalf("denied run changed stage: %+v", stage)
		}
	}
}
