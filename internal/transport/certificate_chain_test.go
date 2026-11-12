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
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func intermediateIssuerFixture(t *testing.T) (*CertificateIssuer, *x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	root, rootKey, roots := testPeerCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(20), Subject: pkix.Name{CommonName: "synthetic-online-issuer"}, NotBefore: root.NotBefore, NotAfter: root.NotAfter, IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, root, key.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	pin := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	issuer, err := NewCertificateIssuer(der, key, roots, [][]byte{root.Raw}, hex.EncodeToString(pin[:]), 1, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return issuer, root, key, roots
}

func TestIssuerRejectsDuplicateMissingOrForeignTerminalChain(t *testing.T) {
	issuer, root, key, roots := intermediateIssuerFixture(t)
	foreign, _, _ := testPeerCA(t)
	for name, chain := range map[string][][]byte{"missing": nil, "duplicate-issuer": {issuer.certificate.Raw}, "duplicate-root": {root.Raw, root.Raw}, "foreign": {foreign.Raw}, "reordered": {root.Raw, issuer.certificate.Raw}} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCertificateIssuer(issuer.certificate.Raw, key, roots, chain, issuer.rootPin, 1, 30*time.Minute); err == nil {
				t.Fatal("invalid supplied chain accepted")
			}
		})
	}
}

func legacyChain(issuer *CertificateIssuer, cert issuedAgentCertificate) string {
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.DER})
	for i := 0; i < 2; i++ {
		raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuer.certificate.Raw})...)
	}
	return string(raw)
}

func TestLegacyApprovedResponseAndRenewalRepairKeepSameLeafAndAuthority(t *testing.T) {
	ctx := context.Background()
	s, store, now := enrollmentFixture(t)
	s.issuer, _, _, _ = intermediateIssuerFixture(t)
	token, err := s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "chain-regression", TTLSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	input := AgentEnrollmentInput{EnrollmentToken: token.Token, RequestID: "chain-regression", CSRPEM: enrollmentCSR(t), PinnedRootSPKI: s.issuer.rootPin, AgentVersion: "0.9.2-test", Platform: "linux"}
	if _, err = s.Submit(ctx, input); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.list"})
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	view := list[0]
	if _, err = s.Decide(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.decide", EnrollmentID: view.ID, NodeID: view.NodeID, ExpectedRevision: view.Revision}, true); err != nil {
		t.Fatal(err)
	}
	original, err := s.Submit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeEnrollment(store.data)
	if err != nil {
		t.Fatal(err)
	}
	record := state.Requests[input.RequestID]
	record.Certificate.PEM = legacyChain(s.issuer, record.Certificate)
	state.Requests[input.RequestID] = record
	store.data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := s.Submit(ctx, input)
	if err != nil || fixed != original {
		t.Fatal("approved replay changed its leaf or failed repair", err)
	}
	unchanged, err := decodeEnrollment(store.data)
	if err != nil || unchanged.Requests[input.RequestID].Certificate.PEM != record.Certificate.PEM || len(unchanged.Identities) != 1 {
		t.Fatal("repair overwrote persisted issuance evidence", err)
	}
	bad := record.Certificate
	bad.DER = bytes.Clone(bad.DER)
	bad.DER[0] ^= 1
	if _, err = s.issuer.responsePEM(bad); err == nil {
		t.Fatal("mismatched leaf evidence normalized")
	}
	tlsState := renewalTLS(t, original)
	*now = now.Add(20 * time.Minute)
	if _, err = s.Submit(ctx, input); err == nil {
		t.Fatal("expired enrollment token bypassed")
	}
	peer, err := NewPeerVerifier(s.Lookup)
	if err != nil {
		t.Fatal(err)
	}
	peer.now = s.now
	id, err := peer.Verify(ctx, tlsState, "")
	if err != nil {
		t.Fatal(err)
	}
	request := IdentityRenewalRequest{RequestID: enrollmentDigest("chain-renewal"), CSRPEM: input.CSRPEM}
	gate := func(context.Context, string) error { return nil }
	next, err := s.RenewIdentity(ctx, tlsState, request, gate)
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeEnrollment(store.data)
	if err != nil {
		t.Fatal(err)
	}
	renewal := state.Renewals[id.FingerprintSHA256]
	renewal.Certificate.PEM = legacyChain(s.issuer, renewal.Certificate)
	state.Renewals[id.FingerprintSHA256] = renewal
	store.data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.RenewIdentity(ctx, tlsState, request, gate)
	if err != nil || replay != next {
		t.Fatal("renewal replay changed its leaf or failed repair", err)
	}
	if err = s.Revoke(ctx, EnrollmentOperation{ActorID: "admin", Action: "agent.revoke", NodeID: id.NodeID, Fingerprint: id.FingerprintSHA256, ExpectedRevision: id.TrustGeneration}, id.FingerprintSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RenewIdentity(ctx, tlsState, request, gate); err == nil {
		t.Fatal("revoked ancestor repaired/renewed")
	}
}
