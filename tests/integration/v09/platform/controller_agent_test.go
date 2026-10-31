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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"serverfleet.local/serverfleet/apps/controller/server"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type testPKI struct {
	rootCert   *x509.Certificate
	rootKey    *ecdsa.PrivateKey
	rootDER    []byte
	roots      *x509.CertPool
	rootSPKI   string
	serverCert tls.Certificate
}

func randomSerial(t *testing.T) *big.Int {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] &= 0x7f
	raw[0] |= 0x40
	return new(big.Int).SetBytes(raw)
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(t),
		Subject:               pkix.Name{CommonName: "ServerFleet Test Root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(rootCert)

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(t),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(12 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, rootCert, &serverKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&rootKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &testPKI{
		rootCert:   rootCert,
		rootKey:    rootKey,
		rootDER:    rootDER,
		roots:      roots,
		rootSPKI:   sha256Hex(spki),
		serverCert: tls.Certificate{Certificate: [][]byte{serverDER, rootDER}, PrivateKey: serverKey},
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func base64RawURL(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func managerIssuer(t *testing.T, pki *testPKI) *transport.CertificateIssuer {
	t.Helper()
	issuer, err := transport.NewCertificateIssuer(pki.rootDER, pki.rootKey, pki.roots, nil, pki.rootSPKI, 1, time.Hour)
	if err != nil {
		t.Fatalf("NewCertificateIssuer: %v", err)
	}
	return issuer
}

func newTestHTTPClient(pki *testPKI, addr string, jar http.CookieJar) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp", addr)
			},
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.roots, ServerName: "localhost"},
		},
		Jar: jar,
	}
}

func TestWave3ControllerAgentEnrollmentWSSAndTaskExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	tlsServer := httptest.NewUnstartedServer(nil)
	port := tlsServer.Listener.Addr().(*net.TCPAddr).Port
	portText := strconv.Itoa(port)
	authority := "localhost:" + portText
	origin := "https://" + authority

	composed, err := server.New(ctx, server.Config{
		DBPath:           filepath.Join(dir, "controller-agent.db"),
		Origin:           origin,
		StagingDir:       filepath.Join(dir, "staging"),
		AgentAuthority:   authority,
		AgentServerName:  "localhost",
		EnrollmentIssuer: managerIssuer(t, pki),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = composed.Close() }()

	tlsServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pki.serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pki.roots,
	}
	tlsServer.Config.Handler = composed
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
		if path == "/api/v1/auth/bootstrap" {
			idempotencyKey = "wave3-bootstrap-0001"
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
	doAgentReq := func(client *http.Client, path string, body any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", origin+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("agent POST %s: %v", path, err)
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

	admin := newClient()
	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatalf("PrepareSetup: %v", err)
	}
	if status, data := doReq(admin, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave3-admin-strong-password",
	}, ""); status != http.StatusNoContent {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := doReq(admin, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wave3-admin-strong-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &login)
	csrf := login.CSRF
	if csrf == "" || login.Identity.UserID == "" {
		t.Fatalf("missing login material: %s", data)
	}

	// Unauthenticated management endpoints stay closed.
	if status, _ := doReq(newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), nil), "GET", "/api/v1/node-enrollments", nil, ""); status == http.StatusOK {
		t.Fatal("anonymous enrollment list unexpectedly succeeded")
	}

	// 1. One-time token, Agent enrollment request, human approval.
	status, data = doReq(admin, "POST", "/api/v1/node-enrollment-tokens", map[string]any{
		"expiryMinutes": 10, "maxUses": 1,
	}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("create token = %d: %s", status, data)
	}
	var token struct {
		ID        string    `json:"id"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	decode(t, data, &token)
	if token.Token == "" || token.ID == "" {
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
		"pinnedRootSpkiSha256": pki.rootSPKI,
		"agentVersion":         "0.9.0-wave3-test",
		"platform":             "linux",
	}
	enrollClient := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), nil)
	status, data = doAgentReq(enrollClient, "/api/v1/agent/enroll", enrollBody)
	if status != http.StatusOK {
		t.Fatalf("agent enroll = %d: %s", status, data)
	}
	var enroll struct {
		State string `json:"state"`
	}
	decode(t, data, &enroll)
	if enroll.State != "pending" {
		t.Fatalf("enroll state = %q: %s", enroll.State, data)
	}

	status, data = doReq(admin, "GET", "/api/v1/node-enrollments", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list enrollments = %d: %s", status, data)
	}
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			NodeID   string `json:"nodeId"`
			Revision uint64 `json:"revision"`
		} `json:"items"`
	}
	decode(t, data, &list)
	if len(list.Items) != 1 || list.Items[0].Status != "pending" || list.Items[0].NodeID == "" {
		t.Fatalf("unexpected enrollment list: %s", data)
	}
	enrollmentID := list.Items[0].ID
	nodeID := list.Items[0].NodeID

	status, data = doReq(admin, "POST", "/api/v1/node-enrollments/"+enrollmentID+"/decision", map[string]any{
		"decision": "approve", "reason": "local integration test",
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("approve enrollment = %d: %s", status, data)
	}
	var approved struct {
		Status string `json:"status"`
	}
	decode(t, data, &approved)
	if approved.Status != "approved" {
		t.Fatalf("approval status = %q: %s", approved.Status, data)
	}

	// 2. Same Agent request now receives the issued certificate over mTLS.
	status, data = doAgentReq(enrollClient, "/api/v1/agent/enroll", enrollBody)
	if status != http.StatusOK {
		t.Fatalf("agent enroll poll = %d: %s", status, data)
	}
	var issued struct {
		State                string `json:"state"`
		NodeID               string `json:"nodeId"`
		CertificatePEM       string `json:"certificatePem"`
		IntermediateChainPEM string `json:"intermediateChainPem"`
		TrustGeneration      uint64 `json:"trustGeneration"`
	}
	decode(t, data, &issued)
	if issued.State != "issued" || issued.NodeID != nodeID || issued.TrustGeneration != 1 || issued.CertificatePEM == "" {
		t.Fatalf("issued result: %s", data)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(agentKey)
	if err != nil {
		t.Fatal(err)
	}
	agentKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	agentCert, err := tls.X509KeyPair([]byte(issued.CertificatePEM+issued.IntermediateChainPEM), agentKeyPEM)
	if err != nil {
		t.Fatalf("agent key pair: %v", err)
	}

	// 3. Grant the bootstrap admin task scope on the enrolled node and create the
	// second approver required by two-person task approval.
	status, data = doReq(admin, "POST", "/api/v1/resource-grants", map[string]any{
		"principalId":  login.Identity.UserID,
		"resourceType": "node",
		"resourceId":   nodeID,
		"permissions":  []string{"nodes.read", "tasks.submit", "tasks.read"},
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("grant admin = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/users", map[string]any{
		"username": "approver", "displayName": "Approver", "password": "wave3-approver-strong-password", "roleIds": []string{"operator"},
	}, csrf)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create approver = %d: %s", status, data)
	}
	var approverUser struct {
		UserID string `json:"userId"`
	}
	decode(t, data, &approverUser)
	status, data = doReq(admin, "POST", "/api/v1/resource-grants", map[string]any{
		"principalId":  approverUser.UserID,
		"resourceType": "node",
		"resourceId":   nodeID,
		"permissions":  []string{"tasks.approve", "tasks.read"},
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("grant approver = %d: %s", status, data)
	}
	approver := newClient()
	status, data = doReq(approver, "POST", "/api/v1/auth/login", map[string]string{
		"username": "approver", "password": "wave3-approver-strong-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("approver login = %d: %s", status, data)
	}
	var approverLogin struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &approverLogin)

	// 4. Agent-initiated mTLS WSS connection: hello, heartbeat, task execution.
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tlsConn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort("127.0.0.1", portText), &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      pki.roots,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{agentCert},
	})
	if err != nil {
		t.Fatalf("agent TLS dial: %v", err)
	}
	link, connectionID, err := transport.ClientWebSocket(ctx, tlsConn, authority, "/api/v1/agent/link", transport.WebSocketConfig{MaxFrameBytes: 64 << 10})
	if err != nil {
		t.Fatalf("agent WSS upgrade: %v", err)
	}
	defer link.Close()

	sequence := uint64(0)
	writeEnvelope := func(message *agentv1.Envelope) error {
		sequence++
		messageID, err := transport.NewConnectionID()
		if err != nil {
			return err
		}
		message.ConnectionId = connectionID
		message.NodeId = nodeID
		message.MessageId = messageID
		message.ProtocolMajor = 1
		message.ProtocolMinor = 0
		message.Sequence = sequence
		message.SentAtUnixMs = time.Now().UnixMilli()
		raw, err := proto.Marshal(message)
		if err != nil {
			return err
		}
		return link.Write(ctx, raw)
	}
	readEnvelope := func(timeout time.Duration) (*agentv1.Envelope, error) {
		readCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		binary, raw, err := link.Read(readCtx)
		if err != nil {
			return nil, err
		}
		if !binary {
			return nil, transport.ErrWebSocketRejected
		}
		var envelope agentv1.Envelope
		if err := proto.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		return &envelope, nil
	}

	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_AgentHello{AgentHello: &agentv1.AgentHello{
		AgentVersion: "0.9.0-wave3-test", Platform: "linux", Architecture: "amd64",
		CapabilityIds:    []string{"tasks.command", "host.patch", "host.root"},
		MinProtocolMajor: 1, MaxProtocolMajor: 1, BootId: "wave3-boot-1",
	}}}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	hello, err := readEnvelope(5 * time.Second)
	if err != nil {
		t.Fatalf("read controller hello: %v", err)
	}
	if hello.GetControllerHello() == nil || !hello.GetControllerHello().Accepted {
		t.Fatalf("controller hello rejected: %v", hello)
	}
	granted := map[string]bool{}
	for _, id := range hello.GetControllerHello().GrantedCapabilityIds {
		granted[id] = true
	}
	if !granted["host.patch"] {
		t.Fatalf("host.patch not granted: %v", hello.GetControllerHello().GrantedCapabilityIds)
	}
	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_Heartbeat{Heartbeat: &agentv1.Heartbeat{
		UptimeSeconds: 1, RunningExecutions: 0, ConnectivityStatus: "connected",
	}}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	status, data = doReq(admin, "POST", "/api/v1/tasks", map[string]any{
		"name": "wave3 agent execution", "command": "host.patch", "dryRun": false,
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{nodeID}},
	}, csrf)
	if status != http.StatusAccepted {
		t.Fatalf("submit task = %d: %s", status, data)
	}
	var submitted struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	decode(t, data, &submitted)
	if submitted.JobID == "" || submitted.State != "awaiting_approval" {
		t.Fatalf("unexpected submit result: %s", data)
	}
	status, data = doReq(admin, "GET", "/api/v1/tasks", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list tasks = %d: %s", status, data)
	}
	var tasksPage struct {
		Items []struct {
			TaskID     string `json:"taskId"`
			PlanDigest string `json:"planDigest"`
		} `json:"items"`
	}
	decode(t, data, &tasksPage)
	if len(tasksPage.Items) != 1 || tasksPage.Items[0].PlanDigest == "" {
		t.Fatalf("unexpected tasks page: %s", data)
	}
	status, data = doReq(approver, "POST", "/api/v1/tasks/"+submitted.JobID+"/approve", map[string]any{
		"planDigest": tasksPage.Items[0].PlanDigest, "decision": "approve", "reason": "peer reviewed",
	}, approverLogin.CSRF)
	if status != http.StatusOK {
		t.Fatalf("approve task = %d: %s", status, data)
	}

	offerEnvelope, err := readEnvelope(15 * time.Second)
	if err != nil {
		t.Fatalf("read task offer: %v", err)
	}
	offer := offerEnvelope.GetTaskOffer()
	if offer == nil || offer.GetTaskId() != submitted.JobID || offer.GetActionId() != "host.patch" {
		t.Fatalf("unexpected offer: %v", offerEnvelope)
	}
	if offer.GetPlanDigestSha256() != tasksPage.Items[0].PlanDigest {
		t.Fatalf("offer plan digest mismatch: %s", offer.GetPlanDigestSha256())
	}
	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_TaskAck{TaskAck: &agentv1.TaskAck{
		TaskId: offer.GetTaskId(), ExecutionId: offer.GetExecutionId(), Accepted: true,
	}}}); err != nil {
		t.Fatalf("send ack: %v", err)
	}
	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_TaskStarted{TaskStarted: &agentv1.TaskStarted{
		TaskId: offer.GetTaskId(), ExecutionId: offer.GetExecutionId(), StartedAtUnixMs: time.Now().UnixMilli(),
	}}}); err != nil {
		t.Fatalf("send started: %v", err)
	}
	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_TaskOutput{TaskOutput: &agentv1.TaskOutput{
		TaskId: offer.GetTaskId(), ExecutionId: offer.GetExecutionId(), StreamSequence: 1,
		Stream: agentv1.TaskOutput_STREAM_STDOUT, Chunk: []byte("wave3 agent executed"),
	}}}); err != nil {
		t.Fatalf("send output: %v", err)
	}
	if err := writeEnvelope(&agentv1.Envelope{Message: &agentv1.Envelope_TaskFinished{TaskFinished: &agentv1.TaskFinished{
		TaskId: offer.GetTaskId(), ExecutionId: offer.GetExecutionId(),
		Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, ExitCode: 0, FinishedAtUnixMs: time.Now().UnixMilli(),
	}}}); err != nil {
		t.Fatalf("send finished: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	finalState := ""
	for time.Now().Before(deadline) {
		status, data = doReq(admin, "GET", "/api/v1/tasks/"+submitted.JobID, nil, "")
		if status != http.StatusOK {
			t.Fatalf("get task = %d: %s", status, data)
		}
		var taskDetail struct {
			State string `json:"state"`
		}
		decode(t, data, &taskDetail)
		finalState = taskDetail.State
		if finalState == "completed" || finalState == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if finalState != "completed" {
		t.Fatalf("task final state = %q, want completed", finalState)
	}

	// 5. Without a verified client certificate the WSS endpoint stays closed.
	resp, err := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), nil).Get(origin + "/api/v1/agent/link")
	if err != nil {
		t.Fatalf("anonymous link probe: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous agent link = %d, want 403", resp.StatusCode)
	}

	// 6. Wrong enrollment tokens are rejected and audited, never approved.
	badToken := make([]byte, 32)
	if _, err := rand.Read(badToken); err != nil {
		t.Fatal(err)
	}
	badBody := map[string]string{
		"enrollmentToken":      base64RawURL(badToken),
		"requestId":            "00000000000000000000000000000001",
		"csrPem":               csrPEM,
		"pinnedRootSpkiSha256": pki.rootSPKI,
		"agentVersion":         "0.9.0-wave3-test",
		"platform":             "linux",
	}
	status, data = doAgentReq(enrollClient, "/api/v1/agent/enroll", badBody)
	if status != http.StatusForbidden {
		t.Fatalf("bad token enroll = %d: %s", status, data)
	}
	if !strings.Contains(string(data), "ENROLLMENT_REJECTED") {
		t.Fatalf("unexpected bad token response: %s", data)
	}
}
