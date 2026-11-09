package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
	"time"
)

func (s *Server) registerResidentUpdateRoutes() {
	s.mux.HandleFunc("POST /api/v1/system/agent-fleet-updates", func(w http.ResponseWriter, r *http.Request) {
		identity, request, e := s.authenticateIdentity(r)
		if r.TLS == nil || e != nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "ADMINISTRATOR_REQUIRED"})
			return
		}
		var body struct {
			NodeIDs        []string `json:"nodeIds"`
			Version        string   `json:"version"`
			KeyID          string   `json:"keyId"`
			ReleaseSHA256  string   `json:"releaseSha256"`
			PolicySHA256   string   `json:"policySha256"`
			IdempotencyKey string   `json:"idempotencyKey"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || d.Decode(&struct{}{}) != io.EOF || len(body.NodeIDs) < 1 || len(body.NodeIDs) > 64 || !transport.FileID(body.IdempotencyKey) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		params := map[string]string{"version": body.Version, "keyId": body.KeyID, "releaseSha256": body.ReleaseSHA256, "policySha256": body.PolicySHA256}
		raw, _ := json.Marshal(params)
		if _, e = transport.ParseResidentUpdateParameters(raw); e != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_RELEASE"})
			return
		}
		// One homogeneous policy/architecture per batch. Heterogeneous hosts need
		// separate approved plans; a browser cannot replace their provisioned policy.
		task, e := s.Tasks.Submit(r.Context(), tasks.SubmitRequest{SubjectID: identity.SubjectID, RequestID: request, Name: "Signed remote Agent fleet update (canary then five)", ActionID: transport.ResidentUpdateAction, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Source: audit.SourceWeb, TargetNodeIDs: body.NodeIDs, Params: params, IdempotencyKey: "fleet-update-" + backupTaskKey(identity.SubjectID, body.IdempotencyKey)})
		if e != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "FLEET_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "planHash": task.PlanHash, "status": task.Status, "approvalRequired": true, "batchPolicy": "1_then_5_stop_on_non_success"})
	})
	s.mux.HandleFunc("POST /api/v1/agent/update-admission", s.residentUpdateAdmission)
	s.mux.HandleFunc("POST /api/v1/system/controller-update-tasks", s.submitResidentControllerUpdate)
	s.mux.HandleFunc("GET /api/v1/system/controller-update-receipts/{taskId}", s.readResidentControllerReceipt)
}

type residentAdmissionRequest struct {
	Intent       transport.ResidentUpdateIntent `json:"intent"`
	Phase        string                         `json:"phase"`
	Nonce        string                         `json:"nonce"`
	ConnectionID string                         `json:"connectionId,omitempty"`
	Receipt      *updates.InstallReceipt        `json:"receipt,omitempty"`
}

func (s *Server) residentUpdateAdmission(w http.ResponseWriter, r *http.Request) {
	deny := func() { releaseSourceJSON(w, 403, map[string]string{"code": "UPDATE_ADMISSION_DENIED"}) }
	if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || s.Enrollment == nil {
		deny()
		return
	}
	for _, name := range []string{"Origin", "Cookie", "Authorization", "Proxy-Authorization", "Referer", "X-CSRF-Token"} {
		if len(r.Header.Values(name)) > 0 {
			deny()
			return
		}
	}
	var b residentAdmissionRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.Decode(&struct{}{}) != io.EOF || !transport.FileID(b.Nonce) || !transport.FileID(b.Phase) || !b.Intent.Valid(time.Now()) {
		deny()
		return
	}
	verifier, e := transport.NewPeerVerifier(s.Enrollment.Lookup)
	if e != nil {
		deny()
		return
	}
	peer, e := verifier.Verify(r.Context(), *r.TLS, b.Intent.NodeID)
	if e != nil {
		deny()
		return
	}
	t, e := s.Tasks.CheckExecutionForNode(r.Context(), b.Intent.TaskID, b.Intent.PlanSHA256, peer.NodeID)
	p, pe := transport.ParseResidentUpdateParameters([]byte(b.Intent.Parameters))
	if e != nil || pe != nil || t.ActionID != transport.ResidentUpdateAction || t.Params != b.Intent.Parameters || t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired || b.Intent.ExecutionID != executionIDFor(t.ID, peer.NodeID, t.PlanHash) || s.authorizeCommandTask(r.Context(), t) != nil || s.Identity.AuthorizeAdministrator(r.Context(), t.SubmitterID) != nil {
		deny()
		return
	}
	dispatched := false
	for _, target := range t.Targets {
		if target.NodeID == peer.NodeID && target.State == tasks.TargetDispatched && target.BatchIndex == t.BatchIndex {
			dispatched = true
		}
	}
	if !dispatched {
		deny()
		return
	}
	if b.Phase == "ready" || b.Phase == "commit" {
		observation, e := s.Links.FileConnection(r.Context(), peer.NodeID)
		if e != nil || observation.ConnectionID != b.ConnectionID {
			deny()
			return
		}
	}
	if b.Phase == "commit" {
		receipt := b.Receipt
		if receipt == nil || receipt.Intent.TaskID != t.ID || receipt.Intent.ExecutionID != b.Intent.ExecutionID || receipt.Attempted.Component != "agent" || receipt.Attempted.Version != p.Version || receipt.Attempted.SHA256 != p.ReleaseSHA256 || (receipt.State != "installed" && receipt.State != "rolled_back") || receipt.State == "installed" && receipt.Current != receipt.Attempted || receipt.State == "rolled_back" && (!receipt.Intent.AllowRollback || receipt.Current != receipt.Intent.Previous) || receipt.Health.ExecutionID != b.Intent.ExecutionID || receipt.Health.Component != "agent" || receipt.Health.Version != receipt.Current.Version || receipt.Health.SHA256 != receipt.Current.SHA256 || receipt.Health.ObservedAt.Before(receipt.Health.StartedAt) || time.Since(receipt.Health.ObservedAt) < 0 || time.Since(receipt.Health.ObservedAt) > 10*time.Second {
			deny()
			return
		}
		if s.Audit.Append(r.Context(), audit.Event{SubjectID: t.SubmitterID, Action: transport.ResidentUpdateAction, Result: "success", RequestID: b.Nonce, TaskID: t.ID, NodeIDs: []string{peer.NodeID}, Source: audit.SourceSystem, ReasonCode: "supervisor_verified_signed_image_and_fresh_connected_health", At: time.Now().UTC()}) != nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "UPDATE_AUDIT_UNAVAILABLE"})
			return
		}
		code, message := tasks.NodeResultSucceeded, "signed Agent installed; owned process and reconnected health observed"
		if receipt.State == "rolled_back" {
			code, message = tasks.NodeResultFailed, "candidate unhealthy; compatible previous Agent restored and observed healthy; later batches halted"
		}
		if _, e = s.Tasks.RecordResult(r.Context(), t.ID, peer.NodeID, code, message); e != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "UPDATE_RESULT_UNKNOWN"})
			return
		}
	}
	releaseSourceJSON(w, 200, map[string]string{"nonce": b.Nonce, "phase": b.Phase, "taskId": t.ID, "executionId": b.Intent.ExecutionID, "planHash": t.PlanHash, "auditRecordId": "update-" + b.Nonce})
}

type residentControllerWorker struct {
	s *Server
	c updates.ResidentSupervisorConfig
}

func (w *residentControllerWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{t.TargetResourceID: {Code: code, Message: message}}, nil
	}
	if _, e := w.s.Tasks.CheckExecution(ctx, t.ID, t.PlanHash); e != nil {
		return result(tasks.NodeResultFailed, "controller update authority denied")
	}
	p, e := transport.ParseResidentUpdateParameters([]byte(t.Params))
	if e != nil || p.PolicySHA256 != updates.ResidentPolicySHA(w.c) {
		return result(tasks.NodeResultFailed, "controller supervisor policy changed")
	}
	intent := transport.ResidentUpdateIntent{Format: 1, TaskID: t.ID, ExecutionID: executionIDFor(t.ID, w.c.NodeID, t.PlanHash), PlanSHA256: t.PlanHash, NodeID: w.c.NodeID, Parameters: t.Params, ExpiresAt: time.Now().Add(5 * time.Minute)}
	if e = updates.WriteResidentIntent(w.c.Mailbox, intent); e != nil {
		return result(tasks.NodeResultUnknown, "controller handoff reservation unknown; never replay")
	}
	// The external supervisor stops this process. On restart the ordinary task
	// remains UNKNOWN; a separate actual install receipt is the result evidence.
	<-ctx.Done()
	return result(tasks.NodeResultUnknown, "controller update handed off; inspect external supervisor receipt without replay")
}
func (s *Server) submitResidentControllerUpdate(w http.ResponseWriter, r *http.Request) {
	identity, request, e := s.authenticateIdentity(r)
	if r.TLS == nil || e != nil || s.residentSupervisor == nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
		releaseSourceJSON(w, 403, map[string]string{"code": "SUPERVISED_CONTROLLER_REQUIRED"})
		return
	}
	body, ok := publicCertificateBody(w, r, 8192)
	if !ok {
		return
	}
	if len(body) != 5 || !transport.FileID(body["idempotencyKey"]) {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return
	}
	key := body["idempotencyKey"]
	delete(body, "idempotencyKey")
	raw, _ := json.Marshal(body)
	p, e := transport.ParseResidentUpdateParameters(raw)
	if e != nil || p.PolicySHA256 != updates.ResidentPolicySHA(*s.residentSupervisor) {
		releaseSourceJSON(w, 409, map[string]string{"code": "SUPERVISOR_POLICY_MISMATCH"})
		return
	}
	t, e := s.Tasks.SubmitController(r.Context(), tasks.SubmitRequest{SubjectID: identity.SubjectID, RequestID: request, Name: "Update owned Controller process from signed release", ActionID: transport.ControllerUpdateAction, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Source: audit.SourceWeb, Params: body, IdempotencyKey: "controller-update-" + backupTaskKey(identity.SubjectID, key)})
	if e != nil {
		releaseSourceJSON(w, 409, map[string]string{"code": "CONTROLLER_UPDATE_TASK_REJECTED"})
		return
	}
	releaseSourceJSON(w, 202, map[string]any{"taskId": t.ID, "status": t.Status, "planHash": t.PlanHash, "approvalRequired": true})
}
func (s *Server) readResidentControllerReceipt(w http.ResponseWriter, r *http.Request) {
	identity, _, e := s.authenticateIdentity(r)
	if r.TLS == nil || e != nil || s.residentSupervisor == nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
		releaseSourceJSON(w, 403, map[string]string{"code": "ADMINISTRATOR_REQUIRED"})
		return
	}
	t, e := s.TasksRepo.Get(r.Context(), r.PathValue("taskId"))
	if e != nil || t.ActionID != transport.ControllerUpdateAction || t.SubmitterID != identity.SubjectID {
		releaseSourceJSON(w, 404, map[string]string{"code": "NOT_FOUND"})
		return
	}
	slot, e := updates.NewSlotInstaller(s.residentSupervisor.Directory)
	if e != nil {
		releaseSourceJSON(w, 409, map[string]string{"code": "RECEIPT_UNAVAILABLE"})
		return
	}
	defer slot.Close()
	receipt, e := slot.Receipt(executionIDFor(t.ID, s.residentSupervisor.NodeID, t.PlanHash))
	if e != nil {
		releaseSourceJSON(w, 409, map[string]string{"code": "INSTALL_RESULT_UNKNOWN"})
		return
	}
	releaseSourceJSON(w, 200, map[string]any{"taskId": t.ID, "taskStatus": t.Status, "receipt": receipt, "historicalReceipt": true, "liveBuildVersion": s.buildVersion})
}
func (s *Server) setupResidentController(c *updates.ResidentSupervisorConfig, authority string, workers map[string]tasks.Worker) (*tasks.ControllerExecution, error) {
	if c == nil {
		return nil, nil
	}
	if c.Component != "controller" || c.NodeID != authority || !filepath.IsAbs(c.Database) || c.Database != s.dbPath {
		return nil, updates.ErrArtifactRejected
	}
	// Require the provisioned parent marker and exact configuration digest.
	if os.Getenv("SF_RESIDENT_SUPERVISOR_POLICY") != updates.ResidentPolicySHA(*c) {
		return nil, updates.ErrArtifactRejected
	}
	s.residentSupervisor = c
	workers[transport.ControllerUpdateAction] = &residentControllerWorker{s: s, c: *c}
	return &tasks.ControllerExecution{AuthorityID: authority, Actions: map[string]tasks.ControllerAction{transport.ControllerUpdateAction: {RequireApproval: true}}, Authorize: func(ctx context.Context, subject, action, permission string) error {
		if action != transport.ControllerUpdateAction {
			return auth.ErrDenied
		}
		return s.Identity.AuthorizeAdministrator(ctx, subject)
	}}, nil
}
