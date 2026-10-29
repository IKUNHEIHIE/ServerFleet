package certificates

import "time"

// Kind separates internal PKI certificates from public ACME certificates.
type Kind string

const (
	KindInternal Kind = "internal"
	KindACME     Kind = "acme"
)

func (k Kind) valid() bool { return k == KindInternal || k == KindACME }

// CertificateStatus is the stored lifecycle state.
type CertificateStatus string

const (
	StatusPending  CertificateStatus = "pending"
	StatusActive   CertificateStatus = "active"
	StatusRenewing CertificateStatus = "renewing"
	StatusExpired  CertificateStatus = "expired"
	StatusRevoked  CertificateStatus = "revoked"
	StatusFailed   CertificateStatus = "failed"
)

func (s CertificateStatus) valid() bool {
	switch s {
	case StatusPending, StatusActive, StatusRenewing, StatusExpired, StatusRevoked, StatusFailed:
		return true
	default:
		return false
	}
}

// DistributionStatus is the state of one internal PKI distribution row.
type DistributionStatus string

const (
	DistributionPending   DistributionStatus = "pending"
	DistributionSubmitted DistributionStatus = "submitted"
	DistributionActive    DistributionStatus = "active"
	DistributionFailed    DistributionStatus = "failed"
	DistributionRevoked   DistributionStatus = "revoked"
)

func (s DistributionStatus) valid() bool {
	switch s {
	case DistributionPending, DistributionSubmitted, DistributionActive, DistributionFailed, DistributionRevoked:
		return true
	default:
		return false
	}
}

// CertificateView is the externally visible certificate metadata. It has no key
// reference or key material field.
type CertificateView struct {
	ID         string
	Kind       Kind
	CommonName string
	Status     CertificateStatus
	IssuerRef  string
	Serial     string
	NotBefore  time.Time
	NotAfter   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DistributionView is one internal PKI distribution target.
type DistributionView struct {
	ID             string
	CertificateID  string
	ServiceGroupID string
	NodeID         string
	Status         DistributionStatus
	TaskID         string
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CertificateDetail is the authorized read model for one certificate.
type CertificateDetail struct {
	Certificate   CertificateView
	Distributions []DistributionView
}

// DistributionResult is returned after certificate issuance and task creation.
type DistributionResult struct {
	Certificate   CertificateView
	Distributions []DistributionView
	TaskID        string
}

type certificateRecord struct {
	ID         string
	Kind       Kind
	CommonName string
	Status     CertificateStatus
	IssuerRef  string
	KeyRef     KeyRef
	Serial     string
	NotBefore  time.Time
	NotAfter   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (r certificateRecord) view() CertificateView {
	return CertificateView{
		ID: r.ID, Kind: r.Kind, CommonName: r.CommonName, Status: r.Status,
		IssuerRef: r.IssuerRef, Serial: r.Serial, NotBefore: r.NotBefore,
		NotAfter: r.NotAfter, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Validity is the requested certificate lifetime.
type Validity struct {
	NotBefore time.Time
	NotAfter  time.Time
}

// CreateInternalDistributionInput issues an internal certificate and creates one
// distribution per node through a unified task. Confirm must be true because the
// operation changes service impact.
type CreateInternalDistributionInput struct {
	ID                    string
	CommonName            string
	ServiceGroupID        string
	NodeIDs               []string
	ExpectedGroupRevision uint64
	KeyRef                KeyRef
	Validity              Validity
	Confirm               bool
}

// RenewInput renews a certificate with a new validity window.
type RenewInput struct {
	Validity Validity
}

// RotateInput replaces the key reference and reissues the certificate.
type RotateInput struct {
	NewKeyRef KeyRef
	Validity  Validity
}
