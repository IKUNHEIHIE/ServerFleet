package notifications

import "errors"

// Exported sentinel errors. Provider errors are deliberately never wrapped: a
// sender implementation may surface URLs, credentials or upstream text, so only
// these stable classifications cross the service boundary.
var (
	ErrServiceNotConfigured = errors.New("notifications: service is not configured")
	ErrMissingAuthorizer    = errors.New("notifications: authorizer is required")
	ErrRequestIDRequired    = errors.New("notifications: request id is required")
	ErrInvalidChannel       = errors.New("notifications: invalid channel")
	ErrChannelNotFound      = errors.New("notifications: channel not found")
	ErrChannelNotConfigured = errors.New("notifications: channel is not configured")
	ErrInvalidConfig        = errors.New("notifications: invalid channel configuration")
	ErrInvalidSettings      = errors.New("notifications: invalid settings")
	ErrInvalidSecretRef     = errors.New("notifications: invalid secret reference")
	ErrInvalidMessage       = errors.New("notifications: invalid message")
	ErrInvalidDedupeKey     = errors.New("notifications: invalid dedupe key")
	ErrInvalidQuery         = errors.New("notifications: invalid delivery query")
	ErrDeliveryNotFound     = errors.New("notifications: delivery not found")
	ErrProviderUnavailable  = errors.New("notifications: provider is not configured")
	ErrProviderFailure      = errors.New("notifications: provider failed")
	ErrProviderRejected     = errors.New("notifications: provider rejected the message")
	ErrProviderRateLimited  = errors.New("notifications: provider rate limited the message")
	ErrDeliveryUnknown      = errors.New("notifications: provider delivery outcome is unknown")
	ErrNotRetryable         = errors.New("notifications: delivery is not retryable")
	ErrRetryLimit           = errors.New("notifications: retry limit reached")
	ErrRetryBackoff         = errors.New("notifications: retry backoff has not elapsed")
)
