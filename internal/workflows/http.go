package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"serverfleet.local/serverfleet/internal/auth"
)

// Permission keys registered by the Controller composition.
const (
	ActionRead  = "workflows.read"
	ActionWrite = "workflows.write"
)

// Operation names are the trusted composition actions inside the workflow engine.
const (
	OperationRead    = "workflows.read"
	OperationSave    = "workflows.save"
	OperationTrigger = "workflows.trigger"
	OperationAdvance = "workflows.advance"
)

// Principal is supplied by the Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

type principalKey struct{}

// WithPrincipal binds the authenticated actor for the composition Admit port.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// PrincipalFromContext returns the bound authenticated actor.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// Authenticator resolves the trusted principal or returns an error (401).
type Authenticator func(*http.Request) (Principal, error)

// HTTPConfig wires the Controller's authorization boundaries. Both callbacks are
// mandatory: subject-level workflow permissions and node-scoped action grants.
type HTTPConfig struct {
	Authenticate    Authenticator
	AuthorizeAction func(ctx context.Context, subjectID, action string) error
	AuthorizeNode   func(ctx context.Context, subjectID, action string, nodeIDs []string) error
}

type httpAPI struct {
	engine *Engine
	cfg    HTTPConfig
}

// RegisterHTTP mounts the workflow management surface. Task submission and
// approval remain in the sole task service; this layer never executes anything.
func RegisterHTTP(mux *http.ServeMux, engine *Engine, cfg HTTPConfig) error {
	if mux == nil {
		return errors.New("workflows: mux is required")
	}
	if engine == nil {
		return ErrWorkflowUnavailable
	}
	if cfg.Authenticate == nil || cfg.AuthorizeAction == nil || cfg.AuthorizeNode == nil {
		return ErrWorkflowUnavailable
	}
	api := &httpAPI{engine: engine, cfg: cfg}
	mux.HandleFunc("GET /api/v1/workflows", api.list)
	mux.HandleFunc("POST /api/v1/workflows", api.create)
	mux.HandleFunc("GET /api/v1/workflows/{workflowId}", api.get)
	mux.HandleFunc("PATCH /api/v1/workflows/{workflowId}", api.update)
	mux.HandleFunc("POST /api/v1/workflows/{workflowId}/runs", api.trigger)
	return nil
}

type stepView struct {
	ActionID      string          `json:"actionId"`
	NodeIDs       []string        `json:"nodeIds"`
	Parameters    json.RawMessage `json:"parameters"`
	HumanApproval bool            `json:"humanApproval,omitempty"`
}

type workflowView struct {
	ID       string            `json:"id"`
	Revision uint64            `json:"revision"`
	Enabled  bool              `json:"enabled"`
	Trigger  string            `json:"trigger"`
	Match    map[string]string `json:"match,omitempty"`
	Steps    []stepView        `json:"steps"`
}

type stepBody struct {
	ActionID      string          `json:"actionId"`
	NodeIDs       []string        `json:"nodeIds"`
	Parameters    json.RawMessage `json:"parameters"`
	HumanApproval bool            `json:"humanApproval"`
}

type createBody struct {
	Enabled *bool             `json:"enabled"`
	Trigger string            `json:"trigger"`
	Match   map[string]string `json:"match"`
	Steps   []stepBody        `json:"steps"`
}

type updateBody struct {
	ExpectedRevision uint64             `json:"expectedRevision"`
	Enabled          *bool              `json:"enabled"`
	Trigger          *string            `json:"trigger"`
	Match            *map[string]string `json:"match"`
	Steps            *[]stepBody        `json:"steps"`
}

type triggerBody struct {
	EventID string            `json:"eventId"`
	Kind    string            `json:"kind"`
	Values  map[string]string `json:"values"`
}

func workflowFromDefinition(def Definition) workflowView {
	view := workflowView{
		ID:       def.ID,
		Revision: def.Revision,
		Enabled:  def.Enabled,
		Trigger:  def.Trigger,
		Match:    map[string]string{},
		Steps:    make([]stepView, len(def.Steps)),
	}
	for key, value := range def.Match {
		view.Match[key] = value
	}
	for i, step := range def.Steps {
		view.Steps[i] = stepView{
			ActionID:      step.ActionID,
			NodeIDs:       append([]string(nil), step.NodeIDs...),
			Parameters:    append(json.RawMessage(nil), step.Parameters...),
			HumanApproval: step.HumanApproval,
		}
	}
	return view
}

func definitionFromBody(subjectID string, revision uint64, enabled bool, trigger string, match map[string]string, steps []stepBody) (Definition, error) {
	if !validID(trigger) || len(match) > 32 || len(steps) == 0 || len(steps) > 32 {
		return Definition{}, ErrWorkflowDenied
	}
	def := Definition{
		Revision:  revision,
		SubjectID: subjectID,
		Enabled:   enabled,
		Trigger:   trigger,
		Match:     map[string]string{},
		Steps:     make([]Step, len(steps)),
	}
	for key, value := range match {
		if !validID(key) || len(value) > 1024 {
			return Definition{}, ErrWorkflowDenied
		}
		def.Match[key] = value
	}
	for i, step := range steps {
		if !validID(step.ActionID) || len(step.NodeIDs) == 0 || len(step.NodeIDs) > 1000 {
			return Definition{}, ErrWorkflowDenied
		}
		params := step.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{}`)
		}
		var object map[string]string
		if len(params) > 64<<10 || json.Unmarshal(params, &object) != nil {
			return Definition{}, ErrWorkflowDenied
		}
		seen := make(map[string]struct{}, len(step.NodeIDs))
		for _, nodeID := range step.NodeIDs {
			if !validID(nodeID) {
				return Definition{}, ErrWorkflowDenied
			}
			if _, ok := seen[nodeID]; ok {
				return Definition{}, ErrWorkflowDenied
			}
			seen[nodeID] = struct{}{}
		}
		canonical, err := json.Marshal(object)
		if err != nil {
			return Definition{}, ErrWorkflowDenied
		}
		def.Steps[i] = Step{
			ActionID:      step.ActionID,
			NodeIDs:       append([]string(nil), step.NodeIDs...),
			Parameters:    canonical,
			HumanApproval: step.HumanApproval,
		}
	}
	return def, nil
}

func (api *httpAPI) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	principal, err := api.cfg.Authenticate(r)
	if err != nil || principal.SubjectID == "" {
		writeFixed(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return Principal{}, false
	}
	if principal.RequestID == "" {
		writeFixed(w, http.StatusBadRequest, "missing_request_id", "request id is required")
		return Principal{}, false
	}
	return principal, true
}

func (api *httpAPI) authorizeSubject(w http.ResponseWriter, ctx context.Context, subjectID, action string) bool {
	if err := api.cfg.AuthorizeAction(ctx, subjectID, action); err != nil {
		writeAuthorizationError(w, err)
		return false
	}
	return true
}

func (api *httpAPI) authorizeNodes(w http.ResponseWriter, ctx context.Context, subjectID string, steps []Step) bool {
	for _, step := range steps {
		if err := api.cfg.AuthorizeNode(ctx, subjectID, step.ActionID, step.NodeIDs); err != nil {
			writeAuthorizationError(w, err)
			return false
		}
	}
	return true
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	ctx := WithPrincipal(r.Context(), principal)
	if !api.authorizeSubject(w, ctx, principal.SubjectID, ActionRead) {
		return
	}
	definitions, err := api.engine.List(ctx, WorkflowOperation{ActorID: principal.SubjectID, Action: OperationRead})
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]workflowView, 0, len(definitions))
	for _, def := range definitions {
		items = append(items, workflowFromDefinition(def))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	ctx := WithPrincipal(r.Context(), principal)
	if !api.authorizeSubject(w, ctx, principal.SubjectID, ActionWrite) {
		return
	}
	var body createBody
	if !decodeJSON(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	def, err := definitionFromBody(principal.SubjectID, 1, enabled, body.Trigger, body.Match, body.Steps)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_workflow", "workflow definition is invalid")
		return
	}
	if !api.authorizeNodes(w, ctx, principal.SubjectID, def.Steps) {
		return
	}
	def.ID = newWorkflowID()
	saved, err := api.engine.Save(ctx, WorkflowOperation{
		ActorID: principal.SubjectID, Action: OperationSave, WorkflowID: def.ID,
	}, def)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/workflows/"+saved.ID)
	writeJSON(w, http.StatusCreated, workflowFromDefinition(saved))
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	ctx := WithPrincipal(r.Context(), principal)
	if !api.authorizeSubject(w, ctx, principal.SubjectID, ActionRead) {
		return
	}
	workflowID := r.PathValue("workflowId")
	def, err := api.engine.Get(ctx, WorkflowOperation{ActorID: principal.SubjectID, Action: OperationRead}, workflowID)
	if err != nil {
		writeNotFoundOrError(w, err)
		return
	}
	runs, err := api.engine.Runs(ctx, WorkflowOperation{ActorID: principal.SubjectID, Action: OperationRead}, workflowID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow": workflowFromDefinition(def),
		"runs":     runs,
	})
}

func (api *httpAPI) update(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	ctx := WithPrincipal(r.Context(), principal)
	if !api.authorizeSubject(w, ctx, principal.SubjectID, ActionWrite) {
		return
	}
	var body updateBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ExpectedRevision == 0 {
		writeFixed(w, http.StatusBadRequest, "invalid_workflow", "expectedRevision is required")
		return
	}
	workflowID := r.PathValue("workflowId")
	current, err := api.engine.Get(ctx, WorkflowOperation{ActorID: principal.SubjectID, Action: OperationRead}, workflowID)
	if err != nil {
		writeNotFoundOrError(w, err)
		return
	}
	trigger := current.Trigger
	if body.Trigger != nil {
		trigger = *body.Trigger
	}
	match := current.Match
	if body.Match != nil {
		match = *body.Match
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	stepsBody := make([]stepBody, 0, len(current.Steps))
	if body.Steps != nil {
		stepsBody = *body.Steps
	} else {
		for _, step := range current.Steps {
			stepsBody = append(stepsBody, stepBody(step))
		}
	}
	def, err := definitionFromBody(principal.SubjectID, body.ExpectedRevision+1, enabled, trigger, match, stepsBody)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_workflow", "workflow definition is invalid")
		return
	}
	if !api.authorizeNodes(w, ctx, principal.SubjectID, def.Steps) {
		return
	}
	def.ID = workflowID
	saved, err := api.engine.Save(ctx, WorkflowOperation{
		ActorID: principal.SubjectID, Action: OperationSave, WorkflowID: workflowID,
		ExpectedRevision: body.ExpectedRevision,
	}, def)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowFromDefinition(saved))
}

// trigger records a frozen plan and advances it into the sole task service. Any
// step action requiring approval lands in tasks as awaiting_approval; the engine
// has no execution path of its own.
func (api *httpAPI) trigger(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	ctx := WithPrincipal(r.Context(), principal)
	if !api.authorizeSubject(w, ctx, principal.SubjectID, ActionWrite) {
		return
	}
	var body triggerBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validID(body.EventID) || !validID(body.Kind) || len(body.Values) > 128 {
		writeFixed(w, http.StatusBadRequest, "invalid_event", "trigger event is invalid")
		return
	}
	for key, value := range body.Values {
		if !validID(key) || len(value) > 1024 {
			writeFixed(w, http.StatusBadRequest, "invalid_event", "trigger event is invalid")
			return
		}
	}
	workflowID := r.PathValue("workflowId")
	def, err := api.engine.Get(ctx, WorkflowOperation{ActorID: principal.SubjectID, Action: OperationRead}, workflowID)
	if err != nil {
		writeNotFoundOrError(w, err)
		return
	}
	if !def.Enabled {
		writeFixed(w, http.StatusConflict, "workflow_disabled", "workflow is disabled")
		return
	}
	if body.Kind != def.Trigger {
		writeJSON(w, http.StatusAccepted, map[string]any{"matched": false, "run": nil})
		return
	}
	if !api.authorizeNodes(w, ctx, principal.SubjectID, def.Steps) {
		return
	}
	op := WorkflowOperation{
		ActorID: principal.SubjectID, Action: OperationTrigger,
		WorkflowID: workflowID, EventID: body.EventID,
	}
	run, err := api.engine.Trigger(ctx, op, TriggerEvent{ID: body.EventID, Kind: body.Kind, Values: body.Values})
	if err != nil {
		writeError(w, err)
		return
	}
	if run == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"matched": false, "run": nil})
		return
	}
	advanced, err := api.engine.Advance(ctx, WorkflowOperation{
		ActorID: principal.SubjectID, Action: OperationAdvance,
		WorkflowID: workflowID, EventID: body.EventID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"matched": true, "run": advanced})
}

func newWorkflowID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("workflows: entropy source failed")
	}
	return "workflow-" + hex.EncodeToString(buffer)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if r.URL.RawQuery != "" || len(r.Header.Values("Content-Type")) != 1 {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && strings.ToLower(params["charset"]) != "utf-8" {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
		return false
	}
	for key := range params {
		if key != "charset" {
			writeFixed(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
			return false
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body must contain one object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeFixed(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeAuthorizationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	default:
		writeFixed(w, http.StatusServiceUnavailable, "workflows_unavailable", "authorization is unavailable")
	}
}

func writeNotFoundOrError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrWorkflowDenied) {
		writeFixed(w, http.StatusNotFound, "not_found", "workflow was not found")
		return
	}
	writeError(w, err)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrWorkflowDenied):
		writeFixed(w, http.StatusBadRequest, "invalid_workflow", "workflow definition or event is invalid")
	case errors.Is(err, ErrWorkflowConflict):
		writeFixed(w, http.StatusConflict, "conflict", "workflow revision or event changed")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrWorkflowUnavailable):
		writeFixed(w, http.StatusServiceUnavailable, "workflows_unavailable", "workflow dependency is unavailable")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
