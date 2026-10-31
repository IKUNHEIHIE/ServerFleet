package certificates_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
)

type fakeMembers struct {
	members []string
	err     error
}

func (f *fakeMembers) Members(context.Context, string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.members...), nil
}

func newCertServer(t *testing.T, fixture certFixture, members certificates.GroupMembers) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if err := certificates.RegisterHTTP(mux, fixture.service, members, func(*http.Request) (certificates.Principal, error) {
		return certificates.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestHTTPCertificateDistributionLifecycle(t *testing.T) {
	fixture := newCertFixture(t, nil, nil)
	server := newCertServer(t, fixture, &fakeMembers{members: []string{"n2", "n1"}})

	empty := certGet(t, server.URL+"/api/v1/certificates/internal/distributions")
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("empty list status = %d body=%s", empty.StatusCode, empty.Body)
	}
	var emptyPage struct {
		Items []any `json:"items"`
	}
	certDecode(t, empty.Body, &emptyPage)
	if len(emptyPage.Items) != 0 {
		t.Fatalf("empty page = %s", empty.Body)
	}

	create := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "per_node_identity",
		"validityHours": 24, "confirmServiceImpact": true, "expectedGroupRevision": 4,
	})
	if create.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", create.StatusCode, create.Body)
	}
	if bytes.Contains(create.Body, []byte("PRIVATE KEY")) || bytes.Contains(create.Body, []byte("keyRef")) ||
		bytes.Contains(create.Body, []byte("keyref_")) {
		t.Fatalf("response leaks key reference: %s", create.Body)
	}
	var job struct {
		JobID                 string `json:"jobId"`
		ServiceGroupID        string `json:"serviceGroupId"`
		MemberNodeIDsSnapshot []string
		CredentialType        string `json:"credentialType"`
		State                 string `json:"state"`
		Results               []struct {
			NodeID string `json:"nodeId"`
			State  string `json:"state"`
		} `json:"results"`
	}
	certDecode(t, create.Body, &job)
	if job.JobID == "" || job.ServiceGroupID != "sg1" || job.CredentialType != "per_node_identity" ||
		job.State != "queued" || len(job.Results) != 2 || len(job.MemberNodeIDsSnapshot) != 2 {
		t.Fatalf("job = %+v", job)
	}
	if fixture.tasks.calls != 1 || fixture.tasks.last.ServiceGroupID != "sg1" {
		t.Fatalf("task submission = %+v", fixture.tasks.last)
	}

	list := certGet(t, server.URL+"/api/v1/certificates/internal/distributions")
	var page struct {
		Items []struct {
			JobID          string `json:"jobId"`
			CredentialType string `json:"credentialType"`
			State          string `json:"state"`
		} `json:"items"`
	}
	certDecode(t, list.Body, &page)
	if len(page.Items) != 1 || page.Items[0].JobID != job.JobID || page.Items[0].CredentialType != "per_node_identity" {
		t.Fatalf("list = %s", list.Body)
	}

	revoke := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions/"+job.JobID+"/revoke", http.MethodPost, map[string]any{
		"reason": "node retired", "includeAllIssuedLeafs": true,
	})
	if revoke.StatusCode != http.StatusAccepted {
		t.Fatalf("revoke status = %d body=%s", revoke.StatusCode, revoke.Body)
	}
	var revoked struct {
		State   string `json:"state"`
		Results []struct {
			State string `json:"state"`
		} `json:"results"`
	}
	certDecode(t, revoke.Body, &revoked)
	if revoked.State != "revoked" || len(revoked.Results) != 2 || revoked.Results[0].State != "revoked" {
		t.Fatalf("revoked = %+v", revoked)
	}
	if fixture.issuer.revokeCalls != 1 {
		t.Fatalf("issuer revoke calls = %d", fixture.issuer.revokeCalls)
	}
}

func TestHTTPCertificateDistributionNegative(t *testing.T) {
	fixture := newCertFixture(t, nil, nil)
	server := newCertServer(t, fixture, &fakeMembers{members: []string{"n1"}})

	missingConfirm := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "trust_bundle", "expectedGroupRevision": 4,
	})
	if missingConfirm.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing confirm status = %d", missingConfirm.StatusCode)
	}
	badKey := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
		"expectedGroupRevision": 4, "keyRef": "-----BEGIN PRIVATE KEY-----abc",
	})
	if badKey.StatusCode != http.StatusBadRequest || !strings.Contains(string(badKey.Body), "key_material_rejected") {
		t.Fatalf("bad key status = %d body=%s", badKey.StatusCode, badKey.Body)
	}

	fixture.revisions.revision = 5
	conflict := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
		"expectedGroupRevision": 4,
	})
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("revision conflict status = %d body=%s", conflict.StatusCode, conflict.Body)
	}
	fixture.revisions.revision = 4

	created := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
		"expectedGroupRevision": 4,
	})
	if created.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", created.StatusCode, created.Body)
	}
	var job struct {
		JobID string `json:"jobId"`
	}
	certDecode(t, created.Body, &job)

	fixture.issuer.revokeErr = errors.New("issuer unavailable")
	failed := certJSON(t, server.URL+"/api/v1/certificates/internal/distributions/"+job.JobID+"/revoke", http.MethodPost, map[string]any{
		"reason": "rotate",
	})
	if failed.StatusCode != http.StatusBadGateway {
		t.Fatalf("issuer failure status = %d body=%s", failed.StatusCode, failed.Body)
	}
	list := certGet(t, server.URL+"/api/v1/certificates/internal/distributions")
	var page struct {
		Items []struct {
			State string `json:"state"`
		} `json:"items"`
	}
	certDecode(t, list.Body, &page)
	if len(page.Items) != 1 || page.Items[0].State != "queued" {
		t.Fatalf("failed revoke mutated state: %s", list.Body)
	}

	deniedFixture := newCertFixture(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}), nil)
	denied := certGet(t, newCertServer(t, deniedFixture, &fakeMembers{}).URL+"/api/v1/certificates/internal/distributions")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("denied list status = %d body=%s", denied.StatusCode, denied.Body)
	}

	noGroup := newCertFixture(t, nil, nil)
	unavailable := certJSON(t, newCertServer(t, noGroup, nil).URL+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
		"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
		"expectedGroupRevision": 4,
	})
	if unavailable.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing group resolver status = %d body=%s", unavailable.StatusCode, unavailable.Body)
	}
}

func TestHTTPCertificateAuthenticationAndAuditActions(t *testing.T) {
	fixture := newCertFixture(t, nil, nil)
	mux := http.NewServeMux()
	if err := certificates.RegisterHTTP(mux, fixture.service, &fakeMembers{}, func(*http.Request) (certificates.Principal, error) {
		return certificates.Principal{}, errors.New("no session")
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	if response := certGet(t, server.URL+"/api/v1/certificates/internal/distributions"); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	if len(certificates.AuditActions()) != len(certificates.ServiceActions()) {
		t.Fatalf("AuditActions = %v", certificates.AuditActions())
	}
}

type certHTTPResult struct {
	StatusCode int
	Body       []byte
}

func certJSON(t *testing.T, url, method string, body any) certHTTPResult {
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
	return certRead(t, response)
}

func certGet(t *testing.T, url string) certHTTPResult {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return certRead(t, response)
}

func certRead(t *testing.T, response *http.Response) certHTTPResult {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return certHTTPResult{StatusCode: response.StatusCode, Body: buffer.Bytes()}
}

func certDecode(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}
