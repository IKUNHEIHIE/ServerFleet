package monitoring

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/notifications"
)

// Kind is the probe protocol modeled by this package.
type Kind string

const (
	KindHTTP Kind = "http"
	KindTCP  Kind = "tcp"
	KindTLS  Kind = "tls"
)

func (k Kind) Valid() bool {
	return k == KindHTTP || k == KindTCP || k == KindTLS
}

// State is the persisted monitor state. Unknown means no conclusive check has
// been recorded yet; it is never converted to a fabricated up or down sample.
type State string

const (
	StateUnknown State = "unknown"
	StateUp      State = "up"
	StateDown    State = "down"
)

func (s State) Valid() bool {
	return s == StateUnknown || s == StateUp || s == StateDown
}

// Monitor is the persisted probe definition and last observed state.
type Monitor struct {
	ID                  string
	Name                string
	Kind                Kind
	Target              string
	IntervalSeconds     int
	TimeoutSeconds      int
	Enabled             bool
	MaintenanceUntil    time.Time
	State               State
	ConsecutiveFailures int
	LastCheckedAt       time.Time
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// MonitorInput is a create or full update of mutable monitor fields. Identity,
// target policy and bounds are validated by the service before persistence.
type MonitorInput struct {
	ID               string
	Name             string
	Kind             Kind
	Target           string
	IntervalSeconds  int
	TimeoutSeconds   int
	Enabled          bool
	MaintenanceUntil time.Time
}

// ProbeResult is one provider-agnostic probe answer. Nil StatusCode and Latency
// mean the prober has no such observation; zero is not substituted.
type ProbeResult struct {
	Healthy    bool
	StatusCode *int
	Latency    *time.Duration
	Reason     string
}

// TargetSpec is handed to the injected Prober. Timeout is derived from the stored
// monitor timeout.
type TargetSpec struct {
	Kind    Kind
	Target  string
	Timeout time.Duration
}

// Prober performs one network probe. Implementations are provided by composition;
// nil means the service fails closed and never records a fabricated result.
type Prober interface {
	HTTP(ctx context.Context, target TargetSpec) (ProbeResult, error)
	TCP(ctx context.Context, target TargetSpec) (ProbeResult, error)
	TLS(ctx context.Context, target TargetSpec) (ProbeResult, error)
}

// Notifier is the notification service port used for transition alerts.
type Notifier interface {
	SendAlert(ctx context.Context, alert notifications.Alert) (notifications.Delivery, error)
}

// Check is one persisted probe sample.
type Check struct {
	ID         int64
	MonitorID  string
	Status     State
	Latency    *time.Duration
	StatusCode *int
	Error      string
	CheckedAt  time.Time
}

// RunResult describes one due monitor processed by RunDue. Maintenance reports
// that the check ran with state transitions and alerts suppressed.
type RunResult struct {
	MonitorID   string
	Check       Check
	State       State
	Alerted     bool
	Maintenance bool
}

const (
	DefaultCheckLimit = 50
	MaxCheckLimit     = 500
)

func validateMonitorInput(input MonitorInput, policy TargetPolicy) error {
	if !validToken(input.ID, 128) || strings.ContainsAny(input.ID, "*?[]/\\") {
		return ErrInvalidMonitor
	}
	if !validText(input.Name, 128) {
		return ErrInvalidMonitor
	}
	if !input.Kind.Valid() {
		return ErrInvalidMonitor
	}
	if input.IntervalSeconds < 1 || input.IntervalSeconds > 604800 {
		return ErrInvalidMonitor
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 300 {
		return ErrInvalidMonitor
	}
	if input.Target == "" || len(input.Target) > 2048 {
		return ErrInvalidMonitor
	}
	if err := policy.Validate(input.Kind, input.Target); err != nil {
		return err
	}
	return nil
}

func validateCheckLimit(limit int) (int, error) {
	if limit < 0 || limit > MaxCheckLimit {
		return 0, ErrInvalidLimit
	}
	if limit == 0 {
		return DefaultCheckLimit, nil
	}
	return limit, nil
}

func validateProbeResult(result ProbeResult) error {
	if result.Latency != nil && *result.Latency < 0 {
		return ErrInvalidProbe
	}
	if result.StatusCode != nil && (*result.StatusCode < 100 || *result.StatusCode > 599) {
		return ErrInvalidProbe
	}
	return nil
}

func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	if !validText(value, max) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsSpace)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
