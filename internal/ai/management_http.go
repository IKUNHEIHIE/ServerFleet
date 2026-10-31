package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/auth"
)

// Principal is trusted Web composition data for AI management endpoints.
type Principal struct {
	SubjectID     string
	RequestID     string
	Administrator bool
	// Recheck revalidates the same request immediately before disclosure/commit.
	Recheck func(context.Context) error
}

type Authenticator func(*http.Request) (Principal, error)

// ManagementHTTP exposes the server-owned AI scope, approval and tool policy.
// It is a candidate management surface, not a model/tool runtime.
type ManagementHTTP struct {
	sessions *SessionService
	auth     Authenticator
	execute  ToolExecutor
	mux      *http.ServeMux
	now      func() time.Time
}

func NewManagementHTTP(sessions *SessionService, authenticate Authenticator, executor ToolExecutor) (*ManagementHTTP, error) {
	if sessions == nil || authenticate == nil || executor == nil {
		return nil, ErrSessionUnavailable
	}
	h := &ManagementHTTP{sessions: sessions, auth: authenticate, execute: executor, mux: http.NewServeMux(), now: time.Now}
	h.mux.HandleFunc("GET /api/v1/ai/security-policy", h.securityPolicy)
	h.mux.HandleFunc("POST /api/v1/ai/sessions", h.createSession)
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}", h.getSession)
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}/approval-policy", h.getApprovalPolicy)
	h.mux.HandleFunc("PATCH /api/v1/ai/sessions/{sessionId}/approval-policy", h.updateApprovalPolicy)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/grants", h.addGrant)
	h.mux.HandleFunc("DELETE /api/v1/ai/sessions/{sessionId}/grants/{grantId}", h.revokeGrant)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/approvals", h.requestApproval)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision", h.decideApproval)
	h.mux.HandleFunc("POST /api/v1/ai/tools/execute", h.executeTool)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/resource-recommendations", h.recommendations)
	return h, nil
}

func (h *ManagementHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}

func (h *ManagementHTTP) request(w http.ResponseWriter, r *http.Request, mutation bool) (context.Context, Principal, bool) {
	if r.TLS == nil {
		writeAIProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return nil, Principal{}, false
	}
	principal, err := h.auth(r)
	if err != nil {
		status := auth.IdentityHTTPStatus(err)
		if status == 0 {
			status = http.StatusServiceUnavailable
		}
		writeAIProblem(w, status, "AUTHENTICATION_REQUIRED")
		return nil, Principal{}, false
	}
	if !exactID(principal.SubjectID) || mutation && !principal.Administrator {
		writeAIProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return nil, Principal{}, false
	}
	actor := SessionActor{SubjectID: principal.SubjectID, HumanWeb: true, CanFullAccess: principal.Administrator, Administrator: principal.Administrator}
	return WithActor(r.Context(), actor, principal.Recheck), principal, true
}

func writeAIProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

func writeAIJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		writeAIProblem(w, http.StatusServiceUnavailable, "AI_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func decodeAIBody(w http.ResponseWriter, r *http.Request, value any, max int64) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	for key := range params {
		if key != "charset" {
			writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
			return false
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	return true
}

func writeAIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrToolDenied):
		writeAIProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
	case errors.Is(err, ErrApprovalRequired):
		writeAIProblem(w, http.StatusForbidden, "APPROVAL_REQUIRED")
	case errors.Is(err, ErrApprovalExpired):
		writeAIProblem(w, http.StatusConflict, "APPROVAL_EXPIRED_OR_STALE")
	case errors.Is(err, ErrScopeConflict):
		writeAIProblem(w, http.StatusConflict, "STALE_SCOPE_OR_REQUEST")
	case errors.Is(err, ErrModelRevisionConflict):
		writeAIProblem(w, http.StatusConflict, "MODEL_REVISION_CONFLICT")
	case errors.Is(err, ErrGenerationUnknown):
		writeAIProblem(w, http.StatusServiceUnavailable, "GENERATION_OUTCOME_UNKNOWN")
	case errors.Is(err, ErrModelUnavailable), errors.Is(err, ErrSessionUnavailable), errors.Is(err, ErrExecutionUnavailable):
		writeAIProblem(w, http.StatusServiceUnavailable, "AI_SOURCE_UNAVAILABLE")
	default:
		writeAIProblem(w, http.StatusServiceUnavailable, "AI_SOURCE_UNAVAILABLE")
	}
}

func (h *ManagementHTTP) securityPolicy(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.request(w, r, false); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writeAIJSON(w, http.StatusOK, map[string]any{
		"defaultDeny":                      true,
		"serverSideEnforced":               true,
		"groupGrantMemberPolicy":           "snapshot_and_live_intersection",
		"allowUnscopedDiscovery":           false,
		"scopeRevisionRequired":            true,
		"lateralMovementRestricted":        true,
		"approvalPolicyServerEnforced":     true,
		"noScopeExpansionFromApprovalMode": true,
	})
}

func (h *ManagementHTTP) createSession(w http.ResponseWriter, r *http.Request) {
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct{}
	if !decodeAIBody(w, r, &body, 4096) {
		return
	}
	view, err := h.sessions.Create(ctx)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusCreated, view)
}

func (h *ManagementHTTP) sessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("sessionId")
	if !exactID(id) {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return "", false
	}
	return id, true
}

func (h *ManagementHTTP) getSession(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	ctx, _, ok := h.request(w, r, false)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	view, err := h.sessions.Get(ctx, id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, view)
}

func (h *ManagementHTTP) getApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	ctx, _, ok := h.request(w, r, false)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	view, err := h.sessions.Get(ctx, id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, view.ApprovalPolicy)
}

func (h *ManagementHTTP) updateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	ctx, principal, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Mode                    string `json:"mode"`
		DurationSeconds         int    `json:"durationSeconds"`
		UserConfirmedFullAccess bool   `json:"userConfirmedFullAccess"`
	}
	if !decodeAIBody(w, r, &body, 64<<10) {
		return
	}
	var mode ApprovalMode
	minutes := 0
	switch body.Mode {
	case "per_action":
		mode = EveryCall
		if body.DurationSeconds != 0 {
			writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	case "automatic":
		mode = LowRisk
		if body.DurationSeconds != 0 {
			writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	case "full_access":
		if !principal.Administrator || !body.UserConfirmedFullAccess || body.DurationSeconds != 900 && body.DurationSeconds != 1800 && body.DurationSeconds != 3600 {
			writeAIProblem(w, http.StatusBadRequest, "FULL_ACCESS_CONFIRMATION_REQUIRED")
			return
		}
		mode = FullAccess
		minutes = body.DurationSeconds / 60
	default:
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	view, err := h.sessions.Get(ctx, id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	updated, err := h.sessions.SetPolicy(ctx, id, view.ScopeRevision, mode, minutes)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, updated.ApprovalPolicy)
}

func (h *ManagementHTTP) addGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct {
		ResourceType   string   `json:"resourceType"`
		ResourceID     string   `json:"resourceId"`
		AllowedActions []string `json:"allowedActions"`
	}
	if !decodeAIBody(w, r, &body, 64<<10) {
		return
	}
	view, err := h.sessions.Get(ctx, id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	updated, err := h.sessions.AddGrant(ctx, id, view.ScopeRevision, ResourceGrantInput{
		ResourceType:   body.ResourceType,
		ResourceID:     body.ResourceID,
		AllowedActions: body.AllowedActions,
	})
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, updated)
}

func (h *ManagementHTTP) revokeGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	grantID := r.PathValue("grantId")
	if !exactID(grantID) {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	view, err := h.sessions.Get(ctx, id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	updated, err := h.sessions.RevokeGrant(ctx, id, grantID, view.ScopeRevision)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, updated)
}

func (h *ManagementHTTP) requestApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct {
		ToolName      string          `json:"toolName"`
		Arguments     json.RawMessage `json:"arguments"`
		TargetNodeIDs []string        `json:"targetNodeIds"`
		ScopeRevision uint64          `json:"scopeRevision"`
	}
	if !decodeAIBody(w, r, &body, 256<<10) {
		return
	}
	view, err := h.sessions.RequestApproval(ctx, id, ApprovalInput{
		ToolName:      body.ToolName,
		Parameters:    body.Arguments,
		NodeIDs:       body.TargetNodeIDs,
		ScopeRevision: body.ScopeRevision,
	})
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusCreated, view)
}

func (h *ManagementHTTP) decideApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionID(w, r)
	if !ok {
		return
	}
	approvalID := r.PathValue("approvalId")
	if !exactID(approvalID) {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decodeAIBody(w, r, &body, 64<<10) {
		return
	}
	view, err := h.sessions.DecideApproval(ctx, id, approvalID, body.Decision, body.Note)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusOK, view)
}

func (h *ManagementHTTP) executeTool(w http.ResponseWriter, r *http.Request) {
	ctx, _, ok := h.request(w, r, true)
	if !ok {
		return
	}
	var body struct {
		SessionID     string          `json:"sessionId"`
		ScopeRevision uint64          `json:"scopeRevision"`
		ToolName      string          `json:"toolName"`
		TargetNodeIDs []string        `json:"targetNodeIds"`
		Arguments     json.RawMessage `json:"arguments"`
		ApprovalID    string          `json:"approvalId"`
	}
	if !decodeAIBody(w, r, &body, 256<<10) {
		return
	}
	if !exactID(body.SessionID) {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := h.sessions.Execute(ctx, body.SessionID, ToolExecutionInput{
		ToolName:      body.ToolName,
		Parameters:    body.Arguments,
		NodeIDs:       body.TargetNodeIDs,
		ScopeRevision: body.ScopeRevision,
	}, body.ApprovalID, h.execute)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeAIJSON(w, http.StatusAccepted, map[string]any{
		"taskId":           result.TaskID,
		"accepted":         true,
		"validatedNodeIds": append([]string{}, body.TargetNodeIDs...),
		"operation":        body.ToolName,
		"state":            "queued",
		"scopeRevision":    body.ScopeRevision,
	})
}

func (h *ManagementHTTP) recommendations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessionID(w, r); !ok {
		return
	}
	if _, _, ok := h.request(w, r, true); !ok {
		return
	}
	var body struct {
		Reason             string   `json:"reason"`
		NeededCapabilities []string `json:"neededCapabilities"`
	}
	if !decodeAIBody(w, r, &body, 64<<10) {
		return
	}
	if strings.TrimSpace(body.Reason) == "" || len(body.Reason) > 2000 || len(body.NeededCapabilities) > 32 {
		writeAIProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	// No recommendation engine is configured in this release. Returning an empty
	// metadata-only list is honest; it never creates a grant or expands scope.
	writeAIJSON(w, http.StatusOK, map[string]any{"suggestions": []any{}, "metadataOnly": true})
}
