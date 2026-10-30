package main

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
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func sessionPKI(t *testing.T) (tls.Certificate, tls.Certificate, *x509.CertPool, string) {
	t.Helper()
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	leaf := func(serial int64, usage x509.ExtKeyUsage) tls.Certificate {
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, DNSNames: []string{"fleet.test"}}
		der, err := x509.CreateCertificate(rand.Reader, template, root, &private.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der, root.Raw}, PrivateKey: private}
	}
	pin := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	return leaf(2, x509.ExtKeyUsageServerAuth), leaf(3, x509.ExtKeyUsageClientAuth), pool, hex.EncodeToString(pin[:])
}

func sessionTLSStates(t *testing.T) (tls.ConnectionState, tls.ConnectionState, string) {
	t.Helper()
	server, client, roots, pin := sessionPKI(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{server}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	states := make(chan tls.ConnectionState, 1)
	fail := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			fail <- err
			return
		}
		defer conn.Close()
		tcp := conn.(*tls.Conn)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err = tcp.HandshakeContext(ctx); err != nil {
			fail <- err
			return
		}
		states <- tcp.ConnectionState()
	}()
	config, err := transport.AgentTLSConfig(client, roots, "fleet.test")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := tls.Dial("tcp", listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case serverState := <-states:
		return conn.ConnectionState(), serverState, pin
	case err := <-fail:
		t.Fatal(err)
	case <-time.After(4 * time.Second):
		t.Fatal("local mTLS did not finish")
	}
	return tls.ConnectionState{}, tls.ConnectionState{}, ""
}

// Explicit in-memory FrameSocket doubles. Actual WSS uses the isolated test.
type sessionFrames struct {
	read, write chan []byte
	done        chan struct{}
	once        *sync.Once
}

func sessionFramePair() (sessionFrames, sessionFrames) {
	a, b := make(chan []byte, 32), make(chan []byte, 32)
	done := make(chan struct{})
	once := new(sync.Once)
	return sessionFrames{a, b, done, once}, sessionFrames{b, a, done, once}
}
func (s sessionFrames) Read(ctx context.Context) (bool, []byte, error) {
	select {
	case raw := <-s.read:
		return true, raw, nil
	case <-s.done:
		return false, nil, io.EOF
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}
func (s sessionFrames) Write(ctx context.Context, raw []byte) error {
	select {
	case s.write <- bytes.Clone(raw):
		return nil
	case <-s.done:
		return io.ErrClosedPipe
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s sessionFrames) Close() error { s.once.Do(func() { close(s.done) }); return nil }
func sessionLimits() (transport.ValidationLimits, transport.FrameDecodeLimits) {
	return transport.ValidationLimits{MaxMessageBytes: 16384, MaxTextBytes: 256, MaxParametersBytes: 1024, MaxOutputBytes: 1024, MaxMetrics: 8, MaxSyncExecutions: 8, MaxCapabilities: 8, MaxTimeoutSeconds: 60, MaxOfferLifetime: 15 * time.Minute, AllowedActions: map[string]string{"test.write-marker": "1"}}, transport.FrameDecodeLimits{MaxFrameBytes: 16384, MaxFields: 256, MaxDepth: 8}
}
func sessionConfig(pin string) agentSessionConfig {
	semantic, frames := sessionLimits()
	return agentSessionConfig{NodeID: "node-a", BootID: "boot-a", Version: "test-v1", ServerName: "fleet.test", RootSPKISHA256: pin, ProtocolMajor: 1, MaxConcurrent: 1, Semantic: semantic, Frames: frames, Current: func(context.Context, tls.ConnectionState) error { return nil }, Admit: func(context.Context, *agentv1.TaskOffer) error { return nil }, Probe: func(context.Context) error { return nil }}
}

func TestAgentSessionProductionLoopDedupeCancelSyncAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientState, serverState, pin := sessionTLSStates(t)
	fp := sha256.Sum256(serverState.PeerCertificates[0].Raw)
	verifier, _ := transport.NewPeerVerifier(func(context.Context, string) (transport.AgentIdentity, error) {
		return transport.AgentIdentity{NodeID: "node-a", FingerprintSHA256: hex.EncodeToString(fp[:]), Serial: "3", Enabled: true, TrustGeneration: 1}, nil
	})
	events := make(chan *agentv1.Envelope, 64)
	semantic, frames := sessionLimits()
	hub, err := transport.NewControllerLinks(verifier, transport.ControllerLinkConfig{ProtocolMajor: 1, Semantic: semantic, Frames: frames, HeartbeatInterval: time.Second, MaxConnections: 1, Handshake: func(context.Context, transport.LinkObservation, *agentv1.AgentHello) ([]string, error) {
		return []string{"test.write-marker"}, nil
	}, Admit: func(context.Context, transport.LinkObservation, *agentv1.Envelope) error { return nil }, Event: func(_ context.Context, _ transport.LinkObservation, e *agentv1.Envelope) error {
		events <- e
		return nil
	}, Disconnected: func(context.Context, transport.LinkObservation) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "effect")
	var calls atomic.Int32
	e, err := newAgentExecutor(dir, testActions(func(ctx context.Context, _ []byte) executionResult {
		n := calls.Add(1)
		if n == 2 {
			<-ctx.Done()
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
		}
		if err := os.WriteFile(marker, []byte("one real local effect"), 0600); err != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: []byte("bounded private output")}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	localCurrent := atomic.Bool{}
	localCurrent.Store(true)
	cfg := sessionConfig(pin)
	cfg.Current = func(context.Context, tls.ConnectionState) error {
		if !localCurrent.Load() {
			return errAgentSession
		}
		return nil
	}
	a, b := sessionFramePair()
	agentDone, controllerDone := make(chan error, 1), make(chan error, 1)
	go func() { controllerDone <- hub.Serve(ctx, b, serverState, "connection-a") }()
	go func() { agentDone <- runAgentSession(ctx, a, clientState, "connection-a", cfg, e) }()
	wait := func(match func(*agentv1.Envelope) bool) *agentv1.Envelope {
		for {
			select {
			case event := <-events:
				if match(event) {
					return event
				}
			case <-ctx.Done():
				t.Fatal("missing runtime event")
				return nil
			}
		}
	}
	wait(func(e *agentv1.Envelope) bool { return e.GetAgentHello() != nil })
	offer := testOffer()
	send := func(e *agentv1.Envelope) {
		if err := hub.Send(ctx, "node-a", e); err != nil {
			t.Fatal(err)
		}
	}
	send(&agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: offer}})
	ack := wait(func(e *agentv1.Envelope) bool { return e.GetTaskAck() != nil }).GetTaskAck()
	if !ack.Accepted || ack.PreviouslySeen {
		t.Fatal(ack)
	}
	wait(func(e *agentv1.Envelope) bool { return e.GetTaskStarted() != nil })
	finished := wait(func(e *agentv1.Envelope) bool { return e.GetTaskFinished() != nil }).GetTaskFinished()
	if finished.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED || !finished.OutputTruncated || calls.Load() != 1 {
		t.Fatal(finished, calls.Load())
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "one real local effect" {
		t.Fatal(err)
	}
	send(&agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: offer}})
	if ack = wait(func(e *agentv1.Envelope) bool { return e.GetTaskAck() != nil }).GetTaskAck(); !ack.Accepted || !ack.PreviouslySeen {
		t.Fatal(ack)
	}
	wait(func(e *agentv1.Envelope) bool { return e.GetTaskFinished() != nil })
	if calls.Load() != 1 {
		t.Fatal("duplicate side effect")
	}
	offer = testOffer()
	offer.ExecutionId = "execution-two"
	offer.TaskId = "task-two"
	send(&agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: offer}})
	wait(func(e *agentv1.Envelope) bool { return e.GetTaskStarted() != nil })
	send(&agentv1.Envelope{Message: &agentv1.Envelope_StateSyncRequest{StateSyncRequest: &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 8}}})
	reply := wait(func(e *agentv1.Envelope) bool { return e.GetStateSyncReply() != nil }).GetStateSyncReply()
	found := false
	for _, snapshot := range reply.Executions {
		if snapshot.ExecutionId == offer.ExecutionId {
			found = true
			if !snapshot.Started || snapshot.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNSPECIFIED {
				t.Fatal("live execution misreported terminal", snapshot)
			}
		}
	}
	if !found {
		t.Fatal(reply)
	}
	send(&agentv1.Envelope{Message: &agentv1.Envelope_TaskCancel{TaskCancel: &agentv1.TaskCancel{TaskId: offer.TaskId, ExecutionId: offer.ExecutionId, ReasonCode: "user_requested", RequestedAtUnixMs: time.Now().UnixMilli()}}})
	if result := wait(func(e *agentv1.Envelope) bool { return e.GetTaskFinished() != nil }).GetTaskFinished(); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
		t.Fatal("canceled possible effect reported successful", result)
	}
	localCurrent.Store(false)
	select {
	case err := <-agentDone:
		if err == nil {
			t.Fatal("revocation succeeded")
		}
	case <-ctx.Done():
		t.Fatal("revoked session remained active")
	}
	select {
	case <-controllerDone:
	case <-ctx.Done():
		t.Fatal("Controller did not observe disconnect")
	}
}

func TestAgentSessionMissingAdmissionOrWrongRootNeverSendsHello(t *testing.T) {
	state, _, pin := sessionTLSStates(t)
	e, err := newAgentExecutor(t.TempDir(), testActions(func(context.Context, []byte) executionResult {
		t.Fatal("rejected trust executed a capability")
		return executionResult{}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, mode := range []string{"no-current", "no-admit", "no-probe", "wrong-root"} {
		cfg := sessionConfig(pin)
		switch mode {
		case "no-current":
			cfg.Current = nil
		case "no-admit":
			cfg.Admit = nil
		case "no-probe":
			cfg.Probe = nil
		case "wrong-root":
			cfg.RootSPKISHA256 = strings.Repeat("b", 64)
		}
		a, b := sessionFramePair()
		if err := runAgentSession(context.Background(), a, state, "connection-a", cfg, e); err == nil {
			t.Fatal(mode)
		}
		select {
		case <-b.read:
			t.Fatal("unadmitted session sent hello", mode)
		default:
		}
		_ = a.Close()
	}
}
