package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

func enrollmentClientServer(t *testing.T, ca *x509.Certificate, signer *ecdsa.PrivateKey, handler http.Handler, name string) (*httptest.Server, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(111), DNSNames: []string{name}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return server, "https://localhost:" + port
}
func enrollmentClientConfig(ca *x509.Certificate, origin string) agentEnrollmentConfig {
	return agentEnrollmentConfig{Origin: origin, RootCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})), Current: func(context.Context) error { return nil }} // explicit profile fixture
}
func enrollmentClientToken() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{13}, 32))
}
func enrollmentClientIssued(t *testing.T, p agentPreparedIdentity, ca *x509.Certificate, key *ecdsa.PrivateKey) transport.EnrollmentHTTPResult {
	t.Helper()
	issued := issuedIdentityFixture(t, p, ca, key, nil)
	leaf, chain := pem.Decode([]byte(issued.CertificatePEM))
	return transport.EnrollmentHTTPResult{State: "issued", NodeID: issued.NodeID, CertificatePEM: string(pem.EncodeToMemory(leaf)), IntermediateChainPEM: string(chain), TrustGeneration: issued.TrustGeneration, ExpiresAt: issued.ExpiresAt}
}

func TestAgentEnrollmentClientActualTLSUsesSameCSRWithoutProxyOrBrowserCredentials(t *testing.T) {
	s, ca, key, dir := identityStoreFixture(t)
	p, err := s.Prepare(context.Background(), "0.9.0-test")
	if err != nil {
		t.Fatal(err)
	}
	var attempts, proxies atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxies.Add(1); w.WriteHeader(502) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	_, origin := enrollmentClientServer(t, ca, key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input transport.AgentEnrollmentInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.RequestID != p.RequestID || input.CSRPEM != p.CSRPEM || input.Platform != runtime.GOOS || input.EnrollmentToken != enrollmentClientToken() || r.URL.Path != "/api/v1/agent/enroll" || r.Method != "POST" || r.URL.RawQuery != "" || r.TLS.Version != tls.VersionTLS13 || len(r.TLS.PeerCertificates) != 0 {
			t.Error("bootstrap request changed identity or transport")
		}
		for _, header := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Origin", "Idempotency-Key"} {
			if len(r.Header.Values(header)) != 0 {
				t.Error("ambient credential sent", header)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if attempts.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(transport.EnrollmentHTTPResult{State: "pending", ExpiresAt: time.Now().Add(5 * time.Minute)})
		} else {
			_ = json.NewEncoder(w).Encode(enrollmentClientIssued(t, p, ca, key))
		}
	}), "localhost")
	c, err := newAgentEnrollmentClient(s, enrollmentClientConfig(ca, origin))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := c.EnrollOnce(context.Background(), enrollmentClientToken()); err != nil || out.State != "pending" {
		t.Fatal("pending bootstrap failed", out.State, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "issued.json")); !os.IsNotExist(err) {
		t.Fatal("pending installed identity", err)
	}
	if out, err := c.EnrollOnce(context.Background(), enrollmentClientToken()); err != nil || out.State != "issued" {
		t.Fatal("issued bootstrap failed", out.State, err)
	}
	if attempts.Load() != 2 || proxies.Load() != 0 {
		t.Fatal("unexpected retry/proxy", attempts.Load(), proxies.Load())
	}
	if err = s.WithCertificate(context.Background(), func(context.Context, agentIdentityFacts) error { return nil }, func(context.Context, tls.Certificate) error { return nil }); err != nil {
		t.Fatal("stored identity invalid", err)
	}
}

func TestAgentEnrollmentClientRejectsResponsesRedirectsAndPostUseRevocation(t *testing.T) {
	s, ca, key, dir := identityStoreFixture(t)
	p, err := s.Prepare(context.Background(), "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	issued := enrollmentClientIssued(t, p, ca, key)
	valid, _ := json.Marshal(issued)
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(204) }))
	defer target.Close()
	var mu sync.Mutex
	body := string(valid)
	status, contentType, cookie, encoding, location := 200, "application/json", "", "", ""
	_, origin := enrollmentClientServer(t, ca, key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		if cookie != "" {
			w.Header().Set("Set-Cookie", cookie)
		}
		if encoding != "" {
			w.Header().Set("Content-Encoding", encoding)
		}
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}), "localhost")
	for _, test := range []struct {
		name, body                              string
		status                                  int
		contentType, cookie, encoding, location string
	}{
		{"duplicate", strings.Replace(string(valid), `"state":"issued"`, `"state":"pending","state":"issued"`, 1), 200, "application/json", "", "", ""},
		{"case", strings.Replace(string(valid), `"state"`, `"State"`, 1), 200, "application/json", "", "", ""},
		{"null", strings.Replace(string(valid), `"state":"issued"`, `"state":null`, 1), 200, "application/json", "", "", ""},
		{"trailing", string(valid) + `{}`, 200, "application/json", "", "", ""},
		{"legacy-state", strings.Replace(string(valid), `"issued"`, `"approved"`, 1), 200, "application/json", "", "", ""},
		{"pending-with-cert", strings.Replace(string(valid), `"issued"`, `"pending"`, 1), 200, "application/json", "", "", ""},
		{"endpoint", strings.TrimSuffix(string(valid), "}") + `,"agentEndpoint":"https://elsewhere.invalid"}`, 200, "application/json", "", "", ""},
		{"token-echo", strings.TrimSuffix(string(valid), "}") + `,"secret":"` + enrollmentClientToken() + `"}`, 200, "application/json", "", "", ""},
		{"oversize", strings.Repeat(" ", 97<<10), 200, "application/json", "", "", ""},
		{"type", string(valid), 200, "text/plain", "", "", ""},
		{"charset", string(valid), 200, "application/json; charset=ascii", "", "", ""},
		{"cookie", string(valid), 200, "application/json", "session=never", "", ""},
		{"compressed", string(valid), 200, "application/json", "", "gzip", ""},
		{"redirect", string(valid), 307, "application/json", "", "", target.URL},
		{"failed-status", string(valid), 503, "application/json", "", "", ""},
		{"expired", `{"state":"pending","expiresAt":"2020-01-01T00:00:00Z"}`, 200, "application/json", "", "", ""},
		{"denied", `{"state":"denied","expiresAt":"2020-01-01T00:00:00Z"}`, 200, "application/json", "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			mu.Lock()
			body, status, contentType, cookie, encoding, location = test.body, test.status, test.contentType, test.cookie, test.encoding, test.location
			mu.Unlock()
			c, err := newAgentEnrollmentClient(s, enrollmentClientConfig(ca, origin))
			if err != nil {
				t.Fatal(err)
			}
			out, err := c.EnrollOnce(context.Background(), enrollmentClientToken())
			if test.name == "denied" {
				if err != errAgentEnrollmentDenied || out.State != "denied" {
					t.Fatal("denial lost", err)
				}
			} else if err != errAgentEnrollment || out.State != "" {
				t.Fatal("invalid response accepted", out.State, err)
			}
			if _, err = os.Stat(filepath.Join(dir, "issued.json")); !os.IsNotExist(err) {
				t.Fatal("invalid response installed identity", err)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("bootstrap credential followed redirect")
	}
	mu.Lock()
	body, status, contentType, cookie, encoding, location = string(valid), 200, "application/json", "", "", ""
	mu.Unlock()
	for _, blockAt := range []int32{1, 2} {
		var gates atomic.Int32
		cfg := enrollmentClientConfig(ca, origin)
		cfg.Current = func(context.Context) error {
			if gates.Add(1) >= blockAt {
				return errors.New("profile revoked")
			}
			return nil
		}
		c, err := newAgentEnrollmentClient(s, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.EnrollOnce(context.Background(), enrollmentClientToken()); err != errAgentEnrollment {
			t.Fatal("revocation allowed bootstrap", err)
		}
		if _, err = os.Stat(filepath.Join(dir, "issued.json")); !os.IsNotExist(err) {
			t.Fatal("revocation installed identity", err)
		}
	}
}

func TestAgentEnrollmentClientIndependentRootAndCancellation(t *testing.T) {
	s, ca, key, _ := identityStoreFixture(t)
	if _, err := s.Prepare(context.Background(), "0.9.0"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	_, origin := enrollmentClientServer(t, ca, key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}), "localhost")
	for _, bad := range []string{"http://localhost", "https://user@localhost", "https://localhost/", "https://localhost?x=y", "https://127.0.0.1", "https://LOCALHOST"} {
		cfg := enrollmentClientConfig(ca, bad)
		if _, err := newAgentEnrollmentClient(s, cfg); err != errAgentEnrollment {
			t.Fatal("untrusted origin accepted")
		}
	}
	cfg := enrollmentClientConfig(ca, origin)
	cfg.Current = nil
	if _, err := newAgentEnrollmentClient(s, cfg); err != errAgentEnrollment {
		t.Fatal("missing profile accepted")
	}
	_, otherCA, _, _ := identityStoreFixture(t)
	if _, err := newAgentEnrollmentClient(s, enrollmentClientConfig(otherCA, origin)); err != errAgentEnrollment {
		t.Fatal("wrong independent root accepted")
	}
	c, err := newAgentEnrollmentClient(s, enrollmentClientConfig(ca, origin))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = c.EnrollOnce(ctx, enrollmentClientToken()); err != errAgentEnrollment || time.Since(start) > time.Second {
		t.Fatal("request did not cancel", err)
	}
	if calls.Load() > 1 {
		t.Fatal("cancelled attempt retried")
	}
	_, wrongOrigin := enrollmentClientServer(t, ca, key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("token reached wrong DNS identity") }), "other.test")
	wrong, err := newAgentEnrollmentClient(s, enrollmentClientConfig(ca, wrongOrigin))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.EnrollOnce(context.Background(), enrollmentClientToken()); err != errAgentEnrollment {
		t.Fatal("wrong DNS identity accepted", err)
	}
}
