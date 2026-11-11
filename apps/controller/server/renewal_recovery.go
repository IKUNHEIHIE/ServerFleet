package server

import (
	"context"
	"net/http"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
	"time"
)

// Recovery adopts independently successful proofs, never replays an uncertain
// operation or rewrites its original task/publication/cycle evidence.
func (s *Server) registerRenewalRecoveryRoute() {
	s.mux.HandleFunc("POST /api/v1/certificates/public-renewals/{policyId}/delivery/recover", func(w http.ResponseWriter, r *http.Request) {
		subject, _, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var in struct {
			ExpectedRevision uint64   `json:"expectedRevision"`
			PolicyRevision   uint64   `json:"policyRevision"`
			OrderTaskID      string   `json:"orderTaskId"`
			InstallTaskID    string   `json:"installTaskId"`
			PublicationIDs   []string `json:"publicationIds"`
			IncidentIDs      []string `json:"incidentIds"`
			Note             string   `json:"note"`
			Confirmed        bool     `json:"confirmed"`
			Enabled          bool     `json:"enabled"`
		}
		if !decodeOperationsBody(w, r, &in) {
			return
		}
		note, e := operatorEvidence(strings.TrimSpace(in.Note))
		if e != nil || note == "" || len(note) > 4096 || !in.Confirmed || len(in.PublicationIDs) > 8 || len(in.IncidentIDs) > 66 || !deploymentID.MatchString(in.OrderTaskID) || !deploymentID.MatchString(in.InstallTaskID) {
			writeOperationsError(w, tasks.ErrInvalidTask)
			return
		}
		m := s.PublicCertificates
		if m == nil || s.Publishing == nil || s.Incidents == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "ACME_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		m.renewals.mu.Lock()
		defer m.renewals.mu.Unlock()
		p, e := s.deliveryPolicy(r.Context(), subject, r.PathValue("policyId"))
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		d, e := certificates.ReadRenewalDelivery(r.Context(), s.DB, p.ID)
		if e != nil || d.OwnerID != subject || d.Revision != in.ExpectedRevision || p.Revision != in.PolicyRevision || d.Cycle == nil || d.Cycle.State != "blocked" || d.Cycle.PolicyRevision != p.Revision || d.Cycle.IssueTaskID != p.TaskID {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		c := d.Cycle
		ids := []string{c.IssueTaskID, c.InstallTaskID}
		for _, pub := range c.Publications {
			ids = append(ids, s.deliveryGatewayTaskIDs(r.Context(), subject, pub.ID)...)
		}
		unknown := map[string]bool{}
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if _, e = s.inspectIncidentTask(r.Context(), subject, id); e != nil {
				writeOperationsError(w, auth.ErrDenied)
				return
			}
			t, err := s.Tasks.Get(r.Context(), id)
			if err != nil || !t.Status.Terminal() {
				writeOperationsError(w, tasks.ErrIncidentConflict)
				return
			}
			if t.Status == tasks.StatusUnknown {
				unknown[id] = true
			}
			for _, target := range t.Targets {
				if target.State == tasks.TargetUnknown || target.Code == tasks.NodeResultUnknown {
					unknown[id] = true
				}
			}
		}
		checkCases := func(ctx context.Context) error {
			covered := map[string]bool{}
			cases := map[string]bool{}
			for _, id := range in.IncidentIDs {
				if cases[id] {
					return tasks.ErrInvalidTask
				}
				cases[id] = true
				incident, err := s.Incidents.Get(ctx, subject, id)
				if err != nil || incident.Source.OwnerID != subject || incident.Source.Kind != "task" || !unknown[incident.Source.TaskID] || incident.State != "resolved" || incident.Proposal == nil || incident.Assessment == nil || incident.Assessment.NeedsRecheck {
					return tasks.ErrIncidentConflict
				}
				covered[incident.Source.TaskID] = true
			}
			for id := range unknown {
				if !covered[id] {
					return tasks.ErrIncidentConflict
				}
			}
			return nil
		}
		if e = checkCases(r.Context()); e != nil {
			writeOperationsError(w, e)
			return
		}
		order, e := m.renewalBase(r.Context(), subject, in.OrderTaskID, in.InstallTaskID)
		if e != nil || order.CSRTaskID != p.CSRTaskID || order.NodeID != p.NodeID || order.Domain != p.Domain || order.KeyRef != p.KeyRef || order.ZoneID != p.ZoneID || !order.NotAfter.After(p.NotAfter) || !order.NotAfter.After(time.Now()) {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		original, e := s.Tasks.Get(r.Context(), c.IssueTaskID)
		candidate, ce := s.Tasks.Get(r.Context(), in.OrderTaskID)
		if e != nil || ce != nil || candidate.CreatedAt.Before(original.CreatedAt) {
			writeOperationsError(w, tasks.ErrIncidentConflict)
			return
		}
		next := p
		next.Revision++
		next.OrderTaskID = in.OrderTaskID
		next.InstallTaskID = in.InstallTaskID
		next.TaskID = ""
		next.NotAfter = order.NotAfter
		next.ProfileSHA256 = order.ProfileSHA256
		next.UpdatedAt = time.Now().UTC()
		pubs := []string{}
		canonical := map[string]bool{}
		for _, id := range in.PublicationIDs {
			plan, err := s.deliveryPublication(r.Context(), next, id)
			if err != nil || canonical[plan.ID] || plan.CreatedAt.Before(original.CreatedAt) {
				writeOperationsError(w, tasks.ErrIncidentConflict)
				return
			}
			canonical[plan.ID] = true
			pubs = append(pubs, plan.ID)
		}
		expected := d.Revision
		d.Revision++
		d.Enabled = in.Enabled
		d.PublicationIDs = pubs
		d.UpdatedAt = next.UpdatedAt
		c.PreviousErrorCode = c.ErrorCode
		c.ErrorCode = ""
		c.State = "recovered"
		c.UpdatedAt = next.UpdatedAt
		c.RecoveredOrderTaskID = in.OrderTaskID
		c.RecoveredInstallTaskID = in.InstallTaskID
		c.RecoveryIncidentIDs = append([]string{}, in.IncidentIDs...)
		c.RecoveryPublicationIDs = pubs
		c.RecoveryNote = note
		// Current task facts and independent case review are checked again immediately
		// before the same transaction commits both CAS-protected baselines.
		if e = checkCases(r.Context()); e != nil {
			writeOperationsError(w, e)
			return
		}
		e = s.DB.InTx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
			if err := certificates.SavePublicRenewalTx(ctx, tx, next, p.Revision); err != nil {
				return err
			}
			if err := certificates.SaveRenewalDeliveryTx(ctx, tx, d, expected); err != nil {
				return err
			}
			return m.auditRenewal(ctx, tx, subject, c.ID, in.InstallTaskID, "renewal_recovered_with_successful_proofs", next, audit.SourceWeb)
		})
		if e != nil {
			writeOperationsError(w, e)
			return
		}
		releaseSourceJSON(w, 200, d)
	})
}
