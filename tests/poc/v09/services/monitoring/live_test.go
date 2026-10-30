package monitoring_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, auth.Check) error { return nil }

type liveProber struct{}

func (liveProber) HTTP(ctx context.Context, spec monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	start := time.Now()
	client := &http.Client{Timeout: spec.Timeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.Target, nil)
	if err != nil {
		return monitoring.ProbeResult{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return monitoring.ProbeResult{}, err
	}
	defer response.Body.Close()
	latency := time.Since(start)
	code := response.StatusCode
	return monitoring.ProbeResult{
		Healthy: code >= 200 && code < 400, StatusCode: &code, Latency: &latency,
	}, nil
}

func (liveProber) TCP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return monitoring.ProbeResult{}, fmt.Errorf("tcp probe not used")
}

func (liveProber) TLS(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return monitoring.ProbeResult{}, fmt.Errorf("tls probe not used")
}

type recordingNotifier struct{ alerts []notifications.Alert }

func (n *recordingNotifier) SendAlert(_ context.Context, alert notifications.Alert) (notifications.Delivery, error) {
	n.alerts = append(n.alerts, alert)
	return notifications.Delivery{ID: int64(len(n.alerts)), Status: notifications.DeliverySubmitted}, nil
}

func TestLiveHTTPMonitorFailureAndRecovery(t *testing.T) {
	ctx := context.Background()
	address := "127.0.0.1:18081"
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Skipf("fixed test port unavailable: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})}
	go func() { _ = server.Serve(listener) }()

	db := testkit.NewDB(t, monitoring.Migrations(), audit.Migrations())
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
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	service, err := monitoring.NewService(monitoring.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: allowAuthorizer{},
		Prober: liveProber{}, Notifier: notifier, AllowedHosts: []string{"127.0.0.1/32"},
	})
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := service.Create(ctx, "admin", "req-monitor", monitoring.MonitorInput{
		ID: "mon-live", Name: "live nginx-like", Kind: monitoring.KindHTTP,
		Target: "http://" + address + "/", IntervalSeconds: 60, TimeoutSeconds: 3, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	check, updated, err := service.RunCheckNow(ctx, "admin", "req-check-1", monitor.ID)
	if err != nil || check.Status != monitoring.StateUp || updated.State != monitoring.StateUp {
		t.Fatalf("up check = %+v state=%s err=%v", check, updated.State, err)
	}
	if len(notifier.alerts) != 0 {
		t.Fatalf("first up produced %d alerts", len(notifier.alerts))
	}
	_ = server.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("listener did not close")
		}
		time.Sleep(20 * time.Millisecond)
	}
	check, updated, err = service.RunCheckNow(ctx, "admin", "req-check-2", monitor.ID)
	if err != nil || check.Status != monitoring.StateDown || updated.State != monitoring.StateDown {
		t.Fatalf("down check = %+v state=%s err=%v", check, updated.State, err)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("down alerts = %d", len(notifier.alerts))
	}

	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	check, updated, err = service.RunCheckNow(ctx, "admin", "req-check-3", monitor.ID)
	if err != nil || check.Status != monitoring.StateUp || updated.State != monitoring.StateUp {
		t.Fatalf("recovery check = %+v state=%s err=%v", check, updated.State, err)
	}
	if len(notifier.alerts) != 2 || notifier.alerts[1].Subject == "" {
		t.Fatalf("recovery alerts = %+v", notifier.alerts)
	}
}
