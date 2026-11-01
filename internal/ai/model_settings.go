package ai

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

var ErrModelRevisionConflict = errors.New("ai: model configuration revision conflict")

type ModelSettingsInput struct {
	Enabled            bool   `json:"enabled"`
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	CredentialRef      string `json:"credentialRef"`
	CredentialRevision uint64 `json:"credentialRevision"`
}
type ModelSettings struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId"`
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
	ModelSettingsInput
}

// CredentialMetadataCheck must authorize the CURRENT owner's use of this exact
// reference, then return fresh metadata. A successful global metadata listing or
// a caller's credentialRef is not sufficient authority. No plaintext read API.
type CredentialMetadataCheck func(context.Context, string, string) (auth.CredentialMetadata, error)

type ModelSettingsService struct {
	sessions   *SessionService
	credential CredentialMetadataCheck
}

func NewModelSettingsService(sessions *SessionService, credential CredentialMetadataCheck) (*ModelSettingsService, error) {
	if sessions == nil || credential == nil {
		return nil, ErrModelUnavailable
	}
	return &ModelSettingsService{sessions, credential}, nil
}

func validModelSettingsInput(input ModelSettingsInput) bool {
	if !input.Enabled && input.Provider == "" && input.Model == "" && input.CredentialRef == "" && input.CredentialRevision == 0 {
		return true
	}
	return input.Provider == "openai-responses" && exactID(input.Model) && len(input.Model) <= 128 && !strings.HasPrefix(input.Model, "sk-") && exactID(input.CredentialRef) && input.CredentialRevision > 0
}

func (m *ModelSettingsService) check(ctx context.Context, owner string, input ModelSettingsInput) (result error) {
	defer func() {
		if recover() != nil {
			result = ErrModelUnavailable
		}
	}()
	meta, err := m.credential(ctx, owner, input.CredentialRef)
	now := m.sessions.now().UTC()
	if err != nil || ctx.Err() != nil || meta.ID != input.CredentialRef || meta.Revision != input.CredentialRevision || meta.Kind != "ai_api_key" || len(meta.UsageScope) != 1 || meta.UsageScope[0] != "ai" || meta.Status != "unverified" && meta.Status != "valid" || meta.ExpiresAt != nil && !now.Before(*meta.ExpiresAt) {
		return ErrModelUnavailable
	}
	return nil
}

func (m *ModelSettingsService) read(ctx context.Context, actor SessionActor, action string) (out ModelSettings, err error) {
	err = m.sessions.change(ctx, func(st *sessionState, _ time.Time) error {
		if _, err := m.sessions.recheck(ctx, actor, action, "", false); err != nil {
			return err
		}
		out = st.Models[actor.SubjectID]
		if out.ID == "" {
			out = ModelSettings{OwnerID: actor.SubjectID}
		}
		return nil
	})
	return out, err
}

// Get returns SAVED configuration, not verified provider health. An absent record
// is explicitly disabled/revision zero, with no inherited model or credential.
func (m *ModelSettingsService) Get(ctx context.Context) (ModelSettings, error) {
	if m == nil {
		return ModelSettings{}, ErrModelUnavailable
	}
	actor, err := m.sessions.actor(ctx, "ai.models.read", "", false)
	if err != nil {
		return ModelSettings{}, err
	}
	return m.read(ctx, actor, "ai.models.read")
}

// Set is an internal CAS operation; only a current human Web actor may change
// their own settings. It does not enable an HTTP route or select a production
// AI framework. Turning off remains possible when metadata/provider is down.
func (m *ModelSettingsService) Set(ctx context.Context, expected uint64, input ModelSettingsInput) (out ModelSettings, result error) {
	if m == nil || !validModelSettingsInput(input) {
		return out, ErrToolDenied
	}
	actor, err := m.sessions.actor(ctx, "ai.models.update", "", true)
	if err != nil {
		return out, err
	}
	previous, err := m.read(ctx, actor, "ai.models.update")
	if err != nil {
		return out, err
	}
	if previous.Revision != expected {
		return out, ErrModelRevisionConflict
	}
	sameReference := previous.Provider == input.Provider && previous.Model == input.Model && previous.CredentialRef == input.CredentialRef && previous.CredentialRevision == input.CredentialRevision
	// New references are verified even in a saved-but-disabled configuration.
	// Disabling an already bound reference (or clearing it) requires no lookup.
	if (input.Enabled || input.CredentialRef != "" && !sameReference) && m.check(ctx, actor.SubjectID, input) != nil {
		return out, ErrModelUnavailable
	}
	err = m.sessions.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := m.sessions.recheck(ctx, actor, "ai.models.update", "", true); err != nil {
			return err
		}
		previous := st.Models[actor.SubjectID]
		if previous.Revision != expected {
			return ErrModelRevisionConflict
		}
		if previous.Revision == math.MaxUint64 || len(st.Outbox) >= 10000 {
			return ErrSessionUnavailable
		}
		if previous.ID == "" {
			if len(st.Models) >= 1000 {
				return ErrSessionUnavailable
			}
			id, err := sessionID()
			if err != nil {
				return err
			}
			previous.ID = id
		}
		out = ModelSettings{ID: previous.ID, OwnerID: actor.SubjectID, Revision: previous.Revision + 1, UpdatedAt: now, ModelSettingsInput: input}
		st.Models[actor.SubjectID] = out
		st.Outbox = append(st.Outbox, sessionAudit{SubjectID: actor.SubjectID, Action: "ai.models.update", ProfileID: out.ID, ModelRevision: out.Revision, Result: "success", At: now, NodeIDs: []string{}})
		return nil
	})
	if err != nil {
		return ModelSettings{}, err
	}
	return out, nil
}

func (m *ModelSettingsService) current(ctx context.Context, owner string) (ModelSettings, error) {
	if m == nil || !exactID(owner) {
		return ModelSettings{}, ErrModelUnavailable
	}
	actor, err := m.sessions.actor(ctx, "ai.messages.generate", "", false)
	if err != nil || actor.SubjectID != owner {
		return ModelSettings{}, ErrToolDenied
	}
	cfg, err := m.read(ctx, actor, "ai.messages.generate")
	if err != nil || !cfg.Enabled || cfg.ID == "" || m.check(ctx, owner, cfg.ModelSettingsInput) != nil {
		return ModelSettings{}, ErrModelUnavailable
	}
	return cfg, nil
}

// Profile can be bound directly to ConversationConfig.Profile. Credential
// rotation invalidates old config until the human explicitly binds its revision;
// no queued/in-flight request silently adopts another model or key revision.
func (m *ModelSettingsService) Profile(ctx context.Context, owner string) (ModelProfile, error) {
	cfg, err := m.current(ctx, owner)
	if err != nil {
		return ModelProfile{}, err
	}
	return ModelProfile{cfg.ID, cfg.Model, cfg.CredentialRef, cfg.Revision}, nil
}

// CheckBudgetProfile is a read-only composition gate. The caller separately
// proves the current administrator identity; this port binds the saved owner
// profile without opening a nested writer inside budget configuration.
func (m *ModelSettingsService) CheckBudgetProfile(ctx context.Context, owner, profile string) error {
	if m == nil || ctx == nil || !exactID(owner) || !exactID(profile) {
		return ErrToolDenied
	}
	return m.sessions.store.View(ctx, func(raw []byte) error {
		state, err := decodeSessions(raw)
		if err != nil {
			return err
		}
		model, ok := state.Models[owner]
		if !ok || model.OwnerID != owner || model.ID != profile || !model.Enabled {
			return ErrToolDenied
		}
		return nil
	})
}

// ResolveCredential supplies metadata only. The credential service MUST also
// run ConversationService.CheckCredentialOperation on the private live ctx.
func (m *ModelSettingsService) ResolveCredential(ctx context.Context, r GenerationRequest, digest string) (auth.CredentialOperation, error) {
	expected, err := generationDigest(r)
	if err != nil || digest != expected {
		return auth.CredentialOperation{}, ErrToolDenied
	}
	cfg, err := m.current(ctx, r.SubjectID)
	if err != nil {
		return auth.CredentialOperation{}, err
	}
	if r.Profile != (ModelProfile{cfg.ID, cfg.Model, cfg.CredentialRef, cfg.Revision}) {
		return auth.CredentialOperation{}, ErrToolDenied
	}
	return auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: cfg.CredentialRef, Module: "ai", Source: "ai", OperationID: r.OperationID, PlanDigest: digest, Action: "use", ExpectedRevision: cfg.CredentialRevision}, nil
}
