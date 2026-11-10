package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"serverfleet.local/serverfleet/internal/auth"
	"strconv"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func (s *Server) registerCommandRoutes() {
	s.mux.HandleFunc("POST /api/v1/commands/tasks", func(w http.ResponseWriter, r *http.Request) {
		identity, request, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		body, ok := publicCertificateBody(w, r, 8<<10)
		if !ok {
			return
		}
		if len(body) != 7 || !deploymentID.MatchString(body["nodeId"]) || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		action, class := transport.CommandCapability, tasks.OperationClassModifying
		if body["privilege"] == "root" {
			action, class = transport.RootCommandCapability, tasks.OperationClassRoot
			if s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
				releaseSourceJSON(w, 403, map[string]string{"code": "ADMINISTRATOR_REQUIRED"})
				return
			}
		} else if body["privilege"] != "normal" {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_PRIVILEGE"})
			return
		}
		params := map[string]string{"profileId": body["profileId"], "arguments": body["arguments"], "timeoutSeconds": body["timeoutSeconds"], "policySha256": body["policySha256"]}
		raw, _ := json.Marshal(params)
		if _, err = transport.ParseCommandParameters(raw); err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_COMMAND"})
			return
		}
		task, err := s.Tasks.Submit(r.Context(), tasks.SubmitRequest{SubjectID: identity.SubjectID, RequestID: request, Name: "Execute approved node command profile", ActionID: action, OperationClass: class, RequireApproval: true, Source: audit.SourceWeb, TargetNodeIDs: []string{body["nodeId"]}, Params: params, IdempotencyKey: "command-" + backupTaskKey(action, body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "COMMAND_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	})
	s.mux.HandleFunc("POST /api/v1/agent/root-admission", s.agentCommandAdmission(true))
	s.mux.HandleFunc("POST /api/v1/agent/command-admission", s.agentCommandAdmission(false))
}
func (s *Server) agentCommandAdmission(root bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || s.Enrollment == nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "ROOT_ADMISSION_DENIED"})
			return
		}
		for _, name := range []string{"Origin", "Cookie", "Authorization", "Proxy-Authorization", "Referer", "X-CSRF-Token"} {
			if len(r.Header.Values(name)) > 0 {
				releaseSourceJSON(w, 403, map[string]string{"code": "ROOT_ADMISSION_DENIED"})
				return
			}
		}
		var body struct {
			NodeID string             `json:"nodeId"`
			Nonce  string             `json:"nonce"`
			Phase  string             `json:"phase"`
			Offer  *agentv1.TaskOffer `json:"offer"`
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 24<<10))
		if err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF || body.Offer == nil || !deploymentID.MatchString(body.Nonce) || (body.Phase != "start" && body.Phase != "current") {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		verifier, err := transport.NewPeerVerifier(s.Enrollment.Lookup)
		if err != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "ROOT_ADMISSION_DENIED"})
			return
		}
		peer, err := verifier.Verify(r.Context(), *r.TLS, body.NodeID)
		if err != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "ROOT_ADMISSION_DENIED"})
			return
		}
		expectedAction, expectedClass, expectedPrivilege := transport.CommandCapability, tasks.OperationClassModifying, agentv1.Privilege_PRIVILEGE_NORMAL
		if root {
			expectedAction, expectedClass, expectedPrivilege = transport.RootCommandCapability, tasks.OperationClassRoot, agentv1.Privilege_PRIVILEGE_ROOT
		}
		offer := body.Offer
		current, err := s.Tasks.CheckExecutionForNode(r.Context(), offer.TaskId, offer.PlanDigestSha256, peer.NodeID)
		parameters, parameterErr := transport.ParseCommandParameters(offer.CanonicalParametersJson)
		approval, approvalErr := s.TasksRepo.Approval(r.Context(), s.DB, current.ID, current.Revision)
		if err != nil || parameterErr != nil || approvalErr != nil || current.Source != "web" || current.ActionID != expectedAction || current.OperationClass != expectedClass || !current.ApprovalRequired || !approval.Approved() || approval.PlanHash != current.PlanHash || root && (s.Identity.AuthorizeAdministrator(r.Context(), approval.ApproverID) != nil || s.Identity.AuthorizeAdministrator(r.Context(), current.SubmitterID) != nil) || string(offer.CanonicalParametersJson) != current.Params || offer.Privilege != expectedPrivilege || offer.ExecutionId != executionIDFor(current.ID, peer.NodeID, current.PlanHash) || offer.ActionSchemaVersion != "1" || offer.ActionId != current.ActionID || offer.PluginId != "" || offer.ScopeRevision != 0 || offer.TimeoutSeconds != uint32(parameters.TimeoutSeconds) || offer.ExpiresAtUnixMs <= time.Now().UnixMilli() || offer.ExpiresAtUnixMs > time.Now().Add(10*time.Minute).UnixMilli() {
			releaseSourceJSON(w, 403, map[string]string{"code": "ROOT_ADMISSION_DENIED"})
			return
		}
		if body.Phase == "start" {
			if err = s.Audit.Append(r.Context(), audit.Event{SubjectID: current.SubmitterID, Action: expectedAction, Result: "allowed", RequestID: body.Nonce, TaskID: current.ID, NodeIDs: []string{peer.NodeID}, Source: audit.SourceSystem, ReasonCode: "command_executor_current_human_approval", At: time.Now().UTC()}); err != nil {
				releaseSourceJSON(w, 503, map[string]string{"code": "ROOT_ADMISSION_UNAVAILABLE"})
				return
			}
		}
		releaseSourceJSON(w, 200, map[string]string{"taskId": current.ID, "executionId": offer.ExecutionId, "planHash": current.PlanHash, "nonce": body.Nonce, "phase": body.Phase, "timeoutSeconds": strconv.Itoa(parameters.TimeoutSeconds)})
	}
}

func (s *Server) authorizeCommandTask(ctx context.Context, t tasks.Task) error {
	approval, err := s.TasksRepo.Approval(ctx, s.DB, t.ID, t.Revision)
	if err != nil || !approval.Approved() || approval.PlanHash != t.PlanHash {
		return auth.ErrDenied
	}
	if t.ActionID == transport.RootCommandCapability || t.ActionID == transport.ResidentUpdateAction {
		return s.Identity.AuthorizeAdministrator(ctx, approval.ApproverID)
	}
	return nil
}
