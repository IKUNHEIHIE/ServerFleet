package notifications

import (
	"context"
	"errors"
)

// ErrSecretUnavailable means the provider credential reference could not be
// resolved. Resolver failures are never treated as an accepted delivery.
var ErrSecretUnavailable = errors.New("notifications: provider secret is unavailable")

// SecretUse is the borrow intent. The controller adapter maps Ref to the CX
// credential id/revision/scope and performs before/after checks inside
// WithSecret; this package never holds a plaintext resolver.
type SecretUse struct {
	Ref        string
	Operation  string
	SubjectID  string
	RequestID  string
	PlanDigest string
}

// SecretBorrower lends a credential only inside a callback so provider I/O and
// the post-use authority check happen in the same borrow window.
type SecretBorrower interface {
	WithSecret(ctx context.Context, use SecretUse, fn func(context.Context, string) error) error
}

// SecretBorrowerFunc adapts a function to SecretBorrower.
type SecretBorrowerFunc func(context.Context, SecretUse, func(context.Context, string) error) error

func (f SecretBorrowerFunc) WithSecret(ctx context.Context, use SecretUse, fn func(context.Context, string) error) error {
	return f(ctx, use, fn)
}

type secretScopeKey struct{}

// SecretScope carries the acting subject/request/plan into a borrow intent.
type SecretScope struct {
	SubjectID  string
	RequestID  string
	PlanDigest string
}

func WithSecretScope(ctx context.Context, scope SecretScope) context.Context {
	return context.WithValue(ctx, secretScopeKey{}, scope)
}

func secretScopeFrom(ctx context.Context) SecretScope {
	if ctx == nil {
		return SecretScope{}
	}
	scope, _ := ctx.Value(secretScopeKey{}).(SecretScope)
	return scope
}
