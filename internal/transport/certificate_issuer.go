package transport

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"time"
)

// CertificateIssuer holds an injected signer, never exports its private key.
// This candidate component does not authorize online Root CA use or install a
// PKI profile. Missing PKI-OPS governance still blocks production activation.
type CertificateIssuer struct {
	certificate *x509.Certificate
	key         crypto.Signer
	chain       [][]byte
	rootPin     string
	generation  uint64
	lifetime    time.Duration
}

func (c *CertificateIssuer) String() string   { return "transport.CertificateIssuer{signer:redacted}" }
func (c *CertificateIssuer) GoString() string { return c.String() }
func NewCertificateIssuer(issuerDER []byte, key crypto.Signer, roots *x509.CertPool, chainDER [][]byte, rootSPKI string, generation uint64, lifetime time.Duration) (*CertificateIssuer, error) {
	if key == nil || roots == nil || generation == 0 || lifetime < 30*time.Minute || lifetime > 24*time.Hour || len(chainDER) > 8 || len(rootSPKI) != 64 {
		return nil, ErrPeerIdentity
	}
	issuer, err := x509.ParseCertificate(bytes.Clone(issuerDER))
	if err != nil || !issuer.IsCA || !issuer.BasicConstraintsValid || issuer.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, ErrPeerIdentity
	}
	pub, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil || !bytes.Equal(pub, issuer.RawSubjectPublicKeyInfo) {
		return nil, ErrPeerIdentity
	}
	intermediate := x509.NewCertPool()
	chain := [][]byte{}
	for _, der := range chainDER {
		cert, err := x509.ParseCertificate(bytes.Clone(der))
		if err != nil {
			return nil, ErrPeerIdentity
		}
		intermediate.AddCert(cert)
		chain = append(chain, bytes.Clone(der))
	}
	paths, err := issuer.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediate, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if err != nil {
		return nil, ErrPeerIdentity
	}
	pinned := false
	for _, path := range paths {
		fp := sha256.Sum256(path[len(path)-1].RawSubjectPublicKeyInfo)
		if hex.EncodeToString(fp[:]) == rootSPKI {
			pinned = true
		}
	}
	if !pinned {
		return nil, ErrPeerIdentity
	}
	return &CertificateIssuer{issuer, key, chain, rootSPKI, generation, lifetime}, nil
}
func parseEnrollmentCSR(raw string) (*x509.CertificateRequest, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return nil, ErrPeerIdentity
	}
	block, rest := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 || len(block.Headers) > 0 {
		return nil, ErrPeerIdentity
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, ErrPeerIdentity
	}
	switch key := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if key.Curve != elliptic.P256() {
			return nil, ErrPeerIdentity
		}
	case ed25519.PublicKey:
		if len(key) != ed25519.PublicKeySize {
			return nil, ErrPeerIdentity
		}
	default:
		return nil, ErrPeerIdentity
	}
	return csr, nil
}

type issuedAgentCertificate struct {
	DER                 []byte
	PEM                 string
	Serial, Fingerprint string
	Expires             time.Time
}

func (c *CertificateIssuer) issue(ctx context.Context, node string, csr *x509.CertificateRequest, now time.Time) (issuedAgentCertificate, error) {
	if c == nil || ctx == nil || ctx.Err() != nil || csr == nil || !validBindingID(node) || len(node) > 128 || now.Before(c.certificate.NotBefore) || !now.Add(c.lifetime).Before(c.certificate.NotAfter) {
		return issuedAgentCertificate{}, ErrPeerIdentity
	}
	serialBytes := make([]byte, 16)
	if _, err := rand.Read(serialBytes); err != nil {
		return issuedAgentCertificate{}, ErrPeerIdentity
	}
	serialBytes[0] &= 0x7f
	serialBytes[0] |= 0x40
	// Subject/SAN/extensions in the CSR are NEVER copied, even if correctly signed.
	template := &x509.Certificate{SerialNumber: new(big.Int).SetBytes(serialBytes), Subject: pkix.Name{CommonName: node}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(c.lifetime), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	if template.NotBefore.Before(c.certificate.NotBefore) {
		template.NotBefore = c.certificate.NotBefore
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.certificate, csr.PublicKey, c.key)
	if err != nil || ctx.Err() != nil {
		return issuedAgentCertificate{}, ErrPeerIdentity
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil || leaf.CheckSignatureFrom(c.certificate) != nil || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) {
		return issuedAgentCertificate{}, ErrPeerIdentity
	}
	fp := sha256.Sum256(der)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.certificate.Raw})...)
	for _, chain := range c.chain {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chain})...)
	}
	return issuedAgentCertificate{der, string(certPEM), leaf.SerialNumber.String(), hex.EncodeToString(fp[:]), leaf.NotAfter}, nil
}
