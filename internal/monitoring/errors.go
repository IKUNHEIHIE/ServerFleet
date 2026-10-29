package monitoring

import "errors"

// Exported sentinel errors. Probe and notifier errors are never wrapped with raw
// upstream text; callers receive stable classifications and persisted error text
// is redacted.
var (
	ErrServiceNotConfigured = errors.New("monitoring: service is not configured")
	ErrMissingAuthorizer    = errors.New("monitoring: authorizer is required")
	ErrRequestIDRequired    = errors.New("monitoring: request id is required")
	ErrInvalidMonitor       = errors.New("monitoring: invalid monitor")
	ErrMonitorExists        = errors.New("monitoring: monitor already exists")
	ErrMonitorNotFound      = errors.New("monitoring: monitor not found")
	ErrInvalidTarget        = errors.New("monitoring: invalid target")
	ErrTargetForbidden      = errors.New("monitoring: target is forbidden")
	ErrInvalidAllowedHosts  = errors.New("monitoring: invalid allowed host entry")
	ErrProberUnavailable    = errors.New("monitoring: prober is not configured")
	ErrInvalidProbe         = errors.New("monitoring: invalid probe result")
	ErrInvalidLimit         = errors.New("monitoring: invalid check limit")
	ErrInvalidNow           = errors.New("monitoring: reference time is required")
	ErrNotifyFailed         = errors.New("monitoring: alert delivery failed")
)
