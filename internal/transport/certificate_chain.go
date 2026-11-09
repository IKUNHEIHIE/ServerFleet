package transport

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"strings"
	"time"
)

const CertificateInstallAction = "certificates.chain.install"

type CertificateInstallRequest struct {
	CSRTaskID   string `json:"csrTaskId"`
	Domain      string `json:"domain"`
	KeyRef      string `json:"keyRef"`
	CSRSHA256   string `json:"csrSha256"`
	ChainPEM    string `json:"certificatePemBase64"`
	ChainSHA256 string `json:"chainSha256"`
}
type CertificateInstallResult struct {
	NodeID      string    `json:"nodeId"`
	Domain      string    `json:"domain"`
	KeyRef      string    `json:"keyRef"`
	CSRSHA256   string    `json:"csrSha256"`
	ChainSHA256 string    `json:"chainSha256"`
	LeafSHA256  string    `json:"leafSha256"`
	NotAfter    time.Time `json:"notAfter"`
}

func canonicalDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
func ParseCertificateInstallRequest(raw []byte) (CertificateInstallRequest, []byte, error) {
	var request CertificateInstallRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > 16<<10 || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validCSRKeyRef(request.CSRTaskID) || !validCSRDomain(request.Domain) || !validCSRKeyRef(request.KeyRef) || !canonicalDigest(request.CSRSHA256) || !canonicalDigest(request.ChainSHA256) {
		return request, nil, ErrPeerIdentity
	}
	var shape map[string]string
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 6 || shape["csrTaskId"] != request.CSRTaskID || shape["domain"] != request.Domain || shape["keyRef"] != request.KeyRef || shape["csrSha256"] != request.CSRSHA256 || shape["certificatePemBase64"] != request.ChainPEM || shape["chainSha256"] != request.ChainSHA256 {
		return request, nil, ErrPeerIdentity
	}
	canonical, _ := json.Marshal(request)
	sorted, _ := json.Marshal(shape)
	if !bytes.Equal(raw, canonical) && !bytes.Equal(raw, sorted) {
		return request, nil, ErrPeerIdentity
	}
	chain, err := base64.StdEncoding.Strict().DecodeString(request.ChainPEM)
	if err != nil || len(chain) == 0 || len(chain) > 8<<10 {
		return request, nil, ErrPeerIdentity
	}
	sum := sha256.Sum256(chain)
	if hex.EncodeToString(sum[:]) != request.ChainSHA256 {
		return request, nil, ErrPeerIdentity
	}
	return request, chain, nil
}

// ParsePublicCertificateChain accepts only a finite canonical certificate PEM
// sequence: no hidden private key, link, extra payload or arbitrarily large CA.
func ParsePublicCertificateChain(raw []byte) ([]*x509.Certificate, error) {
	if len(raw) == 0 || len(raw) > 8<<10 {
		return nil, ErrPeerIdentity
	}
	remaining := raw
	canonical := []byte{}
	certs := []*x509.Certificate{}
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(certs) >= 6 {
			return nil, ErrPeerIdentity
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrPeerIdentity
		}
		certs = append(certs, cert)
		canonical = append(canonical, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
		remaining = rest
	}
	if !bytes.Equal(raw, canonical) {
		return nil, ErrPeerIdentity
	}
	return certs, nil
}
func VerifyNodeCertificateChain(raw, csrDER []byte, domain string, roots *x509.CertPool, now time.Time) (*x509.Certificate, error) {
	certs, err := ParsePublicCertificateChain(raw)
	if err != nil || len(certs) == 0 || roots == nil || now.IsZero() || !validCSRDomain(domain) {
		return nil, ErrPeerIdentity
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	leaf := certs[0]
	if err != nil || csr.CheckSignature() != nil || leaf.IsCA || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, csr.RawSubjectPublicKeyInfo) || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != domain || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, ErrPeerIdentity
	}
	host := domain
	if strings.HasPrefix(host, "*.") {
		host = "probe." + strings.TrimPrefix(host, "*.")
	}
	intermediate := x509.NewCertPool()
	for _, cert := range certs[1:] {
		if !cert.IsCA {
			return nil, ErrPeerIdentity
		}
		intermediate.AddCert(cert)
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediate, DNSName: host, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, ErrPeerIdentity
	}
	return leaf, nil
}
func ValidateCertificateInstallResult(raw []byte, node string, request CertificateInstallRequest) (CertificateInstallResult, error) {
	var result CertificateInstallResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > 2048 || decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF || result.NodeID != node || result.Domain != request.Domain || result.KeyRef != request.KeyRef || result.CSRSHA256 != request.CSRSHA256 || result.ChainSHA256 != request.ChainSHA256 || !canonicalDigest(result.LeafSHA256) || result.NotAfter.IsZero() {
		return result, ErrPeerIdentity
	}
	canonical, _ := json.Marshal(result)
	if !bytes.Equal(raw, canonical) {
		return result, ErrPeerIdentity
	}
	chain, err := base64.StdEncoding.Strict().DecodeString(request.ChainPEM)
	if err != nil {
		return result, ErrPeerIdentity
	}
	certs, err := ParsePublicCertificateChain(chain)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(certs[0].Raw)
	if hex.EncodeToString(sum[:]) != result.LeafSHA256 || !certs[0].NotAfter.Equal(result.NotAfter) {
		return result, ErrPeerIdentity
	}
	return result, nil
}
