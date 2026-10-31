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
	statusCalls int
	enrollCalls int
	lastPin     string
	lastRequest EnrollRequest
	status      Status
	statusErr   error
	enroll      EnrollResult
	enrollErr   error
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
