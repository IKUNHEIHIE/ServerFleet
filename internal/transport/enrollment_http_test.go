package transport

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type enrollmentDeadlineRecorder struct{ *httptest.ResponseRecorder }

func (enrollmentDeadlineRecorder) SetReadDeadline(time.Time) error { return nil } // explicit unit transport double

func enrollmentHTTPFixture(t *testing.T) (*EnrollmentHTTP, *EnrollmentService, *enrollmentTestStore, AgentEnrollmentInput) {
	t.Helper()
	s, store, _ := enrollmentFixture(t)
	token, err := s.IssueToken(context.Background(), EnrollmentOperation{ActorID: "test-admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "Local enrollment test", TTLSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewEnrollmentHTTP(s, EnrollmentHTTPConfig{Authority: "fleet.test", ServerName: "fleet.test", MaxConcurrent: 4, MaxIPs: 32, PerIPPerMinute: 100, GlobalPerMinute: 600, Current: func(context.Context) error { return nil }, Rejected: func(context.Context, EnrollmentHTTPRejection) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return h, s, store, AgentEnrollmentInput{token.Token, "request-1", enrollmentCSR(t), s.issuer.rootPin, "0.9.0-test", "linux"}
}

func enrollmentTestRequest(h *EnrollmentHTTP, body string, alter func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "https://fleet.test/api/v1/agent/enroll", strings.NewReader(body))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13, HandshakeComplete: true, ServerName: "fleet.test"}
	r.Header.Set("Content-Type", "application/json")
	if alter != nil {
		alter(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(enrollmentDeadlineRecorder{w}, r)
	return w
}

func TestEnrollmentHTTPActualTLSIsPendingUntilExplicitDecisionAndWireIssued(t *testing.T) {
	h, s, store, input := enrollmentHTTPFixture(t)
	root := s.issuer.certificate
	key := s.issuer.key.(*ecdsa.PrivateKey)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	server := httptest.NewUnstartedServer(h)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{testPeerCert(t, root, key, true)}}
	server.StartTLS()
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "fleet.test"}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	request := func() (int, EnrollmentHTTPResult) {
		raw, _ := json.Marshal(input)
		r, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/agent/enroll", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "fleet.test"
		r.Header.Set("Content-Type", "application/json")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || bytes.Contains(body, []byte(input.EnrollmentToken)) || bytes.Contains(body, []byte("PRIVATE KEY")) {
			t.Fatal("unsafe enrollment response", err)
		}
		var out EnrollmentHTTPResult
		if response.StatusCode == 200 && json.Unmarshal(body, &out) != nil {
			t.Fatal("bad enrollment response")
		}
		return response.StatusCode, out
	}
	code, pending := request()
	if code != 200 || pending.State != "pending" || pending.NodeID != "" || pending.CertificatePEM != "" || pending.IntermediateChainPEM != "" || pending.TrustGeneration != 0 {
		t.Fatal("pending request issued identity", code, pending.State)
	}
	list, err := s.List(context.Background(), EnrollmentOperation{ActorID: "test-admin", Action: "enrollment.list"})
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	op := EnrollmentOperation{ActorID: "test-admin", Action: "enrollment.decide", EnrollmentID: list[0].ID, NodeID: list[0].NodeID, ExpectedRevision: list[0].Revision}
	store.fail = true
	if _, err = s.Decide(context.Background(), op, true); err != ErrEnrollmentUnavailable {
		t.Fatal(err)
	}
	store.fail = false
	if code, out := request(); code != 200 || out.State != "pending" || out.CertificatePEM != "" {
		t.Fatal("failed approval commit exposed certificate", code)
	}
	if _, err = s.Decide(context.Background(), op, true); err != nil {
		t.Fatal(err)
	}
	code, issued := request()
	if code != 200 || issued.State != "issued" || issued.NodeID != op.NodeID || issued.TrustGeneration != 1 || issued.ExpiresAt.IsZero() {
		t.Fatal("wire state mismatched", code, issued.State)
	}
	leaf, rest := pem.Decode([]byte(issued.CertificatePEM))
	if leaf == nil || len(bytes.TrimSpace(rest)) != 0 || issued.IntermediateChainPEM == "" {
		t.Fatal("leaf and chain not separated")
	}
	cert, err := x509.ParseCertificate(leaf.Bytes)
	if err != nil || cert.Subject.CommonName != op.NodeID || cert.VerifyHostname("evil.test") == nil {
		t.Fatal("CSR claimed identity copied", err)
	}
}

func TestEnrollmentHTTPRejectsBrowserAmbiguousSourcesAndInvalidBodies(t *testing.T) {
	h, _, store, input := enrollmentHTTPFixture(t)
	raw, _ := json.Marshal(input)
	before := bytes.Clone(store.data)
	for _, test := range []struct {
		name   string
		status int
		modify func(*http.Request)
	}{
		{"plain", 403, func(r *http.Request) { r.TLS = nil }},
		{"old-tls", 403, func(r *http.Request) { r.TLS.Version = tls.VersionTLS12 }},
		{"sni", 403, func(r *http.Request) { r.TLS.ServerName = "another.test" }},
		{"host", 403, func(r *http.Request) { r.Host = "another.test" }},
		{"cookie", 403, func(r *http.Request) { r.Header.Set("Cookie", "") }},
		{"bearer", 403, func(r *http.Request) { r.Header.Set("Authorization", "Bearer not-accepted") }},
		{"origin", 403, func(r *http.Request) { r.Header.Set("Origin", "https://fleet.test") }},
		{"fetch", 403, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "none") }},
		{"client-certificate", 403, func(r *http.Request) { r.TLS.PeerCertificates = []*x509.Certificate{{}} }},
		{"query", 400, func(r *http.Request) { r.URL.RawQuery = "token=not-accepted" }},
		{"method", 405, func(r *http.Request) { r.Method = "HEAD" }},
		{"type", 400, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"zip", 400, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := enrollmentTestRequest(h, string(raw), test.modify)
			if w.Code != test.status || strings.Contains(w.Body.String(), input.EnrollmentToken) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, body := range []string{`null`, `[]`, `{}`, string(raw) + `{}`, strings.Replace(string(raw), `"platform":"linux"`, `"platform":"linux","platform":"linux"`, 1), strings.Replace(string(raw), `"platform":"linux"`, `"Platform":"linux"`, 1), strings.Replace(string(raw), `"platform":"linux"`, `"platform":null`, 1), strings.Repeat(" ", 33<<10)} {
		if w := enrollmentTestRequest(h, body, nil); w.Code != 400 {
			t.Fatal("malformed body admitted", w.Code)
		}
	}
	if !bytes.Equal(store.data, before) {
		t.Fatal("rejected HTTP source touched enrollment store")
	}
}

func TestEnrollmentHTTPAdmissionBudgetCannotBeResetByForwardedIP(t *testing.T) {
	h, _, _, _ := enrollmentHTTPFixture(t)
	h.cfg.PerIPPerMinute = 1
	h.cfg.MaxIPs = 1
	h.cfg.GlobalPerMinute = 2
	clock := time.Now().UTC()
	h.now = func() time.Time { return clock }
	if w := enrollmentTestRequest(h, `{}`, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := enrollmentTestRequest(h, `{}`, func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.8") }); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("forwarded IP reset budget", w.Code)
	}
	if w := enrollmentTestRequest(h, `{}`, func(r *http.Request) { r.RemoteAddr = "203.0.113.9:1234" }); w.Code != 429 {
		t.Fatal("IP map cap bypassed", w.Code)
	}
	clock = clock.Add(-time.Minute)
	if w := enrollmentTestRequest(h, `{}`, nil); w.Code != 503 {
		t.Fatal("clock rollback reset budget", w.Code)
	}
	clock = clock.Add(2 * time.Minute)
	if w := enrollmentTestRequest(h, `{}`, nil); w.Code != 400 {
		t.Fatal("new minute did not admit", w.Code)
	}
}

func TestEnrollmentHTTPMissingCurrentOrAuditAndFinalRevocationDoNotIssue(t *testing.T) {
	h, s, store, input := enrollmentHTTPFixture(t)
	if _, err := NewEnrollmentHTTP(s, EnrollmentHTTPConfig{}); err != ErrEnrollmentUnavailable {
		t.Fatal("unbounded/missing callbacks accepted", err)
	}
	raw, _ := json.Marshal(input)
	before := bytes.Clone(store.data)
	h.cfg.Current = func(context.Context) error { return errors.New("quarantined") }
	if w := enrollmentTestRequest(h, string(raw), nil); w.Code != 503 || !bytes.Equal(before, store.data) {
		t.Fatal("quarantined request committed", w.Code)
	}
	h.cfg.Current = func(context.Context) error { return nil }
	h.cfg.Rejected = func(context.Context, EnrollmentHTTPRejection) error { return errors.New("audit unavailable") }
	if w := enrollmentTestRequest(h, `{}`, nil); w.Code != 503 {
		t.Fatal("audit failure hidden", w.Code)
	}
	h.cfg.Rejected = func(context.Context, EnrollmentHTTPRejection) error { panic("synthetic error") }
	if w := enrollmentTestRequest(h, `{}`, nil); w.Code != 503 {
		t.Fatal("audit panic escaped", w.Code)
	}
	h.cfg.Rejected = func(context.Context, EnrollmentHTTPRejection) error { return nil }
	checks := 0
	h.cfg.Current = func(context.Context) error {
		checks++
		if checks > 1 {
			return errors.New("profile changed")
		}
		return nil
	}
	if w := enrollmentTestRequest(h, string(raw), nil); w.Code != 503 || strings.Contains(w.Body.String(), "pending") {
		t.Fatal("revoked response disclosed", w.Code)
	}
}
