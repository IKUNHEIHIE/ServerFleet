package server

import (
	"context"
	"encoding/json"
	"net/http"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/tasks"
	"strconv"
	"strings"
	"unicode/utf8"
)

func operatorReason(v string) string {
	v = redactAISecrets(v)
	if len(v) > 256 {
		v = v[:256]
		for !utf8.ValidString(v) {
			v = v[:len(v)-1]
		}
	}
	return v
}
func (s *Server) inspectIncidentSource(ctx context.Context, subject string, source tasks.IncidentSource) (tasks.IncidentSource, error) {
	gate := &composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}
	if source.TaskID != "" && gate.AuthorizeTaskRead(ctx, subject, source.TaskID) != nil {
		return source, auth.ErrDenied
	}
	if source.Kind == "task" {
		t, e := s.Tasks.Get(ctx, source.ID)
		if e != nil || t.SubmitterID != source.OwnerID || t.PlanHash != source.PlanDigest || t.ActionID != source.Action || source.TaskID != t.ID || gate.AuthorizeTaskRead(ctx, subject, t.ID) != nil {
			return source, auth.ErrDenied
		}
		source.Revision = t.Revision
		source.State = string(t.Status)
		source.Reason = operatorReason(t.FailureReason)
		return source, nil
	}
	if source.Kind != "ai_run" || s.AI == nil {
		return source, auth.ErrDenied
	}
	current, e := s.AI.InspectOperatorIncidentSource(ctx, source.OwnerID, source.SessionID, source.ID)
	if e != nil {
		return source, auth.ErrDenied
	}
	if current.TaskID != "" && gate.AuthorizeTaskRead(ctx, subject, current.TaskID) != nil {
		return source, auth.ErrDenied
	}
	source.Revision = current.Revision
	source.State = current.State
	source.TaskID = current.TaskID
	source.Reason = operatorReason(current.Reason)
	return source, nil
}
func (s *Server) inspectIncidentTask(ctx context.Context, subject, id string) (tasks.IncidentTaskReceipt, error) {
	if (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, subject, id) != nil {
		return tasks.IncidentTaskReceipt{}, auth.ErrDenied
	}
	t, e := s.Tasks.Get(ctx, id)
	if e != nil {
		return tasks.IncidentTaskReceipt{}, e
	}
	return tasks.OperatorTaskReceipt(t), nil
}
func (s *Server) incidentFollowupRelated(ctx context.Context, subject string, source tasks.IncidentSource, id string) error {
	if _, e := s.inspectIncidentTask(ctx, subject, id); e != nil {
		return e
	}
	candidate, e := s.Tasks.Get(ctx, id)
	if e != nil {
		return e
	}
	allowed := map[string]bool{}
	for _, node := range source.NodeIDs {
		allowed[node] = true
	}
	var original tasks.Task
	if source.TaskID != "" {
		original, e = s.Tasks.Get(ctx, source.TaskID)
		if e != nil {
			return e
		}
		for _, target := range original.Targets {
			allowed[target.NodeID] = true
		}
	}
	if candidate.TargetType == tasks.TargetTypeController {
		if original.TargetType != tasks.TargetTypeController || candidate.TargetResourceID != original.TargetResourceID || strings.Split(candidate.ActionID, ".")[0] != strings.Split(original.ActionID, ".")[0] {
			return auth.ErrDenied
		}
		if original.ActionID == recovery.ActionBackupUpload {
			if candidate.ActionID != recovery.ActionBackupRemoteVerify || s.BackupMaintenance == nil {
				return auth.ErrDenied
			}
			var params map[string]string
			if json.Unmarshal([]byte(candidate.Params), &params) != nil {
				return auth.ErrDenied
			}
			records, e := s.BackupMaintenance.RemoteRecords(ctx, subject)
			if e != nil {
				return e
			}
			for _, r := range records {
				if r.ID == params["remoteId"] && r.TaskID == original.ID {
					return nil
				}
			}
			return auth.ErrDenied
		}
		return nil
	}
	if original.TargetType == tasks.TargetTypeController || len(candidate.Targets) == 0 {
		return auth.ErrDenied
	}
	for _, target := range candidate.Targets {
		if !allowed[target.NodeID] {
			return auth.ErrDenied
		}
	}
	return nil
}
func (s *Server) registerIncidentClosureRoutes() {
	s.mux.HandleFunc("GET /api/v1/operations/incidents/{incidentId}/evidence", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		if r.ContentLength != 0 || len(q) > 1 || len(q["after"]) > 1 || len(q) == 1 && !q.Has("after") {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		after := int64(0)
		var e error
		if q.Has("after") {
			after, e = strconv.ParseInt(q.Get("after"), 10, 64)
			if e != nil || after < 0 {
				releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
				return
			}
		}
		items, next, e := s.Incidents.EvidencePage(r.Context(), subject, r.PathValue("incidentId"), after)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"items": items, "nextAfter": next})
	})
	s.mux.HandleFunc("POST /api/v1/operations/incidents/{incidentId}/verify-backup", func(w http.ResponseWriter, r *http.Request) {
		subject, request, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var in struct {
			Expected  uint64 `json:"expectedRevision"`
			RequestID string `json:"requestId"`
			Note      string `json:"note"`
		}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		incident, e := s.Incidents.Get(r.Context(), subject, r.PathValue("incidentId"))
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		if incident.Revision != in.Expected || incident.State != "investigating" || incident.AssigneeID != subject || incident.Source.Kind != "task" || incident.Source.Action != recovery.ActionBackupUpload || !deploymentID.MatchString(in.RequestID) || len(in.Note) > 4096 || strings.TrimSpace(in.Note) == "" {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		if s.BackupMaintenance == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "BACKUP_DEPLOYMENT_REQUIRED"})
			return
		}
		note, e := operatorEvidence(in.Note)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		records, e := s.BackupMaintenance.RemoteRecords(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		remoteID := ""
		for _, record := range records {
			if record.TaskID == incident.Source.ID {
				remoteID = record.ID
				break
			}
		}
		if remoteID == "" {
			writeOperationsError(w, tasks.ErrInvalidTask)
			return
		}
		params, e := s.BackupMaintenance.TaskParameters(r.Context(), subject, recovery.ActionBackupRemoteVerify, remoteID)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		task, e := s.submitBackupMaintenance(r.Context(), subject, request, recovery.ActionBackupRemoteVerify, "incident-"+incident.ID+"-"+in.RequestID, params, audit.SourceWeb)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		// Never hide an admitted task when case linking loses a CAS race.
		_, linkErr := s.Incidents.Decide(r.Context(), subject, incident.ID, tasks.IncidentDecision{ExpectedRevision: in.Expected, RequestID: in.RequestID, Action: "link_task", Note: note, FollowupTaskID: task.ID})
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "linked": linkErr == nil, "approvalRequired": task.ApprovalRequired, "originalResultPreserved": true})
	})
}
