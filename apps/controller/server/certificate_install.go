package server

import (
	"context"
	"net/http"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
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
