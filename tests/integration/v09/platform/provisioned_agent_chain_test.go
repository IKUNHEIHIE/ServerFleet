package platform_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/internal/transport"
	"strconv"
	"sync"
	"testing"
	"time"
)

type provisionedChainStore struct {
	mu   sync.Mutex
	data []byte
}

func (s *provisionedChainStore) View(ctx context.Context, fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(bytes.Clone(s.data))
}
func (s *provisionedChainStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, e := fn(ctx, bytes.Clone(s.data))
	if e == nil {
		s.data = bytes.Clone(raw)
	}
	return e
}

// This is a synthetic PKI/admin fixture; it starts no production service. The
// actual provisioned loader, enrollment HTTP and unmodified Agent CLI are used.
func TestProvisionedIntermediateEnrollmentAcceptedByRealAgentCLI(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary-user private identity ownership")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	base := t.TempDir()
	if e := os.Chmod(base, 0700); e != nil {
		t.Fatal(e)
	}
	root, online, bundle := filepath.Join(base, "offline-root"), filepath.Join(base, "online"), filepath.Join(base, "public-bundle")
	pin, e := localpki.CreateOfflineRoot(root, "Synthetic chain regression Root")
	if e != nil {
		t.Fatal(e)
	}
	if e = localpki.RequestIntermediate(online, "chain-regression"); e != nil {
		t.Fatal(e)
	}
	if e = localpki.SignIntermediate(root, online, bundle, "chain-regression", 3); e != nil {
		t.Fatal(e)
	}
	if _, e = localpki.InstallIntermediate(online, bundle, pin); e != nil {
		t.Fatal(e)
	}
	material, e := localpki.LoadProvisioned(online, pin)
	if e != nil {
		t.Fatal(e)
	}
	gate := func(context.Context, transport.EnrollmentOperation) error { return nil } // isolated trusted admin fixture
	svc, e := transport.NewEnrollmentService(&provisionedChainStore{}, material.Issuer, gate, gate)
	if e != nil {
		t.Fatal(e)
	}
	pki := newTestPKI(t)
	server := httptest.NewUnstartedServer(nil)
	authority := "localhost:" + strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	handler, e := transport.NewEnrollmentHTTP(svc, transport.EnrollmentHTTPConfig{Authority: authority, ServerName: "localhost", MaxConcurrent: 2, MaxIPs: 4, PerIPPerMinute: 10, GlobalPerMinute: 20, Current: func(context.Context) error { return nil }, Rejected: func(context.Context, transport.EnrollmentHTTPRejection) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	server.Config.Handler = handler
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}}
	server.StartTLS()
	defer server.Close()
	token, e := svc.IssueToken(ctx, transport.EnrollmentOperation{ActorID: "synthetic-admin", Action: "enrollment.issue"}, transport.EnrollmentTokenInput{Name: "actual-agent", TTLSeconds: 600})
	if e != nil {
		t.Fatal(e)
	}
	tokenFile := filepath.Join(base, "enrollment-token")
	if e = os.WriteFile(tokenFile, []byte(token.Token), 0600); e != nil {
		t.Fatal(e)
	}
	httpsRoot := filepath.Join(base, "https-root.pem")
	if e = os.WriteFile(httpsRoot, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pki.rootDER}), 0600); e != nil {
		t.Fatal(e)
	}
	agent := filepath.Join(moduleRoot(t), "reports", "agent")
	if _, e = os.Stat(agent); e != nil {
		agent = buildAppBinary(t, moduleRoot(t), "./apps/agent")
	}
	state := filepath.Join(base, "identity")
	args := []string{"-controller-url", "https://" + authority, "-state-dir", state, "-root-cert", material.CertificatePath, "-root-pin", pin, "-controller-ca", httpsRoot, "-enroll-token-file", tokenFile}
	stdout, stderr, code := runAgentCLI(t, ctx, agent, args...)
	if code != 0 {
		t.Fatalf("pending exit=%d %s %s", code, stdout, stderr)
	}
	var pending struct {
		State string `json:"state"`
	}
	if e = json.Unmarshal([]byte(stdout), &pending); e != nil || pending.State != "pending" {
		t.Fatal("unexpected pending result", e)
	}
	views, e := svc.List(ctx, transport.EnrollmentOperation{ActorID: "synthetic-admin", Action: "enrollment.list"})
	if e != nil || len(views) != 1 {
		t.Fatal(e)
	}
	view := views[0]
	if _, e = svc.Decide(ctx, transport.EnrollmentOperation{ActorID: "synthetic-admin", Action: "enrollment.decide", EnrollmentID: view.ID, NodeID: view.NodeID, ExpectedRevision: view.Revision}, true); e != nil {
		t.Fatal(e)
	}
	stdout, stderr, code = runAgentCLI(t, ctx, agent, args...)
	if code != 0 {
		t.Fatalf("issued chain rejected by actual Agent: exit=%d %s %s", code, stdout, stderr)
	}
	var issued struct{ State, NodeID string }
	if e = json.Unmarshal([]byte(stdout), &issued); e != nil || issued.State != "issued" || issued.NodeID != view.NodeID {
		t.Fatal("unexpected issuance", stdout, e)
	}
	stdout, stderr, code = runAgentCLI(t, ctx, agent, "-status", "-state-dir", state, "-root-pin", pin)
	if code != 0 {
		t.Fatalf("persisted identity rejected: %s %s", stdout, stderr)
	}
	var status struct {
		Enrolled bool
		NodeID   string
	}
	if e = json.Unmarshal([]byte(stdout), &status); e != nil || !status.Enrolled || status.NodeID != view.NodeID {
		t.Fatal("identity not persisted", stdout, e)
	}
}
