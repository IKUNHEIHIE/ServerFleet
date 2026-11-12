package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var ErrEnrollmentRejected = errors.New("transport: enrollment rejected")
var ErrEnrollmentUnavailable = errors.New("transport: enrollment source unavailable")

// Authenticated admin/sole task proof is supplied by trusted composition. Gate
// MUST revalidate all fields/current policy; these strings are not credentials.
type EnrollmentOperation struct {
	ActorID, Action, EnrollmentID, NodeID, Fingerprint string
	ExpectedRevision                                   uint64
}
type EnrollmentGate func(context.Context, EnrollmentOperation) error
type EnrollmentStore interface {
	View(context.Context, func([]byte) error) error
	// Callback context carries the trusted transaction-bound policy reader.
	Update(context.Context, func(context.Context, []byte) ([]byte, error)) error
}
type EnrollmentTokenInput struct {
	Name          string
	TTLSeconds    int
	ReplaceNodeID string
}
type EnrollmentToken struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (t EnrollmentToken) String() string   { return "transport.EnrollmentToken{token:redacted}" }
func (t EnrollmentToken) GoString() string { return t.String() }

type AgentEnrollmentInput struct {
	EnrollmentToken string `json:"enrollmentToken"`
	RequestID       string `json:"requestId"`
	CSRPEM          string `json:"csrPem"`
	PinnedRootSPKI  string `json:"pinnedRootSpkiSha256"`
	AgentVersion    string `json:"agentVersion"`
	Platform        string `json:"platform"`
}

func (i AgentEnrollmentInput) String() string {
	return "transport.AgentEnrollmentInput{token:redacted,csr:public}"
}
func (i AgentEnrollmentInput) GoString() string { return i.String() }

type EnrollmentView struct {
	ID          string    `json:"id"`
	NodeID      string    `json:"nodeId"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	RequestedAt time.Time `json:"requestedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Revision    uint64    `json:"revision"`
	CSRSPKI     string    `json:"csrSpkiSha256"`
}
type AgentEnrollmentResult struct {
	State           string    `json:"state"`
	NodeID          string    `json:"nodeId,omitempty"`
	CertificatePEM  string    `json:"certificatePem,omitempty"`
	TrustGeneration uint64    `json:"trustGeneration,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}
type enrollmentTokenRecord struct {
	ID, Name, Digest, RequestID, ReplaceNodeID string
	Expires                                    time.Time
}
type enrollmentRecord struct {
	View                       EnrollmentView
	CSR                        []byte
	RequestDigest, TokenDigest string
	Certificate                issuedAgentCertificate
	DecidedBy                  string
}
type enrollmentAudit struct {
	Actor, Action, NodeID, EnrollmentID string
	At                                  time.Time
}
type enrollmentState struct {
	Version            int
	ClockFloor         time.Time
	Tokens             map[string]enrollmentTokenRecord
	Requests           map[string]enrollmentRecord
	Identities         map[string]AgentIdentity
	RevocationSequence uint64
	Outbox             []enrollmentAudit
	Renewals           map[string]identityRenewal `json:",omitempty"`
	Retirements        map[string]time.Time       `json:",omitempty"`
}
type EnrollmentService struct {
	store  EnrollmentStore
	issuer *CertificateIssuer
	gate   EnrollmentGate
	// Must use current transaction-bound/read-only authority; no nested writer.
	commit EnrollmentGate
	now    func() time.Time
}

func NewEnrollmentService(store EnrollmentStore, issuer *CertificateIssuer, gate, commit EnrollmentGate) (*EnrollmentService, error) {
	if store == nil || issuer == nil || gate == nil || commit == nil {
		return nil, ErrEnrollmentUnavailable
	}
	return &EnrollmentService{store: store, issuer: issuer, gate: gate, commit: commit, now: time.Now}, nil
}
func decodeEnrollment(b []byte) (*enrollmentState, error) {
	st := &enrollmentState{}
	if len(b) > 16<<20 || len(b) > 0 && (json.Unmarshal(b, st) != nil || st.Version != 1) {
		return nil, ErrEnrollmentUnavailable
	}
	st.Version = 1
	if st.Tokens == nil {
		st.Tokens = map[string]enrollmentTokenRecord{}
	}
	if st.Requests == nil {
		st.Requests = map[string]enrollmentRecord{}
	}
	if st.Identities == nil {
		st.Identities = map[string]AgentIdentity{}
	}
	if err := prepareIdentityRenewals(st); err != nil {
		return nil, err
	}
	if len(st.Tokens) > 10000 || len(st.Requests) > 10000 || len(st.Identities) > 20000 || len(st.Outbox) > 10000 {
		return nil, ErrEnrollmentUnavailable
	}
	for fp, id := range st.Identities {
		digest, err := hex.DecodeString(fp)
		if err != nil || len(digest) != 32 || id.FingerprintSHA256 != fp || !validBindingID(id.NodeID) || id.Serial == "" || id.TrustGeneration == 0 {
			return nil, ErrEnrollmentUnavailable
		}
	}
	return st, nil
}
func enrollmentDigest(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func enrollmentSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrEnrollmentUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func validEnrollmentSecret(secret string) bool {
	b, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == secret
}
func x509CSR(raw []byte) (*x509.CertificateRequest, error) {
	csr, err := x509.ParseCertificateRequest(raw)
	if err != nil || csr.CheckSignature() != nil {
		return nil, ErrEnrollmentRejected
	}
	return csr, nil
}
func (s *EnrollmentService) change(ctx context.Context, fn func(context.Context, *enrollmentState, time.Time) error) error {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return ErrEnrollmentUnavailable
	}
	err := s.store.Update(ctx, func(txCtx context.Context, b []byte) ([]byte, error) {
		if txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil {
			return nil, ErrEnrollmentUnavailable
		}
		st, err := decodeEnrollment(b)
		if err != nil {
			return nil, err
		}
		now := s.now().UTC()
		if now.IsZero() || now.Before(st.ClockFloor) {
			return nil, ErrEnrollmentUnavailable
		}
		st.ClockFloor = now
		if err = fn(txCtx, st, now); err != nil {
			return nil, err
		}
		out, err := json.Marshal(st)
		if err != nil || len(out) > 16<<20 || ctx.Err() != nil || txCtx.Err() != nil {
			return nil, ErrEnrollmentUnavailable
		}
		return out, nil
	})
	if errors.Is(err, ErrEnrollmentRejected) {
		return ErrEnrollmentRejected
	}
	if err != nil {
		return ErrEnrollmentUnavailable
	}
	return nil
}
func (s *EnrollmentService) admit(ctx context.Context, op EnrollmentOperation, action string) error {
	if s == nil || ctx == nil || s.gate == nil || s.commit == nil || op.Action != action || !validBindingID(op.ActorID) || len(op.ActorID) > 128 {
		return ErrEnrollmentRejected
	}
	return invokeEnrollmentGate(ctx, s.gate, op)
}
func invokeEnrollmentGate(ctx context.Context, gate EnrollmentGate, op EnrollmentOperation) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrEnrollmentUnavailable
		}
	}()
	if ctx == nil || ctx.Err() != nil || gate == nil {
		return ErrEnrollmentUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = gate(bounded, op)
	if bounded.Err() != nil || errors.Is(err, ErrEnrollmentUnavailable) {
		return ErrEnrollmentUnavailable
	}
	if err != nil {
		return ErrEnrollmentRejected
	}
	return nil
}
func (s *EnrollmentService) privileged(ctx context.Context, op EnrollmentOperation, fn func(context.Context, *enrollmentState, time.Time) error) error {
	return s.change(ctx, func(txCtx context.Context, st *enrollmentState, now time.Time) error {
		if err := fn(txCtx, st, now); err != nil {
			return err
		}
		// A successful entry check cannot authorize a later transaction. A
		// denial rolls back the mutation, certificate mapping AND audit outbox.
		return invokeEnrollmentGate(txCtx, s.commit, op)
	})
}
func appendEnrollmentAudit(st *enrollmentState, op EnrollmentOperation, action, node, id string, now time.Time) error {
	if len(st.Outbox) >= 10000 {
		return ErrEnrollmentUnavailable
	}
	st.Outbox = append(st.Outbox, enrollmentAudit{op.ActorID, action, node, id, now})
	return nil
}
func (s *EnrollmentService) IssueToken(ctx context.Context, op EnrollmentOperation, input EnrollmentTokenInput) (EnrollmentToken, error) {
	if err := s.admit(ctx, op, "enrollment.issue"); err != nil {
		return EnrollmentToken{}, err
	}
	if input.Name == "" || len(input.Name) > 128 || input.TTLSeconds < 60 || input.TTLSeconds > 600 || input.ReplaceNodeID != op.NodeID || input.ReplaceNodeID != "" && (!validBindingID(input.ReplaceNodeID) || len(input.ReplaceNodeID) > 128) {
		return EnrollmentToken{}, ErrEnrollmentRejected
	}
	var result EnrollmentToken
	err := s.privileged(ctx, op, func(_ context.Context, st *enrollmentState, now time.Time) error {
		if input.ReplaceNodeID != "" {
			exists := false
			for _, id := range st.Identities {
				if id.NodeID == input.ReplaceNodeID {
					exists = true
				}
			}
			if !exists {
				return ErrEnrollmentRejected
			}
		}
		for digest, t := range st.Tokens {
			if !now.Before(t.Expires) && t.RequestID == "" {
				delete(st.Tokens, digest)
			}
		}
		if len(st.Tokens) >= 10000 {
			return ErrEnrollmentUnavailable
		}
		token, err := enrollmentSecret()
		if err != nil {
			return err
		}
		id, err := randomLinkID()
		if err != nil {
			return err
		}
		record := enrollmentTokenRecord{ID: id, Name: input.Name, Digest: enrollmentDigest(token), ReplaceNodeID: input.ReplaceNodeID, Expires: now.Add(time.Duration(input.TTLSeconds) * time.Second)}
		st.Tokens[record.Digest] = record
		result = EnrollmentToken{id, token, record.Expires}
		return appendEnrollmentAudit(st, op, "enrollment.token.issue", input.ReplaceNodeID, id, now)
	})
	if err != nil {
		return EnrollmentToken{}, err
	}
	return result, nil
}
func (s *EnrollmentService) Submit(ctx context.Context, input AgentEnrollmentInput) (AgentEnrollmentResult, error) {
	if s == nil || !validEnrollmentSecret(input.EnrollmentToken) || !validBindingID(input.RequestID) || len(input.RequestID) > 128 || input.PinnedRootSPKI != s.issuer.rootPin || input.AgentVersion == "" || len(input.AgentVersion) > 128 || input.Platform != "linux" {
		return AgentEnrollmentResult{}, ErrEnrollmentRejected
	}
	csr, err := parseEnrollmentCSR(input.CSRPEM)
	if err != nil {
		return AgentEnrollmentResult{}, ErrEnrollmentRejected
	}
	digestBytes, _ := json.Marshal(struct {
		Request, Version, Platform, Root string
		CSR                              []byte
	}{input.RequestID, input.AgentVersion, input.Platform, input.PinnedRootSPKI, csr.Raw})
	digest := enrollmentDigest(string(digestBytes))
	var result AgentEnrollmentResult
	var outcome error
	err = s.change(ctx, func(_ context.Context, st *enrollmentState, now time.Time) error {
		token, ok := st.Tokens[enrollmentDigest(input.EnrollmentToken)]
		if !ok || !now.Before(token.Expires) {
			outcome = ErrEnrollmentRejected
			return nil
		}
		if token.RequestID != "" {
			record, ok := st.Requests[token.RequestID]
			if !ok || record.RequestDigest != digest {
				outcome = ErrEnrollmentRejected
				return nil
			}
			result = AgentEnrollmentResult{State: record.View.Status, ExpiresAt: record.View.ExpiresAt}
			if record.View.Status == "approved" {
				id, ok := st.Identities[record.Certificate.Fingerprint]
				if !ok || id.Revoked || !id.Enabled || id.TrustGeneration != s.issuer.generation || !now.Before(record.Certificate.Expires) {
					outcome = ErrEnrollmentRejected
					return nil
				}
				result.NodeID = id.NodeID
				result.CertificatePEM = record.Certificate.PEM
				result.TrustGeneration = id.TrustGeneration
				result.ExpiresAt = record.Certificate.Expires
			}
			return nil
		}
		if _, used := st.Requests[input.RequestID]; used || len(st.Requests) >= 10000 {
			outcome = ErrEnrollmentRejected
			return nil
		}
		node := token.ReplaceNodeID
		if node == "" {
			var err error
			node, err = randomLinkID()
			if err != nil {
				return err
			}
		}
		spki := sha256.Sum256(csr.RawSubjectPublicKeyInfo)
		view := EnrollmentView{input.RequestID, node, token.Name, "pending", now, token.Expires, 1, hex.EncodeToString(spki[:])}
		st.Requests[view.ID] = enrollmentRecord{View: view, CSR: bytes.Clone(csr.Raw), RequestDigest: digest, TokenDigest: token.Digest}
		token.RequestID = view.ID
		st.Tokens[token.Digest] = token
		result = AgentEnrollmentResult{State: "pending", ExpiresAt: token.Expires}
		return appendEnrollmentAudit(st, EnrollmentOperation{ActorID: "enrolling-agent"}, "enrollment.request", node, view.ID, now)
	})
	if err != nil {
		return AgentEnrollmentResult{}, err
	}
	if outcome != nil {
		return AgentEnrollmentResult{}, outcome
	}
	return result, nil
}
func (s *EnrollmentService) List(ctx context.Context, op EnrollmentOperation) ([]EnrollmentView, error) {
	if err := s.admit(ctx, op, "enrollment.list"); err != nil {
		return nil, err
	}
	var out []EnrollmentView
	err := s.privileged(ctx, op, func(_ context.Context, st *enrollmentState, now time.Time) error {
		out = []EnrollmentView{}
		for _, record := range st.Requests {
			v := record.View
			if v.Status == "pending" && !now.Before(v.ExpiresAt) {
				v.Status = "expired"
			}
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *EnrollmentService) Decide(ctx context.Context, op EnrollmentOperation, approve bool) (EnrollmentView, error) {
	if err := s.admit(ctx, op, "enrollment.decide"); err != nil {
		return EnrollmentView{}, err
	}
	var result EnrollmentView
	var outcome error
	err := s.privileged(ctx, op, func(txCtx context.Context, st *enrollmentState, now time.Time) error {
		record, ok := st.Requests[op.EnrollmentID]
		if !ok || record.View.NodeID != op.NodeID || record.View.Revision != op.ExpectedRevision || record.View.Status != "pending" {
			return ErrEnrollmentRejected
		}
		if !now.Before(record.View.ExpiresAt) {
			outcome = ErrEnrollmentRejected
			return nil
		}
		record.View.Status = "rejected"
		record.DecidedBy = op.ActorID
		record.View.Revision++
		if approve {
			csr, err := x509CSR(record.CSR)
			if err != nil {
				return err
			}
			certificate, err := s.issuer.issue(txCtx, record.View.NodeID, csr, now)
			if err != nil {
				return ErrEnrollmentUnavailable
			}
			for fp, id := range st.Identities {
				if id.NodeID == record.View.NodeID && !id.Revoked {
					id.Revoked = true
					st.Identities[fp] = id
					st.RevocationSequence++
					if st.RevocationSequence == 0 {
						return ErrEnrollmentUnavailable
					}
				}
			}
			if len(st.Identities) >= 20000 {
				return ErrEnrollmentUnavailable
			}
			st.Identities[certificate.Fingerprint] = AgentIdentity{record.View.NodeID, certificate.Fingerprint, certificate.Serial, true, false, s.issuer.generation}
			record.Certificate = certificate
			record.View.Status = "approved"
		}
		st.Requests[op.EnrollmentID] = record
		result = record.View
		return appendEnrollmentAudit(st, op, "enrollment."+record.View.Status, record.View.NodeID, record.View.ID, now)
	})
	if err != nil {
		return EnrollmentView{}, err
	}
	if outcome != nil {
		return EnrollmentView{}, outcome
	}
	return result, nil
}
func (s *EnrollmentService) Lookup(ctx context.Context, fingerprint string) (AgentIdentity, error) {
	var id AgentIdentity
	if s == nil || ctx == nil {
		return id, ErrPeerIdentity
	}
	err := s.store.View(ctx, func(b []byte) error {
		st, err := decodeEnrollment(b)
		if err != nil {
			return err
		}
		var ok bool
		id, ok = st.Identities[fingerprint]
		if !ok || s.now().Before(st.ClockFloor) || !st.Retirements[fingerprint].IsZero() && !s.now().Before(st.Retirements[fingerprint]) {
			return ErrPeerIdentity
		}
		return nil
	})
	if err != nil || ctx.Err() != nil {
		return AgentIdentity{}, ErrPeerIdentity
	}
	return id, nil
}
func (s *EnrollmentService) Revoke(ctx context.Context, op EnrollmentOperation, fingerprint string) error {
	if err := s.admit(ctx, op, "agent.revoke"); err != nil {
		return err
	}
	if op.Fingerprint != fingerprint {
		return ErrEnrollmentRejected
	}
	return s.privileged(ctx, op, func(_ context.Context, st *enrollmentState, now time.Time) error {
		id, ok := st.Identities[fingerprint]
		if !ok || id.NodeID != op.NodeID || id.Revoked || op.ExpectedRevision != id.TrustGeneration || st.RevocationSequence == ^uint64(0) {
			return ErrEnrollmentRejected
		}
		id.Revoked = true
		st.Identities[fingerprint] = id
		revokeRenewalDescendants(st, fingerprint)
		st.RevocationSequence++
		return appendEnrollmentAudit(st, op, "agent.revoke", id.NodeID, fingerprint, now)
	})
}
