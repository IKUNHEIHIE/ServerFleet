package auth

import (
	"context"
	"testing"
)

func TestObservationReadOnlyDoesNotNestWriterAndStillRechecksRevocation(t *testing.T) {
	ctx := context.Background()
	s, store, _ := identityFixture(t)
	session := identityLogin(t, s)
	before := string(store.data)
	store.failCommit = true
	if err := s.CheckObservationReadOnly(ctx, session.Secret, session.Identity); err != nil {
		t.Fatal("read-only gate opened writer", err)
	}
	if string(store.data) != before {
		t.Fatal("read-only gate changed idle lifetime/state")
	}
	store.failCommit = false
	if err := s.RevokeSession(ctx, session.Secret, session.CSRF, session.Identity.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckObservationReadOnly(ctx, session.Secret, session.Identity); err != ErrCredential {
		t.Fatal("cached observation bypassed revocation", err)
	}
}
