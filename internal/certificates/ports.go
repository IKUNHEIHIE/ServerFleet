package certificates

import (
	"context"
	"time"
)

// IssueRequest asks the injected issuer to certify an existing secret-store key
// reference.
type IssueRequest struct {
	CommonName string
	Kind       Kind
	KeyRef     KeyRef
	NotBefore  time.Time
	NotAfter   time.Time
}

// RenewRequest renews a certificate using its key reference.
type RenewRequest struct {
	CertificateID string
	CommonName    string
	IssuerRef     string
	Serial        string
	KeyRef        KeyRef
	NotBefore     time.Time
	NotAfter      time.Time
}

// RevokeRequest revokes an issued certificate.
type RevokeRequest struct {
	CertificateID string
	IssuerRef     string
	Serial        string
	KeyRef        KeyRef
}

// IssuedCertificate is the issuer's metadata response. KeyRef remains an opaque
// reference; no key material is accepted or returned.
type IssuedCertificate struct {
	Serial    string
	IssuerRef string
	KeyRef    KeyRef
	NotBefore time.Time
	NotAfter  time.Time
}

// Issuer is the injected certificate authority port.
type Issuer interface {
	Issue(ctx context.Context, request IssueRequest) (IssuedCertificate, error)
	Renew(ctx context.Context, request RenewRequest) (IssuedCertificate, error)
	Revoke(ctx context.Context, request RevokeRequest) error
}

// DistributionTask is the unified task requested for an internal distribution.
type DistributionTask struct {
	SubjectID      string
	CertificateID  string
	ServiceGroupID string
	NodeIDs        []string
}

// TaskSubmitter creates the unified task that delivers the certificate. A nil
// submitter makes distribution creation fail closed.
type TaskSubmitter interface {
	Submit(ctx context.Context, task DistributionTask) (string, error)
}

// GroupRevisions reads the current service group revision. The certificate
// service never imports service group storage directly.
type GroupRevisions interface {
	Revision(ctx context.Context, serviceGroupID string) (uint64, error)
}
