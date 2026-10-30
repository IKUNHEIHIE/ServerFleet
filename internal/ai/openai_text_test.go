package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

const testBYOK = "synthetic-local-model-key"

func providerFixture(t *testing.T, handler http.HandlerFunc) (*OpenAITextProvider, GenerationRequest, *auth.CredentialService, *sessionTestStore) {
	t.Helper()
	ctx := context.Background()
	store := &sessionTestStore{}
	identity, err := auth.NewIdentityService(store)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.Bootstrap(ctx, setup, "admin", "synthetic-long-password"); err != nil {
		t.Fatal(err)
	}
	login, err := identity.Login(ctx, "admin", "synthetic-long-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// Actual cipher and service-use fences; explicit test-only policy gate.
	vault, err := auth.NewCredentialService(identity, "test-key", map[string][]byte{"test-key": bytes.Repeat([]byte{47}, 32)}, func(ctx context.Context, op auth.CredentialOperation) error {
		current, err := identity.Authenticate(ctx, login.Secret, "", false)
		if err != nil || current.SubjectID != op.SubjectID {
			return auth.ErrDenied
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := vault.Create(ctx, login.Secret, login.CSRF, auth.CredentialInput{Name: "synthetic provider", Kind: "ai_api_key", Secret: testBYOK, UsageScope: []string{"ai"}})
	if err != nil {
		t.Fatal(err)
	}
	r := GenerationRequest{SubjectID: login.Identity.SubjectID, SessionID: "session-one", MessageID: "message-one", OperationID: "ai-generation-message-one", ScopeRevision: 1, Profile: ModelProfile{"local-profile", "fixture-model", meta.ID, 1}, Messages: []Message{{MessageID: "message-one", SessionID: "session-one", Role: "user", RedactedContent: "already redacted question", CreatedAt: time.Now().UTC(), TaskIDs: []string{}}}}
	provider, err := NewOpenAITextProvider(vault, func(_ context.Context, r GenerationRequest, digest string) (auth.CredentialOperation, error) {
		return auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: r.Profile.CredentialRef, Module: "ai", Source: "ai", OperationID: r.OperationID, PlanDigest: digest, Action: "use", ExpectedRevision: meta.Revision}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Same-package tests replace PRIVATE endpoint/transport only. No public
	// insecure switch, baseURL or environment key exists in production.
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	provider.endpoint = server.URL + "/v1/responses"
	provider.client.Transport = server.Client().Transport
	t.Cleanup(provider.Close)
	return provider, r, vault, store
}

func providerResponse(text string) string {
	raw, _ := json.Marshal(map[string]any{"id": "resp_local", "object": "response", "status": "completed", "model": "fixture-model", "store": false, "error": nil, "incomplete_details": nil, "output": []any{map[string]any{"type": "reasoning", "summary": []any{}}, map[string]any{"type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}}})
	return string(raw)
}

func TestOpenAITextLocalTLSBorrowedCredentialAndSingleRequest(t *testing.T) {
	var requests atomic.Int32
	p, r, _, store := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.TLS == nil || request.Method != "POST" || request.URL.Path != "/v1/responses" || request.Header.Get("Authorization") != "Bearer "+testBYOK || request.Header.Get("Idempotency-Key") != "" {
			t.Error("wrong provider boundary")
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil || bytes.Contains(raw, []byte(testBYOK)) {
			t.Error("key in request body", err)
		}
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil || body["model"] != "fixture-model" || body["store"] != false || body["stream"] != false || body["background"] != false || body["tool_choice"] != "none" || body["truncation"] != "disabled" || len(body["tools"].([]any)) != 0 {
			t.Error("wrong text-only request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, providerResponse("explicit local provider double"))
	})
	var output string
	if err := p.Generate(context.Background(), r, func(s string) error { output += s; return nil }); err != nil || output == "" || requests.Load() != 1 {
		t.Fatal(err, requests.Load())
	}
	if err := p.Generate(context.Background(), r, func(string) error { t.Error("replayed output"); return nil }); err != ErrGenerationUnknown || requests.Load() != 1 {
		t.Fatal("credential use replayed network", err)
	}
	if bytes.Contains(store.data, []byte(testBYOK)) {
		t.Fatal("key persisted in plaintext")
	}
}

func TestOpenAITextRejectsPartialToolsMalformedAndSecretEcho(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"rate-limit", `{"error":"do not disclose"}`, 429}, {"server-error", `{"error":"do not disclose"}`, 500},
		{"incomplete", strings.Replace(providerResponse("partial"), `"status":"completed"`, `"status":"incomplete"`, 1), 200},
		{"stored", strings.Replace(providerResponse("text"), `"store":false`, `"store":true`, 1), 200},
		{"wrong-model", strings.ReplaceAll(providerResponse("text"), "fixture-model", "another-model"), 200},
		{"secret-echo", providerResponse(testBYOK), 200},
		{"tool", `{"id":"r","object":"response","model":"fixture-model","status":"completed","store":false,"output":[{"type":"function_call","name":"shell.root"}]}`, 200},
		{"duplicate", strings.Replace(providerResponse("text"), `"store":false`, `"store":true,"store":false`, 1), 200},
		{"alias", strings.Replace(providerResponse("text"), `"type":"output_text"`, `"Type":"output_text"`, 1), 200},
		{"trailing", providerResponse("text") + `{}`, 200}, {"large", strings.Repeat(" ", 1<<20) + providerResponse("text"), 200},
		{"refusal", `{"id":"r","object":"response","model":"fixture-model","status":"completed","store":false,"output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"no"}]}]}`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			p, r, _, _ := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			})
			if err := p.Generate(context.Background(), r, func(string) error { t.Error("invalid provider output emitted"); return nil }); err != ErrGenerationUnknown || requests.Load() != 1 {
				t.Fatal(err, requests.Load())
			}
			if err := p.Generate(context.Background(), r, func(string) error { t.Error("unknown replayed"); return nil }); err != ErrGenerationUnknown || requests.Load() != 1 {
				t.Fatal("unknown inference repeated", err)
			}
		})
	}
}

func TestOpenAITextRedirectAndCancellationDoNotReplay(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		var leaked atomic.Int32
		target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
		defer target.Close()
		p, r, _, _ := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		})
		if err := p.Generate(context.Background(), r, func(string) error { t.Error("redirect output"); return nil }); err != ErrGenerationUnknown || leaked.Load() != 0 {
			t.Fatal("redirect followed", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		var requests atomic.Int32
		p, r, _, _ := providerFixture(t, func(_ http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			select {
			case <-r.Context().Done():
			case <-time.After(300 * time.Millisecond):
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := p.Generate(ctx, r, func(string) error { t.Error("cancelled output"); return nil }); err != ErrGenerationUnknown || requests.Load() != 1 {
			t.Fatal(err, requests.Load())
		}
	})
}

func TestOpenAITextRevocationDuringIOWithholdsOutput(t *testing.T) {
	var vault *auth.CredentialService
	var request GenerationRequest
	p, r, v, _ := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if err := vault.Revoke(context.Background(), auth.CredentialOperation{SubjectID: request.SubjectID, CredentialID: request.Profile.CredentialRef, Module: "ai", Source: "ai", OperationID: "revoke-during-generation", PlanDigest: strings.Repeat("c", 64), Action: "revoke", ExpectedRevision: 1}); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, providerResponse("withhold this"))
	})
	vault, request = v, r
	if err := p.Generate(context.Background(), r, func(string) error { t.Error("revoked output disclosed"); return nil }); err != ErrGenerationUnknown {
		t.Fatal(err)
	}
}

func TestOpenAITextWrongBindingDoesNotContactProvider(t *testing.T) {
	var requests atomic.Int32
	p, r, _, _ := providerFixture(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
	if _, err := NewOpenAITextProvider(nil, nil); err != ErrModelUnavailable {
		t.Fatal(err)
	}
	resolver := p.resolve
	p.resolve = func(ctx context.Context, r GenerationRequest, digest string) (auth.CredentialOperation, error) {
		op, err := resolver(ctx, r, digest)
		op.Source = "system"
		return op, err
	}
	if err := p.Generate(context.Background(), r, func(string) error { return nil }); err != ErrToolDenied || requests.Load() != 0 {
		t.Fatal("wrong source borrowed key", err)
	}
	p.resolve = resolver
	r.Messages[0].Role = "system"
	if err := p.Generate(context.Background(), r, func(string) error { return nil }); err != ErrToolDenied || requests.Load() != 0 {
		t.Fatal("system injection reached provider", err)
	}
}

func TestOpenAITextConversationUsesActualCredentialFenceAndTLS(t *testing.T) {
	p, request, _, _ := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, providerResponse("provider answer; this does not create a fleet task"))
	})
	s, _, actor, current, _ := sessionFixture(t)
	actor.SubjectID = request.SubjectID
	current.SubjectID = request.SubjectID
	ctx := context.Background()
	view, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := NewConversationService(s, ConversationConfig{
		Profile: func(context.Context, string) (ModelProfile, error) { return request.Profile, nil },
		Filter: func(_ context.Context, _ SessionView, _ string, text string) (string, error) {
			return strings.ReplaceAll(text, "synthetic-secret", "[redacted]"), nil
		},
		Generate: p.Generate,
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := chat.Submit(ctx, view.ID, "request-one", MessageInput{"question synthetic-secret", "client-one", view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := chat.Generate(ctx, view.ID, message.MessageID)
	if err != nil || answer.Role != "assistant" || len(answer.TaskIDs) != 0 || answer.RedactedContent == "" {
		t.Fatal("actual local bridge failed", err)
	}
	if _, err = chat.Generate(ctx, view.ID, message.MessageID); err != ErrScopeConflict {
		t.Fatal("successful inference replayed", err)
	}
}
