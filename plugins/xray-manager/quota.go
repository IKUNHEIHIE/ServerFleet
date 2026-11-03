package xraymanager

import (
	"errors"
	"math"
	"time"
)

// ErrQuotaSpec is returned for malformed quota/process inputs. Callers must not
// translate it into a "quota exhausted" answer: an unreadable policy is not a
// measured overrun.
var ErrQuotaSpec = errors.New("xray-manager: invalid quota specification")

// QuotaDirection fixes which counter a policy consumes. Mixing directions is a
// configuration error instead of silently summing user+inbound totals.
type QuotaDirection string

const (
	QuotaTotal    QuotaDirection = "total"
	QuotaUplink   QuotaDirection = "uplink"
	QuotaDownlink QuotaDirection = "downlink"
)

// QuotaPolicy is the persisted per-user intent. LimitBytes==0 means "no limit"
// and is explicit, never a default that a missing value silently becomes.
type QuotaPolicy struct {
	UserID      string
	InstanceID  string
	LimitBytes  uint64
	Direction   QuotaDirection
	WindowStart time.Time
	// Period is the accounting window; zero means the policy never rolls over.
	Period time.Duration
	// ResetOnWindow is set only when the operator asked for window-based reset.
	ResetOnWindow bool
}

// QuotaUsage is an authenticated observation of the counters. Available==false
// means "no trustworthy read", not "zero bytes used".
type QuotaUsage struct {
	Available bool
	Uplink    uint64
	Downlink  uint64
	At        time.Time
	// SegmentID identifies the observed process generation.
	SegmentID string
}

// QuotaBaseline carries counters that were already consumed before the current
// process segment (for example before a restart or a window rollover). Quota
// accounting must survive process resets; totals do not restart at zero.
type QuotaBaseline struct {
	Uplink   uint64
	Downlink uint64
	// ConsumedBytes is the accounted amount at the start of the current window.
	ConsumedBytes uint64
	SegmentID     string
	WindowStart   time.Time
}

// QuotaState is the evaluated view. Unknown is honest about unreadable sources.
type QuotaState struct {
	Policy         QuotaPolicy
	Baseline       QuotaBaseline
	UsedBytes      uint64
	RemainingBytes uint64
	Exhausted      bool
	Unknown        bool
	Reason         string
	// Warning is set when usage crosses 80% of the limit; it is advisory only.
	Warning bool
	At      time.Time
}

const quotaWarningPercent = 80

func validQuotaPolicy(policy QuotaPolicy) bool {
	if policy.UserID == "" || len(policy.UserID) > 128 || policy.InstanceID == "" || len(policy.InstanceID) > 128 {
		return false
	}
	switch policy.Direction {
	case QuotaTotal, QuotaUplink, QuotaDownlink:
	default:
		return false
	}
	if policy.Period < 0 || (policy.ResetOnWindow && policy.Period <= 0) {
		return false
	}
	return true
}

func quotaCounters(usage QuotaUsage, direction QuotaDirection) (uint64, bool) {
	switch direction {
	case QuotaUplink:
		return usage.Uplink, true
	case QuotaDownlink:
		return usage.Downlink, true
	case QuotaTotal:
		total, ok := addUint64(usage.Uplink, usage.Downlink)
		return total, ok
	default:
		return 0, false
	}
}

func addUint64(a, b uint64) (uint64, bool) {
	if math.MaxUint64-a < b {
		return 0, false
	}
	return a + b, true
}

// EvaluateQuota is pure so persistence can compare-and-store the result.
// Rules:
//   - an unreadable/absent observation yields Unknown, never Exhausted;
//   - usage below the baseline (counter reset without a recorded baseline) is
//     reported as Unknown with reason, not as zero consumption;
//   - a policy without limit never reports Exhausted and keeps Remaining=0
//     meaning "unbounded", never "fully used".
func EvaluateQuota(policy QuotaPolicy, baseline QuotaBaseline, usage QuotaUsage, now time.Time) (QuotaState, error) {
	if !validQuotaPolicy(policy) || now.IsZero() {
		return QuotaState{}, ErrQuotaSpec
	}
	state := QuotaState{Policy: policy, Baseline: baseline, At: now.UTC()}
	if !usage.Available {
		state.Unknown = true
		state.Reason = "quota_source_unavailable"
		return state, nil
	}
	current, ok := quotaCounters(usage, policy.Direction)
	if !ok {
		state.Unknown = true
		state.Reason = "quota_counter_overflow"
		return state, nil
	}
	baselineCounter, ok := quotaCounters(QuotaUsage{Uplink: baseline.Uplink, Downlink: baseline.Downlink}, policy.Direction)
	if !ok {
		state.Unknown = true
		state.Reason = "quota_counter_overflow"
		return state, nil
	}
	if current < baselineCounter {
		// The process counter restarted without a recorded baseline. The real
		// consumption is unknown; reporting "0 used" would let quota be bypassed.
		state.Unknown = true
		state.Reason = "quota_counter_reset_without_baseline"
		return state, nil
	}
	delta := current - baselineCounter
	used, ok := addUint64(delta, baseline.ConsumedBytes)
	if !ok {
		state.Unknown = true
		state.Reason = "quota_counter_overflow"
		return state, nil
	}
	state.UsedBytes = used
	if policy.LimitBytes == 0 {
		state.RemainingBytes = 0
		state.Reason = "quota_unlimited"
		return state, nil
	}
	if used >= policy.LimitBytes {
		state.Exhausted = true
		state.RemainingBytes = 0
		state.Reason = "quota_exhausted"
		return state, nil
	}
	state.RemainingBytes = policy.LimitBytes - used
	state.Warning = used*100 >= policy.LimitBytes*quotaWarningPercent
	return state, nil
}

// ErrProcessResetRefused explains why a restart intent cannot be recorded.
var ErrProcessResetRefused = errors.New("xray-manager: process reset refused")

// ProcessResetRequest is the declarative intent to restart an instance process.
// It carries no command: the controlled host owns the actual process lifecycle.
type ProcessResetRequest struct {
	InstanceID    string
	SegmentID     string
	Generation    uint64
	RequestedAt   time.Time
	Reason        string
	OperatorID    string
	TaskID        string
	SourcePresent bool
}

// ProcessResetOutcome records what the reset does to quota accounting.
type ProcessResetOutcome struct {
	Baseline   QuotaBaseline
	SegmentID  string
	Generation uint64
	// AccountingUnknown marks that pre-reset totals could not be frozen, so
	// quota must be treated as unknown until a fresh authenticated read.
	AccountingUnknown bool
	Reason            string
}

// PlanProcessReset freezes the current counters into the baseline before a
// restart. A reset is refused without an authenticated source, without a new
// generation, or when the operator/task binding is missing: the function never
// invents an observation and never resets quota to zero.
func PlanProcessReset(
	current ProcessResetRequest,
	prior Baseline,
	known QuotaBaseline,
	usage QuotaUsage,
) (ProcessResetOutcome, error) {
	if current.InstanceID == "" || len(current.InstanceID) > 128 {
		return ProcessResetOutcome{}, ErrQuotaSpec
	}
	if current.SegmentID == "" || len(current.SegmentID) > 128 || current.RequestedAt.IsZero() {
		return ProcessResetOutcome{}, ErrQuotaSpec
	}
	if current.OperatorID == "" || current.TaskID == "" {
		return ProcessResetOutcome{}, ErrProcessResetRefused
	}
	if !current.SourcePresent {
		return ProcessResetOutcome{}, ErrProcessResetRefused
	}
	if current.Generation <= prior.Generation {
		// A new process must have a strictly newer generation; otherwise a late
		// duplicate request would silently rebase quota.
		return ProcessResetOutcome{}, ErrProcessResetRefused
	}
	outcome := ProcessResetOutcome{
		Baseline:   known,
		SegmentID:  current.SegmentID,
		Generation: current.Generation,
	}
	outcome.Baseline.SegmentID = current.SegmentID
	if !usage.Available {
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_source_unavailable_before_reset"
		return outcome, nil
	}
	if !known.WindowStart.IsZero() {
		outcome.Baseline.WindowStart = known.WindowStart
	} else if !current.RequestedAt.IsZero() {
		outcome.Baseline.WindowStart = current.RequestedAt.UTC()
	}
	// Freeze everything consumed so far: post-reset counters start at zero, so
	// the baseline must absorb the pre-reset total plus whatever was already
	// accounted inside the current window.
	uplink, okUp := addUint64(known.Uplink, usage.Uplink)
	downlink, okDown := addUint64(known.Downlink, usage.Downlink)
	if !okUp || !okDown {
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_counter_overflow_before_reset"
		return outcome, nil
	}
	outcome.Baseline.Uplink = uplink
	outcome.Baseline.Downlink = downlink
	if known.SegmentID == "" || known.SegmentID != usage.SegmentID {
		// The observation belongs to another segment; it cannot be attributed to
		// the counters we are about to freeze.
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_segment_mismatch_before_reset"
		return outcome, nil
	}
	return outcome, nil
}

// Baseline is the minimal persistent generation marker for an instance.
type Baseline struct {
	Generation uint64
	SegmentID  string
}

// WindowRollover advances the accounting window. It only resets the accounted
// amount, never the underlying interface counters, and refuses to move the
// window backwards so a replay cannot grant a second full quota.
func WindowRollover(policy QuotaPolicy, baseline QuotaBaseline, usage QuotaUsage, now time.Time) (QuotaBaseline, error) {
	if !validQuotaPolicy(policy) || now.IsZero() || !policy.ResetOnWindow {
		return baseline, ErrQuotaSpec
	}
	next := baseline.WindowStart.Add(policy.Period)
	if baseline.WindowStart.IsZero() {
		next = now.UTC()
	}
	if now.Before(next) {
		return baseline, ErrQuotaSpec
	}
	out := baseline
	out.WindowStart = now.UTC()
	if usage.Available {
		uplink, okUp := addUint64(baseline.Uplink, usage.Uplink)
		downlink, okDown := addUint64(baseline.Downlink, usage.Downlink)
		if okUp && okDown {
			out.Uplink, out.Downlink = uplink, downlink
		}
	}
	// ConsumedBytes restarts for the new window; the lifetime counters stay.
	out.ConsumedBytes = 0
	return out, nil
}
