package gateway_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
)

func TestRouteDomainAndTargetValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)

	invalidDomains := []string{
		"", "https://example.com", "example.com/path", "user@example.com",
		"example.com:443", "*example.com", "foo.*.example.com", "bad_domain.example.com",
		"-bad.example.com", "bad-.example.com", "example", "example.com.", "*.com",
		"*.example", "example..com",
	}
	for index, domain := range invalidDomains {
		input := validRouteInput("bad-domain-"+string(rune('a'+index)), domain)
		if _, err := f.service.CreateRoute(ctx, "admin", "req-bad-domain", "gw1", input); !errors.Is(err, gateway.ErrInvalidRoute) {
			t.Fatalf("domain %q err = %v", domain, err)
		}
	}

	both := validRouteInput("bad-both", "both.example.com")
	both.ServiceGroupID = "sg1"
	if _, err := f.service.CreateRoute(ctx, "admin", "req-both", "gw1", both); !errors.Is(err, gateway.ErrInvalidRoute) {
		t.Fatalf("both targets err = %v", err)
	}
	neither := gateway.CreateRouteInput{ID: "bad-neither", Domain: "neither.example.com"}
	if _, err := f.service.CreateRoute(ctx, "admin", "req-neither", "gw1", neither); !errors.Is(err, gateway.ErrInvalidRoute) {
		t.Fatalf("neither target err = %v", err)
	}
	invalidUpstreams := []string{
		"example.com", "https://example.com:443", "user@example.com:80",
		"example.com:0", "example.com:70000", "example.com:abc", "::1:80",
		"bad_host:80",
	}
	for index, upstream := range invalidUpstreams {
		input := gateway.CreateRouteInput{
			ID: "bad-upstream-" + string(rune('a'+index)), Domain: "upstream.example.com", Upstream: upstream,
		}
		if _, err := f.service.CreateRoute(ctx, "admin", "req-bad-upstream", "gw1", input); !errors.Is(err, gateway.ErrInvalidRoute) {
			t.Fatalf("upstream %q err = %v", upstream, err)
		}
	}
	if routes := f.routes(t, "gw1"); len(routes) != 0 {
		t.Fatalf("invalid routes persisted: %+v", routes)
	}

	created, err := f.service.CreateRoute(ctx, "admin", "req-wild", "gw1", gateway.CreateRouteInput{
		ID: "wild", Domain: "*.Example.COM", Upstream: "UP.Example.com:8080", HTTPS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Domain != "*.example.com" || created.Upstream != "up.example.com:8080" || !created.HTTPS || created.Enabled {
		t.Fatalf("wildcard route = %+v", created)
	}
	shop, err := f.service.CreateRoute(ctx, "admin", "req-shop", "gw1", gateway.CreateRouteInput{
		ID: "shop", Domain: "Shop.Example.COM", Upstream: "127.0.0.1:8080",
	})
	if err != nil || shop.Domain != "shop.example.com" {
		t.Fatalf("shop route = %+v, %v", shop, err)
	}
	if _, err := f.service.CreateRoute(ctx, "admin", "req-duplicate", "gw1", gateway.CreateRouteInput{
		ID: "duplicate", Domain: "shop.example.com", Upstream: "127.0.0.1:9090",
	}); !errors.Is(err, gateway.ErrRouteDomainTaken) {
		t.Fatalf("duplicate domain err = %v", err)
	}
	if routes := f.routes(t, "gw1"); len(routes) != 2 {
		t.Fatalf("routes = %+v", routes)
	}
	if got := f.auditCount(t, gateway.ActionRouteCreate, "success"); got != 2 {
		t.Fatalf("route create audit = %d", got)
	}
}

func TestRouteServiceGroupSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)

	created, err := f.service.CreateRoute(ctx, "admin", "req-group", "gw1", gateway.CreateRouteInput{
		ID: "grouped", Domain: "api.example.com", ServiceGroupID: "sg1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ServiceGroupID != "sg1" || created.ServiceGroupRevision != 7 || created.Upstream != "" {
		t.Fatalf("grouped route = %+v", created)
	}
	f.ports.groups.setRevision("sg1", 9)
	updated, err := f.service.UpdateRoute(ctx, "admin", "req-group-update", "gw1", "grouped", 1, gateway.UpdateRouteInput{
		Domain: "api.example.com", ServiceGroupID: "sg1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ServiceGroupRevision != 9 || updated.Revision != 2 {
		t.Fatalf("updated grouped route = %+v", updated)
	}
	toUpstream, err := f.service.UpdateRoute(ctx, "admin", "req-upstream-update", "gw1", "grouped", 2, gateway.UpdateRouteInput{
		Domain: "api.example.com", Upstream: "10.0.0.5:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if toUpstream.ServiceGroupID != "" || toUpstream.ServiceGroupRevision != 0 || toUpstream.Upstream != "10.0.0.5:8080" {
		t.Fatalf("upstream route = %+v", toUpstream)
	}
	if _, err := f.service.CreateRoute(ctx, "admin", "req-unknown-group", "gw1", gateway.CreateRouteInput{
		ID: "unknown-group", Domain: "missing.example.com", ServiceGroupID: "does-not-exist",
	}); !errors.Is(err, gateway.ErrServiceGroupNotFound) {
		t.Fatalf("unknown group err = %v", err)
	}
	if routes := f.routes(t, "gw1"); len(routes) != 1 {
		t.Fatalf("routes = %+v", routes)
	}
}

func TestRouteEnableRequiresChecker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	withoutChecker := defaultGatewayPorts()
	withoutChecker.checker = nil
	nilFixture := newGatewayFixtureAt(t, filepath.Join(t.TempDir(), "nil-checker.db"), withoutChecker, nil)
	nilFixture.createGateway(t, "gw1", "edge", false)
	if _, err := nilFixture.service.CreateRoute(ctx, "admin", "req-enabled", "gw1", gateway.CreateRouteInput{
		ID: "enabled", Domain: "enabled.example.com", Upstream: "127.0.0.1:8080", Enabled: true,
	}); !errors.Is(err, gateway.ErrCheckerUnavailable) {
		t.Fatalf("nil checker err = %v", err)
	}
	if routes := nilFixture.routes(t, "gw1"); len(routes) != 0 {
		t.Fatalf("nil checker persisted route: %+v", routes)
	}
	disabled, err := nilFixture.service.CreateRoute(ctx, "admin", "req-disabled", "gw1", gateway.CreateRouteInput{
		ID: "disabled", Domain: "disabled.example.com", Upstream: "127.0.0.1:8080",
	})
	if err != nil || disabled.Enabled {
		t.Fatalf("disabled route = %+v, %v", disabled, err)
	}

	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)
	f.ports.checker.set(gateway.CheckResult{Passed: false}, nil)
	if _, err := f.service.CreateRoute(ctx, "admin", "req-fail", "gw1", gateway.CreateRouteInput{
		ID: "failed", Domain: "failed.example.com", Upstream: "127.0.0.1:8080", Enabled: true,
	}); !errors.Is(err, gateway.ErrRouteCheckFailed) {
		t.Fatalf("failed check err = %v", err)
	}
	f.ports.checker.set(gateway.CheckResult{Passed: false, PortConflict: true}, nil)
	if _, err := f.service.CreateRoute(ctx, "admin", "req-conflict", "gw1", gateway.CreateRouteInput{
		ID: "conflict", Domain: "conflict.example.com", Upstream: "127.0.0.1:8080", Enabled: true,
	}); !errors.Is(err, gateway.ErrRoutePortConflict) {
		t.Fatalf("port conflict err = %v", err)
	}
	f.ports.checker.set(gateway.CheckResult{}, errors.New("checker exploded token=secret"))
	if _, err := f.service.CreateRoute(ctx, "admin", "req-error", "gw1", gateway.CreateRouteInput{
		ID: "checker-error", Domain: "error.example.com", Upstream: "127.0.0.1:8080", Enabled: true,
	}); !errors.Is(err, gateway.ErrRouteCheckFailed) {
		t.Fatalf("checker error err = %v", err)
	}
	if routes := f.routes(t, "gw1"); len(routes) != 0 {
		t.Fatalf("failed enables persisted: %+v", routes)
	}

	f.ports.checker.set(gateway.CheckResult{Passed: true}, nil)
	enabled, err := f.service.CreateRoute(ctx, "admin", "req-good", "gw1", gateway.CreateRouteInput{
		ID: "good", Domain: "good.example.com", Upstream: "127.0.0.1:8080", Enabled: true, HTTPS: true,
	})
	if err != nil || !enabled.Enabled {
		t.Fatalf("enabled route = %+v, %v", enabled, err)
	}
	f.ports.checker.mu.Lock()
	lastSpec := f.ports.checker.calls[len(f.ports.checker.calls)-1]
	f.ports.checker.mu.Unlock()
	if lastSpec.GatewayID != "gw1" || lastSpec.NodeID != "n1" || lastSpec.Domain != "good.example.com" || !lastSpec.HTTPS {
		t.Fatalf("checker spec = %+v", lastSpec)
	}
	pending, err := f.service.CreateRoute(ctx, "admin", "req-pending", "gw1", gateway.CreateRouteInput{
		ID: "pending", Domain: "pending.example.com", Upstream: "127.0.0.1:9090",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.ports.checker.set(gateway.CheckResult{Passed: false}, nil)
	if _, err := f.service.UpdateRoute(ctx, "admin", "req-enable", "gw1", "pending", 1, gateway.UpdateRouteInput{
		Domain: "pending.example.com", Upstream: "127.0.0.1:9090", Enabled: true,
	}); !errors.Is(err, gateway.ErrRouteCheckFailed) {
		t.Fatalf("enable failure err = %v", err)
	}
	stored, err := f.repo.GetRoute(ctx, f.db, "pending")
	if err != nil || stored.Enabled || stored.Revision != 1 {
		t.Fatalf("failed enable changed route: %+v, %v", stored, err)
	}
	f.ports.checker.set(gateway.CheckResult{Passed: true}, nil)
	if _, err := f.service.UpdateRoute(ctx, "admin", "req-enable-ok", "gw1", "pending", 1, gateway.UpdateRouteInput{
		Domain: "pending.example.com", Upstream: "127.0.0.1:9090", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	stored, err = f.repo.GetRoute(ctx, f.db, "pending")
	if err != nil || !stored.Enabled || stored.Revision != 2 {
		t.Fatalf("successful enable = %+v, %v", stored, err)
	}
	if pending.Revision != 1 {
		t.Fatalf("unexpected pending revision = %d", pending.Revision)
	}
}

func TestRouteUpdateDeleteAndRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)
	created, err := f.service.CreateRoute(ctx, "admin", "req-create", "gw1", gateway.CreateRouteInput{
		ID: "route1", Domain: "example.com", Upstream: "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.service.UpdateRoute(ctx, "admin", "req-update", "gw1", created.ID, created.Revision, gateway.UpdateRouteInput{
		Domain: "www.example.com", Upstream: "127.0.0.1:8081",
	})
	if err != nil || updated.Revision != 2 || updated.Domain != "www.example.com" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if _, err := f.service.UpdateRoute(ctx, "admin", "req-stale", "gw1", created.ID, 1, gateway.UpdateRouteInput{
		Domain: "stale.example.com", Upstream: "127.0.0.1:8082",
	}); !errors.Is(err, gateway.ErrRevisionConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	if err := f.service.DeleteRoute(ctx, "admin", "req-stale-delete", "gw1", created.ID, 1); !errors.Is(err, gateway.ErrRevisionConflict) {
		t.Fatalf("stale delete err = %v", err)
	}
	got, err := f.service.GetRoute(ctx, "admin", created.ID)
	if err != nil || got.Domain != "www.example.com" || got.Revision != 2 {
		t.Fatalf("get route = %+v, %v", got, err)
	}
	listed, err := f.service.ListRoutes(ctx, "admin", "gw1")
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list routes = %+v, %v", listed, err)
	}
	if err := f.service.DeleteRoute(ctx, "admin", "req-delete", "gw1", created.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GetRoute(ctx, f.db, created.ID); !errors.Is(err, gateway.ErrRouteNotFound) {
		t.Fatalf("route still present: %v", err)
	}
	if got := f.auditCount(t, gateway.ActionRouteUpdate, "success"); got != 1 {
		t.Fatalf("route update audit = %d", got)
	}
	if got := f.auditCount(t, gateway.ActionRouteDelete, "success"); got != 1 {
		t.Fatalf("route delete audit = %d", got)
	}
	f.authorizer.setDeny(true)
	if _, err := f.service.ListRoutes(ctx, "admin", "gw1"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied list err = %v", err)
	}
}
