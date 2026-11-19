package server

import (
	"context"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
)

func (s *Server) runManagedWebsiteSchedule(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			cycle, cancel := context.WithTimeout(ctx, 10*time.Second)
			s.PublicCertificates.scheduleManagedWebsites(cycle, now)
			cancel()
		}
	}
}

func (m *publicCertificateExecution) scheduleManagedWebsites(ctx context.Context, now time.Time) {
	if m.s.runtimeGate(ctx) != nil {
		return
	}
	m.managedMu.Lock()
	defer m.managedMu.Unlock()
	sites, err := m.managedWebsites(ctx, "")
	if err != nil {
		return
	}
	for _, site := range sites {
		if ctx.Err() != nil {
			return
		}
		if m.s.Identity.AuthorizeAdministrator(ctx, site.Owner) != nil {
			continue
		}
		m.scheduleManagedIssue(ctx, now, site)
		m.scheduleManagedDelivery(ctx, site)
	}
}

func (m *publicCertificateExecution) scheduleManagedIssue(ctx context.Context, now time.Time, site managedWebsite) {
	if site.Profile != m.profileSHA {
		return
	}
	t, exists, err := m.managedTask(ctx, site.Owner, site.IssueKey)
	if err != nil {
		return
	}
	if !exists {
		if site.IssueSource == string(audit.SourceWeb) || site.AutoRenew {
			_, _ = m.submitManagedIssue(ctx, site)
		}
		return
	}
	// A failed or uncertain order leaves a single attempt for this generation.
	// An operator can explicitly request another from the certificate page.
	if !site.AutoRenew || site.OrderID == "" || t.ID != site.OrderID || t.Status != tasks.StatusCompleted || site.RenewAtMS > now.UnixMilli() {
		return
	}
	next := "website-renew-" + backupTaskKey(site.Domain, site.OrderID)
	result, err := m.s.DB.Exec(ctx, `UPDATE cx_managed_websites SET issue_key=?,issue_source='system' WHERE domain=? AND issue_key=? AND order_id=? AND auto_renew=1`, next, site.Domain, site.IssueKey, site.OrderID)
	if err != nil {
		return
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return
	}
	site.IssueKey, site.IssueSource = next, string(audit.SourceSystem)
	_, _ = m.submitManagedIssue(ctx, site)
}

func (m *publicCertificateExecution) scheduleManagedDelivery(ctx context.Context, site managedWebsite) {
	if site.OrderID == "" || site.Targets == "[]" {
		return
	}
	if site.DeliveryKey == "" {
		if !site.Redeploy {
			return
		}
		next := "website-redeploy-" + backupTaskKey(site.Domain, site.OrderID)
		result, err := m.s.DB.Exec(ctx, `UPDATE cx_managed_websites SET delivery_key=?,delivery_source='system' WHERE domain=? AND order_id=? AND delivery_key='' AND redeploy=1`, next, site.Domain, site.OrderID)
		if err != nil {
			return
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return
		}
		site.DeliveryKey, site.DeliverySource = next, string(audit.SourceSystem)
	}
	_, exists, err := m.managedTask(ctx, site.Owner, site.DeliveryKey)
	if err == nil && !exists {
		_, _ = m.submitManagedDelivery(ctx, site)
	}
}
