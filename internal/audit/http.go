package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Principal is supplied by Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

type Authenticator func(*http.Request) (Principal, error)

// ReadGate authorizes audit reads. A nil gate fails closed.
type ReadGate interface {
	AuthorizeAuditRead(ctx context.Context, subjectID string) error
}

type httpAPI struct {
	recorder      *SQLRecorder
	authenticator Authenticator
	gate          ReadGate
}

// Register mounts the restricted audit read endpoints. There is deliberately no
// mutation route: retention is an internal operation, not an HTTP API.
func Register(mux *http.ServeMux, recorder *SQLRecorder, authenticator Authenticator, gate ReadGate) error {
	if mux == nil {
		return errors.New("audit: mux is required")
	}
	if recorder == nil {
		return errors.New("audit: recorder is required")
	}
	if authenticator == nil {
		return errors.New("audit: authenticator is required")
	}
	api := &httpAPI{recorder: recorder, authenticator: authenticator, gate: gate}
	mux.HandleFunc("GET /api/v1/audit/events", api.list)
	mux.HandleFunc("GET /api/v1/audit/events/{eventId}", api.get)
	return nil
}

type eventDTO struct {
	EventID         string `json:"eventId"`
	OccurredAt      string `json:"occurredAt"`
	ActorType       string `json:"actorType"`
	ActorID         string `json:"actorId,omitempty"`
	Action          string `json:"action"`
	ResourceType    string `json:"resourceType,omitempty"`
	ResourceID      string `json:"resourceId,omitempty"`
	Result          string `json:"result"`
	RequestID       string `json:"requestId"`
	TaskID          string `json:"taskId,omitempty"`
	RedactedSummary string `json:"redactedSummary,omitempty"`
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorize(w, r, principal) {
		return
	}
	query, err := queryFromRequest(r)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "query parameters are invalid")
		return
	}
	page, err := api.recorder.Query(r.Context(), query)
	if err != nil {
		if errors.Is(err, ErrInvalidQuery) {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "query parameters are invalid")
			return
		}
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
		return
	}
	items := make([]eventDTO, 0, len(page.Items))
	for _, event := range page.Items {
		items = append(items, dtoFromEvent(event))
	}
	var next *string
	if page.HasMore {
		cursor := strconv.Itoa(page.Offset + len(page.Items))
		next = &cursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorize(w, r, principal) {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("eventId")), 10, 64)
	if err != nil || id <= 0 {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "event id is invalid")
		return
	}
	event, err := api.recorder.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrEventNotFound) {
			writeFixed(w, http.StatusNotFound, "not_found", "event was not found")
			return
		}
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
		return
	}
	writeJSON(w, http.StatusOK, dtoFromEvent(event))
}

func (api *httpAPI) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	principal, err := api.authenticator(r)
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

func (api *httpAPI) authorize(w http.ResponseWriter, r *http.Request, principal Principal) bool {
	if api.gate == nil || api.gate.AuthorizeAuditRead(r.Context(), principal.SubjectID) != nil {
		denial := Event{
			SubjectID: principal.SubjectID, Action: "audit.read", Result: "denied",
			RequestID: principal.RequestID, Source: SourceWeb, ReasonCode: "read_denied",
			At: api.recorder.now().UTC(),
		}
		_ = api.recorder.Append(r.Context(), denial)
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
		return false
	}
	return true
}

func queryFromRequest(r *http.Request) (Query, error) {
	values := r.URL.Query()
	query := Query{
		SubjectID:  values.Get("actorId"),
		Action:     values.Get("action"),
		Result:     values.Get("result"),
		TaskID:     values.Get("taskId"),
		ResourceID: values.Get("resourceId"),
	}
	if source := values.Get("source"); source != "" {
		query.Source = Source(source)
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return Query{}, ErrInvalidQuery
		}
		query.Limit = limit
	}
	if raw := values.Get("cursor"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return Query{}, ErrInvalidQuery
		}
		query.Offset = offset
	}
	if raw := values.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Query{}, ErrInvalidQuery
		}
		query.Since = parsed
	}
	if raw := values.Get("until"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Query{}, ErrInvalidQuery
		}
		query.Until = parsed
	}
	return query, nil
}

func dtoFromEvent(event Event) eventDTO {
	actorType := "system"
	switch event.Source {
	case SourceWeb:
		actorType = "user"
	case SourceAI:
		actorType = "ai"
	case SourcePlugin:
		actorType = "plugin"
	case SourceAgent:
		actorType = "agent"
	}
	resourceType, resourceID := "", ""
	if event.TaskID != "" {
		resourceType, resourceID = "task", event.TaskID
	} else if len(event.NodeIDs) > 0 {
		resourceType, resourceID = "node", event.NodeIDs[0]
	}
	return eventDTO{
		EventID: strconv.FormatInt(event.ID, 10), OccurredAt: event.At.UTC().Format(time.RFC3339),
		ActorType: actorType, ActorID: event.SubjectID, Action: event.Action,
		ResourceType: resourceType, ResourceID: resourceID, Result: event.Result,
		RequestID: event.RequestID, TaskID: event.TaskID, RedactedSummary: event.ReasonCode,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeFixed(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
