package ai

import (
	"errors"
	"math"
	"net/http"
	"time"
)

type BudgetHTTP struct {
	budget       *PersistentGenerationBudget
	models       *ModelSettingsService
	authenticate Authenticator
	mux          *http.ServeMux
}

func NewBudgetHTTP(budget *PersistentGenerationBudget, models *ModelSettingsService, authenticate Authenticator) (*BudgetHTTP, error) {
	if budget == nil || models == nil || authenticate == nil {
		return nil, ErrModelUnavailable
	}
	h := &BudgetHTTP{budget, models, authenticate, http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/ai/model-settings/{profileId}/budget", h.get)
	h.mux.HandleFunc("PUT /api/v1/ai/model-settings/{profileId}/budget", h.put)
	return h, nil
}
func (h *BudgetHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil || r.URL.RawQuery != "" {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	p, err := h.authenticate(r)
	if err != nil || !exactID(p.SubjectID) || p.Recheck == nil || p.Recheck(r.Context()) != nil || r.Method == http.MethodPut && !p.Administrator {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	ctx := WithActor(r.Context(), SessionActor{SubjectID: p.SubjectID, HumanWeb: true, Administrator: p.Administrator}, p.Recheck)
	h.mux.ServeHTTP(w, r.WithContext(ctx))
}
func (h *BudgetHTTP) profile(w http.ResponseWriter, r *http.Request) bool {
	settings, err := h.models.Get(r.Context())
	if err != nil {
		writeAIError(w, err)
		return false
	}
	if !exactID(r.PathValue("profileId")) || settings.ID != r.PathValue("profileId") {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return false
	}
	return true
}
func (h *BudgetHTTP) get(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	if !h.profile(w, r) {
		return
	}
	view, err := h.budget.Read(r.Context(), r.PathValue("profileId"))
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, view)
}

type BudgetPolicyInput struct {
	ExpectedRevision uint64    `json:"expectedRevision"`
	WindowID         string    `json:"windowId"`
	StartsAt         time.Time `json:"startsAt"`
	EndsAt           time.Time `json:"endsAt"`
	RequestLimit     uint64    `json:"requestLimit"`
	InputByteLimit   uint64    `json:"inputByteLimit"`
	OutputTokenLimit uint64    `json:"outputTokenLimit"`
}

func (h *BudgetHTTP) put(w http.ResponseWriter, r *http.Request) {
	var input BudgetPolicyInput
	if !decodeAIBody(w, r, &input, 4096) {
		return
	}
	if !h.profile(w, r) {
		return
	}
	actor, _, ok := ActorFromContext(r.Context())
	if !ok {
		writeAIProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	policy := GenerationBudgetPolicy{OwnerID: actor.SubjectID, ProfileID: r.PathValue("profileId"), WindowID: input.WindowID, Revision: input.ExpectedRevision + 1, StartsAt: input.StartsAt, EndsAt: input.EndsAt, RequestLimit: input.RequestLimit, InputByteLimit: input.InputByteLimit, OutputTokenLimit: input.OutputTokenLimit}
	if input.ExpectedRevision == math.MaxUint64 || !validBudgetPolicy(policy) {
		writeAIProblem(w, 400, "INVALID_REQUEST")
		return
	}
	err := h.budget.Configure(r.Context(), actor.SubjectID, input.ExpectedRevision, policy)
	if errors.Is(err, ErrModelRevisionConflict) {
		writeAIProblem(w, 409, "BUDGET_REVISION_CONFLICT")
		return
	}
	if err != nil {
		writeAIError(w, err)
		return
	}
	view, err := h.budget.Read(r.Context(), policy.ProfileID)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, 200, view)
}
