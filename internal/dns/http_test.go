package dns_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

func TestDNSHTTPAccountZoneAndRecordFlow(t *testing.T) {
	t.Parallel()
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	mux := http.NewServeMux()
	if err := dns.RegisterHTTP(mux, f.service, func(*http.Request) (dns.Principal, error) {
		return dns.Principal{SubjectID: "admin-1", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	providers, err := http.Get(server.URL + "/api/v1/dns/providers")
	if err != nil || providers.StatusCode != http.StatusOK {
		t.Fatalf("providers = %v %d", err, providers.StatusCode)
	}
	providers.Body.Close()

	body, _ := json.Marshal(map[string]any{
		"displayName": "Primary", "credentialRef": "vault://cloudflare/primary", "makeDefault": true,
	})
	create, err := http.Post(server.URL+"/api/v1/dns/accounts", "application/json", bytes.NewReader(body))
	if err != nil || create.StatusCode != http.StatusCreated {
		t.Fatalf("create account = %v %d", err, create.StatusCode)
	}
	var account struct {
		ID               string `json:"id"`
		CredentialStatus string `json:"credentialStatus"`
	}
	json.NewDecoder(create.Body).Decode(&account)
	create.Body.Close()
	if account.ID == "" {
		t.Fatal("missing account id")
	}
	if bytes.Contains([]byte(account.CredentialStatus), []byte("vault://")) {
		t.Fatal("credential reference leaked")
	}

	test, err := http.Post(server.URL+"/api/v1/dns/accounts/"+account.ID+"/test", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil || test.StatusCode != http.StatusOK {
		t.Fatalf("test account = %v %d", err, test.StatusCode)
	}
	test.Body.Close()
	zbody, _ := json.Marshal(map[string]any{"providerZoneId": "pz-1"})
	sync, err := http.Post(server.URL+"/api/v1/dns/accounts/"+account.ID+"/zones/sync", "application/json", bytes.NewReader(zbody))
	if err != nil || sync.StatusCode != http.StatusOK {
		t.Fatalf("sync zone = %v %d", err, sync.StatusCode)
	}
	var zone struct {
		ID string `json:"id"`
	}
	json.NewDecoder(sync.Body).Decode(&zone)
	sync.Body.Close()
	if zone.ID == "" {
		t.Fatal("missing zone id")
	}

	rbody, _ := json.Marshal(map[string]any{
		"type": "A", "name": "www", "content": "203.0.113.10", "ttl": 120,
	})
	record, err := http.Post(server.URL+"/api/v1/dns/zones/"+zone.ID+"/records", "application/json", bytes.NewReader(rbody))
	if err != nil || record.StatusCode != http.StatusCreated {
		t.Fatalf("create record = %v %d", err, record.StatusCode)
	}
	var rec struct {
		ID string `json:"id"`
	}
	json.NewDecoder(record.Body).Decode(&rec)
	record.Body.Close()
	if rec.ID == "" {
		t.Fatal("missing record id")
	}
	list, err := http.Get(server.URL + "/api/v1/dns/zones/" + zone.ID + "/records")
	if err != nil || list.StatusCode != http.StatusOK {
		t.Fatalf("list records = %v %d", err, list.StatusCode)
	}
	list.Body.Close()
	request, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/dns/zones/"+zone.ID+"/records/"+rec.ID, nil)
	del, err := http.DefaultClient.Do(request)
	if err != nil || del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete record = %v %d", err, del.StatusCode)
	}
	del.Body.Close()
}
