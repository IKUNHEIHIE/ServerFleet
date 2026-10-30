package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

var errAgentIdentity = errors.New("agent: local identity unavailable; do not replace or re-enroll automatically")

type agentPreparedIdentity struct {
	Format                                         int
	RequestID, RootSPKI, Version, Platform, CSRPEM string
	CreatedAt                                      time.Time
}
type agentIdentityFacts struct {
	NodeID, Fingerprint, RootSPKI string
	TrustGeneration               uint64
	ExpiresAt                     time.Time
}
type agentIdentityStore struct {
	mu                sync.Mutex
	root              *os.Root
	pin               string
	minimumGeneration uint64
}

func validAgentVersion(version string) bool {
	if len(version) == 0 || len(version) > 128 {
		return false
	}
	for _, c := range version {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '+') {
			return false
		}
	}
	return true
}

func (s *agentIdentityStore) String() string   { return "agentIdentityStore{private-key:redacted}" }
func (s *agentIdentityStore) GoString() string { return s.String() }

func openAgentIdentity(directory, pin string, minimumGeneration uint64) (*agentIdentityStore, error) {
	if len(pin) != 64 {
		return nil, errAgentIdentity
	}
	b, err := hex.DecodeString(pin)
	if !filepath.IsAbs(directory) || err != nil || len(b) != 32 || hex.EncodeToString(b) != pin || minimumGeneration == 0 {
		return nil, errAgentIdentity
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errAgentIdentity
	}
	info, err := r.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = r.Close()
		return nil, errAgentIdentity
	}
	return &agentIdentityStore{root: r, pin: pin, minimumGeneration: minimumGeneration}, nil
}
func (s *agentIdentityStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil
	}
	err := s.root.Close()
	s.root = nil
	return err
}
func (s *agentIdentityStore) sync() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	d, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *agentIdentityStore) write(name string, b []byte) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || s.sync() != nil {
		return errAgentIdentity
	}
	return nil
}
func (s *agentIdentityStore) read(name string, max int64) ([]byte, error) {
	before, err := s.root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > max || runtime.GOOS == "linux" && before.Mode().Perm()&0077 != 0 {
		return nil, errAgentIdentity
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, errAgentIdentity
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() {
		return nil, errAgentIdentity
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, errAgentIdentity
	}
	return b, nil
}
func (s *agentIdentityStore) prepared() (agentPreparedIdentity, []byte, error) {
	var record agentPreparedIdentity
	raw, err := s.read("enrollment.json", 32<<10)
	if err != nil || json.Unmarshal(raw, &record) != nil {
		return record, nil, errAgentIdentity
	}
	canonical, _ := json.Marshal(record)
	if !bytes.Equal(raw, canonical) || record.Format != 1 || !executionIDPattern.MatchString(record.RequestID) || record.RootSPKI != s.pin || !validAgentVersion(record.Version) || record.Platform != runtime.GOOS || record.CreatedAt.IsZero() || time.Now().Before(record.CreatedAt) {
		return record, nil, errAgentIdentity
	}
	keyPEM, err := s.read("identity-key.pem", 16<<10)
	if err != nil {
		return record, nil, errAgentIdentity
	}
	ok := false
	defer func() {
		if !ok {
			clear(keyPEM)
		}
	}()
	block, rest := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 || !bytes.HasPrefix(bytes.TrimSpace(keyPEM), []byte("-----BEGIN PRIVATE KEY-----")) {
		return record, nil, errAgentIdentity
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	clear(block.Bytes)
	key, valid := parsed.(*ecdsa.PrivateKey)
	if err != nil || !valid || key.Curve != elliptic.P256() {
		return record, nil, errAgentIdentity
	}
	block, rest = pem.Decode([]byte(record.CSRPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 || !bytes.HasPrefix(bytes.TrimSpace([]byte(record.CSRPEM)), []byte("-----BEGIN CERTIFICATE REQUEST-----")) {
		return record, nil, errAgentIdentity
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil || len(csr.Subject.Names) != 0 || len(csr.DNSNames) != 0 || len(csr.IPAddresses) != 0 || len(csr.URIs) != 0 || len(csr.EmailAddresses) != 0 || len(csr.Extensions) != 0 {
		return record, nil, errAgentIdentity
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil || !bytes.Equal(csr.RawSubjectPublicKeyInfo, pub) {
		return record, nil, errAgentIdentity
	}
	ok = true
	return record, keyPEM, nil
}

// Prepare is explicit local setup, not implicit recovery from missing/corrupted
// key state. A permanent public intent is written BEFORE the secret file; any
// partial state fails closed and is never overwritten or auto-regenerated.
func (s *agentIdentityStore) Prepare(ctx context.Context, version string) (agentPreparedIdentity, error) {
	var zero agentPreparedIdentity
	if s == nil || ctx == nil || ctx.Err() != nil || !validAgentVersion(version) {
		return zero, errAgentIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return zero, errAgentIdentity
	}
	if _, err := s.root.Lstat("enrollment.json"); !errors.Is(err, os.ErrNotExist) {
		r, key, err := s.prepared()
		clear(key)
		if err != nil || r.Version != version {
			return zero, errAgentIdentity
		}
		return r, nil
	}
	if _, err := s.root.Lstat("identity-key.pem"); !errors.Is(err, os.ErrNotExist) {
		return zero, errAgentIdentity
	}
	if _, err := s.root.Lstat("issued.json"); !errors.Is(err, os.ErrNotExist) {
		return zero, errAgentIdentity
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return zero, errAgentIdentity
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return zero, errAgentIdentity
	}
	defer clear(der)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	defer clear(keyPEM)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return zero, errAgentIdentity
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return zero, errAgentIdentity
	}
	r := agentPreparedIdentity{1, hex.EncodeToString(nonce[:]), s.pin, version, runtime.GOOS, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})), time.Now().UTC()}
	raw, _ := json.Marshal(r)
	if ctx.Err() != nil || s.write("enrollment.json", raw) != nil || s.write("identity-key.pem", keyPEM) != nil || ctx.Err() != nil {
		return zero, errAgentIdentity
	}
	verified, borrowed, err := s.prepared()
	clear(borrowed)
	if err != nil {
		return zero, errAgentIdentity
	}
	return verified, nil
}

// Request includes only CSR/public metadata and a transient one-use token.
// Platform is the actual GOOS; this does not claim Windows is a Linux Agent.
func (s *agentIdentityStore) Request(ctx context.Context, token string) (transport.AgentEnrollmentInput, error) {
	var out transport.AgentEnrollmentInput
	if len(token) != 43 {
		return out, errAgentIdentity
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if s == nil || ctx == nil || ctx.Err() != nil || err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		return out, errAgentIdentity
	}
	defer clear(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return out, errAgentIdentity
	}
	r, key, err := s.prepared()
	clear(key)
	if err != nil {
		return out, errAgentIdentity
	}
	return transport.AgentEnrollmentInput{EnrollmentToken: token, RequestID: r.RequestID, CSRPEM: r.CSRPEM, PinnedRootSPKI: r.RootSPKI, AgentVersion: r.Version, Platform: r.Platform}, nil
}

func (s *agentIdentityStore) validate(result transport.AgentEnrollmentResult, key []byte) (tls.Certificate, agentIdentityFacts, error) {
	var empty tls.Certificate
	var zero agentIdentityFacts
	if result.State != "approved" || !executionIDPattern.MatchString(result.NodeID) || result.TrustGeneration < s.minimumGeneration || len(result.CertificatePEM) == 0 || len(result.CertificatePEM) > 64<<10 {
		return empty, zero, errAgentIdentity
	}
	rest := bytes.TrimSpace([]byte(result.CertificatePEM))
	seen := map[[32]byte]bool{}
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return empty, zero, errAgentIdentity
		}
		digest := sha256.Sum256(block.Bytes)
		if seen[digest] || len(seen) >= 10 {
			return empty, zero, errAgentIdentity
		}
		seen[digest] = true
		rest = bytes.TrimSpace(remaining)
	}
	cert, err := tls.X509KeyPair([]byte(result.CertificatePEM), key)
	if err != nil || len(cert.Certificate) < 2 || len(cert.Certificate) > 10 {
		return empty, zero, errAgentIdentity
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || leaf.IsCA || leaf.Subject.CommonName != result.NodeID || len(leaf.Subject.Names) != 1 || leaf.KeyUsage != x509.KeyUsageDigitalSignature || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth || len(leaf.UnknownExtKeyUsage) != 0 || len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 0 || len(leaf.URIs) != 0 || len(leaf.EmailAddresses) != 0 || !result.ExpiresAt.Equal(leaf.NotAfter) || !time.Now().Add(time.Minute).Before(leaf.NotAfter) || leaf.NotAfter.Sub(leaf.NotBefore) > 24*time.Hour+time.Minute {
		return empty, zero, errAgentIdentity
	}
	root, err := x509.ParseCertificate(cert.Certificate[len(cert.Certificate)-1])
	if err != nil {
		return empty, zero, errAgentIdentity
	}
	pin := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	if hex.EncodeToString(pin[:]) != s.pin || !root.IsCA || root.CheckSignatureFrom(root) != nil {
		return empty, zero, errAgentIdentity
	}
	roots, intermediate := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(root)
	for _, der := range cert.Certificate[1 : len(cert.Certificate)-1] {
		ca, err := x509.ParseCertificate(der)
		if err != nil {
			return empty, zero, errAgentIdentity
		}
		intermediate.AddCert(ca)
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediate, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return empty, zero, errAgentIdentity
	}
	fp := sha256.Sum256(leaf.Raw)
	cert.Leaf = leaf
	return cert, agentIdentityFacts{result.NodeID, hex.EncodeToString(fp[:]), s.pin, result.TrustGeneration, leaf.NotAfter}, nil
}

// Accept authenticates issuance against the independently supplied Root pin and
// local key. A certificate is not a live authorization or node-enable proof.
func (s *agentIdentityStore) Accept(ctx context.Context, result transport.AgentEnrollmentResult) error {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return errAgentIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return errAgentIdentity
	}
	_, key, err := s.prepared()
	if err != nil {
		return errAgentIdentity
	}
	defer clear(key)
	if _, _, err = s.validate(result, key); err != nil {
		return errAgentIdentity
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return errAgentIdentity
	}
	if existing, err := s.read("issued.json", 64<<10); err == nil {
		if bytes.Equal(existing, raw) {
			return nil
		}
		return errAgentIdentity
	}
	if ctx.Err() != nil || s.write("issued.json", raw) != nil {
		return errAgentIdentity
	}
	return nil
}

// WithCertificate lends a locally verified pair only through a mandatory current
// profile/trust/revocation gate. Consumer must not retain it or reenter this store;
// session admission and Controller leaf mapping are still required on EVERY use.
func (s *agentIdentityStore) WithCertificate(ctx context.Context, gate func(context.Context, agentIdentityFacts) error, consume func(context.Context, tls.Certificate) error) (result error) {
	if s == nil || ctx == nil || ctx.Err() != nil || gate == nil || consume == nil {
		return errAgentIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return errAgentIdentity
	}
	defer func() {
		if recover() != nil {
			result = errAgentIdentity
		}
	}()
	_, key, err := s.prepared()
	if err != nil {
		return errAgentIdentity
	}
	defer clear(key)
	raw, err := s.read("issued.json", 64<<10)
	if err != nil {
		return errAgentIdentity
	}
	var issued transport.AgentEnrollmentResult
	if json.Unmarshal(raw, &issued) != nil {
		return errAgentIdentity
	}
	canonical, _ := json.Marshal(issued)
	if !bytes.Equal(raw, canonical) {
		return errAgentIdentity
	}
	cert, facts, err := s.validate(issued, key)
	if err != nil || gate(ctx, facts) != nil || ctx.Err() != nil {
		return errAgentIdentity
	}
	if consume(ctx, cert) != nil || ctx.Err() != nil || gate(ctx, facts) != nil {
		return errAgentIdentity
	}
	return nil
}
