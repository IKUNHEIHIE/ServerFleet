package localpki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

const (
	ProvisionedRootName     = "root.pem"
	ProvisionedIssuerName   = "issuer.pem"
	ProvisionedKeyName      = "issuer-key.pem"
	ProvisionedManifestName = "pki.json"
)

type provisionedPayload struct {
	Version      int    `json:"version"`
	RootSPKI     string `json:"rootSpkiSha256"`
	IssuerSHA256 string `json:"issuerSha256"`
	Generation   uint64 `json:"trustGeneration"`
	AuthorityID  string `json:"authorityId"`
}
type provisionedDocument struct {
	provisionedPayload
	Signature string `json:"signature"`
}

func openPrivateDirectory(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrLocalPKI
	}
	before, err := os.Lstat(directory)
	if err != nil || !privatePKIInfo(before, true) {
		return nil, ErrLocalPKI
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrLocalPKI
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, ErrLocalPKI
	}
	return root, nil
}
func privatePKIRead(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !privatePKIInfo(info, false) || info.Size() <= 0 || info.Size() > 128<<10 {
		return nil, ErrLocalPKI
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrLocalPKI
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || !privatePKIInfo(after, false) {
		return nil, ErrLocalPKI
	}
	raw, err := io.ReadAll(io.LimitReader(file, (128<<10)+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, ErrLocalPKI
	}
	return raw, nil
}
func privatePKIWrite(root *os.Root, name string, raw []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrLocalPKI
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err != nil || closed != nil {
		return ErrLocalPKI
	}
	return nil
}
func newPrivatePKIDirectory(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrLocalPKI
	}
	parent, err := openPrivateDirectory(filepath.Dir(directory))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = parent.Mkdir(filepath.Base(directory), 0700); err != nil {
		return nil, ErrLocalPKI
	}
	return openPrivateDirectory(directory)
}
func oneCertificate(raw []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrLocalPKI
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, ErrLocalPKI
	}
	return cert, nil
}
func p256Private(raw []byte) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrLocalPKI
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrLocalPKI
	}
	parsed, ok := key.(*ecdsa.PrivateKey)
	if !ok || parsed.Curve != elliptic.P256() {
		return nil, ErrLocalPKI
	}
	return parsed, nil
}
func validProvisionedID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func provisionedDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func readProvisionedDocument(raw []byte) (provisionedDocument, error) {
	var out provisionedDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return out, ErrLocalPKI
	}
	allowed := map[string]bool{"version": true, "rootSpkiSha256": true, "issuerSha256": true, "trustGeneration": true, "authorityId": true, "signature": true}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return out, ErrLocalPKI
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return out, ErrLocalPKI
		}
	}
	if _, err = decoder.Token(); err != nil {
		return out, ErrLocalPKI
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return out, ErrLocalPKI
	}
	if len(seen) != len(allowed) || json.Unmarshal(raw, &out) != nil || out.Version != 1 || out.Generation == 0 || !validProvisionedID(out.AuthorityID) {
		return out, ErrLocalPKI
	}
	return out, nil
}

// LoadProvisioned accepts a public Root and an offline-Root-authorized online
// Intermediate. No Root private key is loaded, generated, exported or accepted.
func LoadProvisioned(directory, expectedRootPin string) (*Material, error) {
	root, err := openPrivateDirectory(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, ErrLocalPKI
	}
	entries, err := dir.ReadDir(9)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 7 {
		return nil, ErrLocalPKI
	}
	allowed := map[string]bool{ProvisionedRootName: true, ProvisionedIssuerName: true, ProvisionedKeyName: true, ProvisionedManifestName: true, "issuer.csr.pem": true, "request.json": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return nil, ErrLocalPKI
		}
	}
	rootPEM, err := privatePKIRead(root, ProvisionedRootName)
	if err != nil {
		return nil, err
	}
	anchor, err := oneCertificate(rootPEM)
	if err != nil || !anchor.IsCA || anchor.CheckSignatureFrom(anchor) != nil {
		return nil, ErrLocalPKI
	}
	pin := provisionedDigest(anchor.RawSubjectPublicKeyInfo)
	if len(expectedRootPin) != 64 || pin != expectedRootPin {
		return nil, ErrLocalPKI
	}
	issuerPEM, err := privatePKIRead(root, ProvisionedIssuerName)
	if err != nil {
		return nil, err
	}
	issuer, err := oneCertificate(issuerPEM)
	if err != nil || bytes.Equal(issuer.RawSubjectPublicKeyInfo, anchor.RawSubjectPublicKeyInfo) || issuer.CheckSignatureFrom(anchor) != nil || !issuer.IsCA || !issuer.MaxPathLenZero {
		return nil, ErrLocalPKI
	}
	raw, err := privatePKIRead(root, ProvisionedManifestName)
	if err != nil {
		return nil, err
	}
	document, err := readProvisionedDocument(raw)
	if err != nil || document.RootSPKI != pin || document.IssuerSHA256 != provisionedDigest(issuer.Raw) {
		return nil, ErrLocalPKI
	}
	payload, _ := json.Marshal(document.provisionedPayload)
	signed := sha256.Sum256(append([]byte("serverfleet.pki.provisioned.v1\x00"), payload...))
	signature, err := base64.StdEncoding.Strict().DecodeString(document.Signature)
	pub, ok := anchor.PublicKey.(*ecdsa.PublicKey)
	if err != nil || !ok || pub.Curve != elliptic.P256() || !ecdsa.VerifyASN1(pub, signed[:], signature) {
		return nil, ErrLocalPKI
	}
	keyBytes, err := privatePKIRead(root, ProvisionedKeyName)
	if err != nil {
		return nil, err
	}
	defer clear(keyBytes)
	key, err := p256Private(keyBytes)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if now.Before(issuer.NotBefore) || !now.Add(IssuerLifetime).Before(issuer.NotAfter) {
		return nil, ErrLocalPKI
	}
	roots := x509.NewCertPool()
	roots.AddCert(anchor)
	signer, err := transport.NewCertificateIssuer(issuer.Raw, key, roots, [][]byte{issuer.Raw}, pin, document.Generation, IssuerLifetime)
	if err != nil {
		return nil, err
	}
	return &Material{Issuer: signer, Roots: roots, RootPEM: bytes.Clone(rootPEM), RootSPKI: pin, Directory: directory, CertificatePath: filepath.Join(directory, ProvisionedRootName), Provisioned: true, TrustGeneration: document.Generation, AuthorityID: document.AuthorityID}, nil
}

// CreateOfflineRoot is an explicit offline-only operator command. The root key
// is written exclusively into the chosen OFFLINE directory, never online PKI.
func CreateOfflineRoot(directory, name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 128 || strings.ContainsAny(name, "\r\n\x00") {
		return "", ErrLocalPKI
	}
	root, err := newPrivatePKIDirectory(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := randomSerial()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 1, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, key.Public(), key)
	if err != nil {
		return "", err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	defer clear(encoded)
	if err = privatePKIWrite(root, "root-key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})); err != nil {
		return "", err
	}
	if err = privatePKIWrite(root, ProvisionedRootName, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return "", err
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return "", err
	}
	return provisionedDigest(parsed.RawSubjectPublicKeyInfo), nil
}
func RequestIntermediate(directory, authorityID string) error {
	if !validProvisionedID(authorityID) {
		return ErrLocalPKI
	}
	root, err := newPrivatePKIDirectory(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	defer clear(raw)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: authorityID}}, key)
	if err != nil {
		return err
	}
	if err = privatePKIWrite(root, ProvisionedKeyName, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})); err != nil {
		return err
	}
	return privatePKIWrite(root, "issuer.csr.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
}
func SignIntermediate(rootDirectory, requestDirectory, outputDirectory, authorityID string, generation uint64) error {
	if !validProvisionedID(authorityID) || generation == 0 || rootDirectory == outputDirectory || rootDirectory == requestDirectory {
		return ErrLocalPKI
	}
	root, err := openPrivateDirectory(rootDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	certPEM, err := privatePKIRead(root, ProvisionedRootName)
	if err != nil {
		return err
	}
	anchor, err := oneCertificate(certPEM)
	if err != nil || !anchor.IsCA || anchor.CheckSignatureFrom(anchor) != nil {
		return ErrLocalPKI
	}
	keyPEM, err := privatePKIRead(root, "root-key.pem")
	if err != nil {
		return err
	}
	defer clear(keyPEM)
	key, err := p256Private(keyPEM)
	if err != nil {
		return err
	}
	public, _ := x509.MarshalPKIXPublicKey(key.Public())
	if !bytes.Equal(public, anchor.RawSubjectPublicKeyInfo) {
		return ErrLocalPKI
	}
	request, err := openPrivateDirectory(requestDirectory)
	if err != nil {
		return err
	}
	defer request.Close()
	raw, err := privatePKIRead(request, "issuer.csr.pem")
	if err != nil {
		return err
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 {
		return ErrLocalPKI
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil || csr.Subject.CommonName != authorityID {
		return ErrLocalPKI
	}
	csrKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || csrKey.Curve != elliptic.P256() {
		return ErrLocalPKI
	}
	now := time.Now().UTC()
	expires := now.AddDate(1, 0, 0)
	if expires.After(anchor.NotAfter.Add(-time.Hour)) {
		expires = anchor.NotAfter.Add(-time.Hour)
	}
	if !expires.After(now.Add(24 * time.Hour)) {
		return ErrLocalPKI
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: authorityID}, NotBefore: now.Add(-5 * time.Minute), NotAfter: expires, IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, anchor, csr.PublicKey, key)
	if err != nil {
		return err
	}
	payload := provisionedPayload{1, provisionedDigest(anchor.RawSubjectPublicKeyInfo), provisionedDigest(der), generation, authorityID}
	canonical, _ := json.Marshal(payload)
	digest := sha256.Sum256(append([]byte("serverfleet.pki.provisioned.v1\x00"), canonical...))
	signature, err := key.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return err
	}
	document, _ := json.Marshal(provisionedDocument{payload, base64.StdEncoding.EncodeToString(signature)})
	output, err := newPrivatePKIDirectory(outputDirectory)
	if err != nil {
		return err
	}
	defer output.Close()
	for _, file := range []struct {
		name string
		raw  []byte
	}{{ProvisionedRootName, certPEM}, {ProvisionedIssuerName, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, {ProvisionedManifestName, document}} {
		if err = privatePKIWrite(output, file.name, file.raw); err != nil {
			return err
		}
	}
	return nil
}
func InstallIntermediate(directory, bundleDirectory, rootPin string) (*Material, error) {
	if directory == bundleDirectory {
		return nil, ErrLocalPKI
	}
	target, err := openPrivateDirectory(directory)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	bundle, err := openPrivateDirectory(bundleDirectory)
	if err != nil {
		return nil, err
	}
	defer bundle.Close()
	// Validate the complete public bundle against the already existing online
	// key in a new private sibling; never replace an active issuer in place.
	for _, name := range []string{ProvisionedRootName, ProvisionedIssuerName, ProvisionedManifestName} {
		if _, err := target.Lstat(name); !os.IsNotExist(err) {
			return nil, ErrLocalPKI
		}
	}
	keyRaw, err := privatePKIRead(target, ProvisionedKeyName)
	if err != nil {
		return nil, err
	}
	defer clear(keyRaw)
	key, err := p256Private(keyRaw)
	if err != nil {
		return nil, err
	}
	issuerRaw, err := privatePKIRead(bundle, ProvisionedIssuerName)
	if err != nil {
		return nil, err
	}
	issuer, err := oneCertificate(issuerRaw)
	if err != nil {
		return nil, err
	}
	public, _ := x509.MarshalPKIXPublicKey(key.Public())
	if !bytes.Equal(public, issuer.RawSubjectPublicKeyInfo) {
		return nil, ErrLocalPKI
	}
	anchorRaw, err := privatePKIRead(bundle, ProvisionedRootName)
	if err != nil {
		return nil, err
	}
	documentRaw, err := privatePKIRead(bundle, ProvisionedManifestName)
	if err != nil {
		return nil, err
	}
	if err = verifyPublicProvisionedBundle(anchorRaw, issuerRaw, documentRaw, rootPin); err != nil {
		return nil, err
	}
	for _, name := range []string{ProvisionedRootName, ProvisionedIssuerName, ProvisionedManifestName} {
		raw, err := privatePKIRead(bundle, name)
		if err != nil {
			return nil, err
		}
		if err = privatePKIWrite(target, name, raw); err != nil {
			return nil, err
		}
	}
	return LoadProvisioned(directory, rootPin)
}

func verifyPublicProvisionedBundle(rootRaw, issuerRaw, documentRaw []byte, pin string) error {
	root, err := oneCertificate(rootRaw)
	if err != nil || root.CheckSignatureFrom(root) != nil || !root.IsCA || provisionedDigest(root.RawSubjectPublicKeyInfo) != pin {
		return ErrLocalPKI
	}
	issuer, err := oneCertificate(issuerRaw)
	if err != nil || issuer.CheckSignatureFrom(root) != nil || !issuer.IsCA || !issuer.MaxPathLenZero || bytes.Equal(issuer.RawSubjectPublicKeyInfo, root.RawSubjectPublicKeyInfo) {
		return ErrLocalPKI
	}
	doc, err := readProvisionedDocument(documentRaw)
	if err != nil || doc.RootSPKI != pin || doc.IssuerSHA256 != provisionedDigest(issuer.Raw) || doc.AuthorityID != issuer.Subject.CommonName {
		return ErrLocalPKI
	}
	raw, _ := json.Marshal(doc.provisionedPayload)
	digest := sha256.Sum256(append([]byte("serverfleet.pki.provisioned.v1\x00"), raw...))
	sig, err := base64.StdEncoding.Strict().DecodeString(doc.Signature)
	key, ok := root.PublicKey.(*ecdsa.PublicKey)
	if err != nil || !ok || key.Curve != elliptic.P256() || !ecdsa.VerifyASN1(key, digest[:], sig) {
		return ErrLocalPKI
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	_, err = issuer.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if err != nil {
		return ErrLocalPKI
	}
	return nil
}
