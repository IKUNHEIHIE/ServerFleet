package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/tasks"
)

type reviewPublicationGroups struct{ revision uint64 }

func (g reviewPublicationGroups) LookupGroup(_ context.Context, id string) (publishing.GroupRef, bool, error) {
	revision := g.revision
	if revision == 0 {
		revision = 1
	}
	return publishing.GroupRef{ID: id, Revision: revision, NodeIDs: []string{"node-one"}}, true, nil
}

type reviewPublicationDNS struct {
	mu      sync.Mutex
	records map[string]string
	deletes []string
}

func newReviewPublicationDNS() *reviewPublicationDNS {
	return &reviewPublicationDNS{records: map[string]string{}}
}

func (w *reviewPublicationDNS) EnsureRecord(_ context.Context, _, _, _, name string, _ dns.RecordType, content string, _ int) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, value := range w.records {
		if value == name+"="+content {
			return id, false, nil
		}
	}
	id := fmt.Sprintf("record-%d", len(w.records)+1)
	w.records[id] = name + "=" + content
	return id, true, nil
}

func (w *reviewPublicationDNS) DeleteRecord(_ context.Context, _, _, _, recordID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deletes = append(w.deletes, recordID)
	delete(w.records, recordID)
	return nil
}

func (w *reviewPublicationDNS) snapshot() (int, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.records), append([]string(nil), w.deletes...)
}

type reviewPublicationAddresses struct{}

func (reviewPublicationAddresses) Addresses(context.Context, string, []string) ([]string, error) {
	return []string{"203.0.113.10"}, nil
}

type reviewPublicationCertificates struct{}

func (reviewPublicationCertificates) Inspect(context.Context, string, string, string) (string, error) {
	return "serial=test", nil
}

func (reviewPublicationCertificates) Paths(context.Context, string, string) (string, string, error) {
	return "/etc/serverfleet/test.crt", "/etc/serverfleet/test.key", nil
}

type reviewPublicationUpstreams struct{}

func (reviewPublicationUpstreams) Upstreams(context.Context, string, string) ([]string, error) {
	return []string{"10.0.0.2:8080"}, nil
}

type publicationHarness struct {
	gateway *gatewayHarness
	service *publishing.Service
	repo    *publishing.Repo
	routes  *reviewRouteService
	dns     *reviewPublicationDNS
	plan    publishing.Plan
}

func newPublicationHarness(t *testing.T, agentResult tasks.NodeResultCode) *publicationHarness {
	t.Helper()
	gateway := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: true, AgentResult: agentResult,
	})
	repo, err := publishing.NewRepo(gateway.db)
	if err != nil {
		t.Fatal(err)
	}
	routes := &reviewRouteService{}
	dnsWriter := newReviewPublicationDNS()
	applier := &publicationGatewayApplier{
		gateways: routes, tasks: gateway.host, plans: publicationPlanLookup{repo: repo},
	}
	bridge, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{
		Plans: publicationPlanLookup{repo: repo}, DNS: dnsWriter,
		Addresses: reviewPublicationAddresses{}, Certificates: reviewPublicationCertificates{},
		Upstreams: reviewPublicationUpstreams{}, Gateway: applier,
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := audit.NewValidator(AuditActions(), audit.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(gateway.db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service, err := publishing.NewService(publishing.Options{
		DB: gateway.db, Repo: repo, Groups: reviewPublicationGroups{},
		DNS: bridge.DNS, Certificates: bridge.Certificates, Gateway: bridge.Gateway,
		Audit: recorder, Authorizer: reviewGatewayAuth(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &publicationHarness{gateway: gateway, service: service, repo: repo, routes: routes, dns: dnsWriter}
}

func (h *publicationHarness) createPlan(t *testing.T) publishing.Plan {
	t.Helper()
	plan, err := h.service.Create(context.Background(), "owner", "req-create-plan", publishing.CreateInput{
		ID: "plan-pub", Name: "Web", Domain: "app.example.com", ServiceGroupID: "group-one",
		ExpectedGroupRevision: 1, GatewayIDs: []string{"gw-one"}, ZoneID: "zone-one",
		HTTPS: false, TargetRevision: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.plan = plan
	return plan
}

func TestPublishingRealStagesCompleteAndRollbackCompensateOnlyOwnResources(t *testing.T) {
	h := newPublicationHarness(t, tasks.NodeResultSucceeded)
	plan := h.createPlan(t)
	running, err := h.service.Run(context.Background(), "owner", "req-run", plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != publishing.PlanCompleted {
		t.Fatalf("plan = %+v", running)
	}
	records, deletes := h.dns.snapshot()
	if records != 1 || len(deletes) != 0 {
		t.Fatalf("dns records=%d deletes=%v", records, deletes)
	}
	if len(h.gateway.uploader.snapshot()) != 1 || h.gateway.agent.count() != 1 {
		t.Fatalf("uploads=%d agent=%d", len(h.gateway.uploader.snapshot()), h.gateway.agent.count())
	}
	if h.routes.route.ID != plan.ID || h.routes.createCalls != 1 || h.routes.deleteCalls != 0 {
		t.Fatalf("route = %+v", h.routes.route)
	}
	rolled, err := h.service.RollbackAll(context.Background(), "owner", "req-rollback", plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Status != publishing.PlanCanceled {
		t.Fatalf("rolled plan = %+v", rolled)
	}
	records, deletes = h.dns.snapshot()
	if records != 0 || len(deletes) != 1 {
		t.Fatalf("dns after rollback records=%d deletes=%v", records, deletes)
	}
	if h.routes.deleteCalls != 1 || h.routes.route.ID != "" {
		t.Fatalf("route after rollback = %+v", h.routes.route)
	}
	if h.gateway.agent.count() != 2 {
		t.Fatalf("agent calls after compensation = %d", h.gateway.agent.count())
	}
}

func TestPublishingUnknownGatewayFailureKeepsPartialStateForExplicitRollback(t *testing.T) {
	h := newPublicationHarness(t, tasks.NodeResultUnknown)
	plan := h.createPlan(t)
	if _, err := h.service.Run(context.Background(), "owner", "req-run", plan.ID); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("Run err = %v", err)
	}
	_, stages, err := h.repo.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var gatewayStage publishing.Stage
	for _, stage := range stages {
		if stage.Kind == publishing.StageGateway {
			gatewayStage = stage
		}
	}
	if gatewayStage.Status != publishing.StageFailed || !strings.Contains(gatewayStage.ResultSummary, `"planId":"`+plan.ID+`"`) {
		t.Fatalf("gateway stage lost compensation identity: %+v", gatewayStage)
	}
	if h.routes.route.ID != plan.ID {
		t.Fatalf("route missing after uncertain apply: %+v", h.routes.route)
	}
	h.gateway.agent.mu.Lock()
	h.gateway.agent.result = tasks.NodeResultSucceeded
	h.gateway.agent.mu.Unlock()
	rolled, err := h.service.RollbackAll(context.Background(), "owner", "req-rollback", plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Status != publishing.PlanCanceled {
		t.Fatalf("rolled plan = %+v", rolled)
	}
	if h.routes.deleteCalls != 1 || h.routes.route.ID != "" {
		t.Fatalf("route after rollback = %+v", h.routes.route)
	}
	records, deletes := h.dns.snapshot()
	if records != 0 || len(deletes) != 1 {
		t.Fatalf("dns after rollback records=%d deletes=%v", records, deletes)
	}
}
