package gateway_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
)

func TestInjectedPortErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.ports.nodes.setError(errors.New("dial failed password=secret"))
	if _, err := f.service.CreateGateway(ctx, "admin", "req-node-error", gateway.CreateGatewayInput{
		ID: "gw1", NodeID: "n1", Name: "edge",
	}); !errors.Is(err, gateway.ErrNodeLookupFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("node lookup err = %v", err)
	}
	f.ports.nodes.setError(nil)
	f.createGateway(t, "gw1", "edge", false)
	f.ports.groups.setError(errors.New("group lookup token=secret"))
	if _, err := f.service.CreateRoute(ctx, "admin", "req-group-error", "gw1", gateway.CreateRouteInput{
		ID: "route1", Domain: "example.com", ServiceGroupID: "sg1",
	}); !errors.Is(err, gateway.ErrServiceGroupLookupFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("group lookup err = %v", err)
	}
	f.ports.groups.setError(nil)
	f.ports.tasks.setError(errors.New("task submit token=secret"))
	if _, err := f.service.Provision(ctx, "admin", "req-task-error", "gw1", gateway.ProvisionInput{}); !errors.Is(err, gateway.ErrTaskSubmitFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("task submit err = %v", err)
	}
	if stored := f.gateway(t, "gw1"); stored.Status != gateway.StatusPending {
		t.Fatalf("failed submission changed gateway: %+v", stored)
	}
}

func TestNewServiceRequiresAuthorizer(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t)
	_, err := gateway.NewService(gateway.Options{
		DB: f.db, Repo: f.repo, Nodes: f.ports.nodes, ServiceGroups: f.ports.groups,
		Checker: f.ports.checker, Tasks: f.ports.tasks, Audit: f.recorder,
	})
	if !errors.Is(err, gateway.ErrMissingAuthorizer) {
		t.Fatalf("err = %v, want ErrMissingAuthorizer", err)
	}
}

func TestCreateGatewayPersistsAndDerivesEligibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	created := f.createGateway(t, "gw1", "edge", true)
	if created.Status != gateway.StatusPending || created.Revision != 1 {
		t.Fatalf("created = %+v", created)
	}
	if created.CountryCode != "HK" || !created.Enabled || created.ChecksPassed || created.NginxInstalled {
		t.Fatalf("created flags = %+v", created)
	}
	if created.SchedulingEligible(true) {
		t.Fatal("pending gateway must not be scheduling eligible")
	}
	eligible, err := f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || eligible {
		t.Fatalf("eligible = %v, %v", eligible, err)
	}
	updated, err := f.service.UpdateGateway(ctx, "admin", "req-update", "gw1", 1, gateway.UpdateGatewayInput{
		Name: "edge-renamed", Region: "hk", CountryCode: "hk", Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateGateway: %v", err)
	}
	if updated.Revision != 2 || updated.Name != "edge-renamed" {
		t.Fatalf("updated = %+v", updated)
	}
	checked, err := f.service.RecordChecks(ctx, "admin", "req-checks", "gw1", 2, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.25.4",
	})
	if err != nil {
		t.Fatalf("RecordChecks: %v", err)
	}
	if checked.Status != gateway.StatusReady || !checked.NginxInstalled || !checked.ChecksPassed || checked.Revision != 3 {
		t.Fatalf("checked = %+v", checked)
	}
	if checked.LastError != "" {
		t.Fatalf("last error = %q", checked.LastError)
	}
	eligible, err = f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || !eligible {
		t.Fatalf("eligible after checks = %v, %v", eligible, err)
	}
	f.ports.nodes.setOnline("n1", false)
	eligible, err = f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || eligible {
		t.Fatalf("offline eligible = %v, %v", eligible, err)
	}
	f.ports.nodes.setOnline("n1", true)
	disabled, err := f.service.UpdateGateway(ctx, "admin", "req-disable", "gw1", 3, gateway.UpdateGatewayInput{
		Name: "edge-renamed", Region: "hk", CountryCode: "hk", Enabled: false,
	})
	if err != nil || disabled.Enabled || disabled.Revision != 4 {
		t.Fatalf("disabled = %+v, %v", disabled, err)
	}
	eligible, err = f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || eligible {
		t.Fatalf("disabled eligible = %v, %v", eligible, err)
	}
	if got := f.auditCount(t, gateway.ActionCreate, "success"); got != 1 {
		t.Fatalf("create audit = %d", got)
	}
	if got := f.auditCount(t, gateway.ActionUpdate, "success"); got != 2 {
		t.Fatalf("update audit = %d", got)
	}
	if got := f.auditCount(t, gateway.ActionChecks, "success"); got != 1 {
		t.Fatalf("checks audit = %d", got)
	}
	listed, err := f.service.ListGateways(ctx, "admin")
	if err != nil || len(listed) != 1 || listed[0].ID != "gw1" {
		t.Fatalf("ListGateways = %+v, %v", listed, err)
	}
	f.authorizer.setDeny(true)
	if _, err := f.service.GetGateway(ctx, "admin", "gw1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied get err = %v", err)
	}
	if _, err := f.service.ListGateways(ctx, "admin"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied list err = %v", err)
	}
	if got := f.auditCount(t, gateway.ActionRead, "denied"); got != 2 {
		t.Fatalf("read denial audit = %d", got)
	}
}

func TestUpdateGatewayRevisionConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)
	if _, err := f.service.UpdateGateway(ctx, "admin", "req-1", "gw1", 1, gateway.UpdateGatewayInput{
		Name: "first", Region: "hk", CountryCode: "hk",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UpdateGateway(ctx, "admin", "req-2", "gw1", 1, gateway.UpdateGatewayInput{
		Name: "stale", Region: "hk", CountryCode: "hk",
	}); !errors.Is(err, gateway.ErrRevisionConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	stored := f.gateway(t, "gw1")
	if stored.Revision != 2 || stored.Name != "first" {
		t.Fatalf("stored after conflict = %+v", stored)
	}
	if _, err := f.service.RecordChecks(ctx, "admin", "req-3", "gw1", 1, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true,
	}); !errors.Is(err, gateway.ErrRevisionConflict) {
		t.Fatalf("stale checks err = %v", err)
	}
	if got := f.auditCount(t, gateway.ActionUpdate, "success"); got != 1 {
		t.Fatalf("update audit = %d", got)
	}
}

func TestCreateGatewayUnknownNodeAndAuthDenial(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	if _, err := f.service.CreateGateway(ctx, "admin", "req-missing", gateway.CreateGatewayInput{
		ID: "gw-missing", NodeID: "missing", Name: "edge",
	}); !errors.Is(err, gateway.ErrNodeNotFound) {
		t.Fatalf("unknown node err = %v", err)
	}
	if _, err := f.repo.GetGateway(ctx, f.db, "gw-missing"); !errors.Is(err, gateway.ErrGatewayNotFound) {
		t.Fatalf("unknown node wrote a gateway: %v", err)
	}

	denied := newGatewayFixtureAt(t, filepath.Join(t.TempDir(), "denied.db"), nil, &switchAuthorizer{deny: true})
	if _, err := denied.service.CreateGateway(ctx, "admin", "req-denied", gateway.CreateGatewayInput{
		ID: "gw1", NodeID: "n1", Name: "edge",
	}); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied err = %v", err)
	}
	if _, err := denied.repo.GetGateway(ctx, denied.db, "gw1"); !errors.Is(err, gateway.ErrGatewayNotFound) {
		t.Fatalf("denied create mutated storage: %v", err)
	}
	if got := denied.auditCount(t, gateway.ActionWrite, "denied"); got != 1 {
		t.Fatalf("denial audit = %d", got)
	}
	if got := denied.ports.tasks.callCount(); got != 0 {
		t.Fatalf("task submits = %d", got)
	}
}

func TestRecordChecksFailureDisablesAndRedacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", true)
	failed, err := f.service.RecordChecks(ctx, "admin", "req-fail", "gw1", 1, gateway.CheckReport{
		NginxInstalled: false, ChecksPassed: false,
		Error: "nginx failed token=supersecret Authorization: Bearer authsecret at https://user:pass@example.com",
	})
	if err != nil {
		t.Fatalf("RecordChecks: %v", err)
	}
	if failed.Status != gateway.StatusFailed || failed.Enabled || failed.ChecksPassed || failed.NginxInstalled {
		t.Fatalf("failed checks = %+v", failed)
	}
	if strings.Contains(failed.LastError, "supersecret") || strings.Contains(failed.LastError, "authsecret") ||
		strings.Contains(failed.LastError, "user:pass") {
		t.Fatalf("last error was not redacted: %q", failed.LastError)
	}
	if !strings.Contains(failed.LastError, "[redacted]") {
		t.Fatalf("last error = %q", failed.LastError)
	}
	eligible, err := f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || eligible {
		t.Fatalf("failed eligible = %v, %v", eligible, err)
	}
	recovered, err := f.service.RecordChecks(ctx, "admin", "req-ok", "gw1", failed.Revision, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.24.0",
	})
	if err != nil || recovered.Status != gateway.StatusReady || recovered.Enabled {
		t.Fatalf("recovered = %+v, %v", recovered, err)
	}
}

func TestGatewayPersistenceRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	ports := defaultGatewayPorts()
	first := newGatewayFixtureAt(t, path, ports, nil)
	first.createGateway(t, "gw1", "edge", true)
	if _, err := first.service.RecordChecks(ctx, "admin", "req-checks", "gw1", 1, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.25.4",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.service.CreateRoute(ctx, "admin", "req-route", "gw1", gateway.CreateRouteInput{
		ID: "route1", Domain: "example.com", Upstream: "127.0.0.1:8080", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newGatewayFixtureAt(t, path, ports, nil)
	stored := second.gateway(t, "gw1")
	if stored.Status != gateway.StatusReady || !stored.ChecksPassed || !stored.NginxInstalled || stored.Revision != 2 {
		t.Fatalf("stored after restart = %+v", stored)
	}
	eligible, err := second.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || !eligible {
		t.Fatalf("eligible after restart = %v, %v", eligible, err)
	}
	routes := second.routes(t, "gw1")
	if len(routes) != 1 || routes[0].Domain != "example.com" || routes[0].Upstream != "127.0.0.1:8080" {
		t.Fatalf("routes after restart = %+v", routes)
	}
	if got := second.auditCount(t, gateway.ActionChecks, "success"); got != 1 {
		t.Fatalf("durable audit = %d", got)
	}
}
