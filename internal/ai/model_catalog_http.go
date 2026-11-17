package ai

import (
	"context"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// ModelCatalogHTTP lists models for the saved owner endpoint and key. It never
// returns the endpoint credential or accepts a key in the request body.
type ModelCatalogHTTP struct {
	models  *ModelSettingsService
	runtime *BYOKRuntime
	auth    Authenticator
	mux     *http.ServeMux
}

func NewModelCatalogHTTP(models *ModelSettingsService, runtime *BYOKRuntime, authenticate Authenticator) (*ModelCatalogHTTP, error) {
	if models == nil || runtime == nil || authenticate == nil {
		return nil, ErrModelUnavailable
	}
	h := &ModelCatalogHTTP{models, runtime, authenticate, http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/ai/model-settings/models", h.list)
	h.mux.HandleFunc("POST /api/v1/ai/model-settings/probe", h.probe)
	return h, nil
}

func (h *ModelCatalogHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}

func (h *ModelCatalogHTTP) list(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	principal, err := h.auth(r)
	if err != nil || !exactID(principal.SubjectID) || principal.Recheck == nil || !principal.Administrator {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	ctx := WithActor(r.Context(), SessionActor{SubjectID: principal.SubjectID, HumanWeb: true, Administrator: true, CanFullAccess: true}, principal.Recheck)
	cfg, err := h.models.CatalogSettings(ctx)
	if err != nil {
		writeAIError(w, err)
		return
	}
	id, err := sessionID()
	if err != nil {
		writeAIError(w, ErrSessionUnavailable)
		return
	}
	op := auth.CredentialOperation{
		SubjectID:        principal.SubjectID,
		CredentialID:     cfg.CredentialRef,
		Module:           "ai",
		Source:           "ai",
		OperationID:      "ai-models-" + id,
		PlanDigest:       modelsPlanDigest(cfg.Endpoint),
		Action:           "use",
		ExpectedRevision: cfg.CredentialRevision,
	}
	items, err := h.runtime.ListModels(context.WithoutCancel(ctx), op, cfg.Endpoint)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, map[string]any{"items": items, "endpoint": cfg.Endpoint})
}

func (h *ModelCatalogHTTP) probe(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(50 * time.Second))
	if r.TLS == nil || r.URL.RawQuery != "" {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	principal, err := h.auth(r)
	if err != nil || !principal.Administrator || principal.Recheck == nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	var input struct {
		Model string `json:"model"`
	}
	if !decodeAgentBody(w, r, &input, 1024) {
		return
	}
	ctx := WithActor(r.Context(), SessionActor{SubjectID: principal.SubjectID, HumanWeb: true, Administrator: true, CanFullAccess: true}, principal.Recheck)
	cfg, err := h.models.CatalogSettings(ctx)
	if err != nil {
		writeAIError(w, err)
		return
	}
	if input.Model == "" {
		input.Model = cfg.Model
	}
	id, err := sessionID()
	if err != nil {
		writeAIError(w, err)
		return
	}
	op := auth.CredentialOperation{SubjectID: principal.SubjectID, CredentialID: cfg.CredentialRef, Module: "ai", Source: "ai", OperationID: "ai-models-probe-" + id, PlanDigest: modelsPlanDigest(cfg.Endpoint), Action: "use", ExpectedRevision: cfg.CredentialRevision}
	result, err := h.runtime.ProbeModel(ctx, op, cfg.Endpoint, input.Model)
	if err != nil {
		writeAIError(w, err)
		return
	}
	if _, err = h.models.CatalogSettings(ctx); err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, result)
}
