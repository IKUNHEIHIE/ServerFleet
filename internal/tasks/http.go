package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// Principal is supplied by the Controller authentication middleware. Handlers
// never parse cookies, bearer tokens or CSRF themselves.
type Principal struct {
	SubjectID string
	RequestID string
}

// Authenticator resolves the trusted principal or returns an error (mapped to 401).
type Authenticator func(*http.Request) (Principal, error)

// GroupResolver freezes group membership for a task target. Composition adapts
// nodes/servicegroups; group targets fail closed without it.
type GroupResolver interface {
	ResolveTargets(ctx context.Context, targetType, groupID string) ([]string, error)
}

// ReadGate re-checks task visibility for every read and every SSE poll. An empty
// taskID asks for list visibility. A nil gate fails closed.
type ReadGate interface {
	AuthorizeTaskRead(ctx context.Context, subjectID, taskID string) error
}

// ClassResolver maps a trusted action id to its operation class. Without one all
// actions are treated as modifying and therefore require human approval.
type ClassResolver func(actionID string) (OperationClass, error)

type httpAPI struct {
	service       *Service
	authenticator Authenticator
	groups        GroupResolver
	classes       ClassResolver
	reads         ReadGate
}

// Register mounts the task endpoints on the Controller mux. CSRF, cookie/session
// authentication, rate limits and idempotency middleware remain Controller-owned.
func Register(mux *http.ServeMux, service *Service, authenticator Authenticator, groups GroupResolver, classes ClassResolver, reads ReadGate) error {
	if mux == nil {
		return errors.New("tasks: mux is required")
	}
	if service == nil {
		return errors.New("tasks: service is required")
	}
	if authenticator == nil {
		return errors.New("tasks: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator, groups: groups, classes: classes, reads: reads}
	mux.HandleFunc("POST /api/v1/tasks/preview", api.preview)
	mux.HandleFunc("POST /api/v1/tasks", api.create)
	mux.HandleFunc("GET /api/v1/tasks", api.list)
	mux.HandleFunc("GET /api/v1/tasks/{jobId}", api.get)
	mux.HandleFunc("POST /api/v1/tasks/{jobId}/approve", api.approve)
	mux.HandleFunc("POST /api/v1/tasks/{jobId}/cancel", api.cancel)
	mux.HandleFunc("POST /api/v1/tasks/{jobId}/resume", api.resume)
	mux.HandleFunc("GET /api/v1/tasks/{jobId}/executions", api.executions)
	mux.HandleFunc("GET /api/v1/tasks/{jobId}/events", api.events)
	return nil
}

type createRequest struct {
	Name   string `json:"name"`
	Target struct {
		Type    string   `json:"type"`
		NodeIDs []string `json:"nodeIds"`
		GroupID string   `json:"groupId"`
	} `json:"target"`
	Command      string `json:"command"`
	DryRun       bool   `json:"dryRun"`
	DispatchMode string `json:"dispatchMode"`
	ApprovalID   string `json:"approvalId"`
}

type previewResponse struct {
	TargetNodeIDsSnapshot []string `json:"targetNodeIdsSnapshot"`
	EligibleNodeIDs       []string `json:"eligibleNodeIds"`
	DeniedNodeIDs         []string `json:"deniedNodeIds"`
	RequiresApproval      bool     `json:"requiresApproval"`
}

type taskSummary struct {
	TaskID                string   `json:"taskId"`
	State                 string   `json:"state"`
	CreatedAt             string   `json:"createdAt"`
	CreatedBy             string   `json:"createdBy"`
	TargetNodeIDsSnapshot []string `json:"targetNodeIdsSnapshot"`
	Revision              uint64   `json:"revision"`
	PlanDigest            string   `json:"planDigest"`
	ExpiresAt             string   `json:"expiresAt,omitempty"`
}

type executionResult struct {
	NodeID string `json:"nodeId"`
	State  string `json:"state"`
	Error  string `json:"error,omitempty"`
}

type batchTaskJob struct {
	JobID                 string            `json:"jobId"`
	Name                  string            `json:"name,omitempty"`
	TargetType            string            `json:"targetType"`
	TargetResourceID      string            `json:"targetResourceId,omitempty"`
	TargetNodeIDsSnapshot []string          `json:"targetNodeIdsSnapshot"`
	State                 string            `json:"state"`
	Results               []executionResult `json:"results"`
	CreatedAt             string            `json:"createdAt"`
	Revision              uint64            `json:"revision"`
	PlanDigest            string            `json:"planDigest,omitempty"`
}

type page struct {
	Items      any     `json:"items"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

func (api *httpAPI) preview(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body createRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	nodes, class, err := api.resolveTargets(r.Context(), body)
	if err != nil {
		writeError(w, err)
		return
	}
	if body.Command == "" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "command is required")
		return
	}
	snapshot, eligible, denied, requires, err := api.service.Preview(r.Context(), principal.SubjectID, body.Command, class, nodes)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, previewResponse{
		TargetNodeIDsSnapshot: snapshot.IDs(), EligibleNodeIDs: eligible,
		DeniedNodeIDs: denied, RequiresApproval: requires,
	})
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body createRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ApprovalID != "" {
		writeFixed(w, http.StatusBadRequest, "unsupported_approval", "approve through the task approval endpoint")
		return
	}
	if body.DispatchMode == "parallel" {
		writeFixed(w, http.StatusBadRequest, "unsupported_dispatch", "parallel dispatch is not available")
		return
	}
	if body.Name == "" || body.Command == "" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "name and command are required")
		return
	}
	nodes, class, err := api.resolveTargets(r.Context(), body)
	if err != nil {
		writeError(w, err)
		return
	}
	if body.DryRun {
		snapshot, eligible, denied, requires, err := api.service.Preview(r.Context(), principal.SubjectID, body.Command, class, nodes)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, previewResponse{
			TargetNodeIDsSnapshot: snapshot.IDs(), EligibleNodeIDs: eligible,
			DeniedNodeIDs: denied, RequiresApproval: requires,
		})
		return
	}
	task, err := api.service.Submit(r.Context(), SubmitRequest{
		SubjectID: principal.SubjectID, RequestID: principal.RequestID,
		Name: body.Name, ActionID: body.Command, OperationClass: class,
		TargetType: body.Target.Type, TargetResourceID: body.Target.GroupID,
		TargetNodeIDs: nodes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, jobFromTask(task))
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal, "") {
		return
	}
	limit, offset, err := pageParams(r)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "invalid pagination")
		return
	}
	items, err := api.service.repo.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	summaries := make([]taskSummary, 0, len(items))
	for _, task := range items {
		summaries = append(summaries, summaryFromTask(task))
	}
	writeJSON(w, http.StatusOK, page{Items: summaries})
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal, r.PathValue("jobId")) {
		return
	}
	task, err := api.service.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobFromTask(task))
}

func (api *httpAPI) approve(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		PlanDigest string `json:"planDigest"`
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	task, err := api.service.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if body.PlanDigest == "" || body.PlanDigest != task.PlanHash {
		writeFixed(w, http.StatusConflict, "plan_changed", "task plan does not match the approval")
		return
	}
	approve := body.Decision == "approve"
	if !approve && body.Decision != "deny" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "decision must be approve or deny")
		return
	}
	updated, err := api.service.Decide(r.Context(), task.ID, task.Revision, Decision{
		ApproverID: principal.SubjectID, RequestID: principal.RequestID,
		Approve: approve, Reason: body.Reason,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summaryFromTask(updated))
}

func (api *httpAPI) cancel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason           string `json:"reason"`
		ExpectedRevision uint64 `json:"expectedRevision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	updated, err := api.service.Cancel(r.Context(), r.PathValue("jobId"), principal.SubjectID, principal.RequestID, body.ExpectedRevision)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summaryFromTask(updated))
}

func (api *httpAPI) resume(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason           string `json:"reason"`
		ExpectedRevision uint64 `json:"expectedRevision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	updated, err := api.service.Resume(r.Context(), r.PathValue("jobId"), principal.SubjectID, principal.RequestID,
		body.ExpectedRevision, body.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summaryFromTask(updated))
}

func (api *httpAPI) executions(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal, r.PathValue("jobId")) {
		return
	}
	task, err := api.service.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeError(w, err)
		return
	}
	results := make([]executionResult, 0, len(task.Targets))
	for _, target := range task.Targets {
		item := executionResult{NodeID: target.NodeID, State: executionState(target.State)}
		if target.Message != "" && target.State.Terminal() {
			item.Error = target.Message
		}
		results = append(results, item)
	}
	writeJSON(w, http.StatusOK, page{Items: results})
}

func (api *httpAPI) events(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal, r.PathValue("jobId")) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeFixed(w, http.StatusInternalServerError, "streaming_unavailable", "streaming is unavailable")
		return
	}
	after, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("Last-Event-ID")), 10, 64)
	if err != nil {
		after = 0
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if api.reads == nil || api.reads.AuthorizeTaskRead(r.Context(), principal.SubjectID, r.PathValue("jobId")) != nil {
			fmt.Fprint(w, "event: authorization_revoked\ndata: {\"code\":\"authorization_revoked\"}\n\n")
			flusher.Flush()
			return
		}
		events, err := api.service.Events(r.Context(), r.PathValue("jobId"), after, 200)
		if err != nil {
			if errors.Is(err, ErrCursorExpired) {
				fmt.Fprint(w, "event: cursor_expired\ndata: {\"code\":\"cursor_expired\"}\n\n")
				flusher.Flush()
			}
			return
		}
		for _, event := range events {
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, event.Payload)
			after = event.Seq
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (api *httpAPI) resolveTargets(ctx context.Context, body createRequest) ([]string, OperationClass, error) {
	class := OperationClassModifying
	if api.classes != nil {
		resolved, err := api.classes(body.Command)
		if err != nil {
			return nil, "", err
		}
		class = resolved
	}
	switch body.Target.Type {
	case "node_ids":
		if len(body.Target.NodeIDs) == 0 || body.Target.GroupID != "" {
			return nil, "", fmt.Errorf("%w: node target", ErrInvalidTask)
		}
		return body.Target.NodeIDs, class, nil
	case "node_group", "service_group":
		if api.groups == nil || body.Target.GroupID == "" {
			return nil, "", fmt.Errorf("%w: group target requires a resolver", ErrInvalidTask)
		}
		nodes, err := api.groups.ResolveTargets(ctx, body.Target.Type, body.Target.GroupID)
		if err != nil {
			return nil, "", err
		}
		return nodes, class, nil
	default:
		return nil, "", fmt.Errorf("%w: target type", ErrInvalidTask)
	}
}

func (api *httpAPI) authorizeRead(w http.ResponseWriter, r *http.Request, principal Principal, taskID string) bool {
	if api.reads == nil || api.reads.AuthorizeTaskRead(r.Context(), principal.SubjectID, taskID) != nil {
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
		return false
	}
	return true
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

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
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

func pageParams(r *http.Request) (int, int, error) {
	limit := 50
	offset := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			return 0, 0, ErrInvalidTask
		}
		limit = parsed
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, ErrInvalidTask
		}
		offset = parsed
	}
	return limit, offset, nil
}

func summaryFromTask(task Task) taskSummary {
	return taskSummary{
		TaskID: task.ID, State: apiState(task.Status), CreatedAt: formatTime(task.CreatedAt),
		CreatedBy: task.SubmitterID, TargetNodeIDsSnapshot: snapshotIDs(task),
		Revision: task.Revision, PlanDigest: task.PlanHash,
		ExpiresAt: formatTime(task.UnstartedDeadline),
	}
}

func snapshotIDs(task Task) []string {
	ordered := append([]Target(nil), task.Targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })
	ids := make([]string, 0, len(ordered))
	for _, target := range ordered {
		ids = append(ids, target.NodeID)
	}
	return ids
}

func jobFromTask(task Task) batchTaskJob {
	results := make([]executionResult, 0, len(task.Targets))
	for _, target := range task.Targets {
		item := executionResult{NodeID: target.NodeID, State: executionState(target.State)}
		if target.Message != "" && target.State.Terminal() {
			item.Error = target.Message
		}
		results = append(results, item)
	}
	return batchTaskJob{
		JobID: task.ID, Name: task.Name, TargetType: task.TargetType,
		TargetResourceID: task.TargetResourceID, TargetNodeIDsSnapshot: snapshotIDs(task),
		State: batchState(task.Status), Results: results, CreatedAt: formatTime(task.CreatedAt),
		Revision: task.Revision, PlanDigest: task.PlanHash,
	}
}

func apiState(status TaskStatus) string {
	switch status {
	case StatusAwaitingApproval:
		return "awaiting_approval"
	case StatusQueued:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusCancelling:
		return "canceling"
	case StatusCompleted:
		return "completed"
	case StatusPartialFailure:
		return "partially_failed"
	case StatusFailed:
		return "failed"
	case StatusCanceled:
		return "canceled"
	case StatusUnknown:
		return "unknown"
	case StatusExpired:
		return "expired"
	default:
		return "failed"
	}
}

func batchState(status TaskStatus) string {
	switch status {
	case StatusAwaitingApproval:
		return "awaiting_approval"
	case StatusQueued, StatusCancelling:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusCompleted, StatusPartialFailure:
		return "completed"
	case StatusCanceled:
		return "canceled"
	default:
		return "failed"
	}
}

func executionState(state TargetState) string {
	switch state {
	case TargetPending:
		return "queued"
	case TargetDispatched:
		return "acknowledged"
	case TargetSucceeded:
		return "succeeded"
	case TargetFailed:
		return "failed"
	case TargetCanceled:
		return "canceled"
	case TargetTimedOut:
		return "timed_out"
	case TargetUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeFixed(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrDenied):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, ErrTaskNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "task was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrApprovalInvalid), errors.Is(err, ErrSelfApproval),
		errors.Is(err, ErrInvalidStatus), errors.Is(err, ErrDuplicateResult), errors.Is(err, ErrIdempotencyMismatch),
		errors.Is(err, ErrTaskTerminal):
		writeFixed(w, http.StatusConflict, "conflict", "task state changed")
	case errors.Is(err, ErrInvalidTask), errors.Is(err, ErrUnknownOperationClass),
		errors.Is(err, ErrTargetNotFound), errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
