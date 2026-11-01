package publishing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestCreatePersistsTargetsAndStageRequests(t *testing.T) {
	f := newPublicationFixture(t)
	plan, err := f.service.Create(context.Background(), "admin", "req-targets", publishing.CreateInput{
		ID: "plan-targets", Name: "targets", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, GatewayIDs: []string{"gw-1", "gw-2"}, ZoneID: "zone-1",
		CertificateID: "cert-1", HTTPS: true, TargetRevision: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetRevision != 2 || !plan.HTTPS || plan.ZoneID != "zone-1" || plan.CertificateID != "cert-1" ||
		len(plan.GatewayIDs) != 2 || plan.GatewayIDs[0] != "gw-1" {
		t.Fatalf("plan = %+v", plan)
	}
	reloaded, _, err := f.repo.Get(context.Background(), plan.ID)
	if err != nil || reloaded.TargetRevision != 2 || len(reloaded.GatewayIDs) != 2 || !reloaded.HTTPS {
		t.Fatalf("reloaded = %+v, %v", reloaded, err)
	}
	if _, err := f.service.Run(context.Background(), "admin", "req-run", plan.ID); err != nil {
		t.Fatal(err)
	}
	request, ok := f.ports.dns.lastRequest().(publishing.DNSPublishRequest)
	if !ok || request.TargetRevision != 2 || request.ZoneID != "zone-1" || request.CertificateID != "cert-1" ||
		len(request.GatewayIDs) != 2 || !request.HTTPS {
		t.Fatalf("dns request = %+v", request)
	}
}

func TestRunRechecksRevisionBeforeEveryStage(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.dns.setBeforePublish(func() { f.ports.groups.setRevision("sg1", 4) })
	_, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1")
	if !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("err = %v", err)
	}
	_, stages := f.plan(t, "plan-1")
	cert := stageByKind(t, stages, publishing.StageCertificate)
	if cert.Status != publishing.StageFailed || cert.ErrorCode != publishing.ErrorCodeRevisionConflict {
		t.Fatalf("certificate stage = %+v", cert)
	}
	if publishes, _ := f.ports.cert.counts(); publishes != 0 {
		t.Fatalf("certificate publisher ran after a revision change: %d", publishes)
	}
}

func TestRunRechecksAuthorityBeforeEveryStage(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.dns.setBeforePublish(func() { f.authorizer.deny = true })
	_, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1")
	if !errors.Is(err, publishing.ErrStageFailed) || !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	_, stages := f.plan(t, "plan-1")
	cert := stageByKind(t, stages, publishing.StageCertificate)
	if cert.Status != publishing.StageFailed || cert.ErrorCode != publishing.ErrorCodeAuthorizationDenied {
		t.Fatalf("certificate stage = %+v", cert)
	}
	if publishes, _ := f.ports.cert.counts(); publishes != 0 {
		t.Fatalf("certificate publisher ran after revocation: %d", publishes)
	}
}

type countingAuthorizer struct {
	mu        sync.Mutex
	calls     int
	denyAfter int
}

func (a *countingAuthorizer) Authorize(context.Context, auth.Check) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.denyAfter > 0 && a.calls > a.denyAfter {
		return auth.ErrDenied
	}
	return nil
}

func TestRollbackAllRechecksAuthorityBeforeEachCompensation(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	if _, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1"); err != nil {
		t.Fatal(err)
	}
	counting := &countingAuthorizer{denyAfter: 1}
	service := newPublicationService(t, f, counting)
	plan, err := service.RollbackAll(context.Background(), "admin", "req-rollback-all", "plan-1")
	if !errors.Is(err, auth.ErrDenied) || plan.Status != publishing.PlanPartialFailure {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
	if len(f.ports.rollbackLog.snapshot()) != 0 {
		t.Fatal("compensation ran after the per-stage authority check failed")
	}
}

func TestTargetsAreNotSilentlyPersistedWithoutAcceptedDDL(t *testing.T) {
	db := testkit.NewDB(t, publishing.Migrations(), audit.Migrations())
	repo, err := publishing.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if repo.TargetsPersisted() {
		t.Fatal("legacy schema unexpectedly reports target columns")
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(publishing.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ports := defaultPublicationPorts()
	service, err := publishing.NewService(publishing.Options{
		DB: db, Repo: repo, Groups: ports.groups, DNS: ports.dns, Certificates: ports.cert,
		Gateway: ports.gateway, Audit: recorder, Authorizer: &switchAuthorizer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Create(context.Background(), "admin", "req-legacy", publishing.CreateInput{
		ID: "plan-legacy", Name: "legacy", Domain: "shop.example.com", ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1",
		CertificateID: "cert-1", HTTPS: true, TargetRevision: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetsPersisted {
		t.Fatal("legacy plan claimed targets were persisted")
	}
	loaded, _, err := repo.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetsPersisted || len(loaded.GatewayIDs) != 0 || loaded.TargetRevision != 1 {
		t.Fatalf("legacy loaded plan = %+v", loaded)
	}
}
