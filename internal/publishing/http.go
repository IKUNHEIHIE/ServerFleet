package publishing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// Principal is supplied by Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

// Authenticator resolves the trusted principal or returns an error (mapped to 401).
type Authenticator func(*http.Request) (Principal, error)

// AuditActions returns the audit action catalog this domain exposes.
func AuditActions() []string {
	return append([]string(nil), ServiceActions()...)
}

type httpAPI struct {
	service       *Service
	authenticator Authenticator
}

// RegisterHTTP mounts the publication preview and plan endpoints.
func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("publishing: mux is required")
	}
	if service == nil {
		return errors.New("publishing: service is required")
	}
	if authenticator == nil {
		return errors.New("publishing: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("POST /api/v1/publishing/preview", api.preview)
	mux.HandleFunc("GET /api/v1/publishing/publications", api.list)
	mux.HandleFunc("POST /api/v1/publishing/publications", api.create)
	mux.HandleFunc("GET /api/v1/publishing/publications/{publicationId}", api.get)
	mux.HandleFunc("POST /api/v1/publishing/publications/{publicationId}/run", api.run)
	mux.HandleFunc("POST /api/v1/publishing/publications/{publicationId}/rollback", api.rollback)
	mux.HandleFunc("POST /api/v1/publishing/publications/{publicationId}/rollback-all", api.rollbackAll)
	mux.HandleFunc("POST /api/v1/publishing/publications/{publicationId}/cancel", api.cancel)
	// The v0.9 candidate contract and the web client use the unprefixed paths.
	mux.HandleFunc("POST /api/v1/publications/preview", api.preview)
	mux.HandleFunc("GET /api/v1/publications", api.list)
	mux.HandleFunc("POST /api/v1/publications", api.create)
	mux.HandleFunc("GET /api/v1/publications/{publicationId}", api.get)
	mux.HandleFunc("POST /api/v1/publications/{publicationId}/run", api.run)
	mux.HandleFunc("POST /api/v1/publications/{publicationId}/rollback", api.rollback)
	mux.HandleFunc("POST /api/v1/publications/{publicationId}/rollback-all", api.rollbackAll)
	mux.HandleFunc("POST /api/v1/publications/{publicationId}/cancel", api.cancel)
	return nil
}

type draftBody struct {
	Name                  string   `json:"name"`
	ServiceGroupID        string   `json:"serviceGroupId"`
	GatewayIDs            []string `json:"gatewayIds"`
	DNSAccountID          string   `json:"dnsAccountId"`
	ZoneID                string   `json:"zoneId"`
	Domain                string   `json:"domain"`
	HTTPS                 *bool    `json:"https"`
	CertificateID         string   `json:"certificateId"`
	CreateMonitor         bool     `json:"createMonitor"`
	ExpectedGroupRevision uint64   `json:"expectedGroupRevision"`
	TargetRevision        uint64   `json:"targetRevision"`
}

type previewResponse struct {
	Eligible              bool           `json:"eligible"`
	TargetNodeIDsSnapshot []string       `json:"targetNodeIdsSnapshot"`
	Checks                []previewCheck `json:"checks"`
	Warnings              []string       `json:"warnings"`
}

type previewCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type asyncOperationDTO struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	CreatedAt        string     `json:"createdAt"`
	TargetsPersisted bool       `json:"targetsPersisted"`
	Stages           []stageDTO `json:"stages"`
}

type stageDTO struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func (api *httpAPI) preview(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body draftBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validDraft(w, body) {
		return
	}
	preview, err := api.service.Preview(r.Context(), principal.SubjectID, PreviewInput{
		ServiceGroupID: body.ServiceGroupID, ExpectedGroupRevision: body.ExpectedGroupRevision,
		Domain: body.Domain, GatewayIDs: body.GatewayIDs, ZoneID: body.ZoneID,
		CertificateID: body.CertificateID, HTTPS: *body.HTTPS, TargetRevision: body.TargetRevision,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	checks := make([]previewCheck, 0, len(preview.Checks))
	for _, check := range preview.Checks {
		checks = append(checks, previewCheck(check))
	}
	writeJSON(w, http.StatusOK, previewResponse{
		Eligible: preview.Eligible, TargetNodeIDsSnapshot: preview.TargetNodeIDs,
		Checks: checks, Warnings: preview.Warnings,
	})
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body draftBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validDraft(w, body) {
		return
	}
	preview, err := api.service.Preview(r.Context(), principal.SubjectID, PreviewInput{
		ServiceGroupID: body.ServiceGroupID, ExpectedGroupRevision: body.ExpectedGroupRevision,
		Domain: body.Domain, GatewayIDs: body.GatewayIDs, ZoneID: body.ZoneID,
		CertificateID: body.CertificateID, HTTPS: *body.HTTPS, TargetRevision: body.TargetRevision,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if !preview.Eligible {
		writeFixed(w, http.StatusConflict, "preview_ineligible",
			"publication preview reports ineligible; fix the failed checks first")
		return
	}
	revision := body.ExpectedGroupRevision
	if revision == 0 {
		revision = preview.GroupRevision
	}
	name := body.Name
	if name == "" {
		name = body.Domain
	}
	plan, err := api.service.Create(r.Context(), principal.SubjectID, principal.RequestID, CreateInput{
		ID: newPlanID(), Name: name, Domain: body.Domain, ServiceGroupID: body.ServiceGroupID,
		ExpectedGroupRevision: revision, GatewayIDs: body.GatewayIDs, ZoneID: body.ZoneID,
		CertificateID: body.CertificateID, HTTPS: *body.HTTPS, TargetRevision: body.TargetRevision,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	detailPlan, stages, err := api.service.Get(r.Context(), principal.SubjectID, plan.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, operationFromDetail(PlanDetail{Plan: detailPlan, Stages: stages}))
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	details, err := api.service.ListDetails(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]asyncOperationDTO, 0, len(details))
	for _, detail := range details {
		items = append(items, operationFromDetail(detail))
	}
	writeJSON(w, http.StatusOK, items)
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	plan, stages, err := api.service.Get(r.Context(), principal.SubjectID, r.PathValue("publicationId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operationFromDetail(PlanDetail{Plan: plan, Stages: stages}))
}

// run executes pending or failed stages in order. The response always carries
// the current persisted plan and stage rows, including when a stage fails, so an
// operator can distinguish a partial failure from a transport error.
func (api *httpAPI) run(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	detail, err := api.service.RunDetail(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("publicationId"))
	if err == nil {
		writeJSON(w, http.StatusOK, operationFromDetail(detail))
		return
	}
	if detail.Plan.ID == "" || isPublishingAuthorizationError(err) {
		writeError(w, err)
		return
	}
	writeJSON(w, publicationErrorStatus(err), operationWithError(detail, err))
}

// rollback compensates exactly one executed stage through its publisher port.
func (api *httpAPI) rollback(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Stage string `json:"stage"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Stage == "" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "stage is required")
		return
	}
	detail, err := api.service.RollbackDetail(r.Context(), principal.SubjectID, principal.RequestID,
		r.PathValue("publicationId"), StageKind(body.Stage))
	if err == nil {
		writeJSON(w, http.StatusOK, operationFromDetail(detail))
		return
	}
	if detail.Plan.ID == "" || isPublishingAuthorizationError(err) {
		writeError(w, err)
		return
	}
	writeJSON(w, publicationErrorStatus(err), operationWithError(detail, err))
}

// cancel stops a plan with no executed stages; it never claims compensation.
func (api *httpAPI) cancel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	plan, err := api.service.Cancel(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("publicationId"))
	if err != nil {
		writeError(w, err)
		return
	}
	_, stages, getErr := api.service.Get(r.Context(), principal.SubjectID, plan.ID)
	if getErr != nil {
		writeError(w, getErr)
		return
	}
	writeJSON(w, http.StatusOK, operationFromDetail(PlanDetail{Plan: plan, Stages: stages}))
}

// rollbackAll compensates every executed stage in reverse order. A failure stops
// the sequence and returns the persisted partial state.
func (api *httpAPI) rollbackAll(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	detail, err := api.service.RollbackAllDetail(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("publicationId"))
	if err == nil {
		writeJSON(w, http.StatusOK, operationFromDetail(detail))
		return
	}
	if detail.Plan.ID == "" || isPublishingAuthorizationError(err) {
		writeError(w, err)
		return
	}
	writeJSON(w, publicationErrorStatus(err), operationWithError(detail, err))
}

func operationWithError(detail PlanDetail, cause error) map[string]any {
	operation := operationFromDetail(detail)
	return map[string]any{
		"id": operation.ID, "status": operation.Status, "createdAt": operation.CreatedAt,
		"stages": operation.Stages,
		"error":  map[string]string{"code": publicationErrorCode(cause), "message": "publication operation failed"},
	}
}

func isPublishingAuthorizationError(err error) bool {
	return errors.Is(err, auth.ErrDenied) || errors.Is(err, auth.ErrInvalidCheck) || errors.Is(err, auth.ErrPolicyUnavailable)
}

func publicationErrorStatus(err error) int {
	switch {
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		return http.StatusForbidden
	case errors.Is(err, ErrPlanNotFound), errors.Is(err, ErrServiceGroupNotFound):
		return http.StatusNotFound
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrPlanExists), errors.Is(err, ErrDuplicatePlan),
		errors.Is(err, ErrPlanCompleted), errors.Is(err, ErrPlanCanceled), errors.Is(err, ErrPlanNotRunnable),
		errors.Is(err, ErrStageAlreadyRunning):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidPlan), errors.Is(err, ErrInvalidStage), errors.Is(err, ErrStageNotFound),
		errors.Is(err, ErrStageNotRollable), errors.Is(err, ErrRequestIDRequired):
		return http.StatusBadRequest
	case errors.Is(err, ErrGroupLookupFailed), errors.Is(err, ErrPublisherUnavailable), errors.Is(err, ErrStageFailed),
		errors.Is(err, ErrInvalidStageResult), errors.Is(err, ErrRollbackFailed):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func publicationErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		return "forbidden"
	case errors.Is(err, ErrPlanNotFound), errors.Is(err, ErrServiceGroupNotFound):
		return "not_found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "request_timeout"
	case errors.Is(err, ErrRevisionConflict):
		return "revision_conflict"
	case errors.Is(err, ErrPlanCompleted):
		return "plan_completed"
	case errors.Is(err, ErrPlanCanceled):
		return "plan_canceled"
	case errors.Is(err, ErrPlanNotRunnable):
		return "plan_not_runnable"
	case errors.Is(err, ErrStageAlreadyRunning):
		return "stage_already_running"
	case errors.Is(err, ErrStageNotFound):
		return "stage_not_found"
	case errors.Is(err, ErrStageNotRollable):
		return "stage_not_rollable"
	case errors.Is(err, ErrPublisherUnavailable):
		return "publisher_unavailable"
	case errors.Is(err, ErrStageFailed):
		return "stage_failed"
	case errors.Is(err, ErrInvalidStageResult):
		return "invalid_stage_result"
	case errors.Is(err, ErrRollbackFailed):
		return "rollback_failed"
	case errors.Is(err, ErrInvalidPlan), errors.Is(err, ErrInvalidStage), errors.Is(err, ErrRequestIDRequired):
		return "invalid_request"
	case errors.Is(err, ErrGroupLookupFailed):
		return "group_lookup_failed"
	default:
		return "internal_error"
	}
}

func validDraft(w http.ResponseWriter, body draftBody) bool {
	switch {
	case body.ServiceGroupID == "":
		writeFixed(w, http.StatusBadRequest, "invalid_request", "serviceGroupId is required")
		return false
	case len(body.GatewayIDs) == 0:
		writeFixed(w, http.StatusBadRequest, "invalid_request", "gatewayIds must contain at least one gateway")
		return false
	case body.Domain == "":
		writeFixed(w, http.StatusBadRequest, "invalid_request", "domain is required")
		return false
	case body.HTTPS == nil:
		writeFixed(w, http.StatusBadRequest, "invalid_request", "https is required")
		return false
	default:
		return true
	}
}

func operationFromDetail(detail PlanDetail) asyncOperationDTO {
	stages := make([]stageDTO, 0, len(detail.Stages))
	for _, stage := range detail.Stages {
		message := stage.ResultSummary
		if message == "" {
			message = stage.ErrorCode
		}
		stages = append(stages, stageDTO{Name: string(stage.Kind), Status: string(stage.Status), Message: message})
	}
	return asyncOperationDTO{
		ID: detail.Plan.ID, Status: operationStatus(detail.Plan.Status),
		CreatedAt: formatOptionalTime(detail.Plan.CreatedAt), TargetsPersisted: detail.Plan.TargetsPersisted,
		Stages: stages,
	}
}

func operationStatus(status PlanStatus) string {
	switch status {
	case PlanQueued:
		return "queued"
	case PlanRunning:
		return "running"
	case PlanCompleted:
		return "succeeded"
	case PlanPartialFailure:
		return "partially_completed"
	case PlanCanceled:
		return "failed"
	default:
		return "failed"
	}
}

func newPlanID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("publishing: entropy source failed")
	}
	return "pub_" + hex.EncodeToString(buffer)
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

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
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
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, ErrPlanNotFound), errors.Is(err, ErrServiceGroupNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrPlanExists), errors.Is(err, ErrDuplicatePlan),
		errors.Is(err, ErrPlanCompleted), errors.Is(err, ErrPlanCanceled), errors.Is(err, ErrPlanNotRunnable),
		errors.Is(err, ErrStageAlreadyRunning):
		writeFixed(w, http.StatusConflict, "conflict", "publication state changed")
	case errors.Is(err, ErrInvalidPlan), errors.Is(err, ErrInvalidStage), errors.Is(err, ErrStageNotFound),
		errors.Is(err, ErrStageNotRollable), errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, ErrGroupLookupFailed), errors.Is(err, ErrPublisherUnavailable), errors.Is(err, ErrStageFailed),
		errors.Is(err, ErrInvalidStageResult), errors.Is(err, ErrRollbackFailed):
		writeFixed(w, http.StatusBadGateway, "publishing_operation_failed", "publishing dependency failed")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
