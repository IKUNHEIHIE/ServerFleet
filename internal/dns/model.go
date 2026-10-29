package dns

import "time"

// ProviderCloudflare is the only supported DNS provider.
const ProviderCloudflare = "cloudflare"

// CredentialStatus describes the last verification of an opaque secret-store
// reference. It never carries credential material.
type CredentialStatus string

const (
	CredentialNotConfigured       CredentialStatus = "not_configured"
	CredentialPendingVerification CredentialStatus = "pending_verification"
	CredentialValid               CredentialStatus = "valid"
	CredentialInvalid             CredentialStatus = "invalid"
	CredentialMissingPermissions  CredentialStatus = "missing_permissions"
)

func (s CredentialStatus) valid() bool {
	switch s {
	case CredentialNotConfigured, CredentialPendingVerification, CredentialValid,
		CredentialInvalid, CredentialMissingPermissions:
		return true
	default:
		return false
	}
}

// RecordType is the supported DNS record allowlist.
type RecordType string

const (
	RecordA     RecordType = "A"
	RecordAAAA  RecordType = "AAAA"
	RecordCNAME RecordType = "CNAME"
	RecordTXT   RecordType = "TXT"
	RecordMX    RecordType = "MX"
)

func (t RecordType) valid() bool {
	switch t {
	case RecordA, RecordAAAA, RecordCNAME, RecordTXT, RecordMX:
		return true
	default:
		return false
	}
}

func (t RecordType) proxiable() bool {
	return t == RecordA || t == RecordAAAA || t == RecordCNAME
}

// ZoneStatus mirrors the provider lifecycle of a zone.
type ZoneStatus string

const (
	ZoneActive   ZoneStatus = "active"
	ZonePending  ZoneStatus = "pending"
	ZoneInactive ZoneStatus = "inactive"
	ZoneUnknown  ZoneStatus = "unknown"
)

func normalizeZoneStatus(value string) ZoneStatus {
	switch ZoneStatus(value) {
	case ZoneActive, ZonePending, ZoneInactive:
		return ZoneStatus(value)
	default:
		return ZoneUnknown
	}
}

func (s ZoneStatus) valid() bool {
	switch s {
	case ZoneActive, ZonePending, ZoneInactive, ZoneUnknown:
		return true
	default:
		return false
	}
}

// AccountView is the externally visible account. It deliberately has no
// credential reference field.
type AccountView struct {
	ID               string
	ProviderCode     string
	DisplayName      string
	Note             string
	Enabled          bool
	IsDefault        bool
	CredentialStatus CredentialStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Zone is a provider-synced DNS zone.
type Zone struct {
	ID             string
	AccountID      string
	ProviderZoneID string
	Name           string
	Status         ZoneStatus
	SyncedAt       time.Time
}

// Record is a locally tracked DNS record. ID is the provider record identifier
// returned by CreateRecord.
type Record struct {
	ID         string
	ZoneID     string
	Type       RecordType
	Name       string
	Content    string
	TTL        int
	Proxied    bool
	Priority   int
	ModifiedAt time.Time
}

type accountRecord struct {
	ID               string
	ProviderCode     string
	DisplayName      string
	Note             string
	Enabled          bool
	IsDefault        bool
	CredentialRef    CredentialRef
	CredentialStatus CredentialStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (a accountRecord) view() AccountView {
	return AccountView{
		ID: a.ID, ProviderCode: a.ProviderCode, DisplayName: a.DisplayName, Note: a.Note,
		Enabled: a.Enabled, IsDefault: a.IsDefault, CredentialStatus: a.CredentialStatus,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

// CreateAccountInput creates a Cloudflare account reference. Credential is an
// opaque secret-store reference, never an API token.
type CreateAccountInput struct {
	ID          string
	DisplayName string
	Note        string
	Enabled     bool
	MakeDefault bool
	Credential  CredentialRef
}

// UpdateAccountInput updates local metadata. A non-empty Credential replaces the
// secret-store reference and resets verification to pending.
type UpdateAccountInput struct {
	DisplayName string
	Note        string
	Enabled     bool
	Credential  CredentialRef
}

type CreateZoneInput struct {
	ID             string
	ProviderZoneID string
}

// RecordInput is validated locally and passed to the provider port.
type RecordInput struct {
	Type     RecordType
	Name     string
	Content  string
	TTL      int
	Proxied  bool
	Priority *int
}
