package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/updates"
)

type ReleaseSourceDeployment struct {
	Origin          string   `json:"origin"`
	Addresses       []string `json:"addresses"`
	RootCertificate string   `json:"rootCertificate,omitempty"`
}

func LoadReleaseSourceDeployment(filename string) (*ReleaseSourceDeployment, error) {
	raw, err := readPrivateDeployment(filename)
	if err != nil {
		return nil, updates.ErrArtifactRejected
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return nil, updates.ErrArtifactRejected
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, updates.ErrArtifactRejected
	}
	var d struct {
		Version int `json:"version"`
		ReleaseSourceDeployment
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || d.Version != 1 {
		return nil, updates.ErrArtifactRejected
	}
	return &d.ReleaseSourceDeployment, nil
}
func (s *Server) setupReleaseSource(ctx context.Context, cfg Config, workers map[string]tasks.Worker) (*tasks.ControllerExecution, error) {
	if cfg.ReleaseSource == nil {
		return nil, nil
	}
	if cfg.UpdateProduct == nil || !deploymentID.MatchString(cfg.ControllerAuthorityID) {
		return nil, updates.ErrArtifactRejected
	}
	feed := updates.HTTPReleaseFeed{Origin: cfg.ReleaseSource.Origin, Addresses: cfg.ReleaseSource.Addresses}
	if cfg.ReleaseSource.RootCertificate != "" {
		raw, err := readPrivateDeployment(cfg.ReleaseSource.RootCertificate)
		if err != nil {
			return nil, updates.ErrArtifactRejected
		}
		feed.RootPEM = raw
	}
	product := cfg.UpdateProduct
	acquirer, err := updates.NewReleaseAcquirer(updates.ReleaseAcquisitionOptions{DB: s.DB, Audit: s.Audit, Inbox: product.Inbox, StageDir: product.StageDir, KeyID: product.KeyID, Version: product.CandidateVersion, AuthorityID: cfg.ControllerAuthorityID, Feed: feed, Policy: product.Policy, Current: func(ctx context.Context, id, plan string) (tasks.Task, error) {
		if s.Tasks == nil {
			return tasks.Task{}, tasks.ErrInvalidTask
		}
		return s.Tasks.CheckExecution(ctx, id, plan)
	}, Administrator: s.Identity.AuthorizeAdministrator, RuntimeCurrent: s.runtimeGate})
	if err != nil {
		return nil, err
	}
	s.ReleaseAcquirer = acquirer
	workers[updates.ActionAcquireRelease] = acquirer
	return &tasks.ControllerExecution{AuthorityID: cfg.ControllerAuthorityID, Actions: map[string]tasks.ControllerAction{updates.ActionAcquireRelease: {}}, Authorize: func(ctx context.Context, subject, action, operation string) error {
		if action != updates.ActionAcquireRelease {
			return auth.ErrDenied
		}
		return s.Identity.AuthorizeAdministrator(ctx, subject)
	}}, nil
}
func mergeControllerExecutions(first, second *tasks.ControllerExecution) (*tasks.ControllerExecution, error) {
	if first == nil {
		return second, nil
	}
	if second == nil {
		return first, nil
	}
	if first.AuthorityID != second.AuthorityID {
		return nil, tasks.ErrInvalidTask
	}
	actions := map[string]tasks.ControllerAction{}
	firstActions := map[string]bool{}
	for action, rule := range first.Actions {
		actions[action] = rule
		firstActions[action] = true
	}
	for action, rule := range second.Actions {
		if _, exists := actions[action]; exists {
			return nil, tasks.ErrInvalidTask
		}
		actions[action] = rule
	}
	return &tasks.ControllerExecution{AuthorityID: first.AuthorityID, Actions: actions, Authorize: func(ctx context.Context, subject, action, operation string) error {
		if firstActions[action] {
			return first.Authorize(ctx, subject, action, operation)
		}
		return second.Authorize(ctx, subject, action, operation)
	}}, nil
}
func releaseSourceJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) registerReleaseSourceRoutes() {
	handler := func(w http.ResponseWriter, r *http.Request) {
		p, request, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), p.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]any{"code": "NOT_AUTHORIZED", "status": 403})
			return
		}
		if s.ReleaseAcquirer == nil {
			releaseSourceJSON(w, 503, map[string]any{"code": "RELEASE_SOURCE_UNAVAILABLE", "status": 503})
			return
		}
		if r.URL.RawQuery != "" {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		version, digest, host := s.ReleaseAcquirer.Metadata()
		if r.Method == http.MethodGet {
			if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
				releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
				return
			}
			releaseSourceJSON(w, 200, map[string]any{"version": version, "sourceSha256": digest, "host": host, "acquisitionIsInstallation": false})
			return
		}
		media, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || len(r.Header.Values("Content-Type")) != 1 || media != "application/json" || len(params) > 1 || params["charset"] != "" && params["charset"] != "utf-8" || r.Header.Get("Content-Encoding") != "" {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if e != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if gatewayDeploymentUniqueJSON(decoder) != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		var body map[string]string
		if json.Unmarshal(raw, &body) != nil || len(body) != 1 || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		admission, e := s.ReleaseAcquirer.TaskRequest(p.SubjectID, request, body["idempotencyKey"])
		if e != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		task, e := s.Tasks.SubmitController(r.Context(), admission)
		if e != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "RELEASE_ACQUISITION_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "targetType": tasks.TargetTypeController, "controllerId": task.TargetResourceID, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	}
	s.mux.HandleFunc("GET /api/v1/system/updates/source", handler)
	s.mux.HandleFunc("POST /api/v1/system/updates/acquire", handler)
}
