package auth

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type xrayMaterialTestProof struct{}

// Transaction/audit double ONLY. SQLite, current authenticated subject and the
// exact same-Tx reader are exercised separately in the isolated combination.
type xrayMaterialTestStore struct {
	*identityTestStore
	updates, failAt int
	noAudit         bool
	events          []XrayMaterialAuditEvent
}

func (s *xrayMaterialTestStore) UpdateWithContext(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	s.updates++
	var events []XrayMaterialAuditEvent
	err := s.identityTestStore.Update(ctx, func(raw []byte) ([]byte, error) {
		txCtx := ctx
		if !s.noAudit {
			txCtx = context.WithValue(ctx, xrayMaterialAuditContextKey{}, true)
		}
		next, err := fn(txCtx, raw)
		if err != nil {
			return nil, err
		}
		st, err := decodeIdentity(next)
		if err != nil {
			return nil, err
		}
		if !s.noAudit {
			events = append([]XrayMaterialAuditEvent(nil), st.XrayMaterialOutbox...)
			st.XrayMaterialOutbox = nil
			next, err = json.Marshal(st)
			if err != nil {
				return nil, err
			}
		}
		if s.failAt == s.updates {
			return nil, ErrIdentityUnavailable
		}
		return next, nil
	})
	if err == nil {
		s.events = append(s.events, events...)
	}
	return err
}

type xrayMaterialFixture struct {
	service  *XrayMaterialService
	identity *IdentityService
	store    *xrayMaterialTestStore
	login    LoginSession
	ctx      context.Context
	grantID  string
}

func newXrayMaterialFixture(t *testing.T, grant bool) *xrayMaterialFixture {
	t.Helper()
	_, base, _ := identityFixture(t)
	store := &xrayMaterialTestStore{identityTestStore: base}
	identity, err := NewIdentityServiceWithResources(store, func(_ context.Context, kind, id string) ([]string, error) {
		if kind == "node" && id == "node-a" {
			return []string{id}, nil
		}
		return nil, ErrPolicyUnavailable
	}, XrayMaterialPermissions())
	if err != nil {
		t.Fatal(err)
	}
	login, err := identity.Login(context.Background(), "admin", identityTestPassword, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f := &xrayMaterialFixture{identity: identity, store: store, login: login, ctx: context.WithValue(context.Background(), xrayMaterialTestProof{}, login.Identity.SubjectID)}
	if grant {
		permissions := []string{}
		for _, p := range XrayMaterialPermissions() {
			permissions = append(permissions, p.Key)
		}
		g, err := identity.CreateResourceGrant(context.Background(), login.Secret, login.CSRF, GrantRequest{PrincipalID: login.Identity.SubjectID, ResourceType: "node", ResourceID: "node-a", Permissions: permissions})
		if err != nil {
			t.Fatal(err)
		}
		f.grantID = g.GrantID
	}
	credentials, err := NewCredentialService(identity, "material-key", map[string][]byte{"material-key": bytes.Repeat([]byte{73}, 32)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = NewXrayMaterialService(credentials, func(ctx context.Context, check XrayMaterialCheck) error {
		if ctx.Value(xrayMaterialTestProof{}) != check.Operation.SubjectID || check.Operation.Scope.NodeID != "node-a" || (check.Operation.Scope.InstanceID != "instance-a" && check.Operation.Scope.InstanceID != "instance-b") {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *xrayMaterialFixture) op(t *testing.T, action, id string, scope XrayMaterialScope) XrayMaterialOperation {
	t.Helper()
	ref, err := XrayMaterialReference(scope)
	if err != nil {
		t.Fatal(err)
	}
	op := XrayMaterialOperation{SubjectID: f.login.Identity.SubjectID, OperationID: id, Action: action, Source: "web", ReferenceID: ref, PlanDigest: strings.Repeat("a", 64), Scope: scope, ExpiresAt: time.Now().Add(time.Minute)}
	if action != "generate" {
		op.ExpectedRevision = 1
	}
	if action == "reveal" {
		op.RecipientID = op.SubjectID
	}
	if action == "use" {
		op.Source = "plugin"
	}
	return op
}
func xrayUserScope(instance, user string) XrayMaterialScope {
	return XrayMaterialScope{NodeID: "node-a", InstanceID: instance, InboundID: "inbound-a", UserID: user, Purpose: "xray-user"}
}
func (f *xrayMaterialFixture) state(t *testing.T) *identityState {
	t.Helper()
	st, err := decodeIdentity(f.store.data)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestXrayMaterialGeneratedUUIDCentralCiphertextAndPermanentOneUse(t *testing.T) {
	f := newXrayMaterialFixture(t, true)
	scope := xrayUserScope("instance-a", "user-a")
	generate := f.op(t, "generate", "generate-a", scope)
	meta, err := f.service.Generate(f.ctx, generate)
	if err != nil || meta.State != "ready" || meta.Revision != 1 {
		t.Fatal("generation failed", err)
	}
	if same, err := f.service.Generate(f.ctx, generate); err != nil || same.ID != meta.ID {
		t.Fatal("metadata-only generate replay failed", err)
	}
	if f.state(t).CredentialKeyUses["material-key"] != 1 {
		t.Fatal("generate replay reused GCM budget or regenerated identity")
	}
	var secret, retained []byte
	calls := 0
	reveal := f.op(t, "reveal", "reveal-a", scope)
	if err = f.service.WithMaterial(f.ctx, reveal, func(_ context.Context, b []byte) error { calls++; retained = b; secret = bytes.Clone(b); return nil }); err != nil {
		t.Fatal("loan failed (including serialized operation time comparison)", err)
	}
	defer clear(secret)
	hexID := strings.ReplaceAll(string(secret), "-", "")
	uuid, err := hex.DecodeString(hexID)
	if err != nil || len(secret) != 36 || len(uuid) != 16 || uuid[6]>>4 != 4 || uuid[8]>>6 != 2 || !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatal("not a generated canonical UUID or buffer retained")
	}
	if bytes.Contains(f.store.data, secret) {
		t.Fatal("central payload contains plaintext UUID")
	}
	reopened, err := NewXrayMaterialService(f.service.credentials, f.service.gate)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.WithMaterial(f.ctx, reveal, func(context.Context, []byte) error { calls++; return nil }); err != ErrCredentialReplay || calls != 1 {
		t.Fatal("loan replayed after service reopen", err)
	}
	secondScope := xrayUserScope("instance-a", "user-b")
	if _, err = f.service.Generate(f.ctx, f.op(t, "generate", "generate-independent-user", secondScope)); err != nil {
		t.Fatal(err)
	}
	if err = f.service.WithMaterial(f.ctx, f.op(t, "reveal", "reveal-independent-user", secondScope), func(_ context.Context, b []byte) error {
		if bytes.Equal(b, secret) {
			t.Error("different users received a fixed shared identity")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	list, err := f.service.credentials.List(context.Background(), f.login.Secret)
	if err != nil || len(list) != 0 {
		t.Fatal("private material appeared as generic HTTP credential kind", err)
	}
	if _, err = f.service.credentials.Create(context.Background(), f.login.Secret, f.login.CSRF, CredentialInput{Name: "not a generic credential", Kind: "xray_user_uuid", Secret: string(secret), UsageScope: []string{"xray"}}); err != ErrDenied {
		t.Fatal("unaccepted public credential kind enabled", err)
	}
	encoded, _ := json.Marshal(f.store.events)
	if bytes.Contains(encoded, secret) || f.state(t).XrayMaterialUses[reveal.OperationID].State != "delivered" {
		t.Fatal("secret in typed audit or terminal claim missing")
	}
}

func TestXrayMaterialsDifferentInstancesCannotSwapCiphertextOrExtendExpiry(t *testing.T) {
	for _, tamper := range []string{"ciphertext", "expiry", "reference"} {
		t.Run(tamper, func(t *testing.T) {
			f := newXrayMaterialFixture(t, true)
			a := xrayUserScope("instance-a", "user-a")
			b := xrayUserScope("instance-b", "user-a")
			opA, opB := f.op(t, "generate", "create-a", a), f.op(t, "generate", "create-b", b)
			opA.MaterialExpiresAt = time.Now().Add(time.Hour)
			opB.MaterialExpiresAt = opA.MaterialExpiresAt
			if _, err := f.service.Generate(f.ctx, opA); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Generate(f.ctx, opB); err != nil {
				t.Fatal(err)
			}
			st := f.state(t)
			record := st.XrayMaterials[opB.ReferenceID]
			switch tamper {
			case "ciphertext":
				record.Sealed = st.XrayMaterials[opA.ReferenceID].Sealed
			case "expiry":
				record.Meta.ExpiresAt = record.Meta.ExpiresAt.Add(time.Hour)
			case "reference":
				record.Meta.Scope.InstanceID = "instance-a"
			}
			st.XrayMaterials[opB.ReferenceID] = record
			f.store.data, _ = json.Marshal(st)
			err := f.service.WithMaterial(f.ctx, f.op(t, "reveal", "swapped-loan", b), func(context.Context, []byte) error { t.Error("cross-instance/edited metadata decrypted"); return nil })
			if err == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
}

func TestXrayMaterialUnknownGenerationAndMissingTypedAuditNeverRegenerate(t *testing.T) {
	f := newXrayMaterialFixture(t, true)
	op := f.op(t, "generate", "generation-unknown", xrayUserScope("instance-a", "user-a"))
	f.store.failAt = 2
	if meta, err := f.service.Generate(f.ctx, op); err != ErrCredentialUseUnknown || meta.ID != "" {
		t.Fatal("lost completion acknowledged", err)
	}
	st := f.state(t)
	if st.XrayMaterials[op.ReferenceID].Meta.State != "reserved" || st.CredentialKeyUses["material-key"] != 1 {
		t.Fatal("reservation/budget lost")
	}
	f.store.failAt = 0
	if _, err := f.service.Generate(f.ctx, op); err != ErrCredentialUseUnknown {
		t.Fatal("unknown generation retried", err)
	}
	op.OperationID = "new-operation-must-not-overwrite"
	if _, err := f.service.Generate(f.ctx, op); err != ErrIdempotencyConflict {
		t.Fatal("another operation replaced reserved scope", err)
	}
	if f.state(t).CredentialKeyUses["material-key"] != 1 {
		t.Fatal("unknown scope encrypted a second key")
	}
	f = newXrayMaterialFixture(t, true)
	f.store.noAudit = true
	before := bytes.Clone(f.store.data)
	if _, err := f.service.Generate(f.ctx, f.op(t, "generate", "missing-audit", xrayUserScope("instance-a", "user-a"))); err != ErrIdentityUnavailable || !bytes.Equal(before, f.store.data) {
		t.Fatal("missing typed same-Tx audit allowed material mutation", err)
	}
}

func TestXrayMaterialFailedRevokedCancelledAndUncommittedLoansNeverReplay(t *testing.T) {
	for _, failure := range []string{"error", "panic", "revoke", "grant-revoke", "final-audit", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			f := newXrayMaterialFixture(t, true)
			scope := xrayUserScope("instance-a", "user-a")
			if _, err := f.service.Generate(f.ctx, f.op(t, "generate", "create-a", scope)); err != nil {
				t.Fatal(err)
			}
			op := f.op(t, "reveal", "loan-unknown", scope)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			if failure == "final-audit" {
				f.store.failAt = f.store.updates + 2
			}
			calls := 0
			var retained []byte
			err := f.service.WithMaterial(ctx, op, func(_ context.Context, b []byte) error {
				calls++
				retained = b
				switch failure {
				case "error":
					return errors.New("consumer failure")
				case "panic":
					panic("consumer panic")
				case "cancel":
					cancel()
				case "revoke":
					_, err := f.service.Revoke(f.ctx, f.op(t, "revoke", "revoke-inflight", scope))
					return err
				case "grant-revoke":
					return f.identity.RevokeResourceGrant(context.Background(), f.login.Secret, f.login.CSRF, f.grantID)
				}
				return nil
			})
			if err != ErrCredentialUseUnknown || calls != 1 || !bytes.Equal(retained, make([]byte, len(retained))) {
				t.Fatal("failed loan returned success or retained plaintext", calls, err)
			}
			f.store.failAt = 0
			// Authorization may itself now reject; neither path can call consumer.
			if err = f.service.WithMaterial(f.ctx, op, func(context.Context, []byte) error { t.Error("unknown loan repeated"); return nil }); err == nil {
				t.Fatal("unknown result became retryable")
			}
			claim := f.state(t).XrayMaterialUses[op.OperationID]
			want := "unknown"
			if failure == "final-audit" {
				want = "reserved"
			}
			if claim.State != want {
				t.Fatal("wrong durable outcome", claim.State)
			}
		})
	}
}

func TestXrayRealityPrivateKeyNeverRevealedAndPublicParametersAuthenticated(t *testing.T) {
	f := newXrayMaterialFixture(t, true)
	scope := XrayMaterialScope{NodeID: "node-a", InstanceID: "instance-a", Purpose: "xray-reality"}
	if _, err := f.service.Generate(f.ctx, f.op(t, "generate", "create-reality", scope)); err != nil {
		t.Fatal(err)
	}
	var public []byte
	if err := f.service.WithPublicParameters(f.ctx, f.op(t, "public-parameters", "client-parameters", scope), func(_ context.Context, b []byte) error { public = bytes.Clone(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := f.service.WithMaterial(f.ctx, f.op(t, "use", "controlled-template", scope), func(_ context.Context, b []byte) error {
		key, err := base64.RawURLEncoding.DecodeString(string(b))
		if err != nil {
			return err
		}
		defer clear(key)
		private, err := ecdh.X25519().NewPrivateKey(key)
		if err != nil {
			return err
		}
		if base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()) != string(public) {
			t.Error("wrong public parameters")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.WithMaterial(f.ctx, f.op(t, "reveal", "must-not-export-server-key", scope), func(context.Context, []byte) error {
		t.Error("server REALITY private key revealed to browser")
		return nil
	}); err != ErrDenied {
		t.Fatal(err)
	}
	st := f.state(t)
	ref, _ := XrayMaterialReference(scope)
	record := st.XrayMaterials[ref]
	record.PublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32))
	st.XrayMaterials[ref] = record
	f.store.data, _ = json.Marshal(st)
	if err := f.service.WithPublicParameters(f.ctx, f.op(t, "public-parameters", "tampered-parameters", scope), func(context.Context, []byte) error { t.Error("edited public parameters delivered"); return nil }); err == nil {
		t.Fatal("public field was not authenticated")
	}
}

func TestXrayMaterialRequiresRealSourceProofExplicitNodeGrantAndAllowedPurpose(t *testing.T) {
	f := newXrayMaterialFixture(t, false)
	scope := xrayUserScope("instance-a", "user-a")
	op := f.op(t, "generate", "no-node-grant", scope)
	if _, err := f.service.Generate(f.ctx, op); err != ErrDenied {
		t.Fatal("administrator implicitly gained node access", err)
	}
	f = newXrayMaterialFixture(t, true)
	op = f.op(t, "generate", "no-source-proof", scope)
	if _, err := f.service.Generate(context.Background(), op); err != ErrDenied {
		t.Fatal("known subject authenticated itself", err)
	}
	for _, mutate := range []func(*XrayMaterialOperation){func(op *XrayMaterialOperation) { op.Source = "ai" }, func(op *XrayMaterialOperation) { op.Scope.Purpose = "root-ca" }, func(op *XrayMaterialOperation) { op.ReferenceID = strings.Repeat("f", 64) }, func(op *XrayMaterialOperation) { op.ExpiresAt = time.Now().Add(-time.Minute) }, func(op *XrayMaterialOperation) { op.ExpiresAt = time.Now().Add(time.Hour) }} {
		bad := op
		mutate(&bad)
		before := bytes.Clone(f.store.data)
		if _, err := f.service.Generate(f.ctx, bad); err != ErrDenied || !bytes.Equal(before, f.store.data) {
			t.Fatal("untrusted material operation changed state", err)
		}
	}
}

func TestXrayMaterialAuthoritySourceFaultAndExpiredMetadataFailClosed(t *testing.T) {
	f := newXrayMaterialFixture(t, true)
	op := f.op(t, "generate", "source-fault", xrayUserScope("instance-a", "user-a"))
	gate := f.service.gate
	f.service.gate = func(context.Context, XrayMaterialCheck) error { return ErrIdentityUnavailable }
	before := bytes.Clone(f.store.data)
	if _, err := f.service.Generate(f.ctx, op); err != ErrIdentityUnavailable || !bytes.Equal(before, f.store.data) {
		t.Fatal("authority outage misreported or changed state", err)
	}
	f.service.gate = gate
	op.MaterialExpiresAt = time.Now().Add(30 * time.Minute)
	if _, err := f.service.Generate(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	f.identity.now = func() time.Time { return time.Now().Add(time.Hour) }
	metadata := f.op(t, "metadata", "expired-metadata", op.Scope)
	if meta, err := f.service.Metadata(f.ctx, metadata); err != nil || meta.State != "expired" {
		t.Fatal("expired material reported ready", err)
	}
	if err := f.service.WithMaterial(f.ctx, f.op(t, "reveal", "expired-loan", op.Scope), func(context.Context, []byte) error { t.Error("expired secret used"); return nil }); err != ErrDenied {
		t.Fatal(err)
	}
}

func TestXrayMaterialForcesForwardPayloadVersionAndPreservesItOnGenericWrites(t *testing.T) {
	f := newXrayMaterialFixture(t, true)
	if f.state(t).Version != 1 {
		t.Fatal("ordinary legacy identity was unexpectedly migrated")
	}
	if _, err := f.service.Generate(f.ctx, f.op(t, "generate", "versioned-generate", xrayUserScope("instance-a", "user-a"))); err != nil {
		t.Fatal(err)
	}
	if f.state(t).Version != 2 {
		t.Fatal("material was stored where old writers discard it")
	}
	if _, err := f.identity.Authenticate(context.Background(), f.login.Secret, f.login.CSRF, true); err != nil {
		t.Fatal(err)
	}
	st := f.state(t)
	if st.Version != 2 {
		t.Fatal("generic identity write downgraded protected material format")
	}
	st.Version = 1
	bad, _ := json.Marshal(st)
	if _, err := decodeIdentity(bad); err != ErrIdentityUnavailable {
		t.Fatal("material under legacy format accepted", err)
	}
	st.Version = 3
	bad, _ = json.Marshal(st)
	if _, err := decodeIdentity(bad); err != ErrIdentityUnavailable {
		t.Fatal("unknown future format accepted", err)
	}
}
