package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/publishing"
)

type reviewPlanLookup struct {
	plan publishing.Plan
	err  error
}

func (l reviewPlanLookup) GetPlan(context.Context, string) (publishing.Plan, error) {
	if l.err != nil {
		return publishing.Plan{}, l.err
	}
	return l.plan, nil
}

type reviewRouteService struct {
	mu              sync.Mutex
	route           gateway.GatewayRoute
	createCalls     int
	updateCalls     int
	deleteCalls     int
	deletedRevision uint64
	deleteErr       error
}

func (s *reviewRouteService) GetGateway(context.Context, string, string) (gateway.GatewayNode, error) {
	return gateway.GatewayNode{ID: "gw-one", NodeID: "node-one", Enabled: true}, nil
}

func (s *reviewRouteService) GetRoute(context.Context, string, string) (gateway.GatewayRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.ID == "" {
		return gateway.GatewayRoute{}, gateway.ErrRouteNotFound
	}
	return s.route, nil
}

func (s *reviewRouteService) CreateRoute(_ context.Context, _, _, gatewayID string, input gateway.CreateRouteInput) (gateway.GatewayRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	s.route = gateway.GatewayRoute{
		ID: input.ID, GatewayID: gatewayID, Domain: input.Domain, ServiceGroupID: input.ServiceGroupID,
		HTTPS: input.HTTPS, Enabled: input.Enabled, Revision: 1,
	}
	return s.route, nil
}

func (s *reviewRouteService) UpdateRoute(_ context.Context, _, _, gatewayID, routeID string, expectedRevision uint64, input gateway.UpdateRouteInput) (gateway.GatewayRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCalls++
	if s.route.ID != routeID || s.route.Revision != expectedRevision {
		return gateway.GatewayRoute{}, gateway.ErrRevisionConflict
	}
	s.route.Domain = input.Domain
	s.route.ServiceGroupID = input.ServiceGroupID
	s.route.HTTPS = input.HTTPS
	s.route.Enabled = input.Enabled
	s.route.Revision++
	return s.route, nil
}

func (s *reviewRouteService) DeleteRoute(_ context.Context, _, _, _, routeID string, expectedRevision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	s.deletedRevision = expectedRevision
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if s.route.ID != routeID || s.route.Revision != expectedRevision {
		return gateway.ErrRevisionConflict
	}
	s.route = gateway.GatewayRoute{}
	return nil
}

type reviewTaskRunner struct {
	applyCalls  int
	removeCalls int
	applyErr    error
	removeErr   error
	lastPlan    publishing.Plan
	lastSite    gateway.NginxSite
}

func (r *reviewTaskRunner) apply(_ context.Context, plan publishing.Plan, site gateway.NginxSite, _ gateway.NginxTaskInput) (string, error) {
	r.applyCalls++
	r.lastPlan = plan
	r.lastSite = site
	if r.applyErr != nil {
		return "", r.applyErr
	}
	return "task-apply", nil
}

func (r *reviewTaskRunner) remove(_ context.Context, plan publishing.Plan, site gateway.NginxSite, _ gateway.NginxTaskInput) (string, error) {
	r.removeCalls++
	r.lastPlan = plan
	r.lastSite = site
	if r.removeErr != nil {
		return "", r.removeErr
	}
	return "task-remove", nil
}

func reviewGatewayRequest() publishing.GatewayRequest {
	return publishing.GatewayRequest{
		PlanID: "plan-one", SubjectID: "owner", RequestID: "req-gateway",
		Domain: "app.example.com", ServiceGroupID: "group-one", ExpectedGroupRevision: 1,
		GatewayIDs: []string{"gw-one"}, ZoneID: "zone-one", CertificateID: "", HTTPS: false,
		TargetRevision: 2,
	}
}

func reviewGatewaySummary(t *testing.T, plan publishing.Plan, route gateway.GatewayRoute, site gateway.NginxSite, created bool) string {
	t.Helper()
	raw, err := json.Marshal(gatewayPublicationSummary{
		RouteID: route.ID, GatewayID: route.GatewayID, NodeID: "node-one",
		SitePath: site.Path, ConfigSHA256: site.Digest, TaskID: "task-apply",
		RouteRevision: route.Revision, RouteCreated: created,
		PlanID: plan.ID, PlanTargetRevision: plan.TargetRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestGatewayApplyUsesConfiguredPrivatePaths(t *testing.T) {
	plan := reviewGatewayPlan()
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{
		gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan},
		sitePaths: map[string]GatewayInstallPaths{
			"gw-one": {SiteDir: "/srv/fleet/nginx/conf", ArtifactDir: "/srv/fleet/nginx/artifacts"},
		},
	}
	if _, err := applier.Apply(context.Background(), plan, reviewGatewayRequest(), []string{"127.0.0.1:8080"}, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(runner.lastSite.Path, "/srv/fleet/nginx/conf/") ||
		!strings.HasPrefix(runner.lastSite.ArtifactPath, "/srv/fleet/nginx/artifacts/") {
		t.Fatalf("rendered paths = %q / %q", runner.lastSite.Path, runner.lastSite.ArtifactPath)
	}
}

func TestGatewayApplyRejectsMissingPrivatePathsBeforeRouteWrite(t *testing.T) {
	plan := reviewGatewayPlan()
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{
		gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan},
		sitePaths: map[string]GatewayInstallPaths{"other-gateway": {SiteDir: "/srv/a", ArtifactDir: "/srv/b"}},
	}
	if _, err := applier.Apply(context.Background(), plan, reviewGatewayRequest(), []string{"127.0.0.1:8080"}, "", ""); !errors.Is(err, publishing.ErrProductionUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if routes.createCalls != 0 || runner.applyCalls != 0 {
		t.Fatalf("missing paths wrote route/apply: %d/%d", routes.createCalls, runner.applyCalls)
	}
}

func TestGatewayApplyRecordsExactTargetAndRouteRevision(t *testing.T) {
	plan := reviewGatewayPlan()
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary, err := applier.Apply(context.Background(), plan, reviewGatewayRequest(),
		[]string{"127.0.0.1:8080"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var stored gatewayPublicationSummary
	if json.Unmarshal([]byte(summary), &stored) != nil {
		t.Fatalf("summary = %q", summary)
	}
	if stored.PlanID != plan.ID || stored.PlanTargetRevision != plan.TargetRevision ||
		stored.RouteRevision != 1 || !stored.RouteCreated || stored.RouteID != plan.ID {
		t.Fatalf("summary = %+v", stored)
	}
	if routes.createCalls != 1 || runner.applyCalls != 1 || runner.lastPlan.TargetRevision != plan.TargetRevision {
		t.Fatalf("creates=%d applies=%d plan=%+v", routes.createCalls, runner.applyCalls, runner.lastPlan)
	}
}

func TestGatewayApplyRejectsChangedTargetBeforeRouteWrite(t *testing.T) {
	plan := reviewGatewayPlan()
	changed := plan
	changed.TargetRevision = plan.TargetRevision + 1
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: changed}}
	if _, err := applier.Apply(context.Background(), plan, reviewGatewayRequest(), []string{"127.0.0.1:8080"}, "", ""); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("err = %v", err)
	}
	if routes.createCalls != 0 || routes.updateCalls != 0 || runner.applyCalls != 0 {
		t.Fatalf("stale target wrote route/apply: %d/%d/%d", routes.createCalls, routes.updateCalls, runner.applyCalls)
	}
}

func TestGatewayRollbackDeletesOnlyOwnUnchangedRoute(t *testing.T) {
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	routes := &reviewRouteService{route: gateway.GatewayRoute{
		ID: plan.ID, GatewayID: "gw-one", Domain: plan.Domain, ServiceGroupID: plan.ServiceGroupID,
		Revision: 1, Enabled: true,
	}}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary := reviewGatewaySummary(t, plan, routes.route, site, true)
	if err := applier.Rollback(context.Background(), plan, summary); err != nil {
		t.Fatal(err)
	}
	if runner.removeCalls != 1 || routes.deleteCalls != 1 || routes.deletedRevision != 1 {
		t.Fatalf("remove=%d delete=%d revision=%d", runner.removeCalls, routes.deleteCalls, routes.deletedRevision)
	}
}

func TestGatewayRollbackLeavesNewerRoute(t *testing.T) {
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	routes := &reviewRouteService{route: gateway.GatewayRoute{
		ID: plan.ID, GatewayID: "gw-one", Domain: plan.Domain, Revision: 2, Enabled: true,
	}}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary := reviewGatewaySummary(t, plan, gateway.GatewayRoute{
		ID: plan.ID, GatewayID: "gw-one", Revision: 1,
	}, site, true)
	if err := applier.Rollback(context.Background(), plan, summary); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("err = %v", err)
	}
	if routes.deleteCalls != 0 {
		t.Fatalf("newer route was deleted: %d", routes.deleteCalls)
	}
}

func TestGatewayRollbackStopsRouteDeleteWhenSiteRemovalFails(t *testing.T) {
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	routes := &reviewRouteService{route: gateway.GatewayRoute{
		ID: plan.ID, GatewayID: "gw-one", Domain: plan.Domain, Revision: 1, Enabled: true,
	}}
	runner := &reviewTaskRunner{removeErr: errors.New("agent unreachable")}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary := reviewGatewaySummary(t, plan, routes.route, site, true)
	if err := applier.Rollback(context.Background(), plan, summary); err == nil {
		t.Fatal("site removal failure reported success")
	}
	if routes.deleteCalls != 0 {
		t.Fatalf("route deleted before site removal succeeded: %d", routes.deleteCalls)
	}
}

func TestGatewayRollbackKeepsPreexistingRoute(t *testing.T) {
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	routes := &reviewRouteService{route: gateway.GatewayRoute{
		ID: "other-route", GatewayID: "gw-one", Domain: plan.Domain, Revision: 1, Enabled: true,
	}}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary := reviewGatewaySummary(t, plan, gateway.GatewayRoute{ID: plan.ID, GatewayID: "gw-one", Revision: 1}, site, false)
	if err := applier.Rollback(context.Background(), plan, summary); err != nil {
		t.Fatal(err)
	}
	if runner.removeCalls != 1 || routes.deleteCalls != 0 {
		t.Fatalf("remove=%d delete=%d", runner.removeCalls, routes.deleteCalls)
	}
}

func TestGatewayRollbackRejectsChangedTargetBeforeCompensation(t *testing.T) {
	plan := reviewGatewayPlan()
	changed := plan
	changed.TargetRevision = plan.TargetRevision + 1
	site := reviewGatewaySite(t)
	routes := &reviewRouteService{route: gateway.GatewayRoute{ID: plan.ID, GatewayID: "gw-one", Revision: 1}}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: changed}}
	summary := reviewGatewaySummary(t, plan, routes.route, site, true)
	if err := applier.Rollback(context.Background(), plan, summary); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("err = %v", err)
	}
	if runner.removeCalls != 0 || routes.deleteCalls != 0 {
		t.Fatalf("changed target compensated: remove=%d delete=%d", runner.removeCalls, routes.deleteCalls)
	}
}

func TestGatewayApplyFailurePreservesCompensationIdentity(t *testing.T) {
	plan := reviewGatewayPlan()
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{applyErr: errors.New("agent result unknown")}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	summary, err := applier.Apply(context.Background(), plan, reviewGatewayRequest(), []string{"127.0.0.1:8080"}, "", "")
	if err == nil || strings.TrimSpace(summary) == "" {
		t.Fatalf("summary = %q, err = %v", summary, err)
	}
	var stored gatewayPublicationSummary
	if json.Unmarshal([]byte(summary), &stored) != nil || !stored.RouteCreated ||
		stored.RouteID != plan.ID || stored.RouteRevision != 1 || stored.SitePath == "" {
		t.Fatalf("partial summary = %+v", stored)
	}
	runner.applyErr = nil
	if err := applier.Rollback(context.Background(), plan, summary); err != nil {
		t.Fatal(err)
	}
	if runner.removeCalls != 1 || routes.deleteCalls != 1 {
		t.Fatalf("remove=%d delete=%d", runner.removeCalls, routes.deleteCalls)
	}
}

func TestGatewayRollbackRejectsForeignSummary(t *testing.T) {
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	summary := strings.Replace(
		reviewGatewaySummary(t, plan, gateway.GatewayRoute{ID: plan.ID, GatewayID: "gw-one", Revision: 1}, site, true),
		`"planId":"`+plan.ID+`"`, `"planId":"other-plan"`, 1)
	routes := &reviewRouteService{}
	runner := &reviewTaskRunner{}
	applier := &publicationGatewayApplier{gateways: routes, tasks: runner, plans: reviewPlanLookup{plan: plan}}
	if err := applier.Rollback(context.Background(), plan, summary); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("err = %v", err)
	}
	if runner.removeCalls != 0 || routes.deleteCalls != 0 {
		t.Fatalf("foreign summary compensated")
	}
}
