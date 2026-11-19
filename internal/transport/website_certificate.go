package transport

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

const WebsiteCertificateInstallAction = "certificates.website.install"
const WebsiteCertificateBundlePath = "/api/v1/agent/website-certificate"
const MaxWebsiteCertificateBundle = 16 << 10

type WebsiteCertificatePlan struct {
	Domain, OrderID, ChainSHA256 string
	Generation                   uint64
}

func (p WebsiteCertificatePlan) Params() map[string]string {
	return map[string]string{"domain": p.Domain, "orderId": p.OrderID, "chainSha256": p.ChainSHA256, "generation": strconv.FormatUint(p.Generation, 10)}
}

func ValidWebsiteDomain(domain string) bool { return validCSRDomain(domain) }

func ParseWebsiteCertificatePlan(raw []byte) (WebsiteCertificatePlan, error) {
	var p WebsiteCertificatePlan
	var fields map[string]string
	if len(raw) > 1024 || json.Unmarshal(raw, &fields) != nil {
		return p, ErrPeerIdentity
	}
	p = WebsiteCertificatePlan{Domain: fields["domain"], OrderID: fields["orderId"], ChainSHA256: fields["chainSha256"]}
	p.Generation, _ = strconv.ParseUint(fields["generation"], 10, 64)
	canonical, _ := json.Marshal(p.Params())
	if !validCSRDomain(p.Domain) || !validBindingID(p.OrderID) || !canonicalDigest(p.ChainSHA256) || p.Generation == 0 || !bytes.Equal(canonical, raw) {
		return p, ErrPeerIdentity
	}
	return p, nil
}

type WebsiteCertificateBundle struct {
	KeyDER   []byte `json:"keyDer"`
	ChainPEM []byte `json:"chainPem"`
}

func VerifyWebsiteCertificate(p WebsiteCertificatePlan, b WebsiteCertificateBundle, roots *x509.CertPool, now time.Time) error {
	sum := sha256.Sum256(b.ChainPEM)
	if hex.EncodeToString(sum[:]) != p.ChainSHA256 || len(b.KeyDER) > 4096 || roots == nil {
		return ErrPeerIdentity
	}
	private, err := x509.ParsePKCS8PrivateKey(b.KeyDER)
	key, ok := private.(*ecdsa.PrivateKey)
	if err != nil || !ok || key.Curve != elliptic.P256() {
		return ErrPeerIdentity
	}
	spki, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return ErrPeerIdentity
	}
	certs, err := ParsePublicCertificateChain(b.ChainPEM)
	if err != nil {
		return err
	}
	_, err = verifyWebsiteChain(certs, spki, p.Domain, roots, now)
	return err
}
