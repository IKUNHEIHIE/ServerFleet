package ai

import (
	"errors"
	"net/http"
)

// Candidate owner-scoped configuration route. Authenticate must validate the
// same current Cookie/CSRF/Origin as all other management mutations. No secret,
// endpoint URL, provider headers or runtime code is accepted or returned.
type ModelSettingsHTTP struct {
	models *ModelSettingsService
	auth   Authenticator
	mux    *http.ServeMux
}

func NewModelSettingsHTTP(models *ModelSettingsService, authenticate Authenticator) (*ModelSettingsHTTP, error) {
	if models == nil || authenticate == nil {
		return nil, ErrModelUnavailable
	}
	h := &ModelSettingsHTTP{models, authenticate, http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/ai/model-settings", h.get)
	h.mux.HandleFunc("PUT /api/v1/ai/model-settings", h.set)
	return h, nil
}
func (h *ModelSettingsHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil || r.URL.RawQuery != "" {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	p, err := h.auth(r)
	if err != nil || !exactID(p.SubjectID) || p.Recheck == nil {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	if r.Method == http.MethodPut && !p.Administrator {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	ctx := WithActor(r.Context(), SessionActor{SubjectID: p.SubjectID, HumanWeb: true, Administrator: p.Administrator, CanFullAccess: p.Administrator}, p.Recheck)
	h.mux.ServeHTTP(w, r.WithContext(ctx))
}
func (h *ModelSettingsHTTP) get(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	settings, err := h.models.Get(r.Context())
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, settings)
}
func (h *ModelSettingsHTTP) set(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision uint64 `json:"expectedRevision"`
		ModelSettingsInput
	}
	if !decodeAIBody(w, r, &input, 16<<10) {
		return
	}
	settings, err := h.models.Set(r.Context(), input.ExpectedRevision, input.ModelSettingsInput)
	if errors.Is(err, ErrModelRevisionConflict) {
		writeAIProblem(w, 409, "MODEL_REVISION_CONFLICT")
		return
	}
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, settings)
}
