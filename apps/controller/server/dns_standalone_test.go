package server

import (
	"context"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
)

func TestStandaloneDNSGateRejectsUnscopedUse(t *testing.T) {
	s := &Server{}
	op := auth.CredentialOperation{SubjectID: "subject-1", CredentialID: "cred-1", Module: "dns", Source: "web", OperationID: "dns-use-1", PlanDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Action: "use", ExpectedRevision: 1}
	if err := s.standaloneDNSGate(context.Background(), op); err == nil {
		t.Fatal("unscoped dns credential use must be denied")
	}
	scope := &standaloneDNSBorrowScope{server: s, op: op}
	scope.active.Store(true)
	if err := s.standaloneDNSGate(context.WithValue(context.Background(), standaloneDNSBorrowKey{}, scope), op); err != nil {
		t.Fatalf("active matching scope must pass: %v", err)
	}
	other := op
	other.OperationID = "dns-use-2"
	if err := s.standaloneDNSGate(context.WithValue(context.Background(), standaloneDNSBorrowKey{}, scope), other); err == nil {
		t.Fatal("gate must only allow the exact borrowed operation")
	}
	scope.active.Store(false)
	if err := s.standaloneDNSGate(context.WithValue(context.Background(), standaloneDNSBorrowKey{}, scope), op); err == nil {
		t.Fatal("closed borrow window must be denied")
	}
}

func TestStandaloneDNSBorrowRejectsIncompleteIntents(t *testing.T) {
	s := &Server{}
	consume := func(context.Context, string) error { return nil }
	for _, use := range []dns.SecretUse{
		{Module: "dns", Purpose: "dns-provider", SubjectID: "subject-1", Ref: "cred-1"},
		{Module: "smtp", Purpose: "dns-provider", SubjectID: "subject-1", Ref: "cred-1", ExpectedRevision: 1},
		{Module: "dns", Purpose: "dns-provider", SubjectID: "", Ref: "cred-1", ExpectedRevision: 1},
	} {
		if err := s.borrowStandaloneDNS(context.Background(), use, consume); err == nil {
			t.Fatalf("incomplete intent must be denied: %+v", use)
		}
	}
}