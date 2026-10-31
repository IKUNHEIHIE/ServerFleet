package publishing_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/publishing"
)

func newPublishingServer(t *testing.T, fixture *publicationFixture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if err := publishing.RegisterHTTP(mux, fixture.service, func(*http.Request) (publishing.Principal, error) {
		return publishing.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func publicationDraft() map[string]any {
	return map[string]any{
		"serviceGroupId": "sg1", "gatewayIds": []string{"gw1"}, "domain": "app.example.com",
		"https": true, "dnsAccountId": "a1", "zoneId": "z1",
	}
}

func TestHTTPPublicationPreviewCreateListGet(t *testing.T) {
	fixture := newPublicationFixture(t)
	server := newPublishingServer(t, fixture)

	preview := pubJSON(t, server.URL+"/api/v1/publishing/preview", http.MethodPost, publicationDraft())
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", preview.StatusCode, preview.Body)
	}
	var previewBody struct {
		Eligible              bool     `json:"eligible"`
		TargetNodeIDsSnapshot []string `json:"targetNodeIdsSnapshot"`
		Checks                []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
		Warnings []string `json:"warnings"`
	}
	pubDecode(t, preview.Body, &previewBody)
	if !previewBody.Eligible || len(previewBody.TargetNodeIDsSnapshot) != 2 ||
		previewBody.TargetNodeIDsSnapshot[0] != "n1" || len(previewBody.Checks) == 0 || len(previewBody.Warnings) == 0 {
		t.Fatalf("preview = %+v", previewBody)
	}
	// The unprefixed contract/client path must serve the same preview.
	if alias := pubJSON(t, server.URL+"/api/v1/publications/preview", http.MethodPost, publicationDraft()); alias.StatusCode != http.StatusOK {
		t.Fatalf("alias preview status = %d", alias.StatusCode)
	}

	create := pubJSON(t, server.URL+"/api/v1/publishing/publications", http.MethodPost, publicationDraft())
	if create.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var operation struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	}
	pubDecode(t, create.Body, &operation)
	if operation.ID == "" || operation.Status != "queued" || len(operation.Stages) != 3 {
		t.Fatalf("operation = %+v", operation)
	}

	list := pubGet(t, server.URL+"/api/v1/publishing/publications")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.StatusCode, list.Body)
	}
	var items []struct {
		ID string `json:"id"`
	}
	pubDecode(t, list.Body, &items)
	if len(items) != 1 || items[0].ID != operation.ID {
		t.Fatalf("list = %s", list.Body)
	}
	if alias := pubGet(t, server.URL+"/api/v1/publications"); alias.StatusCode != http.StatusOK {
		t.Fatalf("alias list status = %d", alias.StatusCode)
	}

	one := pubGet(t, server.URL+"/api/v1/publishing/publications/"+operation.ID)
	if one.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d body=%s", one.StatusCode, one.Body)
	}
	var fetched struct {
		ID        string `json:"id"`
		CreatedAt string `json:"createdAt"`
	}
	pubDecode(t, one.Body, &fetched)
	if fetched.ID != operation.ID || fetched.CreatedAt == "" {
		t.Fatalf("fetched = %+v", fetched)
	}
}

func TestHTTPPublicationPreviewAndCreateRejections(t *testing.T) {
	fixture := newPublicationFixture(t)
	server := newPublishingServer(t, fixture)

	missingGateway := publicationDraft()
	delete(missingGateway, "gatewayIds")
	if response := pubJSON(t, server.URL+"/api/v1/publishing/preview", http.MethodPost, missingGateway); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing gateway status = %d", response.StatusCode)
	}
	missingHTTPS := publicationDraft()
	delete(missingHTTPS, "https")
	if response := pubJSON(t, server.URL+"/api/v1/publishing/preview", http.MethodPost, missingHTTPS); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing https status = %d", response.StatusCode)
	}

	stale := publicationDraft()
	stale["expectedGroupRevision"] = 2
	preview := pubJSON(t, server.URL+"/api/v1/publishing/preview", http.MethodPost, stale)
	var previewBody struct {
		Eligible bool `json:"eligible"`
		Checks   []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
	}
	pubDecode(t, preview.Body, &previewBody)
	if preview.StatusCode != http.StatusOK || previewBody.Eligible {
		t.Fatalf("stale preview = %d %+v", preview.StatusCode, previewBody)
	}
	if response := pubJSON(t, server.URL+"/api/v1/publishing/publications", http.MethodPost, stale); response.StatusCode != http.StatusConflict {
		t.Fatalf("stale create status = %d body=%s", response.StatusCode, response.Body)
	}

	if response := pubGet(t, server.URL+"/api/v1/publishing/publications/missing"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown plan status = %d", response.StatusCode)
	}
	if response := pubJSON(t, server.URL+"/api/v1/publications/preview", http.MethodPost, stale); response.StatusCode != http.StatusOK {
		t.Fatalf("alias stale preview status = %d", response.StatusCode)
	}

	fixture.authorizer.deny = true
	if response := pubJSON(t, server.URL+"/api/v1/publishing/preview", http.MethodPost, publicationDraft()); response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied preview status = %d body=%s", response.StatusCode, response.Body)
	}
	if response := pubGet(t, server.URL+"/api/v1/publishing/publications"); response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied list status = %d body=%s", response.StatusCode, response.Body)
	}
}

func TestHTTPPublicationAuthenticationAndAuditActions(t *testing.T) {
	fixture := newPublicationFixture(t)
	mux := http.NewServeMux()
	if err := publishing.RegisterHTTP(mux, fixture.service, func(*http.Request) (publishing.Principal, error) {
		return publishing.Principal{}, errors.New("no session")
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	if response := pubGet(t, server.URL+"/api/v1/publishing/publications"); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	if len(publishing.AuditActions()) != len(publishing.ServiceActions()) {
		t.Fatalf("AuditActions = %v", publishing.AuditActions())
	}
}

type pubHTTPResult struct {
	StatusCode int
	Body       []byte
}

func pubJSON(t *testing.T, url, method string, body any) pubHTTPResult {
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
	return pubRead(t, response)
}

func pubGet(t *testing.T, url string) pubHTTPResult {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return pubRead(t, response)
}

func pubRead(t *testing.T, response *http.Response) pubHTTPResult {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return pubHTTPResult{StatusCode: response.StatusCode, Body: buffer.Bytes()}
}

func pubDecode(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}
