package localpki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"time"
)

type RecoveryAuthorityPayload struct {
	Version              int       `json:"version"`
	ID                   string    `json:"id"`
	RootSPKI             string    `json:"rootSpkiSha256"`
	CipherSHA256         string    `json:"cipherSha256"`
	ManifestSHA256       string    `json:"manifestSha256"`
	OldAuthorityID       string    `json:"oldAuthorityId"`
	NewAuthorityID       string    `json:"newAuthorityId"`
	IssuerSHA256         string    `json:"issuerSha256"`
	TrustGeneration      uint64    `json:"trustGeneration"`
	RevocationSequence   uint64    `json:"revocationSequence"`
	RevokedSerials       []string  `json:"revokedSerials"`
	RevokedCredentialIDs []string  `json:"revokedCredentialIds"`
	DisabledSubjectIDs   []string  `json:"disabledSubjectIds"`
	IssuedAt             time.Time `json:"issuedAt"`
	ExpiresAt            time.Time `json:"expiresAt"`
}
type recoveryAuthorityDocument struct {
	Payload   RecoveryAuthorityPayload `json:"payload"`
	Signature string                   `json:"signature"`
}
type VerifiedRecoveryAuthority struct {
	payload RecoveryAuthorityPayload
	digest  string
}

func (v *VerifiedRecoveryAuthority) Payload() RecoveryAuthorityPayload {
	if v == nil {
		return RecoveryAuthorityPayload{}
	}
	p := v.payload
	p.RevokedSerials = slices.Clone(p.RevokedSerials)
	p.RevokedCredentialIDs = slices.Clone(p.RevokedCredentialIDs)
	p.DisabledSubjectIDs = slices.Clone(p.DisabledSubjectIDs)
	return p
}
func (v *VerifiedRecoveryAuthority) SHA256() string {
	if v == nil {
		return ""
	}
	return v.digest
}
func (v *VerifiedRecoveryAuthority) Current(now time.Time) error {
	if v == nil || !validRecoveryAuthority(v.payload, now) {
		return ErrLocalPKI
	}
	return nil
}
func validRecoveryAuthority(p RecoveryAuthorityPayload, now time.Time) bool {
	if p.Version != 1 || !validProvisionedID(p.ID) || !validProvisionedID(p.OldAuthorityID) || !validProvisionedID(p.NewAuthorityID) || p.OldAuthorityID == p.NewAuthorityID || p.TrustGeneration == 0 || p.TrustGeneration > 1<<63-1 || p.RevocationSequence >= 1<<63-1 || p.IssuedAt.IsZero() || p.IssuedAt.After(now.Add(5*time.Minute)) || !now.Before(p.ExpiresAt) || !p.ExpiresAt.After(p.IssuedAt) || p.ExpiresAt.Sub(p.IssuedAt) > 24*time.Hour {
		return false
	}
	for _, digest := range []string{p.RootSPKI, p.CipherSHA256, p.ManifestSHA256, p.IssuerSHA256} {
		if len(digest) != 64 || !validProvisionedDigest(digest) {
			return false
		}
	}
	for _, ids := range [][]string{p.RevokedSerials, p.RevokedCredentialIDs, p.DisabledSubjectIDs} {
		if len(ids) > 100000 || !slices.IsSorted(ids) {
			return false
		}
		for i, id := range ids {
			if !validProvisionedID(id) || i > 0 && id == ids[i-1] {
				return false
			}
		}
	}
	return true
}
func validProvisionedDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func decodeRecoveryAuthority(raw []byte) (recoveryAuthorityDocument, error) {
	var d recoveryAuthorityDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return d, ErrLocalPKI
	}
	canonical, err := json.Marshal(d)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return d, ErrLocalPKI
	}
	return d, nil
}
func recoveryAuthorityHash(p RecoveryAuthorityPayload) [32]byte {
	canonical, _ := json.Marshal(p)
	return sha256.Sum256(append([]byte("serverfleet.recovery.authority.v1\x00"), canonical...))
}
func SignRecoveryAuthority(rootDirectory, requestPath, outputPath string) error {
	root, err := openPrivateDirectory(rootDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	anchorRaw, err := privatePKIRead(root, ProvisionedRootName)
	if err != nil {
		return err
	}
	anchor, err := oneCertificate(anchorRaw)
	if err != nil || !anchor.IsCA || anchor.CheckSignatureFrom(anchor) != nil || time.Now().Before(anchor.NotBefore) || !time.Now().Before(anchor.NotAfter) {
		return ErrLocalPKI
	}
	keyRaw, err := privatePKIRead(root, "root-key.pem")
	if err != nil {
		return err
	}
	defer clear(keyRaw)
	key, err := p256Private(keyRaw)
	if err != nil {
		return err
	}
	public, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil || !bytes.Equal(public, anchor.RawSubjectPublicKeyInfo) {
		return ErrLocalPKI
	}
	input, err := openPrivateDirectory(filepath.Dir(requestPath))
	if err != nil {
		return err
	}
	defer input.Close()
	raw, err := privatePKIRead(input, filepath.Base(requestPath))
	if err != nil {
		return err
	}
	var payload RecoveryAuthorityPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF || payload.RootSPKI != provisionedDigest(anchor.RawSubjectPublicKeyInfo) || !validRecoveryAuthority(payload, time.Now()) {
		return ErrLocalPKI
	}
	digest := recoveryAuthorityHash(payload)
	signature, err := key.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return err
	}
	output, err := openPrivateDirectory(filepath.Dir(outputPath))
	if err != nil {
		return err
	}
	defer output.Close()
	document, err := json.Marshal(recoveryAuthorityDocument{payload, base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		return err
	}
	return privatePKIWrite(output, filepath.Base(outputPath), document)
}
func VerifyRecoveryAuthority(rootCertificatePath, documentPath, expectedRootPin string) (*VerifiedRecoveryAuthority, error) {
	return VerifyRecoveryAuthorityAt(rootCertificatePath, documentPath, expectedRootPin, time.Now())
}

// At is for rechecking an already audited activation, using its immutable
// activation time. Initial emergency authority must always use current time.
func VerifyRecoveryAuthorityAt(rootCertificatePath, documentPath, expectedRootPin string, at time.Time) (*VerifiedRecoveryAuthority, error) {
	dir, err := openPrivateDirectory(filepath.Dir(documentPath))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	raw, err := privatePKIRead(dir, filepath.Base(documentPath))
	if err != nil {
		return nil, err
	}
	return verifyRecoveryAuthorityDocumentAt(rootCertificatePath, raw, expectedRootPin, at)
}

// VerifyRecoveryAuthorityDocument authenticates a bounded incoming document
// before the independent parent persists it in its immutable receipt cache.
func VerifyRecoveryAuthorityDocument(rootCertificatePath string, document []byte, expectedRootPin string) (*VerifiedRecoveryAuthority, error) {
	return verifyRecoveryAuthorityDocumentAt(rootCertificatePath, document, expectedRootPin, time.Now())
}

func verifyRecoveryAuthorityDocumentAt(rootCertificatePath string, document []byte, expectedRootPin string, at time.Time) (*VerifiedRecoveryAuthority, error) {
	if len(document) == 0 || len(document) > 128<<10 {
		return nil, ErrLocalPKI
	}
	root, err := openPrivateDirectory(filepath.Dir(rootCertificatePath))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := privatePKIRead(root, filepath.Base(rootCertificatePath))
	if err != nil {
		return nil, err
	}
	anchor, err := oneCertificate(raw)
	if err != nil || !anchor.IsCA || anchor.CheckSignatureFrom(anchor) != nil || provisionedDigest(anchor.RawSubjectPublicKeyInfo) != expectedRootPin || at.Before(anchor.NotBefore) || !at.Before(anchor.NotAfter) {
		return nil, ErrLocalPKI
	}
	d, err := decodeRecoveryAuthority(document)
	if err != nil || d.Payload.RootSPKI != expectedRootPin || !validRecoveryAuthority(d.Payload, at) {
		return nil, ErrLocalPKI
	}
	pub, ok := anchor.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, ErrLocalPKI
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(d.Signature)
	digest := recoveryAuthorityHash(d.Payload)
	if err != nil || !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return nil, ErrLocalPKI
	}
	return &VerifiedRecoveryAuthority{payload: d.Payload, digest: provisionedDigest(document)}, nil
}
