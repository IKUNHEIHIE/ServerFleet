package ai

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// Explicit inference/filter doubles: no actual model or BYOK claim.
func conversationFixture(t *testing.T, s *SessionService, generate func(context.Context, GenerationRequest, func(string) error) error) (*ConversationService, *bool) {
	t.Helper()
	enabled := true
	c, err := NewConversationService(s, ConversationConfig{
		Profile: func(context.Context, string) (ModelProfile, error) {
			if !enabled {
				return ModelProfile{}, ErrModelUnavailable
			}
			return ModelProfile{"local-profile", "test-model", "credential-ref", 1}, nil
		},
		Filter: func(_ context.Context, _ SessionView, _ string, text string) (string, error) {
			if strings.Contains(text, "PRIVATE KEY") {
				return "", ErrToolDenied
			}
			return strings.ReplaceAll(text, "synthetic-secret", "[redacted]"), nil
		},
		Generate: generate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, &enabled
}
func TestMessagesPersistIdempotencyOrderEventsAndTextOnlyResponses(t *testing.T) {
	ctx := context.Background()
	s, store, actor, _, _ := sessionFixture(t)
	view, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var late func(string) error
	c, enabled := conversationFixture(t, s, func(_ context.Context, r GenerationRequest, emit func(string) error) error {
		calls++
		if r.Profile.CredentialRef != "credential-ref" || r.OperationID != "ai-generation-"+r.MessageID || len(r.Messages) == 0 {
			t.Fatal("unbound provider input", r)
		}
		for _, message := range r.Messages {
			if strings.Contains(message.RedactedContent, "synthetic-secret") || len(message.TaskIDs) != 0 {
				t.Fatal("secret or execution authority reached provider")
			}
		}
		if calls == 2 && (len(r.Messages) != 3 || r.Messages[0].Role != "user" || r.Messages[1].Role != "assistant" || r.Messages[2].Role != "user") {
			t.Fatal("queued next message omitted the preceding committed answer")
		}
		late = emit
		return emit("A text answer with synthetic-secret")
	})
	input := MessageInput{"Hello synthetic-secret", "client-one", view.ScopeRevision}
	*enabled = false
	if _, err = c.Submit(ctx, view.ID, "request-one", input); err != ErrModelUnavailable {
		t.Fatal("disabled model accepted work", err)
	}
	*enabled = true
	first, err := c.Submit(ctx, view.ID, "request-one", input)
	if err != nil || first.Role != "user" || first.RedactedContent != "Hello [redacted]" {
		t.Fatal(first, err)
	}
	replay, err := c.Submit(ctx, view.ID, "request-one", input)
	if err != nil || replay.MessageID != first.MessageID {
		t.Fatal("idempotent message duplicated", replay, err)
	}
	input.Content = "Different content"
	if _, err = c.Submit(ctx, view.ID, "request-one", input); err != ErrScopeConflict {
		t.Fatal("changed request replayed", err)
	}
	input.ClientMessageID = "client-two"
	second, err := c.Submit(ctx, view.ID, "request-two", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(ctx, view.ID, second.MessageID); err != ErrScopeConflict || calls != 0 {
		t.Fatal("later message bypassed pending inference", calls, err)
	}
	answer, err := c.Generate(ctx, view.ID, first.MessageID)
	if err != nil || answer.Role != "assistant" || len(answer.TaskIDs) != 0 || answer.RedactedContent != "A text answer with [redacted]" {
		t.Fatal(answer, err)
	}
	if late("late text") != ErrToolDenied {
		t.Fatal("provider retained live output callback")
	}
	if _, err = c.Generate(ctx, view.ID, first.MessageID); err != ErrScopeConflict || calls != 1 {
		t.Fatal("completed inference replayed", calls, err)
	}
	if _, err = c.Generate(ctx, view.ID, second.MessageID); err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
	messages, err := c.Messages(ctx, view.ID, 0, 100)
	if err != nil || len(messages) != 4 || bytes.Contains(store.data, []byte("synthetic-secret")) {
		t.Fatal("message persistence or disclosure failure", len(messages), err)
	}
	events, err := c.Events(ctx, view.ID, 0, 100)
	if err != nil || len(events) != 8 {
		t.Fatal(events, err)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.SessionID != view.ID {
			t.Fatal("bad durable cursor", event)
		}
	}
	if _, err = c.Events(ctx, view.ID, 99, 10); err != ErrScopeConflict {
		t.Fatal("future cursor accepted", err)
	}
	actor.SubjectID = "other-owner"
	if _, err = c.Messages(ctx, view.ID, 0, 10); err != ErrToolDenied {
		t.Fatal("cross-owner messages", err)
	}
	if _, err = c.Events(ctx, view.ID, 0, 10); err != ErrToolDenied {
		t.Fatal("cross-owner events", err)
	}
}

func TestGenerationFailureRevocationDisableAndCommitAreNeverRetried(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"provider-error", "panic", "revoked", "disabled", "scope-change", "secret-output", "oversized-output", "ignored-emit-error", "commit-error"} {
		t.Run(mode, func(t *testing.T) {
			s, store, actor, _, _ := sessionFixture(t)
			view, err := s.Create(ctx)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var enabled *bool
			c, flag := conversationFixture(t, s, func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
				calls++
				switch mode {
				case "provider-error":
					return errors.New("sensitive upstream error must not persist")
				case "panic":
					panic("sensitive panic must not persist")
				case "revoked":
					actor.SubjectID = "other-owner"
				case "disabled":
					*enabled = false
				case "scope-change":
					if _, err := s.SetPolicy(ctx, view.ID, view.ScopeRevision, LowRisk, 0); err != nil {
						t.Fatal(err)
					}
				case "secret-output":
					return emit("PRIVATE KEY material")
				case "oversized-output":
					return emit(strings.Repeat("a", 128001))
				case "ignored-emit-error":
					_ = emit(strings.Repeat("a", 128001))
				case "commit-error":
					store.fail = true
				}
				return emit("answer")
			})
			enabled = flag
			message, err := c.Submit(ctx, view.ID, "request-one", MessageInput{"question", "client-one", view.ScopeRevision})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Generate(ctx, view.ID, message.MessageID); err != ErrGenerationUnknown {
				t.Fatal(mode, err)
			}
			actor.SubjectID, *enabled, store.fail = "owner", true, false
			if _, err = c.Generate(ctx, view.ID, message.MessageID); err != ErrGenerationUnknown || calls != 1 {
				t.Fatal("uncertain inference was retried", calls, err)
			}
			messages, err := c.Messages(ctx, view.ID, 0, 10)
			if err != nil || len(messages) != 1 || bytes.Contains(store.data, []byte("sensitive upstream")) || bytes.Contains(store.data, []byte("PRIVATE KEY")) {
				t.Fatal("failed inference invented answer or persisted secrets", len(messages), err)
			}
		})
	}
}

func TestInferenceReservationFailureDoesNotContactProvider(t *testing.T) {
	ctx := context.Background()
	s, store, _, _, _ := sessionFixture(t)
	view, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	c, _ := conversationFixture(t, s, func(_ context.Context, _ GenerationRequest, emit func(string) error) error {
		calls++
		return emit("answer")
	})
	message, err := c.Submit(ctx, view.ID, "request-one", MessageInput{"question", "client-one", view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	store.fail = true
	if _, err = c.Generate(ctx, view.ID, message.MessageID); err != ErrToolDenied && err != ErrSessionUnavailable {
		t.Fatal(err)
	}
	store.fail = false
	if calls != 0 {
		t.Fatal("provider contacted before durable reservation")
	}
	if _, err = c.Generate(ctx, view.ID, message.MessageID); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
}
