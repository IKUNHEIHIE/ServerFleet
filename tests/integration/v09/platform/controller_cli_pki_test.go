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
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("module root was not found")
	return ""
}

func buildAppBinary(t *testing.T, root, pkg string) string {
	t.Helper()
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain is required to build %s: %v", pkg, err)
	}
	out := filepath.Join(t.TempDir(), strings.TrimPrefix(pkg, "./apps/"))
	command := exec.Command(goBinary, "build", "-buildvcs=false", "-o", out, pkg)
	command.Dir = root
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, data)
	}
	return out
}

func localCAHTTPClient(roots *x509.CertPool, addr string, jar http.CookieJar) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp", addr)
			},
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "localhost"},
		},
		Jar: jar,
	}
}

// writeLocalCAServerCertificate issues the standalone Controller HTTPS certificate
// from the same local Agent CA the Agent CLI pins with -root-cert, so the
// self-contained CLI trust root covers both the management listener and the
// issued Agent identity.
func writeLocalCAServerCertificate(t *testing.T, material *localpki.Material, certPath, keyPath string) {
	t.Helper()
	keyPEM, err := os.ReadFile(filepath.Join(material.Directory, localpki.KeyName))
	if err != nil {
		t.Fatal(err)
	}
	keyBlock, rest := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatal("local CA key is unusable")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	caKey, ok := parsed.(*ecdsa.PrivateKey)
	if err != nil || !ok {
		t.Fatal("local CA key is not ECDSA")
	}
	rootBlock, rest := pem.Decode(material.RootPEM)
	if rootBlock == nil || rootBlock.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatal("local CA certificate is unusable")
	}
	root, err := x509.ParseCertificate(rootBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(12 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, root, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLogTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 4000 {
		data = data[len(data)-4000:]
	}
	return string(data)
}

func runAgentCLI(t *testing.T, ctx context.Context, binary string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("run agent CLI: %v", err)
	return "", "", -1
}

// TestWave5ControllerAndAgentCLIFullLoopWSSAndTask starts the standalone
// Controller binary against a self-contained local PKI and drives the real Agent
// binary from pending enrollment through mTLS WSS admission, one heartbeat and
// metrics sample, one controlled tasks.command offer and a clean disconnect.
func TestWave5ControllerAndAgentCLIFullLoopWSSAndTask(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Controller Agent enrollment and WSS admission are Linux-only by policy; run this test on Linux (WSL Ubuntu) for full-loop evidence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portText := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	origin := "https://localhost:" + portText
	options := bootstrapOptions(dir, portText, origin)
	options.AgentAuthority = "localhost:" + portText
	options.AgentServerName = "localhost"

	// The standalone CLI does not print the installer setup token; seed the same
	// database once through its composition and then run the real binary on it.
	seed, material, err := composeController(ctx, options)
	if err != nil {
		t.Fatalf("seed compose: %v", err)
	}
	setupToken, err := seed.Identity.PrepareSetup(ctx)
	if err != nil {
		_ = seed.Close()
		t.Fatalf("prepare setup: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
	if len(material.RootSPKI) != 64 || material.CertificatePath == "" {
		t.Fatalf("local CA material is incomplete: %q %q", material.RootSPKI, material.CertificatePath)
	}
	writeLocalCAServerCertificate(t, material, options.TLSCert, options.TLSKey)

	root := moduleRoot(t)
	controllerBinary := buildAppBinary(t, root, "./apps/controller")
	agentBinary := buildAppBinary(t, root, "./apps/agent")

	logPath := filepath.Join(dir, "controller.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	controllerCmd := exec.Command(controllerBinary,
		"-listen", options.Listen, "-db", options.DBPath, "-origin", origin,
		"-tls-cert", options.TLSCert, "-tls-key", options.TLSKey,
	)
	controllerCmd.Stdout, controllerCmd.Stderr = logFile, logFile
	if err := controllerCmd.Start(); err != nil {
		t.Fatal(err)
	}
	controllerDone := make(chan error, 1)
	controllerWaited := make(chan struct{})
	go func() {
		controllerDone <- controllerCmd.Wait()
		close(controllerWaited)
	}()
	var stopControllerOnce sync.Once
	stopController := func() {
		stopControllerOnce.Do(func() {
			_ = controllerCmd.Process.Signal(os.Interrupt)
			select {
			case <-controllerWaited:
			case <-time.After(5 * time.Second):
				_ = controllerCmd.Process.Kill()
				<-controllerWaited
			}
			_ = logFile.Close()
		})
	}
	t.Cleanup(stopController)

	addr := net.JoinHostPort("127.0.0.1", portText)
	anonymous := localCAHTTPClient(material.Roots, addr, nil)
	readyDeadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case <-controllerWaited:
			t.Fatalf("controller exited before readiness: %s", readLogTail(logPath))
		default:
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/v1/nodes", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, requestErr := anonymous.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
				break
			}
		}
		if time.Now().After(readyDeadline) {
			t.Fatalf("controller did not become ready: %v\n%s", requestErr, readLogTail(logPath))
		}
		time.Sleep(50 * time.Millisecond)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := localCAHTTPClient(material.Roots, addr, jar)
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
		request, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("X-Requested-With", "ServerFleetWeb")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		if path == "/api/v1/auth/bootstrap" {
			idempotencyKey = "wave5-bootstrap-0001"
		}
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	decode := func(t *testing.T, data []byte, value any) {
		t.Helper()
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}

	status, data := doReq(admin, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave5-cli-admin-password",
	}, "")
	if status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wave5-cli-admin-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
	}
	decode(t, data, &login)
	if login.CSRF == "" || login.Identity.UserID == "" {
		t.Fatalf("missing login material: %s", data)
	}
	csrf := login.CSRF

	status, data = doReq(admin, "POST", "/api/v1/enrollment-tokens", map[string]any{
		"expiryMinutes": 10, "maxUses": 1,
	}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("issue enrollment token = %d: %s", status, data)
	}
	var token struct {
		Token string `json:"token"`
	}
	decode(t, data, &token)
	if token.Token == "" {
		t.Fatalf("empty enrollment token: %s", data)
	}

	stateDir := filepath.Join(dir, "agent-state")
	enrollArgs := []string{"-enroll-token", token.Token, "-state-dir", stateDir, "-controller-url", origin, "-root-cert", material.CertificatePath}
	stdout, stderr, code := runAgentCLI(t, ctx, agentBinary, enrollArgs...)
	var pending struct {
		State string `json:"state"`
	}
	if code != 0 {
		t.Fatalf("agent pending enrollment exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	decode(t, []byte(stdout), &pending)
	if pending.State != "pending" {
		t.Fatalf("pending state = %q: %s", pending.State, stdout)
	}

	status, data = doReq(admin, "GET", "/api/v1/enrollments", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list enrollments = %d: %s", status, data)
	}
	var list struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			NodeID string `json:"nodeId"`
		} `json:"items"`
	}
	decode(t, data, &list)
	if len(list.Items) != 1 || list.Items[0].Status != "pending" || list.Items[0].NodeID == "" {
		t.Fatalf("unexpected enrollment list: %s", data)
	}
	status, data = doReq(admin, "POST", "/api/v1/enrollments/"+list.Items[0].ID+"/decision", map[string]any{
		"decision": "approve", "reason": "wave5 cli two-process loop",
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("approve enrollment = %d: %s", status, data)
	}

	stdout, stderr, code = runAgentCLI(t, ctx, agentBinary, enrollArgs...)
	var issued struct {
		State  string `json:"state"`
		NodeID string `json:"nodeId"`
	}
	if code != 0 {
		t.Fatalf("agent issued enrollment exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	decode(t, []byte(stdout), &issued)
	if issued.State != "issued" || issued.NodeID != list.Items[0].NodeID {
		t.Fatalf("issued enrollment = %s", stdout)
	}

	stdout, stderr, code = runAgentCLI(t, ctx, agentBinary, "-status", "-state-dir", stateDir, "-root-pin", material.RootSPKI)
	var localStatus struct {
		Enrolled bool   `json:"enrolled"`
		NodeID   string `json:"nodeId"`
	}
	if code != 0 {
		t.Fatalf("agent status exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	decode(t, []byte(stdout), &localStatus)
	if !localStatus.Enrolled || localStatus.NodeID != issued.NodeID {
		t.Fatalf("local status = %s", stdout)
	}

	status, data = doReq(admin, "POST", "/api/v1/resource-grants", map[string]any{
		"principalId":  login.Identity.UserID,
		"resourceType": "node",
		"resourceId":   issued.NodeID,
		"permissions":  []string{"nodes.read", "tasks.submit", "tasks.command", "tasks.read"},
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("grant admin = %d: %s", status, data)
	}

	connectOutPath := filepath.Join(dir, "agent-connect.out")
	connectErrPath := filepath.Join(dir, "agent-connect.err")
	connectOut, err := os.Create(connectOutPath)
	if err != nil {
		t.Fatal(err)
	}
	connectErr, err := os.Create(connectErrPath)
	if err != nil {
		t.Fatal(err)
	}
	connectCmd := exec.Command(agentBinary,
		"-connect-once", "-state-dir", stateDir, "-controller-url", origin,
		"-root-cert", material.CertificatePath, "-node-name", "wave5-cli-node", "-wait-task-ms", "60000",
	)
	connectCmd.Stdout, connectCmd.Stderr = connectOut, connectErr
	if err := connectCmd.Start(); err != nil {
		t.Fatal(err)
	}
	connectDone := make(chan error, 1)
	connectWaited := make(chan struct{})
	go func() {
		connectDone <- connectCmd.Wait()
		close(connectWaited)
	}()
	var stopConnectOnce sync.Once
	stopConnect := func() {
		stopConnectOnce.Do(func() {
			_ = connectCmd.Process.Kill()
			select {
			case <-connectWaited:
			case <-time.After(5 * time.Second):
			}
			_ = connectOut.Close()
			_ = connectErr.Close()
		})
	}
	t.Cleanup(stopConnect)

	findNode := func(items []struct {
		ID            string   `json:"id"`
		Online        bool     `json:"online"`
		CPUPercent    *float64 `json:"cpuPercent"`
		MemoryPercent *float64 `json:"memoryPercent"`
	}, nodeID string) (bool, bool) {
		for _, item := range items {
			if item.ID == nodeID {
				return item.Online, item.CPUPercent != nil && item.MemoryPercent != nil
			}
		}
		return false, false
	}
	onlineSampleDeadline := time.Now().Add(30 * time.Second)
	for {
		status, data = doReq(admin, "GET", "/api/v1/nodes", nil, "")
		if status != http.StatusOK {
			t.Fatalf("list nodes = %d: %s", status, data)
		}
		var page struct {
			Items []struct {
				ID            string   `json:"id"`
				Online        bool     `json:"online"`
				CPUPercent    *float64 `json:"cpuPercent"`
				MemoryPercent *float64 `json:"memoryPercent"`
			} `json:"items"`
		}
		decode(t, data, &page)
		online, sampled := findNode(page.Items, issued.NodeID)
		if online && sampled {
			break
		}
		select {
		case <-connectWaited:
			t.Fatalf("agent exited before becoming online: %s | out=%s err=%s", readLogTail(connectErrPath), readLogTail(connectOutPath), readLogTail(logPath))
		default:
		}
		if time.Now().After(onlineSampleDeadline) {
			t.Fatalf("node did not become online with a sample: online=%t sampled=%t out=%s err=%s", online, sampled, readLogTail(connectOutPath), readLogTail(connectErrPath))
		}
		time.Sleep(50 * time.Millisecond)
	}

	status, data = doReq(admin, "POST", "/api/v1/tasks", map[string]any{
		"name": "wave5 cli controlled task", "command": "tasks.command", "dryRun": false,
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{issued.NodeID}},
	}, csrf)
	if status != http.StatusAccepted {
		t.Fatalf("submit task = %d: %s", status, data)
	}
	var submitted struct {
		JobID string `json:"jobId"`
	}
	decode(t, data, &submitted)
	if submitted.JobID == "" {
		t.Fatalf("missing job id: %s", data)
	}

	var connectErrResult error
	select {
	case connectErrResult = <-connectDone:
	case <-time.After(70 * time.Second):
		t.Fatalf("agent -connect-once did not exit: %s", readLogTail(connectErrPath))
	}
	stopConnect()
	if connectErrResult != nil {
		t.Fatalf("agent -connect-once failed: %v out=%s err=%s controller=%s", connectErrResult, readLogTail(connectOutPath), readLogTail(connectErrPath), readLogTail(logPath))
	}
	var connectResult struct {
		NodeID        string `json:"nodeId"`
		ConnectionID  string `json:"connectionId"`
		HeartbeatSent bool   `json:"heartbeatSent"`
		MetricsSent   bool   `json:"metricsSent"`
		TasksExecuted int    `json:"tasksExecuted"`
		NodeName      string `json:"nodeName"`
	}
	decode(t, []byte(readLogTail(connectOutPath)), &connectResult)
	if connectResult.NodeID != issued.NodeID || connectResult.ConnectionID == "" || !connectResult.HeartbeatSent || !connectResult.MetricsSent || connectResult.TasksExecuted != 1 || connectResult.NodeName != "wave5-cli-node" {
		t.Fatalf("connect result = %s", readLogTail(connectOutPath))
	}

	taskDeadline := time.Now().Add(30 * time.Second)
	taskState := ""
	targetState := ""
	for {
		status, data = doReq(admin, "GET", "/api/v1/tasks/"+submitted.JobID, nil, "")
		if status != http.StatusOK {
			t.Fatalf("get task = %d: %s", status, data)
		}
		var detail struct {
			State   string `json:"state"`
			Results []struct {
				NodeID string `json:"nodeId"`
				State  string `json:"state"`
			} `json:"results"`
		}
		decode(t, data, &detail)
		taskState = detail.State
		for _, result := range detail.Results {
			if result.NodeID == issued.NodeID {
				targetState = result.State
			}
		}
		if taskState == "completed" || taskState == "failed" || taskState == "unknown" {
			break
		}
		if time.Now().After(taskDeadline) {
			t.Fatalf("task state = %q target = %q", taskState, targetState)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if taskState != "completed" || targetState != "succeeded" {
		t.Fatalf("task state = %q target = %q", taskState, targetState)
	}

	auditItems := func(query string) []struct {
		Action     string `json:"action"`
		Result     string `json:"result"`
		TaskID     string `json:"taskId"`
		ResourceID string `json:"resourceId"`
	} {
		t.Helper()
		status, data := doReq(admin, "GET", "/api/v1/audit/events?"+query, nil, "")
		if status != http.StatusOK {
			t.Fatalf("audit %s = %d: %s", query, status, data)
		}
		var page struct {
			Items []struct {
				Action     string `json:"action"`
				Result     string `json:"result"`
				TaskID     string `json:"taskId"`
				ResourceID string `json:"resourceId"`
			} `json:"items"`
		}
		decode(t, data, &page)
		return page.Items
	}
	submitEvents := auditItems("action=tasks.submit&taskId=" + submitted.JobID)
	if len(submitEvents) != 1 || submitEvents[0].Result != "success" || submitEvents[0].TaskID != submitted.JobID {
		t.Fatalf("task submit audit = %+v", submitEvents)
	}
	approvalEvents := auditItems("action=enrollment.approved")
	if len(approvalEvents) == 0 || approvalEvents[0].Result != "success" {
		t.Fatalf("enrollment approval audit = %+v", approvalEvents)
	}
	agentEvents := auditItems("action=nodes.agent_update&resourceId=" + issued.NodeID)
	if len(agentEvents) < 2 {
		t.Fatalf("agent state audit = %+v", agentEvents)
	}

	offlineDeadline := time.Now().Add(10 * time.Second)
	for {
		status, data = doReq(admin, "GET", "/api/v1/nodes", nil, "")
		if status != http.StatusOK {
			t.Fatalf("list nodes = %d: %s", status, data)
		}
		var page struct {
			Items []struct {
				ID            string   `json:"id"`
				Online        bool     `json:"online"`
				CPUPercent    *float64 `json:"cpuPercent"`
				MemoryPercent *float64 `json:"memoryPercent"`
			} `json:"items"`
		}
		decode(t, data, &page)
		online, _ := findNode(page.Items, issued.NodeID)
		if !online {
			break
		}
		if time.Now().After(offlineDeadline) {
			t.Fatalf("node remained online after clean close: %s", readLogTail(connectErrPath))
		}
		time.Sleep(100 * time.Millisecond)
	}
	stopController()
}
