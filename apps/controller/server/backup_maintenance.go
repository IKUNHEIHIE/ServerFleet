package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/tasks"
)

type backupBorrowKey struct{}
type backupBorrowScope struct {
	server      *Server
	task        tasks.Task
	destination recovery.BackupDestination
	operation   auth.CredentialOperation
	active      atomic.Bool
}

func (s *Server) backupCredentialCurrent(ctx context.Context, subject string, d recovery.BackupDestination) error {
	if s.Credentials == nil || s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
		return auth.ErrDenied
	}
	meta, e := s.Credentials.ServiceMetadata(ctx, subject, d.CredentialRef)
	if e != nil || meta.Kind != "backup_storage_secret" || meta.Revision != d.CredentialRevision {
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) backupCredentialGate(ctx context.Context, op auth.CredentialOperation) error {
	scope, ok := ctx.Value(backupBorrowKey{}).(*backupBorrowScope)
	if !ok || scope == nil || scope.server != s || !scope.active.Load() || scope.operation != op || s.BackupMaintenance == nil || s.backupCredentialCurrent(ctx, scope.task.SubmitterID, scope.destination) != nil {
		return auth.ErrDenied
	}
	return s.BackupMaintenance.CredentialOperationCurrent(ctx, scope.task, scope.destination)
}
func (s *Server) borrowBackupCredential(ctx context.Context, t tasks.Task, d recovery.BackupDestination, consume func(context.Context, []byte) error) error {
	if consume == nil || s.BackupMaintenance == nil || s.BackupMaintenance.CredentialOperationCurrent(ctx, t, d) != nil || s.backupCredentialCurrent(ctx, t.SubmitterID, d) != nil {
		return auth.ErrDenied
	}
	op := auth.CredentialOperation{SubjectID: t.SubmitterID, CredentialID: d.CredentialRef, Module: "backups", Source: t.Source, OperationID: "backup-store-" + newRequestID(), PlanDigest: t.PlanHash, Action: "use", ExpectedRevision: d.CredentialRevision}
	scope := &backupBorrowScope{server: s, task: t, destination: d, operation: op}
	scope.active.Store(true)
	defer scope.active.Store(false)
	return s.Credentials.UseForService(context.WithValue(ctx, backupBorrowKey{}, scope), op, consume)
}
func (s *Server) submitBackupMaintenance(ctx context.Context, subject, request, action, key string, params map[string]string, source audit.Source) (tasks.Task, error) {
	if s.BackupMaintenance == nil || s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
		return tasks.Task{}, auth.ErrDenied
	}
	class := tasks.OperationClassStandard
	approval := false
	if action == recovery.ActionBackupRetention {
		class = tasks.OperationClassModifying
		approval = true
	}
	return s.Tasks.SubmitController(ctx, tasks.SubmitRequest{SubjectID: subject, RequestID: request, Name: "Backup maintenance: " + action, ActionID: action, OperationClass: class, RequireApproval: approval, Source: source, IdempotencyKey: backupTaskKey(action, key), Params: params})
}
func (s *Server) registerBackupMaintenanceRoutes() {
	wrap := func(next func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			subject, request, ok := s.operatorPrincipal(w, r)
			if !ok {
				return
			}
			if s.BackupMaintenance == nil {
				releaseSourceJSON(w, 503, map[string]string{"code": "BACKUP_DEPLOYMENT_REQUIRED"})
				return
			}
			next(w, r, subject, request)
		}
	}
	s.mux.HandleFunc("GET /api/v1/backups/maintenance", wrap(func(w http.ResponseWriter, r *http.Request, subject, _ string) {
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		policy, e := s.BackupMaintenance.Policy(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		remote, e := s.BackupMaintenance.RemoteRecords(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		receipts, e := s.BackupMaintenance.CleanupReceipts(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"policy": policy, "remoteObjects": remote, "cleanupReceipts": receipts})
	}))
	s.mux.HandleFunc("PUT /api/v1/backups/maintenance/policy", wrap(func(w http.ResponseWriter, r *http.Request, subject, request string) {
		var in struct {
			Expected uint64                `json:"expectedRevision"`
			Policy   recovery.BackupPolicy `json:"policy"`
		}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		if in.Policy.OwnerID != "" || in.Policy.Revision != 0 || !in.Policy.UpdatedAt.IsZero() {
			releaseSourceJSON(w, 400, map[string]string{"code": "SERVER_OWNED_FIELDS"})
			return
		}
		p, e := s.BackupMaintenance.Configure(r.Context(), subject, request, in.Expected, in.Policy)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, p)
	}))
	submit := func(action, idField string) http.HandlerFunc {
		return wrap(func(w http.ResponseWriter, r *http.Request, subject, request string) {
			var in struct {
				Key  string `json:"idempotencyKey"`
				Plan string `json:"planDigest,omitempty"`
			}
			if !decodeOperationsBody(w, r, &in) {
				return
			}
			if !deploymentID.MatchString(in.Key) || action != recovery.ActionBackupRetention && in.Plan != "" {
				releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
				return
			}
			object := r.PathValue(idField)
			if action == recovery.ActionBackupRetention {
				object = in.Plan
			}
			params, e := s.BackupMaintenance.TaskParameters(r.Context(), subject, action, object)
			if e != nil {
				writeOperationsError(w, e)
				return
			}
			task, e := s.submitBackupMaintenance(r.Context(), subject, request, action, in.Key, params, audit.SourceWeb)
			if e != nil {
				writeOperationsError(w, e)
				return
			}
			releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
		})
	}
	s.mux.HandleFunc("POST /api/v1/backups/{backupId}/remote-upload", submit(recovery.ActionBackupUpload, "backupId"))
	s.mux.HandleFunc("POST /api/v1/backups/remotes/{remoteId}/verify", submit(recovery.ActionBackupRemoteVerify, "remoteId"))
	s.mux.HandleFunc("POST /api/v1/backups/maintenance/cleanup", submit(recovery.ActionBackupRetention, ""))
	s.mux.HandleFunc("POST /api/v1/backups/maintenance/cleanup-preview", wrap(func(w http.ResponseWriter, r *http.Request, subject, _ string) {
		var in struct{}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		plan, e := s.BackupMaintenance.PreviewCleanup(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, plan)
	}))
	s.mux.HandleFunc("POST /api/v1/backups/{backupId}/protection", wrap(func(w http.ResponseWriter, r *http.Request, subject, request string) {
		var in struct {
			Expected uint64 `json:"expectedRevision"`
			Pinned   bool   `json:"pinned"`
			Note     string `json:"note"`
		}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		note, e := operatorEvidence(in.Note)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		local, e := s.BackupMaintenance.Protect(r.Context(), subject, request, r.PathValue("backupId"), in.Expected, in.Pinned, note)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, local)
	}))
}
func (s *Server) backupScheduleTick(ctx context.Context) {
	if s.Backups == nil || s.BackupMaintenance == nil || s.Tasks == nil || s.runtimeGate(ctx) != nil {
		return
	}
	p, work, e := s.BackupMaintenance.AutomaticWork(ctx)
	if e != nil {
		return
	}
	if p.ScheduledBackups && p.OwnerID != "" {
		confirmed, e := s.Backups.KeyConfirmed(ctx)
		if e == nil && confirmed {
			slot := strconv.FormatInt(time.Now().Unix()/int64(p.IntervalHours*3600), 10)
			_, _ = s.submitBackup(ctx, recovery.Principal{SubjectID: p.OwnerID, RequestID: "backup-schedule-" + slot}, "full", "schedule-"+strconv.FormatUint(p.Revision, 10)+"-"+slot, audit.SourceSystem)
		}
	}
	for _, params := range work {
		action := params["_action"]
		delete(params, "_action")
		raw, _ := json.Marshal(params)
		key := string(raw)
		if action == recovery.ActionBackupRetention {
			key = "retention-" + strconv.FormatUint(p.Revision, 10) + "-" + time.Now().UTC().Format("2006-01-02")
		}
		_, _ = s.submitBackupMaintenance(ctx, p.OwnerID, newRequestID(), action, key, params, audit.SourceSystem)
	}
}
