package recovery

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

var ErrBackupNotFound = errors.New("recovery: backup not found")

type Principal struct {
	SubjectID     string
	RequestID     string
	Administrator bool
}
type Authenticator func(*http.Request) (Principal, error)

// BackupVerification is a real integrity observation, never a restore success.
type BackupVerification struct {
	Valid  bool
	Reason string
}
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}
type Plan struct {
	BackupID             string   `json:"backupId"`
	TargetControllerID   string   `json:"targetControllerId"`
	OldControllerFenced  bool     `json:"oldControllerFenced"`
	ExpectedVersion      string   `json:"expectedVersion,omitempty"`
	ExpectedNodeIDs      []string `json:"expectedNodeIds,omitempty"`
	RecoveryKeyReference string   `json:"recoveryKeyReference,omitempty"`
	Checks               []Check  `json:"checks,omitempty"`
	Eligible             bool     `json:"eligible"`
	Verified             bool     `json:"verified,omitempty"`
}

type Options struct {
	Authenticate Authenticator
	VerifyBackup func(context.Context, string) (BackupVerification, error)
	// Execute is a program-registered offline recovery adapter. Nil keeps job
	// creation closed; the HTTP surface cannot approve fencing or restore data.
	Execute func(context.Context, Plan) (string, error)
	Now     func() time.Time
}

type HTTP struct {
	options Options
	mux     *http.ServeMux
}

func NewHTTP(options Options) (*HTTP, error) {
	if options.Authenticate == nil {
		return nil, ErrRecoveryRejected
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	h := &HTTP{options: options, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/backups/{backupId}/verify", h.verifyBackup)
	h.mux.HandleFunc("POST /api/v1/disaster-recovery/preview", h.preview)
	h.mux.HandleFunc("POST /api/v1/disaster-recovery/jobs", h.createJob)
	h.mux.HandleFunc("GET /api/v1/disaster-recovery/jobs/{jobId}", h.job)
	return h, nil
}

func (h *HTTP) Patterns() []string {
	return []string{
		"POST /api/v1/backups/{backupId}/verify",
		"POST /api/v1/disaster-recovery/preview",
		"POST /api/v1/disaster-recovery/jobs",
		"GET /api/v1/disaster-recovery/jobs/{jobId}",
	}
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil {
		writeRecoveryProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	principal, err := h.options.Authenticate(r)
	if err != nil || !principal.Administrator || principal.SubjectID == "" {
		writeRecoveryProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func writeRecoveryProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

func writeRecoveryJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "RECOVERY_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func validRecoveryReference(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func decodeRecoveryBody(w http.ResponseWriter, r *http.Request, value any) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	return true
}

func validPlanInput(plan Plan) bool {
	return validRecoveryReference(plan.BackupID) && validRecoveryReference(plan.TargetControllerID) && (plan.RecoveryKeyReference == "" || validRecoveryReference(plan.RecoveryKeyReference)) && len(plan.ExpectedVersion) <= 64 && len(plan.ExpectedNodeIDs) <= 1000 && !strings.Contains(plan.RecoveryKeyReference, "PRIVATE KEY")
}

func (h *HTTP) verifyBackup(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	backupID := r.PathValue("backupId")
	if !validRecoveryReference(backupID) {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.VerifyBackup == nil {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "BACKUP_VERIFIER_UNAVAILABLE")
		return
	}
	verification, err := h.options.VerifyBackup(r.Context(), backupID)
	if errors.Is(err, ErrBackupNotFound) {
		writeRecoveryProblem(w, http.StatusNotFound, "BACKUP_NOT_FOUND")
		return
	}
	if err != nil {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "BACKUP_VERIFIER_UNAVAILABLE")
		return
	}
	plan := Plan{
		BackupID: backupID,
		Checks:   []Check{{Name: "backup_integrity", Passed: verification.Valid, Reason: verification.Reason}},
		Verified: verification.Valid,
	}
	writeRecoveryJSON(w, http.StatusOK, plan)
}

func (h *HTTP) preview(w http.ResponseWriter, r *http.Request) {
	var plan Plan
	if !decodeRecoveryBody(w, r, &plan) {
		return
	}
	if !validPlanInput(plan) {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.VerifyBackup == nil {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "BACKUP_VERIFIER_UNAVAILABLE")
		return
	}
	verification, err := h.options.VerifyBackup(r.Context(), plan.BackupID)
	if errors.Is(err, ErrBackupNotFound) {
		writeRecoveryProblem(w, http.StatusNotFound, "BACKUP_NOT_FOUND")
		return
	}
	if err != nil {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "BACKUP_VERIFIER_UNAVAILABLE")
		return
	}
	checks := []Check{
		{Name: "backup_integrity", Passed: verification.Valid, Reason: verification.Reason},
		// The HTTP surface has no authenticated Controller identity observation
		// in this release; a client-supplied target ID is not evidence.
		{Name: "target_controller_identity", Passed: false, Reason: "target_identity_not_observed"},
		// A checkbox is a declaration, never fencing evidence.
		{Name: "old_controller_fenced", Passed: false, Reason: "fencing_evidence_unavailable"},
		{Name: "recovery_key_reference", Passed: plan.RecoveryKeyReference == "" || validRecoveryReference(plan.RecoveryKeyReference), Reason: ""},
	}
	plan.Checks = checks
	plan.Eligible = true
	for _, check := range checks {
		if !check.Passed {
			plan.Eligible = false
		}
	}
	plan.Verified = verification.Valid
	writeRecoveryJSON(w, http.StatusOK, plan)
}

func (h *HTTP) createJob(w http.ResponseWriter, r *http.Request) {
	var plan Plan
	if !decodeRecoveryBody(w, r, &plan) {
		return
	}
	if !validPlanInput(plan) {
		writeRecoveryProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if h.options.Execute == nil {
		writeRecoveryProblem(w, http.StatusNotImplemented, "RECOVERY_EXECUTOR_UNAVAILABLE")
		return
	}
	id, err := h.options.Execute(r.Context(), plan)
	if err != nil || id == "" {
		writeRecoveryProblem(w, http.StatusServiceUnavailable, "RECOVERY_EXECUTOR_UNAVAILABLE")
		return
	}
	writeRecoveryJSON(w, http.StatusAccepted, map[string]any{"id": id, "status": "queued", "createdAt": h.options.Now().UTC()})
}

func (h *HTTP) job(w http.ResponseWriter, r *http.Request) {
	writeRecoveryProblem(w, http.StatusNotFound, "RECOVERY_JOB_NOT_FOUND")
}
