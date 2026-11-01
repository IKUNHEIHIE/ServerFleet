package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func streamFixture(text string) string {
	return fmt.Sprintf("event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"delta\":%q}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":%s}\n\n", text, providerResponse(text))
}
func TestOpenAIStreamTerminalAndBounds(t *testing.T) {
	good := streamFixture("local TLS response")
	if text, err := decodeProviderStream(context.Background(), strings.NewReader(good), "fixture-model"); err != nil || text != "local TLS response" {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, `"sequence_number":1`, `"sequence_number":0`, 1), strings.Split(good, "event: response.completed")[0], strings.Replace(good, `"delta":"local TLS response"`, `"delta":"altered"`, 1), good + good, strings.Replace(good, "response.output_text.delta", "response.function_call_arguments.delta", -1), strings.Repeat(":padding\n", 150000), strings.TrimSuffix(good, "\n\n")} {
		if text, err := decodeProviderStream(context.Background(), strings.NewReader(bad), "fixture-model"); err == nil || text != "" {
			t.Fatal("accepted incomplete, ambiguous or oversized stream")
		}
	}
}
func TestBYOKRuntimeLocalTLSSettingsQuotaAndRevocation(t *testing.T) {
	for _, mode := range []string{"success", "quota-denied", "revoked", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			var conversation *ConversationService
			var requests atomic.Int32
			var revoked atomic.Bool
			p, r, vault, _ := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.TLS == nil || request.Header.Get("Authorization") != "Bearer "+testBYOK || request.Header.Get("Accept") != "text/event-stream" {
					t.Error("wrong streaming boundary")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "revoked" {
					revoked.Store(true)
				}
				body := streamFixture("local TLS response")
				if mode == "disconnected" {
					body = strings.Split(body, "event: response.completed")[0]
				}
				io.WriteString(w, body)
			}, func(ctx context.Context, op auth.CredentialOperation) error {
				if revoked.Load() || conversation == nil {
					return ErrToolDenied
				}
				return conversation.CheckCredentialOperation(ctx, op)
			})
			s, _, actor, current, _ := sessionFixture(t)
			actor.SubjectID = r.SubjectID
			actor.Administrator = true
			current.SubjectID = r.SubjectID
			runtime, err := NewBYOKRuntime(s, vault, func(_ context.Context, _ SessionView, _ string, text string) (string, error) { return text, nil }, func(context.Context, GenerationRequest) error {
				if mode == "quota-denied" {
					return ErrToolDenied
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			conversation = runtime.Conversation
			runtime.provider.endpoint = p.endpoint
			runtime.provider.client.Transport = p.client.Transport
			ctx := context.Background()
			_, err = runtime.Models.Set(ctx, 0, ModelSettingsInput{Enabled: true, Provider: "openai-responses", Model: "fixture-model", CredentialRef: r.Profile.CredentialRef, CredentialRevision: 1})
			if err != nil {
				t.Fatal(err)
			}
			view, err := s.Create(ctx)
			if err != nil {
				t.Fatal(err)
			}
			message, err := conversation.Submit(ctx, view.ID, "request-id", MessageInput{Content: "synthetic local question", ClientMessageID: "client-id", ScopeRevision: view.ScopeRevision})
			if err != nil {
				t.Fatal(err)
			}
			var out Message
			if mode == "success" {
				h, e := NewGenerationHTTP(conversation, func(r *http.Request) (context.Context, error) { return r.Context(), nil }, func(context.Context) error { return nil })
				if e != nil {
					t.Fatal(e)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("POST", "https://fleet.test/api/v1/ai/sessions/"+view.ID+"/messages/"+message.MessageID+"/generate", nil))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				err = json.Unmarshal(w.Body.Bytes(), &out)
			} else {
				out, err = conversation.Generate(ctx, view.ID, message.MessageID)
			}
			if mode == "success" {
				if err != nil || out.RedactedContent != "local TLS response" || requests.Load() != 1 {
					t.Fatal(err, requests.Load())
				}
			} else {
				if err == nil || out.MessageID != "" {
					t.Fatal("failed gate disclosed response")
				}
				if mode == "quota-denied" && requests.Load() != 0 {
					t.Fatal("quota performed provider I/O")
				}
				if _, err = conversation.Generate(ctx, view.ID, message.MessageID); err == nil {
					t.Fatal("unknown generation retried")
				}
			}
		})
	}
}
