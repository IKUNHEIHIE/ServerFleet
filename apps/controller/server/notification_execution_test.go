package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type alertTestProber struct{ healthy atomic.Bool }

func (p *alertTestProber) HTTP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return monitoring.ProbeResult{Healthy: p.healthy.Load(), Reason: "synthetic local fixture"}, nil
}
func (p *alertTestProber) TCP(c context.Context, s monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return p.HTTP(c, s)
}
func (p *alertTestProber) TLS(c context.Context, s monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return p.HTTP(c, s)
}

type alertTestSender struct {
	s       *Server
	calls   atomic.Int32
	unknown atomic.Bool
}

func (p *alertTestSender) Send(ctx context.Context, _ notifications.SMTPSettings, ref string, _ notifications.Message) (notifications.ProviderResult, error) {
	scope, ok := ctx.Value(notificationBorrowKey{}).(*notificationBorrowScope)
	if !ok {
		return notifications.ProviderResult{}, auth.ErrDenied
	}
	e := p.s.borrowNotification(ctx, notifications.SecretUse{Ref: ref, Module: "smtp", SubjectID: scope.intent.Binding.OwnerID}, func(c context.Context, raw string) error {
		if raw != "synthetic-r32-smtp-secret" {
			return fmt.Errorf("wrong credential")
		}
		return p.s.notificationIOCurrent(c)
	})
	if e != nil {
		return notifications.ProviderResult{}, e
	}
	p.calls.Add(1)
	if p.unknown.Load() {
		return notifications.ProviderResult{Outcome: notifications.ProviderUnknown, Code: "synthetic_unknown"}, notifications.ErrDeliveryUnknown
	}
	return notifications.ProviderResult{Outcome: notifications.ProviderAccepted, Code: "synthetic_accepted"}, nil
}
func TestControllerMonitorToDurableTaskAndCredentialLoan(t *testing.T) {
	ctx := context.Background()
	sender := &alertTestSender{}
	probe := &alertTestProber{}
	probe.healthy.Store(true)
	s, e := New(ctx, Config{DBPath: filepath.Join(t.TempDir(), "fleet.db"), Origin: "https://controller.example.invalid", StagingDir: filepath.Join(t.TempDir(), "stage"), ControllerAuthorityID: "controller-r32", SMTPSender: sender, MonitoringProber: probe})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	sender.s = s
	s.runCancel()
	<-s.runDone
	setup, e := s.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Identity.Bootstrap(ctx, setup, "admin", "synthetic-r32-password"); e != nil {
		t.Fatal(e)
	}
	session, e := s.Identity.Login(ctx, "admin", "synthetic-r32-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	owner := session.Identity.SubjectID
	credential, e := s.Credentials.Create(ctx, session.Secret, session.CSRF, auth.CredentialInput{Name: "Synthetic SMTP", Kind: "smtp_password", Secret: "synthetic-r32-smtp-secret", UsageScope: []string{"smtp"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Notifications.UpsertChannel(ctx, owner, "configure-smtp", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, SMTP: &notifications.SMTPSettings{Host: "smtp.example.invalid", Port: 465, Security: notifications.SMTPSecurityTLS, Sender: "alerts@example.invalid", Recipient: "ops@example.invalid"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Notifications.SetSecretRef(ctx, owner, "reject-raw-secret", notifications.ChannelSMTP, "raw-password-is-not-a-reference"); e == nil {
		t.Fatal("raw secret accepted as credential reference")
	}
	if _, e = s.Notifications.SetSecretRef(ctx, owner, "bind-smtp", notifications.ChannelSMTP, credential.ID); e != nil {
		t.Fatal(e)
	}
	channel, e := s.Notifications.GetChannel(ctx, notifications.ChannelSMTP)
	if e != nil || !channel.BackgroundReady || channel.CredentialRevision != credential.Revision {
		t.Fatal("channel readiness", channel, e)
	}
	m, e := s.Monitoring.Create(ctx, owner, "create-monitor", monitoring.MonitorInput{ID: "m-r32", Name: "Synthetic service", Kind: monitoring.KindHTTP, Target: "http://93.184.216.34/", IntervalSeconds: 30, TimeoutSeconds: 1, Enabled: true, FailureThreshold: 1, RecoveryThreshold: 1, NotifyChannels: []notifications.Channel{notifications.ChannelSMTP}})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Monitoring.RunCheckNow(ctx, owner, "first-check", m.ID); e != nil {
		t.Fatal(e)
	}
	probe.healthy.Store(false)
	if _, _, e = s.Monitoring.RunCheckNow(ctx, owner, "down-check", m.ID); e != nil {
		t.Fatal(e)
	}
	events, e := s.Monitoring.AlertEvents(ctx, owner, m.ID, 20)
	if e != nil || len(events) != 1 || events[0].State != "queued" || events[0].TaskID == "" || sender.calls.Load() != 0 {
		t.Fatal("queued outbox", events, e)
	}
	waitTask := func(id string, want tasks.TaskStatus) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if e := s.Tasks.RunOnce(ctx); e != nil {
				t.Fatal(e)
			}
			task, e := s.Tasks.Get(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			if task.Status.Terminal() {
				if task.Status != want {
					t.Fatal("terminal task", task.Status, want, task)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("task did not finish")
	}
	waitTask(events[0].TaskID, tasks.StatusCompleted)
	events, e = s.Monitoring.AlertEvents(ctx, owner, m.ID, 20)
	if e != nil || events[0].State != "provider_accepted" || sender.calls.Load() != 1 {
		t.Fatal("accepted result not linked", events, e)
	}
	task, _ := s.Tasks.Get(ctx, events[0].TaskID)
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), "synthetic-r32-smtp-secret") || strings.Contains(string(raw), "Synthetic service") {
		t.Fatal("private notification intent leaked into task")
	}
	// A second transition with UNKNOWN remains durable and cannot be retried.
	sender.unknown.Store(true)
	probe.healthy.Store(true)
	if _, _, e = s.Monitoring.RunCheckNow(ctx, owner, "recovery-unknown", m.ID); e != nil {
		t.Fatal(e)
	}
	events, e = s.Monitoring.AlertEvents(ctx, owner, m.ID, 20)
	if e != nil || len(events) != 2 {
		t.Fatal(events, e)
	}
	waitTask(events[0].TaskID, tasks.StatusUnknown)
	if e = s.Monitoring.FlushAlerts(ctx, 20); e != nil {
		t.Fatal(e)
	}
	events, _ = s.Monitoring.AlertEvents(ctx, owner, m.ID, 20)
	if events[0].State != "delivery_unknown" || sender.calls.Load() != 2 {
		t.Fatal("unknown lost", events)
	}
	if _, e = s.Notifications.RetryDelivery(ctx, owner, "no-unknown-retry", events[0].DeliveryID); e == nil {
		t.Fatal("unknown retry authorized")
	}
	// Editing the source monitor revokes queued old-source work before any loan.
	probe.healthy.Store(false)
	if _, _, e = s.Monitoring.RunCheckNow(ctx, owner, "queued-before-source-change", m.ID); e != nil {
		t.Fatal(e)
	}
	events, _ = s.Monitoring.AlertEvents(ctx, owner, m.ID, 20)
	stale := events[0]
	if _, e = s.Monitoring.Update(ctx, owner, "change-source-config", m.ID, monitoring.MonitorInput{ID: m.ID, Name: "Changed source", Kind: m.Kind, Target: m.Target, IntervalSeconds: 30, TimeoutSeconds: 1, Enabled: true, NotifyChannels: []notifications.Channel{notifications.ChannelSMTP}}); e != nil {
		t.Fatal(e)
	}
	waitTask(stale.TaskID, tasks.StatusFailed)
	if sender.calls.Load() != 2 {
		t.Fatal("stale source reached provider")
	}
	if e = s.Notifications.ReconcileTask(ctx, stale.DeliveryID, 1, "failed"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Notifications.RetryDelivery(ctx, owner, "stale-source-retry", stale.DeliveryID); e == nil {
		t.Fatal("stale source retry accepted")
	}
	if e = s.borrowNotification(context.Background(), notifications.SecretUse{Ref: credential.ID, Module: "smtp", SubjectID: owner}, func(context.Context, string) error { return nil }); e == nil {
		t.Fatal("credential loan escaped trusted worker")
	}
}
