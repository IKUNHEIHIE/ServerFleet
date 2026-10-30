package transport

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"time"
)

var ErrPeerIdentity = errors.New("transport: peer identity rejected")

// AgentIdentity is looked up by the complete leaf certificate fingerprint, not
// an untrusted CN, message node_id, IP, or a shared enrollment credential.
// This candidate port must be reviewed before composition with node persistence.
type AgentIdentity struct {
	NodeID            string
	FingerprintSHA256 string
	Serial            string
	Enabled           bool
	Revoked           bool
	TrustGeneration   uint64
}
type IdentityLookup func(context.Context, string) (AgentIdentity, error)

type PeerVerifier struct {
	lookup IdentityLookup
	now    func() time.Time
}

func NewPeerVerifier(lookup IdentityLookup) (*PeerVerifier, error) {
	if lookup == nil {
		return nil, ErrPeerIdentity
	}
	return &PeerVerifier{lookup, time.Now}, nil
}

// Verify must be repeated at dispatch and inbound event processing, not just on
// connection establishment, so a long-lived TLS session cannot cache revocation.
func (v *PeerVerifier) Verify(ctx context.Context, state tls.ConnectionState, messageNodeID string) (AgentIdentity, error) {
	if !state.HandshakeComplete {
		return AgentIdentity{}, ErrPeerIdentity
	}
	return v.verify(ctx, state, messageNodeID)
}

func (v *PeerVerifier) verify(ctx context.Context, state tls.ConnectionState, messageNodeID string) (AgentIdentity, error) {
	if ctx == nil {
		return AgentIdentity{}, ErrPeerIdentity
	}
	if err := ctx.Err(); err != nil {
		return AgentIdentity{}, err
	}
	if v == nil || v.lookup == nil || v.now == nil || state.Version < tls.VersionTLS13 || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
		return AgentIdentity{}, ErrPeerIdentity
	}
	leaf := state.PeerCertificates[0]
	now := v.now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return AgentIdentity{}, ErrPeerIdentity
	}
	h := sha256.Sum256(leaf.Raw)
	fingerprint := hex.EncodeToString(h[:])
	id, err := v.lookup(ctx, fingerprint)
	if ctx.Err() != nil {
		return AgentIdentity{}, ctx.Err()
	}
	if err != nil || !id.Enabled || id.Revoked || id.NodeID == "" || id.FingerprintSHA256 != fingerprint || id.Serial != leaf.SerialNumber.String() || id.TrustGeneration == 0 || messageNodeID != "" && messageNodeID != id.NodeID {
		return AgentIdentity{}, ErrPeerIdentity
	}
	return id, nil
}

// ControllerTLSConfig performs standard CA/EKU checks before the live mapping
// callback. There is no InsecureSkipVerify, client-cert optional mode, or TOFU.
func (v *PeerVerifier) ControllerTLSConfig(server tls.Certificate, roots *x509.CertPool) (*tls.Config, error) {
	if v == nil || v.lookup == nil || roots == nil || len(server.Certificate) == 0 || server.PrivateKey == nil {
		return nil, ErrPeerIdentity
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{server}, ClientCAs: roots.Clone(), ClientAuth: tls.RequireAndVerifyClientCert, SessionTicketsDisabled: true, VerifyConnection: func(state tls.ConnectionState) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// VerifyConnection runs after CA verification but before HandshakeComplete.
		_, err := v.verify(ctx, state, "")
		return err
	}}, nil
}

// AgentTLSConfig pins the configured CA and server DNS identity using the TLS
// library verifier. Credentials are local; no private key travels on the wire.
func AgentTLSConfig(client tls.Certificate, roots *x509.CertPool, serverName string) (*tls.Config, error) {
	if roots == nil || serverName == "" || len(client.Certificate) == 0 || client.PrivateKey == nil {
		return nil, ErrPeerIdentity
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots.Clone(), Certificates: []tls.Certificate{client}, SessionTicketsDisabled: true}, nil
}
