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

func TestHTTPPublicationRunAndRollback(t *testing.T) {
	fixture := newPublicationFixture(t)
	server := newPublishingServer(t, fixture)

	create := pubJSON(t, server.URL+"/api/v1/publishing/publications", http.MethodPost, publicationDraft())
	if create.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	pubDecode(t, create.Body, &created)

	run := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/run", http.MethodPost, map[string]any{})
	if run.StatusCode != http.StatusOK {
		t.Fatalf("run status = %d body=%s", run.StatusCode, run.Body)
	}
	var operation struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	}
	pubDecode(t, run.Body, &operation)
	if operation.ID != created.ID || operation.Status != "succeeded" || len(operation.Stages) != 3 {
		t.Fatalf("run operation = %+v", operation)
	}
	for _, stage := range operation.Stages {
		if stage.Status != "succeeded" {
			t.Fatalf("stage = %+v", stage)
		}
	}
	order := fixture.ports.callLog.snapshot()
	if len(order) != 3 || order[0] != publishing.StageDNS || order[1] != publishing.StageCertificate || order[2] != publishing.StageGateway {
		t.Fatalf("stage order = %v", order)
	}

	pending := pubJSON(t, server.URL+"/api/v1/publishing/publications", http.MethodPost, publicationDraft())
	if pending.StatusCode != http.StatusAccepted {
		t.Fatalf("second create status = %d body=%s", pending.StatusCode, pending.Body)
	}
	var second struct {
		ID string `json:"id"`
	}
	pubDecode(t, pending.Body, &second)
	notRollable := pubJSON(t, server.URL+"/api/v1/publications/"+second.ID+"/rollback", http.MethodPost, map[string]any{"stage": "dns"})
	if notRollable.StatusCode != http.StatusBadRequest {
		t.Fatalf("pending rollback status = %d body=%s", notRollable.StatusCode, notRollable.Body)
	}

	rollbackDNS := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/rollback", http.MethodPost, map[string]any{"stage": "dns"})
	if rollbackDNS.StatusCode != http.StatusOK {
		t.Fatalf("rollback status = %d body=%s", rollbackDNS.StatusCode, rollbackDNS.Body)
	}
	pubDecode(t, rollbackDNS.Body, &operation)
	if operation.Status != "partially_completed" || operation.Stages[0].Name != "dns" || operation.Stages[0].Status != "rolled_back" {
		t.Fatalf("rollback operation = %+v", operation)
	}
	if _, rollbacks := fixture.ports.dns.counts(); rollbacks != 1 || fixture.ports.dns.lastRollback().PlanID != created.ID {
		t.Fatalf("dns rollback calls = %d %+v", rollbacks, fixture.ports.dns.lastRollback())
	}
	for _, stage := range []string{"certificate", "gateway"} {
		response := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/rollback", http.MethodPost, map[string]any{"stage": stage})
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s rollback status = %d body=%s", stage, response.StatusCode, response.Body)
		}
	}
	after := pubGet(t, server.URL+"/api/v1/publishing/publications/"+created.ID)
	pubDecode(t, after.Body, &operation)
	for _, stage := range operation.Stages {
		if stage.Status != "rolled_back" {
			t.Fatalf("stage after full rollback = %+v", operation.Stages)
		}
	}
}

func TestHTTPPublicationRunFailureAndRetry(t *testing.T) {
	fixture := newPublicationFixture(t)
	server := newPublishingServer(t, fixture)

	create := pubJSON(t, server.URL+"/api/v1/publishing/publications", http.MethodPost, publicationDraft())
	if create.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	pubDecode(t, create.Body, &created)

	fixture.ports.gateway.set(publishing.StageResult{}, errors.New("provider refused"))
	failed := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/run", http.MethodPost, map[string]any{})
	if failed.StatusCode != http.StatusBadGateway {
		t.Fatalf("failed run status = %d body=%s", failed.StatusCode, failed.Body)
	}
	var operation struct {
		Status string `json:"status"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	pubDecode(t, failed.Body, &operation)
	if operation.Status != "partially_completed" || operation.Error.Code != "stage_failed" {
		t.Fatalf("failed operation = %+v", operation)
	}
	state := map[string]string{}
	for _, stage := range operation.Stages {
		state[stage.Name] = stage.Status
	}
	if state["dns"] != "succeeded" || state["certificate"] != "succeeded" || state["gateway"] != "failed" {
		t.Fatalf("stages after failure = %+v", state)
	}
	if bytes.Contains(failed.Body, []byte("provider refused")) {
		t.Fatalf("provider error leaked: %s", failed.Body)
	}

	fixture.ports.gateway.set(publishing.StageResult{Summary: "gateway ok"}, nil)
	retry := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/run", http.MethodPost, map[string]any{})
	if retry.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d body=%s", retry.StatusCode, retry.Body)
	}
	pubDecode(t, retry.Body, &operation)
	if operation.Status != "succeeded" {
		t.Fatalf("retry operation = %+v", operation)
	}
	dnsPublishes, _ := fixture.ports.dns.counts()
	certPublishes, _ := fixture.ports.cert.counts()
	gwPublishes, _ := fixture.ports.gateway.counts()
	if dnsPublishes != 1 || certPublishes != 1 || gwPublishes != 2 {
		t.Fatalf("publish counts = dns:%d cert:%d gateway:%d", dnsPublishes, certPublishes, gwPublishes)
	}
	if response := pubJSON(t, server.URL+"/api/v1/publishing/publications/missing/run", http.MethodPost, map[string]any{}); response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing run status = %d", response.StatusCode)
	}
	if response := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/rollback", http.MethodPost, map[string]any{"stage": "database"}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid stage rollback status = %d body=%s", response.StatusCode, response.Body)
	}
	if response := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/rollback", http.MethodPost, map[string]any{}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing stage rollback status = %d", response.StatusCode)
	}

	fixture.authorizer.deny = true
	denied := pubJSON(t, server.URL+"/api/v1/publishing/publications/"+created.ID+"/run", http.MethodPost, map[string]any{})
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("denied run status = %d body=%s", denied.StatusCode, denied.Body)
	}
	if bytes.Contains(denied.Body, []byte(created.ID)) {
		t.Fatalf("denied run leaked plan data: %s", denied.Body)
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
