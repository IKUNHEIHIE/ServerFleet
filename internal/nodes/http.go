package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// Principal is supplied by Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

type Authenticator func(*http.Request) (Principal, error)

type httpAPI struct {
	service       *Service
	authenticator Authenticator
	now           func() time.Time
}

func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("nodes: mux is required")
	}
	if service == nil {
		return errors.New("nodes: service is required")
	}
	if authenticator == nil {
		return errors.New("nodes: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator, now: service.now}
	mux.HandleFunc("GET /api/v1/nodes", api.listNodes)
	mux.HandleFunc("GET /api/v1/groups", api.listGroups)
	mux.HandleFunc("POST /api/v1/groups", api.createGroup)
	mux.HandleFunc("GET /api/v1/groups/classifications", api.classifications)
	mux.HandleFunc("GET /api/v1/groups/{groupId}", api.getGroup)
	mux.HandleFunc("PATCH /api/v1/groups/{groupId}", api.updateGroup)
	mux.HandleFunc("DELETE /api/v1/groups/{groupId}", api.deleteGroup)
	mux.HandleFunc("PUT /api/v1/groups/{groupId}/members", api.replaceMembers)
	return nil
}

type nodeSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Online        bool     `json:"online"`
	Region        string   `json:"region,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	CountryCode   string   `json:"countryCode,omitempty"`
	IP            string   `json:"ip,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	CPUPercent    *float64 `json:"cpuPercent"`
	MemoryPercent *float64 `json:"memoryPercent"`
}

type groupDTO struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	MemberNodeIDs []string `json:"memberNodeIds"`
	Kind          string   `json:"kind"`
	Description   string   `json:"description,omitempty"`
	Revision      uint64   `json:"revision"`
	MemberCount   int      `json:"memberCount"`
}

func (api *httpAPI) listNodes(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	visible, err := api.service.ListVisible(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	onlineOnly := r.URL.Query().Get("onlineOnly") == "true"
	now := api.now()
	items := make([]nodeSummary, 0, len(visible))
	for _, node := range visible {
		if onlineOnly && node.Status != StatusOnline {
			continue
		}
		summary := nodeSummary{
			ID: node.ID, Name: node.Name, Online: node.Status == StatusOnline,
			Region: node.Region, Provider: node.Provider, CountryCode: node.CountryCode,
			IP: node.Address, Tags: append([]string(nil), node.Tags...),
		}
		if sample, ok := node.Usage(now, 5*time.Minute); ok {
			cpu, memory := sample.CPUPercent, sample.MemoryPercent
			summary.CPUPercent, summary.MemoryPercent = &cpu, &memory
		}
		items = append(items, summary)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	groups, err := api.service.ListVisibleGroups(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]groupDTO, 0, len(groups))
	for _, group := range groups {
		items = append(items, groupFromRecord(group))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) createGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	group, err := api.service.CreateGroup(r.Context(), principal.SubjectID, principal.RequestID, CreateGroupInput{
		ID: newID("grp"), Name: body.Name, Notes: body.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupFromRecord(group))
}

func (api *httpAPI) getGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	group, err := api.service.GetVisibleGroup(r.Context(), principal.SubjectID, r.PathValue("groupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupFromRecord(group))
}

func (api *httpAPI) updateGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Name             *string `json:"name"`
		Description      *string `json:"description"`
		ExpectedRevision uint64  `json:"expectedRevision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	current, err := api.service.GetVisibleGroup(r.Context(), principal.SubjectID, r.PathValue("groupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	name := current.Name
	notes := current.Notes
	if body.Name != nil {
		name = *body.Name
	}
	if body.Description != nil {
		notes = *body.Description
	}
	group, err := api.service.RenameGroup(r.Context(), principal.SubjectID, principal.RequestID, current.ID, RenameGroupInput{
		ExpectedRevision: body.ExpectedRevision, Name: name, Notes: notes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupFromRecord(group))
}

func (api *httpAPI) deleteGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	current, err := api.service.GetVisibleGroup(r.Context(), principal.SubjectID, r.PathValue("groupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	revision := current.Revision
	if raw := r.URL.Query().Get("expectedRevision"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "expectedRevision is invalid")
			return
		}
		revision = parsed
	}
	if err := api.service.DeleteGroup(r.Context(), principal.SubjectID, principal.RequestID, current.ID, revision); err != nil {
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
	group, err := api.service.ReplaceGroupMembers(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("groupId"), ReplaceGroupMembersInput{
		ExpectedRevision: body.ExpectedRevision, MemberNodeIDs: body.MemberNodeIDs,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupFromRecord(group))
}

func (api *httpAPI) classifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	dimension := Dimension(r.URL.Query().Get("dimension"))
	if !dimension.Valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "dimension is invalid")
		return
	}
	items, err := api.service.ClassifyVisible(r.Context(), principal.SubjectID, dimension)
	if err != nil {
		writeError(w, err)
		return
	}
	facets := make([]map[string]any, 0, len(items))
	for _, item := range items {
		facets = append(facets, map[string]any{"value": item.Value, "label": item.Value, "nodeCount": item.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"dimension": string(dimension), "items": facets})
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

func groupFromRecord(record GroupRecord) groupDTO {
	members := append([]string(nil), record.Members...)
	sort.Strings(members)
	return groupDTO{
		ID: record.ID, Name: record.Name, MemberNodeIDs: members, Kind: "manual",
		Description: record.Notes, Revision: record.Revision, MemberCount: len(members),
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
	case errors.Is(err, ErrGroupNotFound), errors.Is(err, ErrNodeNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrGroupNameTaken), errors.Is(err, ErrNodeExists):
		writeFixed(w, http.StatusConflict, "conflict", "resource state changed")
	case errors.Is(err, ErrInvalidNode), errors.Is(err, ErrInvalidSample), errors.Is(err, ErrUnknownNode),
		errors.Is(err, ErrGroupIDRequired), errors.Is(err, ErrGroupNameRequired), errors.Is(err, ErrInvalidRevision),
		errors.Is(err, ErrInvalidMember), errors.Is(err, ErrRequestIDRequired), errors.Is(err, ErrMissingSubject):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func newID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("nodes: entropy source failed")
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}
