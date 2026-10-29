package transport

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
	"errors"
	"sync"
	"testing"
	"time"
)

type enrollmentTestStore struct {
	mu   sync.Mutex
	data []byte
	fail bool
}

func (s *enrollmentTestStore) View(ctx context.Context, fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(bytes.Clone(s.data))
}
func (s *enrollmentTestStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := fn(bytes.Clone(s.data))
	if err != nil {
		return err
	}
	if s.fail {
		return errors.New("failed commit")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.data = bytes.Clone(next)
	return nil
}
func enrollmentFixture(t *testing.T) (*EnrollmentService, *enrollmentTestStore, *time.Time) {
	t.Helper()
	ca, key, roots := testPeerCA(t)
	fp := sha256.Sum256(ca.RawSubjectPublicKeyInfo)
	issuer, err := NewCertificateIssuer(ca.Raw, key, roots, nil, hex.EncodeToString(fp[:]), 1, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := &enrollmentTestStore{}
	service, err := NewEnrollmentService(store, issuer, func(context.Context, EnrollmentOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	return service, store, &now
}
func enrollmentCSR(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "untrusted-root", Organization: []string{"not-copied"}}, DNSNames: []string{"evil.test"}, ExtraExtensions: []pkix.Extension{{Id: []int{2, 5, 29, 19}, Critical: true, Value: []byte{0x30, 3, 1, 1, 0xff}}}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
}
func TestEnrollmentOneTimeCSRApprovalReinstallAndLiveRevocation(t *testing.T) {
	ctx := context.Background()
	s, store, _ := enrollmentFixture(t)
	token, err := s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "Isolated", TTLSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	input := AgentEnrollmentInput{token.Token, "request-1", enrollmentCSR(t), s.issuer.rootPin, "synthetic-agent", "linux"}
	result, err := s.Submit(ctx, input)
	if err != nil || result.State != "pending" || result.CertificatePEM != "" {
		t.Fatal(result, err)
	}
	changed := input
	changed.RequestID = "request-2"
	if _, err = s.Submit(ctx, changed); err != ErrEnrollmentRejected {
		t.Fatal("one token used twice", err)
	}
	changed = input
	changed.CSRPEM = enrollmentCSR(t)
	if _, err = s.Submit(ctx, changed); err != ErrEnrollmentRejected {
		t.Fatal("CSR changed on replay", err)
	}
	list, err := s.List(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.list"})
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	decision := EnrollmentOperation{ActorID: "admin", Action: "enrollment.decide", EnrollmentID: list[0].ID, NodeID: list[0].NodeID, ExpectedRevision: 1}
	store.fail = true
	if _, err = s.Decide(ctx, decision, true); err != ErrEnrollmentUnavailable {
		t.Fatal(err)
	}
	store.fail = false
	result, err = s.Submit(ctx, input)
	if err != nil || result.State != "pending" {
		t.Fatal("commit failure leaked certificate", result, err)
	}
	approved, err := s.Decide(ctx, decision, true)
	if err != nil || approved.Status != "approved" {
		t.Fatal(approved, err)
	}
	if _, err = s.Decide(ctx, decision, true); err != ErrEnrollmentRejected {
		t.Fatal("approval replay", err)
	}
	result, err = s.Submit(ctx, input)
	if err != nil || result.CertificatePEM == "" {
		t.Fatal(result, err)
	}
	block, _ := pem.Decode([]byte(result.CertificatePEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.IsCA || len(cert.DNSNames) > 0 || len(cert.Subject.Organization) > 0 || cert.Subject.CommonName != approved.NodeID || len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatal("CSR authority copied")
	}
	fp := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(fp[:])
	old, err := s.Lookup(ctx, fingerprint)
	if err != nil || old.Revoked {
		t.Fatal(old, err)
	}
	replace, err := s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue", NodeID: old.NodeID}, EnrollmentTokenInput{Name: "Reinstall", TTLSeconds: 600, ReplaceNodeID: old.NodeID})
	if err != nil {
		t.Fatal(err)
	}
	second := input
	second.EnrollmentToken = replace.Token
	second.RequestID = "request-reinstall"
	second.CSRPEM = enrollmentCSR(t)
	if _, err = s.Submit(ctx, second); err != nil {
		t.Fatal(err)
	}
	decision.EnrollmentID = second.RequestID
	if _, err = s.Decide(ctx, decision, true); err != nil {
		t.Fatal(err)
	}
	old, err = s.Lookup(ctx, fingerprint)
	if err != nil || !old.Revoked {
		t.Fatal("reinstall did not atomically revoke old identity", old, err)
	}
	if _, err = s.Submit(ctx, input); err != ErrEnrollmentRejected {
		t.Fatal("old enrollment redelivered revoked identity", err)
	}
	result, err = s.Submit(ctx, second)
	if err != nil || result.NodeID != old.NodeID {
		t.Fatal(result, err)
	}
	block, _ = pem.Decode([]byte(result.CertificatePEM))
	fp = sha256.Sum256(block.Bytes)
	fingerprint = hex.EncodeToString(fp[:])
	if err = s.Revoke(ctx, EnrollmentOperation{ActorID: "admin", Action: "agent.revoke", NodeID: old.NodeID, Fingerprint: fingerprint, ExpectedRevision: 1}, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(ctx, second); err != ErrEnrollmentRejected {
		t.Fatal(err)
	}
	if bytes.Contains(store.data, []byte(token.Token)) || bytes.Contains(store.data, []byte(replace.Token)) || bytes.Contains(store.data, []byte("PRIVATE KEY")) {
		t.Fatal("private enrollment credential persisted")
	}
}
func TestEnrollmentExpiryPinSignatureAndAdminGate(t *testing.T) {
	ctx := context.Background()
	s, _, now := enrollmentFixture(t)
	token, err := s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "Expiry", TTLSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	input := AgentEnrollmentInput{token.Token, "request-1", enrollmentCSR(t), s.issuer.rootPin, "agent", "linux"}
	changed := input
	changed.PinnedRootSPKI = "wrong"
	if _, err = s.Submit(ctx, changed); err != ErrEnrollmentRejected {
		t.Fatal(err)
	}
	changed = input
	changed.CSRPEM = changed.CSRPEM + changed.CSRPEM
	if _, err = s.Submit(ctx, changed); err != ErrEnrollmentRejected {
		t.Fatal("extra PEM accepted", err)
	}
	*now = now.Add(time.Minute)
	if _, err = s.Submit(ctx, input); err != ErrEnrollmentRejected {
		t.Fatal("expired token", err)
	}
	*now = now.Add(-time.Second)
	if _, err = s.Submit(ctx, input); err != ErrEnrollmentUnavailable {
		t.Fatal("clock rollback revived token", err)
	}
	s.gate = func(context.Context, EnrollmentOperation) error { return errors.New("not authorized") }
	if _, err = s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "Denied", TTLSeconds: 60}); err != ErrEnrollmentRejected {
		t.Fatal("untrusted admin claim", err)
	}
}
