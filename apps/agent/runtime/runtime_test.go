package runtime

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDeps struct {
	statusCalls  int
	enrollCalls  int
	connectCalls int
	lastPin      string
	lastRequest  EnrollRequest
	lastConnect  ConnectRequest
	status       Status
	statusErr    error
	enroll       EnrollResult
	enrollErr    error
	connect      ConnectResult
	connectErr   error
}

func (d *fakeDeps) deps() Deps {
	return Deps{
		Status: func(_ context.Context, stateDir, pin string) (Status, error) {
			d.statusCalls++
			d.lastPin = pin
			status := d.status
			status.StateDir = stateDir
			return status, d.statusErr
		},
		Enroll: func(_ context.Context, request EnrollRequest) (EnrollResult, error) {
			d.enrollCalls++
			d.lastRequest = request
			return d.enroll, d.enrollErr
		},
		Connect: func(_ context.Context, request ConnectRequest) (ConnectResult, error) {
			d.connectCalls++
			d.lastConnect = request
			result := d.connect
			result.StateDir = request.StateDir
			return result, d.connectErr
		},
	}
}

func testPin() string { return strings.Repeat("ab", 32) }

func runtimeCA(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "runtime test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	pin := hex.EncodeToString(sum[:])
	path := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, pin
}

func TestRunNoArgumentsKeepsScaffold(t *testing.T) {
	deps := &fakeDeps{}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), nil, deps.deps(), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if strings.TrimSpace(stdout.String()) != Scaffold || stderr.Len() != 0 || deps.statusCalls+deps.enrollCalls != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunStatusValidatesAndPrintsFacts(t *testing.T) {
	dir := t.TempDir()
	deps := &fakeDeps{status: Status{Enrolled: true, NodeID: "node-1", TrustGeneration: 1}}
	var stdout, stderr bytes.Buffer
	args := []string{"-status", "-state-dir", dir, "-root-pin", testPin()}
	if code := Run(context.Background(), args, deps.deps(), &stdout, &stderr); code != 0 {
		t.Fatalf("status code = %d: %s", code, stderr.String())
	}
	var status Status
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Enrolled || status.NodeID != "node-1" || status.StateDir != dir || deps.lastPin != testPin() {
		t.Fatalf("unexpected status output: %+v", status)
	}

	deps.statusErr = errors.New("unavailable")
	stdout.Reset()
	if code := Run(context.Background(), args, deps.deps(), &stdout, &stderr); code != 1 {
		t.Fatalf("failing status code = %d", code)
	}
	if strings.Contains(stderr.String(), "unavailable") && !strings.Contains(stderr.String(), "no automatic replacement") {
		t.Fatalf("status error leaked details: %s", stderr.String())
	}

	if code := Run(context.Background(), []string{"-status", "-state-dir", "relative", "-root-pin", testPin()}, deps.deps(), &stdout, &stderr); code != 2 {
		t.Fatalf("relative state dir code = %d", code)
	}
	if code := Run(context.Background(), []string{"-status", "-state-dir", dir, "-root-pin", "short"}, deps.deps(), &stdout, &stderr); code != 2 {
		t.Fatalf("invalid pin code = %d", code)
	}
}

func TestRunEnrollRequiresExplicitInputs(t *testing.T) {
	dir := t.TempDir()
	deps := &fakeDeps{}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"-enroll-token", "token", "-state-dir", dir, "-root-pin", testPin()}, deps.deps(), &stdout, &stderr); code != 2 {
		t.Fatalf("missing controller URL code = %d", code)
	}
	if deps.enrollCalls != 0 {
		t.Fatal("enrollment was attempted with incomplete input")
	}
	if code := Run(context.Background(), []string{"-status", "-enroll-token", "token", "-state-dir", dir, "-root-pin", testPin()}, deps.deps(), &stdout, &stderr); code != 2 {
		t.Fatalf("mutually exclusive flags code = %d", code)
	}
	if code := Run(context.Background(), []string{"-unknown"}, deps.deps(), &stdout, &stderr); code != 2 {
		t.Fatalf("unknown flag code = %d", code)
	}
}

func TestRunConnectOnceValidatesInputs(t *testing.T) {
	dir := t.TempDir()
	rootCert, _ := runtimeCA(t)
	deps := &fakeDeps{}
	var stdout, stderr bytes.Buffer
	cases := []struct {
		name string
		args []string
	}{
		{"missing root certificate", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-pin", testPin()}},
		{"missing controller url", []string{"-connect-once", "-state-dir", dir, "-root-cert", rootCert}},
		{"relative state dir", []string{"-connect-once", "-state-dir", "relative", "-controller-url", "https://controller.local", "-root-cert", rootCert}},
		{"ip literal", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://127.0.0.1:8443", "-root-cert", rootCert}},
		{"path in url", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local/api", "-root-cert", rootCert}},
		{"negative wait", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert, "-wait-task-ms", "-1"}},
		{"excessive wait", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert, "-wait-task-ms", "600001"}},
		{"control chars in name", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert, "-node-name", "bad\nname"}},
		{"status and connect", []string{"-status", "-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert}},
		{"enroll and connect", []string{"-enroll-token", "token", "-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert}},
	}
	for _, tc := range cases {
		if code := Run(context.Background(), tc.args, deps.deps(), &stdout, &stderr); code != 2 {
			t.Fatalf("%s: code = %d, stderr = %s", tc.name, code, stderr.String())
		}
	}
	if deps.connectCalls != 0 {
		t.Fatal("connection was attempted with invalid input")
	}
}

func TestRunConnectOnceNormalizesOriginAndPrintsResult(t *testing.T) {
	dir := t.TempDir()
	rootCert, pin := runtimeCA(t)
	deps := &fakeDeps{connect: ConnectResult{NodeID: "node-1", ConnectionID: "connection-1", HeartbeatSent: true, MetricsSent: true, TasksExecuted: 1}}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"-connect-once", "-state-dir", dir, "-controller-url", "wss://controller.local:8443",
		"-root-cert", rootCert, "-node-name", "local node", "-wait-task-ms", "1500",
	}, deps.deps(), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, stderr.String())
	}
	if deps.connectCalls != 1 || deps.lastConnect.ControllerURL != "https://controller.local:8443" ||
		deps.lastConnect.RootPin != pin || deps.lastConnect.NodeName != "local node" ||
		deps.lastConnect.WaitTaskMillis != 1500 || deps.lastConnect.StateDir != dir ||
		deps.lastConnect.AgentVersion != DefaultVersion {
		t.Fatalf("unexpected connect request: %+v", deps.lastConnect)
	}
	var result ConnectResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.NodeID != "node-1" || result.TasksExecuted != 1 || result.StateDir != dir {
		t.Fatalf("connect result = %v %+v", err, result)
	}

	stdout.Reset()
	deps.connectErr = errors.New("unreachable details")
	if code := Run(context.Background(), []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert}, deps.deps(), &stdout, &stderr); code != 1 {
		t.Fatalf("failing connect code = %d", code)
	}
	if strings.Contains(stderr.String(), "unreachable details") || !strings.Contains(stderr.String(), "no automatic retry") {
		t.Fatalf("connect error leaked or lacked guidance: %s", stderr.String())
	}
}

func TestRunEnrollPendingIssuedAndDenied(t *testing.T) {
	dir := t.TempDir()
	rootCert, pin := runtimeCA(t)
	deps := &fakeDeps{enroll: EnrollResult{State: "pending", ExpiresAt: time.Now().Add(time.Hour)}}
	var stdout, stderr bytes.Buffer
	args := []string{"-enroll-token", "token", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert}
	if code := Run(context.Background(), args, deps.deps(), &stdout, &stderr); code != 0 {
		t.Fatalf("pending code = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "pending administrator approval") {
		t.Fatalf("pending note missing: %s", stderr.String())
	}
	if deps.lastRequest.RootPin != pin || deps.lastRequest.RootCertificate != rootCert || deps.lastRequest.AgentVersion != DefaultVersion {
		t.Fatalf("unexpected enrollment request: %+v", deps.lastRequest)
	}
	var result EnrollResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.State != "pending" {
		t.Fatalf("pending result = %v %+v", err, result)
	}

	stdout.Reset()
	deps.enroll = EnrollResult{State: "issued", NodeID: "node-1", ExpiresAt: time.Now().Add(time.Hour)}
	if code := Run(context.Background(), args, deps.deps(), &stdout, &stderr); code != 0 {
		t.Fatalf("issued code = %d", code)
	}

	stdout.Reset()
	deps.enrollErr = errors.New("denied")
	if code := Run(context.Background(), args, deps.deps(), &stdout, &stderr); code != 1 {
		t.Fatalf("denied code = %d", code)
	}
	if strings.Contains(stderr.String(), "denied; no automatic") == false && !strings.Contains(stderr.String(), "no automatic retry") {
		t.Fatalf("denied note missing: %s", stderr.String())
	}
}

func TestRunGatewayFlagsStayClosedWithoutExplicitTrustedPaths(t *testing.T) {
	dir := t.TempDir()
	rootCert, _ := runtimeCA(t)
	deps := &fakeDeps{}
	var stdout, stderr bytes.Buffer
	cases := []struct {
		name string
		args []string
	}{
		{"relative gateway prefix", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert, "-gateway-prefix", "gateway"}},
		{"binary without prefix", []string{"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local", "-root-cert", rootCert, "-nginx-binary", "/usr/bin/nginx"}},
		{"status with gateway prefix", []string{"-status", "-state-dir", dir, "-root-cert", rootCert, "-gateway-prefix", "/srv/fleet/nginx"}},
	}
	for _, tc := range cases {
		if code := Run(context.Background(), tc.args, deps.deps(), &stdout, &stderr); code != 2 {
			t.Fatalf("%s: code = %d, stderr = %s", tc.name, code, stderr.String())
		}
	}
	if deps.connectCalls != 0 {
		t.Fatal("connection was attempted with an unsafe gateway configuration")
	}
}

func TestRunConnectOnceCarriesTrustedGatewayConfiguration(t *testing.T) {
	dir := t.TempDir()
	rootCert, _ := runtimeCA(t)
	deps := &fakeDeps{connect: ConnectResult{NodeID: "node-1", ConnectionID: "connection-1", HeartbeatSent: true, MetricsSent: true}}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"-connect-once", "-state-dir", dir, "-controller-url", "https://controller.local",
		"-root-cert", rootCert, "-gateway-prefix", "/srv/fleet/nginx", "-nginx-binary", "/usr/bin/nginx",
	}, deps.deps(), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, stderr.String())
	}
	if deps.lastConnect.GatewayPrefix != "/srv/fleet/nginx" || deps.lastConnect.NginxBinary != "/usr/bin/nginx" {
		t.Fatalf("gateway configuration not carried: %+v", deps.lastConnect)
	}
}
