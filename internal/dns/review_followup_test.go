package dns_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"serverfleet.local/serverfleet/internal/dns"
	"strings"
	"testing"
)

func TestReviewFollowupCreateSuccessWithoutIdentityUnknown(t *testing.T) {
	for _, body := range []string{`{"success":true}`, `{"success":true,"result":null}`, `{"success":true,"result":{}}`, `{"success":true,"result":{"id":"\u0000bad"}}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer s.Close()
			p := cloudflareServerProvider(t, s)
			_, err := p.CreateRecord(context.Background(), "secret/cf-1", "pz-1", dns.RecordInput{Type: dns.RecordTXT, Name: "_acme-challenge.test.example", Content: "test", TTL: 60})
			if !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
				t.Fatalf("possibly applied CREATE classified as certain failure: %v", err)
			}
		})
	}
}

func TestReviewFollowupMissingCreateIDThroughServiceAndHTTP(t *testing.T) {
	for _, body := range []string{`{"success":true}`, `{"success":true,"result":null}`, `{"success":true,"result":{}}`, `{"success":true,"result":{"id":"\u0000bad"}}`} {
		t.Run(body, func(t *testing.T) {
			h := &reviewR2Handler{createID: "unused"}
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/zones/pz-1/dns_records" {
					w.Write([]byte(body))
					return
				}
				h.ServeHTTP(w, r)
			}))
			defer ts.Close()
			provider := cloudflareServerProvider(t, ts)
			f := newDNSFixture(t, nil, provider, nil)
			ctx := context.Background()
			account, err := f.service.CreateAccount(ctx, "admin", "r-account", dns.CreateAccountInput{ID: "acct-1", DisplayName: "Cloudflare", Enabled: true, MakeDefault: true, Credential: dns.CredentialRef("secret/cf-1")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.TestAccount(ctx, "admin", "r-test", account.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.CreateZone(ctx, "admin", "r-zone", account.ID, dns.CreateZoneInput{ID: "zone-1", ProviderZoneID: "pz-1"}); err != nil {
				t.Fatal(err)
			}
			_, err = f.service.CreateRecord(ctx, "admin", "r-create", "zone-1", dns.RecordInput{Type: dns.RecordTXT, Name: "_acme-challenge.test.example.com", Content: "test", TTL: 60})
			if !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
				t.Errorf("service hid possibly applied create: %v", err)
			}
			mux := http.NewServeMux()
			if err = dns.RegisterHTTP(mux, f.service, func(*http.Request) (dns.Principal, error) {
				return dns.Principal{SubjectID: "admin", RequestID: "r-http"}, nil
			}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/dns/zones/zone-1/records", strings.NewReader(`{"type":"TXT","name":"_acme-challenge.test.example.com","content":"test","ttl":60}`))
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != 502 || !strings.Contains(rr.Body.String(), "provider_outcome_unknown") {
				t.Errorf("HTTP lost unknown result: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}
