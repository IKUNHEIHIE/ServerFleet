package server

import (
	"context"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/quota"
	"time"
)

type durableAlertAdapter struct{ service *notifications.Service }

func (a durableAlertAdapter) SendAlert(ctx context.Context, alert notifications.Alert) (notifications.Delivery, error) {
	return a.service.SendDurableAlert(ctx, alert)
}
func (s *Server) monitorAutomationCurrent(ctx context.Context, m monitoring.Monitor) error {
	if s.runtimeGate(ctx) != nil || m.AutomationOwnerID == "" || m.AutomationRevision == 0 {
		return auth.ErrDenied
	}
	return s.Identity.AuthorizeSubjectAction(ctx, m.AutomationOwnerID, monitoring.ActionWrite)
}
func (s *Server) quotaAutomationCurrent(ctx context.Context, p quota.Policy) error {
	if s.runtimeGate(ctx) != nil || p.AutomationOwnerID == "" || p.AutomationRevision == 0 {
		return auth.ErrDenied
	}
	return s.Identity.Authorize(ctx, auth.Check{SubjectID: p.AutomationOwnerID, Action: quota.ActionWrite, NodeIDs: []string{p.NodeID}})
}
func (s *Server) runMonitorAutomation(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.runtimeGate(ctx) != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 25*time.Second)
		_, _ = s.Monitoring.RunDue(attempt, time.Now().UTC())
		cancel()
	}
}
func (s *Server) runQuotaAutomation(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.runtimeGate(ctx) != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, _ = s.Quota.RunReminders(attempt, time.Now().UTC())
		cancel()
	}
}
