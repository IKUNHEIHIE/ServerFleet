package recovery

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"serverfleet.local/serverfleet/internal/tasks"
)

type BackupHTTPOptions struct {
	Authenticate Authenticator
	Manager      *BackupManager
	Submit       func(context.Context, Principal, string, string) (tasks.Task, error)
}
type BackupHTTP struct {
	o   BackupHTTPOptions
	mux *http.ServeMux
}

func NewBackupHTTP(o BackupHTTPOptions) (*BackupHTTP, error) {
	if o.Authenticate == nil || o.Manager == nil || o.Submit == nil {
		return nil, ErrBackupUnavailable
	}
	h := &BackupHTTP{o: o, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/backups", h.list)
	h.mux.HandleFunc("POST /api/v1/backups", h.create)
	h.mux.HandleFunc("POST /api/v1/backups/key-challenges", h.challenge)
	h.mux.HandleFunc("POST /api/v1/backups/key-confirmation", h.confirm)
	h.mux.HandleFunc("GET /api/v1/backups/{backupId}/download", h.download)
	h.mux.HandleFunc("POST /api/v1/backups/{backupId}/verify", h.verify)
	return h, nil
}
func (h *BackupHTTP) Patterns() []string { return BackupHTTPPatterns() }
func BackupHTTPPatterns() []string {
	return []string{"GET /api/v1/backups", "POST /api/v1/backups", "POST /api/v1/backups/key-challenges", "POST /api/v1/backups/key-confirmation", "GET /api/v1/backups/{backupId}/download", "POST /api/v1/backups/{backupId}/verify"}
}
func (h *BackupHTTP) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, err := h.o.Authenticate(r)
	if r.TLS == nil || err != nil || !p.Administrator || h.o.Manager.authorizeRead(r.Context(), p.SubjectID) != nil {
		writeRecoveryProblem(w, 403, "NOT_AUTHORIZED")
		return Principal{}, false
	}
	return p, true
}
func (h *BackupHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, ok := h.principal(w, r); !ok {
		return
	}
	h.mux.ServeHTTP(w, r)
}
func backupEmptyRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeRecoveryProblem(w, 400, "INVALID_REQUEST")
		return false
	}
	return true
}
func (h *BackupHTTP) list(w http.ResponseWriter, r *http.Request) {
	if !backupEmptyRequest(w, r) {
		return
	}
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	values, err := h.o.Manager.List(r.Context(), p.SubjectID)
	if err != nil {
		writeRecoveryProblem(w, 503, "BACKUP_SOURCE_UNAVAILABLE")
		return
	}
	confirmed, err := h.o.Manager.KeyConfirmed(r.Context())
	if err != nil {
		writeRecoveryProblem(w, 503, "BACKUP_SOURCE_UNAVAILABLE")
		return
	}
	automatic := h.o.Manager.o.AutomaticConfigured
	if h.o.Manager.maintenance != nil {
		p, e := h.o.Manager.maintenance.Policy(r.Context(), p.SubjectID)
		if e != nil {
			writeRecoveryProblem(w, 503, "BACKUP_SOURCE_UNAVAILABLE")
			return
		}
		automatic = p.ScheduledBackups
	}
	writeRecoveryJSON(w, 200, map[string]any{"items": values, "recipientFingerprint": h.o.Manager.RecipientFingerprint(), "recoveryKeyConfirmed": confirmed, "automaticBackupsConfigured": automatic})
}
func (h *BackupHTTP) create(w http.ResponseWriter, r *http.Request) { h.submit(w, r, "full") }
func (h *BackupHTTP) challenge(w http.ResponseWriter, r *http.Request) {
	h.submit(w, r, "key_challenge")
}
func (h *BackupHTTP) submit(w http.ResponseWriter, r *http.Request, kind string) {
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decodeRecoveryBody(w, r, &body) {
		return
	}
	if !validReference(body.IdempotencyKey) {
		writeRecoveryProblem(w, 400, "INVALID_REQUEST")
		return
	}
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	if kind == "full" {
		confirmed, err := h.o.Manager.KeyConfirmed(r.Context())
		if err != nil || !confirmed {
			writeRecoveryProblem(w, 409, "OFFLINE_RECOVERY_KEY_CONFIRMATION_REQUIRED")
			return
		}
	}
	t, err := h.o.Submit(r.Context(), p, kind, body.IdempotencyKey)
	if err != nil {
		writeRecoveryProblem(w, 409, "BACKUP_TASK_REJECTED")
		return
	}
	writeRecoveryJSON(w, 202, map[string]any{"taskId": t.ID, "status": t.Status, "controllerId": t.TargetResourceID, "targetType": t.TargetType, "planHash": t.PlanHash, "approvalRequired": t.ApprovalRequired})
}
func (h *BackupHTTP) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChallengeID string `json:"challengeId"`
		Proof       string `json:"proofSha256"`
	}
	if !decodeRecoveryBody(w, r, &body) {
		return
	}
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	if h.o.Manager.ConfirmRecoveryKey(r.Context(), p.SubjectID, p.RequestID, body.ChallengeID, body.Proof) != nil {
		writeRecoveryProblem(w, 409, "RECOVERY_KEY_PROOF_REJECTED")
		return
	}
	writeRecoveryJSON(w, 200, map[string]bool{"confirmed": true})
}
func (h *BackupHTTP) verify(w http.ResponseWriter, r *http.Request) {
	if !backupEmptyRequest(w, r) {
		return
	}
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	v, err := h.o.Manager.VerifyCipher(r.Context(), p.SubjectID, p.RequestID, r.PathValue("backupId"))
	if err != nil {
		writeRecoveryProblem(w, 409, "BACKUP_CIPHERTEXT_NOT_VERIFIED")
		return
	}
	writeRecoveryJSON(w, 200, Plan{BackupID: r.PathValue("backupId"), Verified: v.Valid, Checks: []Check{{Name: "ciphertext_integrity", Passed: v.Valid, Reason: v.Reason}}})
}
func (h *BackupHTTP) download(w http.ResponseWriter, r *http.Request) {
	if !backupEmptyRequest(w, r) {
		return
	}
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	file, meta, err := h.o.Manager.OpenCipher(r.Context(), p.SubjectID, p.RequestID, r.PathValue("backupId"))
	if err != nil {
		writeRecoveryProblem(w, 409, "BACKUP_DOWNLOAD_UNAVAILABLE")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Bytes, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+meta.ID+`.age"`)
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	buffer := make([]byte, 64<<10)
	remaining := meta.Bytes
	for remaining > 0 {
		if h.o.Manager.authorizeRead(r.Context(), p.SubjectID) != nil {
			panic(http.ErrAbortHandler)
		}
		n, e := file.Read(buffer)
		if n > 0 {
			if int64(n) > remaining {
				panic(http.ErrAbortHandler)
			}
			written, writeErr := w.Write(buffer[:n])
			if writeErr != nil || written != n {
				return
			}
			remaining -= int64(n)
		}
		if e != nil {
			if e != io.EOF || remaining != 0 {
				panic(http.ErrAbortHandler)
			}
			break
		}
	}
}
