package platform_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/bootstrap"
	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
)

func bootstrapOptions(dir, portText, origin string) bootstrap.Options {
	return bootstrap.Options{
		Listen:  "127.0.0.1:" + portText,
		DBPath:  filepath.Join(dir, "controller-cli.db"),
		Origin:  origin,
		TLSCert: filepath.Join(dir, "tls-server.pem"),
		TLSKey:  filepath.Join(dir, "tls-server-key.pem"),
	}
}

func composeController(ctx context.Context, options bootstrap.Options) (*server.Server, *localpki.Material, error) {
	return bootstrap.Compose(ctx, options)
}

func writeCLITLSCertificate(t *testing.T, pki *testPKI, certPath, keyPath string) {
	t.Helper()
	var chain bytes.Buffer
	for _, der := range pki.serverCert.Certificate {
		if err := pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(certPath, chain.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(pki.serverCert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestWave4ControllerCLILocalPKIEnrollment drives the same composition the
// standalone controller CLI uses (bootstrap.Compose default local PKI) and runs
// a real HTTPS token -> pending -> approval -> issued Agent enrollment.
func TestWave4ControllerCLILocalPKIEnrollment(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portText := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + portText

	options := bootstrapOptions(dir, portText, origin)
	composed, material, err := composeController(ctx, options)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	defer func() { _ = composed.Close() }()
	if composed.EnrollmentHTTP == nil || composed.EnrollmentManagement == nil || composed.AgentLinkHTTP == nil {
		t.Fatal("composed CLI did not mount the agent transport")
	}
	caPath := filepath.Join(dir, "pki", "local-agent-ca.pem")
	if material.CertificatePath != caPath {
		t.Fatalf("local CA path = %q", material.CertificatePath)
	}
	if info, err := os.Stat(caPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("local CA file: %v", err)
	}
	if len(material.RootSPKI) != 64 {
		t.Fatalf("root pin = %q", material.RootSPKI)
	}

	tlsServer := httptest.NewUnstartedServer(composed)
	tlsServer.Listener.Close()
	tlsServer.Listener = listener
	tlsServer.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}}
	tlsServer.StartTLS()
	defer tlsServer.Close()

	newClient := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), jar)
	}
	doReq := func(client *http.Client, method, path string, body any, csrf string) (int, []byte) {
		t.Helper()
		var reader io.Reader
		idempotencyKey := ""
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
			if method == http.MethodPost {
				sum := sha256.Sum256(append([]byte(path), raw...))
				idempotencyKey = hex.EncodeToString(sum[:16])
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	decode := func(t *testing.T, data []byte, value any) {
		t.Helper()
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}

	// The default CLI composition must mount, not stub, enrollment management.
	anonymous := newClient()
	if status, data := doReq(anonymous, "POST", "/api/v1/enrollment-tokens", map[string]any{}, ""); status == http.StatusServiceUnavailable || bytes.Contains(data, []byte("AGENT_TRANSPORT_UNAVAILABLE")) {
		t.Fatalf("anonymous token route fell back to unavailable: %d %s", status, data)
	}

	admin := newClient()
	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status, data := doReq(admin, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave4-cli-admin-password",
	}, ""); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := doReq(admin, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wave4-cli-admin-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &login)
	csrf := login.CSRF
	if csrf == "" {
		t.Fatalf("missing CSRF: %s", data)
	}

	status, data = doReq(admin, "POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("issue token = %d: %s", status, data)
	}
	var token struct {
		Token string `json:"token"`
	}
	decode(t, data, &token)
	if token.Token == "" {
		t.Fatalf("empty token: %s", data)
	}

	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, agentKey)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	requestBytes := make([]byte, 16)
	if _, err := rand.Read(requestBytes); err != nil {
		t.Fatal(err)
	}
	requestID := hex.EncodeToString(requestBytes)
	enrollBody := map[string]string{
		"enrollmentToken":      token.Token,
		"requestId":            requestID,
		"csrPem":               csrPEM,
		"pinnedRootSpkiSha256": material.RootSPKI,
		"agentVersion":         "0.9.0-wave4-cli-test",
		"platform":             "linux",
	}
	enrollClient := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), nil)
	postEnroll := func() (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(enrollBody)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/agent/enroll", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := enrollClient.Do(req)
		if err != nil {
			t.Fatalf("agent enroll: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}

	status, data = postEnroll()
	if status != http.StatusOK {
		t.Fatalf("agent enroll = %d: %s", status, data)
	}
	var pending struct {
		State string `json:"state"`
	}
	decode(t, data, &pending)
	if pending.State != "pending" {
		t.Fatalf("enroll state = %q: %s", pending.State, data)
	}

	status, data = doReq(admin, "GET", "/api/v1/enrollments", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list enrollments = %d: %s", status, data)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, data, &list)
	if len(list.Items) != 1 {
		t.Fatalf("unexpected enrollment list: %s", data)
	}
	if status, data := doReq(admin, "POST", "/api/v1/enrollments/"+list.Items[0].ID+"/decision", map[string]any{
		"decision": "approve", "reason": "wave4 cli local pki",
	}, csrf); status != http.StatusOK {
		t.Fatalf("approve = %d: %s", status, data)
	}

	status, data = postEnroll()
	if status != http.StatusOK {
		t.Fatalf("issued enroll = %d: %s", status, data)
	}
	var issued struct {
		State           string `json:"state"`
		NodeID          string `json:"nodeId"`
		CertificatePEM  string `json:"certificatePem"`
		TrustGeneration uint64 `json:"trustGeneration"`
	}
	decode(t, data, &issued)
	if issued.State != "issued" || issued.NodeID == "" || issued.CertificatePEM == "" || issued.TrustGeneration != 1 {
		t.Fatalf("issued result: %s", data)
	}

	// The optional mTLS listener configuration trusts exactly the local Agent CA.
	writeCLITLSCertificate(t, pki, options.TLSCert, options.TLSKey)
	config, err := bootstrap.TLSConfig(options, material)
	if err != nil {
		t.Fatalf("tls config: %v", err)
	}
	if config.MinVersion != tls.VersionTLS13 || config.ClientAuth != tls.VerifyClientCertIfGiven || config.ClientCAs == nil {
		t.Fatalf("unexpected management TLS config: %+v", config)
	}

	// Restarting with the same database directory must keep the same trust root.
	composed.Close()
	reopened, reloaded, err := composeController(ctx, options)
	if err != nil {
		t.Fatalf("recompose: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if reloaded.RootSPKI != material.RootSPKI {
		t.Fatal("local Agent trust root changed across restarts")
	}
}
