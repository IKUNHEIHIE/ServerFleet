package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type gatewayWSSFinished struct {
	execution string
	result    agentv1.ExecutionResult
}

// TestActualWSSGatewayApplyReloadAndHTTPBytes drives the real Agent WSS session
// and the real nginx process in an isolated private prefix. The Controller side
// here is a test host; production admission is covered by the Controller task
// tests and remains a central wiring patch.
func TestActualWSSGatewayApplyReloadAndHTTPBytes(t *testing.T) {
	nginxPath := os.Getenv("NGINX_BIN")
	if nginxPath == "" {
		if resolved, err := exec.LookPath("nginx"); err == nil {
			nginxPath = resolved
		}
	}
	if nginxPath == "" {
		t.Skip("NGINX_BIN not set and nginx is not installed")
	}
	prefix, err := os.MkdirTemp(asciiTestRoot(t), "sf-gateway-wss-")
	if err != nil {
		t.Fatal(err)
	}
	prefix = filepath.Clean(prefix)
	t.Cleanup(func() { _ = os.RemoveAll(prefix) })
	siteDir := filepath.Join(prefix, "conf")
	artifactDir := filepath.Join(prefix, "artifacts")
	for _, dir := range []string{siteDir, artifactDir, filepath.Join(prefix, "logs"), filepath.Join(prefix, "temp")} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mainConfig := filepath.Join(prefix, "nginx.conf")
	listenAddr := freeLoopbackAddress(t)
	main := "worker_processes 1;\n" +
		"pid " + filepath.ToSlash(filepath.Join(prefix, "nginx.pid")) + ";\n" +
		"error_log " + filepath.ToSlash(filepath.Join(prefix, "error.log")) + ";\n" +
		"events { worker_connections 64; }\n" +
		"http { access_log off; include " + filepath.ToSlash(filepath.Join(siteDir, "*.conf")) + "; }\n"
	if err := os.WriteFile(mainConfig, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "placeholder.conf"), []byte("# placeholder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capability, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix: prefix, SiteDir: siteDir, ArtifactDir: artifactDir,
		MainConfig: mainConfig, NginxBinary: nginxPath,
	}, execGatewayRunner{})
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("gateway-wss-ok"))
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	start := exec.Command(nginxPath, "-p", filepath.ToSlash(prefix)+"/", "-c", filepath.ToSlash(mainConfig))
	if err := start.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = start.Wait() }()
	waitForGatewayProcess(t, filepath.Join(prefix, "nginx.pid"))
	t.Cleanup(func() {
		_ = exec.Command(nginxPath, "-s", "stop", "-p", filepath.ToSlash(prefix)+"/", "-c", filepath.ToSlash(mainConfig)).Run()
		_ = start.Process.Kill()
	})

	executor, err := newAgentExecutor(privateAgentDirectory(t), capability.Actions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = executor.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	serverCert, clientCert, roots, pin := sessionPKI(t)
	fingerprint := sha256.Sum256(clientCert.Certificate[0])
	verifier, err := transport.NewPeerVerifier(func(context.Context, string) (transport.AgentIdentity, error) {
		return transport.AgentIdentity{
			NodeID: "node-gateway", FingerprintSHA256: hex.EncodeToString(fingerprint[:]),
			Serial: "3", Enabled: true, TrustGeneration: 1,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	semantic, frames := agentSessionLimits()
	semantic.AllowedActions = map[string]string{gatewayApplyAction: "1", gatewayRemoveAction: "1"}
	finished := make(chan gatewayWSSFinished, 8)
	hello := make(chan struct{}, 1)
	hub, err := transport.NewControllerLinks(verifier, transport.ControllerLinkConfig{
		ProtocolMajor: 1, ProtocolMinor: 1, Semantic: semantic, Frames: frames,
		HeartbeatInterval: time.Second, MaxConnections: 1,
		Handshake: func(context.Context, transport.LinkObservation, *agentv1.AgentHello) ([]string, error) {
			return []string{gatewayApplyAction, gatewayRemoveAction}, nil
		},
		Admit: func(context.Context, transport.LinkObservation, *agentv1.Envelope) error { return nil },
		Event: func(_ context.Context, _ transport.LinkObservation, e *agentv1.Envelope) error {
			if e.GetAgentHello() != nil {
				hello <- struct{}{}
			}
			if f := e.GetTaskFinished(); f != nil {
				finished <- gatewayWSSFinished{execution: f.GetExecutionId(), result: f.GetResult()}
			}
			return nil
		},
		Disconnected: func(context.Context, transport.LinkObservation) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := verifier.ControllerTLSConfig(serverCert, roots)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, id, _, err := transport.AcceptWebSocket(w, r, transport.WebSocketConfig{MaxFrameBytes: 64 << 10})
		if err != nil {
			return
		}
		_ = hub.Serve(ctx, socket, *r.TLS, id)
	}))
	server.TLS = tlsConfig
	server.StartTLS()
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "https://")
	connection, err := tls.Dial("tcp", address, &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientCert},
		RootCAs: roots, ServerName: "fleet.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	socket, id, err := transport.ClientWebSocket(ctx, connection, address, "/link", transport.WebSocketConfig{MaxFrameBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	current := func(context.Context, *agentv1.TaskOffer) error { return nil }
	config := agentSessionConfig{
		NodeID: "node-gateway", BootID: "boot-gateway", Version: "0.9.0-test", ServerName: "fleet.test",
		RootSPKISHA256: pin, ProtocolMajor: 1, ProtocolMinor: 1, MaxConcurrent: 1,
		Semantic: semantic, Frames: frames,
		Current: func(context.Context, tls.ConnectionState) error { return nil },
		Admit:   current, Probe: socket.Ping,
	}
	done := make(chan error, 1)
	go func() { done <- runAgentSession(ctx, socket, connection.ConnectionState(), id, config, executor) }()
	t.Cleanup(func() {
		cancel()
		_ = socket.Close()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("agent WSS session did not stop")
		}
	})
	select {
	case <-hello:
	case <-ctx.Done():
		t.Fatal("WSS handshake did not complete")
	}

	configBytes := []byte("server {\n    listen " + listenAddr + ";\n    location / {\n        proxy_pass http://" + upstreamURL.Host + ";\n        proxy_set_header Host $host;\n    }\n}\n")
	sum := sha256.Sum256(configBytes)
	digest := hex.EncodeToString(sum[:])
	sitePath := filepath.Join(siteDir, "site.conf")
	artifactPath := filepath.Join(artifactDir, "site.conf")
	if err := os.WriteFile(artifactPath, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	applyParams, _ := json.Marshal(map[string]string{
		"operation": "apply", "site_path": sitePath, "artifact_path": artifactPath,
		"config_sha256": digest, "takeover_approved": "false",
	})
	applyOffer := &agentv1.TaskOffer{
		TaskId: "task-gateway", ExecutionId: "exec-gateway-apply",
		PlanDigestSha256: strings.Repeat("a", 64), ActionId: gatewayApplyAction,
		CanonicalParametersJson: applyParams, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(), TimeoutSeconds: 30,
		ActionSchemaVersion: "1",
	}
	if err := hub.Send(ctx, "node-gateway", &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: applyOffer}}); err != nil {
		t.Fatal(err)
	}
	waitGatewayWSSResult(t, ctx, finished, "exec-gateway-apply", agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)
	waitHTTP(t, "http://"+listenAddr+"/", http.StatusOK)

	// The same frozen offer is idempotent and does not rewrite a different site.
	repeat := proto.Clone(applyOffer).(*agentv1.TaskOffer)
	repeat.ExecutionId = "exec-gateway-apply-2"
	if err := hub.Send(ctx, "node-gateway", &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: repeat}}); err != nil {
		t.Fatal(err)
	}
	waitGatewayWSSResult(t, ctx, finished, "exec-gateway-apply-2", agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)
	waitHTTP(t, "http://"+listenAddr+"/", http.StatusOK)

	// A stale digest cannot remove a newer site; the live route must stay up.
	wrongParams, _ := json.Marshal(map[string]string{
		"operation": "remove", "site_path": sitePath, "config_sha256": strings.Repeat("c", 64),
	})
	wrongRemove := &agentv1.TaskOffer{
		TaskId: "task-gateway", ExecutionId: "exec-gateway-remove-stale",
		PlanDigestSha256: strings.Repeat("b", 64), ActionId: gatewayRemoveAction,
		CanonicalParametersJson: wrongParams, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(), TimeoutSeconds: 30,
		ActionSchemaVersion: "1",
	}
	if err := hub.Send(ctx, "node-gateway", &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: wrongRemove}}); err != nil {
		t.Fatal(err)
	}
	waitGatewayWSSResult(t, ctx, finished, "exec-gateway-remove-stale", agentv1.ExecutionResult_EXECUTION_RESULT_FAILED)
	waitHTTP(t, "http://"+listenAddr+"/", http.StatusOK)

	removeParams, _ := json.Marshal(map[string]string{
		"operation": "remove", "site_path": sitePath, "config_sha256": digest,
	})
	removeOffer := &agentv1.TaskOffer{
		TaskId: "task-gateway", ExecutionId: "exec-gateway-remove",
		PlanDigestSha256: strings.Repeat("b", 64), ActionId: gatewayRemoveAction,
		CanonicalParametersJson: removeParams, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(), TimeoutSeconds: 30,
		ActionSchemaVersion: "1",
	}
	if err := hub.Send(ctx, "node-gateway", &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: removeOffer}}); err != nil {
		t.Fatal(err)
	}
	waitGatewayWSSResult(t, ctx, finished, "exec-gateway-remove", agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)
	waitNotOK(t, "http://"+listenAddr+"/")
}

func waitGatewayWSSResult(t *testing.T, ctx context.Context, finished <-chan gatewayWSSFinished, execution string, want agentv1.ExecutionResult) {
	t.Helper()
	for {
		select {
		case result := <-finished:
			if result.execution != execution {
				continue
			}
			if result.result != want {
				t.Fatalf("execution %s result = %v, want %v", execution, result.result, want)
			}
			return
		case <-ctx.Done():
			t.Fatalf("execution %s did not finish", execution)
		}
	}
}
