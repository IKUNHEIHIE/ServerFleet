package ai

import (
	"context"
	"net/http"
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
	bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if h.commit(bounded) != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	result, err := h.conversation.Generate(bounded, r.PathValue("sessionId"), r.PathValue("messageId"))
	if err != nil {
		writeAIError(w, err)
		return
	}
	if h.commit(bounded) != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	writeAIJSON(w, 200, result)
}
