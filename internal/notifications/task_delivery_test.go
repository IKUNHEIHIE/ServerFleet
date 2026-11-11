package notifications_test

import (
	"context"
	"errors"
	"fmt"
	"serverfleet.local/serverfleet/internal/notifications"
	"testing"
	"time"
)

func asyncService(t *testing.T, h fixture, current notifications.DeliveryAuthority, dispatch notifications.DeliveryDispatcher) *notifications.Service {
	t.Helper()
	if dispatch == nil {
		dispatch = func(_ context.Context, in notifications.DeliveryIntent) (string, error) {
			return fmt.Sprintf("task-%d-%d", in.DeliveryID, in.Attempt), nil
		}
	}
	s, e := notifications.NewService(notifications.Options{DB: h.db, Repo: h.repo, Audit: h.audit, Authorizer: authorizerFunc(allowAll), SMTPSender: h.smtp, QQSender: h.qq, Now: h.clock.Now, Dispatcher: dispatch, CurrentDelivery: current, PrepareCredential: func(context.Context, string, notifications.Channel, string) (uint64, error) { return 7, nil }})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestDurableQueuedUnknownSurvivesRestartAndNeverResends(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil)
	current := func(context.Context, notifications.DeliveryIntent) error { return nil }
	h.service = asyncService(t, h, current, nil)
	h.configureSMTP(t)
	a := notifications.Alert{DedupeKey: "monitor:episode:1", Subject: "Down", Body: "Synthetic down"}
	d, e := h.service.SendDurableAlert(ctx, a)
	if e != nil || d.Status != notifications.DeliveryQueued || d.TaskID == "" || h.smtp.callCount() != 0 {
		t.Fatal("not a queued intent", d, e)
	}
	h.smtp.result = notifications.ProviderResult{Outcome: notifications.ProviderUnknown, Code: "synthetic_unknown"}
	h.smtp.err = notifications.ErrDeliveryUnknown
	d, e = h.service.ExecuteIntent(ctx, d.ID, 1, d.TaskID)
	if !errors.Is(e, notifications.ErrDeliveryUnknown) || d.Status != notifications.DeliveryUnknown || d.Attempts != 1 || h.smtp.callCount() != 1 {
		t.Fatal("unknown fence", d, e)
	}
	h.clock.Advance(48 * time.Hour)
	h.service = asyncService(t, h, current, nil)
	same, e := h.service.SendDurableAlert(ctx, a)
	if e != nil || same.ID != d.ID || same.Status != notifications.DeliveryUnknown {
		t.Fatal("restart or dedup window lost claim", same, e)
	}
	if _, e = h.service.ExecuteIntent(ctx, d.ID, 1, d.TaskID); !errors.Is(e, notifications.ErrNotRetryable) {
		t.Fatal("attempt replay accepted", e)
	}
	if _, e = h.service.RetryDelivery(ctx, "admin", "retry-unknown", d.ID); !errors.Is(e, notifications.ErrNotRetryable) {
		t.Fatal("UNKNOWN retry accepted", e)
	}
	if h.smtp.callCount() != 1 {
		t.Fatal("unknown sent again")
	}
}
func TestTaskCancellationBeforeFenceIsKnownNotSentAndRetryPreservesSource(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil)
	current := func(context.Context, notifications.DeliveryIntent) error { return nil }
	h.service = asyncService(t, h, current, nil)
	h.configureSMTP(t)
	a := notifications.Alert{DedupeKey: "quota:n1:threshold", Subject: "Quota", Body: "Original frozen message", Source: notifications.AlertSource{Kind: "quota", ID: "n1", OwnerID: "policy-owner", Revision: 2}}
	d, e := h.service.SendDurableAlert(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.service.ReconcileTask(ctx, d.ID, 1, "canceled"); e != nil {
		t.Fatal(e)
	}
	d, e = h.service.Delivery(ctx, d.ID)
	if e != nil || d.Status != notifications.DeliveryFailed || d.Attempts != 1 || h.smtp.callCount() != 0 {
		t.Fatal(d, e)
	}
	h.clock.Advance(time.Minute)
	d, e = h.service.RetryDelivery(ctx, "admin", "known-failure-retry", d.ID)
	if e != nil || d.Status != notifications.DeliveryQueued {
		t.Fatal("new attempt", d, e)
	}
	in, e := h.service.ReadIntent(ctx, d.ID, 2)
	if e != nil || in.SourceReference != a.Source || in.Message.Body != a.Body {
		t.Fatal("retry lost source or frozen body", in, e)
	}
	d, e = h.service.ExecuteIntent(ctx, d.ID, 2, in.TaskID)
	if e != nil || d.Status != notifications.DeliveryProviderAccepted || h.smtp.callCount() != 1 {
		t.Fatal("retry failed", d, e)
	}
	if _, e = h.service.ExecuteIntent(ctx, d.ID, 2, in.TaskID); e == nil {
		t.Fatal("accepted delivery replayed")
	}
}
func TestDurableIntentRecoversDispatchGapAndBlocksRevokedAuthority(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil)
	revoked := false
	current := func(context.Context, notifications.DeliveryIntent) error {
		if revoked {
			return notifications.ErrSecretUnavailable
		}
		return nil
	}
	h.service = asyncService(t, h, current, func(context.Context, notifications.DeliveryIntent) (string, error) {
		return "", errors.New("scheduler temporarily unavailable")
	})
	h.configureSMTP(t)
	d, e := h.service.SendDurableAlert(ctx, notifications.Alert{DedupeKey: "durable-dispatch-gap", Subject: "Test", Body: "Test"})
	if e == nil {
		t.Fatal("dispatch failure hidden")
	}
	pending, e := h.service.PendingIntents(ctx, 20)
	if e != nil || len(pending) != 1 {
		t.Fatal("durable intent lost", pending, e)
	}
	h.service = asyncService(t, h, current, nil)
	d, e = h.service.DispatchIntent(ctx, pending[0].DeliveryID, 1)
	if e != nil || d.TaskID == "" {
		t.Fatal("dispatch recovery", d, e)
	}
	revoked = true
	if _, e = h.service.ExecuteIntent(ctx, d.ID, 1, d.TaskID); e == nil {
		t.Fatal("revoked intent sent")
	}
	if h.smtp.callCount() != 0 {
		t.Fatal("provider called after revocation")
	}
}

func TestTaskQQFallbackIsSeparateOnceAndUnknownNeverFallsBack(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil)
	current := func(context.Context, notifications.DeliveryIntent) error { return nil }
	h.service = asyncService(t, h, current, nil)
	h.configureSMTP(t)
	h.configureQQ(t)
	h.updateSettings(t, notifications.Settings{SMTPFallbackOnQQFailure: true, MaxRetryAttempts: 2, DeduplicationWindowSeconds: 60})
	h.qq.result = notifications.ProviderResult{Outcome: notifications.ProviderRejected, Code: "synthetic_rejection"}
	a := notifications.Alert{DedupeKey: "auto-primary-fallback", Subject: "Test", Body: "Frozen fallback message"}
	primary, e := h.service.SendDurableAlert(ctx, a)
	if e != nil || primary.Channel != notifications.ChannelQQBot {
		t.Fatal(primary, e)
	}
	_, _ = h.service.ExecuteIntent(ctx, primary.ID, 1, primary.TaskID)
	child, e := h.service.EffectiveDelivery(ctx, primary.ID)
	if e != nil || child.ID == primary.ID || child.Channel != notifications.ChannelSMTP || child.TaskID == primary.TaskID || child.Status != notifications.DeliveryQueued {
		t.Fatal("fallback not separate", child, e)
	}
	in, e := h.service.ReadIntent(ctx, primary.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	same, e := h.service.QueueFallback(ctx, in, primary)
	if e != nil || same.ID != child.ID {
		t.Fatal("duplicate fallback", same, e)
	}
	if _, e = h.service.ExecuteIntent(ctx, child.ID, 1, child.TaskID); e != nil {
		t.Fatal(e)
	}
	a.DedupeKey = "auto-unknown-no-fallback"
	h.qq.result = notifications.ProviderResult{Outcome: notifications.ProviderUnknown, Code: "synthetic_unknown"}
	h.qq.err = notifications.ErrDeliveryUnknown
	unknown, e := h.service.SendDurableAlert(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = h.service.ExecuteIntent(ctx, unknown.ID, 1, unknown.TaskID)
	effective, e := h.service.EffectiveDelivery(ctx, unknown.ID)
	if e != nil || effective.ID != unknown.ID || effective.Status != notifications.DeliveryUnknown || h.smtp.callCount() != 1 {
		t.Fatal("unknown triggered fallback", effective, e)
	}
}

func TestRevokedUnboundIntentRetiresInsteadOfReactivatingOrStarving(t *testing.T) {
	ctx := context.Background()
	h := newFixture(t, nil)
	revoked := false
	current := func(context.Context, notifications.DeliveryIntent) error {
		if revoked {
			return notifications.ErrSecretUnavailable
		}
		return nil
	}
	h.service = asyncService(t, h, current, func(context.Context, notifications.DeliveryIntent) (string, error) {
		return "", errors.New("synthetic dispatch gap")
	})
	h.configureSMTP(t)
	_, _ = h.service.SendDurableAlert(ctx, notifications.Alert{DedupeKey: "old-unbound", Subject: "Old", Body: "Old"})
	pending, e := h.service.PendingIntents(ctx, 20)
	if e != nil || len(pending) != 1 {
		t.Fatal(pending, e)
	}
	revoked = true
	h.service = asyncService(t, h, current, nil)
	d, e := h.service.DispatchIntent(ctx, pending[0].DeliveryID, 1)
	if e == nil || d.Status != notifications.DeliveryFailed || d.Attempts != 1 {
		t.Fatal("revoked intent not retired", d, e)
	}
	pending, e = h.service.PendingIntents(ctx, 20)
	if e != nil || len(pending) != 0 {
		t.Fatal("retired intent starves pending queue", pending, e)
	}
	revoked = false
	if _, e = h.service.DispatchIntent(ctx, d.ID, 1); !errors.Is(e, notifications.ErrNotRetryable) {
		t.Fatal("retired intent reactivated", e)
	}
	if h.smtp.callCount() != 0 {
		t.Fatal("retired intent sent")
	}
}
