package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/url"
	"runtime"

	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
)

type ACMEDeployment struct {
	DirectoryURL          string `json:"directoryUrl"`
	CloudflareURL         string `json:"cloudflareUrl"`
	AccountKeyFile        string `json:"accountKeyFile"`
	DNSCredentialID       string `json:"dnsCredentialId"`
	DNSCredentialRevision uint64 `json:"dnsCredentialRevision"`
	ACMERootFile          string `json:"acmeRootFile,omitempty"`
	DNSRootFile           string `json:"dnsRootFile,omitempty"`
	// These anchors are filled by provisioned bootstrap, never management HTTP.
	ForbiddenKeySPKI []string `json:"-"`
}

func LoadACMEDeployment(filename string) (*ACMEDeployment, error) {
	raw, err := readPrivateDeployment(filename)
	if err != nil {
		return nil, certificates.ErrACMEConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return nil, certificates.ErrACMEConfig
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, certificates.ErrACMEConfig
	}
	var doc struct {
		Version int `json:"version"`
		ACMEDeployment
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || doc.Version != 1 || runtime.GOOS != "linux" || doc.DNSCredentialRevision == 0 || !deploymentID.MatchString(doc.DNSCredentialID) {
		return nil, certificates.ErrACMEConfig
	}
	if doc.DirectoryURL == "" {
		doc.DirectoryURL = certificates.DefaultACMEDirectory
	}
	if doc.CloudflareURL == "" {
		doc.CloudflareURL = dns.DefaultCloudflareBaseURL
	}
	for _, address := range []string{doc.DirectoryURL, doc.CloudflareURL} {
		u, e := url.Parse(address)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || u.ForceQuery {
			return nil, certificates.ErrACMEConfig
		}
	}
	for _, path := range []string{doc.AccountKeyFile, doc.ACMERootFile, doc.DNSRootFile} {
		if path != "" && !validGatewayDeploymentPath(path) {
			return nil, certificates.ErrACMEConfig
		}
	}
	if doc.AccountKeyFile == "" {
		return nil, certificates.ErrACMEConfig
	}
	return &doc.ACMEDeployment, nil
}
func loadACMEAccountKey(filename string) (*ecdsa.PrivateKey, string, string, error) {
	raw, err := readPrivateDeployment(filename)
	if err != nil {
		return nil, "", "", certificates.ErrACMESigner
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, "", "", certificates.ErrACMESigner
	}
	defer clear(block.Bytes)
	private, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", "", certificates.ErrACMESigner
	}
	key, ok := private.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, "", "", certificates.ErrACMESigner
	}
	public, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return nil, "", "", certificates.ErrACMESigner
	}
	pubHash := sha256.Sum256(public)
	fileHash := sha256.Sum256(raw)
	return key, hex.EncodeToString(fileHash[:]), hex.EncodeToString(pubHash[:]), nil
}
