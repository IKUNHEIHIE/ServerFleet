package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/transport"
)

var errAgentEnrollment = errors.New("agent: enrollment unavailable or outcome unconfirmed; retain existing identity")
var errAgentEnrollmentDenied = errors.New("agent: enrollment denied; no automatic replacement or retry")

type agentEnrollmentConfig struct {
	// Public Root certificate and origin MUST come from independent trusted
	// installation input. Neither a response nor a redirect can supply them.
	Origin, RootCertificatePEM string
	ControllerCAPEM            string
	Current                    func(context.Context) error
}
type agentEnrollmentObservation struct {
	State     string
	ExpiresAt time.Time
}
type agentEnrollmentClient struct {
	identity *agentIdentityStore
	origin   string
	client   *http.Client
	current  func(context.Context) error
}

func newAgentEnrollmentClient(identity *agentIdentityStore, cfg agentEnrollmentConfig) (*agentEnrollmentClient, error) {
	u, err := url.Parse(cfg.Origin)
	if identity == nil || cfg.Current == nil || err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || cfg.Origin != "https://"+u.Host || net.ParseIP(u.Hostname()) != nil || strings.ToLower(u.Hostname()) != u.Hostname() {
		return nil, errAgentEnrollment
	}
	if len(cfg.RootCertificatePEM) > 16<<10 {
		return nil, errAgentEnrollment
	}
	raw := bytes.TrimSpace([]byte(cfg.RootCertificatePEM))
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || !bytes.HasPrefix(raw, []byte("-----BEGIN CERTIFICATE-----")) || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errAgentEnrollment
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !root.IsCA || !root.BasicConstraintsValid || root.KeyUsage&x509.KeyUsageCertSign == 0 || root.CheckSignatureFrom(root) != nil || time.Now().Before(root.NotBefore) || !time.Now().Before(root.NotAfter) {
		return nil, errAgentEnrollment
	}
	pin := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	if hex.EncodeToString(pin[:]) != identity.pin {
		return nil, errAgentEnrollment
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	// The explicit HTTPS trust root does not replace the independently pinned
	// internal identity Root checked again by identity.Accept.
	tlsPin := hex.EncodeToString(pin[:])
	if cfg.ControllerCAPEM != "" {
		if len(cfg.ControllerCAPEM) > 16<<10 {
			return nil, errAgentEnrollment
		}
		var e error
		roots, tlsPin, e = parseAgentConnectionRoot([]byte(cfg.ControllerCAPEM))
		if e != nil {
			return nil, errAgentEnrollment
		}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: u.Hostname(), VerifyConnection: func(state tls.ConnectionState) error {
		// VerifyConnection runs before HandshakeComplete becomes true.
		if state.Version < tls.VersionTLS13 || state.ServerName != u.Hostname() || len(state.VerifiedChains) == 0 {
			return errAgentEnrollment
		}
		for _, chain := range state.VerifiedChains {
			if len(chain) == 0 {
				return errAgentEnrollment
			}
			verifiedPin := sha256.Sum256(chain[len(chain)-1].RawSubjectPublicKeyInfo)
			if hex.EncodeToString(verifiedPin[:]) != tlsPin {
				return errAgentEnrollment
			}
		}
		return nil
	}}
	// A fresh connection per explicit attempt, no environment proxy, cookie
	// jar, TLS client credential, fallback roots, compression or hidden retries.
	t := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: -1}).DialContext,
		TLSClientConfig: tlsConfig, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		DisableCompression: true, DisableKeepAlives: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 16 << 10}
	client := &http.Client{Transport: t, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &agentEnrollmentClient{identity: identity, origin: cfg.Origin, client: client, current: cfg.Current}, nil
}

func (c *agentEnrollmentClient) observe(ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = errAgentEnrollment
		}
	}()
	if ctx.Err() != nil || c.current(ctx) != nil || ctx.Err() != nil {
		return errAgentEnrollment
	}
	return nil
}

func decodeAgentEnrollmentResult(raw []byte) (transport.EnrollmentHTTPResult, error) {
	var out transport.EnrollmentHTTPResult
	if !utf8.Valid(raw) {
		return out, errAgentEnrollment
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return out, errAgentEnrollment
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return out, errAgentEnrollment
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return out, errAgentEnrollment
		}
		switch key {
		case "state", "nodeId", "certificatePem", "intermediateChainPem", "expiresAt":
			var text string
			if json.Unmarshal(value, &text) != nil || text == "" {
				return out, errAgentEnrollment
			}
		case "trustGeneration":
			var generation uint64
			if json.Unmarshal(value, &generation) != nil || generation == 0 {
				return out, errAgentEnrollment
			}
		default:
			// No server-supplied endpoint can redirect the bootstrap credential.
			return out, errAgentEnrollment
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || !seen["state"] || !seen["expiresAt"] {
		return out, errAgentEnrollment
	}
	if _, err = d.Token(); err != io.EOF || json.Unmarshal(raw, &out) != nil || out.ExpiresAt.IsZero() {
		return out, errAgentEnrollment
	}
	if out.State == "issued" {
		if len(seen) != 6 || out.NodeID == "" || out.CertificatePEM == "" || out.IntermediateChainPEM == "" || out.TrustGeneration == 0 {
			return out, errAgentEnrollment
		}
	} else if (out.State != "pending" && out.State != "denied") || len(seen) != 2 {
		return out, errAgentEnrollment
	}
	return out, nil
}

// EnrollOnce sends exactly ONE explicit attempt with the existing CSR. Pending
// is an observation, never installation success. Lost/invalid responses retain
// the identity; this method does not loop, prepare, replace or retry anything.
func (c *agentEnrollmentClient) EnrollOnce(ctx context.Context, token string) (agentEnrollmentObservation, error) {
	var zero agentEnrollmentObservation
	if c == nil || ctx == nil || c.identity == nil || c.client == nil || c.current == nil {
		return zero, errAgentEnrollment
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if c.observe(bounded) != nil {
		return zero, errAgentEnrollment
	}
	input, err := c.identity.Request(bounded, token)
	if err != nil {
		return zero, errAgentEnrollment
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 32<<10 {
		clear(raw)
		return zero, errAgentEnrollment
	}
	defer clear(raw)
	r, err := http.NewRequestWithContext(bounded, http.MethodPost, c.origin+"/api/v1/agent/enroll", bytes.NewReader(raw))
	if err != nil {
		return zero, errAgentEnrollment
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.GetBody = nil
	r.Close = true
	response, err := c.client.Do(r)
	if err != nil {
		return zero, errAgentEnrollment
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 || response.ContentLength > 96<<10 || len(response.Header.Values("Set-Cookie")) != 0 || response.Header.Get("Content-Encoding") != "" || len(response.Header.Values("Content-Type")) != 1 {
		return zero, errAgentEnrollment
	}
	media, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return zero, errAgentEnrollment
	}
	for key := range params {
		if key != "charset" {
			return zero, errAgentEnrollment
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (96<<10)+1))
	defer clear(body)
	if err != nil || len(body) > 96<<10 || bytes.Contains(body, []byte(token)) || c.observe(bounded) != nil {
		return zero, errAgentEnrollment
	}
	result, err := decodeAgentEnrollmentResult(body)
	if err != nil {
		return zero, errAgentEnrollment
	}
	observation := agentEnrollmentObservation{State: result.State, ExpiresAt: result.ExpiresAt}
	if result.State == "denied" {
		return observation, errAgentEnrollmentDenied
	}
	if !time.Now().Before(result.ExpiresAt) {
		return zero, errAgentEnrollment
	}
	if result.State == "issued" {
		issued := transport.AgentEnrollmentResult{State: "approved", NodeID: result.NodeID, CertificatePEM: result.CertificatePEM + result.IntermediateChainPEM, TrustGeneration: result.TrustGeneration, ExpiresAt: result.ExpiresAt}
		if c.identity.Accept(bounded, issued) != nil || c.observe(bounded) != nil {
			return zero, errAgentEnrollment
		}
	}
	return observation, nil
}
