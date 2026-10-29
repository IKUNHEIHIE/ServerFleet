package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/gateway"
)

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
