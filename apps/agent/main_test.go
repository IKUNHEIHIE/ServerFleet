package main

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
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
)

func agentCLIFixture(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "agent cli test root"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, hex.EncodeToString(sum[:])
}

func TestAgentStatusReportsFreshDirectoryAsNotEnrolled(t *testing.T) {
	dir, pin := agentCLIFixture(t)
	status, err := agentStatus(context.Background(), dir, pin)
	if err != nil {
		t.Fatalf("agentStatus: %v", err)
	}
	if status.StateDir != dir || status.Enrolled || status.NodeID != "" || status.ExpiresAt != nil {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestAgentStatusRejectsUnusableDirectory(t *testing.T) {
	dir, pin := agentCLIFixture(t)
	if _, err := agentStatus(context.Background(), filepath.Join(dir, "missing"), pin); err == nil {
		t.Fatal("missing state directory accepted")
	}
	if _, err := agentStatus(context.Background(), dir, "short"); err == nil {
		t.Fatal("invalid root pin accepted")
	}
}

func TestAgentEnrollOnceFailsClosedOnMissingRootCertificate(t *testing.T) {
	dir, pin := agentCLIFixture(t)
	_, err := agentEnrollOnce(context.Background(), agentruntime.EnrollRequest{
		ControllerURL:   "https://controller.local",
		EnrollToken:     "token",
		StateDir:        dir,
		RootPin:         pin,
		RootCertificate: filepath.Join(dir, "missing.pem"),
		AgentVersion:    agentruntime.DefaultVersion,
	})
	if err == nil {
		t.Fatal("enrollment proceeded without a root certificate")
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 0 {
		t.Fatalf("failed enrollment wrote local state: %v %v", entries, readErr)
	}
}

func TestAgentCLINoArgumentsKeepsScaffold(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := agentruntime.Run(context.Background(), nil, agentRuntimeDeps(), &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if stdout.String() != agentruntime.Scaffold+"\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
