package nodes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
)

func TestHTTPNodeLifecycleIdentityCapabilitiesDiagnostics(t *testing.T) {
	t.Parallel()
	policy := readPolicy("u1", []string{"n1"}, []string{"n1"})
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), policy,
		nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	mux := http.NewServeMux()
	if err := nodes.RegisterHTTP(mux, h.svc, func(*http.Request) (nodes.Principal, error) {
		return nodes.Principal{SubjectID: "u1", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	identity := httpGet(t, server.URL+"/api/v1/nodes/n1/identity")
	var identityBody struct {
		Recorded bool `json:"recorded"`
		Revoked  bool `json:"revoked"`
	}
	decodeNodeResponse(t, identity, &identityBody)
	if identityBody.Recorded || identityBody.Revoked {
		t.Fatalf("identity = %+v", identityBody)
	}
	capabilities := httpGet(t, server.URL+"/api/v1/nodes/n1/capabilities")
	var capabilitiesBody struct {
		Recorded     bool     `json:"recorded"`
		Capabilities []string `json:"capabilities"`
	}
	decodeNodeResponse(t, capabilities, &capabilitiesBody)
	if capabilitiesBody.Recorded || capabilitiesBody.Capabilities == nil {
		t.Fatalf("capabilities = %+v", capabilitiesBody)
	}
	diagnostics := httpGet(t, server.URL+"/api/v1/nodes/n1/diagnostics")
	var diagnosticsBody struct {
		ConnectionState string `json:"connectionState"`
	}
	decodeNodeResponse(t, diagnostics, &diagnosticsBody)
	if diagnosticsBody.ConnectionState != "unreachable" {
		t.Fatalf("diagnostics = %+v", diagnosticsBody)
	}

	suspend := nodeJSON(t, server.URL+"/api/v1/nodes/n1/suspend", map[string]any{"reason": "maintenance window"})
	if suspend.StatusCode != http.StatusOK {
		t.Fatalf("suspend status = %d body=%s", suspend.StatusCode, suspend.Body)
	}
	var suspended struct {
		Status string `json:"status"`
		Online bool   `json:"online"`
	}
	decodeNodeBody(t, suspend.Body, &suspended)
	if suspended.Status != "suspended" || suspended.Online {
		t.Fatalf("suspend = %+v", suspended)
	}
	updated, applied, err := h.repo.ApplyAgentUpdate(context.Background(), h.db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: time.Now().UTC(),
	})
	if err != nil || !applied || updated.Status != nodes.StatusSuspended {
		t.Fatalf("agent update overrode suspension: %+v applied=%t err=%v", updated, applied, err)
	}

	revoke := nodeJSON(t, server.URL+"/api/v1/nodes/n1/revoke", map[string]any{"reason": "retired"})
	if revoke.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", revoke.StatusCode, revoke.Body)
	}
	var revoked struct {
		Status  string `json:"status"`
		Revoked bool   `json:"revoked"`
	}
	decodeNodeBody(t, revoke.Body, &revoked)
	if revoked.Status != "revoked" || !revoked.Revoked {
		t.Fatalf("revoke = %+v", revoked)
	}

	again := nodeJSON(t, server.URL+"/api/v1/nodes/n1/suspend", map[string]any{"reason": "late"})
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("suspend after revoke status = %d body=%s", again.StatusCode, again.Body)
	}
	unsupported := nodeJSON(t, server.URL+"/api/v1/nodes/n1/revoke", map[string]any{
		"reason": "retired", "expectedRevision": 3,
	})
	if unsupported.StatusCode != http.StatusBadRequest {
		t.Fatalf("expectedRevision status = %d body=%s", unsupported.StatusCode, unsupported.Body)
	}
	unknown := nodeJSON(t, server.URL+"/api/v1/nodes/missing/revoke", map[string]any{"reason": "retired"})
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown node status = %d", unknown.StatusCode)
	}
}

func nodeJSON(t *testing.T, url string, body any) nodeHTTPResult {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return nodeRead(t, response)
}

func httpGet(t *testing.T, url string) nodeHTTPResult {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return nodeRead(t, response)
}

type nodeHTTPResult struct {
	StatusCode int
	Body       []byte
}

func nodeRead(t *testing.T, response *http.Response) nodeHTTPResult {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return nodeHTTPResult{StatusCode: response.StatusCode, Body: buffer.Bytes()}
}

func decodeNodeResponse(t *testing.T, response nodeHTTPResult, destination any) {
	t.Helper()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode, response.Body)
	}
	decodeNodeBody(t, response.Body, destination)
}

func decodeNodeBody(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func TestHTTPNodesGroupsAndClassifications(t *testing.T) {
	t.Parallel()
	policy := readPolicy("u1", []string{"n1"}, []string{"n1"})
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), policy,
		nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	mux := http.NewServeMux()
	if err := nodes.RegisterHTTP(mux, h.svc, func(*http.Request) (nodes.Principal, error) {
		return nodes.Principal{SubjectID: "u1", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	list, err := http.Get(server.URL + "/api/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var nodesPage struct {
		Items []struct {
			ID            string   `json:"id"`
			CPUPercent    *float64 `json:"cpuPercent"`
			MemoryPercent *float64 `json:"memoryPercent"`
		} `json:"items"`
	}
	json.NewDecoder(list.Body).Decode(&nodesPage)
	list.Body.Close()
	if len(nodesPage.Items) != 1 || nodesPage.Items[0].CPUPercent != nil || nodesPage.Items[0].MemoryPercent != nil {
		t.Fatalf("nodes = %+v", nodesPage)
	}

	body, _ := json.Marshal(map[string]any{"name": "Maintenance", "description": "patching"})
	create, err := http.Post(server.URL+"/api/v1/groups", "application/json", bytes.NewReader(body))
	if err != nil || create.StatusCode != http.StatusCreated {
		t.Fatalf("create = %v %d", err, create.StatusCode)
	}
	var group struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}
	json.NewDecoder(create.Body).Decode(&group)
	create.Body.Close()
	if group.ID == "" || group.Revision != 1 {
		t.Fatalf("group = %+v", group)
	}

	members, _ := json.Marshal(map[string]any{"memberNodeIds": []string{"n1"}, "expectedRevision": 1})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/groups/"+group.ID+"/members", bytes.NewReader(members))
	request.Header.Set("Content-Type", "application/json")
	replace, err := http.DefaultClient.Do(request)
	if err != nil || replace.StatusCode != http.StatusOK {
		t.Fatalf("members = %v %d", err, replace.StatusCode)
	}
	replace.Body.Close()

	facets, err := http.Get(server.URL + "/api/v1/groups/classifications?dimension=provider")
	if err != nil || facets.StatusCode != http.StatusOK {
		t.Fatalf("facets = %v %d", err, facets.StatusCode)
	}
	facets.Body.Close()
}
