package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerationHTTPPreviewAndEarlyFailure(t *testing.T) {
	for _, mode := range []string{"success", "unconfigured", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			loop, view, _, _ := agentLoopFixture(t, false, false)
			message, err := loop.conversation.Submit(ctx, view.ID, "http-preview", MessageInput{
				Content: "inspect", ClientMessageID: "http-preview", ScopeRevision: view.ScopeRevision,
			})
			if err != nil {
				t.Fatal(err)
			}
			loop.conversation.config.Generate = func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
				if err := generationPreviewFrom(ctx)("text", "tentative"); err != nil {
					return err
				}
				if mode == "interrupted" {
					return ErrGenerationUnknown
				}
				return emit("committed")
			}
			if mode == "unconfigured" {
				loop.conversation.config.Profile = func(context.Context, string) (ModelProfile, error) {
					return ModelProfile{}, ErrModelUnavailable
				}
			}
			handler, err := NewGenerationHTTP(loop.conversation, func(r *http.Request) (context.Context, error) {
				return r.Context(), nil
			}, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "https://fleet.test/api/v1/ai/sessions/"+view.ID+"/messages/"+message.MessageID+"/generate", nil)
			request.Header.Set("Accept", "text/event-stream")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			body := response.Body.String()
			if mode == "unconfigured" {
				if response.Code != 503 || strings.Contains(body, "event:") || !strings.Contains(body, "AI_SOURCE_UNAVAILABLE") {
					t.Fatal("early model failure lost its status", response.Code, body)
				}
				return
			}
			if response.Code != 200 || !strings.Contains(body, "event: preview") {
				t.Fatal(response.Code, body)
			}
			if mode == "interrupted" {
				if strings.Contains(body, "event: completed") || !strings.Contains(body, "GENERATION_OUTCOME_UNKNOWN") {
					t.Fatal("interrupted preview committed", body)
				}
			} else if !strings.Contains(body, "event: completed") || !strings.Contains(body, "committed") {
				t.Fatal("durable final missing", body)
			}
		})
	}
}
