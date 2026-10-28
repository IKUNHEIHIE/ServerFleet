package testkit

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func TestAuthorizationFixtureRequiresExplicitAllow(t *testing.T) {
	if err := (Authorizer{}).Authorize(context.Background(), auth.Check{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("default must deny, got %v", err)
	}
	if err := (Authorizer{Allow: true}).Authorize(context.Background(), auth.Check{}); err != nil {
		t.Fatal(err)
	}
}
