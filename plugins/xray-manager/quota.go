package xraymanager

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
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

// QuotaBaseline is the persisted anchor one quota evaluation is measured from.
// The three concepts must stay distinct, because mixing them silently forgives
// (or double-charges) traffic:
//
//	Uplink/Downlink  counter value when the **current process segment** started
//	                 ("segment offset"). Process counters are absolute since
//	                 process start, so segment usage is current - offset.
//	ConsumedBytes    bytes already charged that **no current counter can
//	                 reproduce** (earlier segments before a reset, earlier
//	                 windows). It is settled usage, never a counter value.
//	WindowStart      the start of the accounting window.
//
// UsedBytes = (current - segmentOffset) + ConsumedBytes (per policy direction).
// A reset makes the new segment start at counter zero, so it must move the
// segment usage into ConsumedBytes and zero the offsets — not add absolute
// counters into the offset fields.
type QuotaBaseline struct {
	Uplink   uint64
	Downlink uint64
	// ConsumedBytes is settled usage carried into this segment/window.
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
	if baseline.SegmentID != "" && usage.SegmentID != baseline.SegmentID {
		// The observed counters belong to another process segment than the one the
		// baseline was frozen from; subtracting them would forgive real usage.
		state.Unknown = true
		state.Reason = "quota_segment_mismatch"
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
	state.Warning = quotaWarningReached(used, policy.LimitBytes)
	return state, nil
}

// quotaWarningReached compares used/limit against the warning percentage in 128
// bits, so a huge limit cannot overflow the multiplication into a false warning.
func quotaWarningReached(used, limit uint64) bool {
	hiUsed, loUsed := bits.Mul64(used, 100)
	hiLimit, loLimit := bits.Mul64(limit, quotaWarningPercent)
	if hiUsed != hiLimit {
		return hiUsed > hiLimit
	}
	return loUsed >= loLimit
}

// segmentConsumption returns how many bytes the policy direction consumed
// inside the observed process segment, refusing (ok=false) when the counters
// went backwards: that means the observation is not from this baseline.
func segmentConsumption(baseline QuotaBaseline, usage QuotaUsage, direction QuotaDirection) (uint64, bool) {
	if usage.Uplink < baseline.Uplink || usage.Downlink < baseline.Downlink {
		return 0, false
	}
	up := usage.Uplink - baseline.Uplink
	down := usage.Downlink - baseline.Downlink
	switch direction {
	case QuotaUplink:
		return up, true
	case QuotaDownlink:
		return down, true
	case QuotaTotal:
		return addUint64(up, down)
	default:
		return 0, false
	}
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
	// Direction names the counter this baseline charges. It is required: freezing
	// quota without knowing which counter applies would have to guess between
	// uplink/downlink/total, and a guessed freeze silently loses accounting.
	Direction QuotaDirection
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
	switch current.Direction {
	case QuotaTotal, QuotaUplink, QuotaDownlink:
	default:
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
		SegmentID:  current.SegmentID,
		Generation: current.Generation,
		// Anything below may leave this baseline untouched: a refused freeze must
		// never look like a frozen (zeroed) one.
		Baseline: known,
	}
	if !usage.Available {
		// Without an authenticated read the pre-reset total is unknown. Keeping the
		// known baseline is the only honest answer; zeroing it would forgive usage.
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_source_unavailable_before_reset"
		return outcome, nil
	}
	if known.SegmentID == "" || known.SegmentID != usage.SegmentID {
		// The observation belongs to another segment; it cannot be attributed to
		// the counters we are about to freeze.
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_segment_mismatch_before_reset"
		return outcome, nil
	}
	consumedInSegment, ok := segmentConsumption(known, usage, current.Direction)
	if !ok {
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_counter_reset_before_reset"
		return outcome, nil
	}
	settled, ok := addUint64(known.ConsumedBytes, consumedInSegment)
	if !ok {
		outcome.AccountingUnknown = true
		outcome.Reason = "quota_counter_overflow_before_reset"
		return outcome, nil
	}
	windowStart := known.WindowStart
	if windowStart.IsZero() {
		windowStart = current.RequestedAt.UTC()
	}
	// The new process counts from zero, so the settled amount moves into
	// ConsumedBytes and the offsets restart at zero. Adding absolute counters into
	// the offsets (the previous behaviour) made the next EvaluateQuota subtract
	// them again and report Unknown/0 instead of the carried total.
	outcome.Baseline = QuotaBaseline{
		Uplink:        0,
		Downlink:      0,
		ConsumedBytes: settled,
		SegmentID:     current.SegmentID,
		WindowStart:   windowStart,
	}
	return outcome, nil
}

// Baseline is the minimal persistent generation marker for an instance.
type Baseline struct {
	Generation uint64
	SegmentID  string
}

// WindowRollover advances the accounting window. The new window is anchored on
// the **current absolute counters**, so the next evaluation charges only bytes
// after this instant; it never accumulates offsets. It refuses to roll over
// without an authenticated read (zeroing accounting would forgive usage) and
// refuses to move the window backwards so a replay cannot grant a second quota.
func WindowRollover(policy QuotaPolicy, baseline QuotaBaseline, usage QuotaUsage, now time.Time) (QuotaBaseline, error) {
	if !validQuotaPolicy(policy) || now.IsZero() || !policy.ResetOnWindow {
		return baseline, ErrQuotaSpec
	}
	next := baseline.WindowStart.Add(policy.Period)
	if baseline.WindowStart.IsZero() {
		next = now.UTC()
	}
	if now.Before(next) {
		return baseline, fmt.Errorf("%w: quota_window_not_elapsed", ErrQuotaSpec)
	}
	if !usage.Available {
		return baseline, fmt.Errorf("%w: quota_source_unavailable_for_rollover", ErrQuotaSpec)
	}
	if baseline.SegmentID != "" && usage.SegmentID != baseline.SegmentID {
		return baseline, fmt.Errorf("%w: quota_segment_mismatch_for_rollover", ErrQuotaSpec)
	}
	return QuotaBaseline{
		Uplink:        usage.Uplink,
		Downlink:      usage.Downlink,
		ConsumedBytes: 0,
		SegmentID:     baseline.SegmentID,
		WindowStart:   now.UTC(),
	}, nil
}
