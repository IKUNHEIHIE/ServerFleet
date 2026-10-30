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
func (s *enrollmentTestStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := fn(ctx, bytes.Clone(s.data))
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
	gate := func(context.Context, EnrollmentOperation) error { return nil } // explicit authority fixture
	service, err := NewEnrollmentService(store, issuer, gate, gate)
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

func TestEnrollmentCommitAuthorityRollsBackTokensCertificatesRevocationAndReads(t *testing.T) {
	s, store, _ := enrollmentFixture(t)
	ctx := context.Background()
	allow := s.commit
	denied := errors.New("authority revoked after entry")
	checkUnchanged := func(before []byte, err error) {
		t.Helper()
		if err != ErrEnrollmentRejected || !bytes.Equal(before, store.data) {
			t.Fatal("commit denial changed durable enrollment state", err)
		}
	}
	deny := func(want EnrollmentOperation) {
		s.commit = func(_ context.Context, got EnrollmentOperation) error {
			if got != want {
				t.Error("commit authority lost exact operation binding")
			}
			return denied
		}
	}
	issueOp := EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}
	before := bytes.Clone(store.data)
	deny(issueOp)
	token, err := s.IssueToken(ctx, issueOp, EnrollmentTokenInput{Name: "Revoked issue", TTLSeconds: 600})
	checkUnchanged(before, err)
	if token.Token != "" {
		t.Fatal("denied transaction returned enrollment secret")
	}
	s.commit = allow
	token, err = s.IssueToken(ctx, issueOp, EnrollmentTokenInput{Name: "Test pending", TTLSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	input := AgentEnrollmentInput{token.Token, "request-fenced", enrollmentCSR(t), s.issuer.rootPin, "0.9.0", "linux"}
	if _, err = s.Submit(ctx, input); err != nil {
		t.Fatal(err)
	}
	listOp := EnrollmentOperation{ActorID: "admin", Action: "enrollment.list"}
	before = bytes.Clone(store.data)
	deny(listOp)
	list, err := s.List(ctx, listOp)
	checkUnchanged(before, err)
	if list != nil {
		t.Fatal("commit denial returned privileged list")
	}
	s.commit = allow
	list, err = s.List(ctx, listOp)
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	decideOp := EnrollmentOperation{ActorID: "admin", Action: "enrollment.decide", EnrollmentID: list[0].ID, NodeID: list[0].NodeID, ExpectedRevision: list[0].Revision}
	before = bytes.Clone(store.data)
	deny(decideOp)
	decision, err := s.Decide(ctx, decideOp, true)
	checkUnchanged(before, err)
	if decision.ID != "" {
		t.Fatal("denied transaction acknowledged certificate")
	}
	if result, err := s.Submit(ctx, input); err != nil || result.State != "pending" || result.CertificatePEM != "" {
		t.Fatal("commit denial published certificate", err)
	}
	s.commit = allow
	if _, err = s.Decide(ctx, decideOp, true); err != nil {
		t.Fatal(err)
	}
	result, err := s.Submit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(result.CertificatePEM))
	fp := sha256.Sum256(block.Bytes)
	fingerprint := hex.EncodeToString(fp[:])
	revokeOp := EnrollmentOperation{ActorID: "admin", Action: "agent.revoke", NodeID: result.NodeID, Fingerprint: fingerprint, ExpectedRevision: result.TrustGeneration}
	before = bytes.Clone(store.data)
	deny(revokeOp)
	checkUnchanged(before, s.Revoke(ctx, revokeOp, fingerprint))
	if id, err := s.Lookup(ctx, fingerprint); err != nil || id.Revoked {
		t.Fatal("unauthorized revocation persisted", err)
	}
	s.commit = allow
	if err = s.Revoke(ctx, revokeOp, fingerprint); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentMissingPanickingUnavailableAndCancelledAuthorityCloses(t *testing.T) {
	s, store, _ := enrollmentFixture(t)
	if _, err := NewEnrollmentService(store, s.issuer, s.gate, nil); err != ErrEnrollmentUnavailable {
		t.Fatal("missing commit authority accepted", err)
	}
	op := EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}
	for _, stage := range []string{"entry", "commit"} {
		for _, mode := range []string{"panic", "unavailable", "cancel"} {
			t.Run(stage+"-"+mode, func(t *testing.T) {
				s, store, _ := enrollmentFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				gate := func(context.Context, EnrollmentOperation) error {
					switch mode {
					case "panic":
						panic("authority source failed")
					case "cancel":
						cancel()
						return nil
					default:
						return ErrEnrollmentUnavailable
					}
				}
				if stage == "entry" {
					s.gate = gate
				} else {
					s.commit = gate
				}
				before := bytes.Clone(store.data)
				token, err := s.IssueToken(ctx, op, EnrollmentTokenInput{Name: "Unsafe source", TTLSeconds: 600})
				if err != ErrEnrollmentUnavailable || token.Token != "" || !bytes.Equal(before, store.data) {
					t.Fatal("failed authority source allowed mutation", err)
				}
			})
		}
	}
}
