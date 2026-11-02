package certificates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// CleanupPrincipal is the authenticated caller for cleanup routes. RequestID is
// the transport request identity used by audit and authorization.
type CleanupPrincipal struct {
	SubjectID string
	RequestID string
}

// CleanupAuthenticator is supplied by the Controller composition. It must not
// parse Web-supplied subjects or trust query/body identity.
type CleanupAuthenticator func(*http.Request) (CleanupPrincipal, error)

// RegisterCleanupHTTP mounts the durable ACME cleanup read/retry/cancel routes.
// It is a candidate entry point; registration in the central router is a
// Controller patch.
func RegisterCleanupHTTP(mux *http.ServeMux, journal *CleanupJournal, authenticate CleanupAuthenticator) error {
	if mux == nil || journal == nil || authenticate == nil {
		return ErrCleanupUnavailable
	}
	api := &cleanupHTTP{journal: journal, authenticate: authenticate}
	mux.HandleFunc("GET /api/v1/certificates/cleanup", api.list)
	mux.HandleFunc("POST /api/v1/certificates/cleanup/{cleanupId}/retry", api.retry)
	mux.HandleFunc("POST /api/v1/certificates/cleanup/{cleanupId}/cancel", api.cancel)
	return nil
}

type cleanupHTTP struct {
	journal      *CleanupJournal
	authenticate CleanupAuthenticator
}

type cleanupResponse struct {
	ID            int64      `json:"id"`
	Domain        string     `json:"domain,omitempty"`
	FQDN          string     `json:"fqdn"`
	RecordID      string     `json:"recordId"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	LastErrorCode string     `json:"lastErrorCode,omitempty"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func cleanupDTO(record CleanupRecord) cleanupResponse {
	dto := cleanupResponse{
		ID: record.ID, Domain: record.Domain, FQDN: record.FQDN, RecordID: record.RecordID,
		State: string(record.State), Attempts: record.Attempts, LastErrorCode: record.LastErrorCode,
		UpdatedAt: record.UpdatedAt,
	}
	if !record.NextAttemptAt.IsZero() {
		next := record.NextAttemptAt
		dto.NextAttemptAt = &next
	}
	return dto
}

func (api *cleanupHTTP) principal(w http.ResponseWriter, r *http.Request) (CleanupPrincipal, bool) {
	principal, err := api.authenticate(r)
	if err != nil || principal.SubjectID == "" {
		writeCleanupError(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return CleanupPrincipal{}, false
	}
	if principal.RequestID == "" {
		principal.RequestID = "certificate-cleanup"
	}
	return principal, true
}

func (api *cleanupHTTP) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	records, err := api.journal.List(r.Context(), principal.SubjectID)
	if err != nil {
		writeCleanupError(w, cleanupStatus(err), "cleanup_list_failed", "cleanup records are unavailable")
		return
	}
	items := make([]cleanupResponse, 0, len(records))
	for _, record := range records {
		items = append(items, cleanupDTO(record))
	}
	writeCleanupJSON(w, http.StatusOK, items)
}

func (api *cleanupHTTP) retry(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	id, ok := cleanupID(w, r)
	if !ok {
		return
	}
	record, err := api.journal.Retry(r.Context(), principal.SubjectID, principal.RequestID, id)
	if err != nil {
		writeCleanupError(w, cleanupStatus(err), "cleanup_retry_failed", "cleanup retry failed")
		return
	}
	writeCleanupJSON(w, http.StatusOK, cleanupDTO(record))
}

func (api *cleanupHTTP) cancel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	id, ok := cleanupID(w, r)
	if !ok {
		return
	}
	record, err := api.journal.Cancel(r.Context(), principal.SubjectID, principal.RequestID, id)
	if err != nil {
		writeCleanupError(w, cleanupStatus(err), "cleanup_cancel_failed", "cleanup cancel failed")
		return
	}
	writeCleanupJSON(w, http.StatusOK, cleanupDTO(record))
}

func cleanupID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("cleanupId"), 10, 64)
	if err != nil || id < 1 {
		writeCleanupError(w, http.StatusBadRequest, "invalid_request", "cleanup id must be positive")
		return 0, false
	}
	return id, true
}

func cleanupStatus(err error) int {
	switch {
	case errors.Is(err, ErrCleanupNotFound):
		return http.StatusNotFound
	case errors.Is(err, auth.ErrDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrCleanupState), errors.Is(err, ErrCleanupExhausted),
		errors.Is(err, ErrCleanupBusy), errors.Is(err, ErrCleanupUnknown), errors.Is(err, ErrInvalidInput):
		return http.StatusConflict
	case errors.Is(err, ErrCleanupUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout
	default:
		return http.StatusBadGateway
	}
}

func writeCleanupJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeCleanupError(w http.ResponseWriter, status int, code, message string) {
	writeCleanupJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
