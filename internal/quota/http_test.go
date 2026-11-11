package quota_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/quota"
)

func newQuotaServer(t *testing.T, authorizer auth.Authorizer) *httptest.Server {
	t.Helper()
	h := newFixture(t, authorizer, &fakeNotifier{})
	mux := http.NewServeMux()
	if err := quota.RegisterHTTP(mux, h.service, func(*http.Request) (quota.Principal, error) {
		return quota.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestHTTPTrafficQuotaRoundTrip(t *testing.T) {
	h := newFixture(t, nil, &fakeNotifier{})
	mux := http.NewServeMux()
	if err := quota.RegisterHTTP(mux, h.service, func(*http.Request) (quota.Principal, error) {
		return quota.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	missing := quotaGet(t, server.URL+"/api/v1/nodes/n1/traffic-quota")
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing policy status = %d body=%s", missing.StatusCode, missing.Body)
	}

	put := quotaJSON(t, server.URL+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
		"nodeId": "n1", "quotaGb": 100, "thresholdsPct": []int{70, 85, 95},
		"cycleResetDay": 1, "timezone": "UTC", "direction": "combined",
		"source": "agent_estimate", "remindDays": []int{30, 7, 1},
	})
	if put.StatusCode != http.StatusOK {
		t.Fatalf("put status = %d body=%s", put.StatusCode, put.Body)
	}
	var policy struct {
		NodeID         string  `json:"nodeId"`
		QuotaBytes     int64   `json:"quotaBytes"`
		QuotaGb        float64 `json:"quotaGb"`
		WarningPercent int     `json:"warningPercent"`
		ThresholdsPct  []int   `json:"thresholdsPct"`
		Direction      string  `json:"direction"`
		Estimated      bool    `json:"estimated"`
	}
	quotaDecode(t, put.Body, &policy)
	const wantBytes = int64(100_000_000_000)
	if policy.NodeID != "n1" || policy.QuotaBytes != wantBytes || policy.WarningPercent != 70 ||
		len(policy.ThresholdsPct) != 3 || policy.ThresholdsPct[0] != 70 || policy.Direction != "combined" {
		t.Fatalf("policy = %+v", policy)
	}

	h.record(t, "n1", 40e9, 20e9, quota.SourceAgent)
	get := quotaGet(t, server.URL+"/api/v1/nodes/n1/traffic-quota")
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d body=%s", get.StatusCode, get.Body)
	}
	var view struct {
		QuotaGb   float64  `json:"quotaGb"`
		UsedBytes *float64 `json:"usedBytes"`
		UsedGb    *float64 `json:"usedGb"`
		Source    string   `json:"source"`
	}
	quotaDecode(t, get.Body, &view)
	if view.QuotaGb != 100 || view.UsedBytes == nil || *view.UsedBytes != 60e9 ||
		view.UsedGb == nil || *view.UsedGb != 60 || view.Source != string(quota.SourceAgent) {
		t.Fatalf("quota view = %+v", view)
	}
}

func TestHTTPTrafficQuotaRejectsInvalidAndUnauthorized(t *testing.T) {
	server := newQuotaServer(t, nil)
	badDirection := quotaJSON(t, server.URL+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
		"quotaGb": 10, "warningPercent": 80, "direction": "unsupported",
	})
	if badDirection.StatusCode != http.StatusBadRequest {
		t.Fatalf("direction status = %d body=%s", badDirection.StatusCode, badDirection.Body)
	}
	badNode := quotaJSON(t, server.URL+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
		"nodeId": "n2", "quotaGb": 10, "warningPercent": 80,
	})
	if badNode.StatusCode != http.StatusBadRequest {
		t.Fatalf("node mismatch status = %d", badNode.StatusCode)
	}
	unknown := quotaJSON(t, server.URL+"/api/v1/nodes/missing/traffic-quota", http.MethodPut, map[string]any{
		"quotaGb": 10, "warningPercent": 80,
	})
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown node status = %d body=%s", unknown.StatusCode, unknown.Body)
	}
	noLimit := quotaJSON(t, server.URL+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
		"warningPercent": 80,
	})
	if noLimit.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing limit status = %d", noLimit.StatusCode)
	}

	denied := newQuotaServer(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}))
	if response := quotaGet(t, denied.URL+"/api/v1/nodes/n1/traffic-quota"); response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied status = %d body=%s", response.StatusCode, response.Body)
	}
}

func TestListTrafficRisksUsesStoredUsage(t *testing.T) {
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 700, 500, quota.SourceAgent)
	risks, err := h.service.ListTrafficRisks(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 1 || risks[0].NodeID != "n1" || risks[0].Kind != "quota_exceeded" ||
		risks[0].Severity != "critical" || !risks[0].Estimated {
		t.Fatalf("risks = %+v", risks)
	}
	expiry := h.clock.Now().Add(48 * time.Hour)
	h.upsertPolicy(t, "n2", 1000, 80, expiry)
	risks, err = h.service.ListTrafficRisks(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 2 || risks[1].NodeID != "n2" || risks[1].Kind != "quota_expiring" {
		t.Fatalf("expiry risks = %+v", risks)
	}
}

func TestQuotaAuditActions(t *testing.T) {
	if len(quota.AuditActions()) != len(quota.ServiceActions()) {
		t.Fatalf("AuditActions = %v", quota.AuditActions())
	}
}

type quotaHTTPResult struct {
	StatusCode int
	Body       []byte
}

func quotaJSON(t *testing.T, url, method string, body any) quotaHTTPResult {
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
	return quotaRead(t, response)
}

func quotaGet(t *testing.T, url string) quotaHTTPResult {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return quotaRead(t, response)
}

func quotaRead(t *testing.T, response *http.Response) quotaHTTPResult {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return quotaHTTPResult{StatusCode: response.StatusCode, Body: buffer.Bytes()}
}

func quotaDecode(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}
