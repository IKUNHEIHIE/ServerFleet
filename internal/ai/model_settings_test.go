package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

func modelSettingsFixture(t *testing.T) (*ModelSettingsService, *SessionService, *sessionTestStore, *SessionActor, *auth.CredentialMetadata, *bool) {
	t.Helper()
	s, store, actor, _, _ := sessionFixture(t)
	meta := &auth.CredentialMetadata{ID: "credential-ref", Kind: "ai_api_key", Status: "unverified", UsageScope: []string{"ai"}, Revision: 1}
	available := true
	m, err := NewModelSettingsService(s, func(_ context.Context, owner, ref string) (auth.CredentialMetadata, error) {
		if !available || owner != actor.SubjectID || ref != meta.ID {
			return auth.CredentialMetadata{}, ErrToolDenied
		}
		return *meta, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, s, store, actor, meta, &available
}

func TestModelSettingsConcurrentCASAndLegacyAuditSinkPreservesProfileEvents(t *testing.T) {
	ctx := context.Background()
	m, s, _, _, _, _ := modelSettingsFixture(t)
	var committed = []byte(`{"Version":1}`)
	modelSink := false
	normalCalls, modelCalls := 0, 0
	backend := SessionSQLBackend{
		Read: func(context.Context, string, ...any) ([]byte, error) { return bytes.Clone(committed), nil },
		InTx: func(ctx context.Context, fn func(SessionSQLStatements) error) error {
			next := bytes.Clone(committed)
			ops := SessionSQLStatements{Read: func(context.Context, string, ...any) ([]byte, error) { return bytes.Clone(next), nil }, Exec: func(_ context.Context, _ string, args ...any) (int64, error) {
				next = bytes.Clone(args[0].([]byte))
				return 1, nil
			}, AppendAudit: func(context.Context, []SessionAuditEvent) error { normalCalls++; return nil }}
			if modelSink {
				ops.AppendModelAudit = func(_ context.Context, e []ModelSettingsAuditEvent) error { modelCalls += len(e); return nil }
			}
			if err := fn(ops); err != nil {
				return err
			}
			committed = next
			return nil
		},
	}
	sqlStore, err := NewSQLSessionStore(backend)
	if err != nil {
		t.Fatal(err)
	}
	s.store = sqlStore
	if _, err = m.Set(ctx, 0, configuredModelInput()); err != nil {
		t.Fatal(err)
	}
	state, err := decodeSessions(committed)
	if err != nil || len(state.Outbox) != 1 || state.Outbox[0].ProfileID == "" || normalCalls != 0 || modelCalls != 0 {
		t.Fatal("legacy sink silently consumed model audit", err)
	}
	if _, err = s.Create(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = decodeSessions(committed)
	if err != nil || len(state.Outbox) != 1 || normalCalls != 1 {
		t.Fatal("session audit removed pending model event", err)
	}
	modelSink = true
	if _, err = m.Get(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = decodeSessions(committed)
	if err != nil || len(state.Outbox) != 0 || modelCalls != 1 {
		t.Fatal("typed model audit not acknowledged", err)
	}
	// Actual CAS concurrency uses the serial test persistence adapter, separately
	// from the SQL statement double above; real SQLite rollback is in the PoC.
	concurrent, _, _, _, _, _ := modelSettingsFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { _, err := concurrent.Set(ctx, 0, configuredModelInput()); results <- err })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrModelRevisionConflict {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent model updates lost CAS", success, conflict)
	}
}
func configuredModelInput() ModelSettingsInput {
	return ModelSettingsInput{true, "openai-responses", "fixture-model", "credential-ref", 1}
}

func TestModelSettingsDefaultDisabledCASPersistenceAndNoResourceGrant(t *testing.T) {
	m, s, store, _, _, _ := modelSettingsFixture(t)
	ctx := context.Background()
	got, err := m.Get(ctx)
	if err != nil || got.Enabled || got.Revision != 0 || got.ID != "" || got.CredentialRef != "" {
		t.Fatal("implicit model configuration", got, err)
	}
	if _, err = m.Profile(ctx, "owner"); err != ErrModelUnavailable {
		t.Fatal("unconfigured model enabled", err)
	}
	beforeSession, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := m.Set(ctx, 0, configuredModelInput())
	if err != nil || cfg.Revision != 1 || cfg.OwnerID != "owner" || cfg.ID == "" {
		t.Fatal(cfg, err)
	}
	if _, err = m.Set(ctx, 0, configuredModelInput()); err != ErrModelRevisionConflict {
		t.Fatal("stale config overwrote", err)
	}
	profile, err := m.Profile(ctx, "owner")
	if err != nil || profile.ID != cfg.ID || profile.Revision != 1 || profile.CredentialRef != "credential-ref" {
		t.Fatal(profile, err)
	}
	view, err := s.Get(ctx, beforeSession.ID)
	if err != nil || len(view.AllowedNodeIDs) != 0 || view.ScopeRevision != beforeSession.ScopeRevision {
		t.Fatal("model config granted resources", view, err)
	}
	// Same durable store, new service instance; Profile is not process memory.
	restarted, _ := NewModelSettingsService(s, m.credential)
	if restored, err := restarted.Get(ctx); err != nil || restored.ID != cfg.ID || restored.Revision != 1 {
		t.Fatal(restored, err)
	}
	var aggregate sessionState
	if json.Unmarshal(store.data, &aggregate) != nil || len(aggregate.Outbox) == 0 || aggregate.Outbox[len(aggregate.Outbox)-1].ProfileID != cfg.ID || aggregate.Outbox[len(aggregate.Outbox)-1].ModelRevision != 1 {
		t.Fatal("model mutation missing bound audit")
	}
}

func TestModelSettingsRevocationRotationOwnerAndDisableWithMetadataDown(t *testing.T) {
	m, _, _, actor, meta, available := modelSettingsFixture(t)
	ctx := context.Background()
	cfg, err := m.Set(ctx, 0, configuredModelInput())
	if err != nil {
		t.Fatal(err)
	}
	meta.Revision = 2
	if _, err = m.Profile(ctx, "owner"); err != ErrModelUnavailable {
		t.Fatal("silently adopted new key revision", err)
	}
	input := configuredModelInput()
	input.CredentialRevision = 2
	cfg, err = m.Set(ctx, cfg.Revision, input)
	if err != nil {
		t.Fatal(err)
	}
	*available = false
	input.Enabled = false
	cfg, err = m.Set(ctx, cfg.Revision, input)
	if err != nil || cfg.Enabled {
		t.Fatal("metadata outage prevented disable", err)
	}
	if _, err = m.Profile(ctx, "owner"); err != ErrModelUnavailable {
		t.Fatal(err)
	}
	actor.SubjectID = "another-owner"
	if _, err = m.Profile(ctx, "owner"); err != ErrToolDenied {
		t.Fatal("cross-owner profile exposed", err)
	}
	if own, err := m.Get(ctx); err != nil || own.ID != "" || own.Enabled {
		t.Fatal("another owner inherited configuration", err)
	}
}

func TestModelSettingsInvalidMetadataAndFailedCommitNeverEnable(t *testing.T) {
	for _, failure := range []string{"reference", "kind", "scope", "revoked", "expired", "revision", "source", "commit", "model-key", "provider", "non-human"} {
		t.Run(failure, func(t *testing.T) {
			m, _, store, actor, meta, available := modelSettingsFixture(t)
			ctx := context.Background()
			input := configuredModelInput()
			switch failure {
			case "reference":
				input.CredentialRef = "another-ref"
			case "kind":
				meta.Kind = "smtp_password"
			case "scope":
				meta.UsageScope = []string{"dns"}
			case "revoked":
				meta.Status = "revoked"
			case "expired":
				expired := time.Now().Add(-time.Second)
				meta.ExpiresAt = &expired
			case "revision":
				meta.Revision = 2
			case "source":
				*available = false
			case "commit":
				store.fail = true
			case "model-key":
				input.Model = "sk-synthetic-api-key"
			case "provider":
				input.Provider = "arbitrary-endpoint"
			case "non-human":
				actor.HumanWeb = false
			}
			if cfg, err := m.Set(ctx, 0, input); err == nil || cfg.ID != "" {
				t.Fatal("invalid model enabled", cfg, err)
			}
			store.fail = false
			cfg, err := m.Get(ctx)
			if err != nil || cfg.Enabled || cfg.Revision != 0 {
				t.Fatal("failure persisted settings", cfg, err)
			}
		})
	}
}

func TestModelSettingsProfileChangeInvalidatesQueuedAndInFlightGeneration(t *testing.T) {
	m, s, _, _, _, _ := modelSettingsFixture(t)
	ctx := context.Background()
	cfg, err := m.Set(ctx, 0, configuredModelInput())
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var chat *ConversationService
	humanContext := ctx
	providerCalls := 0
	chat, err = NewConversationService(s, ConversationConfig{Profile: m.Profile, Filter: func(_ context.Context, _ SessionView, _ string, text string) (string, error) { return text, nil }, Generate: func(ctx context.Context, r GenerationRequest, emit func(string) error) error {
		providerCalls++
		digest, err := generationDigest(r)
		if err != nil {
			return err
		}
		op, err := m.ResolveCredential(ctx, r, digest)
		if err != nil {
			return err
		}
		if op.ExpectedRevision != 1 || chat.CheckCredentialOperation(ctx, op) != nil {
			return ErrToolDenied
		}
		input := cfg.ModelSettingsInput
		input.Enabled = false
		if _, err := m.Set(ctx, cfg.Revision, input); err != ErrToolDenied {
			t.Error("inference context performed human configuration change", err)
		}
		if _, err = m.Set(humanContext, cfg.Revision, input); err != nil {
			return err
		}
		if chat.CheckCredentialOperation(ctx, op) != ErrToolDenied {
			t.Error("disabled profile retained key admission")
		}
		return emit("must not be persisted")
	}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := chat.Submit(ctx, v.ID, "request-one", MessageInput{"question", "client-one", v.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chat.Submit(ctx, v.ID, "request-two", MessageInput{"next question", "client-two", v.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = chat.Generate(ctx, v.ID, message.MessageID); err != ErrGenerationUnknown {
		t.Fatal("disabled in-flight model succeeded", err)
	}
	if messages, err := chat.Messages(ctx, v.ID, 0, 100); err != nil || len(messages) != 2 {
		t.Fatal("partial answer persisted", err)
	}
	saved, err := m.Get(humanContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Set(humanContext, saved.Revision, cfg.ModelSettingsInput); err != nil {
		t.Fatal(err)
	}
	if _, err = chat.Generate(ctx, v.ID, queued.MessageID); err != ErrScopeConflict || providerCalls != 1 {
		t.Fatal("queued request adopted changed profile", err, providerCalls)
	}
}

func TestModelSettingsLegacyAndCorruptPersistence(t *testing.T) {
	m, _, store, _, _, _ := modelSettingsFixture(t)
	ctx := context.Background()
	store.data = []byte(`{"Version":1}`)
	if cfg, err := m.Get(ctx); err != nil || cfg.Enabled || cfg.ID != "" {
		t.Fatal("legacy state enabled model", err)
	}
	if _, err := m.Set(ctx, 0, configuredModelInput()); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(store.data)
	var state map[string]any
	if err := json.Unmarshal(before, &state); err != nil {
		t.Fatal(err)
	}
	models := state["Models"].(map[string]any)
	models["owner"].(map[string]any)["ownerId"] = "other-owner"
	store.data, _ = json.Marshal(state)
	if _, err := m.Get(ctx); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatal("corrupt owner binding accepted", err)
	}
}
