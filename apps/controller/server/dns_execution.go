package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/tasks"
)

const actionDNSProviderInspect = "dns.provider.inspect"

type dnsInspectionWorker struct{ manager *publicCertificateExecution }

func (m *publicCertificateExecution) inspectionExecution(authority string, workers map[string]tasks.Worker) (*tasks.ControllerExecution, error) {
	if !deploymentID.MatchString(authority) {
		return nil, tasks.ErrInvalidTask
	}
	m.authorityID = authority
	workers[actionDNSProviderInspect] = dnsInspectionWorker{manager: m}
	workers[actionDNSRecordApply] = dnsMutationWorker{manager: m}
	return &tasks.ControllerExecution{AuthorityID: authority, Actions: map[string]tasks.ControllerAction{actionDNSProviderInspect: {}, actionDNSRecordApply: {RequireApproval: true}}, Authorize: func(ctx context.Context, subject, action, operation string) error {
		if action != actionDNSProviderInspect && action != actionDNSRecordApply {
			return auth.ErrDenied
		}
		return m.s.Identity.AuthorizeAdministrator(ctx, subject)
	}}, nil
}
func (m *publicCertificateExecution) validExecutionTask(t tasks.Task) bool {
	if t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" {
		return false
	}
	switch t.ActionID {
	case actionDNSRecordApply:
		_, _, err := m.mutationParams(t)
		return err == nil
	case actionPublicCertificateIssue:
		_, _, err := m.requestParams(t)
		return err == nil && t.TargetType != tasks.TargetTypeController
	case actionDNSProviderInspect:
		_, err := m.inspectionParams(t)
		return err == nil
	}
	return false
}
func (m *publicCertificateExecution) inspectionParams(t tasks.Task) (map[string]string, error) {
	var p map[string]string
	if t.ActionID != actionDNSProviderInspect || t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" || t.OperationClass != tasks.OperationClassStandard || t.TargetType != tasks.TargetTypeController || t.TargetResourceID != m.authorityID || len(t.Targets) != 1 || t.Targets[0].NodeID != m.authorityID || json.Unmarshal([]byte(t.Params), &p) != nil || p["profileSha256"] != m.profileSHA || !deploymentID.MatchString(p["accountId"]) {
		return nil, tasks.ErrInvalidTask
	}
	switch p["operation"] {
	case "test":
		if len(p) != 3 {
			return nil, tasks.ErrInvalidTask
		}
	case "import-zone":
		if len(p) != 5 || !deploymentID.MatchString(p["zoneId"]) || !deploymentID.MatchString(p["providerZoneId"]) {
			return nil, tasks.ErrInvalidTask
		}
	default:
		return nil, tasks.ErrInvalidTask
	}
	return p, nil
}
func (w dnsInspectionWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	m := w.manager
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{m.authorityID: {Code: code, Message: message}}, nil
	}
	p, err := m.inspectionParams(t)
	if err != nil {
		return result(tasks.NodeResultFailed, "DNS inspection plan rejected")
	}
	scope := m.taskScope(t, "")
	ctx, cancel := context.WithTimeout(m.scope(ctx, scope), time.Minute)
	defer cancel()
	ref, err := m.s.DNS.AccountExecutionBinding(ctx, t.SubmitterID, p["accountId"])
	if err != nil || ref != m.o.DNSCredentialID || m.current(ctx, scope) != nil {
		return result(tasks.NodeResultFailed, "DNS account or current credential rejected")
	}
	switch p["operation"] {
	case "test":
		checked, e := m.s.DNS.TestAccount(ctx, t.SubmitterID, t.RequestID, p["accountId"])
		if e != nil {
			return result(tasks.NodeResultUnknown, "DNS inspection result unobserved; inspect task and credential audit before resubmitting")
		}
		if !checked.OK {
			return result(tasks.NodeResultFailed, "DNS credential verification rejected")
		}
		return result(tasks.NodeResultSucceeded, "DNS credential verified by provider; no remote record changed")
	case "import-zone":
		zone, e := m.s.DNS.CreateZone(ctx, t.SubmitterID, t.RequestID, p["accountId"], dns.CreateZoneInput{ID: p["zoneId"], ProviderZoneID: p["providerZoneId"]})
		if e != nil {
			return result(tasks.NodeResultUnknown, "DNS zone import not durably observed; read local zones before resubmitting")
		}
		return result(tasks.NodeResultSucceeded, "Imported provider-confirmed zone "+zone.ID+" ("+zone.Name+"); no remote record changed")
	}
	return result(tasks.NodeResultFailed, "unsupported DNS inspection")
}
func (s *Server) registerDNSProviderTaskRoutes() {
	s.registerDNSRecordTaskRoutes()
	s.mux.HandleFunc("POST /api/v1/dns/provider-tasks", func(w http.ResponseWriter, r *http.Request) {
		id, requestID, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		m := s.PublicCertificates
		if m == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "DNS_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		body, ok := publicCertificateBody(w, r, 4096)
		if !ok {
			return
		}
		p := map[string]string{"operation": body["operation"], "accountId": body["accountId"], "profileSha256": m.profileSHA}
		if body["operation"] == "import-zone" {
			p["zoneId"] = body["zoneId"]
			p["providerZoneId"] = body["providerZoneId"]
		}
		if len(body) != len(p) || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		// Validate with the same frozen-task parser before task admission. The private
		// worker context, current authorization and credential scope are checked again.
		encoded, _ := json.Marshal(p)
		candidate := tasks.Task{ActionID: actionDNSProviderInspect, Source: "web", OperationClass: tasks.OperationClassStandard, TargetType: tasks.TargetTypeController, TargetResourceID: m.authorityID, Targets: []tasks.Target{{NodeID: m.authorityID}}, Params: string(encoded)}
		if _, err = m.inspectionParams(candidate); err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		ref, err := s.DNS.AccountExecutionBinding(r.Context(), id.SubjectID, p["accountId"])
		meta, metaErr := s.Credentials.ServiceMetadata(r.Context(), id.SubjectID, m.o.DNSCredentialID)
		if err != nil || ref != m.o.DNSCredentialID || metaErr != nil || meta.Kind != "cloudflare_token" || meta.Revision != m.o.DNSCredentialRevision {
			releaseSourceJSON(w, 403, map[string]string{"code": "DNS_CREDENTIAL_NOT_OWNED_OR_CURRENT"})
			return
		}
		t, err := s.Tasks.SubmitController(r.Context(), tasks.SubmitRequest{SubjectID: id.SubjectID, RequestID: requestID, Name: "Inspect DNS provider account or import existing zone", ActionID: actionDNSProviderInspect, Source: audit.SourceWeb, OperationClass: tasks.OperationClassStandard, Params: p, IdempotencyKey: "dns-inspect-" + backupTaskKey("inspect", body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "DNS_INSPECTION_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": t.ID, "status": t.Status, "planHash": t.PlanHash, "approvalRequired": t.ApprovalRequired})
	})
}
