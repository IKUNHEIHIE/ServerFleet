package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func credentialFixture(t *testing.T) (*CredentialService, *identityTestStore, *time.Time, LoginSession) {
	t.Helper()
	s, store, now := identityFixture(t)
	session := identityLogin(t, s)
	v, err := NewCredentialService(s, "synthetic-key-1", map[string][]byte{"synthetic-key-1": bytes.Repeat([]byte{17}, 32)}, func(context.Context, CredentialOperation) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return v, store, now, session
}
func credentialInput(secret string) CredentialInput {
	return CredentialInput{Name: "Local synthetic", Kind: "ai_api_key", Secret: secret, UsageScope: []string{"ai"}}
}
func credentialOp(session LoginSession, m CredentialMetadata, id, action string) CredentialOperation {
	return CredentialOperation{SubjectID: session.Identity.SubjectID, CredentialID: m.ID, Module: "ai", Source: "web", OperationID: id, PlanDigest: strings.Repeat("c", 64), Action: action, ExpectedRevision: m.Revision}
}
func TestCredentialEncryptedLifecycleAndIdempotence(t *testing.T) {
	ctx := context.Background()
	v, store, _, session := credentialFixture(t)
	const first = "synthetic-first-secret"
	input := credentialInput(first)
	meta, err := v.CreateIdempotent(ctx, session.Secret, session.CSRF, "credential-idempotence-01", input)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "unverified" || meta.Revision != 1 || meta.LastValidatedAt != nil {
		t.Fatal(meta)
	}
	again, err := v.CreateIdempotent(ctx, session.Secret, session.CSRF, "credential-idempotence-01", input)
	if err != nil || again.ID != meta.ID {
		t.Fatal(again, err)
	}
	_, err = v.CreateIdempotent(ctx, session.Secret, session.CSRF, "credential-idempotence-01", credentialInput("different-secret"))
	if err != ErrIdempotencyConflict {
		t.Fatal(err)
	}
	listed, err := v.List(ctx, session.Secret)
	if err != nil || len(listed) != 1 {
		t.Fatal(listed, err)
	}
	public, _ := json.Marshal(listed)
	if bytes.Contains(store.data, []byte(first)) || bytes.Contains(public, []byte(first)) || strings.Contains(fmt.Sprintf("%#v", v), first) {
		t.Fatal("secret disclosure")
	}
	if _, err = v.References(ctx, session.Secret, meta.ID); err != ErrIdentityUnavailable {
		t.Fatal("missing reference source became empty", err)
	}
	calls := 0
	use := func(want string) func(context.Context, []byte) error {
		return func(_ context.Context, secret []byte) error {
			calls++
			if string(secret) != want {
				t.Error("wrong secret")
			}
			return nil
		}
	}
	op := credentialOp(session, meta, "use-original-01", "use")
	if err = v.UseForService(ctx, op, use(first)); err != nil {
		t.Fatal(err)
	}
	if err = v.UseForService(ctx, op, use(first)); err != ErrCredentialReplay || calls != 1 {
		t.Fatal("replayed provider side effect", calls, err)
	}
	keys := map[string][]byte{"synthetic-key-1": bytes.Repeat([]byte{17}, 32), "synthetic-key-2": bytes.Repeat([]byte{18}, 32)}
	rotated, _ := NewCredentialService(v.identity, "synthetic-key-2", keys, v.gate, nil)
	stage, err := rotated.StageRotation(ctx, session.Secret, session.CSRF, meta.ID, meta.Revision, credentialInput("synthetic-second-secret"))
	if err != nil {
		t.Fatal(err)
	}
	op.OperationID = "old-still-usable"
	if err = v.UseForService(ctx, op, use(first)); err != nil {
		t.Fatal(err)
	}
	apply := credentialOp(session, meta, "rotation-01", "rotate")
	apply.ChangeID = stage.ID
	next, err := rotated.ApplyRotation(ctx, apply, stage.ID)
	if err != nil || next.Revision != 2 {
		t.Fatal(next, err)
	}
	if _, err = rotated.ApplyRotation(ctx, apply, stage.ID); err == nil {
		t.Fatal("rotation replay")
	}
	op.OperationID = "stale-revision-01"
	if err = rotated.UseForService(ctx, op, use(first)); err != ErrDenied {
		t.Fatal(err)
	}
	retired, _ := NewCredentialService(v.identity, "synthetic-key-2", map[string][]byte{"synthetic-key-2": keys["synthetic-key-2"]}, v.gate, nil)
	op = credentialOp(session, next, "new-credential-01", "use")
	if err = retired.UseForService(ctx, op, use("synthetic-second-secret")); err != nil {
		t.Fatal(err)
	}
	revoke := credentialOp(session, next, "revoke-01", "revoke")
	if err = retired.Revoke(ctx, revoke); err != nil {
		t.Fatal(err)
	}
	op.OperationID = "revoked-credential-01"
	if err = retired.UseForService(ctx, op, use("synthetic-second-secret")); err != ErrDenied {
		t.Fatal(err)
	}
	if bytes.Contains(store.data, []byte(first)) || bytes.Contains(store.data, []byte("synthetic-second-secret")) {
		t.Fatal("persisted plaintext")
	}
}
func TestCredentialFailsClosedAndNeverRetriesUnknown(t *testing.T) {
	ctx := context.Background()
	v, store, now, session := credentialFixture(t)
	for _, input := range []CredentialInput{{Name: "root", Kind: "ca_key", Secret: "root", UsageScope: []string{"ai"}}, credentialInput("-----BEGIN PRIVATE KEY-----")} {
		if _, err := v.Create(ctx, session.Secret, session.CSRF, input); err == nil {
			t.Fatal("root secret accepted")
		}
	}
	meta, err := v.Create(ctx, session.Secret, session.CSRF, credentialInput("synthetic-use-secret"))
	if err != nil {
		t.Fatal(err)
	}
	op := credentialOp(session, meta, "failure-01", "use")
	raw := []byte(nil)
	if err = v.UseForService(ctx, op, func(_ context.Context, b []byte) error { raw = b; return errors.New(string(b)) }); err != ErrCredentialUseUnknown {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("plaintext buffer retained")
	}
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error { t.Fatal("replayed unknown"); return nil }); err != ErrCredentialReplay {
		t.Fatal(err)
	}
	op.OperationID = "panic-01"
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error { panic("private") }); err != ErrCredentialUseUnknown {
		t.Fatal(err)
	}
	op.OperationID = "revoke-in-flight"
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error {
		return v.Revoke(ctx, credentialOp(session, meta, "revoke-in-flight-01", "revoke"))
	}); err != ErrCredentialUseUnknown {
		t.Fatal("reported success after revocation", err)
	}
	v.gate = nil
	op.OperationID = "missing-gate"
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error { t.Fatal("nil gate"); return nil }); err != ErrDenied {
		t.Fatal(err)
	}
	v.gate = func(context.Context, CredentialOperation) error { return nil }
	meta, err = v.Create(ctx, session.Secret, session.CSRF, credentialInput("another-synthetic-secret"))
	if err != nil {
		t.Fatal(err)
	}
	stage, err := v.StageRotation(ctx, session.Secret, session.CSRF, meta.ID, 1, credentialInput("staged-synthetic-secret"))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	apply := credentialOp(session, meta, "expired-stage", "rotate")
	apply.ChangeID = stage.ID
	if _, err = v.ApplyRotation(ctx, apply, stage.ID); err != ErrDenied {
		t.Fatal("expired stage", err)
	}
	if err = v.identity.update(ctx, func(st *identityState, _ time.Time) error {
		st.CredentialKeyUses[v.cipher.active] = (1 << 32) - 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(store.data)
	if _, err = v.Create(ctx, session.Secret, session.CSRF, credentialInput("too-many")); err != ErrIdentityUnavailable {
		t.Fatal(err)
	}
	if !bytes.Equal(before, store.data) {
		t.Fatal("budget exhaustion changed state")
	}
}
func TestCredentialProviderCompletionAuditFailureAndActionBinding(t *testing.T) {
	ctx := context.Background()
	v, store, _, session := credentialFixture(t)
	meta, err := v.Create(ctx, session.Secret, session.CSRF, credentialInput("synthetic-secret"))
	if err != nil {
		t.Fatal(err)
	}
	op := credentialOp(session, meta, "terminal-audit-failure", "use")
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error { store.failCommit = true; return nil }); err != ErrCredentialUseUnknown {
		t.Fatal(err)
	}
	store.failCommit = false
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error {
		t.Fatal("provider repeated after completion audit failure")
		return nil
	}); err != ErrCredentialReplay {
		t.Fatal(err)
	}
	op = credentialOp(session, meta, "wrong-action", "revoke")
	if err = v.UseForService(ctx, op, func(context.Context, []byte) error { t.Fatal("revoke admission used as decrypt"); return nil }); err != ErrDenied {
		t.Fatal(err)
	}
	if _, err = NewCredentialService(v.identity, "one", map[string][]byte{"one": bytes.Repeat([]byte{1}, 32), "two": bytes.Repeat([]byte{1}, 32)}, v.gate, nil); err == nil {
		t.Fatal("same key bypassed per-key budget with alias")
	}
}
