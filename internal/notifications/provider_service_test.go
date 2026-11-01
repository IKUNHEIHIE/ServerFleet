package notifications_test

import (
	"context"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestNotificationServiceBindsRealSMTPProviderConfirmation(t *testing.T) {
	server, pool := newSMTPServer(t, true)
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
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	service, err := notifications.NewService(notifications.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizerFunc(allowAll),
		SMTPSender: mailer,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := smtpSettings(server, notifications.SMTPSecurityTLS)
	if _, err := service.UpsertChannel(context.Background(), "admin-1", "req-config", notifications.ChannelSMTP, notifications.ChannelInput{
		Enabled: true, SMTP: &settings,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetSecretRef(context.Background(), "admin-1", "req-secret", notifications.ChannelSMTP, "secret/smtp"); err != nil {
		t.Fatal(err)
	}
	delivery, err := service.TestChannel(context.Background(), "admin-1", "req-test", notifications.ChannelSMTP, "test-1")
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != notifications.DeliveryProviderAccepted || delivery.ProviderCode != "smtp_250" {
		t.Fatalf("delivery = %+v", delivery)
	}
	messages := server.storedMessages()
	if len(messages) != 1 {
		t.Fatalf("server messages = %d", len(messages))
	}
	channel, err := service.GetChannel(context.Background(), notifications.ChannelSMTP)
	if err != nil {
		t.Fatal(err)
	}
	if channel.CredentialStatus != notifications.CredentialConfigured {
		t.Fatalf("credential status = %s", channel.CredentialStatus)
	}
	page, err := recorder.Query(context.Background(), audit.Query{Action: notifications.ActionTest})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}
