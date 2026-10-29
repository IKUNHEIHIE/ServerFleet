package servicegroups

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

type Principal struct {
	SubjectID string
	RequestID string
}

type Authenticator func(*http.Request) (Principal, error)

type httpAPI struct {
	service       *Service
	authenticator Authenticator
}

func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("servicegroups: mux is required")
	}
	if service == nil {
		return errors.New("servicegroups: service is required")
	}
	if authenticator == nil {
		return errors.New("servicegroups: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("GET /api/v1/service-groups", api.list)
	mux.HandleFunc("POST /api/v1/service-groups", api.create)
	mux.HandleFunc("GET /api/v1/service-groups/{serviceGroupId}", api.get)
	mux.HandleFunc("PATCH /api/v1/service-groups/{serviceGroupId}", api.update)
	mux.HandleFunc("DELETE /api/v1/service-groups/{serviceGroupId}", api.delete)
	mux.HandleFunc("PUT /api/v1/service-groups/{serviceGroupId}/members", api.replaceMembers)
	mux.HandleFunc("PATCH /api/v1/service-groups/{serviceGroupId}/members/{nodeId}/scheduling", api.scheduling)
	mux.HandleFunc("GET /api/v1/service-groups/{serviceGroupId}/dispatch-preview", api.preview)
	return nil
}

type groupDTO struct {
	ID            string     `json:"id"`
	Revision      uint64     `json:"revision"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	MemberNodeIDs []string   `json:"memberNodeIds"`
	Service       serviceDTO `json:"service"`
	LoadBalancing lbDTO      `json:"loadBalancing"`
	CreatedAt     string     `json:"createdAt,omitempty"`
}

type serviceDTO struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	HealthCheck string `json:"healthCheck,omitempty"`
}

type lbDTO struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

type inputDTO struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	MemberNodeIDs []string `json:"memberNodeIds"`
	Service       struct {
		Port        int    `json:"port"`
		Protocol    string `json:"protocol"`
		HealthCheck string `json:"healthCheck"`
	} `json:"service"`
	LoadBalancing struct {
		Enabled  bool   `json:"enabled"`
		Strategy string `json:"strategy"`
	} `json:"loadBalancing"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	records, err := api.service.ListVisible(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]groupDTO, 0, len(records))
	for _, record := range records {
		items = append(items, dtoFromRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body inputDTO
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := validateHTTPInput(body); err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
		return
	}
	input := inputFromDTO(body)
	input.ID = newGroupID()
	record, err := api.service.Create(r.Context(), principal.SubjectID, principal.RequestID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dtoFromRecord(record))
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	record, err := api.service.GetVisible(r.Context(), principal.SubjectID, r.PathValue("serviceGroupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromRecord(record))
}

func (api *httpAPI) update(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body inputDTO
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := validateHTTPInput(body); err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
		return
	}
	input := inputFromDTO(body)
	input.ID = r.PathValue("serviceGroupId")
	record, err := api.service.Update(r.Context(), principal.SubjectID, principal.RequestID, input.ID, body.ExpectedRevision, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromRecord(record))
}

func (api *httpAPI) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	record, err := api.service.GetVisible(r.Context(), principal.SubjectID, r.PathValue("serviceGroupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := api.service.Delete(r.Context(), principal.SubjectID, principal.RequestID, record.ID, record.Revision); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *httpAPI) replaceMembers(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		MemberNodeIDs    []string `json:"memberNodeIds"`
		ExpectedRevision uint64   `json:"expectedRevision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	record, err := api.service.ReplaceMembers(r.Context(), principal.SubjectID, principal.RequestID,
		r.PathValue("serviceGroupId"), body.ExpectedRevision, body.MemberNodeIDs)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromRecord(record))
}

func (api *httpAPI) scheduling(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Paused bool   `json:"paused"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	member, revision, err := api.service.SetScheduling(r.Context(), principal.SubjectID, principal.RequestID,
		r.PathValue("serviceGroupId"), r.PathValue("nodeId"), body.Paused, body.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodeId": member.NodeID, "paused": member.Paused, "reason": member.Reason,
		"revision": revision, "dispatchEligible": !member.Paused,
	})
}

func (api *httpAPI) preview(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	preview, err := api.service.Preview(r.Context(), principal.SubjectID, r.PathValue("serviceGroupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	excluded := make([]map[string]string, 0, len(preview.ExcludedNodes))
	for _, item := range preview.ExcludedNodes {
		excluded = append(excluded, map[string]string{"nodeId": item.NodeID, "reason": item.Reason})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serviceGroupId": preview.ServiceGroupID, "gatewayEligible": preview.GatewayEligible,
		"eligibleNodeIds": preview.EligibleNodeIDs, "excludedNodes": excluded, "policy": preview.Policy,
	})
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

func validateHTTPInput(body inputDTO) error {
	if body.Service.Port < 1 || body.Service.Port > 65535 {
		return ErrInvalid
	}
	switch body.Service.Protocol {
	case "http", "https", "tcp", "udp":
	default:
		return ErrInvalid
	}
	switch body.LoadBalancing.Strategy {
	case "round_robin", "least_connections", "none":
	default:
		return ErrInvalid
	}
	return nil
}

func inputFromDTO(body inputDTO) Input {
	return Input{
		Name: body.Name, Description: body.Description, MemberNodeIDs: body.MemberNodeIDs,
		Service:       ServiceConfig{Port: body.Service.Port, Protocol: body.Service.Protocol, HealthCheck: body.Service.HealthCheck},
		LoadBalancing: LoadBalancing{Enabled: body.LoadBalancing.Enabled, Strategy: body.LoadBalancing.Strategy},
	}
}

func dtoFromRecord(record Record) groupDTO {
	members := make([]string, 0, len(record.Members))
	for _, member := range record.Members {
		members = append(members, member.NodeID)
	}
	sort.Strings(members)
	return groupDTO{
		ID: record.ID, Revision: record.Revision, Name: record.Name, Description: record.Description,
		MemberNodeIDs: members,
		Service:       serviceDTO{Port: record.Service.Port, Protocol: record.Service.Protocol, HealthCheck: record.Service.HealthCheck},
		LoadBalancing: lbDTO{Enabled: record.LoadBalancing.Enabled, Strategy: record.LoadBalancing.Strategy},
		CreatedAt:     record.CreatedAt.UTC().Format(time.RFC3339),
	}
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
	case errors.Is(err, ErrNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "service group was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrNameTaken):
		writeFixed(w, http.StatusConflict, "conflict", "service group state changed")
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrUnknownMember), errors.Is(err, ErrMemberNotFound),
		errors.Is(err, ErrInvalidRevision), errors.Is(err, ErrRequestIDRequired),
		errors.Is(err, ErrDeleteGuardUnavailable):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func newGroupID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("servicegroups: entropy source failed")
	}
	return "sg_" + hex.EncodeToString(buffer)
}
