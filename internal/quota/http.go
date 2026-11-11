package quota

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
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

// RegisterHTTP mounts the per-node traffic quota endpoints on the Controller mux.
func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("quota: mux is required")
	}
	if service == nil {
		return errors.New("quota: service is required")
	}
	if authenticator == nil {
		return errors.New("quota: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("GET /api/v1/nodes/{nodeId}/traffic-quota", api.getPolicy)
	mux.HandleFunc("PUT /api/v1/nodes/{nodeId}/traffic-quota", api.putPolicy)
	return nil
}

type trafficQuotaDTO struct {
	ExpiryType         string           `json:"expiryType,omitempty"`
	RemindDays         []int            `json:"remindDays,omitempty"`
	CycleResetDay      int              `json:"cycleResetDay"`
	Timezone           string           `json:"timezone"`
	DataComplete       bool             `json:"dataComplete"`
	Coverage           *CounterCoverage `json:"coverage,omitempty"`
	AutomationOwnerID  string           `json:"automationOwnerId,omitempty"`
	AutomationRevision uint64           `json:"automationRevision,omitempty"`
	NodeID             string           `json:"nodeId"`
	QuotaBytes         int64            `json:"quotaBytes"`
	QuotaGb            float64          `json:"quotaGb"`
	UsedBytes          *float64         `json:"usedBytes,omitempty"`
	UsedGb             *float64         `json:"usedGb,omitempty"`
	WarningPercent     int              `json:"warningPercent"`
	ThresholdsPct      []int            `json:"thresholdsPct"`
	ExpiresAt          string           `json:"expiresAt,omitempty"`
	Estimated          bool             `json:"estimated"`
	Direction          string           `json:"direction"`
	Source             string           `json:"source,omitempty"`
	UpdatedAt          string           `json:"updatedAt,omitempty"`
}

func (api *httpAPI) getPolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	nodeID := r.PathValue("nodeId")
	policy, found, err := api.service.GetPolicy(r.Context(), principal.SubjectID, nodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeFixed(w, http.StatusNotFound, "quota_not_found", "no traffic quota is configured for this node")
		return
	}
	usage, hasUsage, err := api.service.LatestUsage(r.Context(), principal.SubjectID, nodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	dto := quotaFromPolicy(policy)
	period, _, periodErr := PolicyPeriod(api.service.now(), policy)
	if periodErr != nil {
		writeError(w, periodErr)
		return
	}
	if coverage, found, e := api.service.CounterCoverage(r.Context(), principal.SubjectID, nodeID, period); e != nil {
		writeError(w, e)
		return
	} else if found {
		dto.Coverage = &coverage
	}

	if hasUsage && api.service.now().Before(usage.PeriodEnd) && !api.service.now().Before(usage.PeriodStart) {
		dto.Estimated = dto.Estimated || usage.Source == SourceAgent
		dto.DataComplete = usage.Source == SourceProvider
		if coverage, found, e := api.service.CounterCoverage(r.Context(), principal.SubjectID, nodeID, usage.PeriodStart); e != nil {
			writeError(w, e)
			return
		} else if found {
			dto.Coverage = &coverage
		}
		used := policy.used(usage)
		dto.UsedBytes = &used
		gb := bytesToGB(used)
		dto.UsedGb = &gb
		dto.Source = string(usage.Source)
	}
	writeJSON(w, http.StatusOK, dto)
}

type quotaBody struct {
	NodeID            *string  `json:"nodeId"`
	MonthlyLimitBytes *int64   `json:"monthlyLimitBytes"`
	QuotaBytes        *int64   `json:"quotaBytes"`
	QuotaGb           *float64 `json:"quotaGb"`
	WarningPercent    *int     `json:"warningPercent"`
	ThresholdsPct     []int    `json:"thresholdsPct"`
	ExpiryAt          *string  `json:"expiryAt"`
	ExpiresAt         *string  `json:"expiresAt"`
	Estimated         *bool    `json:"estimated"`
	// Accounting and reminder configuration is validated and persisted.
	Direction            *string  `json:"direction"`
	Source               *string  `json:"source"`
	CycleResetDay        *int     `json:"cycleResetDay"`
	Timezone             *string  `json:"timezone"`
	DataComplete         *bool    `json:"dataComplete"`
	ExpiryType           *string  `json:"expiryType"`
	RemindDays           []int    `json:"remindDays"`
	PredictionGb         *float64 `json:"predictionGb"`
	PredictionConfidence *string  `json:"predictionConfidence"`
}

func (api *httpAPI) putPolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	nodeID := r.PathValue("nodeId")
	var body quotaBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.NodeID != nil && *body.NodeID != "" && *body.NodeID != nodeID {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "node id in the body does not match the path")
		return
	}

	limit, ok := limitBytes(w, body)
	if !ok {
		return
	}
	warning, ok := warningPercent(w, body)
	if !ok {
		return
	}
	expiry, ok := parseExpiry(w, body)
	if !ok {
		return
	}
	estimated := false
	if body.Estimated != nil {
		estimated = *body.Estimated
	}
	resetDay := 1
	if body.CycleResetDay != nil {
		resetDay = *body.CycleResetDay
	}
	timezone := "UTC"
	if body.Timezone != nil {
		timezone = *body.Timezone
	}
	direction := "combined"
	if body.Direction != nil {
		direction = *body.Direction
	}
	expiryType := ""
	if body.ExpiryType != nil {
		expiryType = *body.ExpiryType
	}
	policy, err := api.service.UpsertPolicy(r.Context(), principal.SubjectID, principal.RequestID, PolicyInput{
		NodeID: nodeID, MonthlyLimitBytes: limit, WarningPercent: warning, Direction: direction, ThresholdsPct: body.ThresholdsPct, RemindDays: body.RemindDays, ExpiryType: expiryType,
		ExpiryAt: expiry, Estimated: estimated, ResetDay: resetDay, Timezone: timezone,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, quotaFromPolicy(policy))
}

func limitBytes(w http.ResponseWriter, body quotaBody) (int64, bool) {
	switch {
	case body.MonthlyLimitBytes != nil:
		if *body.MonthlyLimitBytes <= 0 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "monthlyLimitBytes must be positive")
			return 0, false
		}
		return *body.MonthlyLimitBytes, true
	case body.QuotaBytes != nil:
		if *body.QuotaBytes <= 0 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "quotaBytes must be positive")
			return 0, false
		}
		return *body.QuotaBytes, true
	case body.QuotaGb != nil:
		if math.IsNaN(*body.QuotaGb) || math.IsInf(*body.QuotaGb, 0) || *body.QuotaGb <= 0 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "quotaGb must be a positive number")
			return 0, false
		}
		value := *body.QuotaGb * 1e9
		if value > math.MaxInt64 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "quotaGb is too large")
			return 0, false
		}
		return int64(math.Round(value)), true
	default:
		writeFixed(w, http.StatusBadRequest, "invalid_request", "quotaGb or monthlyLimitBytes is required")
		return 0, false
	}
}

func warningPercent(w http.ResponseWriter, body quotaBody) (int, bool) {
	if body.WarningPercent != nil {
		return *body.WarningPercent, true
	}
	lowest := 0
	for _, threshold := range body.ThresholdsPct {
		if threshold <= 0 || threshold > 100 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "thresholdsPct values must be within 1-100")
			return 0, false
		}
		if lowest == 0 || threshold < lowest {
			lowest = threshold
		}
	}
	if lowest == 0 {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "warningPercent or thresholdsPct is required")
		return 0, false
	}
	return lowest, true
}

func parseExpiry(w http.ResponseWriter, body quotaBody) (time.Time, bool) {
	raw := body.ExpiryAt
	if raw == nil {
		raw = body.ExpiresAt
	}
	if raw == nil || *raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "expiryAt must be RFC3339")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func quotaFromPolicy(policy Policy) trafficQuotaDTO {
	thresholds := policy.ThresholdsPct
	if len(thresholds) == 0 {
		thresholds = []int{policy.WarningPercent}
	}
	direction := policy.Direction
	if direction == "" {
		direction = "combined"
	}
	return trafficQuotaDTO{
		CycleResetDay: policy.ResetDay, Timezone: policy.Timezone, AutomationOwnerID: policy.AutomationOwnerID, AutomationRevision: policy.AutomationRevision,
		NodeID: policy.NodeID, QuotaBytes: policy.MonthlyLimitBytes,
		QuotaGb: bytesToGB(float64(policy.MonthlyLimitBytes)), WarningPercent: policy.WarningPercent,
		ThresholdsPct: thresholds, ExpiryType: policy.ExpiryType, RemindDays: policy.RemindDays, ExpiresAt: formatOptionalTime(policy.ExpiryAt),
		Estimated: policy.Estimated, Direction: direction, UpdatedAt: formatOptionalTime(policy.UpdatedAt),
	}
}

func bytesToGB(value float64) float64 {
	return math.Round(value/1e9*1000) / 1000
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
	case errors.Is(err, ErrPolicyNotFound):
		writeFixed(w, http.StatusNotFound, "quota_not_found", "no traffic quota is configured for this node")
	case errors.Is(err, ErrUnknownNode):
		writeFixed(w, http.StatusNotFound, "node_not_found", "node was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrInvalidPolicy), errors.Is(err, ErrInvalidPeriod), errors.Is(err, ErrInvalidUsage),
		errors.Is(err, ErrInvalidNow), errors.Is(err, ErrRequestIDRequired), errors.Is(err, ErrNonMonotonic):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, ErrNotifierUnavailable), errors.Is(err, ErrServiceNotConfigured):
		writeFixed(w, http.StatusServiceUnavailable, "quota_unavailable", "quota dependency is unavailable")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
