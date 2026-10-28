// Package testkit contains explicit test-only adapters, never production wiring.
package testkit

import (
	"context"
	"errors"

	"serverfleet.local/serverfleet/internal/auth"
)

var ErrDenied = errors.New("test authorization denied")

// Authorizer denies by default. Tests must opt into Allow explicitly.
type Authorizer struct{ Allow bool }

func (a Authorizer) Authorize(_ context.Context, _ auth.Check) error {
	if !a.Allow {
		return ErrDenied
	}
	return nil
}

var _ auth.Authorizer = Authorizer{}
