package monitoring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
)

type fakeRiskSource struct {
	risks []monitoring.TrafficRisk
	err   error
	calls int
}

func (f *fakeRiskSource) ListTrafficRisks(context.Context, string) ([]monitoring.TrafficRisk, error) {
	f.calls++
	return f.risks, f.err
}

func newMonitorServer(t *testing.T, authorizer interface {
	Authorize(context.Context, auth.Check) error
}, risks monitoring.TrafficRiskSource) *httptest.Server {
	t.Helper()
	h := newFixture(t, authorizer, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	return serveMonitorFixture(t, h, risks)
}

func serveMonitorFixture(t *testing.T, h fixture, risks monitoring.TrafficRiskSource) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if err := monitoring.RegisterHTTP(mux, h.service, risks, func(*http.Request) (monitoring.Principal, error) {
		return monitoring.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestHTTPMonitorProbeAndHistory(t *testing.T) {
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	server := serveMonitorFixture(t, h, &fakeRiskSource{})

	create := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
		"name": "Public API", "type": "http", "target": "https://example.com/health",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	})
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var monitor struct {
		ID string `json:"id"`
	}
	decode(t, create.Body, &monitor)
	if monitor.ID == "" {
		t.Fatalf("created monitor = %+v", monitor)
	}

	probe := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil)
	if probe.StatusCode != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", probe.StatusCode, probe.Body)
	}
	var probeBody struct {
		MonitorID           string   `json:"monitorId"`
		Status              string   `json:"status"`
		State               string   `json:"state"`
		LatencyMs           *float64 `json:"latencyMs"`
		StatusCode          *int     `json:"statusCode"`
		Error               string   `json:"error"`
		CheckedAt           string   `json:"checkedAt"`
		ConsecutiveFailures int      `json:"consecutiveFailures"`
	}
	decode(t, probe.Body, &probeBody)
	if probeBody.MonitorID != monitor.ID || probeBody.Status != "up" || probeBody.State != "up" ||
		probeBody.LatencyMs == nil || *probeBody.LatencyMs != 25 || probeBody.StatusCode == nil ||
		*probeBody.StatusCode != 200 || probeBody.CheckedAt == "" || probeBody.ConsecutiveFailures != 0 {
		t.Fatalf("probe = %+v", probeBody)
	}

	h.prober.set(monitoring.ProbeResult{Healthy: false, Reason: "connection refused"}, nil)
	down := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil)
	if down.StatusCode != http.StatusOK {
		t.Fatalf("down probe status = %d body=%s", down.StatusCode, down.Body)
	}
	decode(t, down.Body, &probeBody)
	if probeBody.Status != "down" || probeBody.State != "down" || probeBody.ConsecutiveFailures != 1 || probeBody.Error == "" {
		t.Fatalf("down probe = %+v", probeBody)
	}

	history := get(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history?limit=1")
	if history.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d body=%s", history.StatusCode, history.Body)
	}
	var page struct {
		MonitorID string `json:"monitorId"`
		Items     []struct {
			ID        int64  `json:"id"`
			Status    string `json:"status"`
			CheckedAt string `json:"checkedAt"`
		} `json:"items"`
		Limit int `json:"limit"`
	}
	decode(t, history.Body, &page)
	if page.MonitorID != monitor.ID || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].Status != "down" {
		t.Fatalf("history = %+v", page)
	}

	full := get(t, server.URL+"/api/v1/availability-monitors/"+monitor.ID+"/history")
	if full.StatusCode != http.StatusOK {
		t.Fatalf("alias history status = %d", full.StatusCode)
	}
	decode(t, full.Body, &page)
	if len(page.Items) != 2 || page.Items[0].Status != "down" || page.Items[1].Status != "up" {
		t.Fatalf("full history = %+v", page)
	}

	if badLimit := get(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history?limit=0"); badLimit.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d", badLimit.StatusCode)
	}
	if missing := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/missing/probe", http.MethodPost, nil); missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing probe status = %d", missing.StatusCode)
	}
	if missingHistory := get(t, server.URL+"/api/v1/monitoring/availability-monitors/missing/history"); missingHistory.StatusCode != http.StatusNotFound {
		t.Fatalf("missing history status = %d", missingHistory.StatusCode)
	}
}

func TestHTTPMonitorProbeDeniedAndFailsClosed(t *testing.T) {
	allowed := true
	deniable := authorizerFunc(func(context.Context, auth.Check) error {
		if allowed {
			return nil
		}
		return auth.ErrDenied
	})
	h := newFixture(t, deniable, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	server := serveMonitorFixture(t, h, &fakeRiskSource{})
	create := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
		"name": "Public API", "type": "http", "target": "https://example.com/health",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	})
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var monitor struct {
		ID string `json:"id"`
	}
	decode(t, create.Body, &monitor)
	allowed = false
	if denied := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil); denied.StatusCode != http.StatusForbidden {
		t.Fatalf("denied probe status = %d body=%s", denied.StatusCode, denied.Body)
	}
	if deniedHistory := get(t, server.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history"); deniedHistory.StatusCode != http.StatusForbidden {
		t.Fatalf("denied history status = %d body=%s", deniedHistory.StatusCode, deniedHistory.Body)
	}

	unconfigured := newFixture(t, nil, nil, nil)
	unconfiguredServer := serveMonitorFixture(t, unconfigured, &fakeRiskSource{})
	create = doJSON(t, unconfiguredServer.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
		"name": "No prober", "type": "http", "target": "https://example.com/health",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	})
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	decode(t, create.Body, &monitor)
	unavailable := doJSON(t, unconfiguredServer.URL+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil)
	if unavailable.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured probe status = %d body=%s", unavailable.StatusCode, unavailable.Body)
	}
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, unavailable.Body, &failure)
	if failure.Error.Code != "prober_unavailable" {
		t.Fatalf("unconfigured probe error = %+v", failure)
	}
}

func TestHTTPAvailabilityMonitorsAndTrafficRisks(t *testing.T) {
	risks := &fakeRiskSource{risks: []monitoring.TrafficRisk{
		{NodeID: "n-1", Kind: "quota_near_limit", Severity: "warning", Estimated: true},
	}}
	server := newMonitorServer(t, nil, risks)

	createBody := map[string]any{
		"name": "Public API", "type": "http", "target": "https://example.com/health",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	}
	create := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, createBody)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var created struct {
		ID      string `json:"id"`
		State   string `json:"state"`
		Enabled bool   `json:"enabled"`
	}
	decode(t, create.Body, &created)
	if created.ID == "" || created.State != "unknown" || !created.Enabled {
		t.Fatalf("created monitor = %+v", created)
	}

	patch := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/"+created.ID, http.MethodPatch, map[string]any{
		"state": "paused",
	})
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d body=%s", patch.StatusCode, patch.Body)
	}
	var paused struct {
		State   string `json:"state"`
		Enabled bool   `json:"enabled"`
	}
	decode(t, patch.Body, &paused)
	if paused.State != "paused" || paused.Enabled {
		t.Fatalf("paused monitor = %+v", paused)
	}

	full := doJSON(t, server.URL+"/api/v1/monitoring/availability-monitors/"+created.ID, http.MethodPatch, map[string]any{
		"id": created.ID, "name": "Public API", "type": "http", "target": "https://example.com/health",
		"intervalSeconds": 60, "timeoutSeconds": 5, "state": "pending", "lastCheckedAt": nil,
	})
	if full.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d body=%s", full.StatusCode, full.Body)
	}
	decode(t, full.Body, &paused)
	if paused.State == "paused" || !paused.Enabled {
		t.Fatalf("resumed monitor = %+v", paused)
	}

	list := get(t, server.URL+"/api/v1/monitoring/availability-monitors")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", list.StatusCode)
	}
	var items []map[string]any
	decode(t, list.Body, &items)
	if len(items) != 1 || items[0]["id"] != created.ID {
		t.Fatalf("list = %+v", items)
	}
	// The unprefixed contract/client path must serve the same data.
	alias := get(t, server.URL+"/api/v1/availability-monitors")
	if alias.StatusCode != http.StatusOK {
		t.Fatalf("alias list status = %d", alias.StatusCode)
	}

	riskResponse := get(t, server.URL+"/api/v1/monitoring/traffic-risks")
	if riskResponse.StatusCode != http.StatusOK || risks.calls != 1 {
		t.Fatalf("risks status = %d calls=%d", riskResponse.StatusCode, risks.calls)
	}
	var riskItems []monitoring.TrafficRisk
	decode(t, riskResponse.Body, &riskItems)
	if len(riskItems) != 1 || riskItems[0].NodeID != "n-1" || !riskItems[0].Estimated {
		t.Fatalf("risks = %+v", riskItems)
	}
	if alias := get(t, server.URL+"/api/v1/traffic-risks"); alias.StatusCode != http.StatusOK {
		t.Fatalf("alias risks status = %d", alias.StatusCode)
	}
}

func TestHTTPMonitorRejectsUnauthorizedForbiddenAndMissingSource(t *testing.T) {
	server := newMonitorServer(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}), nil)
	list := get(t, server.URL+"/api/v1/monitoring/availability-monitors")
	if list.StatusCode != http.StatusForbidden {
		t.Fatalf("denied list status = %d body=%s", list.StatusCode, list.Body)
	}
	risks := get(t, server.URL+"/api/v1/monitoring/traffic-risks")
	if risks.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing risk source status = %d body=%s", risks.StatusCode, risks.Body)
	}

	denyAll := newMonitorServer(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}), &fakeRiskSource{})
	forbidden := doJSON(t, denyAll.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
		"name": "API", "type": "http", "target": "https://example.com/",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	})
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("forbidden create status = %d body=%s", forbidden.StatusCode, forbidden.Body)
	}

	allowed := newMonitorServer(t, nil, &fakeRiskSource{})
	badTarget := doJSON(t, allowed.URL+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
		"name": "SSRF", "type": "http", "target": "http://127.0.0.1/",
		"intervalSeconds": 60, "timeoutSeconds": 5,
	})
	if badTarget.StatusCode != http.StatusBadRequest {
		t.Fatalf("forbidden target status = %d body=%s", badTarget.StatusCode, badTarget.Body)
	}
}

func TestHTTPMonitorAuthenticationFailure(t *testing.T) {
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	mux := http.NewServeMux()
	if err := monitoring.RegisterHTTP(mux, h.service, &fakeRiskSource{}, func(*http.Request) (monitoring.Principal, error) {
		return monitoring.Principal{}, errors.New("no session")
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	if response := get(t, server.URL+"/api/v1/monitoring/availability-monitors"); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
}

func TestAuditActionsIncludeServiceCatalog(t *testing.T) {
	actions := monitoring.AuditActions()
	if len(actions) != len(monitoring.ServiceActions()) {
		t.Fatalf("AuditActions = %v", actions)
	}
	found := map[string]bool{}
	for _, action := range actions {
		found[action] = true
	}
	for _, want := range []string{monitoring.ActionRead, monitoring.ActionWrite} {
		if !found[want] {
			t.Fatalf("missing action %q in %v", want, actions)
		}
	}
}

type httpResult struct {
	StatusCode int
	Body       []byte
}

func doJSON(t *testing.T, url, method string, body any) httpResult {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return readResult(t, response)
}

func get(t *testing.T, url string) httpResult {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return readResult(t, response)
}

func readResult(t *testing.T, response *http.Response) httpResult {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return httpResult{StatusCode: response.StatusCode, Body: buffer.Bytes()}
}

func decode(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

// ResponseController must override the ordinary HTTP server's shorter write
// deadline while the service still bounds network I/O to the configured timeout.
type probeDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
	failure  error
}

func (w *probeDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return w.failure
}
func TestManualProbeHasBoundedResponseDeadline(t *testing.T) {
	h := newFixture(t, nil, &fakeProber{result: healthyResult()}, &fakeNotifier{})
	input := validInput("slow-probe", "http", "https://example.com/health")
	input.TimeoutSeconds = 300
	if _, err := h.service.Create(context.Background(), "admin", "slow-create", input); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := monitoring.RegisterHTTP(mux, h.service, nil, func(*http.Request) (monitoring.Principal, error) {
		return monitoring.Principal{SubjectID: "admin", RequestID: "slow-probe"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	writer := &probeDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	mux.ServeHTTP(writer, httptest.NewRequest("POST", "/api/v1/availability-monitors/slow-probe/probe", nil))
	if writer.Code != 200 || writer.deadline.Before(before.Add(300*time.Second)) || writer.deadline.After(time.Now().Add(311*time.Second)) {
		t.Fatal(writer.Code, writer.deadline)
	}
	unavailable := &probeDeadlineWriter{ResponseRecorder: httptest.NewRecorder(), failure: errors.New("deadline failed")}
	mux.ServeHTTP(unavailable, httptest.NewRequest("POST", "/api/v1/availability-monitors/slow-probe/probe", nil))
	if unavailable.Code != 503 || len(h.prober.calls) != 1 {
		t.Fatal("deadline failure still performed I/O", unavailable.Code, len(h.prober.calls))
	}
}
