package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func decodeAgentBody(w http.ResponseWriter, r *http.Request, value any, max int64) bool {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if e != nil || decodeAgentJSON(raw, value) != nil {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return false
	}
	var actual, canonical map[string]json.RawMessage
	b, e := json.Marshal(value)
	if e != nil || json.Unmarshal(raw, &actual) != nil || json.Unmarshal(b, &canonical) != nil {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return false
	}
	for key := range actual {
		if _, ok := canonical[key]; !ok {
			writeAIProblem(w, 400, "INVALID_REQUEST")
			return false
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return decodeAIBody(w, r, value, max)
}

type AgentHTTP struct {
	loop         *AgentLoop
	authenticate func(*http.Request) (context.Context, error)
	commit       func(context.Context) error
	mux          *http.ServeMux
}

func NewAgentHTTP(loop *AgentLoop, authenticate func(*http.Request) (context.Context, error), commit func(context.Context) error) (*AgentHTTP, error) {
	if loop == nil || authenticate == nil || commit == nil {
		return nil, ErrSessionUnavailable
	}
	h := &AgentHTTP{loop, authenticate, commit, http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}/runs", h.list)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/runs", h.create)
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}/runs/{runId}", h.get)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/runs/{runId}/advance", h.advance)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/runs/{runId}/cancel", h.cancel)
	return h, nil
}
func (h *AgentHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}
func (h *AgentHTTP) request(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	if r.TLS == nil || r.URL.RawQuery != "" || !exactID(r.PathValue("sessionId")) || r.PathValue("runId") != "" && !exactID(r.PathValue("runId")) {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return nil, false
	}
	ctx, e := h.authenticate(r)
	if e != nil || ctx == nil || ctx.Err() != nil || h.commit(ctx) != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return nil, false
	}
	return ctx, true
}
func (h *AgentHTTP) respond(w http.ResponseWriter, ctx context.Context, status int, result any, e error) {
	if e != nil {
		writeAIError(w, e)
		return
	}
	if h.commit(ctx) != nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	writeAIJSON(w, status, result)
}
func (h *AgentHTTP) create(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.request(w, r)
	if !ok {
		return
	}
	var in AgentRunInput
	if !decodeAgentBody(w, r, &in, 64<<10) {
		return
	}
	run, e := h.loop.Create(ctx, r.PathValue("sessionId"), in)
	if e == nil {
		run, e = h.loop.Drive(ctx, run.SessionID, run.ID, run.Revision)
	}
	h.respond(w, ctx, 202, run, e)
}
func (h *AgentHTTP) list(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.request(w, r)
	if !ok {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	items, e := h.loop.List(ctx, r.PathValue("sessionId"))
	h.respond(w, ctx, 200, map[string]any{"items": items}, e)
}
func (h *AgentHTTP) get(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.request(w, r)
	if !ok {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	v, e := h.loop.Get(ctx, r.PathValue("sessionId"), r.PathValue("runId"))
	h.respond(w, ctx, 200, v, e)
}
func (h *AgentHTTP) advance(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.request(w, r)
	if !ok {
		return
	}
	var in struct {
		Expected uint64 `json:"expectedRevision"`
	}
	if !decodeAgentBody(w, r, &in, 1024) {
		return
	}
	if in.Expected == 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	v, e := h.loop.Drive(ctx, r.PathValue("sessionId"), r.PathValue("runId"), in.Expected)
	h.respond(w, ctx, 202, v, e)
}
func (h *AgentHTTP) cancel(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.request(w, r)
	if !ok {
		return
	}
	var in struct {
		Expected uint64 `json:"expectedRevision"`
	}
	if !decodeAgentBody(w, r, &in, 1024) {
		return
	}
	if in.Expected == 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	v, e := h.loop.Cancel(ctx, r.PathValue("sessionId"), r.PathValue("runId"), in.Expected)
	h.respond(w, ctx, 200, v, e)
}
