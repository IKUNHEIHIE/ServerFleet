package monitoring_test

import (
	"context"
	"serverfleet.local/serverfleet/internal/monitoring"
	"testing"
	"time"
)

type remoteFixtureProbe struct {
	result monitoring.ProbeResult
	actor  string
	node   string
}

func (p *remoteFixtureProbe) Probe(_ context.Context, m monitoring.Monitor, actor string, manual bool) (monitoring.ProbeResult, error) {
	p.actor = actor
	p.node = m.SourceNodeID
	return p.result, nil
}
func TestRemoteUnknownPreservesLastBusinessStateAndBreaksFailureSequence(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	remote := &remoteFixtureProbe{result: monitoring.ProbeResult{Healthy: true, TaskID: "probe-up"}}
	service, e := monitoring.NewService(monitoring.Options{DB: h.db, Repo: h.repo, Audit: h.audit, Authorizer: authorizerFunc(allowAll), Prober: h.prober, RemoteProber: remote, SourceValidator: func(context.Context, string, string) error { return nil }, Notifier: h.notifier, Now: h.clock.Now})
	if e != nil {
		t.Fatal(e)
	}
	h.service = service
	input := validInput("remote", "http", "https://example.com/")
	input.SourceNodeID = "n1"
	input.FailureThreshold = 2
	m, e := service.Create(ctx, "admin", "remote-create", input)
	if e != nil {
		t.Fatal(e)
	}
	run := func() (monitoring.Check, monitoring.Monitor) {
		t.Helper()
		h.clock.Advance(time.Minute)
		c, m, e := service.RunCheckNow(ctx, "operator", "remote-manual", "remote")
		if e != nil {
			t.Fatal(e)
		}
		return c, m
	}
	_, m = run()
	if m.State != monitoring.StateUp || remote.actor != "operator" || remote.node != "n1" {
		t.Fatal(m, remote)
	}
	remote.result = monitoring.ProbeResult{Healthy: false, TaskID: "first-down"}
	_, m = run()
	if m.State != monitoring.StateUp || m.ConsecutiveFailures != 1 {
		t.Fatal("threshold ignored", m)
	}
	remote.result = monitoring.ProbeResult{Unknown: true, TaskID: "source-unknown", Reason: "probe_source_offline"}
	c, m := run()
	if c.Status != monitoring.StateUnknown || c.TaskID != "source-unknown" || c.SourceNodeID != "n1" || !m.LastProbeUnknown || m.State != monitoring.StateUp || m.ConsecutiveFailures != 0 || h.notifier.count() != 0 {
		t.Fatal("source failure became business outage", c, m)
	}
	checks, e := service.ListChecks(ctx, "admin", "remote", 20)
	if e != nil || checks[0].TaskID != "source-unknown" || checks[0].Status != monitoring.StateUnknown {
		t.Fatal("receipt provenance lost", checks, e)
	}
	remote.result = monitoring.ProbeResult{TaskID: "second-down"}
	_, m = run()
	if m.State != monitoring.StateUp || m.ConsecutiveFailures != 1 {
		t.Fatal("unknown did not break consecutive failures", m)
	}
	_, m = run()
	if m.State != monitoring.StateDown || m.LastProbeUnknown || h.notifier.count() != 1 {
		t.Fatal("real target failure did not alert", m, h.notifier.count())
	}
	input.SourceNodeID = "n2"
	m, e = service.Update(ctx, "admin", "source-change", "remote", input)
	if e != nil || m.SourceNodeID != "n2" || m.State != monitoring.StateUnknown {
		t.Fatal("source change did not reset observations", m, e)
	}
}
