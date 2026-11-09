package transport

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
)

const CertificateCSRAction = "certificates.csr.create"

type CertificateCSRRequest struct {
	Domain string `json:"domain"`
	KeyRef string `json:"keyRef"`
}
type CertificateCSRResult struct {
	NodeID string `json:"nodeId"`
	Domain string `json:"domain"`
	KeyRef string `json:"keyRef"`
	CSRDER string `json:"csrDerBase64"`
	SHA256 string `json:"sha256"`
}

func validCSRDomain(domain string) bool {
	if len(domain) > 253 || strings.ToLower(domain) != domain {
		return false
	}
	name := strings.TrimPrefix(domain, "*.")
	if !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func ParseCertificateCSRRequest(raw []byte) (CertificateCSRRequest, error) {
	var request CertificateCSRRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > 1024 || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validCSRDomain(request.Domain) || !validCSRKeyRef(request.KeyRef) {
		return request, ErrPeerIdentity
	}
	canonical, _ := json.Marshal(request)
	// Task params are canonical sorted string maps; do not admit duplicate or
	// case-aliased keys while accepting either legitimate object key order.
	var shape map[string]string
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["domain"] != request.Domain || shape["keyRef"] != request.KeyRef {
		return request, ErrPeerIdentity
	}
	sorted, _ := json.Marshal(shape)
	if !bytes.Equal(raw, canonical) && !bytes.Equal(raw, sorted) {
		return request, ErrPeerIdentity
	}
	return request, nil
}
func ValidateCertificateCSRResult(raw []byte, node, domain, keyRef string) (CertificateCSRResult, error) {
	var result CertificateCSRResult
	if len(raw) == 0 || len(raw) > 8<<10 || !validBindingID(node) {
		return result, ErrPeerIdentity
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF || result.NodeID != node || result.Domain != domain || result.KeyRef != keyRef || !validCSRDomain(domain) || !validCSRKeyRef(keyRef) {
		return result, ErrPeerIdentity
	}
	canonical, _ := json.Marshal(result)
	if !bytes.Equal(raw, canonical) {
		return result, ErrPeerIdentity
	}
	der, err := base64.StdEncoding.Strict().DecodeString(result.CSRDER)
	if err != nil || len(der) > 4096 {
		return result, ErrPeerIdentity
	}
	sum := sha256.Sum256(der)
	if hex.EncodeToString(sum[:]) != result.SHA256 {
		return result, ErrPeerIdentity
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil || csr.CheckSignature() != nil || csr.Subject.CommonName != domain || len(csr.DNSNames) != 1 || csr.DNSNames[0] != domain || len(csr.IPAddresses) != 0 || len(csr.EmailAddresses) != 0 || len(csr.URIs) != 0 || csr.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return result, ErrPeerIdentity
	}
	public, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || public.Curve != elliptic.P256() {
		return result, ErrPeerIdentity
	}
	for _, extension := range csr.Extensions {
		if !extension.Id.Equal([]int{2, 5, 29, 17}) {
			return result, ErrPeerIdentity
		}
	}
	return result, nil
}

func validCSRKeyRef(ref string) bool {
	if len(ref) == 0 || len(ref) > 96 {
		return false
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
