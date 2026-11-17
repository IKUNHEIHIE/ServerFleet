package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GenerationHTTP is a separately mounted candidate action. A queued message is
// never implicitly retried. A repeated generating/unknown request remains denied
// by the conversation's durable admission fence. Response is verified text only.
type GenerationHTTP struct {
	conversation *ConversationService
	authenticate func(*http.Request) (context.Context, error)
	commit       func(context.Context) error
	mux          *http.ServeMux
}

func NewGenerationHTTP(conversation *ConversationService, authenticate func(*http.Request) (context.Context, error), commit func(context.Context) error) (*GenerationHTTP, error) {
	if conversation == nil || authenticate == nil || commit == nil {
		return nil, ErrModelUnavailable
	}
	h := &GenerationHTTP{conversation, authenticate, commit, http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate", h.generate)
	return h, nil
}
func (h *GenerationHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}
func (h *GenerationHTTP) generate(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) != 0 || !exactID(r.PathValue("sessionId")) || !exactID(r.PathValue("messageId")) {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	ctx, err := h.authenticate(r)
	if err != nil || ctx == nil || ctx.Err() != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 185*time.Second)
	defer cancel()
	if h.commit(bounded) != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	streaming := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	started := false
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(190 * time.Second))
	send := func(kind string, value any) error {
		if h.commit(bounded) != nil {
			return ErrToolDenied
		}
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(200)
			started = true
		}
		raw, e := json.Marshal(value)
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw); e != nil {
			return e
		}
		return http.NewResponseController(w).Flush()
	}
	if streaming {
		bounded = withGenerationPreview(bounded, func(kind, value string) error { return send("preview", map[string]string{"kind": kind, "text": value}) })
	}
	result, err := h.conversation.Generate(bounded, r.PathValue("sessionId"), r.PathValue("messageId"))
	if err != nil {
		if started {
			_ = send("error", map[string]string{"code": "GENERATION_OUTCOME_UNKNOWN"})
			return
		}
		writeAIError(w, err)
		return
	}
	if h.commit(bounded) != nil {
		if !started {
			writeAIProblem(w, 403, "NOT_AUTHORIZED")
		}
		return
	}
	if streaming {
		_ = send("completed", result)
		return
	}
	writeAIJSON(w, 200, result)
}
