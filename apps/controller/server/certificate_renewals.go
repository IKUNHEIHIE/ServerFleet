package server

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

const actionPublicRenewalConfigure = "certificates.public.renewal.configure"

// All scheduling and policy writes use the same lock. This prevents a local
// disable/adopt racing task submission. Execution independently reads the policy
// at every provider I/O; locking alone is never treated as authorization.
type publicRenewalState struct{ mu sync.Mutex }

func renewalTaskKey(p certificates.PublicRenewal) string {
	return "cert-renew-" + backupTaskKey("cycle", p.ID+":"+strconv.FormatUint(p.Revision, 10)+":"+p.OrderTaskID)
}

func (m *publicCertificateExecution) renewalTaskCurrent(ctx context.Context, t tasks.Task, p map[string]string) error {
	if len(p) != 6 || t.Source != string(audit.SourceSystem) || !deploymentID.MatchString(p["renewalId"]) {
		return auth.ErrDenied
	}
	policy, err := certificates.ReadPublicRenewal(ctx, m.s.DB, p["renewalId"])
	if err != nil || !policy.Enabled || policy.OwnerID != t.SubmitterID || strconv.FormatUint(policy.Revision, 10) != p["renewalRevision"] || policy.OrderTaskID != p["previousOrderTaskId"] || policy.CSRTaskID != p["csrTaskId"] || policy.ZoneID != p["zoneId"] || policy.ProfileSHA256 != p["profileSha256"] || len(t.Targets) != 1 || policy.NodeID != t.Targets[0].NodeID || t.IdempotencyKey != renewalTaskKey(policy) || policy.TaskID != "" && policy.TaskID != t.ID {
		return auth.ErrDenied
	}
	// A task may have been submitted just before a crash and not yet attached to
	// its policy. Its deterministic key is enough to rediscover that SAME task.
	if err = m.renewalPolicyBase(ctx, policy); err != nil {
		return err
	}
	return nil
}

func (m *publicCertificateExecution) renewalPolicyBase(ctx context.Context, p certificates.PublicRenewal) error {
	order, err := m.renewalBase(ctx, p.OwnerID, p.OrderTaskID, p.InstallTaskID)
	if err != nil {
		return err
	}
	if order.CSRTaskID != p.CSRTaskID || order.NodeID != p.NodeID || order.Domain != p.Domain || order.KeyRef != p.KeyRef || order.ZoneID != p.ZoneID || order.ProfileSHA256 != p.ProfileSHA256 || !order.NotAfter.Equal(p.NotAfter) {
		return certificates.ErrACMEConfig
	}
	return nil
}

// A clean CA order alone is insufficient. Only an installed chain for the exact
// public bytes, node, key and CSR can be the base of a renewal policy.
func (m *publicCertificateExecution) renewalBase(ctx context.Context, owner, orderID, installID string) (certificates.PublicOrder, error) {
	denied := certificates.PublicOrder{}
	gate := &composedTaskReadGate{identity: m.s.Identity, tasks: m.s.TasksRepo}
	if m.s.Identity.AuthorizeAdministrator(ctx, owner) != nil || gate.AuthorizeTaskRead(ctx, owner, orderID) != nil || gate.AuthorizeTaskRead(ctx, owner, installID) != nil {
		return denied, auth.ErrDenied
	}
	orderTask, err := m.s.Tasks.Get(ctx, orderID)
	if err != nil || orderTask.ActionID != actionPublicCertificateIssue || orderTask.Status != tasks.StatusCompleted || len(orderTask.Targets) != 1 || orderTask.Targets[0].State != tasks.TargetSucceeded {
		return denied, certificates.ErrACMEConfig
	}
	order, err := certificates.ReadPublicOrder(ctx, m.s.DB, orderID)
	if err != nil || order.Status != "issued" || len(order.PendingCleanup) != 0 || order.ErrorCode != "" || order.PlanSHA256 != orderTask.PlanHash || order.NodeID != orderTask.Targets[0].NodeID || order.ProfileSHA256 != m.profileSHA {
		return denied, certificates.ErrACMEConfig
	}
	installed, err := m.s.Tasks.Get(ctx, installID)
	if err != nil || installed.ActionID != transport.CertificateInstallAction || installed.Status != tasks.StatusCompleted || len(installed.Targets) != 1 || installed.Targets[0].NodeID != order.NodeID || installed.Targets[0].State != tasks.TargetSucceeded {
		return denied, certificates.ErrACMEConfig
	}
	proof, err := transport.ReadCertificateChain(ctx, m.s.DB, installID)
	if err != nil || proof.PlanSHA256 != installed.PlanHash || proof.CSRTaskID != order.CSRTaskID || proof.NodeID != order.NodeID || proof.Request.ChainSHA256 != order.ChainSHA256 || proof.Result.ChainSHA256 != order.ChainSHA256 || proof.Result.KeyRef != order.KeyRef || proof.Result.Domain != order.Domain || !proof.Result.NotAfter.Equal(order.NotAfter) {
		return denied, certificates.ErrACMEConfig
	}
	if _, err = m.csr(ctx, order.CSRTaskID, order.NodeID); err != nil || m.zone(ctx, tasks.Task{SubmitterID: owner}, order.ZoneID, order.Domain) != nil {
		return denied, auth.ErrDenied
	}
	return order, nil
}

func (m *publicCertificateExecution) auditRenewal(ctx context.Context, tx storage.Tx, owner, request, taskID, reason string, p certificates.PublicRenewal, source audit.Source) error {
	return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: owner, RequestID: request, TaskID: taskID, Action: actionPublicRenewalConfigure, Source: source, Result: "success", NodeIDs: []string{p.NodeID}, ReasonCode: reason, At: time.Now().UTC()})
}

func (s *Server) registerPublicRenewalRoutes() {
	s.mux.HandleFunc("GET /api/v1/certificates/public-renewals", func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		if s.PublicCertificates == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "ACME_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		policies, err := certificates.ListPublicRenewals(r.Context(), s.DB)
		if err != nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "RENEWAL_STATE_UNAVAILABLE"})
			return
		}
		items := []map[string]any{}
		gate := &composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}
		for _, p := range policies {
			if p.OwnerID != id.SubjectID || gate.AuthorizeTaskRead(r.Context(), id.SubjectID, p.OrderTaskID) != nil || gate.AuthorizeTaskRead(r.Context(), id.SubjectID, p.InstallTaskID) != nil {
				continue
			}
			state := "scheduled"
			if !p.Enabled {
				state = "disabled"
			} else if p.ProfileSHA256 != s.PublicCertificates.profileSHA {
				state = "configuration_changed"
			}
			if p.TaskID != "" && p.Enabled {
				t, e := s.Tasks.Get(r.Context(), p.TaskID)
				if e != nil || gate.AuthorizeTaskRead(r.Context(), id.SubjectID, p.TaskID) != nil {
					releaseSourceJSON(w, 503, map[string]string{"code": "RENEWAL_TASK_UNAVAILABLE"})
					return
				}
				state = string(t.Status)
				if t.Status == tasks.StatusCompleted {
					state = "issuance_completed_installation_required"
				}
			}
			items = append(items, map[string]any{"policy": p, "state": state, "dueAt": p.NotAfter.Add(-time.Duration(p.RenewBeforeDays) * 24 * time.Hour), "approvalRequired": true, "automaticInstallation": false})
		}
		releaseSourceJSON(w, 200, map[string]any{"items": items})
	})
	s.mux.HandleFunc("POST /api/v1/certificates/public-renewals", s.savePublicRenewal)
}

// One strict POST supports create, enable/disable and adoption with explicit CAS.
// Replaying a committed request returns a revision conflict; changing a stale
// request cannot create a new revision or repeat a failed cycle.
func (s *Server) savePublicRenewal(w http.ResponseWriter, r *http.Request) {
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
	if s.runtimeGate(r.Context()) != nil {
		releaseSourceJSON(w, 409, map[string]string{"code": "RECOVERY_RUNTIME_HELD"})
		return
	}
	body, ok := publicCertificateBody(w, r, 4096)
	if !ok {
		return
	}
	if len(body) != 7 || !deploymentID.MatchString(body["id"]) || !deploymentID.MatchString(body["orderTaskId"]) || !deploymentID.MatchString(body["installTaskId"]) {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return
	}
	expected, e1 := strconv.ParseUint(body["expectedRevision"], 10, 53)
	days, e2 := strconv.Atoi(body["renewBeforeDays"])
	enabled := body["enabled"] == "true"
	operation := body["operation"]
	if e1 != nil || strconv.FormatUint(expected, 10) != body["expectedRevision"] || e2 != nil || strconv.Itoa(days) != body["renewBeforeDays"] || days < 1 || days > 60 || body["enabled"] != "true" && body["enabled"] != "false" || operation != "configure" && operation != "adopt" {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return
	}
	m.renewals.mu.Lock()
	defer m.renewals.mu.Unlock()
	var result certificates.PublicRenewal
	var previous certificates.PublicRenewal
	if expected > 0 {
		previous, err = certificates.ReadPublicRenewal(r.Context(), s.DB, body["id"])
		if err != nil || previous.OwnerID != id.SubjectID {
			releaseSourceJSON(w, 403, map[string]string{"code": "RENEWAL_NOT_OWNED"})
			return
		}
		if expected != previous.Revision {
			releaseSourceJSON(w, 409, map[string]string{"code": "RENEWAL_REVISION_CHANGED"})
			return
		}
		// Recover the submit/bind crash gap before allowing a policy change.
		// Otherwise a new revision could mint a fresh task after unknown effects.
		if previous.TaskID == "" {
			prior, found, e := s.TasksRepo.GetByIdempotencyKey(r.Context(), previous.OwnerID, renewalTaskKey(previous))
			if e != nil {
				releaseSourceJSON(w, 503, map[string]string{"code": "RENEWAL_TASK_UNAVAILABLE"})
				return
			}
			if found {
				previous.TaskID = prior.ID
			}
		}
	}
	// Disabling uses the frozen references even if deployment credentials or
	// provider profile changed; revocation must never depend on CA availability.
	if expected > 0 && !enabled && operation == "configure" && body["orderTaskId"] == previous.OrderTaskID && body["installTaskId"] == previous.InstallTaskID && days == previous.RenewBeforeDays {
		result = previous
		result.Enabled = false
	} else {
		order, e := m.renewalBase(r.Context(), id.SubjectID, body["orderTaskId"], body["installTaskId"])
		if e != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CLEAN_INSTALLED_CERTIFICATE_REQUIRED"})
			return
		}
		result = certificates.PublicRenewal{ID: body["id"], OwnerID: id.SubjectID, Revision: expected + 1, Enabled: enabled, OrderTaskID: order.TaskID, InstallTaskID: body["installTaskId"], CSRTaskID: order.CSRTaskID, NodeID: order.NodeID, Domain: order.Domain, KeyRef: order.KeyRef, ZoneID: order.ZoneID, ProfileSHA256: order.ProfileSHA256, NotAfter: order.NotAfter, RenewBeforeDays: days}
		if expected == 0 {
			if operation != "configure" {
				releaseSourceJSON(w, 409, map[string]string{"code": "RENEWAL_BASE_REQUIRED"})
				return
			}
		} else if operation == "adopt" {
			if previous.TaskID == "" || order.TaskID != previous.TaskID || order.CSRTaskID != previous.CSRTaskID || order.NodeID != previous.NodeID || order.Domain != previous.Domain || order.KeyRef != previous.KeyRef || order.ZoneID != previous.ZoneID || !order.NotAfter.After(previous.NotAfter) {
				releaseSourceJSON(w, 409, map[string]string{"code": "RENEWAL_SUCCESSOR_MISMATCH"})
				return
			}
		} else {
			if order.TaskID != previous.OrderTaskID || body["installTaskId"] != previous.InstallTaskID || previous.TaskID != "" {
				releaseSourceJSON(w, 409, map[string]string{"code": "EXISTING_CYCLE_MUST_BE_RECONCILED"})
				return
			}
		}
	}
	result.Revision = expected + 1
	result.UpdatedAt = time.Now().UTC()
	err = s.DB.InTx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		if err := certificates.SavePublicRenewalTx(ctx, tx, result, expected); err != nil {
			return err
		}
		return m.auditRenewal(ctx, tx, id.SubjectID, requestID, "", "renewal_policy_"+operation, result, audit.SourceWeb)
	})
	if err != nil {
		releaseSourceJSON(w, 409, map[string]string{"code": "RENEWAL_POLICY_REJECTED"})
		return
	}
	releaseSourceJSON(w, 200, result)
}

func (s *Server) runPublicRenewalSchedule(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cycle, cancel := context.WithTimeout(ctx, 30*time.Second)
			s.schedulePublicRenewals(cycle, time.Now().UTC())
			cancel()
		}
	}
}

func (s *Server) schedulePublicRenewals(ctx context.Context, now time.Time) {
	m := s.PublicCertificates
	if m == nil || s.runtimeGate(ctx) != nil {
		return
	}
	m.renewals.mu.Lock()
	defer m.renewals.mu.Unlock()
	policies, err := certificates.ListPublicRenewals(ctx, s.DB)
	if err != nil {
		return
	}
	for _, p := range policies {
		if ctx.Err() != nil {
			return
		}
		if !p.Enabled || p.TaskID != "" || p.ProfileSHA256 != m.profileSHA || now.Before(p.NotAfter.Add(-time.Duration(p.RenewBeforeDays)*24*time.Hour)) {
			continue
		}
		if err = m.renewalPolicyBase(ctx, p); err != nil {
			continue
		}
		meta, e := s.Credentials.ServiceMetadata(ctx, p.OwnerID, m.o.DNSCredentialID)
		if e != nil || meta.Revision != m.o.DNSCredentialRevision || meta.Kind != "cloudflare_token" {
			continue
		}
		key := renewalTaskKey(p)
		t, err := s.Tasks.Submit(ctx, tasks.SubmitRequest{SubjectID: p.OwnerID, RequestID: key, Name: "Renew node-local public certificate", ActionID: actionPublicCertificateIssue, Source: audit.SourceSystem, OperationClass: tasks.OperationClassModifying, RequireApproval: true, TargetNodeIDs: []string{p.NodeID}, Params: map[string]string{"csrTaskId": p.CSRTaskID, "zoneId": p.ZoneID, "profileSha256": p.ProfileSHA256, "renewalId": p.ID, "renewalRevision": strconv.FormatUint(p.Revision, 10), "previousOrderTaskId": p.OrderTaskID}, IdempotencyKey: key})
		if err != nil {
			continue
		}
		// Commit uncertainty only rediscovers the same deterministic task next
		// tick. It never invents a different key or resubmits provider effects.
		_ = s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			if err := certificates.BindPublicRenewalTaskTx(ctx, tx, p.ID, p.Revision, t.ID); err != nil {
				return err
			}
			return m.auditRenewal(ctx, tx, p.OwnerID, key, t.ID, "renewal_approval_task_registered", p, audit.SourceSystem)
		})
	}
}
