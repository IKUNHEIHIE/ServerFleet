package testkit

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func TestAuthorizationFixtureRequiresExplicitAllow(t *testing.T) {
	err := (Authorizer{}).Authorize(context.Background(), auth.Check{})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("default must deny with the testkit sentinel, got %v", err)
	}
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("default denial must be classified as auth.ErrDenied, got %v", err)
	}
	if err := (Authorizer{Allow: true}).Authorize(context.Background(), auth.Check{}); err != nil {
		t.Fatal(err)
	}
}

func TestDeniedSentinelRemainsCompatibleWhenWrapped(t *testing.T) {
	if !errors.Is(ErrDenied, auth.ErrDenied) {
		t.Fatal("testkit.ErrDenied must match auth.ErrDenied")
	}
	wrapped := fmt.Errorf("outer: %w", ErrDenied)
	if !errors.Is(wrapped, ErrDenied) {
		t.Fatal("wrapped testkit.ErrDenied must remain discoverable")
	}
	if !errors.Is(wrapped, auth.ErrDenied) {
		t.Fatal("wrapped testkit denial must remain classified as auth.ErrDenied")
	}
}
