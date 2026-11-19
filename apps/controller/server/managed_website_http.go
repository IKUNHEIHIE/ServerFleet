package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

const actionManagedWebsiteConfigure = "certificates.website.configure"

func managedWebsiteError(w http.ResponseWriter, status int, code string) {
	releaseSourceJSON(w, status, map[string]string{"code": code})
}

func (s *Server) registerManagedWebsiteRoutes() {
	s.mux.HandleFunc("GET /api/v1/certificates/websites", s.listManagedWebsites)
	s.mux.HandleFunc("POST /api/v1/certificates/websites", s.changeManagedWebsite)
	s.mux.HandleFunc("POST /api/v1/agent/website-certificate", s.serveManagedWebsiteBundle)
}

func (s *Server) managedWebsitePrincipal(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	id, request, err := s.authenticateIdentity(r)
	if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
		managedWebsiteError(w, 403, "NOT_AUTHORIZED")
		return "", "", false
	}
	return id.SubjectID, request, true
}

func (m *publicCertificateExecution) managedTask(ctx context.Context, owner, key string) (tasks.Task, bool, error) {
	if key == "" {
		return tasks.Task{}, false, nil
	}
	return m.s.TasksRepo.GetByIdempotencyKey(ctx, owner, key)
}

type managedTaskView struct {
	PlanHash string                       `json:"planHash"`
	ID       string                       `json:"id"`
	Status   tasks.TaskStatus             `json:"status"`
	Targets  map[string]tasks.TargetState `json:"targets,omitempty"`
}

func (m *publicCertificateExecution) managedTaskView(ctx context.Context, owner, key string, targets bool) *managedTaskView {
	t, found, err := m.managedTask(ctx, owner, key)
	if err != nil || !found {
		return nil
	}
	if (&composedTaskReadGate{identity: m.s.Identity, tasks: m.s.TasksRepo}).AuthorizeTaskRead(ctx, owner, t.ID) != nil {
		return nil
	}
	v := &managedTaskView{ID: t.ID, Status: t.Status, PlanHash: t.PlanHash}
	if targets {
		v.Targets = map[string]tasks.TargetState{}
		for _, target := range t.Targets {
			v.Targets[target.NodeID] = target.State
		}
	}
	return v
}

func (s *Server) listManagedWebsites(w http.ResponseWriter, r *http.Request) {
	owner, _, ok := s.managedWebsitePrincipal(w, r)
	if !ok {
		return
	}
	m := s.PublicCertificates
	items := []map[string]any{}
	choices := []map[string]any{}
	ctx := r.Context()
	if m != nil {
		inventory, err := s.Nodes.ListVisible(ctx, owner)
		if err != nil {
			managedWebsiteError(w, 503, "WEBSITE_STATE_UNAVAILABLE")
			return
		}
		visible := map[string]bool{}
		for _, node := range inventory {
			if node.Status == nodes.StatusRevoked {
				continue
			}
			online := node.Status == nodes.StatusOnline
			ready := online && s.Links.HasCurrentCapabilities(ctx, node.ID, transport.WebsiteCertificateInstallAction) && s.authorizeManagedTargets(ctx, owner, []string{node.ID})
			choices = append(choices, map[string]any{"id": node.ID, "name": node.Name, "online": online, "supported": ready})
			visible[node.ID] = true
		}
		sites, err := m.managedWebsites(ctx, owner)
		if err != nil {
			managedWebsiteError(w, 503, "WEBSITE_STATE_UNAVAILABLE")
			return
		}
		for _, site := range sites {
			var targets []string
			_ = json.Unmarshal([]byte(site.Targets), &targets)
			targets = slices.DeleteFunc(targets, func(id string) bool { return !visible[id] })
			item := map[string]any{"domain": site.Domain, "autoRenew": site.AutoRenew, "nodeIds": targets, "generation": site.Generation, "issue": m.managedTaskView(ctx, owner, site.IssueKey, false), "delivery": m.managedTaskView(ctx, owner, site.DeliveryKey, true)}
			if site.OrderID != "" {
				if order, err := certificates.ReadPublicOrder(ctx, s.DB, site.OrderID); err == nil {
					item["installDirectory"] = fmt.Sprintf("managed/%x/current", sha256.Sum256([]byte(site.Domain)))
					item["notAfter"] = order.NotAfter
					item["renewAt"] = time.UnixMilli(site.RenewAtMS).UTC()
				}
			}
			items = append(items, item)
		}
	}
	releaseSourceJSON(w, 200, map[string]any{"configured": m != nil, "items": items, "nodes": choices})
}

func (s *Server) changeManagedWebsite(w http.ResponseWriter, r *http.Request) {
	owner, request, ok := s.managedWebsitePrincipal(w, r)
	if !ok {
		return
	}
	m := s.PublicCertificates
	if m == nil {
		managedWebsiteError(w, 503, "ACME_DEPLOYMENT_UNAVAILABLE")
		return
	}
	body, ok := publicCertificateBody(w, r, 24<<10)
	if !ok {
		return
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(body["domain"])), ".")
	if !transport.ValidWebsiteDomain(domain) || !deploymentID.MatchString(body["idempotencyKey"]) || len(body) != 4 {
		managedWebsiteError(w, 400, "INVALID_REQUEST")
		return
	}
	m.managedMu.Lock()
	defer m.managedMu.Unlock()
	ctx := r.Context()
	site, err := m.managedWebsite(ctx, domain)
	found := err == nil
	if !found && !storage.IsNotFound(err) {
		managedWebsiteError(w, 503, "WEBSITE_STATE_UNAVAILABLE")
		return
	}
	if found && site.Owner != owner {
		managedWebsiteError(w, 403, "NOT_AUTHORIZED")
		return
	}
	enabled := body["autoRenew"] == "true"
	switch body["operation"] {
	case "issue":
		if body["autoRenew"] != "true" && body["autoRenew"] != "false" {
			managedWebsiteError(w, 400, "INVALID_REQUEST")
			return
		}
		key := "website-issue-" + backupTaskKey(domain, body["idempotencyKey"])
		if t, exists, err := m.managedTask(ctx, owner, key); err == nil && exists {
			releaseManagedTask(w, t)
			return
		}
		zoneID, err := m.findManagedZone(ctx, owner, domain)
		if err != nil {
			managedWebsiteError(w, 409, "WEBSITE_DNS_ZONE_UNAVAILABLE")
			return
		}
		if found {
			t, exists, e := m.managedTask(ctx, owner, site.IssueKey)
			if site.Profile == m.profileSHA && exists && t.ID == site.OrderID && t.Status == tasks.StatusCompleted {
				order, err := certificates.ReadPublicOrder(ctx, s.DB, site.OrderID)
				if err != nil {
					managedWebsiteError(w, 503, "WEBSITE_STATE_UNAVAILABLE")
					return
				}
				if order.NotAfter.After(time.Now()) {
					managedWebsiteError(w, 409, "WEBSITE_CERTIFICATE_ALREADY_ISSUED")
					return
				}
			}
			if e != nil || exists && !t.Status.Terminal() {
				managedWebsiteError(w, 409, "WEBSITE_ISSUE_IN_PROGRESS")
				return
			}
		}
		err = s.changeManagedWebsiteState(ctx, owner, request, "website_issue_requested", func(tx storage.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO cx_managed_websites(domain,owner_id,zone_id,profile_sha256,auto_renew,issue_key,issue_source)
				VALUES(?,?,?,?,?,?,'web') ON CONFLICT(domain) DO UPDATE SET zone_id=excluded.zone_id,profile_sha256=excluded.profile_sha256,
				auto_renew=excluded.auto_renew,issue_key=excluded.issue_key,issue_source='web' WHERE cx_managed_websites.owner_id=excluded.owner_id`,
				domain, owner, zoneID, m.profileSHA, intBool(enabled), key)
			return err
		})
		if err == nil {
			site, err = m.managedWebsite(ctx, domain)
		}
		if err == nil {
			if t, e := m.submitManagedIssue(ctx, site); e == nil {
				releaseManagedTask(w, t)
				return
			}
		}
	case "auto-renew":
		if !found || body["autoRenew"] != "true" && body["autoRenew"] != "false" {
			managedWebsiteError(w, 400, "INVALID_REQUEST")
			return
		}
		err = s.changeManagedWebsiteState(ctx, owner, request, "website_renewal_setting_changed", func(tx storage.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE cx_managed_websites SET auto_renew=? WHERE domain=? AND owner_id=?`, intBool(enabled), domain, owner)
			return err
		})
		if err == nil {
			releaseSourceJSON(w, 200, map[string]bool{"autoRenew": enabled})
			return
		}
	case "deliver":
		if !found || site.OrderID == "" {
			managedWebsiteError(w, 409, "WEBSITE_CERTIFICATE_UNAVAILABLE")
			return
		}
		targetIDs := strings.Split(body["nodeIds"], ",")
		if len(targetIDs) > 100 || !s.validManagedTargets(ctx, owner, targetIDs) {
			managedWebsiteError(w, 409, "WEBSITE_NODES_UNAVAILABLE")
			return
		}
		slices.Sort(targetIDs)
		raw, _ := json.Marshal(targetIDs)
		key := "website-delivery-" + backupTaskKey(domain, body["idempotencyKey"])
		if t, exists, err := m.managedTask(ctx, owner, key); err == nil && exists {
			releaseManagedTask(w, t)
			return
		}
		t, exists, e := m.managedTask(ctx, owner, site.DeliveryKey)
		if e != nil || exists && !t.Status.Terminal() {
			managedWebsiteError(w, 409, "WEBSITE_DELIVERY_IN_PROGRESS")
			return
		}
		err = s.changeManagedWebsiteState(ctx, owner, request, "website_targets_selected", func(tx storage.Tx) error {
			result, e := tx.Exec(ctx, `UPDATE cx_managed_websites SET targets_json=?,delivery_key=?,delivery_source='web',redeploy=0 WHERE domain=? AND order_id=?`, string(raw), key, domain, site.OrderID)
			if e != nil {
				return e
			}
			if n, e := result.RowsAffected(); e != nil || n != 1 {
				return tasks.ErrInvalidTask
			}
			return nil
		})
		if err == nil {
			site, err = m.managedWebsite(ctx, domain)
		}
		if err == nil && site.DeliveryKey == key {
			if t, e := m.submitManagedDelivery(ctx, site); e == nil {
				releaseManagedTask(w, t)
				return
			}
		}
	default:
		managedWebsiteError(w, 400, "INVALID_REQUEST")
		return
	}
	managedWebsiteError(w, 409, "WEBSITE_CHANGE_REJECTED")
}

func (s *Server) changeManagedWebsiteState(ctx context.Context, owner, request, reason string, change func(storage.Tx) error) error {
	return s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := change(tx); err != nil {
			return err
		}
		return s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: owner, Action: actionManagedWebsiteConfigure, Result: "allowed", RequestID: request, Source: audit.SourceWeb, ReasonCode: reason, At: time.Now().UTC()})
	})
}

func (s *Server) validManagedTargets(ctx context.Context, owner string, ids []string) bool {
	if s.Links == nil {
		return false
	}
	inventory, err := s.Nodes.ListVisible(ctx, owner)
	if err != nil {
		return false
	}
	available := map[string]bool{}
	for _, n := range inventory {
		available[n.ID] = n.Status == nodes.StatusOnline
	}
	for _, id := range ids {
		if !available[id] || !s.Links.HasCurrentCapabilities(ctx, id, transport.WebsiteCertificateInstallAction) {
			return false
		}
		delete(available, id)
	}
	return s.authorizeManagedTargets(ctx, owner, ids)
}

func (s *Server) authorizeManagedTargets(ctx context.Context, owner string, ids []string) bool {
	return s.Identity.Authorize(ctx, auth.Check{SubjectID: owner, Action: transport.WebsiteCertificateInstallAction, NodeIDs: ids}) == nil
}

func (m *publicCertificateExecution) findManagedZone(ctx context.Context, subject, domain string) (string, error) {
	zones, err := m.s.publicCertificateZones(ctx, subject)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(domain, "*.")
	zoneID, longest := "", 0
	for _, z := range zones {
		if (name == z.Name || strings.HasSuffix(name, "."+z.Name)) && (len(z.Name) > longest || len(z.Name) == longest && z.ID < zoneID) {
			zoneID, longest = z.ID, len(z.Name)
		}
	}
	if zoneID == "" {
		return "", certificates.ErrACMEConfig
	}
	return zoneID, nil
}

func intBool(value bool) int {
	if value {
		return 1
	}
	return 0
}

func releaseManagedTask(w http.ResponseWriter, t tasks.Task) {
	releaseSourceJSON(w, 202, map[string]any{"taskId": t.ID, "status": t.Status, "approvalRequired": t.ApprovalRequired})
}
