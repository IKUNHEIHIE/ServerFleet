package publishing

import "context"

// DNSPublishRequest is the complete input for the DNS stage.
type DNSPublishRequest struct {
	PlanID                string
	Domain                string
	ServiceGroupID        string
	ExpectedGroupRevision uint64
}

// CertificateRequest is the complete input for the certificate stage.
type CertificateRequest struct {
	PlanID                string
	Domain                string
	ServiceGroupID        string
	ExpectedGroupRevision uint64
}

// GatewayRequest is the complete input for the gateway stage.
type GatewayRequest struct {
	PlanID                string
	Domain                string
	ServiceGroupID        string
	ExpectedGroupRevision uint64
}

// StageResult is a validated provider answer. Only Summary is persisted, and it
// is redacted before storage. Rollback receives the persisted summary and must
// rely on its own durable provider-side state keyed by plan id; an empty Summary
// is never treated as success.
type StageResult struct {
	Summary string
}

// DNSPublisher creates and compensates DNS records.
type DNSPublisher interface {
	Publish(ctx context.Context, request DNSPublishRequest) (StageResult, error)
	Rollback(ctx context.Context, planID string, result StageResult) error
}

// CertificatePublisher issues and compensates a certificate.
type CertificatePublisher interface {
	Publish(ctx context.Context, request CertificateRequest) (StageResult, error)
	Rollback(ctx context.Context, planID string, result StageResult) error
}

// GatewayPublisher installs and compensates a gateway route/site.
type GatewayPublisher interface {
	Publish(ctx context.Context, request GatewayRequest) (StageResult, error)
	Rollback(ctx context.Context, planID string, result StageResult) error
}

// GroupRef is the owner-provided service group snapshot used for authorization
// and revision validation.
type GroupRef struct {
	ID       string
	Revision uint64
	NodeIDs  []string
}

// GroupLookup reads service group existence, current revision and member nodes
// without importing service group storage.
type GroupLookup interface {
	LookupGroup(ctx context.Context, serviceGroupID string) (GroupRef, bool, error)
}
