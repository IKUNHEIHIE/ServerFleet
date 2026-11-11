package monitoring_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
	"testing"
	"time"
)

type queuedNotifier struct {
	status notifications.DeliveryStatus
	calls  int
}

func (n *queuedNotifier) SendAlert(_ context.Context, _ notifications.Alert) (notifications.Delivery, error) {
	n.calls++
	return notifications.Delivery{ID: 7, TaskID: "notification-task", Status: n.status}, nil
}
func TestPersistedMonitorThresholdsOutboxAndQueuedIsNotAccepted(t *testing.T) {
	ctx := context.Background()
	probe := &fakeProber{result: monitoring.ProbeResult{Healthy: false, Reason: "synthetic down"}}
	notify := &queuedNotifier{status: notifications.DeliveryQueued}
	h := newFixture(t, nil, probe, notify)
	input := validInput("m-threshold", "http", "http://93.184.216.34/")
	input.FailureThreshold = 2
	input.RecoveryThreshold = 2
	input.NotifyChannels = []notifications.Channel{notifications.ChannelSMTP}
	m, e := h.service.Create(ctx, "admin", "create-threshold", input)
	if e != nil {
		t.Fatal(e)
	}
	m, e = h.service.Get(ctx, "admin", m.ID)
	if e != nil || m.FailureThreshold != 2 || m.RecoveryThreshold != 2 || len(m.NotifyChannels) != 1 {
		t.Fatal("settings not persisted", m, e)
	}
	run := func() monitoring.RunResult {
		t.Helper()
		rs, e := h.service.RunDue(ctx, h.clock.Now())
		if e != nil || len(rs) != 1 {
			t.Fatalf("run %+v %v", rs, e)
		}
		h.clock.Advance(time.Minute)
		return rs[0]
	}
	if r := run(); r.State != monitoring.StateUnknown || r.Alerted {
		t.Fatal("first failure transitioned before threshold", r)
	}
	if r := run(); r.State != monitoring.StateDown || r.Alerted {
		t.Fatal("queued confused with provider accepted", r)
	}
	events, e := h.service.AlertEvents(ctx, "admin", m.ID, 20)
	if e != nil || len(events) != 1 || events[0].State != "queued" || events[0].TaskID != "notification-task" || events[0].Alert.Channel != notifications.ChannelSMTP {
		t.Fatal("outbox", events, e)
	}
	// Composition restart uses the durable queued event; it does not need a new transition.
	restarted := buildFixture(t, h.db, h.clock, authorizerFunc(allowAll), probe, notify)
	notify.status = notifications.DeliveryProviderAccepted
	if e = restarted.service.FlushAlerts(ctx, 20); e != nil {
		t.Fatal(e)
	}
	events, _ = restarted.service.AlertEvents(ctx, "admin", m.ID, 20)
	if events[0].State != "provider_accepted" {
		t.Fatal("restart did not finish alert", events)
	}
	probe.set(healthyResult(), nil)
	if r := run(); r.State != monitoring.StateDown {
		t.Fatal("recovered too early", r)
	}
	if r := run(); r.State != monitoring.StateUp || !r.Alerted {
		t.Fatal("recovery threshold", r)
	}
}

type callbackProber struct {
	check func() (monitoring.ProbeResult, error)
}

func (p callbackProber) HTTP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return p.check()
}
func (p callbackProber) TCP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return p.check()
}
func (p callbackProber) TLS(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return p.check()
}
func TestProbeCannotCommitAfterConfigurationChanges(t *testing.T) {
	ctx := context.Background()
	var h fixture
	p := callbackProber{check: func() (monitoring.ProbeResult, error) {
		input := validInput("m-race", "http", "http://93.184.216.35/")
		if _, e := h.service.Update(ctx, "admin", "change-during-probe", "m-race", input); e != nil {
			t.Fatal(e)
		}
		return healthyResult(), nil
	}}
	h = newFixture(t, nil, p, &fakeNotifier{})
	h.create(t, "m-race", "http", "http://93.184.216.34/")
	if _, _, e := h.service.RunCheckNow(ctx, "admin", "stale-probe", "m-race"); !errors.Is(e, monitoring.ErrObservationChanged) {
		t.Fatal("stale probe accepted", e)
	}
	checks, e := h.service.ListChecks(ctx, "admin", "m-race", 20)
	if e != nil || len(checks) != 0 {
		t.Fatal("stale history committed", checks, e)
	}
	events, e := h.service.AlertEvents(ctx, "admin", "m-race", 20)
	if e != nil || len(events) != 0 {
		t.Fatal("stale alert committed", events, e)
	}
}
func TestMonitorSchedulerUsesBoundedRoundRobin(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	for i := 0; i < 60; i++ {
		h.create(t, fmt.Sprintf("m-%02d", i), "http", "http://93.184.216.34/")
	}
	first, e := h.service.RunDue(ctx, h.clock.Now())
	if e != nil || len(first) != 50 {
		t.Fatal("first budget", len(first), e)
	}
	second, e := h.service.RunDue(ctx, h.clock.Now())
	if e != nil || len(second) != 10 {
		t.Fatal("later monitors starved", len(second), e)
	}
}

type statusReaderNotifier struct {
	db       *storage.DB
	queryErr error
	calls    int
}

func (n *statusReaderNotifier) SendAlert(context.Context, notifications.Alert) (notifications.Delivery, error) {
	return notifications.Delivery{ID: 1, Status: notifications.DeliveryQueued}, nil
}
func (n *statusReaderNotifier) DeliveryStatus(ctx context.Context, id int64) (notifications.Delivery, error) {
	n.calls++
	var value int
	n.queryErr = n.db.QueryRow(ctx, `SELECT 1`).Scan(&value)
	return notifications.Delivery{ID: id, Status: notifications.DeliveryProviderAccepted}, n.queryErr
}
func TestAlertHistoryReleasesConnectionBeforeReadingDeliveryStatus(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "one-connection.db"), MaxOpenConns: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	migrations, e := storage.NewMigrator(monitoring.Migrations(), audit.Migrations())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrations.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	clock := &testClock{now: time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC)}
	prober := &fakeProber{result: healthyResult()}
	notifier := &statusReaderNotifier{db: db}
	h := buildFixture(t, db, clock, authorizerFunc(allowAll), prober, notifier)
	h.create(t, "single-connection", "http", "http://93.184.216.34/")
	if _, _, e = h.service.RunCheckNow(ctx, "admin", "healthy", "single-connection"); e != nil {
		t.Fatal(e)
	}
	prober.set(monitoring.ProbeResult{Reason: "synthetic down"}, nil)
	if _, _, e = h.service.RunCheckNow(ctx, "admin", "down", "single-connection"); e != nil {
		t.Fatal(e)
	}
	limited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	events, e := h.service.AlertEvents(limited, "admin", "single-connection", 20)
	if e != nil || notifier.queryErr != nil || notifier.calls != 1 || len(events) != 1 || events[0].State != "provider_accepted" {
		t.Fatal("history held a connection during nested delivery read", events, e, notifier.queryErr, notifier.calls)
	}
}
