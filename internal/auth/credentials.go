package auth

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrCredentialUseUnknown = errors.New("auth: credential operation result unknown; do not automatically retry")
var ErrCredentialReplay = errors.New("auth: credential operation already admitted")

// A durable at-most-once fence, not a second task scheduler. Entries are never
// expired into retryability. Bounded exhaustion requires reviewed archival with
// the sole tasks store; failed/unknown provider calls cannot be auto replayed.
type credentialUse struct {
	Operation CredentialOperation
	Result    string
}

type CredentialMetadata struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	UsageScope      []string   `json:"usageScope"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	Revision        uint64     `json:"revision"`
}
type CredentialInput struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Secret     string     `json:"secret"`
	UsageScope []string   `json:"usageScope"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}
type storedCredential struct {
	Meta      CredentialMetadata
	Owner     string
	Sealed    sealedCredential
	BindingID string
	Revoked   bool
}
type credentialChange struct {
	ID, CredentialID, ActorID, Owner, BindingID string
	ExpectedRevision                            uint64
	Meta                                        CredentialMetadata
	Sealed                                      sealedCredential
	Expires                                     time.Time
}
type CredentialChangeView struct {
	ID               string    `json:"changeId"`
	CredentialID     string    `json:"credentialId"`
	ExpectedRevision uint64    `json:"expectedRevision"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

// CredentialOperation is created by trusted Controller composition, never a
// client claim. Gate must verify the current unified task/approval/actor/scope
// and reject completed or replayed operations. No secret is returned by this API.
type CredentialOperation struct {
	SubjectID, CredentialID, Module, Source, OperationID, PlanDigest string
	Action, ChangeID                                                 string
	ExpectedRevision                                                 uint64
}
type CredentialOperationGate func(context.Context, CredentialOperation) error
type CredentialReferenceReader func(context.Context, string, string) ([]string, error)
type CredentialService struct {
	identity   *IdentityService
	cipher     *credentialCipher
	gate       CredentialOperationGate
	references CredentialReferenceReader
}

func NewCredentialService(identity *IdentityService, activeKey string, keys map[string][]byte, gate CredentialOperationGate, references CredentialReferenceReader) (*CredentialService, error) {
	if identity == nil || !validPolicyID(activeKey) {
		return nil, ErrIdentityUnavailable
	}
	seen := map[string]bool{}
	for id, key := range keys {
		fingerprint := credentialDigest(string(key))
		if !validPolicyID(id) || seen[fingerprint] {
			return nil, ErrInvalidCheck
		}
		seen[fingerprint] = true
	}
	c, err := newCredentialCipher(activeKey, keys)
	if err != nil {
		return nil, err
	}
	return &CredentialService{identity, c, gate, references}, nil
}
func (v *CredentialService) String() string   { return "auth.CredentialService{secrets:redacted}" }
func (v *CredentialService) GoString() string { return v.String() }
func credentialPurpose(kind string) (string, string, error) {
	switch kind {
	case "cloudflare_token":
		return "dns-provider", "dns", nil
	case "smtp_password":
		return "smtp-password", "smtp", nil
	case "qqbot_secret":
		return "qqbot-secret", "qqbot", nil
	case "ai_api_key":
		return "ai-byok", "ai", nil
	case "backup_storage_secret":
		return "backup-storage", "backups", nil
	default:
		return "", "", ErrDenied
	}
}
func validateCredentialInput(input CredentialInput, now time.Time) error {
	_, module, err := credentialPurpose(input.Kind)
	if err != nil {
		return err
	}
	if input.Name == "" || len(input.Name) > 128 || len(input.Secret) == 0 || len(input.Secret) > 64<<10 || strings.Contains(input.Secret, "-----BEGIN") || strings.ContainsAny(input.Secret, "\r\n\x00") || !validExactIDs(input.UsageScope) || len(input.UsageScope) != 1 || input.UsageScope[0] != module || input.ExpiresAt != nil && !now.Before(*input.ExpiresAt) {
		return ErrInvalidCheck
	}
	return nil
}
func credentialExpiry(meta CredentialMetadata) time.Time {
	if meta.ExpiresAt != nil {
		return *meta.ExpiresAt
	}
	return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
}
func credentialView(record storedCredential, now time.Time) CredentialMetadata {
	meta := record.Meta
	meta.UsageScope = append([]string{}, meta.UsageScope...)
	if record.Revoked {
		meta.Status = "revoked"
	} else if !now.Before(credentialExpiry(meta)) {
		meta.Status = "expired"
	}
	return meta
}
func credentialBindingFor(record storedCredential) (credentialBinding, error) {
	purpose, _, err := credentialPurpose(record.Meta.Kind)
	if err != nil {
		return credentialBinding{}, err
	}
	return credentialBinding{record.BindingID, record.Owner, purpose, record.Meta.Revision}, nil
}
func cloneCredentialInput(input CredentialInput) CredentialInput {
	input.UsageScope = append([]string{}, input.UsageScope...)
	if input.ExpiresAt != nil {
		t := input.ExpiresAt.UTC()
		input.ExpiresAt = &t
	}
	return input
}

// Reserve encryption usage BEFORE Seal, in a separate committed transaction.
// Failed subsequent writes still consume the nonce/key budget, including across
// process restarts. Recovery must rotate keys if this counter could roll back.
func (v *CredentialService) reserve(ctx context.Context, secret, csrf string, input CredentialInput) error {
	if v == nil || v.cipher == nil || v.identity == nil {
		return ErrIdentityUnavailable
	}
	return v.identity.update(ctx, func(st *identityState, now time.Time) error {
		if _, err := managementActor(st, secret, csrf, true, now); err != nil {
			return err
		}
		if err := validateCredentialInput(input, now); err != nil {
			return err
		}
		if st.CredentialKeyUses[v.cipher.active] >= (1<<32)-1 {
			return ErrIdentityUnavailable
		}
		st.CredentialKeyUses[v.cipher.active]++
		return nil
	})
}
func (v *CredentialService) Create(ctx context.Context, secret, csrf string, input CredentialInput) (CredentialMetadata, error) {
	return v.CreateIdempotent(ctx, secret, csrf, "", input)
}
func (v *CredentialService) CreateIdempotent(ctx context.Context, secret, csrf, key string, input CredentialInput) (CredentialMetadata, error) {
	input = cloneCredentialInput(input)
	if err := v.reserve(ctx, secret, csrf, input); err != nil {
		return CredentialMetadata{}, err
	}
	var result CredentialMetadata
	err := v.identity.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		if err = validateCredentialInput(input, now); err != nil {
			return err
		}
		digest := requestDigest(struct {
			Name, Kind string
			Scope      []string
			Expires    *time.Time
		}{input.Name, input.Kind, input.UsageScope, input.ExpiresAt})
		receipt, replayed, err := loadReceipt(st, actor.ID, "credentials.create", key, digest, now)
		if err != nil {
			return err
		}
		if replayed {
			record, ok := st.Credentials[receipt.EntityID]
			if !ok || !v.secretEquals(record, []byte(input.Secret), now) {
				return ErrIdempotencyConflict
			}
			result = credentialView(record, now)
			return nil
		}
		if len(st.Credentials) >= 5000 {
			return ErrIdentityUnavailable
		}
		id, err := randomIdentity(v.identity.random)
		if err != nil {
			return err
		}
		purpose, _, _ := credentialPurpose(input.Kind)
		plain := []byte(input.Secret)
		defer clear(plain)
		sealed, err := v.cipher.seal(credentialBinding{id, actor.ID, purpose, 1}, plain)
		if err != nil {
			return err
		}
		meta := CredentialMetadata{ID: id, Name: input.Name, Kind: input.Kind, Status: "unverified", UsageScope: input.UsageScope, CreatedAt: now, ExpiresAt: input.ExpiresAt, Revision: 1}
		st.Credentials[id] = storedCredential{Meta: meta, Owner: actor.ID, Sealed: sealed, BindingID: id}
		saveReceipt(st, actor.ID, "credentials.create", key, digest, id, passwordRecord{}, now)
		result = meta
		return appendIdentityAudit(st, actor.ID, "credentials.create", "succeeded", now)
	})
	if err != nil {
		return CredentialMetadata{}, err
	}
	return result, nil
}
func (v *CredentialService) List(ctx context.Context, secret string) ([]CredentialMetadata, error) {
	if v == nil {
		return nil, ErrIdentityUnavailable
	}
	var out []CredentialMetadata
	err := v.identity.read(ctx, func(st *identityState, now time.Time) error {
		if _, err := managementActor(st, secret, "", false, now); err != nil {
			return err
		}
		out = []CredentialMetadata{}
		for _, record := range st.Credentials {
			out = append(out, credentialView(record, now))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (v *CredentialService) References(ctx context.Context, secret, id string) ([]string, error) {
	if v == nil || v.references == nil {
		return nil, ErrIdentityUnavailable
	}
	var subject string
	err := v.identity.read(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, "", false, now)
		if err != nil {
			return err
		}
		if _, ok := st.Credentials[id]; !ok {
			return ErrDenied
		}
		subject = actor.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	refs, err := v.references(ctx, subject, id)
	if err != nil || !validExactIDs(refs) {
		return nil, ErrIdentityUnavailable
	}
	return append([]string{}, refs...), nil
}

// Staging stores only ciphertext; the task request carries changeId, never
// secret text. No credential is changed until the sole task worker applies it.
func (v *CredentialService) StageRotation(ctx context.Context, secret, csrf, id string, expected uint64, input CredentialInput) (CredentialChangeView, error) {
	input = cloneCredentialInput(input)
	if expected == 0 {
		return CredentialChangeView{}, ErrRevisionConflict
	}
	if err := v.reserve(ctx, secret, csrf, input); err != nil {
		return CredentialChangeView{}, err
	}
	var result CredentialChangeView
	err := v.identity.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		old, ok := st.Credentials[id]
		if !ok || old.Revoked {
			return ErrDenied
		}
		if old.Meta.Revision != expected {
			return ErrRevisionConflict
		}
		if old.Meta.Kind != input.Kind {
			return ErrInvalidCheck
		}
		if err = validateCredentialInput(input, now); err != nil {
			return err
		}
		for key, change := range st.CredentialChanges {
			if !now.Before(change.Expires) {
				delete(st.CredentialChanges, key)
			}
		}
		if len(st.CredentialChanges) >= 1000 {
			return ErrIdentityUnavailable
		}
		changeID, err := randomIdentity(v.identity.random)
		if err != nil {
			return err
		}
		bindingID := id + "." + changeID
		purpose, _, _ := credentialPurpose(input.Kind)
		plain := []byte(input.Secret)
		defer clear(plain)
		sealed, err := v.cipher.seal(credentialBinding{bindingID, old.Owner, purpose, expected + 1}, plain)
		if err != nil {
			return err
		}
		meta := old.Meta
		meta.Name = input.Name
		meta.Revision++
		meta.Status = "unverified"
		meta.LastValidatedAt = nil
		meta.UsageScope = input.UsageScope
		meta.ExpiresAt = input.ExpiresAt
		change := credentialChange{changeID, id, actor.ID, old.Owner, bindingID, expected, meta, sealed, now.Add(10 * time.Minute)}
		st.CredentialChanges[changeID] = change
		result = CredentialChangeView{changeID, id, expected, change.Expires}
		return appendIdentityAudit(st, actor.ID, "credentials.rotation.stage", "succeeded", now)
	})
	if err != nil {
		return CredentialChangeView{}, err
	}
	return result, nil
}
func (v *CredentialService) authorizeOperation(ctx context.Context, op CredentialOperation) error {
	if ctx == nil || v == nil || v.gate == nil || !validPolicyID(op.SubjectID) || len(op.SubjectID) > 128 || !validSecret(op.CredentialID) || !validPolicyID(op.OperationID) || len(op.OperationID) > 128 || !validPolicyID(op.Module) || op.ExpectedRevision == 0 {
		return ErrDenied
	}
	if op.Source != "web" && op.Source != "ai" && op.Source != "plugin" && op.Source != "system" {
		return ErrDenied
	}
	digest, err := hex.DecodeString(op.PlanDigest)
	if err != nil || len(digest) != 32 {
		return ErrDenied
	}
	if err = v.gate(ctx, op); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrDenied
	}
	return ctx.Err()
}
func (v *CredentialService) ApplyRotation(ctx context.Context, op CredentialOperation, changeID string) (CredentialMetadata, error) {
	if op.Action != "rotate" || op.ChangeID != changeID || !validSecret(changeID) {
		return CredentialMetadata{}, ErrDenied
	}
	if err := v.authorizeOperation(ctx, op); err != nil {
		return CredentialMetadata{}, err
	}
	var result CredentialMetadata
	err := v.identity.update(ctx, func(st *identityState, now time.Time) error {
		change, ok := st.CredentialChanges[changeID]
		if !ok || !now.Before(change.Expires) || change.CredentialID != op.CredentialID || change.ActorID != op.SubjectID || change.ExpectedRevision != op.ExpectedRevision {
			return ErrDenied
		}
		old, ok := st.Credentials[op.CredentialID]
		if !ok || old.Revoked || old.Meta.Revision != op.ExpectedRevision {
			return ErrRevisionConflict
		}
		if _, module, err := credentialPurpose(old.Meta.Kind); err != nil || module != op.Module {
			return ErrDenied
		}
		actor, ok := st.Users[op.SubjectID]
		if !ok || actor.Disabled || !actor.Administrator {
			return ErrDenied
		}
		st.Credentials[op.CredentialID] = storedCredential{Meta: change.Meta, Owner: change.Owner, Sealed: change.Sealed, BindingID: change.BindingID}
		delete(st.CredentialChanges, changeID)
		result = change.Meta
		st.PolicyRevision++
		return appendIdentityAudit(st, actor.ID, "credentials.rotate", "succeeded", now)
	})
	if err != nil {
		return CredentialMetadata{}, err
	}
	return result, nil
}
func (v *CredentialService) Revoke(ctx context.Context, op CredentialOperation) error {
	if op.Action != "revoke" || op.ChangeID != "" {
		return ErrDenied
	}
	if err := v.authorizeOperation(ctx, op); err != nil {
		return err
	}
	return v.identity.update(ctx, func(st *identityState, now time.Time) error {
		record, ok := st.Credentials[op.CredentialID]
		if !ok {
			return ErrDenied
		}
		if record.Meta.Revision != op.ExpectedRevision {
			return ErrRevisionConflict
		}
		if _, module, err := credentialPurpose(record.Meta.Kind); err != nil || module != op.Module {
			return ErrDenied
		}
		actor, ok := st.Users[op.SubjectID]
		if !ok || actor.Disabled || !actor.Administrator {
			return ErrDenied
		}
		record.Revoked = true
		record.Meta.Revision++
		record.Sealed.Ciphertext = nil
		st.Credentials[op.CredentialID] = record
		st.PolicyRevision++
		return appendIdentityAudit(st, actor.ID, "credentials.revoke", "succeeded", now)
	})
}

// UseForService only lends plaintext to a program-registered service callback.
// It cannot be exposed as a generic HTTP/tool/Host decrypt operation. Gate is
// checked before and after use; in-flight revocation cannot unsend provider I/O,
// so a changed/unknown result is not reported as success or automatically retried.
func (v *CredentialService) UseForService(ctx context.Context, op CredentialOperation, consume func(context.Context, []byte) error) (err error) {
	if consume == nil || op.Action != "use" || op.ChangeID != "" {
		return ErrDenied
	}
	if err = v.authorizeOperation(ctx, op); err != nil {
		return err
	}
	var snapshot storedCredential
	var plaintext []byte
	err = v.identity.update(ctx, func(st *identityState, now time.Time) error {
		if _, seen := st.CredentialUses[op.OperationID]; seen {
			return ErrCredentialReplay
		}
		if len(st.CredentialUses) >= 10000 {
			return ErrIdentityUnavailable
		}
		record, ok := st.Credentials[op.CredentialID]
		if !ok || record.Revoked || record.Meta.Revision != op.ExpectedRevision || !now.Before(credentialExpiry(record.Meta)) {
			return ErrDenied
		}
		_, module, err := credentialPurpose(record.Meta.Kind)
		if err != nil || op.Module != module || !stringsIn(record.Meta.UsageScope, module) {
			return ErrDenied
		}
		if op.Source != "system" {
			actor, ok := st.Users[op.SubjectID]
			if !ok || actor.Disabled {
				return ErrDenied
			}
		}
		binding, err := credentialBindingFor(record)
		if err != nil {
			return err
		}
		plaintext, err = v.cipher.open(binding, record.Sealed, credentialExpiry(record.Meta), now, false)
		if err != nil {
			return ErrDenied
		}
		snapshot = record
		st.CredentialUses[op.OperationID] = credentialUse{op, "unknown"}
		return appendIdentityAudit(st, op.SubjectID, "credentials.use.intent", "prepared", now)
	})
	if err != nil {
		clear(plaintext)
		return err
	}
	defer clear(plaintext)
	defer func() {
		if recover() != nil {
			err = ErrCredentialUseUnknown
		}
		// Provider I/O may have occurred, including when ctx was canceled. Keep
		// the durable unknown fence even if recording the terminal audit fails.
		result := "failed"
		if err == nil {
			result = "succeeded"
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if auditErr := v.identity.update(auditCtx, func(st *identityState, now time.Time) error {
			use, ok := st.CredentialUses[op.OperationID]
			if !ok || use.Operation != op {
				return ErrIdentityUnavailable
			}
			use.Result = result
			st.CredentialUses[op.OperationID] = use
			return appendIdentityAudit(st, op.SubjectID, "credentials.use.complete", result, now)
		}); auditErr != nil {
			err = ErrCredentialUseUnknown
		}
	}()
	loanTimeout := 30 * time.Second
	// Backup transfer is bounded but may stream a full encrypted bundle. This
	// exception applies only to the registered backup module, with current
	// operation authorization before/after the loan and per-chunk service gates.
	if op.Module == "backups" {
		loanTimeout = 10 * time.Minute
	}
	useCtx, cancel := context.WithTimeout(ctx, loanTimeout)
	defer cancel()
	if consume(useCtx, plaintext) != nil || useCtx.Err() != nil {
		return ErrCredentialUseUnknown
	}
	if err = v.authorizeOperation(ctx, op); err != nil {
		return ErrCredentialUseUnknown
	}
	err = v.identity.read(ctx, func(st *identityState, now time.Time) error {
		current, ok := st.Credentials[op.CredentialID]
		if !ok || current.Revoked || current.Meta.Revision != snapshot.Meta.Revision || !now.Before(credentialExpiry(current.Meta)) {
			return ErrCredentialUseUnknown
		}
		return nil
	})
	if err != nil {
		return ErrCredentialUseUnknown
	}
	return nil
}
func stringsIn(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// SecretEquals is intentionally private: replay validation may compare a write
// against ciphertext but there is no metadata API capable of reconstructing it.
func (v *CredentialService) secretEquals(record storedCredential, value []byte, now time.Time) bool {
	binding, err := credentialBindingFor(record)
	if err != nil {
		return false
	}
	plain, err := v.cipher.open(binding, record.Sealed, credentialExpiry(record.Meta), now, record.Revoked)
	if err != nil {
		return false
	}
	defer clear(plain)
	return subtle.ConstantTimeCompare(plain, value) == 1
}
