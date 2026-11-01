package notifications

import (
	"context"
	"errors"
)

// ErrSecretUnavailable means the provider credential reference could not be
// resolved. Resolver failures are never treated as an accepted delivery.
var ErrSecretUnavailable = errors.New("notifications: provider secret is unavailable")

// SecretResolver resolves an opaque secret-store reference. Implementations must
// not log or persist the returned value; senders keep it only for one request.
type SecretResolver interface {
	Resolve(ctx context.Context, secretRef string) (string, error)
}

// SecretResolverFunc adapts a function to SecretResolver.
type SecretResolverFunc func(context.Context, string) (string, error)

func (f SecretResolverFunc) Resolve(ctx context.Context, secretRef string) (string, error) {
	return f(ctx, secretRef)
}
