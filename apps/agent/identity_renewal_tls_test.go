package main

import (
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
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func TestAgentRenewalActualMutualTLSRetainsOldIdentityOnFailure(t *testing.T) {
	store, ca, key, directory := identityStoreFixture(t)
	ctx := context.Background()
	prepared, e := store.Prepare(ctx, "0.9.1-test")
	if e != nil {
		t.Fatal(e)
	}
	initial := issuedIdentityFixture(t, prepared, ca, key, nil)
	if e = store.Accept(ctx, initial); e != nil {
		t.Fatal(e)
	}
	old, _, _ := store.Status(ctx)
	next := issuedIdentityFixture(t, prepared, ca, key, func(c *x509.Certificate) { c.SerialNumber = big.NewInt(99); c.NotAfter = time.Now().Add(time.Hour) })
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	var calls atomic.Int32
	var firstID atomic.Value
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != "node-a" || r.URL.Path != "/api/v1/agent/identity/renew" {
			t.Error("renewal did not use mutual TLS")
		}
		for _, h := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Origin"} {
			if r.Header.Get(h) != "" {
				t.Error("ambient credentials sent")
			}
		}
		var input transport.IdentityRenewalRequest
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.CSRPEM != prepared.CSRPEM {
			t.Error("changed renewal CSR")
		}
		if calls.Add(1) == 1 {
			firstID.Store(input.RequestID)
			w.WriteHeader(503)
			return
		}
		if firstID.Load() != input.RequestID {
			t.Error("retry changed identity idempotency key")
		}
		leaf, chain := pem.Decode([]byte(next.CertificatePEM))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transport.EnrollmentHTTPResult{State: "issued", NodeID: next.NodeID, CertificatePEM: string(pem.EncodeToMemory(leaf)), IntermediateChainPEM: string(chain), TrustGeneration: next.TrustGeneration, ExpiresAt: next.ExpiresAt})
	})
	serverKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	serverLeaf := &x509.Certificate{SerialNumber: big.NewInt(112), DNSNames: []string{"localhost"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, serverLeaf, ca, &serverKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der, ca.Raw}, PrivateKey: serverKey}}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	rootFile := filepath.Join(directory, "root.pem")
	_ = os.WriteFile(rootFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}), 0600)
	request := agentruntime.ConnectRequest{StateDir: directory, RootPin: store.pin, RootCertificate: rootFile, ControllerURL: "https://localhost:" + port}
	if changed, e := maybeRenewAgentIdentity(ctx, request); e == nil || changed {
		t.Fatal("failed response replaced old identity")
	}
	current, _, _ := store.Status(ctx)
	if current != old {
		t.Fatal("failure changed installed identity")
	}
	if changed, e := maybeRenewAgentIdentity(ctx, request); e != nil || !changed {
		t.Fatal("verified renewal failed", e)
	}
	current, _, _ = store.Status(ctx)
	if current.Fingerprint == old.Fingerprint {
		t.Fatal("renewal did not install new leaf")
	}
}

func TestIdentityRotationDrainsExecutionBeforeReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	clientState, serverState, pin := sessionTLSStates(t)
	fingerprint := sha256.Sum256(serverState.PeerCertificates[0].Raw)
	verifier, _ := transport.NewPeerVerifier(func(context.Context, string) (transport.AgentIdentity, error) {
		return transport.AgentIdentity{NodeID: "node-a", FingerprintSHA256: hex.EncodeToString(fingerprint[:]), Serial: "3", Enabled: true, TrustGeneration: 1}, nil
	})
	events := make(chan *agentv1.Envelope, 32)
	semantic, frames := sessionLimits()
	hub, e := transport.NewControllerLinks(verifier, transport.ControllerLinkConfig{ProtocolMajor: 1, Semantic: semantic, Frames: frames, HeartbeatInterval: time.Second, MaxConnections: 1, Handshake: func(context.Context, transport.LinkObservation, *agentv1.AgentHello) ([]string, error) {
		return []string{"test.write-marker"}, nil
	}, Admit: func(context.Context, transport.LinkObservation, *agentv1.Envelope) error { return nil }, Event: func(_ context.Context, _ transport.LinkObservation, event *agentv1.Envelope) error {
		events <- event
		return nil
	}, Disconnected: func(context.Context, transport.LinkObservation) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	started, release, renewed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var canceled atomic.Bool
	executor, e := newAgentExecutor(privateAgentDirectory(t), testActions(func(ctx context.Context, _ []byte) executionResult {
		close(started)
		select {
		case <-release:
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
		case <-ctx.Done():
			canceled.Store(true)
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
	}))
	if e != nil {
		t.Fatal(e)
	}
	defer executor.Close()
	cfg := sessionConfig(pin)
	var once sync.Once
	cfg.RenewIdentity = func(context.Context) (bool, error) {
		select {
		case <-started:
			once.Do(func() { close(renewed) })
			return true, nil
		default:
			return false, nil
		}
	}
	a, b := sessionFramePair()
	done := make(chan error, 1)
	go func() { _ = hub.Serve(ctx, b, serverState, "connection-a") }()
	go func() { done <- runAgentSession(ctx, a, clientState, "connection-a", cfg, executor) }()
	for {
		select {
		case event := <-events:
			if event.GetAgentHello() != nil {
				goto connected
			}
		case <-ctx.Done():
			t.Fatal("handshake timeout")
		}
	}
connected:
	if e = hub.Send(ctx, "node-a", &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: testOffer()}}); e != nil {
		t.Fatal(e)
	}
	select {
	case <-renewed:
	case <-ctx.Done():
		t.Fatal("renewal did not run")
	}
	if canceled.Load() {
		t.Fatal("renewal canceled an active execution")
	}
	close(release)
	select {
	case e = <-done:
		if !errors.Is(e, errAgentIdentityRotated) || canceled.Load() {
			t.Fatal("rotation did not drain safely", e)
		}
	case <-ctx.Done():
		t.Fatal("rotation failed to reconnect")
	}
}
