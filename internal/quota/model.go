package quota

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/notifications"
)

// Source identifies the trusted origin of a traffic observation.
// provider_billing and manual_correction are authoritative and may reduce a
// stored period; agent_estimate must be monotonic.
type Source string

const (
	SourceAgent      Source = "agent_estimate"
	SourceProvider   Source = "provider_billing"
	SourceCorrection Source = "manual_correction"
)

func (s Source) Valid() bool {
	return s == SourceAgent || s == SourceProvider || s == SourceCorrection
}

// allowsDecrease marks authoritative sources that may correct a previously stored
// period downward. Agent estimates remain monotonic.
func (s Source) allowsDecrease() bool {
	return s == SourceProvider || s == SourceCorrection
}

// Policy is the per-node quota configuration. ExpiryAt is optional; a zero value
// means the quota has no expiry reminder. Estimated marks an approximate limit.
type Policy struct {
	NodeID            string
	MonthlyLimitBytes int64
	WarningPercent    int
	ExpiryAt          time.Time
	Estimated         bool
	UpdatedAt         time.Time
}

// PolicyInput is a policy write. Node existence is verified through the nodes
// owner port before persistence.
type PolicyInput struct {
	NodeID            string
	MonthlyLimitBytes int64
	WarningPercent    int
	ExpiryAt          time.Time
	Estimated         bool
}

// Usage is one accumulated traffic period. Bytes are floating point because
// provider billing data can be fractional; downstream exact byte accounting must
// not assume integer precision beyond 2^53.
type Usage struct {
	NodeID      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	BytesIn     float64
	BytesOut    float64
	Source      Source
	UpdatedAt   time.Time
}

// TotalBytes returns the combined ingress and egress value. It is never computed
// from missing data; callers must check GetUsage's found flag first.
func (u Usage) TotalBytes() float64 {
	return u.BytesIn + u.BytesOut
}

// UsageSample is one trusted observation. ObservedAt may be zero, in which case
// the service uses its injected clock. A missing period is never fabricated.
type UsageSample struct {
	NodeID      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	BytesIn     float64
	BytesOut    float64
	Source      Source
	ObservedAt  time.Time
}

// Level classifies a quota evaluation. Exceeded is advisory only and never
// disables a node or blocks networking.
type Level string

const (
	LevelOK       Level = "ok"
	LevelWarning  Level = "warning"
	LevelExceeded Level = "exceeded"
	LevelExpiring Level = "expiring"
)

// Evaluation is the quota view for one usage period. Usage is nil when no sample
// exists, so zero bytes can never be mistaken for missing data.
type Evaluation struct {
	NodeID           string
	PeriodStart      time.Time
	Usage            *Usage
	Policy           *Policy
	TotalBytes       float64
	PercentUsed      float64
	Level            Level
	AlertRecommended bool
	Expiring         bool
	Expired          bool
	ExpiryAt         time.Time
}

// ExpiryEstimate is returned only when enough observations exist to compute a
// positive burn rate. Insufficient data returns found=false instead of an
// invented date.
type ExpiryEstimate struct {
	NodeID      string
	PeriodStart time.Time
	ExhaustedAt time.Time
	BytesPerDay float64
	Samples     int
	From        time.Time
	To          time.Time
	Estimated   bool
}

// Reminder is one notification accepted by the injected Notifier port.
type Reminder struct {
	NodeID      string
	PeriodStart time.Time
	DedupeKey   string
	Threshold   Level
	SentAt      time.Time
}

// Notifier is the notification service port. Quota alerts carry a stable
// node+period+threshold dedupe key.
type Notifier interface {
	SendAlert(ctx context.Context, alert notifications.Alert) (notifications.Delivery, error)
}

const (
	// ExpiryWarningWindow is the look-ahead for expiring quota reminders.
	ExpiryWarningWindow = 7 * 24 * time.Hour

	// MinEstimateSpan and MinEstimateSamples gate burn-rate estimates.
	MinEstimateSpan    = time.Hour
	MinEstimateSamples = 2
)

func validatePolicyInput(input PolicyInput) error {
	if !validToken(input.NodeID, 128) {
		return ErrInvalidPolicy
	}
	if input.MonthlyLimitBytes <= 0 {
		return ErrInvalidPolicy
	}
	if input.WarningPercent < 1 || input.WarningPercent > 100 {
		return ErrInvalidPolicy
	}
	return nil
}

func validateUsageSample(sample UsageSample) error {
	if !validToken(sample.NodeID, 128) {
		return ErrInvalidUsage
	}
	if sample.PeriodStart.IsZero() || sample.PeriodEnd.IsZero() || !sample.PeriodEnd.After(sample.PeriodStart) {
		return ErrInvalidPeriod
	}
	if sample.PeriodEnd.Sub(sample.PeriodStart) > 366*24*time.Hour {
		return ErrInvalidPeriod
	}
	if !finiteNonNegative(sample.BytesIn) || !finiteNonNegative(sample.BytesOut) {
		return ErrInvalidUsage
	}
	if math.IsInf(sample.BytesIn+sample.BytesOut, 0) {
		return ErrInvalidUsage
	}
	if !sample.Source.Valid() {
		return ErrInvalidUsage
	}
	return nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
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
