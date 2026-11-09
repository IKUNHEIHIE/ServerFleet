package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// CSRTask is a frozen public reference alongside the chain. The Agent also
// verifies its own protected key/CSR association independently of this ID.
func certificateInstallTaskParams(raw []byte) (transport.CertificateInstallRequest, string, error) {
	request, _, err := transport.ParseCertificateInstallRequest(raw)
	return request, request.CSRTaskID, err
}

func (b *agentLinkBridge) observeCertificateInstall(ctx context.Context, observation transport.LinkObservation, output *agentv1.TaskOutput) (bool, error) {
	if output == nil || b.tasks == nil {
		return false, nil
	}
	task, err := b.tasksRepo.Get(ctx, output.TaskId)
	if err != nil || task.ActionID != transport.CertificateInstallAction {
		return false, nil
	}
	if b.recordChain == nil || output.Truncated || output.StreamSequence != 1 || output.Stream != agentv1.TaskOutput_STREAM_STDOUT || output.ExecutionId != executionIDFor(task.ID, observation.Identity.NodeID, task.PlanHash) {
		return true, transport.ErrLinkRejected
	}
	current, err := b.tasks.CheckExecutionForNode(ctx, task.ID, task.PlanHash, observation.Identity.NodeID)
	if err != nil || current.OperationClass != tasks.OperationClassModifying || !current.ApprovalRequired || current.Source != "web" || len(current.Targets) != 1 {
		return true, transport.ErrLinkRejected
	}
	request, csrTask, err := certificateInstallTaskParams([]byte(current.Params))
	if err != nil {
		return true, err
	}
	result, err := transport.ValidateCertificateInstallResult(output.Chunk, observation.Identity.NodeID, request)
	if err != nil {
		return true, err
	}
	receipt := transport.CertificateChainReceipt{TaskID: task.ID, CSRTaskID: csrTask, NodeID: observation.Identity.NodeID, PlanSHA256: task.PlanHash, Request: request, Result: result, ObservedAt: time.Now().UTC()}
	return true, b.recordChain(ctx, receipt)
}
func (s *Server) registerCertificateInstallRoutes() {
	s.mux.HandleFunc("POST /api/v1/certificates/chain-tasks", func(w http.ResponseWriter, r *http.Request) {
		id, requestID, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if err != nil {
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
		if json.Unmarshal(raw, &body) != nil || len(body) != 3 || !deploymentID.MatchString(body["csrTaskId"]) || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		csrTask, err := s.Tasks.Get(r.Context(), body["csrTaskId"])
		if err != nil || csrTask.ActionID != transport.CertificateCSRAction || csrTask.Status != tasks.StatusCompleted || len(csrTask.Targets) != 1 || csrTask.Targets[0].State != tasks.TargetSucceeded || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), id.SubjectID, csrTask.ID) != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CSR_PROOF_NOT_AVAILABLE"})
			return
		}
		csr, err := transport.ReadCertificateCSR(r.Context(), s.DB, csrTask.ID)
		if err != nil || csr.PlanSHA256 != csrTask.PlanHash || csr.NodeID != csrTask.Targets[0].NodeID {
			releaseSourceJSON(w, 409, map[string]string{"code": "CSR_PROOF_NOT_AVAILABLE"})
			return
		}
		chain, err := base64.StdEncoding.Strict().DecodeString(body["certificatePemBase64"])
		if err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_PUBLIC_CHAIN"})
			return
		}
		certs, err := transport.ParsePublicCertificateChain(chain)
		if err != nil || len(certs) == 0 || certs[0].IsCA {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_PUBLIC_CHAIN"})
			return
		}
		// Controller can check association without claiming the Agent's trust store.
		csrDER, _ := base64.StdEncoding.Strict().DecodeString(csr.PublicResult.CSRDER)
		parsedCSR, err := x509.ParseCertificateRequest(csrDER)
		if err != nil || !bytes.Equal(parsedCSR.RawSubjectPublicKeyInfo, certs[0].RawSubjectPublicKeyInfo) {
			releaseSourceJSON(w, 400, map[string]string{"code": "CERTIFICATE_KEY_MISMATCH"})
			return
		}
		sum := sha256.Sum256(chain)
		params := map[string]string{"domain": csr.Domain, "keyRef": csr.KeyRef, "csrSha256": csr.PublicResult.SHA256, "certificatePemBase64": body["certificatePemBase64"], "chainSha256": hex.EncodeToString(sum[:]), "csrTaskId": csr.TaskID}
		task, err := s.Tasks.Submit(r.Context(), tasks.SubmitRequest{SubjectID: id.SubjectID, RequestID: requestID, Name: "Install public website certificate chain", ActionID: transport.CertificateInstallAction, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Source: audit.SourceWeb, TargetNodeIDs: []string{csr.NodeID}, Params: params, IdempotencyKey: "certificate-chain-" + backupTaskKey("chain", body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	})
	s.mux.HandleFunc("GET /api/v1/certificates/chain-tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), id.SubjectID, r.PathValue("taskId")) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		task, err := s.Tasks.Get(r.Context(), r.PathValue("taskId"))
		if err != nil || task.ActionID != transport.CertificateInstallAction || len(task.Targets) != 1 {
			releaseSourceJSON(w, 404, map[string]string{"code": "CERTIFICATE_TASK_NOT_FOUND"})
			return
		}
		if task.Status != tasks.StatusCompleted || task.Targets[0].State != tasks.TargetSucceeded {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_TASK_NOT_COMPLETE"})
			return
		}
		receipt, err := transport.ReadCertificateChain(r.Context(), s.DB, task.ID)
		if err != nil || receipt.PlanSHA256 != task.PlanHash || receipt.NodeID != task.Targets[0].NodeID {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_RECEIPT_UNAVAILABLE"})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"taskId": task.ID, "csrTaskId": receipt.CSRTaskID, "certificateId": nodeCertificatePrefix + task.ID, "planHash": receipt.PlanSHA256, "observedAt": receipt.ObservedAt, "result": receipt.Result})
	})
}
