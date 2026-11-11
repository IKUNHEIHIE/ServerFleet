package server

import (
	"context"
	"encoding/json"
	"errors"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/tasks"
	"strconv"
	"sync/atomic"
	"time"
)

type notificationTaskWorker struct {
	s         *Server
	authority string
}
type notificationBorrowKey struct{}
type notificationBorrowScope struct {
	server    *Server
	task      tasks.Task
	intent    notifications.DeliveryIntent
	operation auth.CredentialOperation
	active    atomic.Bool
}

func (s *Server) setupNotificationExecution(authority string, workers map[string]tasks.Worker) (*tasks.ControllerExecution, error) {
	if authority == "" {
		return nil, nil
	}
	s.notificationAuthority = authority
	workers[notifications.ActionSend] = &notificationTaskWorker{s: s, authority: authority}
	return &tasks.ControllerExecution{AuthorityID: authority, Actions: map[string]tasks.ControllerAction{notifications.ActionSend: {AllowSystem: true}}, Authorize: func(ctx context.Context, subject, action, operation string) error {
		if action != notifications.ActionSend || s.runtimeGate(ctx) != nil || s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
			return auth.ErrDenied
		}
		return s.Identity.AuthorizeSubjectAction(ctx, subject, notifications.ActionWrite)
	}}, nil
}
func (s *Server) prepareNotificationCredential(ctx context.Context, owner string, ch notifications.Channel, ref string) (uint64, error) {
	if s.runtimeGate(ctx) != nil || s.Identity.AuthorizeAdministrator(ctx, owner) != nil || s.Identity.AuthorizeSubjectAction(ctx, owner, notifications.ActionWrite) != nil {
		return 0, auth.ErrDenied
	}
	meta, e := s.Credentials.ServiceMetadata(ctx, owner, ref)
	expected := "smtp_password"
	if ch == notifications.ChannelQQBot {
		expected = "qqbot_secret"
	}
	if e != nil || meta.Kind != expected {
		return 0, auth.ErrDenied
	}
	return meta.Revision, nil
}
func (s *Server) notificationCurrent(ctx context.Context, in notifications.DeliveryIntent) error {
	if s.Notifications == nil || s.notificationAuthority == "" || s.runtimeGate(ctx) != nil {
		return auth.ErrDenied
	}
	record, e := s.Notifications.GetChannel(ctx, in.Channel.Channel)
	if e != nil || !record.Enabled || record.CredentialStatus != notifications.CredentialConfigured || notifications.ChannelConfigurationSHA256(record) != in.Binding.ConfigurationSHA256 {
		return auth.ErrDenied
	}
	binding, e := s.Notifications.ReadBinding(ctx, in.Channel.Channel)
	if e != nil || binding != in.Binding {
		return auth.ErrDenied
	}
	revision, e := s.prepareNotificationCredential(ctx, binding.OwnerID, in.Channel.Channel, record.SecretRef)
	if e != nil || revision != binding.CredentialRevision {
		return auth.ErrDenied
	}
	switch in.SourceReference.Kind {
	case "":
	case "monitor":
		if s.Monitoring == nil {
			return auth.ErrDenied
		}
		if e = s.Monitoring.AutomationCurrent(ctx, in.SourceReference.ID, in.SourceReference.OwnerID, in.SourceReference.Revision); e != nil {
			return e
		}
	case "quota":
		if s.Quota == nil {
			return auth.ErrDenied
		}
		if e = s.Quota.AutomationCurrent(ctx, in.SourceReference.ID, in.SourceReference.OwnerID, in.SourceReference.Revision); e != nil {
			return e
		}
	default:
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) dispatchNotification(ctx context.Context, in notifications.DeliveryIntent) (string, error) {
	if s.notificationCurrent(ctx, in) != nil || s.Tasks == nil {
		return "", auth.ErrDenied
	}
	source := audit.Source(in.Source)
	if source != audit.SourceWeb && source != audit.SourceSystem {
		return "", auth.ErrDenied
	}
	params := map[string]string{"deliveryId": strconv.FormatInt(in.DeliveryID, 10), "attempt": strconv.Itoa(in.Attempt), "intentSha256": notifications.IntentSHA256(in)}
	key := "notification-" + params["deliveryId"] + "-" + params["attempt"]
	t, e := s.Tasks.SubmitController(ctx, tasks.SubmitRequest{SubjectID: in.Binding.OwnerID, RequestID: key, Name: "Deliver configured notification", ActionID: notifications.ActionSend, OperationClass: tasks.OperationClassStandard, Source: source, IdempotencyKey: key, Params: params})
	return t.ID, e
}
func (s *Server) notificationTaskIntent(ctx context.Context, t tasks.Task) (notifications.DeliveryIntent, error) {
	var p map[string]string
	if t.ActionID != notifications.ActionSend || t.TargetType != tasks.TargetTypeController || t.TargetResourceID != s.notificationAuthority || t.OperationClass != tasks.OperationClassStandard || t.PluginID != "" || t.ScopeRevision != 0 || json.Unmarshal([]byte(t.Params), &p) != nil || len(p) != 3 {
		return notifications.DeliveryIntent{}, auth.ErrDenied
	}
	id, e := strconv.ParseInt(p["deliveryId"], 10, 64)
	if e != nil || id <= 0 {
		return notifications.DeliveryIntent{}, auth.ErrDenied
	}
	a, e := strconv.Atoi(p["attempt"])
	if e != nil || a < 1 || a > 6 {
		return notifications.DeliveryIntent{}, auth.ErrDenied
	}
	in, e := s.Notifications.ReadIntent(ctx, id, a)
	if e != nil || in.TaskID != "" && in.TaskID != t.ID || in.Binding.OwnerID != t.SubmitterID || in.Source != t.Source || notifications.IntentSHA256(in) != p["intentSha256"] || t.IdempotencyKey != "notification-"+p["deliveryId"]+"-"+p["attempt"] || s.notificationCurrent(ctx, in) != nil {
		return in, auth.ErrDenied
	}
	return in, nil
}
func (w *notificationTaskWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	results := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{w.authority: {Code: code, Message: message}}, nil
	}
	current, e := w.s.Tasks.CheckExecution(ctx, t.ID, t.PlanHash)
	if e != nil {
		return results(tasks.NodeResultFailed, "notification_execution_denied")
	}
	in, e := w.s.notificationTaskIntent(ctx, current)
	if e != nil {
		return results(tasks.NodeResultFailed, "notification_intent_changed")
	}
	if e = w.s.Notifications.BindTask(ctx, in.DeliveryID, in.Attempt, t.ID); e != nil {
		return results(tasks.NodeResultFailed, "notification_binding_conflict")
	}
	// This context is process-owned and never accepted from HTTP/AI/plugin input.
	scope := &notificationBorrowScope{server: w.s, task: current, intent: in}
	scope.active.Store(true)
	defer scope.active.Store(false)
	d, e := w.s.Notifications.ExecuteIntent(context.WithValue(ctx, notificationBorrowKey{}, scope), in.DeliveryID, in.Attempt, t.ID)
	if d.Status == notifications.DeliveryProviderAccepted || d.Status == notifications.DeliverySubmitted && d.ProviderCode != "attempt_pending" {
		if e == nil {
			return results(tasks.NodeResultSucceeded, "notification_provider_accepted_not_read_receipt")
		}
	}
	if d.Status == notifications.DeliveryUnknown || d.Status == notifications.DeliverySubmitted || errors.Is(e, auth.ErrCredentialUseUnknown) || errors.Is(e, notifications.ErrDeliveryUnknown) {
		return results(tasks.NodeResultUnknown, "notification_delivery_unknown_no_replay")
	}
	return results(tasks.NodeResultFailed, "notification_not_accepted")
}
func (s *Server) notificationCredentialGate(ctx context.Context, op auth.CredentialOperation) error {
	scope, ok := ctx.Value(notificationBorrowKey{}).(*notificationBorrowScope)
	if !ok || scope == nil || scope.server != s || !scope.active.Load() || scope.operation != op || s.notificationCurrent(ctx, scope.intent) != nil {
		return auth.ErrDenied
	}
	t, e := s.Tasks.CheckExecution(ctx, scope.task.ID, scope.task.PlanHash)
	if e != nil {
		return e
	}
	_, e = s.notificationTaskIntent(ctx, t)
	return e
}
func (s *Server) borrowNotification(ctx context.Context, use notifications.SecretUse, consume func(context.Context, string) error) error {
	scope, ok := ctx.Value(notificationBorrowKey{}).(*notificationBorrowScope)
	if !ok || scope == nil || scope.server != s || !scope.active.Load() || consume == nil || use.Ref != scope.intent.Channel.SecretRef || use.SubjectID != scope.intent.Binding.OwnerID {
		return auth.ErrDenied
	}
	expected := "smtp"
	if scope.intent.Channel.Channel == notifications.ChannelQQBot {
		expected = "qqbot"
	}
	if use.Module != expected {
		return auth.ErrDenied
	}
	op := auth.CredentialOperation{SubjectID: scope.task.SubmitterID, CredentialID: use.Ref, Module: expected, Source: scope.task.Source, OperationID: "notification-use-" + scope.task.ID, PlanDigest: scope.task.PlanHash, Action: "use", ExpectedRevision: scope.intent.Binding.CredentialRevision}
	scope.operation = op
	var providerErr error
	err := s.Credentials.UseForService(ctx, op, func(c context.Context, raw []byte) error { providerErr = consume(c, string(raw)); return nil })
	if err != nil {
		return notifications.ErrDeliveryUnknown
	}
	return providerErr
}
func (s *Server) runNotificationDispatch(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.runtimeGate(ctx) != nil || s.Notifications == nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 4*time.Second)
		pending, e := s.Notifications.PendingIntents(attempt, 20)
		if e == nil {
			for _, in := range pending {
				_, _ = s.Notifications.DispatchIntent(attempt, in.DeliveryID, in.Attempt)
			}
		}
		// Reconcile only metadata from terminal unified tasks. This does no I/O and
		// cannot queue another send for UNKNOWN, interrupted or expired work.
		records, e := s.Notifications.RecentTaskIntents(attempt, 100)
		if e == nil {
			for _, in := range records {
				t, err := s.Tasks.Get(attempt, in.TaskID)
				if err == nil && t.Status.Terminal() {
					_ = s.Notifications.ReconcileTask(attempt, in.DeliveryID, in.Attempt, string(t.Status))
				}
			}
		}
		cancel()
	}
}

func (s *Server) notificationIOCurrent(ctx context.Context) error {
	scope, ok := ctx.Value(notificationBorrowKey{}).(*notificationBorrowScope)
	if !ok || scope == nil {
		return auth.ErrDenied
	}
	return s.notificationCredentialGate(ctx, scope.operation)
}
