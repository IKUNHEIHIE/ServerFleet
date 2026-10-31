package auth

import "errors"

var (
	ErrDenied            = errors.New("auth: denied")
	ErrInvalidCheck      = errors.New("auth: invalid check")
	ErrPolicyUnavailable = errors.New("auth: policy unavailable")
)

type PolicyErrorKind string

const (
	Denied            PolicyErrorKind = "DENIED"
	InvalidCheck      PolicyErrorKind = "INVALID_CHECK"
	PolicyUnavailable PolicyErrorKind = "POLICY_UNAVAILABLE"
)

// PolicyError exposes a category and a fixed field label, never request values
// or an upstream error. Use errors.Is for categories and errors.As for detail.
type PolicyError struct {
	Kind  PolicyErrorKind
	Field string
}

func (e *PolicyError) Error() string {
	return "auth: " + string(e.Kind) + " (" + e.Field + ")"
}

func (e *PolicyError) Unwrap() error {
	switch e.Kind {
	case Denied:
		return ErrDenied
	case InvalidCheck:
		return ErrInvalidCheck
	case PolicyUnavailable:
		return ErrPolicyUnavailable
	default:
		return nil
	}
}

func policyError(kind PolicyErrorKind, field string) error {
	return &PolicyError{Kind: kind, Field: field}
}
