package dns_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

const cloudflareTestToken = "cf-test-token-abc123"

func cloudflareProvider(t *testing.T, baseURL string) *dns.CloudflareProvider {
	t.Helper()
	return cloudflareProviderWithClient(t, baseURL, &http.Client{})
}

func cloudflareServerProvider(t *testing.T, server *httptest.Server) *dns.CloudflareProvider {
	t.Helper()
	return cloudflareProviderWithClient(t, server.URL, server.Client())
}

func cloudflareProviderWithClient(t *testing.T, baseURL string, client *http.Client) *dns.CloudflareProvider {
	t.Helper()
	provider, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		Client:  client,
		BaseURL: baseURL,
		Secrets: dns.SecretBorrowerFunc(func(ctx context.Context, use dns.SecretUse, fn func(context.Context, string) error) error {
			if use.Ref != "secret/cf-1" {
				return errors.New("unknown ref")
			}
			return fn(ctx, cloudflareTestToken)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestCloudflareTestCredential(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   dns.CredentialStatus
	}{
		{"active", 200, `{"success":true,"result":{"id":"t1","status":"active"}}`, dns.CredentialValid},
		{"inactive", 200, `{"success":true,"result":{"id":"t1","status":"disabled"}}`, dns.CredentialInvalid},
		{"unauthorized", 401, `{"success":false,"errors":[{"code":1000,"message":"bad token ` + cloudflareTestToken + `"}]}`, dns.CredentialInvalid},
		{"forbidden", 403, `{"success":false,"errors":[{"code":9109,"message":"unauthorized"}]}`, dns.CredentialMissingPermissions},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/user/tokens/verify" || r.Method != http.MethodGet {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+cloudflareTestToken {
					t.Errorf("authorization = %q", r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			provider := cloudflareServerProvider(t, server)
			result, err := provider.Test(context.Background(), "secret/cf-1")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if result.Status != test.want || result.OK != (test.want == dns.CredentialValid) {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestCloudflareTokenNeverAppearsInErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":9,"message":"token `+cloudflareTestToken+` exploded"}]}`)
	}))
	defer server.Close()
	provider := cloudflareServerProvider(t, server)
	_, err := provider.Test(context.Background(), "secret/cf-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), cloudflareTestToken) {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestCloudflareListZonesPaginatesAndDeduplicates(t *testing.T) {
	var pages int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+cloudflareTestToken {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		page := r.URL.Query().Get("page")
		atomic.AddInt32(&pages, 1)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"z1","name":"example.com","status":"active"},{"id":"z2","name":"example.net","status":"active"}],"result_info":{"page":1,"total_pages":2}}`)
		case "2":
			_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"z2","name":"example.net","status":"active"},{"id":"z3","name":"example.org","status":"pending"}],"result_info":{"page":2,"total_pages":2}}`)
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	defer server.Close()
	provider := cloudflareServerProvider(t, server)
	zones, err := provider.ListZones(context.Background(), "secret/cf-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 3 || zones[0].ProviderZoneID != "z1" || zones[2].ProviderZoneID != "z3" {
		t.Fatalf("zones = %+v", zones)
	}
	if pages != 2 {
		t.Fatalf("pages = %d", pages)
	}
}

func TestCloudflareRecordCRUDWritesProviderPayload(t *testing.T) {
	type request struct {
		method string
		path   string
		body   map[string]any
	}
	var mu sync.Mutex
	var requests []request
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		requests = append(requests, request{method: r.Method, path: r.URL.Path, body: decoded})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"result":{"id":"record-1","type":"A"}}`)
	}))
	defer server.Close()
	provider := cloudflareServerProvider(t, server)
	priority := 10
	id, err := provider.CreateRecord(context.Background(), "secret/cf-1", "zone-1", dns.RecordInput{
		Type: dns.RecordA, Name: "app.example.com", Content: "203.0.113.10", TTL: 300, Proxied: true,
	})
	if err != nil || id != "record-1" {
		t.Fatalf("create = %q, %v", id, err)
	}
	if err := provider.UpdateRecord(context.Background(), "secret/cf-1", "zone-1", "record-1", dns.RecordInput{
		Type: dns.RecordMX, Name: "example.com", Content: "mail.example.com", TTL: 600, Priority: &priority,
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.DeleteRecord(context.Background(), "secret/cf-1", "zone-1", "record-1"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v", requests)
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/zones/zone-1/dns_records" ||
		requests[0].body["type"] != "A" || requests[0].body["proxied"] != true || requests[0].body["ttl"] != float64(300) {
		t.Fatalf("create request = %+v", requests[0])
	}
	if requests[1].method != http.MethodPatch || requests[1].path != "/zones/zone-1/dns_records/record-1" ||
		requests[1].body["priority"] != float64(10) || requests[1].body["proxied"] != nil {
		t.Fatalf("update request = %+v", requests[1])
	}
	if requests[2].method != http.MethodDelete || requests[2].path != "/zones/zone-1/dns_records/record-1" {
		t.Fatalf("delete request = %+v", requests[2])
	}
}

func TestCloudflareRedirectIsRefused(t *testing.T) {
	var leaked int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&leaked, 1)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("authorization leaked to redirect target")
		}
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer source.Close()
	provider := cloudflareServerProvider(t, source)
	_, err := provider.Test(context.Background(), "secret/cf-1")
	if !errors.Is(err, dns.ErrCloudflareRedirect) {
		t.Fatalf("err = %v", err)
	}
	if leaked != 0 {
		t.Fatalf("redirect target requests = %d", leaked)
	}
}

func TestCloudflareConfigAndSecretFailures(t *testing.T) {
	if _, err := dns.NewCloudflareProvider(dns.CloudflareConfig{}); !errors.Is(err, dns.ErrCloudflareConfig) {
		t.Fatalf("missing resolver err = %v", err)
	}
	for _, base := range []string{"http://api.cloudflare.com/client/v4", "https://user:pass@example.com", "not a url"} {
		_, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
			BaseURL: base,
			Secrets: dns.SecretBorrowerFunc(func(ctx context.Context, _ dns.SecretUse, fn func(context.Context, string) error) error {
				return fn(ctx, "x")
			}),
		})
		if !errors.Is(err, dns.ErrCloudflareConfig) {
			t.Fatalf("base %q err = %v", base, err)
		}
	}
	provider, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		BaseURL: "https://api.cloudflare.com/client/v4",
		Secrets: dns.SecretBorrowerFunc(func(context.Context, dns.SecretUse, func(context.Context, string) error) error {
			return errors.New("store offline")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Test(context.Background(), "secret/cf-1"); !errors.Is(err, dns.ErrCloudflareSecret) {
		t.Fatalf("resolver err = %v", err)
	}
}

func TestCloudflareRejectsInvalidRecordInput(t *testing.T) {
	provider := cloudflareProvider(t, "https://api.cloudflare.com/client/v4")
	priority := 10
	invalid := []dns.RecordInput{
		{Type: dns.RecordTXT, Name: "x", Content: "y", TTL: 60, Proxied: true},
		{Type: dns.RecordA, Name: "x", Content: "203.0.113.1", TTL: 60, Priority: &priority},
		{Type: dns.RecordMX, Name: "x", Content: "mail.x", TTL: 60},
	}
	for _, input := range invalid {
		if _, err := provider.CreateRecord(context.Background(), "secret/cf-1", "zone-1", input); !errors.Is(err, dns.ErrInvalidInput) {
			t.Fatalf("input %+v err = %v", input, err)
		}
	}
}

func TestCloudflareNotFoundIsTyped(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":81044,"message":"record not found"}]}`)
	}))
	defer server.Close()
	provider := cloudflareServerProvider(t, server)
	if err := provider.DeleteRecord(context.Background(), "secret/cf-1", "zone-1", "record-1"); !errors.Is(err, dns.ErrProviderNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCloudflareBorrowBoundaries(t *testing.T) {
	var requests int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"result":{"status":"active"}}`)
	}))
	defer server.Close()

	pre, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		Client: server.Client(), BaseURL: server.URL,
		Secrets: dns.SecretBorrowerFunc(func(context.Context, dns.SecretUse, func(context.Context, string) error) error {
			return errors.New("revoked before use")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.Test(context.Background(), "secret/cf-1"); !errors.Is(err, dns.ErrCloudflareSecret) {
		t.Fatalf("pre err = %v", err)
	}
	if requests != 0 {
		t.Fatalf("provider I/O ran after the borrow pre-check failed: %d", requests)
	}

	post, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		Client: server.Client(), BaseURL: server.URL,
		Secrets: dns.SecretBorrowerFunc(func(ctx context.Context, use dns.SecretUse, fn func(context.Context, string) error) error {
			if err := fn(ctx, cloudflareTestToken); err != nil {
				return err
			}
			return errors.New("revoked after use")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := post.Test(context.Background(), "secret/cf-1"); err == nil {
		t.Fatal("post-borrow failure was reported as success")
	}
	if requests == 0 {
		t.Fatal("provider I/O did not run inside the borrow callback")
	}
}
