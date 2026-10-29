// Package testkit contains explicit test-only adapters, never production wiring.
package testkit

import (
	"context"
	"fmt"

	"serverfleet.local/serverfleet/internal/auth"
)

// ErrDenied stays source-compatible for existing references and also matches
// auth.ErrDenied through errors.Is.
var ErrDenied = fmt.Errorf("test authorization denied: %w", auth.ErrDenied)

// Authorizer denies by default. Tests must opt into Allow explicitly.
type Authorizer struct{ Allow bool }

func (a Authorizer) Authorize(_ context.Context, _ auth.Check) error {
	if !a.Allow {
		return ErrDenied
	}
	return nil
}

var _ auth.Authorizer = Authorizer{}
