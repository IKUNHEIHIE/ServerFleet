// Package localpki provisions a self-contained local Agent CA beside a standalone
// Controller database. It is explicitly local-only: this is not a G2 PKI profile,
// an online Root CA authorization, or a replacement for the pending PKI-OPS decision.
package localpki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

const (
	// CertificateName is the public local CA certificate file.
	CertificateName = "local-agent-ca.pem"
	// KeyName is the private local CA signing key file.
	KeyName = "local-agent-ca-key.pem"
	// Generation is the initial trust generation for issued Agent identities.
	Generation = 1
	// IssuerLifetime bounds each issued Agent certificate.
	IssuerLifetime = 12 * time.Hour
	// CAValidity is the local development CA lifetime.
	CAValidity = 30 * 24 * time.Hour
)

// ErrLocalPKI marks any unusable local PKI state. Partial or corrupted material
// is never regenerated or overwritten automatically.
var ErrLocalPKI = errors.New("localpki: local agent CA is unavailable")

// Material is the loaded local CA. The private key stays inside transport.
type Material struct {
	Provisioned     bool
	IssuerSHA256    string
	IssuerSPKI      string
	TrustGeneration uint64
	AuthorityID     string
	Issuer          *transport.CertificateIssuer
	Roots           *x509.CertPool
	RootPEM         []byte
	RootSPKI        string
	Directory       string
	CertificatePath string
}

// Ensure loads or first-time provisions the local CA in directory.
func Ensure(directory string) (*Material, error) {
	if directory == "" {
		return nil, fmt.Errorf("%w: directory is required", ErrLocalPKI)
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, ErrLocalPKI
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create directory: %v", ErrLocalPKI, err)
	}
	certPath := filepath.Join(abs, CertificateName)
	keyPath := filepath.Join(abs, KeyName)
	certExists := regularFile(certPath)
	keyExists := regularFile(keyPath)
	switch {
	case certExists && keyExists:
		return load(abs, certPath, keyPath)
	case !certExists && !keyExists:
		return generate(abs, certPath, keyPath)
	default:
		return nil, fmt.Errorf("%w: partial state in %s; refusing to regenerate", ErrLocalPKI, abs)
	}
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func load(directory, certPath, keyPath string) (*Material, error) {
	certPEM, err := readBounded(certPath, 64<<10)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: certificate PEM is invalid", ErrLocalPKI)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: certificate is invalid", ErrLocalPKI)
	}
	keyPEM, err := readBounded(keyPath, 64<<10)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("%w: private key permissions are too open", ErrLocalPKI)
		}
	}
	keyBlock, rest := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(keyBlock.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: private key PEM is invalid", ErrLocalPKI)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: private key is invalid", ErrLocalPKI)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: private key must be ECDSA P-256", ErrLocalPKI)
	}
	return build(directory, certPath, cert, key)
}

func build(directory, certPath string, cert *x509.Certificate, key *ecdsa.PrivateKey) (*Material, error) {
	now := time.Now()
	if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 ||
		cert.CheckSignatureFrom(cert) != nil || now.Before(cert.NotBefore) || !now.Add(IssuerLifetime).Before(cert.NotAfter) {
		return nil, fmt.Errorf("%w: certificate is not a usable local CA", ErrLocalPKI)
	}
	public, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil || !bytes.Equal(public, cert.RawSubjectPublicKeyInfo) {
		return nil, fmt.Errorf("%w: private key does not match the CA certificate", ErrLocalPKI)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	spki := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	rootSPKI := hex.EncodeToString(spki[:])
	issuer, err := transport.NewCertificateIssuer(cert.Raw, key, roots, nil, rootSPKI, Generation, IssuerLifetime)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLocalPKI, err)
	}
	return &Material{
		Issuer:          issuer,
		TrustGeneration: Generation,
		Roots:           roots,
		RootPEM:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		RootSPKI:        rootSPKI,
		Directory:       directory,
		CertificatePath: certPath,
	}, nil
}

func generate(directory, certPath, keyPath string) (*Material, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("%w: generate key: %v", ErrLocalPKI, err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Server Fleet Local Agent CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(CAValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("%w: create certificate: %v", ErrLocalPKI, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("%w: parse certificate: %v", ErrLocalPKI, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("%w: encode key: %v", ErrLocalPKI, err)
	}
	defer clear(keyDER)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writeExclusive(keyPath, keyPEM); err != nil {
		return nil, err
	}
	if err := writeExclusive(certPath, certPEM); err != nil {
		return nil, err
	}
	return build(directory, certPath, cert, key)
}

func writeExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: write %s: %v", ErrLocalPKI, filepath.Base(path), err)
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("%w: write %s failed", ErrLocalPKI, filepath.Base(path))
	}
	return nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("%w: %s is unusable", ErrLocalPKI, filepath.Base(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: %s is unreadable", ErrLocalPKI, filepath.Base(path))
	}
	return raw, nil
}

func randomSerial() (*big.Int, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("%w: entropy source failed", ErrLocalPKI)
	}
	raw[0] &= 0x7f
	raw[0] |= 0x40
	return new(big.Int).SetBytes(raw), nil
}
