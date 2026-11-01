package certificates

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultACMEDirectory = "https://acme-v02.api.letsencrypt.org/directory"
	maxACMEBodyBytes     = 1 << 20
	acmeJWSContentType   = "application/jose+json"
)

var (
	ErrACMEConfig    = errors.New("certificates: acme configuration is invalid")
	ErrACMEProtocol  = errors.New("certificates: acme protocol error")
	ErrACMEChallenge = errors.New("certificates: acme dns-01 challenge failed")
	ErrACMEIssuance  = errors.New("certificates: acme issuance failed")
	ErrACMESigner    = errors.New("certificates: acme signer failed")
	ErrACMERedirect  = errors.New("certificates: acme redirect is not allowed")
)

// ACMESigner exposes only public-key and digest-signing operations for a key held
// by the secret store. SignDigest returns a raw ECDSA P-256 r||s signature.
type ACMESigner interface {
	PublicKey(ctx context.Context, ref KeyRef) (crypto.PublicKey, error)
	SignDigest(ctx context.Context, ref KeyRef, digest []byte) ([]byte, error)
}

// ACMEDNS01Publisher publishes the dns-01 TXT value and returns a cleanup
// function. The client calls cleanup for every published value even when a later
// step fails.
type ACMEDNS01Publisher interface {
	Publish(ctx context.Context, fqdn, value string) (func(context.Context) error, error)
}

// ACMEConfig configures the RFC 8555 DNS-01 client.
type ACMEConfig struct {
	DirectoryURL string
	Client       *http.Client
	AccountKey   KeyRef
	Signer       ACMESigner
	Challenge    ACMEDNS01Publisher
	PollInterval time.Duration
	Timeout      time.Duration
}

// ACMERequest issues one certificate for CommonName using KeyRef.
type ACMERequest struct {
	CommonName string
	KeyRef     KeyRef
}

// ACMEIssued is the verified certificate chain returned by the ACME server.
type ACMEIssued struct {
	CertificatePEM []byte
	ChainPEM       []byte
	Serial         string
	NotBefore      time.Time
	NotAfter       time.Time
}

// ACMEClient implements a bounded RFC 8555 DNS-01 flow. It never handles private
// key material; signing is delegated to ACMESigner.
type ACMEClient struct {
	directoryURL string
	client       *http.Client
	dirMu        sync.Mutex
	directory    acmeDirectory
	dirFetched   bool
	accountKey   KeyRef
	signer       ACMESigner
	challenge    ACMEDNS01Publisher
	pollInterval time.Duration
	timeout      time.Duration
}

func NewACMEClient(config ACMEConfig) (*ACMEClient, error) {
	if config.Signer == nil || config.Challenge == nil {
		return nil, ErrACMEConfig
	}
	if err := config.AccountKey.Validate(); err != nil {
		return nil, ErrACMEConfig
	}
	directory := config.DirectoryURL
	if directory == "" {
		directory = DefaultACMEDirectory
	}
	parsed, err := url.Parse(directory)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("%w: directory url", ErrACMEConfig)
	}
	client := config.Client
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
				MaxIdleConns:          4,
				IdleConnTimeout:       30 * time.Second,
			},
		}
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return ErrACMERedirect
	}
	poll := config.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &ACMEClient{
		directoryURL: directory, client: &cloned, accountKey: config.AccountKey,
		signer: config.Signer, challenge: config.Challenge,
		pollInterval: poll, timeout: timeout,
	}, nil
}

type acmeDirectory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
}

type acmeProblem struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
}

func (p *acmeProblem) Error() string {
	if p == nil {
		return "acme: problem"
	}
	kind := p.Type
	if index := strings.LastIndex(kind, ":"); index >= 0 {
		kind = kind[index+1:]
	}
	if kind == "" {
		kind = "unknown"
	}
	return fmt.Sprintf("acme: problem %s (status %d)", kind, p.Status)
}

func (p *acmeProblem) Unwrap() error { return ErrACMEProtocol }

type acmeOrder struct {
	Status         string       `json:"status"`
	Authorizations []string     `json:"authorizations"`
	Finalize       string       `json:"finalize"`
	Certificate    string       `json:"certificate"`
	Error          *acmeProblem `json:"error"`
}

type acmeAuthorization struct {
	Status     string          `json:"status"`
	Identifier acmeIdentifier  `json:"identifier"`
	Wildcard   bool            `json:"wildcard"`
	Challenges []acmeChallenge `json:"challenges"`
}

type acmeIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type acmeChallenge struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type acmeJWK struct {
	Crv string `json:"crv"`
	Kty string `json:"kty"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type acmeProtected struct {
	Alg   string   `json:"alg"`
	Nonce string   `json:"nonce"`
	URL   string   `json:"url"`
	KID   string   `json:"kid,omitempty"`
	JWK   *acmeJWK `json:"jwk,omitempty"`
}

type acmeJWS struct {
	Protected string `json:"protected"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// IssueDNS01 obtains one certificate through a DNS-01 challenge. The returned
// chain is only produced after the CA reports the order as valid and the leaf
// parses; no local self-signed fallback exists.
func (c *ACMEClient) IssueDNS01(ctx context.Context, request ACMERequest) (ACMEIssued, error) {
	if c == nil {
		return ACMEIssued{}, ErrACMEConfig
	}
	if err := request.KeyRef.Validate(); err != nil {
		return ACMEIssued{}, fmt.Errorf("%w: key ref", ErrACMEConfig)
	}
	commonName, err := normalizeACMEDomain(request.CommonName)
	if err != nil {
		return ACMEIssued{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	directory, err := c.fetchDirectory(ctx)
	if err != nil {
		return ACMEIssued{}, err
	}
	publicKey, err := c.signer.PublicKey(ctx, c.accountKey)
	if err != nil {
		return ACMEIssued{}, fmt.Errorf("%w: account public key", ErrACMESigner)
	}
	jwk, err := acmeJWKFromKey(publicKey)
	if err != nil {
		return ACMEIssued{}, err
	}
	accountURL, err := c.registerAccount(ctx, directory, jwk)
	if err != nil {
		return ACMEIssued{}, err
	}
	order, orderURL, err := c.newOrder(ctx, directory, accountURL, commonName)
	if err != nil {
		return ACMEIssued{}, err
	}
	thumbprint, err := acmeThumbprint(jwk)
	if err != nil {
		return ACMEIssued{}, err
	}
	var cleanups []func(context.Context) error
	cleanup := func() error {
		var failures []error
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, fn := range cleanups {
			if fn == nil {
				continue
			}
			if err := fn(cleanupCtx); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	for _, authzURL := range order.Authorizations {
		authz, err := c.fetchAuthorization(ctx, accountURL, authzURL)
		if err != nil {
			_ = cleanup()
			return ACMEIssued{}, err
		}
		if authz.Status == "valid" {
			continue
		}
		challenge, ok := findDNS01(authz.Challenges)
		if !ok {
			_ = cleanup()
			return ACMEIssued{}, fmt.Errorf("%w: no dns-01 challenge", ErrACMEChallenge)
		}
		if err := c.authorizeChallenge(ctx, accountURL, authz, challenge, thumbprint, &cleanups); err != nil {
			_ = cleanup()
			return ACMEIssued{}, err
		}
		if err := c.waitAuthorization(ctx, accountURL, authzURL); err != nil {
			_ = cleanup()
			return ACMEIssued{}, err
		}
	}
	csr, err := c.createCSR(ctx, commonName, request.KeyRef)
	if err != nil {
		_ = cleanup()
		return ACMEIssued{}, err
	}
	finalized, err := c.finalize(ctx, accountURL, order.Finalize, csr)
	if err != nil {
		_ = cleanup()
		return ACMEIssued{}, err
	}
	if finalized.Status != "valid" {
		finalized, err = c.waitOrder(ctx, accountURL, orderURL)
		if err != nil {
			_ = cleanup()
			return ACMEIssued{}, err
		}
	}
	if finalized.Certificate == "" {
		_ = cleanup()
		return ACMEIssued{}, fmt.Errorf("%w: order has no certificate url", ErrACMEIssuance)
	}
	chain, err := c.downloadCertificate(ctx, accountURL, finalized.Certificate)
	if err != nil {
		_ = cleanup()
		return ACMEIssued{}, err
	}
	issued, err := parseACMEChain(chain)
	if err != nil {
		_ = cleanup()
		return ACMEIssued{}, err
	}
	if cleanupErr := cleanup(); cleanupErr != nil {
		return issued, fmt.Errorf("%w: dns cleanup: %v", ErrACMEChallenge, cleanupErr)
	}
	return issued, nil
}

func (c *ACMEClient) fetchDirectory(ctx context.Context) (acmeDirectory, error) {
	c.dirMu.Lock()
	defer c.dirMu.Unlock()
	if c.dirFetched {
		return c.directory, nil
	}
	body, status, err := c.doRaw(ctx, http.MethodGet, c.directoryURL, nil)
	if err != nil {
		return acmeDirectory{}, err
	}
	if status < 200 || status > 299 {
		return acmeDirectory{}, fmt.Errorf("%w: directory status %d", ErrACMEProtocol, status)
	}
	var directory acmeDirectory
	if err := json.Unmarshal(body, &directory); err != nil {
		return acmeDirectory{}, fmt.Errorf("%w: directory body", ErrACMEProtocol)
	}
	for _, endpoint := range []string{directory.NewNonce, directory.NewAccount, directory.NewOrder} {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return acmeDirectory{}, fmt.Errorf("%w: endpoint", ErrACMEProtocol)
		}
	}
	c.directory = directory
	c.dirFetched = true
	return directory, nil
}

func (c *ACMEClient) registerAccount(ctx context.Context, directory acmeDirectory, jwk *acmeJWK) (string, error) {
	headers, body, status, err := c.postJWS(ctx, directory.NewAccount, map[string]any{"termsOfServiceAgreed": true}, jwk, "", "")
	if err != nil {
		return "", err
	}
	if status < 200 || status > 299 {
		return "", fmt.Errorf("%w: account status %d", ErrACMEProtocol, status)
	}
	location := headers.Get("Location")
	parsed, err := url.Parse(location)
	if location == "" || err != nil || parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: account location", ErrACMEProtocol)
	}
	_ = body
	return location, nil
}

func (c *ACMEClient) newOrder(ctx context.Context, directory acmeDirectory, accountURL, domain string) (acmeOrder, string, error) {
	headers, body, status, err := c.postJWS(ctx, directory.NewOrder, map[string]any{
		"identifiers": []map[string]string{{"type": "dns", "value": domain}},
	}, nil, accountURL, "")
	if err != nil {
		return acmeOrder{}, "", err
	}
	if status < 200 || status > 299 {
		return acmeOrder{}, "", fmt.Errorf("%w: order status %d", ErrACMEProtocol, status)
	}
	var order acmeOrder
	if err := json.Unmarshal(body, &order); err != nil {
		return acmeOrder{}, "", fmt.Errorf("%w: order body", ErrACMEProtocol)
	}
	orderURL := headers.Get("Location")
	if orderURL == "" || len(order.Authorizations) == 0 || order.Finalize == "" {
		return acmeOrder{}, "", fmt.Errorf("%w: incomplete order", ErrACMEProtocol)
	}
	return order, orderURL, nil
}

func (c *ACMEClient) fetchAuthorization(ctx context.Context, accountURL, authzURL string) (acmeAuthorization, error) {
	_, body, status, err := c.postJWS(ctx, authzURL, []byte{}, nil, accountURL, "")
	if err != nil {
		return acmeAuthorization{}, err
	}
	if status < 200 || status > 299 {
		return acmeAuthorization{}, fmt.Errorf("%w: authorization status %d", ErrACMEProtocol, status)
	}
	var authz acmeAuthorization
	if err := json.Unmarshal(body, &authz); err != nil {
		return acmeAuthorization{}, fmt.Errorf("%w: authorization body", ErrACMEProtocol)
	}
	if authz.Status == "invalid" || authz.Identifier.Value == "" {
		return acmeAuthorization{}, fmt.Errorf("%w: authorization invalid", ErrACMEChallenge)
	}
	return authz, nil
}

func (c *ACMEClient) authorizeChallenge(ctx context.Context, accountURL string, authz acmeAuthorization, challenge acmeChallenge, thumbprint string, cleanups *[]func(context.Context) error) error {
	keyAuth := challenge.Token + "." + thumbprint
	digest := sha256.Sum256([]byte(keyAuth))
	value := base64.RawURLEncoding.EncodeToString(digest[:])
	fqdn := "_acme-challenge." + authz.Identifier.Value
	cleanup, err := c.challenge.Publish(ctx, fqdn, value)
	if err != nil {
		return fmt.Errorf("%w: publish dns-01", ErrACMEChallenge)
	}
	*cleanups = append(*cleanups, cleanup)
	_, _, status, err := c.postJWS(ctx, challenge.URL, map[string]any{}, nil, accountURL, "")
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("%w: challenge status %d", ErrACMEChallenge, status)
	}
	return nil
}

func (c *ACMEClient) waitAuthorization(ctx context.Context, accountURL, authzURL string) error {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		authz, err := c.fetchAuthorization(ctx, accountURL, authzURL)
		if err != nil {
			return err
		}
		switch authz.Status {
		case "valid":
			return nil
		case "invalid", "deactivated", "expired", "revoked":
			return fmt.Errorf("%w: authorization %s", ErrACMEChallenge, authz.Status)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: authorization timeout", ErrACMEChallenge)
		case <-ticker.C:
		}
	}
}

func (c *ACMEClient) finalize(ctx context.Context, accountURL, finalizeURL string, csr []byte) (acmeOrder, error) {
	_, body, status, err := c.postJWS(ctx, finalizeURL, map[string]string{
		"csr": base64.RawURLEncoding.EncodeToString(csr),
	}, nil, accountURL, "")
	if err != nil {
		return acmeOrder{}, err
	}
	if status < 200 || status > 299 {
		return acmeOrder{}, fmt.Errorf("%w: finalize status %d", ErrACMEIssuance, status)
	}
	var order acmeOrder
	if err := json.Unmarshal(body, &order); err != nil {
		return acmeOrder{}, fmt.Errorf("%w: finalize body", ErrACMEIssuance)
	}
	if order.Error != nil {
		return acmeOrder{}, fmt.Errorf("%w: order invalid", ErrACMEIssuance)
	}
	return order, nil
}

func (c *ACMEClient) waitOrder(ctx context.Context, accountURL, orderURL string) (acmeOrder, error) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		_, body, status, err := c.postJWS(ctx, orderURL, []byte{}, nil, accountURL, "")
		if err != nil {
			return acmeOrder{}, err
		}
		if status < 200 || status > 299 {
			return acmeOrder{}, fmt.Errorf("%w: order poll status %d", ErrACMEProtocol, status)
		}
		var order acmeOrder
		if err := json.Unmarshal(body, &order); err != nil {
			return acmeOrder{}, fmt.Errorf("%w: order poll body", ErrACMEProtocol)
		}
		switch order.Status {
		case "valid":
			return order, nil
		case "invalid":
			return acmeOrder{}, fmt.Errorf("%w: order invalid", ErrACMEIssuance)
		}
		select {
		case <-ctx.Done():
			return acmeOrder{}, fmt.Errorf("%w: order timeout", ErrACMEIssuance)
		case <-ticker.C:
		}
	}
}

func (c *ACMEClient) downloadCertificate(ctx context.Context, accountURL, certificateURL string) ([]byte, error) {
	headers, body, status, err := c.postJWS(ctx, certificateURL, []byte{}, nil, accountURL, "")
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("%w: certificate status %d", ErrACMEIssuance, status)
	}
	contentType := headers.Get("Content-Type")
	if !strings.Contains(contentType, "pem-certificate-chain") && !strings.Contains(contentType, "application/pem-certificate-chain") {
		return nil, fmt.Errorf("%w: certificate content type", ErrACMEIssuance)
	}
	return body, nil
}

func (c *ACMEClient) createCSR(ctx context.Context, commonName string, keyRef KeyRef) ([]byte, error) {
	publicKey, err := c.signer.PublicKey(ctx, keyRef)
	if err != nil {
		return nil, fmt.Errorf("%w: certificate public key", ErrACMESigner)
	}
	signer := &acmeCSRSigner{ctx: ctx, engine: c.signer, ref: keyRef, public: publicKey}
	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: commonName},
		DNSNames: []string{commonName},
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, template, signer)
	if err != nil {
		return nil, fmt.Errorf("%w: csr", ErrACMESigner)
	}
	return csr, nil
}

type acmeCSRSigner struct {
	ctx    context.Context
	engine ACMESigner
	ref    KeyRef
	public crypto.PublicKey
}

func (s *acmeCSRSigner) Public() crypto.PublicKey { return s.public }

func (s *acmeCSRSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts.HashFunc() != crypto.SHA256 {
		return nil, ErrACMESigner
	}
	raw, err := s.engine.SignDigest(s.ctx, s.ref, digest)
	if err != nil {
		return nil, fmt.Errorf("%w: certificate key", ErrACMESigner)
	}
	if len(raw) != 64 {
		return nil, fmt.Errorf("%w: certificate signature length", ErrACMESigner)
	}
	value, err := asn1.Marshal(struct {
		R *big.Int
		S *big.Int
	}{R: new(big.Int).SetBytes(raw[:32]), S: new(big.Int).SetBytes(raw[32:])})
	if err != nil {
		return nil, fmt.Errorf("%w: certificate signature", ErrACMESigner)
	}
	return value, nil
}

// postJWS signs one request. newNonce is fetched lazily; a badNonce problem is
// retried exactly once with the nonce from the failed response.
func (c *ACMEClient) postJWS(ctx context.Context, endpoint string, payload any, jwk *acmeJWK, kid, nonce string) (http.Header, []byte, int, error) {
	if nonce == "" {
		fetched, err := c.fetchNonce(ctx)
		if err != nil {
			return nil, nil, 0, err
		}
		nonce = fetched
	}
	for attempt := 0; attempt < 2; attempt++ {
		protected := acmeProtected{Alg: "ES256", Nonce: nonce, URL: endpoint}
		if jwk != nil {
			protected.JWK = jwk
		} else {
			protected.KID = kid
		}
		protectedJSON, err := json.Marshal(protected)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("%w: protected header", ErrACMEProtocol)
		}
		var payloadJSON []byte
		switch value := payload.(type) {
		case []byte:
			payloadJSON = value
		default:
			payloadJSON, err = json.Marshal(value)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("%w: payload", ErrACMEProtocol)
			}
		}
		signingInput := base64.RawURLEncoding.EncodeToString(protectedJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
		digest := sha256.Sum256([]byte(signingInput))
		signature, err := c.signer.SignDigest(ctx, c.accountKey, digest[:])
		if err != nil || len(signature) != 64 {
			return nil, nil, 0, fmt.Errorf("%w: account signature", ErrACMESigner)
		}
		body, err := json.Marshal(acmeJWS{
			Protected: base64.RawURLEncoding.EncodeToString(protectedJSON),
			Payload:   base64.RawURLEncoding.EncodeToString(payloadJSON),
			Signature: base64.RawURLEncoding.EncodeToString(signature),
		})
		if err != nil {
			return nil, nil, 0, fmt.Errorf("%w: jws body", ErrACMEProtocol)
		}
		responseBody, headers, status, callErr := c.doRawHeaders(ctx, http.MethodPost, endpoint, body)
		var problem *acmeProblem
		if errors.As(callErr, &problem) {
			if attempt == 0 && strings.HasSuffix(problem.Type, ":badNonce") {
				nonce = headers.Get("Replay-Nonce")
				if nonce == "" {
					next, err := c.fetchNonce(ctx)
					if err != nil {
						return nil, nil, 0, err
					}
					nonce = next
				}
				continue
			}
			return nil, responseBody, status, callErr
		}
		if callErr != nil {
			return nil, nil, 0, callErr
		}
		if status >= 400 {
			return nil, responseBody, status, fmt.Errorf("%w: status %d", ErrACMEProtocol, status)
		}
		return headers, responseBody, status, nil
	}
	return nil, nil, 0, fmt.Errorf("%w: nonce retry", ErrACMEProtocol)
}

func (c *ACMEClient) fetchNonce(ctx context.Context) (string, error) {
	nonceURL := c.directoryNewNonce(ctx)
	_, headers, status, err := c.doRawHeaders(ctx, http.MethodHead, nonceURL, nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusMethodNotAllowed {
		_, headers, status, err = c.doRawHeaders(ctx, http.MethodGet, nonceURL, nil)
		if err != nil {
			return "", err
		}
	}
	if status < 200 || status > 299 {
		return "", fmt.Errorf("%w: nonce status %d", ErrACMEProtocol, status)
	}
	nonce := headers.Get("Replay-Nonce")
	if nonce == "" || strings.ContainsAny(nonce, " \r\n") {
		return "", fmt.Errorf("%w: missing nonce", ErrACMEProtocol)
	}
	return nonce, nil
}

func (c *ACMEClient) directoryNewNonce(ctx context.Context) string {
	directory, err := c.fetchDirectory(ctx)
	if err != nil {
		return c.directoryURL
	}
	return directory.NewNonce
}

func (c *ACMEClient) doRaw(ctx context.Context, method, endpoint string, body []byte) ([]byte, int, error) {
	responseBody, _, status, err := c.doRawHeaders(ctx, method, endpoint, body)
	return responseBody, status, err
}

func (c *ACMEClient) doRawHeaders(ctx context.Context, method, endpoint string, body []byte) ([]byte, http.Header, int, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, nil, 0, fmt.Errorf("%w: endpoint", ErrACMEProtocol)
	}
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: request", ErrACMEProtocol)
	}
	request.Header.Set("Accept", "application/json, application/problem+json")
	if body != nil {
		request.Header.Set("Content-Type", acmeJWSContentType)
	}
	response, err := c.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		if errors.Is(err, ErrACMERedirect) {
			return nil, nil, 0, ErrACMERedirect
		}
		return nil, nil, 0, fmt.Errorf("%w: transport", ErrACMEProtocol)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxACMEBodyBytes+1))
	if err != nil || len(payload) > maxACMEBodyBytes {
		return nil, nil, 0, fmt.Errorf("%w: response body", ErrACMEProtocol)
	}
	if response.StatusCode >= 400 {
		var problem acmeProblem
		if json.Unmarshal(payload, &problem) == nil && problem.Type != "" {
			problem.Status = response.StatusCode
			return payload, response.Header, response.StatusCode, &problem
		}
	}
	return payload, response.Header, response.StatusCode, nil
}

func findDNS01(challenges []acmeChallenge) (acmeChallenge, bool) {
	for _, challenge := range challenges {
		if challenge.Type == "dns-01" && challenge.URL != "" && challenge.Token != "" {
			return challenge, true
		}
	}
	return acmeChallenge{}, false
}

func acmeJWKFromKey(publicKey crypto.PublicKey) (*acmeJWK, error) {
	key, ok := publicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
		return nil, fmt.Errorf("%w: only ecdsa p-256 is supported", ErrACMESigner)
	}
	return &acmeJWK{
		Crv: "P-256", Kty: "EC",
		X: base64.RawURLEncoding.EncodeToString(pad32(key.X)),
		Y: base64.RawURLEncoding.EncodeToString(pad32(key.Y)),
	}, nil
}

func acmeThumbprint(jwk *acmeJWK) (string, error) {
	canonical, err := json.Marshal(jwk)
	if err != nil {
		return "", fmt.Errorf("%w: jwk", ErrACMEProtocol)
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func pad32(value *big.Int) []byte {
	raw := value.Bytes()
	out := make([]byte, 32)
	copy(out[32-len(raw):], raw)
	return out
}

func parseACMEChain(chain []byte) (ACMEIssued, error) {
	remaining := chain
	var blocks [][]byte
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		blocks = append(blocks, block.Bytes)
	}
	if len(blocks) == 0 {
		return ACMEIssued{}, fmt.Errorf("%w: empty certificate chain", ErrACMEIssuance)
	}
	leaf, err := x509.ParseCertificate(blocks[0])
	if err != nil {
		return ACMEIssued{}, fmt.Errorf("%w: leaf certificate", ErrACMEIssuance)
	}
	if leaf.SerialNumber == nil || leaf.NotAfter.Before(leaf.NotBefore) {
		return ACMEIssued{}, fmt.Errorf("%w: leaf metadata", ErrACMEIssuance)
	}
	var certificatePEM strings.Builder
	_ = pem.Encode(&certificatePEM, &pem.Block{Type: "CERTIFICATE", Bytes: blocks[0]})
	return ACMEIssued{
		CertificatePEM: []byte(certificatePEM.String()), ChainPEM: chain,
		Serial: leaf.SerialNumber.String(), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter,
	}, nil
}

func normalizeACMEDomain(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: common name", ErrACMEConfig)
	}
	domain := strings.ToLower(raw)
	if len(domain) > 253 {
		return "", fmt.Errorf("%w: common name", ErrACMEConfig)
	}
	base := domain
	if strings.HasPrefix(base, "*.") {
		base = base[2:]
	}
	labels := strings.Split(base, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("%w: common name", ErrACMEConfig)
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%w: common name", ErrACMEConfig)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", fmt.Errorf("%w: common name", ErrACMEConfig)
			}
		}
	}
	return domain, nil
}
