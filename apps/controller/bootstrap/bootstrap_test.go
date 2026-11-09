package bootstrap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOptionsDefaultsAndValidation(t *testing.T) {
	options, err := ParseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !options.Scaffold() || options.Listen != "127.0.0.1:8080" {
		t.Fatalf("unexpected defaults: %+v", options)
	}
	if err := options.Validate(); err != nil {
		t.Fatalf("scaffold validate: %v", err)
	}
	if _, err := ParseOptions([]string{"-db", "x.db"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOptions([]string{"-db", "x.db", "-unknown"}, io.Discard); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if _, err := ParseOptions([]string{"extra"}, io.Discard); err == nil {
		t.Fatal("positional argument accepted")
	}

	management, err := ParseOptions([]string{
		"-listen", "127.0.0.1:9443", "-db", "fleet.db", "-origin", "https://console.local",
		"-tls-cert", "cert.pem", "-tls-key", "key.pem",
		"-agent-pki-mode", "local-development", "-agent-pki-dir", "/tmp/pki", "-agent-authority", "fleet.local:9443", "-agent-server-name", "fleet.local",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if management.Scaffold() || management.Validate() != nil {
		t.Fatalf("unexpected management options: %+v", management)
	}
	incomplete := management
	incomplete.TLSKey = ""
	if err := incomplete.Validate(); err == nil || !strings.Contains(err.Error(), "-tls-key") {
		t.Fatalf("incomplete management options = %v", err)
	}
}

func TestDefaultAgentAuthority(t *testing.T) {
	if authority, err := DefaultAgentAuthority("127.0.0.1:8443"); err != nil || authority != "localhost:8443" {
		t.Fatalf("loopback authority = %q %v", authority, err)
	}
	if authority, err := DefaultAgentAuthority("host.example:8443"); err != nil || authority != "host.example:8443" {
		t.Fatalf("host authority = %q %v", authority, err)
	}
	if _, err := DefaultAgentAuthority("missing-port"); err == nil {
		t.Fatal("invalid listen accepted")
	}
}

func TestComposeInitializesLocalAgentPKI(t *testing.T) {
	dir := t.TempDir()
	options := Options{
		AgentPKIMode: "local-development",
		Listen:       "127.0.0.1:9443",
		DBPath:       filepath.Join(dir, "controller.db"),
		Origin:       "https://console.local",
		TLSCert:      "unused-cert.pem",
		TLSKey:       "unused-key.pem",
	}
	ctx := context.Background()
	composed, material, err := Compose(ctx, options)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	defer func() { _ = composed.Close() }()
	if material == nil || material.CertificatePath != filepath.Join(dir, "pki", "local-agent-ca.pem") {
		t.Fatalf("unexpected material: %+v", material)
	}
	if composed.EnrollmentHTTP == nil || composed.EnrollmentManagement == nil || composed.AgentLinkHTTP == nil {
		t.Fatal("agent transport was not mounted by the CLI composition")
	}

	tlsServer := httptest.NewTLSServer(composed)
	defer tlsServer.Close()
	response, err := tlsServer.Client().Post(tlsServer.URL+"/api/v1/enrollment-tokens", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode == http.StatusServiceUnavailable || bytes.Contains(body, []byte("AGENT_TRANSPORT_UNAVAILABLE")) {
		t.Fatalf("management enrollment route fell back to unavailable: %d %s", response.StatusCode, body)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous enrollment management = %d: %s", response.StatusCode, body)
	}

	enroll, err := tlsServer.Client().Post(tlsServer.URL+"/api/v1/agent/enroll", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(enroll.Body)
	enroll.Body.Close()
	if enroll.StatusCode == http.StatusServiceUnavailable || bytes.Contains(body, []byte("AGENT_TRANSPORT_UNAVAILABLE")) {
		t.Fatalf("agent enrollment route fell back to unavailable: %d %s", enroll.StatusCode, body)
	}

	composed.Close()
	reopened, reloaded, err := Compose(ctx, options)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if reloaded.RootSPKI != material.RootSPKI {
		t.Fatal("local PKI changed across Controller restarts")
	}
}

func TestComposeFailsClosedOnPartialLocalPKI(t *testing.T) {
	dir := t.TempDir()
	pkiDir := filepath.Join(dir, "pki")
	if err := os.MkdirAll(pkiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkiDir, "local-agent-ca-key.pem"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Compose(context.Background(), Options{
		AgentPKIMode: "local-development",
		Listen:       "127.0.0.1:9443", DBPath: filepath.Join(dir, "controller.db"),
		Origin: "https://console.local", TLSCert: "cert.pem", TLSKey: "key.pem",
	})
	if err == nil {
		t.Fatal("partial local PKI was accepted")
	}
}

func TestTrustedUpdateAndCleanupFlagsRequireManagementConfiguration(t *testing.T) {
	options, err := ParseOptions([]string{"-update-deployment", "/private/update.json", "-disable-cleanup-retries"}, io.Discard)
	if err != nil || options.UpdateDeployment != "/private/update.json" || !options.DisableCleanupRetries {
		t.Fatalf("flags: %+v %v", options, err)
	}
	if options.Validate() == nil {
		t.Fatal("update deployment accepted in health scaffold")
	}
	options.DBPath = "test.db"
	options.Origin = "https://localhost:8443"
	options.TLSCert = "cert.pem"
	options.TLSKey = "key.pem"
	options.AgentRootPin = strings.Repeat("a", 64)
	if err := options.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleAndVersionOptionsDoNotStartAnInsecureManagementHost(t *testing.T) {
	options, err := ParseOptions([]string{"-web-dir", "/private/web"}, io.Discard)
	if err != nil || options.WebDirectory != "/private/web" || options.Validate() == nil {
		t.Fatalf("console allowed without TLS management: %+v %v", options, err)
	}
	options, err = ParseOptions([]string{"-version"}, io.Discard)
	if err != nil || !options.ShowVersion {
		t.Fatalf("version flags: %+v %v", options, err)
	}
}
