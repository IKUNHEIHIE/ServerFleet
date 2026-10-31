package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/gateway"
)

func TestHTTPGatewayChecksAndDispatch(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t)
	created := f.createGateway(t, "gw-checks", "Edge", false)
	mux := http.NewServeMux()
	if err := gateway.RegisterHTTP(mux, f.service, func(*http.Request) (gateway.Principal, error) {
		return gateway.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	checks, err := http.Get(server.URL + "/api/v1/gateways/" + created.ID + "/checks")
	if err != nil || checks.StatusCode != http.StatusOK {
		t.Fatalf("checks = %v %d", err, checks.StatusCode)
	}
	var checksBody struct {
		Recorded bool   `json:"recorded"`
		Message  string `json:"message"`
	}
	json.NewDecoder(checks.Body).Decode(&checksBody)
	checks.Body.Close()
	if checksBody.Recorded || checksBody.Message == "" {
		t.Fatalf("initial checks = %+v", checksBody)
	}

	dispatchBody, _ := json.Marshal(map[string]any{"state": "enabled", "expectedRevision": created.Revision})
	request, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/gateways/"+created.ID+"/dispatch", bytes.NewReader(dispatchBody))
	request.Header.Set("Content-Type", "application/json")
	dispatch, err := http.DefaultClient.Do(request)
	if err != nil || dispatch.StatusCode != http.StatusOK {
		t.Fatalf("dispatch = %v %d", err, dispatch.StatusCode)
	}
	var dispatchView struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(dispatch.Body).Decode(&dispatchView)
	dispatch.Body.Close()
	if !dispatchView.Enabled {
		t.Fatalf("dispatch view = %+v", dispatchView)
	}

	staleBody, _ := json.Marshal(map[string]any{"enabled": false, "expectedRevision": created.Revision})
	staleRequest, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/gateways/"+created.ID+"/dispatch", bytes.NewReader(staleBody))
	staleRequest.Header.Set("Content-Type", "application/json")
	stale, err := http.DefaultClient.Do(staleRequest)
	if err != nil || stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale dispatch = %v %d", err, stale.StatusCode)
	}
	stale.Body.Close()

	invalidBody, _ := json.Marshal(map[string]any{"state": "sideways"})
	invalidRequest, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/gateways/"+created.ID+"/dispatch", bytes.NewReader(invalidBody))
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalid, err := http.DefaultClient.Do(invalidRequest)
	if err != nil || invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid dispatch = %v %d", err, invalid.StatusCode)
	}
	invalid.Body.Close()

	if _, err := f.service.RecordChecks(context.Background(), "admin", "req-checks", created.ID, 2, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.27.0",
	}); err != nil {
		t.Fatal(err)
	}
	recorded, err := http.Get(server.URL + "/api/v1/gateways/" + created.ID + "/checks")
	if err != nil || recorded.StatusCode != http.StatusOK {
		t.Fatalf("recorded checks = %v %d", err, recorded.StatusCode)
	}
	var recordedBody struct {
		Recorded       bool   `json:"recorded"`
		NginxInstalled bool   `json:"nginxInstalled"`
		ChecksPassed   bool   `json:"checksPassed"`
		NginxVersion   string `json:"nginxVersion"`
	}
	json.NewDecoder(recorded.Body).Decode(&recordedBody)
	recorded.Body.Close()
	if !recordedBody.Recorded || !recordedBody.NginxInstalled || !recordedBody.ChecksPassed || recordedBody.NginxVersion != "1.27.0" {
		t.Fatalf("recorded body = %+v", recordedBody)
	}
}

func TestHTTPGatewayProvisionAndRoute(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t)
	mux := http.NewServeMux()
	if err := gateway.RegisterHTTP(mux, f.service, func(*http.Request) (gateway.Principal, error) {
		return gateway.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]any{"nodeId": "n1", "acknowledgeInstallImpact": true})
	create, err := http.Post(server.URL+"/api/v1/gateways", "application/json", bytes.NewReader(body))
	if err != nil || create.StatusCode != http.StatusAccepted {
		t.Fatalf("create = %v %d", err, create.StatusCode)
	}
	var job struct {
		ID        string `json:"id"`
		GatewayID string `json:"gatewayId"`
		Status    string `json:"status"`
	}
	json.NewDecoder(create.Body).Decode(&job)
	create.Body.Close()
	if job.ID == "" || job.GatewayID == "" {
		t.Fatalf("job = %+v", job)
	}
	get, err := http.Get(server.URL + "/api/v1/gateways/" + job.GatewayID)
	if err != nil || get.StatusCode != http.StatusOK {
		t.Fatalf("get = %v %d", err, get.StatusCode)
	}
	var gatewayView struct {
		Online bool   `json:"online"`
		Status string `json:"status"`
	}
	json.NewDecoder(get.Body).Decode(&gatewayView)
	get.Body.Close()
	if !gatewayView.Online {
		t.Fatalf("view = %+v", gatewayView)
	}
	routeBody, _ := json.Marshal(map[string]any{
		"gatewayId": job.GatewayID, "domain": "app.example.com",
		"upstream": "n1:8080", "https": true, "enabled": true,
	})
	route, err := http.Post(server.URL+"/api/v1/gateway-routes", "application/json", bytes.NewReader(routeBody))
	if err != nil || route.StatusCode != http.StatusCreated {
		t.Fatalf("route = %v %d", err, route.StatusCode)
	}
	route.Body.Close()
	list, err := http.Get(server.URL + "/api/v1/gateway-routes")
	if err != nil || list.StatusCode != http.StatusOK {
		t.Fatalf("routes = %v %d", err, list.StatusCode)
	}
	list.Body.Close()
}
