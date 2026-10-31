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
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func agentConnectRoot(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(17), Subject: pkix.Name{CommonName: "agent connect test root"},
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
	path := filepath.Join(t.TempDir(), "agent-root.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, hex.EncodeToString(sum[:])
}

func TestControlledCommandActionIsBounded(t *testing.T) {
	action := controlledCommandActions()[controlledCommandAction]
	if action.Validate == nil || action.Run == nil || action.FilterOutput == nil {
		t.Fatal("controlled action is incomplete")
	}
	if err := action.Validate([]byte(`{}`)); err != nil {
		t.Fatalf("empty canonical parameters rejected: %v", err)
	}
	for _, params := range []string{``, `null`, `[]`, `{"command":"uptime"}`, `{"command":""}`, strings.Repeat(" ", 1025)} {
		if err := action.Validate([]byte(params)); err == nil {
			t.Fatalf("unsafe parameters accepted: %q", params)
		}
	}
	result := action.Run(context.Background(), []byte(`{}`))
	if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED || result.ExitCode != 0 || len(result.Output) == 0 {
		t.Fatalf("unexpected self-check result: %+v", result)
	}
	filtered, err := action.FilterOutput(context.Background(), result.Output)
	if err != nil || !bytes.Contains(filtered, []byte("server-fleet agent self-check ok")) {
		t.Fatalf("filtered output = %q %v", filtered, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result := action.Run(canceled, []byte(`{}`)); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
		t.Fatalf("canceled action result = %v", result.Result)
	}
}

func TestAdmitControlledOfferRejectsForeignShapes(t *testing.T) {
	fixed := func() *agentv1.TaskOffer {
		return &agentv1.TaskOffer{
			ActionId: "tasks.command", Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
			ScopeRevision: 1, ActionSchemaVersion: "1", CanonicalParametersJson: []byte(`{}`),
		}
	}
	if err := admitControlledOffer(fixed()); err != nil {
		t.Fatalf("valid offer rejected: %v", err)
	}
	unscoped := fixed()
	unscoped.ScopeRevision = 0
	if err := admitControlledOffer(unscoped); err != nil {
		t.Fatalf("unscoped web-task offer rejected: %v", err)
	}
	mutations := []func(*agentv1.TaskOffer){
		func(o *agentv1.TaskOffer) { o.ActionId = "host.root" },
		func(o *agentv1.TaskOffer) { o.Privilege = agentv1.Privilege_PRIVILEGE_ROOT },
		func(o *agentv1.TaskOffer) { o.PluginId = "plugin" },
		func(o *agentv1.TaskOffer) { o.ActionSchemaVersion = "2" },
		func(o *agentv1.TaskOffer) { o.CanonicalParametersJson = []byte(`{"command":"uptime"}`) },
	}
	for i, mutate := range mutations {
		offer := fixed()
		mutate(offer)
		if err := admitControlledOffer(offer); err == nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
}

func TestParseAgentLinkEndpointRejectsUnsafeOrigins(t *testing.T) {
	endpoint, err := parseAgentLinkEndpoint("https://controller.local:8443")
	if err != nil || endpoint.serverName != "controller.local" || endpoint.address != "controller.local:8443" || endpoint.authority != endpoint.address {
		t.Fatalf("endpoint = %+v %v", endpoint, err)
	}
	endpoint, err = parseAgentLinkEndpoint("https://controller.local")
	if err != nil || endpoint.address != "controller.local:443" {
		t.Fatalf("default port endpoint = %+v %v", endpoint, err)
	}
	for _, origin := range []string{
		"http://controller.local", "wss://controller.local", "https://127.0.0.1:8443",
		"https://user@controller.local", "https://controller.local/api", "https://Controller.local",
		"https://controller.local?x=1", "https://controller.local#fragment",
	} {
		if _, err := parseAgentLinkEndpoint(origin); err == nil {
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
}

func TestAgentConnectOnceFailsClosedWithoutIssuedIdentity(t *testing.T) {
	rootPath, pin := agentConnectRoot(t)
	base := agentruntime.ConnectRequest{
		ControllerURL: "https://localhost:8443", StateDir: filepath.Join(t.TempDir(), "missing"),
		RootPin: pin, RootCertificate: rootPath, AgentVersion: agentruntime.DefaultVersion,
	}
	if _, err := agentConnectOnce(context.Background(), base); err == nil {
		t.Fatal("connection proceeded without a state directory")
	}
	if _, err := os.Stat(base.StateDir); !os.IsNotExist(err) {
		t.Fatalf("failed connection created local state: %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	base.StateDir = stateDir
	if _, err := agentConnectOnce(context.Background(), base); err == nil {
		t.Fatal("connection proceeded without an issued identity")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "executions")); !os.IsNotExist(err) {
		t.Fatalf("failed connection created an execution ledger: %v", err)
	}

	store, err := openAgentIdentity(stateDir, pin, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prepare(context.Background(), agentruntime.DefaultVersion); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := agentConnectOnce(context.Background(), base); err == nil {
		t.Fatal("connection proceeded with only a pending identity")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "executions")); !os.IsNotExist(err) {
		t.Fatalf("pending identity created an execution ledger: %v", err)
	}

	base.RootPin = strings.Repeat("b", 64)
	if _, err := agentConnectOnce(context.Background(), base); err == nil {
		t.Fatal("connection accepted a mismatched root pin")
	}
}
