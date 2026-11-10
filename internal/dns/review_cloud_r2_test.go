package dns_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

type reviewR2Mode int

const (
	reviewR2Success reviewR2Mode = iota
	reviewR2MalformedJSON
	reviewR2TruncatedBody
	reviewR2ServerError
	reviewR2ClientError
)

type reviewR2Handler struct {
	mu           sync.Mutex
	createMode   reviewR2Mode
	deleteMode   reviewR2Mode
	createRecord map[string]any
	createID     string
	createCalls  int
	deleteCalls  int
}

func (h *reviewR2Handler) setCreateMode(mode reviewR2Mode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.createMode = mode
}

func (h *reviewR2Handler) setDeleteMode(mode reviewR2Mode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deleteMode = mode
}

func (h *reviewR2Handler) calls() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.createCalls, h.deleteCalls
}

func (h *reviewR2Handler) serveMode(w http.ResponseWriter, mode reviewR2Mode, successBody string) {
	switch mode {
	case reviewR2Success:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(successBody))
	case reviewR2MalformedJSON:
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":tr`))
	case reviewR2TruncatedBody:
		// A response that started but cannot be read to completion: the request
		// may have reached the provider, so the outcome is unknown.
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true`))
	case reviewR2ServerError:
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[]}`))
	case reviewR2ClientError:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1004,"message":"invalid"}]}`))
	}
}

func (h *reviewR2Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user/tokens/verify":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"result":{"status":"active"}}`))
	case r.Method == http.MethodGet && r.URL.Path == "/zones":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"pz-1","name":"example.com","status":"active"}],"result_info":{"page":1,"total_pages":1}}`))
	case r.Method == http.MethodPost && r.URL.Path == "/zones/pz-1/dns_records":
		h.mu.Lock()
		h.createCalls++
		_ = json.NewDecoder(r.Body).Decode(&h.createRecord)
		mode, id := h.createMode, h.createID
		h.mu.Unlock()
		h.serveMode(w, mode, `{"success":true,"result":{"id":"`+id+`"}}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/zones/pz-1/dns_records/"):
		h.mu.Lock()
		record := map[string]any{"id": h.createID, "proxied": false}
		for k, v := range h.createRecord {
			record[k] = v
		}
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": record})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/zones/pz-1/dns_records/"):
		h.mu.Lock()
		h.deleteCalls++
		mode := h.deleteMode
		h.mu.Unlock()
		h.serveMode(w, mode, `{"success":true,"result":{}}`)
	default:
		http.NotFound(w, r)
	}
}

func newReviewR2Fixture(t *testing.T, handler *reviewR2Handler) dnsFixture {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	provider, err := dns.NewCloudflareProvider(dns.CloudflareConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
		Secrets: dns.SecretBorrowerFunc(func(ctx context.Context, _ dns.SecretUse, fn func(context.Context, string) error) error {
			return fn(ctx, "review-token")
		}),
	})
	if err != nil {
		t.Fatalf("NewCloudflareProvider: %v", err)
	}
	fixture := newDNSFixture(t, nil, provider, nil)
	ctx := context.Background()
	account, err := fixture.service.CreateAccount(ctx, "admin", "req-account", dns.CreateAccountInput{
		ID: "acct-1", DisplayName: "Cloudflare", Enabled: true, MakeDefault: true,
		Credential: dns.CredentialRef("secret/cf-1"),
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := fixture.service.TestAccount(ctx, "admin", "req-test", account.ID); err != nil {
		t.Fatalf("TestAccount: %v", err)
	}
	if _, err := fixture.service.CreateZone(ctx, "admin", "req-zone", account.ID, dns.CreateZoneInput{
		ID: "zone-1", ProviderZoneID: "pz-1",
	}); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	return fixture
}

func newReviewR2Publisher(t *testing.T, fixture dnsFixture) *dns.ACMEChallengePublisher {
	t.Helper()
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "admin", RequestID: "req-acme", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatalf("NewACMEChallengePublisher: %v", err)
	}
	return publisher
}

// D1: a local TLS Cloudflare protocol endpoint returns an ambiguous response to
// DELETE. The service must classify it as an unknown outcome and the cleanup
// state machine must never replay the provider delete.
func TestReviewR2UnknownDeleteNeverReplayed(t *testing.T) {
	for _, testCase := range []struct {
		name string
		mode reviewR2Mode
	}{
		{"malformed json", reviewR2MalformedJSON},
		{"truncated body", reviewR2TruncatedBody},
		{"server error", reviewR2ServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := &reviewR2Handler{createID: "rec-fixed"}
			fixture := newReviewR2Fixture(t, handler)
			_, cleanup, err := newReviewR2Publisher(t, fixture).Publish(context.Background(), "_acme-challenge.app.example.com", "value")
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			handler.setDeleteMode(testCase.mode)
			if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
				t.Fatalf("first cleanup err = %v, want unknown outcome", err)
			}
			if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
				t.Fatalf("second cleanup err = %v, want cached unknown outcome", err)
			}
			if _, deletes := handler.calls(); deletes != 1 {
				t.Fatalf("unknown delete was replayed: provider deletes=%d", deletes)
			}
			records, err := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
			if err != nil || len(records) != 1 {
				t.Fatalf("local record was removed after unknown outcome: %+v %v", records, err)
			}
		})
	}
}

// A confirmed rejection is safe to retry explicitly; success then becomes
// idempotent and does not touch the provider again.
func TestReviewR2ConfirmedDeleteRetriedThenIdempotent(t *testing.T) {
	handler := &reviewR2Handler{createID: "rec-fixed"}
	fixture := newReviewR2Fixture(t, handler)
	_, cleanup, err := newReviewR2Publisher(t, fixture).Publish(context.Background(), "_acme-challenge.app.example.com", "value")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	handler.setDeleteMode(reviewR2ClientError)
	if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("confirmed failure err = %v, want ErrProviderFailed", err)
	}
	handler.setDeleteMode(reviewR2Success)
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if _, deletes := handler.calls(); deletes != 2 {
		t.Fatalf("confirmed failure was not retried: deletes=%d", deletes)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
	if _, deletes := handler.calls(); deletes != 2 {
		t.Fatalf("successful cleanup touched the provider again: deletes=%d", deletes)
	}
	records, err := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if err != nil || len(records) != 0 {
		t.Fatalf("record remained after cleanup: %+v %v", records, err)
	}
}

// A create whose response cannot be confirmed is unknown: it may have reached
// the provider, so the service must not report a retryable provider failure and
// must not persist a local record as if the create were proven.
func TestReviewR2UnknownCreateIsNotRetryable(t *testing.T) {
	handler := &reviewR2Handler{createID: "rec-fixed"}
	fixture := newReviewR2Fixture(t, handler)
	handler.setCreateMode(reviewR2MalformedJSON)
	_, err := fixture.service.CreateRecord(context.Background(), "admin", "req-create", "zone-1", dns.RecordInput{
		Type: dns.RecordA, Name: "www", Content: "203.0.113.10", TTL: 120,
	})
	if !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		t.Fatalf("create err = %v, want unknown outcome", err)
	}
	if creates, _ := handler.calls(); creates != 1 {
		t.Fatalf("create was replayed: creates=%d", creates)
	}
	records, listErr := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if listErr != nil || len(records) != 0 {
		t.Fatalf("unconfirmed create persisted a local record: %+v %v", records, listErr)
	}
}

// A successful provider response followed by a local persistence failure has
// already caused a remote side effect. It must not be reported as a retryable
// provider network failure, and the compensation path must delete the remote
// record exactly once.
func TestReviewR2PersistFailureAfterProviderSuccessIsNotRetryClass(t *testing.T) {
	handler := &reviewR2Handler{createID: "rec-fixed"}
	fixture := newReviewR2Fixture(t, handler)
	ctx := context.Background()
	if _, err := fixture.service.CreateRecord(ctx, "admin", "req-create-1", "zone-1", dns.RecordInput{
		Type: dns.RecordA, Name: "first", Content: "203.0.113.10", TTL: 120,
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := fixture.service.CreateRecord(ctx, "admin", "req-create-2", "zone-1", dns.RecordInput{
		Type: dns.RecordA, Name: "second", Content: "203.0.113.11", TTL: 120,
	})
	if err == nil {
		t.Fatal("duplicate provider id unexpectedly persisted")
	}
	if errors.Is(err, dns.ErrProviderOutcomeUnknown) || errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("persistence failure misclassified as provider failure: %v", err)
	}
	creates, deletes := handler.calls()
	if creates != 2 {
		t.Fatalf("provider creates = %d, want 2", creates)
	}
	if deletes != 1 {
		t.Fatalf("provider compensation deletes = %d, want 1", deletes)
	}
}

// D2: the public DNS HTTP surface must expose a distinct, redacted unknown
// outcome instead of collapsing into internal_error.
func TestReviewR2HTTPUnknownOutcomeContract(t *testing.T) {
	handler := &reviewR2Handler{createID: "rec-fixed"}
	fixture := newReviewR2Fixture(t, handler)
	mux := http.NewServeMux()
	if err := dns.RegisterHTTP(mux, fixture.service, func(*http.Request) (dns.Principal, error) {
		return dns.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	handler.setCreateMode(reviewR2MalformedJSON)
	body := strings.NewReader(`{"type":"A","name":"www","content":"203.0.113.10","ttl":120}`)
	response, err := http.Post(server.URL+"/api/v1/dns/zones/zone-1/records", "application/json", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("unknown create status = %d, want 502", response.StatusCode)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload.Error.Code != "provider_outcome_unknown" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
	if strings.Contains(strings.ToLower(payload.Error.Message), "cloudflare") ||
		strings.Contains(payload.Error.Message, "token") {
		t.Fatalf("unknown outcome message leaked provider detail: %q", payload.Error.Message)
	}
}
