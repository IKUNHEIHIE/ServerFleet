package updates

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
)

var ErrUpdateSourceUnavailable = errors.New("updates: release source unavailable")

type Principal struct {
	SubjectID     string
	RequestID     string
	Administrator bool
}
type Authenticator func(*http.Request) (Principal, error)

type CheckResult struct {
	CurrentVersion     string    `json:"currentVersion"`
	LatestVersion      string    `json:"latestVersion,omitempty"`
	Channel            string    `json:"channel"`
	UpdateAvailable    bool      `json:"updateAvailable"`
	ReleaseNotes       string    `json:"releaseNotes,omitempty"`
	PublishedAt        string    `json:"publishedAt,omitempty"`
	SignatureVerified  bool      `json:"signatureVerified,omitempty"`
	PlatformCompatible bool      `json:"platformCompatible,omitempty"`
	CheckedAt          time.Time `json:"checkedAt"`
}
type PreflightCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}
type Plan struct {
	PlanID              string           `json:"planId"`
	TargetVersion       string           `json:"targetVersion"`
	Eligible            bool             `json:"eligible"`
	MigrationReversible bool             `json:"migrationReversible,omitempty"`
	PreflightChecks     []PreflightCheck `json:"preflightChecks,omitempty"`
	EstimatedImpact     string           `json:"estimatedImpact,omitempty"`
	BackupRequired      bool             `json:"backupRequired"`
}

// Options are program-registered adapters. A nil Check/Plan disables the route
// with an explicit unavailable code; this package never invents a release feed,
// installer or rollback execution.
type Options struct {
	Authenticate Authenticator
	Check        func(context.Context, string) (CheckResult, error)
	Plan         func(context.Context, string) (Plan, error)
	Now          func() time.Time
	// Apply resolves a persisted approved plan through the sole task service.
	// It must not take executable paths or privilege from this HTTP request.
	Apply func(context.Context, ApplyRequest) (ApplyResult, error)
}

type ApplyRequest struct {
	Principal                        Principal
	PlanID, Confirmation, BackupMode string
}
type ApplyResult struct {
	ID            string `json:"id"`
	TaskID        string `json:"taskId"`
	Status        string `json:"status"`
	TargetVersion string `json:"targetVersion"`
}
type updatePrincipalKey struct{}

type HTTP struct {
	options Options
	mux     *http.ServeMux
}

func NewHTTP(options Options) (*HTTP, error) {
	if options.Authenticate == nil {
		return nil, ErrArtifactRejected
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	h := &HTTP{options: options, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/system/updates/check", h.check)
	h.mux.HandleFunc("POST /api/v1/system/updates/plan", h.plan)
	h.mux.HandleFunc("POST /api/v1/system/updates/apply", h.apply)
	h.mux.HandleFunc("GET /api/v1/system/updates/jobs", h.jobs)
	h.mux.HandleFunc("GET /api/v1/system/updates/jobs/{jobId}", h.job)
	h.mux.HandleFunc("POST /api/v1/system/updates/jobs/{jobId}/rollback", h.rollback)
	return h, nil
}

func (h *HTTP) Patterns() []string {
	return []string{
		"POST /api/v1/system/updates/check",
		"POST /api/v1/system/updates/plan",
		"POST /api/v1/system/updates/apply",
		"GET /api/v1/system/updates/jobs",
		"GET /api/v1/system/updates/jobs/{jobId}",
		"POST /api/v1/system/updates/jobs/{jobId}/rollback",
	}
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil {
		writeUpdateProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	principal, err := h.options.Authenticate(r)
	if err != nil || !principal.Administrator || principal.SubjectID == "" {
		writeUpdateProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	h.mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), updatePrincipalKey{}, principal)))
}

func writeUpdateProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

func writeUpdateJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		writeUpdateProblem(w, http.StatusServiceUnavailable, "UPDATE_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func decodeUpdateBody(w http.ResponseWriter, r *http.Request, value any) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	return true
}

func (h *HTTP) check(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
	}
	if !decodeUpdateBody(w, r, &body) {
		return
	}
	if body.Channel != "stable" && body.Channel != "beta" {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.Check == nil {
		writeUpdateProblem(w, http.StatusServiceUnavailable, "UPDATE_SOURCE_UNAVAILABLE")
		return
	}
	result, err := h.options.Check(r.Context(), body.Channel)
	if err != nil {
		writeUpdateProblem(w, http.StatusServiceUnavailable, "UPDATE_SOURCE_UNAVAILABLE")
		return
	}
	if result.Channel == "" {
		result.Channel = body.Channel
	}
	if result.CheckedAt.IsZero() {
		result.CheckedAt = h.options.Now().UTC()
	}
	writeUpdateJSON(w, http.StatusOK, result)
}

func (h *HTTP) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetVersion string `json:"targetVersion"`
		DryRun        bool   `json:"dryRun"`
	}
	if !decodeUpdateBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.TargetVersion) == "" || len(body.TargetVersion) > 64 {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.Plan == nil {
		writeUpdateProblem(w, http.StatusServiceUnavailable, "UPDATE_PLANNER_UNAVAILABLE")
		return
	}
	result, err := h.options.Plan(r.Context(), body.TargetVersion)
	if err != nil {
		writeUpdateProblem(w, http.StatusServiceUnavailable, "UPDATE_PLANNER_UNAVAILABLE")
		return
	}
	writeUpdateJSON(w, http.StatusOK, result)
}

func (h *HTTP) apply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID       string `json:"planId"`
		Confirmation string `json:"confirmation"`
		BackupMode   string `json:"backupMode"`
	}
	if !decodeUpdateBody(w, r, &body) {
		return
	}
	if body.PlanID == "" || body.Confirmation == "" || body.BackupMode != "required" {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.Apply == nil {
		writeUpdateProblem(w, http.StatusNotImplemented, "UPDATE_INSTALLER_UNAVAILABLE")
		return
	}
	principal, ok := r.Context().Value(updatePrincipalKey{}).(Principal)
	if !ok {
		writeUpdateProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	result, err := h.options.Apply(r.Context(), ApplyRequest{principal, body.PlanID, body.Confirmation, body.BackupMode})
	if err != nil || !installID(result.ID) || !installID(result.TaskID) || (result.Status != "installed" && result.Status != "rolled_back" && result.Status != "queued") {
		writeUpdateProblem(w, 503, "UPDATE_OUTCOME_UNKNOWN")
		return
	}
	writeUpdateJSON(w, http.StatusAccepted, result)
}

func (h *HTTP) jobs(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	// No job can be created without an admitted installer/hook set.
	writeUpdateJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (h *HTTP) job(w http.ResponseWriter, r *http.Request) {
	writeUpdateProblem(w, http.StatusNotFound, "UPDATE_JOB_NOT_FOUND")
}

func (h *HTTP) rollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		if !decodeUpdateBody(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Reason) == "" || len(body.Reason) > 1024 {
			writeUpdateProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	}
	writeUpdateProblem(w, http.StatusNotFound, "UPDATE_JOB_NOT_FOUND")
}
