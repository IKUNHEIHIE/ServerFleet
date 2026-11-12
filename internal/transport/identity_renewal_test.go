package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

func renewalFixture(t *testing.T) (*EnrollmentService, *enrollmentTestStore, *time.Time, tls.ConnectionState, IdentityRenewalRequest) {
	t.Helper()
	ctx := context.Background()
	s, store, now := enrollmentFixture(t)
	token, e := s.IssueToken(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.issue"}, EnrollmentTokenInput{Name: "renewable", TTLSeconds: 600})
	if e != nil {
		t.Fatal(e)
	}
	in := AgentEnrollmentInput{EnrollmentToken: token.Token, RequestID: "renewable-node", CSRPEM: enrollmentCSR(t), PinnedRootSPKI: s.issuer.rootPin, AgentVersion: "synthetic", Platform: "linux"}
	if _, e = s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	views, e := s.List(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.list"})
	if e != nil || len(views) != 1 {
		t.Fatal(views, e)
	}
	view := views[0]
	_, e = s.Decide(ctx, EnrollmentOperation{ActorID: "admin", Action: "enrollment.decide", EnrollmentID: view.ID, NodeID: view.NodeID, ExpectedRevision: view.Revision}, true)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Submit(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	state := renewalTLS(t, result)
	peer, e := NewPeerVerifier(s.Lookup)
	if e != nil {
		t.Fatal(e)
	}
	peer.now = s.now
	id, e := peer.Verify(ctx, state, "")
	if e != nil {
		t.Fatal(e)
	}
	*now = now.Add(20 * time.Minute)
	return s, store, now, state, IdentityRenewalRequest{RequestID: enrollmentDigest("serverfleet.identity.renew:" + id.FingerprintSHA256), CSRPEM: in.CSRPEM}
}
func renewalTLS(t *testing.T, result AgentEnrollmentResult) tls.ConnectionState {
	t.Helper()
	block, _ := pem.Decode([]byte(result.CertificatePEM))
	leaf, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	return tls.ConnectionState{HandshakeComplete: true, Version: tls.VersionTLS13, PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf}}}
}

func TestIdentityRenewalLostResponseReplayActivationAndExpiry(t *testing.T) {
	s, _, now, state, input := renewalFixture(t)
	ctx := context.Background()
	gate := func(context.Context, string) error { return nil }
	next, e := s.RenewIdentity(ctx, state, input, gate)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.RenewIdentity(ctx, state, input, gate)
	if e != nil || next != replay {
		t.Fatal("lost-response retry issued a different identity", e)
	}
	verifier, _ := NewPeerVerifier(s.Lookup)
	verifier.now = s.now
	if _, e = verifier.Verify(ctx, state, ""); e != nil {
		t.Fatal("old identity retired before activation", e)
	}
	changed := input
	changed.RequestID = enrollmentDigest("different")
	if _, e = s.RenewIdentity(ctx, state, changed, gate); !errors.Is(e, ErrEnrollmentRejected) {
		t.Fatal("accepted changed replay", e)
	}
	newState := renewalTLS(t, next)
	peer, e := verifier.Verify(ctx, newState, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ActivateRenewedIdentity(ctx, peer); e != nil {
		t.Fatal(e)
	}
	*now = now.Add(3 * time.Minute)
	if _, e = verifier.Verify(ctx, state, ""); e == nil {
		t.Fatal("old leaf survived activation overlap")
	}
	*now = now.Add(8 * time.Minute)
	if _, e = verifier.Verify(ctx, newState, ""); e != nil {
		t.Fatal("renewed identity failed beyond original expiry", e)
	}
}

func TestIdentityRenewalRejectsForeignCSRCurrentDenialAndRevokedAncestor(t *testing.T) {
	s, _, _, state, input := renewalFixture(t)
	ctx := context.Background()
	gate := func(context.Context, string) error { return nil }
	foreign := input
	foreign.CSRPEM = enrollmentCSR(t)
	if _, e := s.RenewIdentity(ctx, state, foreign, gate); e == nil {
		t.Fatal("foreign-key CSR accepted")
	}
	if _, e := s.RenewIdentity(ctx, state, input, func(context.Context, string) error { return ErrEnrollmentRejected }); e == nil {
		t.Fatal("disabled node renewed")
	}
	next, e := s.RenewIdentity(ctx, state, input, gate)
	if e != nil {
		t.Fatal(e)
	}
	verifier, _ := NewPeerVerifier(s.Lookup)
	verifier.now = s.now
	old, e := verifier.Verify(ctx, state, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Revoke(ctx, EnrollmentOperation{ActorID: "admin", Action: "agent.revoke", NodeID: old.NodeID, Fingerprint: old.FingerprintSHA256, ExpectedRevision: old.TrustGeneration}, old.FingerprintSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e = verifier.Verify(ctx, renewalTLS(t, next), ""); e == nil {
		t.Fatal("pending descendant survived revocation")
	}
}

func TestIdentityRenewalRejectsUnverifiedTLSAndCommitFailure(t *testing.T) {
	s, store, _, state, input := renewalFixture(t)
	ctx := context.Background()
	gate := func(context.Context, string) error { return nil }
	unverified := state
	unverified.VerifiedChains = nil
	if _, e := s.RenewIdentity(ctx, unverified, input, gate); e == nil {
		t.Fatal("unverified peer renewed")
	}
	store.fail = true
	if _, e := s.RenewIdentity(ctx, state, input, gate); e == nil {
		t.Fatal("failed commit reported issuance")
	}
	store.fail = false
	if _, e := s.RenewIdentity(ctx, state, input, gate); e != nil {
		t.Fatal("failed commit consumed retry", e)
	}
}
