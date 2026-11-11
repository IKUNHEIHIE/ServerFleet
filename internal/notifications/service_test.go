package notifications_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

func allowAll(context.Context, auth.Check) error { return nil }

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeSender struct {
	mu      sync.Mutex
	result  notifications.ProviderResult
	err     error
	calls   int
	refs    []string
	subject []string
	bodies  []string
}

func (f *fakeSender) Send(_ context.Context, _ notifications.SMTPSettings, secretRef string, message notifications.Message) (notifications.ProviderResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.refs = append(f.refs, secretRef)
	f.subject = append(f.subject, message.Subject)
	f.bodies = append(f.bodies, message.Body)
	return f.result, f.err
}

type fakeQQSender struct {
	mu      sync.Mutex
	result  notifications.ProviderResult
	err     error
	calls   int
	refs    []string
	bodies  []string
	subject []string
}

func (f *fakeQQSender) Send(_ context.Context, _ notifications.QQSettings, secretRef string, message notifications.Message) (notifications.ProviderResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.refs = append(f.refs, secretRef)
	f.subject = append(f.subject, message.Subject)
	f.bodies = append(f.bodies, message.Body)
	return f.result, f.err
}

func (f *fakeSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeQQSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fixture struct {
	db      *storage.DB
	repo    *notifications.Repo
	audit   *audit.SQLRecorder
	service *notifications.Service
	clock   *testClock
	smtp    *fakeSender
	qq      *fakeQQSender
}

func newFixture(t *testing.T, authorizer auth.Authorizer) fixture {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	db := testkit.NewDB(t, notifications.Migrations(), audit.Migrations())
	repo, err := notifications.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(notifications.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(allowAll)
	}
	smtp := &fakeSender{result: notifications.ProviderResult{Outcome: notifications.ProviderAccepted, Code: "smtp-ok"}}
	qq := &fakeQQSender{result: notifications.ProviderResult{Outcome: notifications.ProviderAccepted, Code: "qq-ok"}}
	service, err := notifications.NewService(notifications.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		SMTPSender: smtp, QQSender: qq, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{db: db, repo: repo, audit: recorder, service: service, clock: clock, smtp: smtp, qq: qq}
}

func (f fixture) configureSMTP(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.service.UpsertChannel(ctx, "admin", "req-smtp", notifications.ChannelSMTP, notifications.ChannelInput{
		Enabled: true,
		SMTP: &notifications.SMTPSettings{
			Host: "smtp.example.com", Port: 587, Security: notifications.SMTPSecurityStartTLS,
			Sender: "alerts@example.com", Recipient: "ops@example.com",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SetSecretRef(ctx, "admin", "req-smtp-secret", notifications.ChannelSMTP, "vault://notifications/smtp"); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) configureQQ(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.service.UpsertChannel(ctx, "admin", "req-qq", notifications.ChannelQQBot, notifications.ChannelInput{
		Enabled: true,
		QQ: &notifications.QQSettings{
			AppID: "102000000", TargetType: notifications.QQTargetGroup, TargetID: "group-1",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SetSecretRef(ctx, "admin", "req-qq-secret", notifications.ChannelQQBot, "vault://notifications/qqbot"); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) updateSettings(t *testing.T, settings notifications.Settings) {
	t.Helper()
	if _, err := f.service.UpdateSettings(context.Background(), "admin", "req-settings", settings); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationSecretValueNeverStoredOrReturned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureSMTP(t)
	const secretValue = "hunter2-super-secret"
	body := "disk is full password=" + secretValue + " url https://user:pass@example.com/path"
	if _, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "secret-scan", Subject: "Alert", Body: body, Channel: notifications.ChannelSMTP,
	}); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	channel, err := h.service.GetChannel(ctx, notifications.ChannelSMTP)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(channel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretValue) || strings.Contains(string(encoded), "user:pass") {
		t.Fatalf("channel JSON leaked sensitive text: %s", encoded)
	}
	if !strings.Contains(string(encoded), "vault://notifications/smtp") {
		t.Fatalf("opaque secretRef missing from channel view: %s", encoded)
	}
	h.smtp.mu.Lock()
	lastBody := h.smtp.bodies[len(h.smtp.bodies)-1]
	lastRef := h.smtp.refs[len(h.smtp.refs)-1]
	h.smtp.mu.Unlock()
	if strings.Contains(lastBody, secretValue) || strings.Contains(lastBody, "user:pass") {
		t.Fatalf("provider received unredacted message: %q", lastBody)
	}
	if lastRef != "vault://notifications/smtp" {
		t.Fatalf("provider got secret ref %q", lastRef)
	}

	var stored strings.Builder
	rows, err := h.db.Query(ctx, `SELECT smtp_host, smtp_security, smtp_sender, smtp_recipient,
		qq_app_id, qq_target_type, qq_target_id, secret_ref, credential_status
		FROM notification_channels`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var values [9]string
		if err := rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4],
			&values[5], &values[6], &values[7], &values[8]); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			stored.WriteString(value)
			stored.WriteByte('\n')
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	deliveries, err := h.db.Query(ctx, `SELECT provider_code, message, dedupe_key FROM notification_deliveries`)
	if err != nil {
		t.Fatal(err)
	}
	for deliveries.Next() {
		var code, message, key string
		if err := deliveries.Scan(&code, &message, &key); err != nil {
			t.Fatal(err)
		}
		stored.WriteString(code + message + key)
	}
	if err := deliveries.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.String(), secretValue) || strings.Contains(stored.String(), "user:pass") {
		t.Fatalf("database leaked sensitive text: %s", stored.String())
	}

	for _, typ := range []reflect.Type{
		reflect.TypeOf(notifications.SMTPSettings{}),
		reflect.TypeOf(notifications.QQSettings{}),
		reflect.TypeOf(notifications.ChannelInput{}),
		reflect.TypeOf(notifications.ChannelRecord{}),
		reflect.TypeOf(notifications.Delivery{}),
		reflect.TypeOf(notifications.Alert{}),
	} {
		for index := 0; index < typ.NumField(); index++ {
			name := strings.ToLower(typ.Field(index).Name)
			if name == "secretref" || name == "credentialstatus" || name == "credentialrevision" {
				continue
			}
			if strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "credential") {
				t.Fatalf("%s exposes sensitive-looking field %s", typ.Name(), typ.Field(index).Name)
			}
		}
	}
}

func TestDedupeWindowSuppressesSecondSend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureQQ(t)
	h.updateSettings(t, notifications.Settings{
		SMTPFallbackOnQQFailure: false, MaxRetryAttempts: 2, DeduplicationWindowSeconds: 60,
	})
	first, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "dedupe-key", Subject: "Disk", Body: "first",
	})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	h.clock.Advance(30 * time.Second)
	second, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "dedupe-key", Subject: "Disk", Body: "second",
	})
	if err != nil {
		t.Fatalf("suppressed send: %v", err)
	}
	if h.qq.callCount() != 1 {
		t.Fatalf("dedupe window sent twice: %d calls", h.qq.callCount())
	}
	if second.ID != first.ID || second.Message != "first" {
		t.Fatalf("dedupe did not return existing delivery: first=%+v second=%+v", first, second)
	}
	list, err := h.service.ListDeliveries(ctx, notifications.DeliveryFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("deliveries = %+v, %v", list, err)
	}
	h.clock.Advance(31 * time.Second)
	third, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "dedupe-key", Subject: "Disk", Body: "third",
	})
	if err != nil {
		t.Fatalf("post-window send: %v", err)
	}
	if h.qq.callCount() != 2 || third.ID == first.ID {
		t.Fatalf("post-window send calls=%d third=%+v", h.qq.callCount(), third)
	}
	if _, err := h.service.UpsertChannel(ctx, "admin", "req-disable", notifications.ChannelQQBot, notifications.ChannelInput{
		Enabled: false,
		QQ: &notifications.QQSettings{
			AppID: "102000000", TargetType: notifications.QQTargetGroup, TargetID: "group-1",
		},
	}); err != nil {
		t.Fatal(err)
	}
	suppressed, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "dedupe-key", Subject: "Disk", Body: "fourth",
	})
	if err != nil || suppressed.ID != third.ID {
		t.Fatalf("dedupe with disabled channel = %+v, %v", suppressed, err)
	}
	if h.qq.callCount() != 2 {
		t.Fatalf("disabled channel dedupe sent again: %d", h.qq.callCount())
	}
}

func TestQQFailureFallsBackToSMTPExactlyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureQQ(t)
	h.configureSMTP(t)
	h.updateSettings(t, notifications.Settings{
		SMTPFallbackOnQQFailure: true, MaxRetryAttempts: 2, DeduplicationWindowSeconds: 60,
	})
	h.qq.err = errors.New("qq transport failed with token=leak")
	final, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "fallback-key", Subject: "Node down", Body: "node n1 is down",
	})
	if err != nil {
		t.Fatalf("fallback send: %v", err)
	}
	if final.Channel != notifications.ChannelSMTP || final.Status != notifications.DeliveryProviderAccepted || final.Attempts != 1 {
		t.Fatalf("final delivery = %+v", final)
	}
	if h.qq.callCount() != 1 || h.smtp.callCount() != 1 {
		t.Fatalf("provider calls qq=%d smtp=%d", h.qq.callCount(), h.smtp.callCount())
	}
	list, err := h.service.ListDeliveries(ctx, notifications.DeliveryFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("deliveries = %+v, %v", list, err)
	}
	var failed, accepted int
	for _, delivery := range list {
		switch {
		case delivery.Channel == notifications.ChannelQQBot && delivery.Status == notifications.DeliveryFailed:
			failed++
		case delivery.Channel == notifications.ChannelSMTP && delivery.Status == notifications.DeliveryProviderAccepted:
			accepted++
		}
	}
	if failed != 1 || accepted != 1 {
		t.Fatalf("fallback rows failed=%d accepted=%d list=%+v", failed, accepted, list)
	}
	for _, delivery := range list {
		if delivery.Channel == notifications.ChannelQQBot && delivery.Status == notifications.DeliveryFailed {
			if _, err := h.service.RetryDelivery(ctx, "admin", "req-fallback-retry", delivery.ID); !errors.Is(err, notifications.ErrNotRetryable) {
				t.Fatalf("retry after fallback accepted err = %v", err)
			}
		}
	}
	if _, err := h.service.SendAlert(ctx, notifications.Alert{
		DedupeKey: "fallback-key", Subject: "Node down", Body: "node n1 is down",
	}); err != nil {
		t.Fatal(err)
	}
	if h.qq.callCount() != 1 || h.smtp.callCount() != 1 {
		t.Fatalf("fallback repeated on dedupe: qq=%d smtp=%d", h.qq.callCount(), h.smtp.callCount())
	}
}

func TestFallbackDisabledDoesNotUseSMTP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureQQ(t)
	h.configureSMTP(t)
	h.updateSettings(t, notifications.Settings{
		SMTPFallbackOnQQFailure: false, MaxRetryAttempts: 2, DeduplicationWindowSeconds: 0,
	})
	h.qq.err = errors.New("boom")
	delivery, err := h.service.SendAlert(ctx, notifications.Alert{DedupeKey: "no-fallback", Subject: "x", Body: "y"})
	if !errors.Is(err, notifications.ErrProviderFailure) {
		t.Fatalf("err = %v, want ErrProviderFailure", err)
	}
	if delivery.Channel != notifications.ChannelQQBot || delivery.Status != notifications.DeliveryFailed {
		t.Fatalf("delivery = %+v", delivery)
	}
	if h.smtp.callCount() != 0 {
		t.Fatalf("fallback disabled but smtp sent %d", h.smtp.callCount())
	}
}

func TestUnknownAndRateLimitedAreNotRetried(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureQQ(t)
	h.updateSettings(t, notifications.Settings{
		SMTPFallbackOnQQFailure: true, MaxRetryAttempts: 5, DeduplicationWindowSeconds: 0,
	})
	h.configureSMTP(t)
	h.qq.result = notifications.ProviderResult{Outcome: notifications.ProviderUnknown, Code: "timeout"}
	unknown, err := h.service.SendAlert(ctx, notifications.Alert{DedupeKey: "unknown", Subject: "x", Body: "y"})
	if !errors.Is(err, notifications.ErrDeliveryUnknown) {
		t.Fatalf("unknown err = %v", err)
	}
	if unknown.Status != notifications.DeliveryUnknown || h.qq.callCount() != 1 || h.smtp.callCount() != 0 {
		t.Fatalf("unknown delivery=%+v qq=%d smtp=%d", unknown, h.qq.callCount(), h.smtp.callCount())
	}
	if _, err := h.service.RetryDelivery(ctx, "admin", "req-r1", unknown.ID); !errors.Is(err, notifications.ErrNotRetryable) {
		t.Fatalf("unknown retry err = %v", err)
	}
	h.qq.result = notifications.ProviderResult{Outcome: notifications.ProviderRateLimited, Code: "too_many"}
	limited, err := h.service.SendAlert(ctx, notifications.Alert{DedupeKey: "limited", Subject: "x", Body: "y"})
	if !errors.Is(err, notifications.ErrProviderRateLimited) {
		t.Fatalf("rate limited err = %v", err)
	}
	if limited.Status != notifications.DeliveryRateLimited || h.qq.callCount() != 2 || h.smtp.callCount() != 0 {
		t.Fatalf("limited delivery=%+v qq=%d smtp=%d", limited, h.qq.callCount(), h.smtp.callCount())
	}
	if _, err := h.service.RetryDelivery(ctx, "admin", "req-r2", limited.ID); !errors.Is(err, notifications.ErrNotRetryable) {
		t.Fatalf("limited retry err = %v", err)
	}
}

func TestInvalidChannelConfigurationRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	cases := []struct {
		name    string
		channel notifications.Channel
		input   notifications.ChannelInput
	}{
		{"missing host", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, SMTP: &notifications.SMTPSettings{Port: 587, Security: notifications.SMTPSecurityTLS, Sender: "a@b.c", Recipient: "d@e.f"}}},
		{"bad port", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, SMTP: &notifications.SMTPSettings{Host: "smtp.example.com", Port: 70000, Security: notifications.SMTPSecurityTLS, Sender: "a@b.c", Recipient: "d@e.f"}}},
		{"bad security", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, SMTP: &notifications.SMTPSettings{Host: "smtp.example.com", Port: 587, Security: "ssl", Sender: "a@b.c", Recipient: "d@e.f"}}},
		{"bad sender", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, SMTP: &notifications.SMTPSettings{Host: "smtp.example.com", Port: 587, Security: notifications.SMTPSecurityTLS, Sender: "not-an-email", Recipient: "d@e.f"}}},
		{"wrong block", notifications.ChannelSMTP, notifications.ChannelInput{Enabled: true, QQ: &notifications.QQSettings{AppID: "a", TargetType: notifications.QQTargetGroup, TargetID: "g"}}},
		{"bad target type", notifications.ChannelQQBot, notifications.ChannelInput{Enabled: true, QQ: &notifications.QQSettings{AppID: "a", TargetType: "channel", TargetID: "g"}}},
		{"missing app id", notifications.ChannelQQBot, notifications.ChannelInput{Enabled: true, QQ: &notifications.QQSettings{TargetType: notifications.QQTargetGroup, TargetID: "g"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.service.UpsertChannel(ctx, "admin", "req-invalid", tc.channel, tc.input); !errors.Is(err, notifications.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := h.service.GetChannel(ctx, notifications.ChannelSMTP); !errors.Is(err, notifications.ErrChannelNotFound) {
		t.Fatalf("invalid config mutated storage: %v", err)
	}
	if _, err := h.service.SetSecretRef(ctx, "admin", "req-secret", notifications.ChannelSMTP, "has space"); !errors.Is(err, notifications.ErrInvalidSecretRef) {
		t.Fatalf("invalid secret ref err = %v", err)
	}
	if _, err := h.service.SetSecretRef(ctx, "admin", "req-secret", notifications.ChannelSMTP, "vault://missing"); !errors.Is(err, notifications.ErrChannelNotFound) {
		t.Fatalf("secret ref on missing channel err = %v", err)
	}
}

func TestAuthorizationDenialAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }))
	_, err := h.service.UpsertChannel(ctx, "mallory", "req-deny", notifications.ChannelSMTP, notifications.ChannelInput{
		Enabled: true,
		SMTP: &notifications.SMTPSettings{
			Host: "smtp.example.com", Port: 587, Security: notifications.SMTPSecurityTLS,
			Sender: "a@example.com", Recipient: "b@example.com",
		},
	})
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want denied", err)
	}
	if _, err := h.repo.GetChannel(ctx, notifications.ChannelSMTP); !errors.Is(err, notifications.ErrChannelNotFound) {
		t.Fatalf("denied write mutated storage: %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: notifications.ActionWrite, Result: "denied"})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "mallory" {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestTestChannelSanitizesProviderErrorAndIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureSMTP(t)
	h.smtp.err = errors.New("dial smtp://user:pass@example.com:587 secret=topsecret")
	first, err := h.service.TestChannel(ctx, "admin", "req-test", notifications.ChannelSMTP, "idem-1")
	if !errors.Is(err, notifications.ErrProviderFailure) {
		t.Fatalf("test err = %v", err)
	}
	if strings.Contains(err.Error(), "user:pass") || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("provider error leaked: %v", err)
	}
	if first.Status != notifications.DeliveryFailed || h.smtp.callCount() != 1 {
		t.Fatalf("first test = %+v calls=%d", first, h.smtp.callCount())
	}
	second, err := h.service.TestChannel(ctx, "admin", "req-test", notifications.ChannelSMTP, "idem-1")
	if !errors.Is(err, notifications.ErrProviderFailure) {
		t.Fatalf("idempotent failed test err = %v", err)
	}
	if second.ID != first.ID || h.smtp.callCount() != 1 {
		t.Fatalf("idempotency failed: first=%+v second=%+v calls=%d", first, second, h.smtp.callCount())
	}
	h.smtp.err = nil
	h.smtp.result = notifications.ProviderResult{Outcome: notifications.ProviderAccepted, Code: "ok response with spaces"}
	third, err := h.service.TestChannel(ctx, "admin", "req-test", notifications.ChannelSMTP, "idem-2")
	if err != nil || third.Status != notifications.DeliveryProviderAccepted {
		t.Fatalf("third test = %+v, %v", third, err)
	}
	if third.ProviderCode != "provider_error" {
		t.Fatalf("provider code not sanitized: %q", third.ProviderCode)
	}
	channel, err := h.service.GetChannel(ctx, notifications.ChannelSMTP)
	if err != nil || channel.CredentialStatus != notifications.CredentialConfigured {
		t.Fatalf("credential status = %+v, %v", channel, err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: notifications.ActionTest})
	if err != nil || page.Total != 2 {
		t.Fatalf("test audit = %+v, %v", page, err)
	}
}

func TestRetryHonorsBackoffAndLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureQQ(t)
	h.updateSettings(t, notifications.Settings{
		SMTPFallbackOnQQFailure: false, MaxRetryAttempts: 1, DeduplicationWindowSeconds: 0,
	})
	h.qq.err = errors.New("boom")
	failed, err := h.service.SendAlert(ctx, notifications.Alert{DedupeKey: "retry-key", Subject: "x", Body: "y"})
	if !errors.Is(err, notifications.ErrProviderFailure) || failed.Attempts != 1 {
		t.Fatalf("failed = %+v, %v", failed, err)
	}
	if _, err := h.service.RetryDelivery(ctx, "admin", "req-retry-1", failed.ID); !errors.Is(err, notifications.ErrRetryBackoff) {
		t.Fatalf("immediate retry err = %v", err)
	}
	h.clock.Advance(notifications.MinRetryBackoff)
	retried, err := h.service.RetryDelivery(ctx, "admin", "req-retry-2", failed.ID)
	if !errors.Is(err, notifications.ErrProviderFailure) || retried.Attempts != 2 {
		t.Fatalf("retry = %+v, %v", retried, err)
	}
	h.clock.Advance(notifications.MinRetryBackoff)
	if _, err := h.service.RetryDelivery(ctx, "admin", "req-retry-3", failed.ID); !errors.Is(err, notifications.ErrRetryLimit) {
		t.Fatalf("retry limit err = %v", err)
	}
}

func TestCredentialInvalidStatusIsStickyUntilReferenceChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	h.configureSMTP(t)
	h.smtp.result = notifications.ProviderResult{
		Outcome: notifications.ProviderRejected, Code: "auth_failed", CredentialInvalid: true,
	}
	if _, err := h.service.TestChannel(ctx, "admin", "req-invalid-cred", notifications.ChannelSMTP, "cred-1"); !errors.Is(err, notifications.ErrProviderRejected) {
		t.Fatalf("test err = %v", err)
	}
	channel, err := h.service.GetChannel(ctx, notifications.ChannelSMTP)
	if err != nil || channel.CredentialStatus != notifications.CredentialInvalid {
		t.Fatalf("credential status = %+v, %v", channel, err)
	}
	if _, err := h.service.UpsertChannel(ctx, "admin", "req-reupsert", notifications.ChannelSMTP, notifications.ChannelInput{
		Enabled: true,
		SMTP: &notifications.SMTPSettings{
			Host: "smtp.example.com", Port: 587, Security: notifications.SMTPSecurityStartTLS,
			Sender: "alerts@example.com", Recipient: "ops@example.com",
		},
	}); err != nil {
		t.Fatal(err)
	}
	channel, err = h.service.GetChannel(ctx, notifications.ChannelSMTP)
	if err != nil || channel.CredentialStatus != notifications.CredentialInvalid {
		t.Fatalf("upsert hid invalid credential: %+v, %v", channel, err)
	}
	if _, err := h.service.SetSecretRef(ctx, "admin", "req-new-ref", notifications.ChannelSMTP, "vault://notifications/smtp-v2"); err != nil {
		t.Fatal(err)
	}
	channel, err = h.service.GetChannel(ctx, notifications.ChannelSMTP)
	if err != nil || channel.CredentialStatus != notifications.CredentialConfigured {
		t.Fatalf("new secret ref status = %+v, %v", channel, err)
	}
}

func TestSettingsAndQueryValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil)
	for _, settings := range []notifications.Settings{
		{MaxRetryAttempts: -1, DeduplicationWindowSeconds: 10},
		{MaxRetryAttempts: 6, DeduplicationWindowSeconds: 10},
		{MaxRetryAttempts: 1, DeduplicationWindowSeconds: -1},
		{MaxRetryAttempts: 1, DeduplicationWindowSeconds: 86401},
	} {
		if _, err := h.service.UpdateSettings(ctx, "admin", "req-bad-settings", settings); !errors.Is(err, notifications.ErrInvalidSettings) {
			t.Fatalf("settings %+v err = %v", settings, err)
		}
	}
	settings, err := h.service.GetSettings(ctx)
	if err != nil || settings.SMTPFallbackOnQQFailure != true || settings.MaxRetryAttempts != 2 {
		t.Fatalf("default settings = %+v, %v", settings, err)
	}
	if _, err := h.service.ListDeliveries(ctx, notifications.DeliveryFilter{Channel: "sms"}); !errors.Is(err, notifications.ErrInvalidQuery) {
		t.Fatalf("bad filter err = %v", err)
	}
}
