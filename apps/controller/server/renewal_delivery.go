package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func (s *Server) deliveryPolicy(ctx context.Context, subject, id string) (certificates.PublicRenewal, error) {
	p, e := certificates.ReadPublicRenewal(ctx, s.DB, id)
	if e != nil || p.OwnerID != subject || s.Identity.AuthorizeAdministrator(ctx, subject) != nil || s.runtimeGate(ctx) != nil {
		return p, auth.ErrDenied
	}
	gate := &composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}
	for _, tid := range []string{p.OrderTaskID, p.InstallTaskID, p.TaskID} {
		if tid != "" && gate.AuthorizeTaskRead(ctx, subject, tid) != nil {
			return p, auth.ErrDenied
		}
	}
	return p, nil
}
func (s *Server) deliveryReference(ctx context.Context, p certificates.PublicRenewal, reference string) bool {
	if reference == nodeCertificatePrefix+p.InstallTaskID {
		return true
	}
	if !strings.HasPrefix(reference, transport.CertificateSetPrefix) {
		return false
	}
	set, e := transport.ReadCertificateSet(ctx, s.DB, reference)
	if e != nil {
		return false
	}
	for _, entry := range set.Entries {
		if entry.NodeID == p.NodeID && entry.TaskID == p.InstallTaskID {
			return true
		}
	}
	return false
}
func (s *Server) deliveryPublication(ctx context.Context, p certificates.PublicRenewal, id string) (publishing.Plan, error) {
	plan, e := s.Publishing.LatestCertificatePlan(ctx, p.OwnerID, id)
	if e != nil || plan.Status != publishing.PlanCompleted || !plan.TargetsPersisted || !plan.HTTPS || !certificateMatchesDomain(p.Domain, plan.Domain) || !s.deliveryReference(ctx, p, plan.CertificateID) {
		return plan, publishing.ErrProductionTarget
	}
	return plan, nil
}
func (s *Server) saveDelivery(ctx context.Context, d *certificates.RenewalDelivery, reason string) error {
	expected := d.Revision
	d.Revision++
	d.UpdatedAt = time.Now().UTC()
	if d.Cycle != nil {
		d.Cycle.UpdatedAt = d.UpdatedAt
	}
	source := audit.SourceSystem
	if reason == "renewal_delivery_configured_by_web" {
		source = audit.SourceWeb
	}
	e := s.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		if e := certificates.SaveRenewalDeliveryTx(c, tx, *d, expected); e != nil {
			return e
		}
		taskID := ""
		if d.Cycle != nil {
			taskID = d.Cycle.InstallTaskID
		}
		return s.Audit.AppendTx(c, tx, audit.Event{SubjectID: d.OwnerID, Action: actionPublicRenewalConfigure, Result: "success", RequestID: "renew-delivery-" + backupTaskKey("audit", d.PolicyID+":"+d.UpdatedAt.Format(time.RFC3339Nano)), TaskID: taskID, Source: source, ReasonCode: reason, At: d.UpdatedAt})
	})
	if e != nil {
		d.Revision = expected
	}
	return e
}
func (s *Server) registerRenewalDeliveryRoutes() {
	s.registerRenewalRecoveryRoute()
	s.mux.HandleFunc("GET /api/v1/certificates/public-renewals/{policyId}/delivery", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		p, e := s.deliveryPolicy(r.Context(), subject, r.PathValue("policyId"))
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		d, e := certificates.ReadRenewalDelivery(r.Context(), s.DB, p.ID)
		if storage.IsNotFound(e) {
			d = certificates.RenewalDelivery{PolicyID: p.ID, OwnerID: subject, PublicationIDs: []string{}}
		} else if e != nil {
			writeOperationsError(w, e)
			return
		}
		history, e := certificates.RenewalDeliveryHistory(r.Context(), s.DB, p.ID)
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		receipts := []tasks.IncidentTaskReceipt{}
		seenReceipts := map[string]bool{}
		if d.Cycle != nil {
			for _, id := range []string{d.Cycle.IssueTaskID, d.Cycle.InstallTaskID, d.Cycle.RecoveredOrderTaskID, d.Cycle.RecoveredInstallTaskID} {
				if id != "" && !seenReceipts[id] {
					seenReceipts[id] = true
					v, e := s.inspectIncidentTask(r.Context(), subject, id)
					if e != nil {
						writeOperationsError(w, auth.ErrDenied)
						return
					}
					receipts = append(receipts, v)
				}
			}
			for _, pub := range d.Cycle.Publications {
				for _, g := range s.deliveryGatewayTaskIDs(r.Context(), subject, pub.ID) {
					v, e := s.inspectIncidentTask(r.Context(), subject, g)
					if e != nil {
						writeOperationsError(w, auth.ErrDenied)
						return
					}
					receipts = append(receipts, v)
				}
			}
		}
		releaseSourceJSON(w, 200, map[string]any{"delivery": d, "history": history, "tasks": receipts})
	})
	s.mux.HandleFunc("POST /api/v1/certificates/public-renewals/{policyId}/delivery", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var in struct {
			ExpectedRevision uint64   `json:"expectedRevision"`
			PolicyRevision   uint64   `json:"policyRevision"`
			Enabled          bool     `json:"enabled"`
			PublicationIDs   []string `json:"publicationIds"`
		}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		if len(in.PublicationIDs) > 8 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		if s.PublicCertificates == nil || s.Publishing == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "ACME_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		s.PublicCertificates.renewals.mu.Lock()
		defer s.PublicCertificates.renewals.mu.Unlock()
		p, e := s.deliveryPolicy(r.Context(), subject, r.PathValue("policyId"))
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		if p.Revision != in.PolicyRevision {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		d, e := certificates.ReadRenewalDelivery(r.Context(), s.DB, p.ID)
		if storage.IsNotFound(e) {
			d = certificates.RenewalDelivery{PolicyID: p.ID, OwnerID: subject}
		} else if e != nil {
			writeOperationsError(w, e)
			return
		}
		if d.Revision != in.ExpectedRevision || d.OwnerID != subject {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		if in.Enabled {
			if !p.Enabled {
				writeOperationsError(w, auth.ErrDenied)
				return
			}
			if d.Cycle != nil && (d.Cycle.State != "completed" && d.Cycle.State != "recovered") && d.Cycle.PolicyRevision == p.Revision {
				writeOperationsError(w, tasks.ErrIncidentConflict)
				return
			}
			seen := map[string]bool{}
			for _, id := range in.PublicationIDs {
				plan, lookupErr := s.deliveryPublication(r.Context(), p, id)
				if lookupErr != nil {
					writeOperationsError(w, lookupErr)
					return
				}
				if seen[plan.ID] {
					writeOperationsError(w, tasks.ErrInvalidTask)
					return
				}
				seen[plan.ID] = true
				if _, e = s.deliveryPublication(r.Context(), p, id); e != nil {
					writeOperationsError(w, e)
					return
				}
			}
		} else if !slices.Equal(in.PublicationIDs, d.PublicationIDs) {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		d.Enabled = in.Enabled
		d.PublicationIDs = append([]string{}, in.PublicationIDs...)
		if d.Cycle != nil && d.Cycle.PolicyRevision != p.Revision {
			d.Cycle = nil
		}
		if e = s.saveDelivery(r.Context(), &d, "renewal_delivery_configured_by_web"); e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, d)
	})
}
func (s *Server) deliveryGatewayTaskIDs(ctx context.Context, subject, id string) []string {
	plan, _, e := s.Publishing.Get(ctx, subject, id)
	if e != nil {
		return nil
	}
	out := []string{}
	for _, g := range plan.GatewayIDs {
		key := gatewayTaskIdempotencyKey(plan.ID, g, "-apply", len(plan.GatewayIDs) > 1)
		t, found, e := s.TasksRepo.GetByIdempotencyKey(ctx, subject, key)
		if e == nil && found {
			out = append(out, t.ID)
		}
	}
	return out
}
func (s *Server) replacementCertificate(ctx context.Context, p certificates.PublicRenewal, oldRef, installID string) (string, error) {
	if oldRef == nodeCertificatePrefix+p.InstallTaskID {
		return nodeCertificatePrefix + installID, nil
	}
	set, e := transport.ReadCertificateSet(ctx, s.DB, oldRef)
	if e != nil {
		return "", e
	}
	receipt, e := transport.ReadCertificateChain(ctx, s.DB, installID)
	if e != nil {
		return "", e
	}
	replaced := false
	for i, entry := range set.Entries {
		if entry.NodeID == p.NodeID && entry.TaskID == p.InstallTaskID {
			set.Entries[i] = transport.CertificateSetEntryFromReceipt(receipt)
			replaced = true
		}
		if (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, p.OwnerID, set.Entries[i].TaskID) != nil {
			return "", auth.ErrDenied
		}
	}
	if !replaced {
		return "", publishing.ErrProductionCertificate
	}
	frozen, e := transport.FreezeCertificateSet(set.Entries)
	if e != nil {
		return "", e
	}
	frozen.CreatedAt = time.Now().UTC()
	e = s.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error { return transport.RecordCertificateSetTx(c, tx, frozen) })
	return frozen.ID, e
}
func (s *Server) driveRenewalDeliveries(ctx context.Context) {
	m := s.PublicCertificates
	if m == nil || s.runtimeGate(ctx) != nil {
		return
	}
	m.renewals.mu.Lock()
	defer m.renewals.mu.Unlock()
	policies, e := certificates.ListPublicRenewals(ctx, s.DB)
	if e != nil || len(policies) == 0 {
		return
	}
	for n := 0; n < len(policies) && n < 50; n++ {
		if ctx.Err() != nil {
			return
		}
		i := m.renewals.deliveryCursor % len(policies)
		m.renewals.deliveryCursor++
		p := policies[i]
		d, e := certificates.ReadRenewalDelivery(ctx, s.DB, p.ID)
		if e != nil || !d.Enabled || !p.Enabled || p.TaskID == "" {
			continue
		}
		if _, e = s.deliveryPolicy(ctx, p.OwnerID, p.ID); e != nil {
			continue
		}
		if d.Cycle != nil && d.Cycle.PolicyRevision == p.Revision && (d.Cycle.State == "blocked" || d.Cycle.State == "completed" || d.Cycle.State == "recovered") {
			continue
		}
		if d.Cycle == nil || d.Cycle.IssueTaskID != p.TaskID {
			c := &certificates.RenewalDeliveryCycle{ID: "renew-cycle-" + backupTaskKey("delivery", p.ID+":"+p.TaskID), PolicyRevision: p.Revision, IssueTaskID: p.TaskID, State: "issuance", Publications: []certificates.RenewalPublication{}}
			valid := true
			for _, id := range d.PublicationIDs {
				plan, e := s.deliveryPublication(ctx, p, id)
				if errors.Is(e, publishing.ErrStageAwaitingTask) {
					valid = false
					c = nil
					break
				}
				if e != nil {
					valid = false
					break
				}
				c.Publications = append(c.Publications, certificates.RenewalPublication{BaseID: plan.ID, ID: "renew-pub-" + backupTaskKey("publication", c.ID+":"+plan.ID), PreviousCertificateID: plan.CertificateID})
			}
			if c == nil {
				continue
			}
			d.Cycle = c
			if !valid {
				c.State = "blocked"
				c.ErrorCode = "PUBLICATION_BASE_CHANGED"
			}
			if s.saveDelivery(ctx, &d, "renewal_delivery_cycle_started") != nil || !valid {
				continue
			}
		}
		if d.Cycle.PolicyRevision != p.Revision {
			continue
		}
		s.advanceRenewalDelivery(ctx, p, &d)
	}
}
func (s *Server) advanceRenewalDelivery(ctx context.Context, p certificates.PublicRenewal, d *certificates.RenewalDelivery) {
	c := d.Cycle
	block := func(code string) {
		c.State = "blocked"
		c.ErrorCode = code
		_ = s.saveDelivery(ctx, d, "renewal_delivery_stopped_no_replay")
	}
	if c.ID != "renew-cycle-"+backupTaskKey("delivery", p.ID+":"+p.TaskID) {
		block("CYCLE_PROOF_MISMATCH")
		return
	}
	issue, e := s.Tasks.Get(ctx, c.IssueTaskID)
	if e != nil {
		block("ISSUANCE_UNAVAILABLE")
		return
	}
	if !issue.Status.Terminal() {
		return
	}
	if !cleanDeliveryTask(issue) {
		block("ISSUANCE_FAILED_OR_UNKNOWN")
		return
	}
	var issueParams map[string]string
	if issue.SubmitterID != p.OwnerID || issue.ActionID != actionPublicCertificateIssue || json.Unmarshal([]byte(issue.Params), &issueParams) != nil || s.PublicCertificates.renewalTaskCurrent(ctx, issue, issueParams) != nil {
		block("ISSUANCE_TASK_MISMATCH")
		return
	}
	order, e := certificates.ReadPublicOrder(ctx, s.DB, issue.ID)
	if e == nil && len(order.PendingCleanup) > 0 {
		return
	}
	if e != nil || order.Status != "issued" || order.PlanSHA256 != issue.PlanHash || order.ProfileSHA256 != p.ProfileSHA256 || order.ProfileSHA256 != s.PublicCertificates.profileSHA || order.ZoneID != p.ZoneID || !order.NotAfter.After(time.Now()) || len(order.PendingCleanup) > 0 || order.ErrorCode != "" || order.CSRTaskID != p.CSRTaskID || order.NodeID != p.NodeID || order.Domain != p.Domain || order.KeyRef != p.KeyRef || !order.NotAfter.After(p.NotAfter) {
		block("ISSUANCE_PROOF_MISMATCH")
		return
	}
	if c.InstallTaskID == "" {
		csr, e := transport.ReadCertificateCSR(ctx, s.DB, p.CSRTaskID)
		if e != nil {
			block("CSR_PROOF_UNAVAILABLE")
			return
		}
		params := map[string]string{"domain": p.Domain, "keyRef": p.KeyRef, "csrSha256": csr.PublicResult.SHA256, "certificatePemBase64": base64.StdEncoding.EncodeToString(order.ChainPEM), "chainSha256": order.ChainSHA256, "csrTaskId": p.CSRTaskID}
		key := "renew-install-" + backupTaskKey("installation", c.ID)
		t, e := s.Tasks.Submit(ctx, tasks.SubmitRequest{SubjectID: p.OwnerID, RequestID: c.ID, Name: "Install renewed public certificate", ActionID: transport.CertificateInstallAction, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Source: audit.SourceWeb, TargetNodeIDs: []string{p.NodeID}, Params: params, IdempotencyKey: key})
		if e != nil {
			block("INSTALLATION_ADMISSION_REJECTED")
			return
		}
		c.InstallTaskID = t.ID
		c.State = "installation"
		if s.saveDelivery(ctx, d, "renewal_installation_task_registered") != nil {
			return
		}
	}
	installed, e := s.Tasks.Get(ctx, c.InstallTaskID)
	if e != nil {
		block("INSTALLATION_UNAVAILABLE")
		return
	}
	if !installed.Status.Terminal() {
		return
	}
	if !cleanDeliveryTask(installed) {
		block("INSTALLATION_FAILED_OR_UNKNOWN")
		return
	}
	if _, e = s.PublicCertificates.renewalBase(ctx, p.OwnerID, c.IssueTaskID, c.InstallTaskID); e != nil {
		block("INSTALLATION_PROOF_MISMATCH")
		return
	}
	for i := range c.Publications {
		pub := &c.Publications[i]
		if pub.CertificateID == "" {
			pub.CertificateID, e = s.replacementCertificate(ctx, p, pub.PreviousCertificateID, c.InstallTaskID)
			if e != nil {
				block("CERTIFICATE_SET_CHANGED")
				return
			}
			c.State = "publication"
			if s.saveDelivery(ctx, d, "renewal_publication_intent_frozen") != nil {
				return
			}
		}
		plan, e := s.Publishing.ReplaceCertificate(ctx, p.OwnerID, c.ID, pub.ID, pub.BaseID, pub.PreviousCertificateID, pub.CertificateID)
		if e != nil {
			block("PUBLICATION_ADMISSION_REJECTED")
			return
		}
		if plan.Status == publishing.PlanCompleted {
			continue
		}
		if plan.Status != publishing.PlanQueued && plan.Status != publishing.PlanRunning {
			block("PUBLICATION_FAILED_REQUIRES_REVIEW")
			return
		}
		for _, id := range s.deliveryGatewayTaskIDs(ctx, p.OwnerID, plan.ID) {
			t, e := s.Tasks.Get(ctx, id)
			if e != nil || t.Status.Terminal() && !cleanDeliveryTask(t) {
				block("GATEWAY_FAILED_OR_UNKNOWN")
				return
			}
		}
		step, done := context.WithTimeout(publishing.ObserveTasksWithoutWaiting(ctx), 8*time.Second)
		result, err := s.Publishing.Run(step, p.OwnerID, c.ID, plan.ID)
		done()
		if err != nil && !errors.Is(err, publishing.ErrStageAwaitingTask) && !errors.Is(err, context.DeadlineExceeded) {
			block("PUBLICATION_OR_HEALTH_CHECK_FAILED")
			return
		}
		if result.Status != publishing.PlanCompleted {
			return
		}
	}
	// Adoption, next-cycle bindings and proof references commit together. A crash
	// before this point can only rediscover the same install and publication IDs.
	next := p
	next.Revision++
	next.OrderTaskID = c.IssueTaskID
	next.InstallTaskID = c.InstallTaskID
	next.TaskID = ""
	next.NotAfter = order.NotAfter
	next.UpdatedAt = time.Now().UTC()
	expected := d.Revision
	d.Revision++
	d.PublicationIDs = []string{}
	for _, pub := range c.Publications {
		d.PublicationIDs = append(d.PublicationIDs, pub.ID)
	}
	c.State = "completed"
	c.UpdatedAt = next.UpdatedAt
	d.UpdatedAt = next.UpdatedAt
	_ = s.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if e := certificates.SavePublicRenewalTx(txCtx, tx, next, p.Revision); e != nil {
			return e
		}
		if e := certificates.SaveRenewalDeliveryTx(txCtx, tx, *d, expected); e != nil {
			return e
		}
		return s.PublicCertificates.auditRenewal(txCtx, tx, p.OwnerID, c.ID, c.InstallTaskID, "renewal_installed_published_and_adopted", next, audit.SourceSystem)
	})
}
func cleanDeliveryTask(t tasks.Task) bool {
	if t.Status != tasks.StatusCompleted || len(t.Targets) == 0 {
		return false
	}
	for _, v := range t.Targets {
		if v.State != tasks.TargetSucceeded || v.Code == tasks.NodeResultUnknown {
			return false
		}
	}
	return true
}
