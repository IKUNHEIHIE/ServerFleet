package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Renewal reuses the locally held key. It cannot change node, Root or generation.
type IdentityRenewalRequest struct {
	RequestID string `json:"requestId"`
	CSRPEM    string `json:"csrPem"`
}
type identityRenewal struct {
	NodeID, RequestID, CSRHash string
	Certificate                issuedAgentCertificate
	ActivatedAt                time.Time
}

func (s *EnrollmentService) CheckIdentityIssuer(ctx context.Context) error {
	if s == nil || s.issuer == nil || s.now == nil || ctx == nil || ctx.Err() != nil {
		return ErrEnrollmentUnavailable
	}
	now := s.now()
	if now.Before(s.issuer.certificate.NotBefore) || !now.Add(s.issuer.lifetime).Before(s.issuer.certificate.NotAfter) {
		return ErrEnrollmentUnavailable
	}
	return s.store.View(ctx, func(raw []byte) error { _, e := decodeEnrollment(raw); return e })
}

func prepareIdentityRenewals(st *enrollmentState) error {
	if st.Renewals == nil {
		st.Renewals = map[string]identityRenewal{}
	}
	if st.Retirements == nil {
		st.Retirements = map[string]time.Time{}
	}
	if len(st.Renewals) > 20000 || len(st.Retirements) > 20000 {
		return ErrEnrollmentUnavailable
	}
	for previous, r := range st.Renewals {
		if !canonicalDigest(previous) || !validBindingID(r.NodeID) || !canonicalDigest(r.RequestID) || !canonicalDigest(r.CSRHash) || !canonicalDigest(r.Certificate.Fingerprint) || r.Certificate.Expires.IsZero() {
			return ErrEnrollmentUnavailable
		}
	}
	for fp, at := range st.Retirements {
		if !canonicalDigest(fp) || at.IsZero() {
			return ErrEnrollmentUnavailable
		}
	}
	return nil
}

func (s *EnrollmentService) RenewIdentity(ctx context.Context, state tls.ConnectionState, input IdentityRenewalRequest, current func(context.Context, string) error) (AgentEnrollmentResult, error) {
	var out AgentEnrollmentResult
	if s == nil || ctx == nil || current == nil || !canonicalDigest(input.RequestID) {
		return out, ErrEnrollmentRejected
	}
	verifier, e := NewPeerVerifier(s.Lookup)
	if e != nil {
		return out, e
	}
	verifier.now = s.now
	peer, e := verifier.Verify(ctx, state, "")
	if e != nil {
		return out, ErrEnrollmentRejected
	}
	csr, e := parseEnrollmentCSR(input.CSRPEM)
	if e != nil || !bytes.Equal(csr.RawSubjectPublicKeyInfo, state.PeerCertificates[0].RawSubjectPublicKeyInfo) {
		return out, ErrEnrollmentRejected
	}
	digest := enrollmentDigest(string(csr.Raw))
	e = s.change(ctx, func(tx context.Context, st *enrollmentState, now time.Time) error {
		id, ok := st.Identities[peer.FingerprintSHA256]
		if !ok || id != peer || !id.Enabled || id.Revoked || id.TrustGeneration != s.issuer.generation || !now.Before(state.PeerCertificates[0].NotAfter) || !st.Retirements[id.FingerprintSHA256].IsZero() || current(tx, id.NodeID) != nil {
			return ErrEnrollmentRejected
		}
		if prior, ok := st.Renewals[id.FingerprintSHA256]; ok {
			next, exists := st.Identities[prior.Certificate.Fingerprint]
			if prior.RequestID != input.RequestID || prior.CSRHash != digest || !exists || next.Revoked || !next.Enabled || !now.Before(prior.Certificate.Expires) {
				return ErrEnrollmentRejected
			}
			out = renewalResult(prior.Certificate, id.NodeID, s.issuer.generation)
			return nil
		}
		// Do not issue hours early on every heartbeat. A retry returns the same leaf.
		if state.PeerCertificates[0].NotAfter.Sub(now) > s.issuer.lifetime/2 {
			return ErrEnrollmentRejected
		}
		for fp, r := range st.Renewals {
			if !now.Before(r.Certificate.Expires) {
				delete(st.Renewals, fp)
				delete(st.Retirements, fp)
				delete(st.Identities, fp)
				delete(st.Identities, r.Certificate.Fingerprint)
			}
		}
		if len(st.Renewals) >= 20000 || len(st.Identities) >= 20000 {
			return ErrEnrollmentUnavailable
		}
		cert, e := s.issuer.issue(tx, id.NodeID, csr, now)
		if e != nil {
			return e
		}
		if current(tx, id.NodeID) != nil {
			return ErrEnrollmentRejected
		}
		st.Renewals[id.FingerprintSHA256] = identityRenewal{NodeID: id.NodeID, RequestID: input.RequestID, CSRHash: digest, Certificate: cert}
		st.Identities[cert.Fingerprint] = AgentIdentity{id.NodeID, cert.Fingerprint, cert.Serial, true, false, s.issuer.generation}
		out = renewalResult(cert, id.NodeID, s.issuer.generation)
		return appendEnrollmentAudit(st, EnrollmentOperation{ActorID: id.NodeID}, "agent.identity.renew", id.NodeID, input.RequestID, now)
	})
	return out, e
}
func renewalResult(c issuedAgentCertificate, node string, generation uint64) AgentEnrollmentResult {
	return AgentEnrollmentResult{State: "approved", NodeID: node, CertificatePEM: c.PEM, TrustGeneration: generation, ExpiresAt: c.Expires}
}

func (s *EnrollmentService) IdentityExpiry(ctx context.Context, fingerprint string) (time.Time, error) {
	var expiry time.Time
	e := s.store.View(ctx, func(raw []byte) error {
		st, e := decodeEnrollment(raw)
		if e != nil {
			return e
		}
		for _, r := range st.Requests {
			if r.Certificate.Fingerprint == fingerprint {
				expiry = r.Certificate.Expires
				return nil
			}
		}
		for _, r := range st.Renewals {
			if r.Certificate.Fingerprint == fingerprint {
				expiry = r.Certificate.Expires
				return nil
			}
		}
		return ErrPeerIdentity
	})
	return expiry, e
}

// Called only after a real new-certificate WSS handshake. Lost renewal responses
// leave the old certificate usable; successful activation gives it a 2m overlap.
func (s *EnrollmentService) ActivateRenewedIdentity(ctx context.Context, peer AgentIdentity) error {
	var pending bool
	if e := s.store.View(ctx, func(raw []byte) error {
		st, e := decodeEnrollment(raw)
		if e != nil {
			return e
		}
		for _, r := range st.Renewals {
			if r.Certificate.Fingerprint == peer.FingerprintSHA256 && r.ActivatedAt.IsZero() {
				pending = true
			}
		}
		return nil
	}); e != nil {
		return e
	}
	if !pending {
		return nil
	}
	return s.change(ctx, func(_ context.Context, st *enrollmentState, now time.Time) error {
		id, ok := st.Identities[peer.FingerprintSHA256]
		if !ok || id != peer || !id.Enabled || id.Revoked || id.TrustGeneration != s.issuer.generation {
			return ErrEnrollmentRejected
		}
		for previous, r := range st.Renewals {
			if r.Certificate.Fingerprint == peer.FingerprintSHA256 && r.ActivatedAt.IsZero() {
				old, ok := st.Identities[previous]
				if !ok || old.Revoked || !old.Enabled || old.NodeID != peer.NodeID {
					return ErrEnrollmentRejected
				}
				r.ActivatedAt = now
				st.Renewals[previous] = r
				st.Retirements[previous] = now.Add(2 * time.Minute)
				return appendEnrollmentAudit(st, EnrollmentOperation{ActorID: id.NodeID}, "agent.identity.activate", id.NodeID, r.RequestID, now)
			}
		}
		return nil
	})
}
func revokeRenewalDescendants(st *enrollmentState, parent string) {
	queue := []string{parent}
	seen := map[string]bool{}
	for len(queue) > 0 {
		fp := queue[0]
		queue = queue[1:]
		if seen[fp] {
			continue
		}
		seen[fp] = true
		if r, ok := st.Renewals[fp]; ok {
			next, exists := st.Identities[r.Certificate.Fingerprint]
			if exists {
				next.Revoked = true
				st.Identities[r.Certificate.Fingerprint] = next
				queue = append(queue, r.Certificate.Fingerprint)
			}
		}
	}
}

func NewIdentityRenewalHTTP(service *EnrollmentService, authority, serverName string, current func(context.Context, string) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTransportProblem(w, 405, "METHOD_NOT_ALLOWED")
			return
		}
		if service == nil || current == nil || r.URL.EscapedPath() != "/api/v1/agent/identity/renew" || r.URL.RawQuery != "" || r.TLS == nil || r.TLS.ServerName != serverName || r.Host != authority {
			writeTransportProblem(w, 403, "IDENTITY_SOURCE_REJECTED")
			return
		}
		for _, key := range []string{"Cookie", "Origin", "Authorization", "Proxy-Authorization", "X-CSRF-Token"} {
			if len(r.Header.Values(key)) > 0 {
				writeTransportProblem(w, 403, "IDENTITY_SOURCE_REJECTED")
				return
			}
		}
		if r.Header.Get("Content-Type") != "application/json" {
			writeTransportProblem(w, 400, "INVALID_REQUEST")
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		var input IdentityRenewalRequest
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeTransportProblem(w, 400, "INVALID_REQUEST")
			return
		}
		result, e := service.RenewIdentity(bounded, *r.TLS, input, current)
		if e != nil {
			writeEnrollmentError(w, e)
			return
		}
		response, e := enrollmentHTTPResult(result)
		if e != nil {
			writeEnrollmentError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
}
