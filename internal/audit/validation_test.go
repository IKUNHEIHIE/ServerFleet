package audit_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
)

var auditNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func validAuditEvent() audit.Event {
	return audit.Event{
		SubjectID: "user-1",
		Action:    "nodes.read",
		Result:    "success",
		RequestID: "req-1",
		At:        auditNow.Add(-time.Minute),
	}
}

func newTestValidator(t *testing.T) *audit.Validator {
	t.Helper()
	validator, err := audit.NewValidator(
		[]string{"nodes.read", "tasks.submit"},
		audit.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("NewValidator returned error: %v", err)
	}
	return validator
}

func TestValidateAcceptsValidEvent(t *testing.T) {
	if err := newTestValidator(t).Validate(validAuditEvent(), auditNow); err != nil {
		t.Fatalf("Validate rejected valid event: %v", err)
	}
}

func TestValidateRejectsInvalidFields(t *testing.T) {
	validator := newTestValidator(t)
	for _, tc := range []struct {
		name       string
		mutate     func(*audit.Event)
		wantField  string
		wantReason audit.Reason
	}{
		{
			name:       "empty subject",
			mutate:     func(e *audit.Event) { e.SubjectID = "" },
			wantField:  "subjectId",
			wantReason: audit.ReasonEmpty,
		},
		{
			name:       "subject too long",
			mutate:     func(e *audit.Event) { e.SubjectID = strings.Repeat("a", 129) },
			wantField:  "subjectId",
			wantReason: audit.ReasonTooLong,
		},
		{
			name:       "subject invalid utf-8",
			mutate:     func(e *audit.Event) { e.SubjectID = string([]byte{0xff}) },
			wantField:  "subjectId",
			wantReason: audit.ReasonInvalidUTF8,
		},
		{
			name:       "subject control character",
			mutate:     func(e *audit.Event) { e.SubjectID = "user\x00" },
			wantField:  "subjectId",
			wantReason: audit.ReasonControlCharacter,
		},
		{
			name:       "subject zero-width space",
			mutate:     func(e *audit.Event) { e.SubjectID = "user-1\u200b" },
			wantField:  "subjectId",
			wantReason: audit.ReasonUnsafeCharacter,
		},
		{
			name:       "subject line separator",
			mutate:     func(e *audit.Event) { e.SubjectID = "user-1\u2028" },
			wantField:  "subjectId",
			wantReason: audit.ReasonUnsafeCharacter,
		},
		{
			name:       "subject surrounding whitespace",
			mutate:     func(e *audit.Event) { e.SubjectID = " user-1 " },
			wantField:  "subjectId",
			wantReason: audit.ReasonSurroundingWhitespace,
		},
		{
			name:       "empty action",
			mutate:     func(e *audit.Event) { e.Action = "" },
			wantField:  "action",
			wantReason: audit.ReasonEmpty,
		},
		{
			name:       "action too long",
			mutate:     func(e *audit.Event) { e.Action = strings.Repeat("a", 129) },
			wantField:  "action",
			wantReason: audit.ReasonTooLong,
		},
		{
			name:       "action control character",
			mutate:     func(e *audit.Event) { e.Action = "nodes.read\x00" },
			wantField:  "action",
			wantReason: audit.ReasonControlCharacter,
		},
		{
			name:       "action right-to-left override",
			mutate:     func(e *audit.Event) { e.Action = "nodes.read\u202e" },
			wantField:  "action",
			wantReason: audit.ReasonUnsafeCharacter,
		},
		{
			name:       "action surrounding whitespace",
			mutate:     func(e *audit.Event) { e.Action = " nodes.read " },
			wantField:  "action",
			wantReason: audit.ReasonSurroundingWhitespace,
		},
		{
			name:       "action not in allowlist",
			mutate:     func(e *audit.Event) { e.Action = "nodes.delete" },
			wantField:  "action",
			wantReason: audit.ReasonNotAllowed,
		},
		{
			name:       "empty result",
			mutate:     func(e *audit.Event) { e.Result = "" },
			wantField:  "result",
			wantReason: audit.ReasonEmpty,
		},
		{
			name:       "result too long",
			mutate:     func(e *audit.Event) { e.Result = strings.Repeat("a", 33) },
			wantField:  "result",
			wantReason: audit.ReasonTooLong,
		},
		{
			name:       "result control character",
			mutate:     func(e *audit.Event) { e.Result = "success\n" },
			wantField:  "result",
			wantReason: audit.ReasonControlCharacter,
		},
		{
			name:       "result not allowed",
			mutate:     func(e *audit.Event) { e.Result = "unknown" },
			wantField:  "result",
			wantReason: audit.ReasonNotAllowed,
		},
		{
			name:       "empty request id",
			mutate:     func(e *audit.Event) { e.RequestID = "" },
			wantField:  "requestId",
			wantReason: audit.ReasonEmpty,
		},
		{
			name:       "request id too long",
			mutate:     func(e *audit.Event) { e.RequestID = strings.Repeat("r", 129) },
			wantField:  "requestId",
			wantReason: audit.ReasonTooLong,
		},
		{
			name:       "request id control character",
			mutate:     func(e *audit.Event) { e.RequestID = "req\x01" },
			wantField:  "requestId",
			wantReason: audit.ReasonControlCharacter,
		},
		{
			name:       "request id paragraph separator",
			mutate:     func(e *audit.Event) { e.RequestID = "req-1\u2029" },
			wantField:  "requestId",
			wantReason: audit.ReasonUnsafeCharacter,
		},
		{
			name:       "zero event time",
			mutate:     func(e *audit.Event) { e.At = time.Time{} },
			wantField:  "at",
			wantReason: audit.ReasonZeroTime,
		},
		{
			name:       "event too far in the future",
			mutate:     func(e *audit.Event) { e.At = auditNow.Add(2 * time.Minute) },
			wantField:  "at",
			wantReason: audit.ReasonInFuture,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := validAuditEvent()
			tc.mutate(&event)
			err := validator.Validate(event, auditNow)
			if !errors.Is(err, audit.ErrInvalidEvent) {
				t.Fatalf("error = %v, want ErrInvalidEvent", err)
			}
			var fieldErr *audit.FieldError
			if !errors.As(err, &fieldErr) {
				t.Fatalf("error = %v, want *FieldError", err)
			}
			if fieldErr.Field != tc.wantField || fieldErr.Reason != tc.wantReason {
				t.Fatalf("field/reason = %q/%q, want %q/%q", fieldErr.Field, fieldErr.Reason, tc.wantField, tc.wantReason)
			}
		})
	}
}

func TestValidateTimeBoundsAreInjectable(t *testing.T) {
	limits := audit.DefaultLimits()
	limits.MaxFutureSkew = 0
	limits.MaxPastAge = time.Hour
	validator, err := audit.NewValidator([]string{"nodes.read"}, limits)
	if err != nil {
		t.Fatal(err)
	}

	event := validAuditEvent()
	event.At = auditNow.Add(-2 * time.Hour)
	var fieldErr *audit.FieldError
	if err := validator.Validate(event, auditNow); !errors.As(err, &fieldErr) || fieldErr.Reason != audit.ReasonTooOld {
		t.Fatalf("too-old error = %v, want ReasonTooOld", err)
	}

	event.At = auditNow.Add(-time.Hour)
	if err := validator.Validate(event, auditNow); err != nil {
		t.Fatalf("boundary old event rejected: %v", err)
	}
	event.At = auditNow
	if err := validator.Validate(event, auditNow); err != nil {
		t.Fatalf("event at reference time rejected: %v", err)
	}
	event.At = auditNow.Add(time.Second)
	if err := validator.Validate(event, auditNow); !errors.As(err, &fieldErr) || fieldErr.Reason != audit.ReasonInFuture {
		t.Fatalf("future error = %v, want ReasonInFuture", err)
	}
}

func TestValidateRequiresReferenceTime(t *testing.T) {
	err := newTestValidator(t).Validate(validAuditEvent(), time.Time{})
	if !errors.Is(err, audit.ErrInvalidReferenceTime) {
		t.Fatalf("error = %v, want ErrInvalidReferenceTime", err)
	}
}

func TestNilValidatorRejects(t *testing.T) {
	var validator *audit.Validator
	if err := validator.Validate(validAuditEvent(), auditNow); !errors.Is(err, audit.ErrInvalidValidator) {
		t.Fatalf("error = %v, want ErrInvalidValidator", err)
	}
}

func TestNewValidatorRejectsInvalidConfiguration(t *testing.T) {
	if _, err := audit.NewValidator(nil, audit.DefaultLimits()); !errors.Is(err, audit.ErrInvalidValidator) {
		t.Fatalf("empty action list error = %v, want ErrInvalidValidator", err)
	}
	for name, mutate := range map[string]func(*audit.Limits){
		"zero subject limit":   func(l *audit.Limits) { l.MaxSubjectIDBytes = 0 },
		"zero action limit":    func(l *audit.Limits) { l.MaxActionBytes = 0 },
		"zero result limit":    func(l *audit.Limits) { l.MaxResultBytes = 0 },
		"zero request limit":   func(l *audit.Limits) { l.MaxRequestIDBytes = 0 },
		"negative future skew": func(l *audit.Limits) { l.MaxFutureSkew = -time.Second },
		"negative past age":    func(l *audit.Limits) { l.MaxPastAge = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			limits := audit.DefaultLimits()
			mutate(&limits)
			if _, err := audit.NewValidator([]string{"nodes.read"}, limits); !errors.Is(err, audit.ErrInvalidValidator) {
				t.Fatalf("error = %v, want ErrInvalidValidator", err)
			}
		})
	}
}

func TestAllowedResultCodes(t *testing.T) {
	want := []string{"allowed", "denied", "failure", "partial", "success"}
	got := audit.AllowedResultCodes()
	if !slices.Equal(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
	validator := newTestValidator(t)
	for _, code := range want {
		event := validAuditEvent()
		event.Result = code
		if err := validator.Validate(event, auditNow); err != nil {
			t.Errorf("result %q rejected: %v", code, err)
		}
	}
	got[0] = "mutated"
	if audit.AllowedResultCodes()[0] != "allowed" {
		t.Fatal("AllowedResultCodes returned an aliased slice")
	}
}

func TestValidationErrorsDoNotExposeValues(t *testing.T) {
	validator := newTestValidator(t)
	secret := "sk-" + strings.Repeat("a", 200)
	event := validAuditEvent()
	event.SubjectID = secret
	err := validator.Validate(event, auditNow)
	if err == nil {
		t.Fatal("oversized secret was accepted")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "sk-") {
		t.Fatalf("error leaked the rejected value: %v", err)
	}

	event = validAuditEvent()
	event.Action = "secret-action-token"
	err = validator.Validate(event, auditNow)
	if err == nil {
		t.Fatal("unknown action was accepted")
	}
	if strings.Contains(err.Error(), "secret-action-token") {
		t.Fatalf("error leaked the rejected action: %v", err)
	}
}
