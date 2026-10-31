package monitoring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
