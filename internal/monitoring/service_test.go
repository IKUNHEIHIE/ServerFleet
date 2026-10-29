package monitoring_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

func allowAll(context.Context, auth.Check) error { return nil }

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeProber struct {
	mu     sync.Mutex
	result monitoring.ProbeResult
	err    error
	calls  []monitoring.TargetSpec
}

func (f *fakeProber) run(spec monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, spec)
	return f.result, f.err
}

func (f *fakeProber) HTTP(_ context.Context, spec monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return f.run(spec)
}

func (f *fakeProber) TCP(_ context.Context, spec monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return f.run(spec)
}

func (f *fakeProber) TLS(_ context.Context, spec monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return f.run(spec)
}

func (f *fakeProber) set(result monitoring.ProbeResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = result
	f.err = err
}

func (f *fakeProber) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProber) call(i int) monitoring.TargetSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

func healthyResult() monitoring.ProbeResult {
	statusCode := 200
	latency := 25 * time.Millisecond
	return monitoring.ProbeResult{Healthy: true, StatusCode: &statusCode, Latency: &latency}
}

type fakeNotifier struct {
	mu     sync.Mutex
	alerts []notifications.Alert
	err    error
}

func (f *fakeNotifier) SendAlert(_ context.Context, alert notifications.Alert) (notifications.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, alert)
	return notifications.Delivery{ID: int64(len(f.alerts)), Status: notifications.DeliveryProviderAccepted}, f.err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.alerts)
}

func (f *fakeNotifier) at(index int) notifications.Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alerts[index]
}

type fixture struct {
	db       *storage.DB
	repo     *monitoring.Repo
	audit    *audit.SQLRecorder
	service  *monitoring.Service
	clock    *testClock
	prober   *fakeProber
	notifier *fakeNotifier
}

func buildFixture(t *testing.T, db *storage.DB, clock *testClock, authorizer auth.Authorizer, prober monitoring.Prober, notifier monitoring.Notifier, allowed ...string) fixture {
	t.Helper()
	repo, err := monitoring.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(monitoring.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(allowAll)
	}
	service, err := monitoring.NewService(monitoring.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		Prober: prober, Notifier: notifier, AllowedHosts: allowed, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := prober.(*fakeProber)
	fakeNotifier, _ := notifier.(*fakeNotifier)
	return fixture{db: db, repo: repo, audit: recorder, service: service, clock: clock, prober: fake, notifier: fakeNotifier}
}

func newFixture(t *testing.T, authorizer auth.Authorizer, prober monitoring.Prober, notifier monitoring.Notifier, allowed ...string) fixture {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	db := testkit.NewDB(t, monitoring.Migrations(), audit.Migrations())
	return buildFixture(t, db, clock, authorizer, prober, notifier, allowed...)
}

func validInput(id, kind, target string) monitoring.MonitorInput {
	return monitoring.MonitorInput{
		ID: id, Name: "Monitor " + id, Kind: monitoring.Kind(kind), Target: target,
		IntervalSeconds: 30, TimeoutSeconds: 5, Enabled: true,
	}
}

func (f fixture) create(t *testing.T, id, kind, target string) monitoring.Monitor {
	t.Helper()
	monitor, err := f.service.Create(context.Background(), "admin", "req-"+id, validInput(id, kind, target))
	if err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return monitor
}

func TestSSRFMatrix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	cases := []struct {
		name   string
		kind   monitoring.Kind
		target string
		want   error
	}{
		{"loopback v4", monitoring.KindHTTP, "http://127.0.0.1/", monitoring.ErrTargetForbidden},
		{"private 10", monitoring.KindHTTP, "http://10.0.0.1/", monitoring.ErrTargetForbidden},
		{"private 172", monitoring.KindHTTP, "http://172.16.0.1/", monitoring.ErrTargetForbidden},
		{"private 192", monitoring.KindHTTP, "http://192.168.1.10/", monitoring.ErrTargetForbidden},
		{"metadata", monitoring.KindHTTP, "http://169.254.169.254/latest/meta-data/", monitoring.ErrTargetForbidden},
		{"metadata v6", monitoring.KindHTTP, "http://[fd00:ec2::254]/", monitoring.ErrTargetForbidden},
		{"loopback v6", monitoring.KindHTTP, "http://[::1]/", monitoring.ErrTargetForbidden},
		{"mapped loopback v6", monitoring.KindHTTP, "http://[::ffff:127.0.0.1]/", monitoring.ErrTargetForbidden},
		{"nat64 metadata", monitoring.KindHTTP, "http://[64:ff9b::a9fe:a9fe]/", monitoring.ErrTargetForbidden},
		{"link local v6", monitoring.KindHTTP, "http://[fe80::1]/", monitoring.ErrTargetForbidden},
		{"unspecified", monitoring.KindHTTP, "http://0.0.0.0/", monitoring.ErrTargetForbidden},
		{"multicast", monitoring.KindHTTP, "http://224.0.0.1/", monitoring.ErrTargetForbidden},
		{"localhost", monitoring.KindHTTP, "http://localhost/", monitoring.ErrTargetForbidden},
		{"localhost suffix", monitoring.KindHTTP, "http://app.localhost/", monitoring.ErrTargetForbidden},
		{"internal suffix", monitoring.KindHTTP, "http://db.internal/", monitoring.ErrTargetForbidden},
		{"single label", monitoring.KindHTTP, "http://intranet/", monitoring.ErrTargetForbidden},
		{"decimal v4", monitoring.KindHTTP, "http://2130706433/", monitoring.ErrInvalidTarget},
		{"hex v4", monitoring.KindHTTP, "http://0x7f.0.0.1/", monitoring.ErrTargetForbidden},
		{"malformed v4", monitoring.KindHTTP, "http://127.1/", monitoring.ErrInvalidTarget},
		{"userinfo", monitoring.KindHTTP, "http://user:pass@93.184.216.34/", monitoring.ErrTargetForbidden},
		{"ftp scheme", monitoring.KindHTTP, "ftp://93.184.216.34/", monitoring.ErrInvalidTarget},
		{"missing scheme", monitoring.KindHTTP, "93.184.216.34", monitoring.ErrInvalidTarget},
		{"zero port", monitoring.KindHTTP, "http://93.184.216.34:0/", monitoring.ErrInvalidTarget},
		{"huge port", monitoring.KindHTTP, "http://93.184.216.34:99999/", monitoring.ErrInvalidTarget},
		{"loopback tcp", monitoring.KindTCP, "127.0.0.1:443", monitoring.ErrTargetForbidden},
		{"private tcp", monitoring.KindTCP, "10.0.0.1:443", monitoring.ErrTargetForbidden},
		{"metadata tcp", monitoring.KindTCP, "169.254.169.254:443", monitoring.ErrTargetForbidden},
		{"loopback tcp v6", monitoring.KindTCP, "[::1]:443", monitoring.ErrTargetForbidden},
		{"localhost tcp", monitoring.KindTCP, "localhost:443", monitoring.ErrTargetForbidden},
		{"missing tcp port", monitoring.KindTCP, "93.184.216.34", monitoring.ErrInvalidTarget},
		{"bad tcp port", monitoring.KindTCP, "93.184.216.34:https", monitoring.ErrInvalidTarget},
		{"userinfo tcp", monitoring.KindTCP, "user@93.184.216.34:443", monitoring.ErrTargetForbidden},
		{"loopback tls", monitoring.KindTLS, "[::1]:443", monitoring.ErrTargetForbidden},
		{"private tls", monitoring.KindTLS, "10.0.0.1:443", monitoring.ErrTargetForbidden},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := fmt.Sprintf("bad-%d", index)
			_, err := h.service.Create(ctx, "admin", "req-"+id, validInput(id, string(tc.kind), tc.target))
			if !errors.Is(err, tc.want) {
				t.Fatalf("target %q err = %v, want %v", tc.target, err, tc.want)
			}
			if _, err := h.repo.Get(ctx, id); !errors.Is(err, monitoring.ErrMonitorNotFound) {
				t.Fatalf("rejected target persisted: %v", err)
			}
		})
	}
	public := []struct {
		id, kind, target string
	}{
		{"public-http", "http", "http://93.184.216.34/health"},
		{"public-https", "http", "https://example.com/health"},
		{"public-tcp", "tcp", "93.184.216.34:443"},
		{"public-tls", "tls", "93.184.216.34:8443"},
	}
	for _, tc := range public {
		if _, err := h.service.Create(ctx, "admin", "req-"+tc.id, validInput(tc.id, tc.kind, tc.target)); err != nil {
			t.Fatalf("public target %q rejected: %v", tc.target, err)
		}
	}
}

func TestSSRFAllowlistOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{},
		"10.0.0.0/8", "127.0.0.1")
	for _, tc := range []struct {
		id, kind, target string
	}{
		{"allowed-private", "http", "http://10.0.0.1/"},
		{"allowed-loopback", "http", "http://127.0.0.1:8080/"},
		{"allowed-tcp", "tcp", "10.1.2.3:443"},
	} {
		if _, err := h.service.Create(ctx, "admin", "req-"+tc.id, validInput(tc.id, tc.kind, tc.target)); err != nil {
			t.Fatalf("allowlisted target %q rejected: %v", tc.target, err)
		}
	}
	for _, tc := range []struct {
		id, kind, target string
	}{
		{"still-metadata", "http", "http://169.254.169.254/"},
		{"still-localhost", "http", "http://localhost/"},
		{"still-other-private", "http", "http://192.168.1.1/"},
	} {
		if _, err := h.service.Create(ctx, "admin", "req-"+tc.id, validInput(tc.id, tc.kind, tc.target)); err == nil {
			t.Fatalf("target %q unexpectedly allowed", tc.target)
		}
	}
	if _, err := monitoring.NewTargetPolicy([]string{"not a cidr/"}); !errors.Is(err, monitoring.ErrInvalidAllowedHosts) {
		t.Fatalf("invalid allowlist err = %v", err)
	}
}

func TestTransitionAlertExactlyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	notifier := &fakeNotifier{}
	h := newFixture(t, nil, prober, notifier)
	h.create(t, "m1", "http", "http://93.184.216.34/health")

	check, monitor, err := h.service.RunCheckNow(ctx, "admin", "req-1", "m1")
	if err != nil || check.Status != monitoring.StateUp || monitor.State != monitoring.StateUp {
		t.Fatalf("first check = %+v monitor=%+v err=%v", check, monitor, err)
	}
	if check.Latency == nil || *check.Latency != 25*time.Millisecond || check.StatusCode == nil || *check.StatusCode != 200 {
		t.Fatalf("first check sample = %+v", check)
	}
	if notifier.count() != 0 {
		t.Fatalf("unknown->up alerted: %d", notifier.count())
	}
	h.clock.Advance(10 * time.Second)
	prober.set(monitoring.ProbeResult{}, errors.New("connection refused"))
	check, monitor, err = h.service.RunCheckNow(ctx, "admin", "req-2", "m1")
	if err != nil || check.Status != monitoring.StateDown || monitor.State != monitoring.StateDown || monitor.ConsecutiveFailures != 1 {
		t.Fatalf("down check = %+v monitor=%+v err=%v", check, monitor, err)
	}
	if check.Latency != nil || check.StatusCode != nil {
		t.Fatalf("failed probe fabricated a sample: %+v", check)
	}
	if notifier.count() != 1 || !strings.Contains(notifier.at(0).Subject, "down") {
		t.Fatalf("down alert = %d %+v", notifier.count(), notifier.at(0))
	}
	downKey := notifier.at(0).DedupeKey
	if _, _, err := h.service.RunCheckNow(ctx, "admin", "req-3", "m1"); err != nil {
		t.Fatal(err)
	}
	if notifier.count() != 1 {
		t.Fatalf("repeated failure alerted again: %d", notifier.count())
	}
	h.clock.Advance(10 * time.Second)
	prober.set(healthyResult(), nil)
	check, monitor, err = h.service.RunCheckNow(ctx, "admin", "req-4", "m1")
	if err != nil || check.Status != monitoring.StateUp || monitor.State != monitoring.StateUp || monitor.ConsecutiveFailures != 0 {
		t.Fatalf("recovery check = %+v monitor=%+v err=%v", check, monitor, err)
	}
	if notifier.count() != 2 || !strings.Contains(notifier.at(1).Subject, "recovered") {
		t.Fatalf("recovery alert = %d %+v", notifier.count(), notifier.at(1))
	}
	if notifier.at(1).DedupeKey == downKey {
		t.Fatal("recovery reused the down dedupe key")
	}
	checks, err := h.service.ListChecks(ctx, "admin", "m1", 10)
	if err != nil || len(checks) != 4 || checks[0].Status != monitoring.StateUp {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: monitoring.ActionCheck})
	if err != nil || page.Total != 4 {
		t.Fatalf("check audit = %+v, %v", page, err)
	}
}

func TestRunDueTransitionAlertExactlyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	notifier := &fakeNotifier{}
	h := newFixture(t, nil, prober, notifier)
	h.create(t, "m1", "http", "http://93.184.216.34/health")
	results, err := h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 1 || results[0].State != monitoring.StateUp || results[0].Alerted {
		t.Fatalf("initial due = %+v, %v", results, err)
	}
	h.clock.Advance(30 * time.Second)
	prober.set(monitoring.ProbeResult{}, errors.New("refused"))
	results, err = h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 1 || results[0].State != monitoring.StateDown || !results[0].Alerted {
		t.Fatalf("down due = %+v, %v", results, err)
	}
	if notifier.count() != 1 {
		t.Fatalf("down alerts = %d", notifier.count())
	}
	h.clock.Advance(30 * time.Second)
	results, err = h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 1 || results[0].State != monitoring.StateDown || results[0].Alerted {
		t.Fatalf("repeat down due = %+v, %v", results, err)
	}
	if notifier.count() != 1 {
		t.Fatalf("repeat down alerts = %d", notifier.count())
	}
	h.clock.Advance(30 * time.Second)
	prober.set(healthyResult(), nil)
	results, err = h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 1 || results[0].State != monitoring.StateUp || !results[0].Alerted {
		t.Fatalf("recovery due = %+v, %v", results, err)
	}
	if notifier.count() != 2 {
		t.Fatalf("recovery alerts = %d", notifier.count())
	}
}

func TestMaintenanceSuppressesAlertsButKeepsStateAndChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	notifier := &fakeNotifier{}
	h := newFixture(t, nil, prober, notifier)
	h.create(t, "m1", "http", "http://93.184.216.34/health")
	if _, _, err := h.service.RunCheckNow(ctx, "admin", "req-up", "m1"); err != nil {
		t.Fatal(err)
	}
	input := validInput("m1", "http", "http://93.184.216.34/health")
	input.MaintenanceUntil = h.clock.Now().Add(time.Hour)
	if _, err := h.service.Update(ctx, "admin", "req-maint", "m1", input); err != nil {
		t.Fatal(err)
	}
	prober.set(monitoring.ProbeResult{}, errors.New("connection refused"))
	h.clock.Advance(30 * time.Second)
	check, monitor, err := h.service.RunCheckNow(ctx, "admin", "req-maint-check", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != monitoring.StateDown {
		t.Fatalf("maintenance check status = %s", check.Status)
	}
	if monitor.State != monitoring.StateUp || monitor.ConsecutiveFailures != 0 {
		t.Fatalf("maintenance changed state: %+v", monitor)
	}
	if monitor.LastCheckedAt.IsZero() || monitor.LastError == "" {
		t.Fatalf("maintenance check not persisted: %+v", monitor)
	}
	if notifier.count() != 0 {
		t.Fatalf("maintenance alerted: %d", notifier.count())
	}
	checks, err := h.service.ListChecks(ctx, "admin", "m1", 10)
	if err != nil || len(checks) != 2 {
		t.Fatalf("maintenance checks = %+v, %v", checks, err)
	}
	h.clock.Advance(2 * time.Hour)
	_, monitor, err = h.service.RunCheckNow(ctx, "admin", "req-after-maint", "m1")
	if err != nil || monitor.State != monitoring.StateDown {
		t.Fatalf("post-maintenance monitor = %+v, %v", monitor, err)
	}
	if notifier.count() != 1 {
		t.Fatalf("post-maintenance alert count = %d, want 1", notifier.count())
	}
}

func TestNilProberFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, nil, &fakeNotifier{})
	h.create(t, "m1", "http", "http://93.184.216.34/health")
	if _, _, err := h.service.RunCheckNow(ctx, "admin", "req-check", "m1"); !errors.Is(err, monitoring.ErrProberUnavailable) {
		t.Fatalf("RunCheckNow err = %v", err)
	}
	monitor, err := h.repo.Get(ctx, "m1")
	if err != nil || monitor.State != monitoring.StateUnknown || !monitor.LastCheckedAt.IsZero() || monitor.LastError != "" {
		t.Fatalf("nil prober mutated monitor: %+v, %v", monitor, err)
	}
	checks, err := h.repo.ListChecks(ctx, "m1", 10)
	if err != nil || len(checks) != 0 {
		t.Fatalf("nil prober persisted checks: %+v, %v", checks, err)
	}
	if _, err := h.service.RunDue(ctx, h.clock.Now()); !errors.Is(err, monitoring.ErrProberUnavailable) {
		t.Fatalf("RunDue err = %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: monitoring.ActionCheck})
	if err != nil || page.Total != 0 {
		t.Fatalf("nil prober audit = %+v, %v", page, err)
	}
}

func TestPersistenceAndRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fleet.db")
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	db := testkit.NewDBAt(t, path, monitoring.Migrations(), audit.Migrations())
	prober := &fakeProber{result: healthyResult()}
	notifier := &fakeNotifier{}
	h := buildFixture(t, db, clock, nil, prober, notifier)
	h.create(t, "m1", "http", "http://93.184.216.34/health")
	if _, _, err := h.service.RunCheckNow(ctx, "admin", "req-1", "m1"); err != nil {
		t.Fatal(err)
	}
	prober.set(monitoring.ProbeResult{}, errors.New("timeout"))
	if _, _, err := h.service.RunCheckNow(ctx, "admin", "req-2", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	h2 := buildFixture(t, reopened, clock, nil, prober, notifier)
	monitor, err := h2.service.Get(ctx, "admin", "m1")
	if err != nil || monitor.State != monitoring.StateDown || monitor.ConsecutiveFailures != 1 {
		t.Fatalf("restarted monitor = %+v, %v", monitor, err)
	}
	checks, err := h2.service.ListChecks(ctx, "admin", "m1", 10)
	if err != nil || len(checks) != 2 {
		t.Fatalf("restarted checks = %+v, %v", checks, err)
	}
}

func TestAuthorizationDenialAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }), &fakeProber{result: healthyResult()}, &fakeNotifier{})
	_, err := h.service.Create(ctx, "mallory", "req-deny", validInput("m-denied", "http", "http://93.184.216.34/"))
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want denied", err)
	}
	if _, err := h.repo.Get(ctx, "m-denied"); !errors.Is(err, monitoring.ErrMonitorNotFound) {
		t.Fatalf("denied write mutated storage: %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: monitoring.ActionWrite, Result: "denied"})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "mallory" {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestRunDueSkipsDisabledAndNotDue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	notifier := &fakeNotifier{}
	h := newFixture(t, nil, prober, notifier)
	h.create(t, "m1", "http", "http://93.184.216.34/one")
	h.create(t, "m2", "http", "http://93.184.216.34/two")
	disabled := validInput("m3", "http", "http://93.184.216.34/three")
	disabled.Enabled = false
	if _, err := h.service.Create(ctx, "admin", "req-m3", disabled); err != nil {
		t.Fatal(err)
	}
	input := validInput("m2", "http", "http://93.184.216.34/two")
	input.IntervalSeconds = 3600
	if _, err := h.service.Update(ctx, "admin", "req-m2", "m2", input); err != nil {
		t.Fatal(err)
	}
	results, err := h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 2 || prober.count() != 2 {
		t.Fatalf("initial due results=%+v calls=%d err=%v", results, prober.count(), err)
	}
	for _, result := range results {
		if result.State != monitoring.StateUp || result.Alerted {
			t.Fatalf("initial result = %+v", result)
		}
	}
	h.clock.Advance(10 * time.Second)
	results, err = h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 0 || prober.count() != 2 {
		t.Fatalf("not-due results=%+v calls=%d err=%v", results, prober.count(), err)
	}
	h.clock.Advance(21 * time.Second)
	results, err = h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 1 || results[0].MonitorID != "m1" || prober.count() != 3 {
		t.Fatalf("second due results=%+v calls=%d err=%v", results, prober.count(), err)
	}
	checks, err := h.service.ListChecks(ctx, "admin", "m3", 10)
	if err != nil || len(checks) != 0 {
		t.Fatalf("disabled monitor checks = %+v, %v", checks, err)
	}
	if notifier.count() != 0 {
		t.Fatalf("healthy due checks alerted: %d", notifier.count())
	}
}

func TestRunDueDispatchesProbeKindAndTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	h := newFixture(t, nil, prober, &fakeNotifier{})
	h.create(t, "a-http", "http", "http://93.184.216.34/")
	h.create(t, "b-tcp", "tcp", "93.184.216.34:80")
	h.create(t, "c-tls", "tls", "93.184.216.34:443")
	results, err := h.service.RunDue(ctx, h.clock.Now())
	if err != nil || len(results) != 3 || prober.count() != 3 {
		t.Fatalf("results=%+v calls=%d err=%v", results, prober.count(), err)
	}
	want := []struct {
		kind   monitoring.Kind
		target string
	}{
		{monitoring.KindHTTP, "http://93.184.216.34/"},
		{monitoring.KindTCP, "93.184.216.34:80"},
		{monitoring.KindTLS, "93.184.216.34:443"},
	}
	for index, expected := range want {
		call := prober.call(index)
		if call.Kind != expected.kind || call.Target != expected.target || call.Timeout != 5*time.Second {
			t.Fatalf("call %d = %+v, want %+v", index, call, expected)
		}
	}
}

func TestProbeErrorRedacted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prober := &fakeProber{result: healthyResult()}
	h := newFixture(t, nil, prober, &fakeNotifier{})
	h.create(t, "m1", "http", "http://93.184.216.34/health")
	prober.set(monitoring.ProbeResult{}, errors.New("dial https://user:pass@example.com/x failed token=topsecret"))
	check, monitor, err := h.service.RunCheckNow(ctx, "admin", "req-redact", "m1")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{check.Error, monitor.LastError} {
		if strings.Contains(value, "user:pass") || strings.Contains(value, "topsecret") {
			t.Fatalf("redaction failed: %q", value)
		}
		if !strings.Contains(value, "[REDACTED]") {
			t.Fatalf("redaction marker missing: %q", value)
		}
	}
}

func TestInvalidMonitorInputAndConflicts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	h.create(t, "m-existing", "http", "http://93.184.216.34/")
	cases := []struct {
		name  string
		input monitoring.MonitorInput
	}{
		{"empty name", monitoring.MonitorInput{ID: "bad1", Kind: monitoring.KindHTTP, Target: "http://93.184.216.34/", IntervalSeconds: 30, TimeoutSeconds: 5}},
		{"zero interval", monitoring.MonitorInput{ID: "bad2", Name: "x", Kind: monitoring.KindHTTP, Target: "http://93.184.216.34/", IntervalSeconds: 0, TimeoutSeconds: 5}},
		{"zero timeout", monitoring.MonitorInput{ID: "bad3", Name: "x", Kind: monitoring.KindHTTP, Target: "http://93.184.216.34/", IntervalSeconds: 30, TimeoutSeconds: 0}},
		{"bad kind", monitoring.MonitorInput{ID: "bad4", Name: "x", Kind: "udp", Target: "93.184.216.34:53", IntervalSeconds: 30, TimeoutSeconds: 5}},
		{"bad id", monitoring.MonitorInput{ID: "bad/5", Name: "x", Kind: monitoring.KindHTTP, Target: "http://93.184.216.34/", IntervalSeconds: 30, TimeoutSeconds: 5}},
	}
	for _, tc := range cases {
		if _, err := h.service.Create(ctx, "admin", "req-"+tc.name, tc.input); !errors.Is(err, monitoring.ErrInvalidMonitor) {
			t.Fatalf("%s err = %v", tc.name, err)
		}
	}
	if _, err := h.service.Create(ctx, "admin", "req-dup", validInput("m-existing", "http", "http://93.184.216.34/")); !errors.Is(err, monitoring.ErrMonitorExists) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, err := h.service.Update(ctx, "admin", "req-update", "m-existing", validInput("different", "http", "http://93.184.216.34/")); !errors.Is(err, monitoring.ErrInvalidMonitor) {
		t.Fatalf("mismatched update err = %v", err)
	}
	if _, err := h.service.Update(ctx, "admin", "req-missing", "missing", validInput("missing", "http", "http://93.184.216.34/")); !errors.Is(err, monitoring.ErrMonitorNotFound) {
		t.Fatalf("missing update err = %v", err)
	}
	if err := h.service.Delete(ctx, "admin", "req-delete", "m-existing"); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Delete(ctx, "admin", "req-delete", "m-existing"); !errors.Is(err, monitoring.ErrMonitorNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	if _, err := h.service.ListChecks(ctx, "admin", "m-existing", 1); !errors.Is(err, monitoring.ErrMonitorNotFound) {
		t.Fatalf("checks after delete err = %v", err)
	}
}
