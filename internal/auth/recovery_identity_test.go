package auth

import (
	"context"
	"testing"
)

func TestRestoredIdentityInvalidatesSessionsAndMergesRevocations(t *testing.T) {
	v, store, now, session := credentialFixture(t)
	meta, err := v.Create(context.Background(), session.Secret, session.CSRF, credentialInput("synthetic-restore-secret"))
	if err != nil {
		t.Fatal(err)
	}
	policy := RestoredIdentityPolicy{ActorID: "offline-recovery", At: *now, RevokedCredentialIDs: []string{meta.ID}, DisabledSubjectIDs: []string{session.Identity.SubjectID}}
	raw, err := InvalidateRestoredIdentity(store.data, policy)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := decodeIdentity(store.data)
	after, _ := decodeIdentity(raw)
	if !after.Credentials[meta.ID].Revoked || after.Users[session.Identity.SubjectID].Epoch <= before.Users[session.Identity.SubjectID].Epoch || after.CredentialKeyUses[v.cipher.active] != before.CredentialKeyUses[v.cipher.active] {
		t.Fatal("restored authority resurrected")
	}
	store.data = raw
	if _, err = v.identity.Authenticate(context.Background(), session.Secret, "", false); err == nil {
		t.Fatal("old session revived")
	}
	op := credentialOp(session, meta, "restore-use", "use")
	if err = v.UseForService(context.Background(), op, func(context.Context, []byte) error { t.Fatal("revoked credential borrowed"); return nil }); err == nil {
		t.Fatal("old credential revived")
	}
	policy.At = now.Add(-1)
	if _, err = InvalidateRestoredIdentity(raw, policy); err == nil {
		t.Fatal("clock rollback")
	}
}
