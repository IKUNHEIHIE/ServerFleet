package dns

import "context"

// ConnectionTest is the provider's verification result. Message and token
// material are intentionally absent.
type ConnectionTest struct {
	OK     bool
	Status CredentialStatus
}

// ZoneInfo is one provider zone returned during sync.
type ZoneInfo struct {
	ProviderZoneID string
	Name           string
	Status         string
}

// Provider is the outbound Cloudflare port. Implementations live outside this
// package and are injected; a nil Provider makes every provider operation fail
// closed. Methods receive only opaque credential references.
type Provider interface {
	Test(ctx context.Context, credential CredentialRef) (ConnectionTest, error)
	ListZones(ctx context.Context, credential CredentialRef) ([]ZoneInfo, error)
	CreateRecord(ctx context.Context, credential CredentialRef, zoneProviderID string, input RecordInput) (string, error)
	UpdateRecord(ctx context.Context, credential CredentialRef, zoneProviderID, recordProviderID string, input RecordInput) error
	DeleteRecord(ctx context.Context, credential CredentialRef, zoneProviderID, recordProviderID string) error
}
