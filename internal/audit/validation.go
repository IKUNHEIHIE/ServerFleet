package audit

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidEvent         = errors.New("audit: invalid event")
	ErrInvalidValidator     = errors.New("audit: invalid validator configuration")
	ErrInvalidReferenceTime = errors.New("audit: reference time is required")
)

// Reason classifies a rejected field without echoing its value.
type Reason string

const (
	ReasonEmpty                 Reason = "empty"
	ReasonTooLong               Reason = "too long"
	ReasonInvalidUTF8           Reason = "invalid utf-8"
	ReasonControlCharacter      Reason = "control character"
	ReasonUnsafeCharacter       Reason = "unsafe character"
	ReasonSurroundingWhitespace Reason = "surrounding whitespace"
	ReasonNotAllowed            Reason = "not allowed"
	ReasonZeroTime              Reason = "zero time"
	ReasonInFuture              Reason = "in the future"
	ReasonTooOld                Reason = "too old"
	ReasonDuplicate             Reason = "duplicate"
)

// FieldError identifies a rejected event field. It never contains the field value.
type FieldError struct {
	Field  string
	Reason Reason
}

func (e *FieldError) Error() string {
	return "audit: invalid " + e.Field + ": " + string(e.Reason)
}

func (e *FieldError) Unwrap() error { return ErrInvalidEvent }

// Limits bounds accepted event text and time. Zero MaxPastAge disables the lower time bound.
type Limits struct {
	MaxSubjectIDBytes  int
	MaxActionBytes     int
	MaxResultBytes     int
	MaxRequestIDBytes  int
	MaxTaskIDBytes     int
	MaxNodeIDs         int
	MaxReasonCodeBytes int
	MaxFutureSkew      time.Duration
	MaxPastAge         time.Duration
	RequireSource      bool
}

// DefaultLimits returns local provisional bounds, not a frozen G2 contract.
func DefaultLimits() Limits {
	return Limits{
		MaxSubjectIDBytes:  128,
		MaxActionBytes:     128,
		MaxResultBytes:     32,
		MaxRequestIDBytes:  128,
		MaxTaskIDBytes:     128,
		MaxNodeIDs:         200,
		MaxReasonCodeBytes: 64,
		MaxFutureSkew:      time.Minute,
		MaxPastAge:         0,
	}
}

var resultCodes = map[string]struct{}{
	"allowed": {},
	"denied":  {},
	"success": {},
	"failure": {},
	"partial": {},
}

// AllowedResultCodes returns a sorted copy of the accepted result codes.
func AllowedResultCodes() []string {
	codes := make([]string, 0, len(resultCodes))
	for code := range resultCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// Validator checks event fields against an explicit action allowlist and injected limits.
// It is immutable and safe for concurrent use.
type Validator struct {
	actions map[string]struct{}
	limits  Limits
}

// NewValidator requires a non-empty action allowlist. Callers must supply the trusted
// current action catalog; this package does not define a frozen global action list.
func NewValidator(actions []string, limits Limits) (*Validator, error) {
	if len(actions) == 0 {
		return nil, ErrInvalidValidator
	}
	if limits.MaxSubjectIDBytes <= 0 || limits.MaxActionBytes <= 0 ||
		limits.MaxResultBytes <= 0 || limits.MaxRequestIDBytes <= 0 ||
		limits.MaxTaskIDBytes <= 0 || limits.MaxNodeIDs <= 0 || limits.MaxReasonCodeBytes <= 0 {
		return nil, ErrInvalidValidator
	}
	if limits.MaxFutureSkew < 0 || limits.MaxPastAge < 0 {
		return nil, ErrInvalidValidator
	}
	allowed := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		allowed[action] = struct{}{}
	}
	return &Validator{actions: allowed, limits: limits}, nil
}

// Validate rejects blank, oversized, non-UTF-8, control-character or whitespace-padded
// fields, codes outside the explicit allowlists, and timestamps outside the injected bounds.
// Errors identify the field and reason but never include the offending value.
func (v *Validator) Validate(event Event, now time.Time) error {
	if v == nil {
		return ErrInvalidValidator
	}
	if now.IsZero() {
		return ErrInvalidReferenceTime
	}
	if err := validateText("subjectId", event.SubjectID, v.limits.MaxSubjectIDBytes); err != nil {
		return err
	}
	if err := validateText("action", event.Action, v.limits.MaxActionBytes); err != nil {
		return err
	}
	if _, ok := v.actions[event.Action]; !ok {
		return &FieldError{Field: "action", Reason: ReasonNotAllowed}
	}
	if err := validateText("result", event.Result, v.limits.MaxResultBytes); err != nil {
		return err
	}
	if _, ok := resultCodes[event.Result]; !ok {
		return &FieldError{Field: "result", Reason: ReasonNotAllowed}
	}
	if err := validateText("requestId", event.RequestID, v.limits.MaxRequestIDBytes); err != nil {
		return err
	}
	if event.TaskID != "" {
		if err := validateText("taskId", event.TaskID, v.limits.MaxTaskIDBytes); err != nil {
			return err
		}
	}
	if len(event.NodeIDs) > v.limits.MaxNodeIDs {
		return &FieldError{Field: "nodeIds", Reason: ReasonTooLong}
	}
	seenNodes := make(map[string]struct{}, len(event.NodeIDs))
	for _, nodeID := range event.NodeIDs {
		if err := validateText("nodeIds", nodeID, v.limits.MaxSubjectIDBytes); err != nil {
			return err
		}
		if _, duplicate := seenNodes[nodeID]; duplicate {
			return &FieldError{Field: "nodeIds", Reason: ReasonDuplicate}
		}
		seenNodes[nodeID] = struct{}{}
	}
	if event.Source == "" {
		if v.limits.RequireSource {
			return &FieldError{Field: "source", Reason: ReasonEmpty}
		}
	} else if !knownSource(event.Source) {
		return &FieldError{Field: "source", Reason: ReasonNotAllowed}
	}
	if event.ReasonCode != "" {
		if err := validateReasonCode(event.ReasonCode, v.limits.MaxReasonCodeBytes); err != nil {
			return err
		}
	}
	if event.At.IsZero() {
		return &FieldError{Field: "at", Reason: ReasonZeroTime}
	}
	if event.At.After(now.Add(v.limits.MaxFutureSkew)) {
		return &FieldError{Field: "at", Reason: ReasonInFuture}
	}
	if v.limits.MaxPastAge > 0 && event.At.Before(now.Add(-v.limits.MaxPastAge)) {
		return &FieldError{Field: "at", Reason: ReasonTooOld}
	}
	return nil
}

func validateText(field, value string, maxBytes int) error {
	if value == "" {
		return &FieldError{Field: field, Reason: ReasonEmpty}
	}
	if len(value) > maxBytes {
		return &FieldError{Field: field, Reason: ReasonTooLong}
	}
	if !utf8.ValidString(value) {
		return &FieldError{Field: field, Reason: ReasonInvalidUTF8}
	}
	if reason, unsafe := unsafeRuneReason(value); unsafe {
		return &FieldError{Field: field, Reason: reason}
	}
	if value != strings.TrimSpace(value) {
		return &FieldError{Field: field, Reason: ReasonSurroundingWhitespace}
	}
	return nil
}

func unsafeRuneReason(value string) (Reason, bool) {
	for _, r := range value {
		if unicode.IsControl(r) {
			return ReasonControlCharacter, true
		}
		if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ReasonUnsafeCharacter, true
		}
	}
	return "", false
}

func knownSource(source Source) bool {
	switch source {
	case SourceWeb, SourceAI, SourcePlugin, SourceAgent, SourceSystem:
		return true
	default:
		return false
	}
}

func validateReasonCode(value string, maxBytes int) error {
	if len(value) > maxBytes {
		return &FieldError{Field: "reasonCode", Reason: ReasonTooLong}
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return &FieldError{Field: "reasonCode", Reason: ReasonNotAllowed}
	}
	return nil
}
