package monitoring_test

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/monitoring"
)

type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]net.IPAddr
	err     error
	calls   int
	hosts   []string
}

func (r *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.hosts = append(r.hosts, host)
	if r.err != nil {
		return nil, r.err
	}
	return append([]net.IPAddr(nil), r.answers[host]...), nil
}

func (r *fakeResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func mustProber(t *testing.T, cfg monitoring.NetProberConfig) *monitoring.NetProber {
	t.Helper()
	prober, err := monitoring.NewNetProber(cfg)
	if err != nil {
		t.Fatalf("NewNetProber: %v", err)
	}
	return prober
}

func probeCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected probe error")
	}
	var classified *monitoring.ProbeError
	if !errors.As(err, &classified) {
		t.Fatalf("error %v (%T) is not a *ProbeError", err, err)
	}
	return classified.Code
}

func TestNetProberHTTPHealthyUsesRealTransport(t *testing.T) {
	const secretBody = "internal-secret-payload"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Internal-Token", "header-secret")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer server.Close()

	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	result, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: server.URL + "/health", Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !result.Healthy || result.StatusCode == nil || *result.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v", result)
	}
	if result.Latency == nil || *result.Latency < 0 {
		t.Fatalf("latency = %v", result.Latency)
	}
	if result.Reason != "" {
		t.Fatalf("healthy reason = %q", result.Reason)
	}
}

func TestNetProberHTTPStatusFailureDoesNotEchoBody(t *testing.T) {
	const body = "database-password=hunter2"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	result, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: server.URL, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Healthy || result.StatusCode == nil || *result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("result = %+v", result)
	}
	if result.Reason != "http_status_503" {
		t.Fatalf("reason = %q", result.Reason)
	}
	if strings.Contains(result.Reason, "hunter2") || strings.Contains(result.Reason, "password") {
		t.Fatalf("reason leaked body: %q", result.Reason)
	}
}

func TestNetProberRejectsDNSRebindingToLoopback(t *testing.T) {
	resolver := &fakeResolver{answers: map[string][]net.IPAddr{
		"rebind.example": {{IP: net.ParseIP("127.0.0.1")}},
	}}
	prober := mustProber(t, monitoring.NetProberConfig{Resolver: resolver})

	_, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: "http://rebind.example:18080/", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if resolver.count() != 1 {
		t.Fatalf("resolver calls = %d", resolver.count())
	}

	_, err = prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: "rebind.example:18080", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("tcp code = %s err=%v", code, err)
	}
}

func TestNetProberRejectsMixedPublicAndPrivateAnswers(t *testing.T) {
	resolver := &fakeResolver{answers: map[string][]net.IPAddr{
		"mixed.example": {
			{IP: net.ParseIP("93.184.216.34")},
			{IP: net.ParseIP("10.0.0.5")},
		},
	}}
	prober := mustProber(t, monitoring.NetProberConfig{Resolver: resolver})
	_, err := prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: "mixed.example:443", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("code = %s err=%v", code, err)
	}
}

func TestNetProberLoopbackAllowedOnlyForExplicitHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{answers: map[string][]net.IPAddr{
		"allowed.example": {{IP: net.ParseIP("127.0.0.1")}},
	}}
	target := "http://allowed.example:" + port + "/"

	allowed := mustProber(t, monitoring.NetProberConfig{
		AllowedHosts: []string{"allowed.example"}, Resolver: resolver,
	})
	result, err := allowed.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: target, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("allowlisted probe: %v", err)
	}
	if !result.Healthy {
		t.Fatalf("allowlisted result = %+v", result)
	}

	ipOnly := mustProber(t, monitoring.NetProberConfig{
		AllowedHosts: []string{"127.0.0.1"}, Resolver: resolver,
	})
	_, err = ipOnly.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: target, Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("ip-only allowlist code = %s err=%v", code, err)
	}
}

func TestNetProberRejectsMetadataAndLocalhostNames(t *testing.T) {
	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{
		"169.254.169.254", "metadata.google.internal", "localhost",
	}})
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://localhost:8080/",
		"http://service.localhost/",
	} {
		_, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
			Kind: monitoring.KindHTTP, Target: target, Timeout: time.Second,
		})
		if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
			t.Fatalf("%s code = %s err=%v", target, code, err)
		}
	}
}

func TestNetProberRejectsUserInfoAndInvalidTargets(t *testing.T) {
	prober := mustProber(t, monitoring.NetProberConfig{})
	_, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: "http://user:pass@127.0.0.1:8080/", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("userinfo code = %s err=%v", code, err)
	}
	if strings.Contains(err.Error(), "pass") {
		t.Fatalf("error echoed userinfo: %v", err)
	}
	for _, target := range []string{"ftp://127.0.0.1/", "127.0.0.1:8080", "http://127.0.0.1:70000/"} {
		_, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
			Kind: monitoring.KindHTTP, Target: target, Timeout: time.Second,
		})
		if code := probeCode(t, err); code != monitoring.ProbeCodeInvalidTarget {
			t.Fatalf("%s code = %s err=%v", target, code, err)
		}
	}
	_, err = prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: "127.0.0.1", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeInvalidTarget {
		t.Fatalf("tcp missing port code = %s err=%v", code, err)
	}
}

func TestNetProberTCPProbe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	result, err := prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: address, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("tcp probe: %v", err)
	}
	if !result.Healthy || result.StatusCode != nil {
		t.Fatalf("result = %+v", result)
	}
	_ = listener.Close()

	_, err = prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: address, Timeout: 500 * time.Millisecond,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeDialFailed {
		t.Fatalf("closed listener code = %s err=%v", code, err)
	}

	restricted := mustProber(t, monitoring.NetProberConfig{})
	_, err = restricted.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: address, Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTargetForbidden {
		t.Fatalf("restricted code = %s err=%v", code, err)
	}
}

func TestNetProberTLSProbeVerifiesCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	address := server.Listener.Addr().String()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	trusted := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}, RootCAs: pool})
	result, err := trusted.TLS(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTLS, Target: address, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("tls probe: %v", err)
	}
	if !result.Healthy || result.StatusCode != nil {
		t.Fatalf("result = %+v", result)
	}

	untrusted := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	_, err = untrusted.TLS(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTLS, Target: address, Timeout: 3 * time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTLSFailed {
		t.Fatalf("untrusted code = %s err=%v", code, err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()
	_, err = trusted.TLS(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTLS, Target: plain.Listener.Addr().String(), Timeout: 3 * time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTLSFailed {
		t.Fatalf("plain server code = %s err=%v", code, err)
	}
}

func TestNetProberHTTPSUsesVerifiedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}, RootCAs: pool})
	result, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: server.URL + "/secure", Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("https probe: %v", err)
	}
	if !result.Healthy || result.StatusCode == nil || *result.StatusCode != http.StatusNoContent {
		t.Fatalf("result = %+v", result)
	}
}

func TestNetProberHTTPRespectsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	start := time.Now()
	_, err := prober.HTTP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindHTTP, Target: server.URL, Timeout: 50 * time.Millisecond,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeTimeout {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout not enforced: %v", elapsed)
	}
}

func TestNetProberPropagatesCallerCancellation(t *testing.T) {
	prober := mustProber(t, monitoring.NetProberConfig{AllowedHosts: []string{"127.0.0.1"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := prober.TCP(ctx, monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: "127.0.0.1:1", Timeout: time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestNetProberDNSFailureIsClassified(t *testing.T) {
	resolver := &fakeResolver{err: errors.New("name not found")}
	prober := mustProber(t, monitoring.NetProberConfig{Resolver: resolver})
	_, err := prober.TCP(context.Background(), monitoring.TargetSpec{
		Kind: monitoring.KindTCP, Target: "missing.example:443", Timeout: time.Second,
	})
	if code := probeCode(t, err); code != monitoring.ProbeCodeDNSFailure {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if strings.Contains(err.Error(), "name not found") {
		t.Fatalf("resolver detail leaked into error: %v", err)
	}
}

func TestNewNetProberRejectsInvalidAllowedHosts(t *testing.T) {
	if _, err := monitoring.NewNetProber(monitoring.NetProberConfig{AllowedHosts: []string{"not a host/"}}); !errors.Is(err, monitoring.ErrInvalidAllowedHosts) {
		t.Fatalf("error = %v", err)
	}
}
