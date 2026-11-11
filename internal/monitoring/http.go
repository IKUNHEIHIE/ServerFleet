package monitoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/notifications"
)

// Principal is supplied by Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

// Authenticator resolves the trusted principal or returns an error (mapped to 401).
type Authenticator func(*http.Request) (Principal, error)

// TrafficRisk is one visible quota threshold or expiry risk. It is produced by the
// quota owner through TrafficRiskSource; monitoring never reads quota storage.
type TrafficRisk struct {
	NodeID    string `json:"nodeId"`
	Kind      string `json:"kind"`
	Severity  string `json:"severity"`
	Estimated bool   `json:"estimated"`
}

// TrafficRiskSource is implemented by composition (the quota service adapter).
// A nil source makes the traffic-risks read fail closed with 503 instead of
// pretending no risks exist.
type TrafficRiskSource interface {
	ListTrafficRisks(ctx context.Context, subjectID string) ([]TrafficRisk, error)
}

// AuditActions returns the audit action catalog this domain exposes.
func AuditActions() []string {
	return append([]string(nil), ServiceActions()...)
}

type httpAPI struct {
	service       *Service
	risks         TrafficRiskSource
	authenticator Authenticator
	now           func() time.Time
}

// RegisterHTTP mounts the monitoring endpoints on the Controller mux.
// Authentication, CSRF and idempotency middleware remain Controller-owned.
func RegisterHTTP(mux *http.ServeMux, service *Service, risks TrafficRiskSource, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("monitoring: mux is required")
	}
	if service == nil {
		return errors.New("monitoring: service is required")
	}
	if authenticator == nil {
		return errors.New("monitoring: authenticator is required")
	}
	api := &httpAPI{service: service, risks: risks, authenticator: authenticator, now: service.now}
	mux.HandleFunc("GET /api/v1/monitoring/availability-monitors", api.listMonitors)
	mux.HandleFunc("POST /api/v1/monitoring/availability-monitors", api.createMonitor)
	mux.HandleFunc("PATCH /api/v1/monitoring/availability-monitors/{monitorId}", api.updateMonitor)
	mux.HandleFunc("GET /api/v1/monitoring/availability-monitors/{monitorId}/history", api.monitorHistory)
	mux.HandleFunc("POST /api/v1/monitoring/availability-monitors/{monitorId}/probe", api.probeMonitor)
	mux.HandleFunc("GET /api/v1/monitoring/traffic-risks", api.trafficRisks)
	// The v0.9 candidate contract and the web client use the unprefixed paths.
	mux.HandleFunc("GET /api/v1/availability-monitors", api.listMonitors)
	mux.HandleFunc("GET /api/v1/availability-monitors/{monitorId}/alerts", func(w http.ResponseWriter, r *http.Request) {
		p, ok := api.principal(w, r)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			writeFixed(w, 400, "invalid_request", "invalid query")
			return
		}
		events, e := api.service.AlertEvents(r.Context(), p.SubjectID, r.PathValue("monitorId"), 20)
		if e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": events})
	})
	mux.HandleFunc("POST /api/v1/availability-monitors", api.createMonitor)
	mux.HandleFunc("PATCH /api/v1/availability-monitors/{monitorId}", api.updateMonitor)
	mux.HandleFunc("GET /api/v1/availability-monitors/{monitorId}/history", api.monitorHistory)
	mux.HandleFunc("POST /api/v1/availability-monitors/{monitorId}/probe", api.probeMonitor)
	mux.HandleFunc("GET /api/v1/traffic-risks", api.trafficRisks)
	return nil
}

type monitorDTO struct {
	AutomationOwnerID   string                  `json:"automationOwnerId,omitempty"`
	AutomationRevision  uint64                  `json:"automationRevision,omitempty"`
	ID                  string                  `json:"id"`
	Name                string                  `json:"name"`
	Type                string                  `json:"type"`
	Target              string                  `json:"target"`
	IntervalSeconds     int                     `json:"intervalSeconds"`
	TimeoutSeconds      int                     `json:"timeoutSeconds"`
	Enabled             bool                    `json:"enabled"`
	State               string                  `json:"state"`
	FailureCount        int                     `json:"failureCount"`
	RecoveryCount       int                     `json:"recoveryCount"`
	NotifyChannels      []notifications.Channel `json:"notifyChannels"`
	LastCheckedAt       string                  `json:"lastCheckedAt,omitempty"`
	LastError           string                  `json:"lastError,omitempty"`
	MaintenanceUntil    string                  `json:"maintenanceUntil,omitempty"`
	ConsecutiveFailures int                     `json:"consecutiveFailures"`
	CreatedAt           string                  `json:"createdAt,omitempty"`
	UpdatedAt           string                  `json:"updatedAt,omitempty"`
}

func (api *httpAPI) listMonitors(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	monitors, err := api.service.List(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]monitorDTO, 0, len(monitors))
	for _, monitor := range monitors {
		items = append(items, monitorFromRecord(monitor))
	}
	writeJSON(w, http.StatusOK, items)
}

type monitorBody struct {
	ID               *string                  `json:"id"`
	Name             *string                  `json:"name"`
	Type             *string                  `json:"type"`
	Kind             *string                  `json:"kind"`
	Target           *string                  `json:"target"`
	IntervalSeconds  *int                     `json:"intervalSeconds"`
	TimeoutSeconds   *int                     `json:"timeoutSeconds"`
	Enabled          *bool                    `json:"enabled"`
	Paused           *bool                    `json:"paused"`
	State            *string                  `json:"state"`
	FailureCount     *int                     `json:"failureCount"`
	RecoveryCount    *int                     `json:"recoveryCount"`
	LastCheckedAt    json.RawMessage          `json:"lastCheckedAt"`
	SourceNodeID     *string                  `json:"sourceNodeId"`
	LinkedResource   json.RawMessage          `json:"linkedResource"`
	NotifyChannels   *[]notifications.Channel `json:"notifyChannels"`
	MaintenanceUntil *string                  `json:"maintenanceUntil"`
}

func (api *httpAPI) createMonitor(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body monitorBody
	if !decodeJSON(w, r, &body) {
		return
	}
	kind, paused := monitorKind(body), bodyPaused(body)
	if body.Name == nil || *body.Name == "" || body.Target == nil || *body.Target == "" ||
		body.IntervalSeconds == nil || body.TimeoutSeconds == nil || kind == "" {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "name, type, target, intervalSeconds and timeoutSeconds are required")
		return
	}
	id := ""
	if body.ID != nil {
		id = *body.ID
	}
	if id == "" {
		id = newMonitorID()
	}
	enabled := !paused
	if body.Enabled != nil && body.Paused == nil && body.State == nil {
		enabled = *body.Enabled
	}
	maintenance, ok := parseOptionalTime(w, body.MaintenanceUntil)
	if !ok {
		return
	}
	if body.SourceNodeID != nil && *body.SourceNodeID != "" {
		writeFixed(w, http.StatusBadRequest, "unsupported_probe_source", "background probes run on the Controller")
		return
	}
	failure, recovery := 1, 1
	if body.FailureCount != nil {
		failure = *body.FailureCount
		if failure < 1 {
			writeError(w, ErrInvalidMonitor)
			return
		}
	}
	if body.RecoveryCount != nil {
		recovery = *body.RecoveryCount
		if recovery < 1 {
			writeError(w, ErrInvalidMonitor)
			return
		}
	}
	var channels []notifications.Channel
	if body.NotifyChannels != nil {
		channels = *body.NotifyChannels
	}
	monitor, err := api.service.Create(r.Context(), principal.SubjectID, principal.RequestID, MonitorInput{
		ID: id, Name: *body.Name, Kind: Kind(kind), Target: *body.Target, FailureThreshold: failure, RecoveryThreshold: recovery, NotifyChannels: channels,
		IntervalSeconds: *body.IntervalSeconds, TimeoutSeconds: *body.TimeoutSeconds,
		Enabled: enabled, MaintenanceUntil: maintenance,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, monitorFromRecord(monitor))
}

func (api *httpAPI) updateMonitor(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body monitorBody
	if !decodeJSON(w, r, &body) {
		return
	}
	monitorID := r.PathValue("monitorId")
	if body.ID != nil && *body.ID != "" && *body.ID != monitorID {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "monitor id in the body does not match the path")
		return
	}
	current, err := api.service.Get(r.Context(), principal.SubjectID, monitorID)
	if err != nil {
		writeError(w, err)
		return
	}
	input := MonitorInput{
		ID: current.ID, Name: current.Name, Kind: current.Kind, Target: current.Target, FailureThreshold: current.FailureThreshold, RecoveryThreshold: current.RecoveryThreshold, NotifyChannels: current.NotifyChannels,
		IntervalSeconds: current.IntervalSeconds, TimeoutSeconds: current.TimeoutSeconds,
		Enabled: current.Enabled, MaintenanceUntil: current.MaintenanceUntil,
	}
	if body.SourceNodeID != nil && *body.SourceNodeID != "" {
		writeFixed(w, http.StatusBadRequest, "unsupported_probe_source", "background probes run on the Controller")
		return
	}
	if body.FailureCount != nil {
		input.FailureThreshold = *body.FailureCount
		if input.FailureThreshold < 1 {
			writeError(w, ErrInvalidMonitor)
			return
		}
	}
	if body.RecoveryCount != nil {
		input.RecoveryThreshold = *body.RecoveryCount
		if input.RecoveryThreshold < 1 {
			writeError(w, ErrInvalidMonitor)
			return
		}
	}
	if body.NotifyChannels != nil {
		input.NotifyChannels = *body.NotifyChannels
	}
	if body.Name != nil {
		input.Name = *body.Name
	}
	if kind := monitorKind(body); kind != "" {
		input.Kind = Kind(kind)
	}
	if body.Target != nil {
		input.Target = *body.Target
	}
	if body.IntervalSeconds != nil {
		input.IntervalSeconds = *body.IntervalSeconds
	}
	if body.TimeoutSeconds != nil {
		input.TimeoutSeconds = *body.TimeoutSeconds
	}
	if body.Paused != nil {
		input.Enabled = !*body.Paused
	} else if body.Enabled != nil {
		input.Enabled = *body.Enabled
	} else if body.State != nil {
		input.Enabled = *body.State != "paused"
	}
	if body.MaintenanceUntil != nil {
		maintenance, ok := parseOptionalTime(w, body.MaintenanceUntil)
		if !ok {
			return
		}
		input.MaintenanceUntil = maintenance
	}
	updated, err := api.service.Update(r.Context(), principal.SubjectID, principal.RequestID, monitorID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorFromRecord(updated))
}

type checkDTO struct {
	ID         int64    `json:"id"`
	MonitorID  string   `json:"monitorId"`
	Status     string   `json:"status"`
	LatencyMs  *float64 `json:"latencyMs,omitempty"`
	StatusCode *int     `json:"statusCode,omitempty"`
	Error      string   `json:"error,omitempty"`
	CheckedAt  string   `json:"checkedAt"`
}

func checkFromRecord(check Check) checkDTO {
	dto := checkDTO{
		ID: check.ID, MonitorID: check.MonitorID, Status: string(check.Status),
		StatusCode: check.StatusCode, Error: check.Error,
		CheckedAt: formatOptionalTime(check.CheckedAt),
	}
	if check.Latency != nil {
		milliseconds := float64(*check.Latency) / float64(time.Millisecond)
		dto.LatencyMs = &milliseconds
	}
	return dto
}

// monitorHistory returns persisted probe samples newest first. The read grant is
// checked by the service before any stored row is loaded.
func (api *httpAPI) monitorHistory(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	limit, ok := parseHistoryLimit(w, r)
	if !ok {
		return
	}
	monitorID := r.PathValue("monitorId")
	checks, err := api.service.ListChecks(r.Context(), principal.SubjectID, monitorID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]checkDTO, 0, len(checks))
	for _, check := range checks {
		items = append(items, checkFromRecord(check))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"monitorId": monitorID, "items": items, "limit": limit,
	})
}

type probeDTO struct {
	MonitorID           string   `json:"monitorId"`
	Status              string   `json:"status"`
	State               string   `json:"state"`
	LatencyMs           *float64 `json:"latencyMs,omitempty"`
	StatusCode          *int     `json:"statusCode,omitempty"`
	Error               string   `json:"error,omitempty"`
	CheckedAt           string   `json:"checkedAt"`
	ConsecutiveFailures int      `json:"consecutiveFailures"`
}

// probeMonitor runs one explicit, authorized probe through the SSRF/allowlist
// policed service and persists the sample. A missing Prober fails closed.
func (api *httpAPI) probeMonitor(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	monitorID := r.PathValue("monitorId")
	check, monitor, err := api.service.RunCheckNow(r.Context(), principal.SubjectID, principal.RequestID, monitorID)
	if err != nil {
		if errors.Is(err, ErrProberUnavailable) {
			writeFixed(w, http.StatusServiceUnavailable, "prober_unavailable", "monitoring prober is not configured")
			return
		}
		writeError(w, err)
		return
	}
	dto := probeDTO{
		MonitorID: monitor.ID, Status: string(check.Status), State: string(monitor.State),
		StatusCode: check.StatusCode, Error: check.Error,
		CheckedAt:           formatOptionalTime(check.CheckedAt),
		ConsecutiveFailures: monitor.ConsecutiveFailures,
	}
	if check.Latency != nil {
		milliseconds := float64(*check.Latency) / float64(time.Millisecond)
		dto.LatencyMs = &milliseconds
	}
	writeJSON(w, http.StatusOK, dto)
}

func parseHistoryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return DefaultCheckLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxCheckLimit {
		writeFixed(w, http.StatusBadRequest, "invalid_request",
			"limit must be an integer between 1 and "+strconv.Itoa(MaxCheckLimit))
		return 0, false
	}
	return limit, true
}

func (api *httpAPI) trafficRisks(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if api.risks == nil {
		writeFixed(w, http.StatusServiceUnavailable, "traffic_risk_source_unavailable",
			"traffic risk source is not configured")
		return
	}
	risks, err := api.risks.ListTrafficRisks(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if risks == nil {
		risks = []TrafficRisk{}
	}
	writeJSON(w, http.StatusOK, risks)
}

func monitorKind(body monitorBody) string {
	if body.Type != nil && *body.Type != "" {
		return *body.Type
	}
	if body.Kind != nil && *body.Kind != "" {
		return *body.Kind
	}
	return ""
}

func bodyPaused(body monitorBody) bool {
	if body.Paused != nil {
		return *body.Paused
	}
	if body.State != nil {
		return *body.State == "paused"
	}
	return false
}

func monitorFromRecord(monitor Monitor) monitorDTO {
	var state string
	if !monitor.Enabled {
		state = "paused"
	} else {
		switch monitor.State {
		case StateUp:
			state = "healthy"
		case StateDown:
			state = "unhealthy"
		default:
			state = "unknown"
		}
	}
	return monitorDTO{
		AutomationOwnerID: monitor.AutomationOwnerID, AutomationRevision: monitor.AutomationRevision,
		ID: monitor.ID, Name: monitor.Name, Type: string(monitor.Kind), Target: monitor.Target,
		IntervalSeconds: monitor.IntervalSeconds, TimeoutSeconds: monitor.TimeoutSeconds,
		Enabled: monitor.Enabled, State: state, FailureCount: normalizeThreshold(monitor.FailureThreshold), RecoveryCount: normalizeThreshold(monitor.RecoveryThreshold), NotifyChannels: monitor.NotifyChannels,
		LastCheckedAt: formatOptionalTime(monitor.LastCheckedAt), LastError: monitor.LastError,
		MaintenanceUntil:    formatOptionalTime(monitor.MaintenanceUntil),
		ConsecutiveFailures: monitor.ConsecutiveFailures,
		CreatedAt:           formatOptionalTime(monitor.CreatedAt), UpdatedAt: formatOptionalTime(monitor.UpdatedAt),
	}
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

func parseOptionalTime(w http.ResponseWriter, raw *string) (time.Time, bool) {
	if raw == nil || *raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "timestamp must be RFC3339")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func newMonitorID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("monitoring: entropy source failed")
	}
	return "mon_" + hex.EncodeToString(buffer)
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
	case errors.Is(err, ErrObservationChanged):
		writeFixed(w, http.StatusConflict, "monitor_changed", "monitor changed during probe; refresh before retrying")
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, ErrMonitorNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "monitor was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrMonitorExists):
		writeFixed(w, http.StatusConflict, "conflict", "monitor already exists")
	case errors.Is(err, ErrInvalidMonitor), errors.Is(err, ErrInvalidTarget), errors.Is(err, ErrTargetForbidden),
		errors.Is(err, ErrInvalidAllowedHosts), errors.Is(err, ErrInvalidLimit), errors.Is(err, ErrInvalidNow),
		errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, ErrProberUnavailable), errors.Is(err, ErrServiceNotConfigured):
		writeFixed(w, http.StatusServiceUnavailable, "monitoring_unavailable", "monitoring dependency is unavailable")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
