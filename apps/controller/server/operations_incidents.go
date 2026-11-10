package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
)

func (s *Server) setupOperatorIncidents() error {
	service, e := tasks.NewIncidentService(tasks.IncidentOptions{DB: s.DB, Audit: s.Audit, Administrator: s.Identity.AuthorizeAdministrator, RuntimeCurrent: s.runtimeGate, Filter: operatorEvidence,
		ReadSource: func(ctx context.Context, subject string, source tasks.IncidentSource) error {
			if source.Kind == "task" {
				t, e := s.Tasks.Get(ctx, source.ID)
				if e != nil || t.SubmitterID != source.OwnerID || t.Revision != source.Revision || t.PlanHash != source.PlanDigest {
					return auth.ErrDenied
				}
				return (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, subject, t.ID)
			}
			if source.Kind != "ai_run" || s.AI == nil || s.AI.CheckOperatorIncidentSource(ctx, source.OwnerID, source.SessionID, source.ID, source.State, source.Revision) != nil {
				return auth.ErrDenied
			}
			if source.TaskID != "" {
				return (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, subject, source.TaskID)
			}
			return nil
		}, ReadFollowup: func(ctx context.Context, subject, id string, after time.Time) error {
			t, e := s.Tasks.Get(ctx, id)
			if e != nil || t.CreatedAt.Before(after) {
				return auth.ErrDenied
			}
			return (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, subject, id)
		},
	})
	if e != nil {
		return e
	}
	s.Incidents = service
	return nil
}
func operatorEvidence(text string) (string, error) {
	if strings.Contains(text, "-----BEGIN") && strings.Contains(text, "PRIVATE KEY-----") {
		return "", auth.ErrDenied
	}
	text = redactAISecrets(text)
	if strings.Contains(strings.ToLower(text), "x-amz-signature=") {
		return "", auth.ErrDenied
	}
	return text, nil
}
func (s *Server) syncOperatorIncidents(ctx context.Context) error {
	if s.Incidents == nil || s.runtimeGate(ctx) != nil {
		return auth.ErrDenied
	}
	sources, e := s.TasksRepo.IncidentSources(ctx)
	if e != nil {
		return e
	}
	for _, source := range sources {
		if len(source.Reason) > 256 {
			source.Reason = source.Reason[:256]
			for !utf8.ValidString(source.Reason) {
				source.Reason = source.Reason[:len(source.Reason)-1]
			}
		}
		if e = s.Incidents.Observe(ctx, source); e != nil {
			return e
		}
	}
	if s.AI != nil {
		runs, e := s.AI.OperatorIncidentSources(ctx)
		if e != nil {
			return e
		}
		for _, r := range runs {
			if e = s.Incidents.Observe(ctx, tasks.IncidentSource{Kind: "ai_run", ID: r.RunID, OwnerID: r.OwnerID, Revision: r.Revision, State: r.State, TaskID: r.TaskID, SessionID: r.SessionID, Action: "ai.runs", Reason: r.Reason, NodeIDs: []string{}}); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Server) runOperatorIncidentMonitor(ctx context.Context) {
	_ = s.syncOperatorIncidents(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.syncOperatorIncidents(ctx)
		}
	}
}
func decodeOperationsBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
	if e != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(d) != nil || d.Decode(&struct{}{}) != io.EOF {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return false
	}
	return true
}
func (s *Server) operatorPrincipal(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	identity, request, e := s.authenticateIdentity(r)
	if r.TLS == nil || e != nil || !identity.Administrator || s.runtimeGate(r.Context()) != nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
		releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
		return "", "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return identity.SubjectID, request, true
}
func writeOperationsError(w http.ResponseWriter, e error) {
	status, code := 409, "OPERATION_REJECTED"
	if errors.Is(e, auth.ErrDenied) {
		status, code = 403, "NOT_AUTHORIZED"
	}
	releaseSourceJSON(w, status, map[string]string{"code": code})
}
func (s *Server) registerOperatorIncidentRoutes() {
	s.mux.HandleFunc("GET /api/v1/operations/incidents", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		items, e := s.Incidents.List(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		for i := range items {
			items[i].Events = []tasks.IncidentEvent{}
			items[i].Proposal = nil
		}
		releaseSourceJSON(w, 200, map[string]any{"items": items})
	})
	s.mux.HandleFunc("POST /api/v1/operations/incidents/sync", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var body struct{}
		if !decodeOperationsBody(w, r, &body) {
			return
		}
		if e := s.syncOperatorIncidents(r.Context()); e != nil {
			writeOperationsError(w, e)
			return
		}
		items, e := s.Incidents.List(r.Context(), subject)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, map[string]int{"observed": len(items)})
	})
	s.mux.HandleFunc("GET /api/v1/operations/incidents/{incidentId}", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		v, e := s.Incidents.Get(r.Context(), subject, r.PathValue("incidentId"))
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/operations/incidents/{incidentId}/actions", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var in tasks.IncidentDecision
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		v, e := s.Incidents.Decide(r.Context(), subject, r.PathValue("incidentId"), in)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, v)
	})
}
