package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func (m *publicCertificateExecution) submitManagedDelivery(ctx context.Context, site managedWebsite) (tasks.Task, error) {
	order, err := certificates.ReadPublicOrder(ctx, m.s.DB, site.OrderID)
	if err != nil || order.Status != "issued" || order.Domain != site.Domain || !order.NotAfter.After(time.Now()) {
		return tasks.Task{}, certificates.ErrACMEConfig
	}
	var targets []string
	if json.Unmarshal([]byte(site.Targets), &targets) != nil || len(targets) == 0 {
		return tasks.Task{}, tasks.ErrInvalidTask
	}
	p := transport.WebsiteCertificatePlan{Domain: site.Domain, OrderID: order.TaskID, ChainSHA256: order.ChainSHA256, Generation: site.Generation}
	source := audit.Source(site.DeliverySource)
	return m.s.Tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID: site.Owner, RequestID: site.DeliveryKey, Name: "下发网站证书 " + websiteTaskDomain(site.Domain),
		ActionID: transport.WebsiteCertificateInstallAction, OperationClass: tasks.OperationClassModifying,
		Source: source, Params: p.Params(), IdempotencyKey: site.DeliveryKey, TargetNodeIDs: targets,
		RequireApproval: source == audit.SourceWeb, ApprovalSatisfied: source == audit.SourceSystem,
	})
}

func (s *Server) managedDeliverySite(ctx context.Context, t tasks.Task) (managedWebsite, error) {
	if s.PublicCertificates == nil || t.ScopeRevision != 0 || t.PluginID != "" || t.TargetType != "node_ids" || t.OperationClass != tasks.OperationClassModifying || s.Identity.AuthorizeAdministrator(ctx, t.SubmitterID) != nil {
		return managedWebsite{}, tasks.ErrInvalidTask
	}
	p, err := transport.ParseWebsiteCertificatePlan([]byte(t.Params))
	if err != nil {
		return managedWebsite{}, err
	}
	site, err := s.PublicCertificates.managedWebsite(ctx, p.Domain)
	if err != nil || site.Owner != t.SubmitterID || site.OrderID != p.OrderID || site.Generation != p.Generation || site.DeliveryKey == "" || site.DeliveryKey != t.IdempotencyKey || site.DeliverySource != t.Source {
		return managedWebsite{}, tasks.ErrInvalidTask
	}
	// Submission checks happen before the task service generates its approval
	// record. Persisted manual tasks must always carry the formal approval.
	if t.Source == string(audit.SourceWeb) {
		if t.ID != "" && !t.ApprovalRequired {
			return managedWebsite{}, tasks.ErrInvalidTask
		}
	} else if t.Source != string(audit.SourceSystem) || !site.Redeploy {
		return managedWebsite{}, tasks.ErrInvalidTask
	}
	var targets []string
	if json.Unmarshal([]byte(site.Targets), &targets) != nil || len(targets) != len(t.Targets) {
		return managedWebsite{}, tasks.ErrInvalidTask
	}
	for _, target := range t.Targets {
		if !slices.Contains(targets, target.NodeID) {
			return managedWebsite{}, tasks.ErrInvalidTask
		}
	}
	return site, nil
}

func (s *Server) serveManagedWebsiteBundle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	denied := func() { releaseSourceJSON(w, 403, map[string]string{"code": "CERTIFICATE_DELIVERY_DENIED"}) }
	if r.TLS == nil || s.Enrollment == nil || s.PublicCertificates == nil {
		denied()
		return
	}
	body, ok := publicCertificateBody(w, r, 2048)
	if !ok {
		return
	}
	if len(body) != 4 || !deploymentID.MatchString(body["nodeId"]) || !deploymentID.MatchString(body["taskId"]) {
		denied()
		return
	}
	verifier, err := transport.NewPeerVerifier(s.Enrollment.Lookup)
	if err != nil {
		denied()
		return
	}
	peer, err := verifier.Verify(ctx, *r.TLS, body["nodeId"])
	if err != nil {
		denied()
		return
	}
	connection, err := s.Links.CurrentConnection(ctx, peer.NodeID)
	if err != nil || connection.Identity != peer {
		denied()
		return
	}
	task, err := s.Tasks.CheckExecutionForNode(ctx, body["taskId"], body["planHash"], peer.NodeID)
	if err != nil || task.ActionID != transport.WebsiteCertificateInstallAction || body["executionId"] != executionIDFor(task.ID, peer.NodeID, task.PlanHash) {
		denied()
		return
	}
	dispatched := false
	for _, target := range task.Targets {
		if target.NodeID == peer.NodeID && target.State == tasks.TargetDispatched {
			dispatched = true
			break
		}
	}
	site, err := s.managedDeliverySite(ctx, task)
	if err != nil || !dispatched {
		denied()
		return
	}
	plan, err := transport.ParseWebsiteCertificatePlan([]byte(task.Params))
	if err != nil {
		denied()
		return
	}
	order, err := certificates.ReadPublicOrder(ctx, s.DB, site.OrderID)
	if err != nil || order.Status != "issued" || order.ChainSHA256 != plan.ChainSHA256 || !order.NotAfter.After(time.Now()) {
		denied()
		return
	}
	var keyID string
	var sealed []byte
	if err := s.DB.QueryRow(ctx, `SELECT key_id,sealed_key FROM cx_managed_website_keys WHERE order_id=?`, site.OrderID).Scan(&keyID, &sealed); err != nil {
		denied()
		return
	}
	key, err := s.Credentials.OpenWebsiteKey(site.OrderID, site.Owner, keyID, sealed, order.NotAfter, time.Now())
	if err != nil {
		denied()
		return
	}
	defer clear(key)
	// This task has passed the unified task/approval gate. Persist the exact
	// approved target set for subsequent certificate generations.
	if err = s.recordManagedBundleAccess(ctx, task, site, peer.NodeID); err != nil {
		denied()
		return
	}
	releaseSourceJSON(w, 200, transport.WebsiteCertificateBundle{KeyDER: key, ChainPEM: order.ChainPEM})
}

func (s *Server) recordManagedBundleAccess(ctx context.Context, task tasks.Task, site managedWebsite, node string) error {
	return s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		result, err := tx.Exec(ctx, `UPDATE cx_managed_websites SET redeploy=1 WHERE domain=? AND delivery_key=? AND order_id=?`, site.Domain, site.DeliveryKey, site.OrderID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return tasks.ErrInvalidTask
		}
		return s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: site.Owner, Action: transport.WebsiteCertificateInstallAction, Result: "allowed", RequestID: task.RequestID, TaskID: task.ID, NodeIDs: []string{node}, Source: audit.Source(task.Source), ReasonCode: "approved_website_bundle_delivered", At: time.Now().UTC()})
	})
}
