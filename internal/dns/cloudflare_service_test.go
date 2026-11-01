package dns_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

func TestCloudflareProviderThroughDNSDomain(t *testing.T) {
	var recordCreates int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cloudflareTestToken {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user/tokens/verify":
			_, _ = io.WriteString(w, `{"success":true,"result":{"status":"active"}}`)
		case "/zones":
			_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"zone-provider-1","name":"example.com","status":"active"}],"result_info":{"page":1,"total_pages":1}}`)
		case "/zones/zone-provider-1/dns_records":
			if r.Method != http.MethodPost {
				t.Errorf("record method = %s", r.Method)
			}
			atomic.AddInt32(&recordCreates, 1)
			_, _ = io.WriteString(w, `{"success":true,"result":{"id":"record-provider-9"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	provider, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		Client:  server.Client(),
		BaseURL: server.URL,
		Secrets: dns.SecretResolverFunc(func(context.Context, dns.CredentialRef) (string, error) {
			return cloudflareTestToken, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := newDNSFixture(t, nil, provider, nil)
	account, err := fixture.service.CreateAccount(context.Background(), "user-1", "req-account", dns.CreateAccountInput{
		ID: "account-1", DisplayName: "Cloudflare", Enabled: true, MakeDefault: true, Credential: "secret/cf-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	test, err := fixture.service.TestAccount(context.Background(), "user-1", "req-test", account.ID)
	if err != nil || !test.OK || test.Status != dns.CredentialValid {
		t.Fatalf("test = %+v, %v", test, err)
	}
	zone, err := fixture.service.CreateZone(context.Background(), "user-1", "req-zone", account.ID, dns.CreateZoneInput{
		ID: "zone-1", ProviderZoneID: "zone-provider-1",
	})
	if err != nil || zone.Name != "example.com" {
		t.Fatalf("zone = %+v, %v", zone, err)
	}
	record, err := fixture.service.CreateRecord(context.Background(), "user-1", "req-record", zone.ID, dns.RecordInput{
		Type: dns.RecordA, Name: "app", Content: "203.0.113.9", TTL: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "record-provider-9" || recordCreates != 1 {
		t.Fatalf("record = %+v, creates = %d", record, recordCreates)
	}
	records, err := fixture.service.ListRecords(context.Background(), "user-1", zone.ID)
	if err != nil || len(records) != 1 || records[0].ID != "record-provider-9" {
		t.Fatalf("records = %+v, %v", records, err)
	}
}
