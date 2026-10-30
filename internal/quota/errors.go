package quota

import "errors"

// Exported sentinel errors. Provider/notifier errors are reduced to stable
// classifications; raw upstream text is never wrapped.
var (
	ErrServiceNotConfigured = errors.New("quota: service is not configured")
	ErrMissingAuthorizer    = errors.New("quota: authorizer is required")
	ErrRequestIDRequired    = errors.New("quota: request id is required")
	ErrInvalidPolicy        = errors.New("quota: invalid quota policy")
	ErrPolicyNotFound       = errors.New("quota: quota policy not found")
	ErrUnknownNode          = errors.New("quota: node does not exist")
	ErrInvalidUsage         = errors.New("quota: invalid traffic sample")
	ErrNonMonotonic         = errors.New("quota: traffic sample decreases the stored period")
	ErrInvalidPeriod        = errors.New("quota: invalid traffic period")
	ErrInvalidNow           = errors.New("quota: reference time is required")
	ErrNotifierUnavailable  = errors.New("quota: notifier is not configured")
	ErrNotifyFailed         = errors.New("quota: reminder delivery failed")
	ErrUsageNotFound        = errors.New("quota: usage not found")
)
