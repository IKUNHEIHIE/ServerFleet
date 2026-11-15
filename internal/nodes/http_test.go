package nodes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestHTTPGroupNodeResultsVisibilityFiltersAndPagination(t *testing.T) {
	t.Parallel()
	policy := &auth.NodePolicy{
		KnownNodes: map[string]struct{}{"a": {}, "b": {}, "c": {}, "d": {}},
		ReadGrants: map[string]map[string]struct{}{"reader": {"a": {}, "c": {}, "d": {}}, "zero": {}},
	}
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		func(context.Context) (*auth.NodePolicy, error) { return policy, nil })
	ctx := context.Background()
	for _, node := range []nodes.NodeCreateInput{
		{ID: "c", Name: "Gamma", Address: "192.0.2.3", Region: "eu", Provider: "aws", Tags: []string{"web"}, Status: nodes.StatusOffline},
		{ID: "b", Name: "hidden-marker", Address: "192.0.2.99", Region: "eu", Provider: "secret", Tags: []string{"web", "hidden-tag"}, Status: nodes.StatusOnline},
		{ID: "a", Name: "Alpha", Address: "192.0.2.1", Region: "eu", Provider: "aws", Tags: []string{"web", "web"}, Status: nodes.StatusOnline},
		{ID: "d", Name: "Empty attributes", Status: nodes.StatusUnknown},
	} {
		if _, err := h.repo.Create(ctx, h.db, node); err != nil {
			t.Fatal(err)
		}
	}
	for id, members := range map[string][]string{"safe": {"c", "a"}, "mixed": {"a", "b"}, "hidden": {"b"}, "empty": {}} {
		if _, err := h.groups.Create(ctx, h.db, id, id, "", members); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	if err := nodes.RegisterHTTP(mux, h.svc, func(r *http.Request) (nodes.Principal, error) {
		subject := r.Header.Get("X-Test-Subject")
		if subject == "" {
			subject = "reader"
		}
		return nodes.Principal{SubjectID: subject, RequestID: "group-results-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	type summary struct {
		ID     string   `json:"id"`
		CPU    *float64 `json:"cpuPercent"`
		Memory *float64 `json:"memoryPercent"`
	}
	type page struct {
		Items      []summary `json:"items"`
		Total      int       `json:"total"`
		NextCursor *string   `json:"nextCursor"`
	}
	read := func(query string) page {
		t.Helper()
		response := httpGet(t, server.URL+"/api/v1/groups/node-results?"+query)
		if strings.Contains(string(response.Body), "hidden-marker") || strings.Contains(string(response.Body), "192.0.2.99") || strings.Contains(string(response.Body), "hidden-tag") {
			t.Fatalf("hidden inventory leaked: %s", response.Body)
		}
		var got page
		decodeNodeResponse(t, response, &got)
		if got.Items == nil {
			t.Fatalf("items must be an array: %s", response.Body)
		}
		for _, item := range got.Items {
			if item.ID == "b" || item.CPU != nil || item.Memory != nil {
				t.Fatalf("unsafe or fabricated summary: %+v", got)
			}
		}
		return got
	}
	base := "mode=automatic&dimension=region&value=eu&limit=1"
	first := read(base)
	if first.Total != 2 || len(first.Items) != 1 || first.Items[0].ID != "a" || first.NextCursor == nil {
		t.Fatalf("first = %+v", first)
	}
	cursor := url.QueryEscape(*first.NextCursor)
	second := read(base + "&cursor=" + cursor)
	if second.Total != 2 || len(second.Items) != 1 || second.Items[0].ID != "c" || second.NextCursor != nil {
		t.Fatalf("second = %+v", second)
	}
	manual := read("mode=manual&groupId=safe")
	if manual.Total != 2 || len(manual.Items) != 2 || manual.Items[0].ID != "a" || manual.Items[1].ID != "c" {
		t.Fatalf("manual = %+v", manual)
	}
	for _, query := range []string{
		"mode=automatic&dimension=tag&value=web", "mode=automatic&dimension=provider&value=aws",
	} {
		if got := read(query); got.Total != 2 || len(got.Items) != 2 {
			t.Fatalf("bucket = %+v", got)
		}
	}
	for _, query := range []string{
		"mode=automatic&dimension=region&value=eu&search=ALPHA&status=online",
		"mode=manual&groupId=safe&q=192.0.2.3&status=offline",
	} {
		if got := read(query); got.Total != 1 || len(got.Items) != 1 {
			t.Fatalf("filtered = %+v", got)
		}
	}
	for _, query := range []string{
		"mode=automatic&dimension=tag&value=hidden-tag", "mode=automatic&dimension=provider&value=secret",
		"mode=automatic&dimension=region&value=eu&search=hidden-marker", "mode=manual&groupId=empty",
		"mode=automatic&dimension=region&value=eu&search=Alpha&status=offline",
	} {
		if got := read(query); got.Total != 0 || len(got.Items) != 0 || got.NextCursor != nil {
			t.Fatalf("empty = %+v", got)
		}
	}
	for _, group := range []string{"mixed", "hidden"} {
		response := httpGet(t, server.URL+"/api/v1/groups/node-results?mode=manual&groupId="+group)
		if response.StatusCode != http.StatusForbidden || strings.Contains(string(response.Body), "hidden") {
			t.Fatalf("group denial = %+v", response)
		}
	}
	if got := httpGet(t, server.URL+"/api/v1/groups/safe"); got.StatusCode != http.StatusOK {
		t.Fatalf("group route shadowed: %+v", got)
	}
	if got := httpGet(t, server.URL+"/api/v1/groups/node-results?mode=manual&groupId=missing"); got.StatusCode != http.StatusNotFound {
		t.Fatalf("missing = %+v", got)
	}
	for _, query := range []string{
		"", "mode=other", "mode=manual", "mode=automatic&dimension=invalid&value=eu",
		"mode=automatic&dimension=region", "mode=automatic&dimension=region&value=%20",
		"mode=manual&groupId=safe&dimension=region", "mode=automatic&dimension=region&value=eu&groupId=safe",
		base + "&status=unknown", base + "&value=eu", base + "&cursor=%21", base + "&search=%FF", base + "&cursor=garbage", base + "&cursor=" + cursor + "&search=Alpha",
		"mode=manual&groupId=safe&cursor=" + cursor, "mode=manual&groupId=safe&limit=0",
		"mode=manual&groupId=safe&limit=201", "mode=manual&groupId=safe&limit=-1",
		"mode=manual&groupId=safe&limit=no", base + "&limit=2", base + "&search=a&q=b",
		base + "&q=" + strings.Repeat("x", 257), base + "&cursor=" + strings.Repeat("x", 2049), base + "&unknown=1",
	} {
		response := httpGet(t, server.URL+"/api/v1/groups/node-results?"+query)
		if response.StatusCode != http.StatusBadRequest || strings.Contains(string(response.Body), "hidden-marker") {
			t.Fatalf("invalid query %q: %+v", query, response)
		}
	}
	// The same cursor cannot be reused by a different subject.
	foreign, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/groups/node-results?"+base+"&cursor="+cursor, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Header.Set("X-Test-Subject", "zero")
	foreignResponse, err := http.DefaultClient.Do(foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignResult := nodeRead(t, foreignResponse)
	foreignResponse.Body.Close()
	if foreignResult.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign cursor = %+v", foreignResult)
	}
	// Every page reauthorizes: a cursor issued before revocation cannot reveal a.
	delete(policy.ReadGrants["reader"], "a")
	if got := read(base + "&cursor=" + cursor); got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != "c" {
		t.Fatalf("revoked page = %+v", got)
	}
	for _, subject := range []string{"zero", "unknown"} {
		request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/groups/node-results?"+base, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Test-Subject", subject)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		result := nodeRead(t, response)
		response.Body.Close()
		if subject == "zero" {
			var empty page
			decodeNodeResponse(t, result, &empty)
			if empty.Total != 0 || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
				t.Fatalf("zero scope = %+v", empty)
			}
		} else if result.StatusCode != http.StatusForbidden {
			t.Fatalf("subject %s = %+v", subject, result)
		}
	}
}
