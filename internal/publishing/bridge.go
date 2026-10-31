package publishing

import "context"

// LocalBridgeConfig binds the three stage ports to composition-owned functions.
// Each function is executed exactly as the stage service calls it; a nil function
// fails that stage closed with ErrPublisherUnavailable and is never treated as a
// successful no-op.
type LocalBridgeConfig struct {
	DNSPublish          func(context.Context, DNSPublishRequest) (StageResult, error)
	DNSRollback         func(context.Context, string, StageResult) error
	CertificatePublish  func(context.Context, CertificateRequest) (StageResult, error)
	CertificateRollback func(context.Context, string, StageResult) error
	GatewayPublish      func(context.Context, GatewayRequest) (StageResult, error)
	GatewayRollback     func(context.Context, string, StageResult) error
}

// LocalBridge exposes the three publisher ports from one configuration so the
// Controller composition layer can inject DNS, certificate and gateway adapters
// without adding provider imports to this package. The returned ports are never
// nil; unconfigured stages fail closed.
type LocalBridge struct {
	DNS          DNSPublisher
	Certificates CertificatePublisher
	Gateway      GatewayPublisher
}

// NewLocalBridge returns a bridge whose ports delegate to the configured
// functions. The caller keeps ownership of every provider implementation.
func NewLocalBridge(config LocalBridgeConfig) LocalBridge {
	return LocalBridge{
		DNS: &dnsBridge{
			publish: config.DNSPublish, rollback: config.DNSRollback,
		},
		Certificates: &certificateBridge{
			publish: config.CertificatePublish, rollback: config.CertificateRollback,
		},
		Gateway: &gatewayBridge{
			publish: config.GatewayPublish, rollback: config.GatewayRollback,
		},
	}
}

type dnsBridge struct {
	publish  func(context.Context, DNSPublishRequest) (StageResult, error)
	rollback func(context.Context, string, StageResult) error
}

func (b *dnsBridge) Publish(ctx context.Context, request DNSPublishRequest) (StageResult, error) {
	if b == nil || b.publish == nil {
		return StageResult{}, ErrPublisherUnavailable
	}
	return b.publish(ctx, request)
}

func (b *dnsBridge) Rollback(ctx context.Context, planID string, result StageResult) error {
	if b == nil || b.rollback == nil {
		return ErrPublisherUnavailable
	}
	return b.rollback(ctx, planID, result)
}

type certificateBridge struct {
	publish  func(context.Context, CertificateRequest) (StageResult, error)
	rollback func(context.Context, string, StageResult) error
}

func (b *certificateBridge) Publish(ctx context.Context, request CertificateRequest) (StageResult, error) {
	if b == nil || b.publish == nil {
		return StageResult{}, ErrPublisherUnavailable
	}
	return b.publish(ctx, request)
}

func (b *certificateBridge) Rollback(ctx context.Context, planID string, result StageResult) error {
	if b == nil || b.rollback == nil {
		return ErrPublisherUnavailable
	}
	return b.rollback(ctx, planID, result)
}

type gatewayBridge struct {
	publish  func(context.Context, GatewayRequest) (StageResult, error)
	rollback func(context.Context, string, StageResult) error
}

func (b *gatewayBridge) Publish(ctx context.Context, request GatewayRequest) (StageResult, error) {
	if b == nil || b.publish == nil {
		return StageResult{}, ErrPublisherUnavailable
	}
	return b.publish(ctx, request)
}

func (b *gatewayBridge) Rollback(ctx context.Context, planID string, result StageResult) error {
	if b == nil || b.rollback == nil {
		return ErrPublisherUnavailable
	}
	return b.rollback(ctx, planID, result)
}

var (
	_ DNSPublisher         = (*dnsBridge)(nil)
	_ CertificatePublisher = (*certificateBridge)(nil)
	_ GatewayPublisher     = (*gatewayBridge)(nil)
)
