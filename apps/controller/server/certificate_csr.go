package server

import (
	"context"
	"net/http"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func (b *agentLinkBridge) observeCertificateCSR(ctx context.Context, observation transport.LinkObservation, output *agentv1.TaskOutput) (bool, error) {
	if output == nil || b.tasks == nil {
		return false, nil
	}
	task, err := b.tasksRepo.Get(ctx, output.TaskId)
	if err != nil || task.ActionID != transport.CertificateCSRAction {
		return false, nil
	}
	if output.Truncated || len(output.Chunk) > 8<<10 || output.ExecutionId != executionIDFor(task.ID, observation.Identity.NodeID, task.PlanHash) || output.StreamSequence != 1 || output.Stream != agentv1.TaskOutput_STREAM_STDOUT {
		return true, transport.ErrLinkRejected
	}
	current, err := b.tasks.CheckExecutionForNode(ctx, task.ID, task.PlanHash, observation.Identity.NodeID)
	if err != nil || current.OperationClass != tasks.OperationClassModifying || !current.ApprovalRequired || current.Source != "web" || len(current.Targets) != 1 {
		return true, transport.ErrLinkRejected
	}
	request, err := transport.ParseCertificateCSRRequest([]byte(current.Params))
	if err != nil {
		return true, err
	}
	result, err := transport.ValidateCertificateCSRResult(output.Chunk, observation.Identity.NodeID, request.Domain, request.KeyRef)
	if err != nil {
		return true, err
	}
	receipt := transport.CertificateCSRReceipt{TaskID: task.ID, NodeID: observation.Identity.NodeID, PlanSHA256: task.PlanHash, Domain: request.Domain, KeyRef: request.KeyRef, PublicResult: result, ObservedAt: time.Now().UTC()}
	if b.recordCSR == nil {
		return true, transport.ErrLinkRejected
	}
	err = b.recordCSR(ctx, receipt)
	return true, err
}

func (s *Server) registerCertificateCSRRoutes() {
	s.mux.HandleFunc("GET /api/v1/certificates/csr-tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		identity, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), identity.SubjectID, r.PathValue("taskId")) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		task, err := s.Tasks.Get(r.Context(), r.PathValue("taskId"))
		if err != nil || task.ActionID != transport.CertificateCSRAction || len(task.Targets) != 1 {
			releaseSourceJSON(w, 404, map[string]string{"code": "CSR_TASK_NOT_FOUND"})
			return
		}
		if task.Status != tasks.StatusCompleted || task.Targets[0].State != tasks.TargetSucceeded {
			releaseSourceJSON(w, 409, map[string]any{"code": "CSR_RESULT_NOT_COMPLETE", "status": task.Status})
			return
		}
		receipt, err := transport.ReadCertificateCSR(r.Context(), s.DB, task.ID)
		if err != nil || receipt.PlanSHA256 != task.PlanHash || receipt.NodeID != task.Targets[0].NodeID {
			releaseSourceJSON(w, 409, map[string]string{"code": "CSR_RECEIPT_UNAVAILABLE"})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"taskId": task.ID, "planHash": task.PlanHash, "observedAt": receipt.ObservedAt, "result": receipt.PublicResult})
	})
}
