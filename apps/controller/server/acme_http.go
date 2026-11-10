package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/tasks"
)

func publicCertificateBody(w http.ResponseWriter, r *http.Request, max int64) (map[string]string, bool) {
	if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	var fields map[string]string
	if json.Unmarshal(raw, &fields) != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	return fields, true
}
func (s *Server) registerPublicCertificateRoutes() {
	s.registerPublicRenewalRoutes()
	s.registerDNSProviderTaskRoutes()
	s.mux.HandleFunc("GET /api/v1/certificates/public-profile", func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if s.PublicCertificates == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "ACME_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"profileSha256": s.PublicCertificates.profileSHA, "websitePrivateKeyOnController": false, "configured": true, "dnsCredentialId": s.PublicCertificates.o.DNSCredentialID, "dnsCredentialRevision": s.PublicCertificates.o.DNSCredentialRevision})
	})
	s.mux.HandleFunc("POST /api/v1/certificates/public-tasks", func(w http.ResponseWriter, r *http.Request) {
		id, requestID, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		m := s.PublicCertificates
		if m == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "ACME_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		body, ok := publicCertificateBody(w, r, 4096)
		if !ok {
			return
		}
		if len(body) != 3 || !deploymentID.MatchString(body["csrTaskId"]) || !deploymentID.MatchString(body["zoneId"]) || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		source, err := s.Tasks.Get(r.Context(), body["csrTaskId"])
		if err != nil || len(source.Targets) != 1 || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), id.SubjectID, source.ID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "CSR_READ_DENIED"})
			return
		}
		proof, err := m.csr(r.Context(), source.ID, source.Targets[0].NodeID)
		if err != nil || m.zone(r.Context(), tasks.Task{SubmitterID: id.SubjectID}, body["zoneId"], proof.Domain) != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CSR_OR_ZONE_UNAVAILABLE"})
			return
		}
		meta, err := s.Credentials.ServiceMetadata(r.Context(), id.SubjectID, m.o.DNSCredentialID)
		if err != nil || meta.Revision != m.o.DNSCredentialRevision {
			releaseSourceJSON(w, 403, map[string]string{"code": "DNS_CREDENTIAL_NOT_OWNED_OR_CURRENT"})
			return
		}
		task, err := s.Tasks.Submit(r.Context(), tasks.SubmitRequest{SubjectID: id.SubjectID, RequestID: requestID, Name: "Issue public certificate for node-local CSR", ActionID: actionPublicCertificateIssue, Source: audit.SourceWeb, OperationClass: tasks.OperationClassModifying, RequireApproval: true, TargetNodeIDs: []string{proof.NodeID}, Params: map[string]string{"csrTaskId": source.ID, "zoneId": body["zoneId"], "profileSha256": m.profileSHA}, IdempotencyKey: "public-certificate-" + backupTaskKey("order", body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_ORDER_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	})
	s.mux.HandleFunc("GET /api/v1/certificates/public-tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), id.SubjectID, r.PathValue("taskId")) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		task, err := s.Tasks.Get(r.Context(), r.PathValue("taskId"))
		if err != nil || task.ActionID != actionPublicCertificateIssue || len(task.Targets) != 1 {
			releaseSourceJSON(w, 404, map[string]string{"code": "CERTIFICATE_ORDER_NOT_FOUND"})
			return
		}
		order, err := certificates.ReadPublicOrder(r.Context(), s.DB, task.ID)
		if err != nil {
			releaseSourceJSON(w, 409, map[string]any{"code": "CERTIFICATE_ORDER_NOT_OBSERVED", "status": task.Status})
			return
		}
		if order.PlanSHA256 != task.PlanHash || order.NodeID != task.Targets[0].NodeID {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_ORDER_BINDING_CHANGED"})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"taskId": task.ID, "taskStatus": task.Status, "orderStatus": order.Status, "csrTaskId": order.CSRTaskID, "nodeId": order.NodeID, "domain": order.Domain, "keyRef": order.KeyRef, "chainSha256": order.ChainSHA256, "certificatePemBase64": base64.StdEncoding.EncodeToString(order.ChainPEM), "notAfter": order.NotAfter, "pendingCleanupCount": len(order.PendingCleanup), "errorCode": order.ErrorCode, "observedAt": order.UpdatedAt, "installed": false})
	})
}
