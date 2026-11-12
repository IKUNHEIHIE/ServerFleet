package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
	"serverfleet.local/serverfleet/internal/transport"
)

var errAgentIdentityRotated = errors.New("agent: identity renewed; reconnect after draining")

func installedAgentIdentityChanged(ctx context.Context, request agentruntime.ConnectRequest, prior agentIdentityFacts) (bool, error) {
	status, e := agentStatus(ctx, request.StateDir, request.RootPin)
	if e != nil || !status.Enrolled || status.NodeID != prior.NodeID || status.TrustGeneration != prior.TrustGeneration {
		return false, errAgentIdentity
	}
	return status.Fingerprint != prior.Fingerprint, nil
}

// Read/replace uses its own store handle: the current link lends the old pair
// under another handle's lock until the session has safely drained its workers.
func maybeRenewAgentIdentity(ctx context.Context, request agentruntime.ConnectRequest) (bool, error) {
	store, e := openAgentIdentity(request.StateDir, request.RootPin, 1)
	if e != nil {
		return false, e
	}
	defer store.Close()
	facts, enrolled, e := store.Status(ctx)
	if e != nil || !enrolled {
		return false, errAgentIdentity
	}
	if time.Until(facts.ExpiresAt) > 4*time.Hour {
		return false, nil
	}
	root, e := readAgentFile(request.RootCertificate, 16<<10)
	if e != nil {
		return false, e
	}
	var ca []byte
	if request.ControllerCA != "" {
		ca, e = readAgentFile(request.ControllerCA, 16<<10)
		if e != nil {
			return false, e
		}
	}
	client, e := newAgentEnrollmentClient(store, agentEnrollmentConfig{Origin: strings.Replace(request.ControllerURL, "wss://", "https://", 1), RootCertificatePEM: string(root), ControllerCAPEM: string(ca), Current: func(context.Context) error { return nil }})
	if e != nil {
		return false, e
	}
	prepared, key, e := store.prepared()
	clear(key)
	if e != nil {
		return false, e
	}
	digest := sha256.Sum256([]byte("serverfleet.identity.renew:" + facts.Fingerprint))
	raw, e := json.Marshal(transport.IdentityRenewalRequest{RequestID: hex.EncodeToString(digest[:]), CSRPEM: prepared.CSRPEM})
	if e != nil {
		return false, e
	}
	var issued transport.AgentEnrollmentResult
	e = store.WithCertificate(ctx, func(_ context.Context, current agentIdentityFacts) error {
		if current != facts {
			return errAgentIdentity
		}
		return nil
	}, func(ctx context.Context, pair tls.Certificate) error {
		t := client.client.Transport.(*http.Transport)
		t.TLSClientConfig = t.TLSClientConfig.Clone()
		t.TLSClientConfig.Certificates = []tls.Certificate{pair}
		defer t.CloseIdleConnections()
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		r, e := http.NewRequestWithContext(bounded, "POST", client.origin+"/api/v1/agent/identity/renew", bytes.NewReader(raw))
		if e != nil {
			return e
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		r.GetBody = nil
		r.Close = true
		response, e := client.client.Do(r)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 || response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 || response.ContentLength > 96<<10 || len(response.Header.Values("Set-Cookie")) != 0 || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Type") != "application/json" {
			return errAgentIdentity
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, (96<<10)+1))
		if e != nil || len(body) > 96<<10 {
			return errAgentIdentity
		}
		result, e := decodeAgentEnrollmentResult(body)
		if e != nil || result.State != "issued" {
			return errAgentIdentity
		}
		issued = transport.AgentEnrollmentResult{State: "approved", NodeID: result.NodeID, CertificatePEM: result.CertificatePEM + result.IntermediateChainPEM, TrustGeneration: result.TrustGeneration, ExpiresAt: result.ExpiresAt}
		return nil
	})
	if e != nil {
		return false, e
	}
	if e = store.replaceIssued(ctx, facts, issued); e != nil {
		return false, e
	}
	slog.Info("agent identity renewed", "node_id", facts.NodeID, "expires_at", issued.ExpiresAt.UTC())
	return true, nil
}

func (s *agentIdentityStore) replaceIssued(ctx context.Context, prior agentIdentityFacts, result transport.AgentEnrollmentResult) error {
	if ctx == nil || ctx.Err() != nil {
		return errAgentIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return errAgentIdentity
	}
	_, key, e := s.prepared()
	if e != nil {
		return e
	}
	defer clear(key)
	_, next, e := s.validate(result, key)
	if e != nil || next.NodeID != prior.NodeID || next.TrustGeneration != prior.TrustGeneration || !next.ExpiresAt.After(prior.ExpiresAt) {
		return errAgentIdentity
	}
	current, e := s.read("issued.json", 64<<10)
	if e != nil {
		return e
	}
	var existing transport.AgentEnrollmentResult
	if json.Unmarshal(current, &existing) != nil {
		return errAgentIdentity
	}
	_, observed, e := s.validate(existing, key)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return e
	}
	if bytes.Equal(current, raw) {
		return nil
	}
	if observed != prior {
		return errAgentIdentity
	}
	var random [16]byte
	if _, e = rand.Read(random[:]); e != nil {
		return e
	}
	temporary := "issued-" + hex.EncodeToString(random[:]) + ".next"
	defer s.root.Remove(temporary)
	if e = s.write(temporary, raw); e != nil {
		return e
	}
	// Recheck before the atomic rename. A failed network/disk operation leaves the
	// previous issued identity untouched, never a partially truncated JSON file.
	again, e := s.read("issued.json", 64<<10)
	if e != nil || !bytes.Equal(current, again) || ctx.Err() != nil {
		return errAgentIdentity
	}
	if info, e := s.root.Lstat(temporary); e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errAgentIdentity
	}
	if e = s.root.Rename(temporary, "issued.json"); e != nil {
		return e
	}
	return s.sync()
}

// Failures are logged as fixed categories only, never HTTP responses or keys.
func renewalLog(err error, expires time.Time) {
	if err != nil {
		slog.Warn("agent identity renewal pending", "code", "IDENTITY_RENEWAL_UNAVAILABLE", "expires_at", expires.UTC())
	}
}
