package auth

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// These typed materials remain inside the CENTRAL credential service/store and
// key budget. They are not generic HTTP credential kinds or a plugin database.
type XrayMaterialScope struct{ NodeID, InstanceID, InboundID, UserID, Purpose string }
type XrayMaterialMetadata struct {
	ID                   string
	Scope                XrayMaterialScope
	Revision             uint64
	State                string
	CreatedAt, ExpiresAt time.Time
}
type XrayMaterialOperation struct {
	SubjectID, OperationID, Action, Source, ReferenceID, PlanDigest, RecipientID string
	Scope                                                                        XrayMaterialScope
	ExpectedRevision                                                             uint64
	ExpiresAt, MaterialExpiresAt                                                 time.Time
}
type XrayMaterialCheck struct {
	Operation XrayMaterialOperation
	Stage     string
}
type XrayMaterialGate func(context.Context, XrayMaterialCheck) error
type XrayMaterialAuditEvent struct {
	Scope                                                                   XrayMaterialScope
	SubjectID, OperationID, Action, Source, ReferenceID, PlanDigest, Result string
	Revision                                                                uint64
	At                                                                      time.Time
}
type xrayMaterialRecord struct {
	Meta                                           XrayMaterialMetadata
	Owner, CreateBinding, ReservedKeyID, PublicKey string
	Sealed                                         sealedCredential
}
type xrayMaterialUse struct {
	Operation XrayMaterialOperation
	State     string
}
type xrayMaterialAuditContextKey struct{}
type XrayMaterialService struct {
	credentials *CredentialService
	gate        XrayMaterialGate
}

func (s XrayMaterialMetadata) String() string   { return "auth.XrayMaterialMetadata{material:redacted}" }
func (s XrayMaterialMetadata) GoString() string { return s.String() }
func (s *XrayMaterialService) String() string {
	return "auth.XrayMaterialService{central-credentials:redacted}"
}
func (s *XrayMaterialService) GoString() string { return s.String() }

func validXrayMaterialScope(scope XrayMaterialScope) bool {
	if !validPolicyID(scope.NodeID) || !validPolicyID(scope.InstanceID) || len(scope.NodeID) > 128 || len(scope.InstanceID) > 128 {
		return false
	}
	switch scope.Purpose {
	case "xray-user":
		return validPolicyID(scope.InboundID) && len(scope.InboundID) <= 128 && validPolicyID(scope.UserID) && len(scope.UserID) <= 128
	case "xray-reality":
		return scope.InboundID == "" && scope.UserID == ""
	default:
		return false
	}
}
func XrayMaterialReference(scope XrayMaterialScope) (string, error) {
	if !validXrayMaterialScope(scope) {
		return "", ErrInvalidCheck
	}
	return requestDigest(struct {
		Domain string
		Scope  XrayMaterialScope
	}{"serverfleet.xray.material-reference.v1", scope}), nil
}
func xrayMaterialBinding(record xrayMaterialRecord) credentialBinding {
	return credentialBinding{ID: requestDigest(struct {
		Domain, ID, PublicKey string
		Scope                 XrayMaterialScope
		CreatedAt, ExpiresAt  time.Time
	}{"serverfleet.xray.material-aad.v1", record.Meta.ID, record.PublicKey, record.Meta.Scope, record.Meta.CreatedAt, record.Meta.ExpiresAt}), Owner: record.Owner, Purpose: record.Meta.Scope.Purpose, Revision: record.Meta.Revision}
}
func XrayMaterialPermissions() []PermissionDefinition {
	return []PermissionDefinition{
		{Key: "xray.credentials.create", Description: "Generate credentials for explicitly granted Xray nodes", ResourceType: "node"},
		{Key: "xray.credentials.metadata", Description: "Read redacted Xray credential references", ResourceType: "node", ReadOnly: true},
		{Key: "xray.credentials.use", Description: "Use Xray material only for approved controlled tasks", ResourceType: "node"},
		{Key: "xray.credentials.reveal", Description: "Explicit confirmed single-user client export", ResourceType: "node"},
		{Key: "xray.credentials.revoke", Description: "Revoke central Xray material references", ResourceType: "node"},
	}
}
func NewXrayMaterialService(credentials *CredentialService, gate XrayMaterialGate) (*XrayMaterialService, error) {
	if credentials == nil || credentials.identity == nil || credentials.cipher == nil || gate == nil {
		return nil, ErrIdentityUnavailable
	}
	if _, ok := credentials.identity.store.(IdentityTransactionStore); !ok {
		return nil, ErrIdentityUnavailable
	}
	for _, p := range XrayMaterialPermissions() {
		if _, ok := credentials.identity.permissions[p.Key]; !ok {
			return nil, ErrPolicyUnavailable
		}
	}
	return &XrayMaterialService{credentials, gate}, nil
}
func validateXrayMaterialState(st *identityState) error {
	// Version-1 writers discard unknown fields. Never store secret material or
	// permanent fences where an older writer could silently erase them.
	if (len(st.XrayMaterials) > 0 || len(st.XrayMaterialUses) > 0 || len(st.XrayMaterialOutbox) > 0) && st.Version != 2 {
		return ErrIdentityUnavailable
	}
	if st.XrayMaterials == nil {
		st.XrayMaterials = map[string]xrayMaterialRecord{}
	}
	if st.XrayMaterialUses == nil {
		st.XrayMaterialUses = map[string]xrayMaterialUse{}
	}
	if len(st.XrayMaterials) > 5000 || len(st.XrayMaterialUses) > 10000 || len(st.XrayMaterialOutbox) > 10000 {
		return ErrIdentityUnavailable
	}
	for id, r := range st.XrayMaterials {
		want, err := XrayMaterialReference(r.Meta.Scope)
		if err != nil || id != want || r.Meta.ID != id || r.Meta.Revision == 0 || r.Meta.CreatedAt.IsZero() || r.Meta.CreatedAt.After(st.ClockFloor) || !r.Meta.ExpiresAt.After(r.Meta.CreatedAt) || st.Users[r.Owner].ID == "" || !validPolicyID(r.ReservedKeyID) || !validSecret(r.CreateBinding) {
			return ErrIdentityUnavailable
		}
		switch r.Meta.State {
		case "reserved":
			if len(r.Sealed.Ciphertext) != 0 || r.Sealed.KeyID != "" || r.PublicKey != "" {
				return ErrIdentityUnavailable
			}
		case "ready", "revoked":
			expected := 64
			if r.Meta.Scope.Purpose == "xray-reality" {
				expected = 71
				key, err := base64.RawURLEncoding.DecodeString(r.PublicKey)
				if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != r.PublicKey {
					return ErrIdentityUnavailable
				}
			} else if r.PublicKey != "" {
				return ErrIdentityUnavailable
			}
			if len(r.Sealed.Ciphertext) != expected || r.Sealed.KeyID != r.ReservedKeyID {
				return ErrIdentityUnavailable
			}
		default:
			return ErrIdentityUnavailable
		}
	}
	for id, use := range st.XrayMaterialUses {
		if id != use.Operation.OperationID || !validPolicyID(id) || len(id) > 128 || (use.State != "reserved" && use.State != "delivered" && use.State != "unknown") || !validXrayMaterialScope(use.Operation.Scope) {
			return ErrIdentityUnavailable
		}
	}
	return nil
}
func (s *XrayMaterialService) check(ctx context.Context, op XrayMaterialOperation, stage string) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrIdentityUnavailable
		}
	}()
	if s == nil || ctx == nil || ctx.Err() != nil || !validSecret(op.SubjectID) || !validPolicyID(op.OperationID) || len(op.OperationID) > 128 || !validXrayMaterialScope(op.Scope) || op.Source != "web" && op.Source != "plugin" || !time.Now().Before(op.ExpiresAt) || op.ExpiresAt.After(time.Now().Add(15*time.Minute)) {
		return ErrDenied
	}
	ref, e := XrayMaterialReference(op.Scope)
	if e != nil || ref != op.ReferenceID {
		return ErrDenied
	}
	digest, e := hex.DecodeString(op.PlanDigest)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != op.PlanDigest {
		return ErrDenied
	}
	if op.Action == "generate" {
		if op.ExpectedRevision != 0 || !op.MaterialExpiresAt.IsZero() && !time.Now().Before(op.MaterialExpiresAt) {
			return ErrDenied
		}
	} else if op.ExpectedRevision == 0 || !op.MaterialExpiresAt.IsZero() {
		return ErrDenied
	}
	if op.Action == "reveal" {
		if op.Source != "web" || op.RecipientID != op.SubjectID || op.Scope.Purpose != "xray-user" {
			return ErrDenied
		}
	} else if op.RecipientID != "" {
		return ErrDenied
	}
	if op.Action == "use" && op.Source != "plugin" {
		return ErrDenied
	}
	if xrayMaterialPermission(op) == "" {
		return ErrDenied
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = s.gate(bounded, XrayMaterialCheck{op, stage}); err != nil || bounded.Err() != nil {
		if bounded.Err() != nil {
			return ErrIdentityUnavailable
		}
		if errors.Is(err, ErrIdentityUnavailable) {
			return ErrIdentityUnavailable
		}
		if errors.Is(err, ErrPolicyUnavailable) {
			return ErrPolicyUnavailable
		}
		return ErrDenied
	}
	return nil
}
func xrayMaterialPermission(op XrayMaterialOperation) string {
	switch op.Action {
	case "generate":
		return "xray.credentials.create"
	case "metadata":
		return "xray.credentials.metadata"
	case "use":
		return "xray.credentials.use"
	case "reveal":
		return "xray.credentials.reveal"
	case "public-parameters":
		if op.Source == "web" {
			return "xray.credentials.reveal"
		}
		return "xray.credentials.use"
	case "revoke":
		return "xray.credentials.revoke"
	default:
		return ""
	}
}
func (s *XrayMaterialService) authorizeState(ctx context.Context, st *identityState, op XrayMaterialOperation, now time.Time) error {
	allowed, err := s.credentials.identity.grantedNodes(ctx, st, op.SubjectID, xrayMaterialPermission(op), now)
	if err != nil {
		return err
	}
	if _, ok := allowed[op.Scope.NodeID]; !ok {
		return ErrDenied
	}
	return nil
}
func appendXrayMaterialAudit(st *identityState, op XrayMaterialOperation, revision uint64, result string, now time.Time) error {
	if len(st.XrayMaterialOutbox) >= 10000 {
		return ErrIdentityUnavailable
	}
	st.XrayMaterialOutbox = append(st.XrayMaterialOutbox, XrayMaterialAuditEvent{Scope: op.Scope, SubjectID: op.SubjectID, OperationID: op.OperationID, Action: op.Action, Source: op.Source, ReferenceID: op.ReferenceID, PlanDigest: op.PlanDigest, Result: result, Revision: revision, At: now})
	return nil
}
func (s *XrayMaterialService) mutate(ctx context.Context, op XrayMaterialOperation, stage string, fn func(*identityState, time.Time) error) error {
	return s.credentials.identity.updateTransaction(ctx, func(txCtx context.Context, st *identityState, now time.Time) error {
		if txCtx.Value(xrayMaterialAuditContextKey{}) != true {
			return ErrIdentityUnavailable
		}
		if err := s.authorizeState(txCtx, st, op, now); err != nil {
			return err
		}
		if err := s.check(txCtx, op, "commit-"+stage+"-prepare"); err != nil {
			return err
		}
		if err := fn(st, now); err != nil {
			return err
		}
		return s.check(txCtx, op, "commit-"+stage)
	})
}

// Generate has no secret input or output. A durable placeholder and shared GCM
// budget are committed BEFORE key generation/Seal. A lost completion retains
// the reservation; it never generates a different identity automatically.
func (s *XrayMaterialService) Generate(ctx context.Context, op XrayMaterialOperation) (XrayMaterialMetadata, error) {
	var zero XrayMaterialMetadata
	if op.Action != "generate" {
		return zero, ErrDenied
	}
	if err := s.check(ctx, op, "prepare"); err != nil {
		return zero, err
	}
	binding := requestDigest(op)
	var result XrayMaterialMetadata
	replayed := false
	err := s.mutate(ctx, op, "generate-reserve", func(st *identityState, now time.Time) error {
		if record, exists := st.XrayMaterials[op.ReferenceID]; exists {
			if record.CreateBinding != binding {
				return ErrIdempotencyConflict
			}
			if record.Meta.State == "reserved" {
				return ErrCredentialUseUnknown
			}
			if record.Meta.State != "ready" || !now.Before(record.Meta.ExpiresAt) {
				return ErrDenied
			}
			result = record.Meta
			replayed = true
			return nil
		}
		if len(st.XrayMaterials) >= 5000 || st.CredentialKeyUses[s.credentials.cipher.active] >= (1<<32)-1 {
			return ErrIdentityUnavailable
		}
		expires := op.MaterialExpiresAt
		if expires.IsZero() {
			expires = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		}
		if !now.Before(expires) {
			return ErrDenied
		}
		st.CredentialKeyUses[s.credentials.cipher.active]++
		st.Version = 2
		meta := XrayMaterialMetadata{ID: op.ReferenceID, Scope: op.Scope, Revision: 1, State: "reserved", CreatedAt: now, ExpiresAt: expires}
		st.XrayMaterials[meta.ID] = xrayMaterialRecord{Meta: meta, Owner: op.SubjectID, CreateBinding: binding, ReservedKeyID: s.credentials.cipher.active}
		return appendXrayMaterialAudit(st, op, 1, "reserved", now)
	})
	if err != nil {
		return zero, err
	}
	if !replayed {
		err = s.mutate(ctx, op, "generate-complete", func(st *identityState, now time.Time) error {
			record, ok := st.XrayMaterials[op.ReferenceID]
			if !ok || record.CreateBinding != binding || record.Meta.State != "reserved" || record.ReservedKeyID != s.credentials.cipher.active {
				return ErrCredentialUseUnknown
			}
			var plain []byte
			if op.Scope.Purpose == "xray-user" {
				var id [16]byte
				if _, err := rand.Read(id[:]); err != nil {
					return ErrIdentityUnavailable
				}
				id[6] = id[6]&0x0f | 0x40
				id[8] = id[8]&0x3f | 0x80
				hexID := hex.EncodeToString(id[:])
				plain = []byte(hexID[:8] + "-" + hexID[8:12] + "-" + hexID[12:16] + "-" + hexID[16:20] + "-" + hexID[20:])
			} else {
				key, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					return ErrIdentityUnavailable
				}
				private := key.Bytes()
				defer clear(private)
				plain = []byte(base64.RawURLEncoding.EncodeToString(private))
				record.PublicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
			}
			defer clear(plain)
			sealed, err := s.credentials.cipher.seal(xrayMaterialBinding(record), plain)
			if err != nil {
				return err
			}
			record.Sealed = sealed
			record.Meta.State = "ready"
			st.XrayMaterials[op.ReferenceID] = record
			result = record.Meta
			return appendXrayMaterialAudit(st, op, 1, "ready", now)
		})
		if err != nil {
			return zero, ErrCredentialUseUnknown
		}
	}
	if s.check(ctx, op, "deliver") != nil {
		return zero, ErrCredentialUseUnknown
	}
	return result, nil
}

func (s *XrayMaterialService) Metadata(ctx context.Context, op XrayMaterialOperation) (XrayMaterialMetadata, error) {
	var result XrayMaterialMetadata
	if op.Action != "metadata" {
		return result, ErrDenied
	}
	if err := s.check(ctx, op, "read"); err != nil {
		return result, err
	}
	err := s.credentials.identity.read(ctx, func(st *identityState, now time.Time) error {
		if err := s.authorizeState(ctx, st, op, now); err != nil {
			return err
		}
		record, ok := st.XrayMaterials[op.ReferenceID]
		if !ok || record.Meta.Scope != op.Scope || record.Meta.Revision != op.ExpectedRevision {
			return ErrDenied
		}
		result = record.Meta
		if result.State == "ready" && !now.Before(result.ExpiresAt) {
			result.State = "expired"
		}
		return nil
	})
	if err != nil {
		return XrayMaterialMetadata{}, err
	}
	if s.check(ctx, op, "deliver") != nil {
		return XrayMaterialMetadata{}, ErrDenied
	}
	return result, nil
}

func (s *XrayMaterialService) Revoke(ctx context.Context, op XrayMaterialOperation) (XrayMaterialMetadata, error) {
	var result XrayMaterialMetadata
	if op.Action != "revoke" {
		return result, ErrDenied
	}
	if err := s.check(ctx, op, "prepare"); err != nil {
		return result, err
	}
	err := s.mutate(ctx, op, "revoke", func(st *identityState, now time.Time) error {
		record, ok := st.XrayMaterials[op.ReferenceID]
		if !ok || record.Meta.Scope != op.Scope || record.Meta.Revision != op.ExpectedRevision || record.Meta.State != "ready" || record.Meta.Revision == ^uint64(0) {
			return ErrRevisionConflict
		}
		record.Meta.Revision++
		record.Meta.State = "revoked"
		st.XrayMaterials[op.ReferenceID] = record
		result = record.Meta
		return appendXrayMaterialAudit(st, op, record.Meta.Revision, "revoked", now)
	})
	if err != nil {
		return XrayMaterialMetadata{}, err
	}
	if s.check(ctx, op, "deliver") != nil {
		return XrayMaterialMetadata{}, ErrCredentialUseUnknown
	}
	return result, nil
}

// WithMaterial uses CENTRAL one-use state. reveal is human-Web/user-UUID only;
// server REALITY private keys may only serve registered controlled plugin tasks.
// Neither this method nor revoke claims to disconnect existing Xray sessions.
func (s *XrayMaterialService) WithMaterial(ctx context.Context, op XrayMaterialOperation, consume func(context.Context, []byte) error) (result error) {
	if consume == nil || (op.Action != "use" && op.Action != "reveal") {
		return ErrDenied
	}
	if err := s.check(ctx, op, "prepare"); err != nil {
		return err
	}
	var plain []byte
	err := s.mutate(ctx, op, "use-reserve", func(st *identityState, now time.Time) error {
		if _, seen := st.XrayMaterialUses[op.OperationID]; seen {
			return ErrCredentialReplay
		}
		if len(st.XrayMaterialUses) >= 10000 {
			return ErrIdentityUnavailable
		}
		record, ok := st.XrayMaterials[op.ReferenceID]
		if !ok || record.Meta.Scope != op.Scope || record.Meta.Revision != op.ExpectedRevision || record.Meta.State != "ready" || !now.Before(record.Meta.ExpiresAt) {
			return ErrDenied
		}
		var err error
		plain, err = s.credentials.cipher.open(xrayMaterialBinding(record), record.Sealed, record.Meta.ExpiresAt, now, false)
		if err != nil {
			return err
		}
		st.XrayMaterialUses[op.OperationID] = xrayMaterialUse{op, "reserved"}
		return appendXrayMaterialAudit(st, op, record.Meta.Revision, "reserved", now)
	})
	if err != nil {
		clear(plain)
		return err
	}
	defer clear(plain)
	known := false
	defer func() {
		defer func() {
			if recover() != nil {
				result = ErrCredentialUseUnknown
			}
		}()
		if recover() != nil {
			known = false
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		finalKnown := false
		finishErr := s.credentials.identity.updateTransaction(finishCtx, func(txCtx context.Context, st *identityState, now time.Time) error {
			if txCtx.Value(xrayMaterialAuditContextKey{}) != true {
				return ErrIdentityUnavailable
			}
			claim, ok := st.XrayMaterialUses[op.OperationID]
			if !ok || requestDigest(claim.Operation) != requestDigest(op) || claim.State != "reserved" {
				return ErrDenied
			}
			record, exists := st.XrayMaterials[op.ReferenceID]
			finalKnown = known && exists && record.Meta.State == "ready" && record.Meta.Revision == op.ExpectedRevision && now.Before(record.Meta.ExpiresAt) && s.authorizeState(txCtx, st, op, now) == nil && s.check(txCtx, op, "commit-use-observe") == nil
			claim.State = "unknown"
			if finalKnown {
				claim.State = "delivered"
			}
			st.XrayMaterialUses[op.OperationID] = claim
			return appendXrayMaterialAudit(st, op, op.ExpectedRevision, claim.State, now)
		})
		if finishErr != nil || !finalKnown {
			result = ErrCredentialUseUnknown
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if s.check(bounded, op, "consume") != nil || consume(bounded, plain) != nil || bounded.Err() != nil || s.check(bounded, op, "observe") != nil {
		return ErrCredentialUseUnknown
	}
	known = true
	return nil
}

// PublicParameters contains only the stored REALITY client verification value.
// It still requires current reveal/use permission; ordinary metadata omits it.
func (s *XrayMaterialService) WithPublicParameters(ctx context.Context, op XrayMaterialOperation, consume func(context.Context, []byte) error) error {
	if consume == nil || op.Action != "public-parameters" || op.Scope.Purpose != "xray-reality" {
		return ErrDenied
	}
	if err := s.check(ctx, op, "read-public"); err != nil {
		return err
	}
	var public []byte
	err := s.credentials.identity.read(ctx, func(st *identityState, now time.Time) error {
		if err := s.authorizeState(ctx, st, op, now); err != nil {
			return err
		}
		record, ok := st.XrayMaterials[op.ReferenceID]
		if !ok || record.Meta.Scope != op.Scope || record.Meta.Revision != op.ExpectedRevision || record.Meta.State != "ready" || !now.Before(record.Meta.ExpiresAt) {
			return ErrDenied
		}
		// Authenticate public metadata against the encrypted private key too;
		// an edited database field must not become a bogus client credential.
		plain, err := s.credentials.cipher.open(xrayMaterialBinding(record), record.Sealed, record.Meta.ExpiresAt, now, false)
		if err != nil {
			return err
		}
		defer clear(plain)
		private, err := base64.RawURLEncoding.DecodeString(string(plain))
		if err != nil {
			return ErrDenied
		}
		defer clear(private)
		key, err := ecdh.X25519().NewPrivateKey(private)
		if err != nil || base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()) != record.PublicKey {
			return ErrDenied
		}
		public = []byte(record.PublicKey)
		return nil
	})
	defer clear(public)
	if err != nil {
		return err
	}
	if s.check(ctx, op, "consume-public") != nil || consume(ctx, public) != nil || s.check(ctx, op, "observe-public") != nil {
		return ErrCredentialUseUnknown
	}
	return nil
}
