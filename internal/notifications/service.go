package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionWrite    = "notifications.write"
	ActionChannel  = "notifications.channel"
	ActionSecret   = "notifications.secret"
	ActionSettings = "notifications.settings"
	ActionTest     = "notifications.test"
	ActionSend     = "notifications.send"
	ActionRetry    = "notifications.retry"
)

// MinRetryBackoff is the minimum interval between a failed delivery and an
// operator-triggered retry. Automatic retry loops are intentionally absent.
const MinRetryBackoff = 30 * time.Second

const systemAuditSubject = "notification-service"

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionChannel, ActionRetry, ActionSecret, ActionSend, ActionSettings, ActionTest, ActionWrite}
}

// AuditSink is the durable audit port. Sensitive writes append in their own
// transaction so a failed audit record rolls the mutation back.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the notification service. DB, Repo, Audit and Authorizer are
// mandatory; provider ports are optional until a send is attempted.
type Options struct {
	DB         *storage.DB
	Repo       *Repo
	Audit      AuditSink
	Authorizer auth.Authorizer
	SMTPSender SMTPSender
	QQSender   QQSender
	Now        func() time.Time
}

type Service struct {
	db         *storage.DB
	repo       *Repo
	audit      AuditSink
	authorizer auth.Authorizer
	smtp       SMTPSender
	qq         QQSender
	now        func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("notifications: database is required")
	case options.Repo == nil:
		return nil, errors.New("notifications: repository is required")
	case options.Audit == nil:
		return nil, errors.New("notifications: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, audit: options.Audit,
		authorizer: options.Authorizer, smtp: options.SMTPSender, qq: options.QQSender,
		now: now,
	}, nil
}

// GetChannel returns the redacted configuration of one channel.
func (s *Service) GetChannel(ctx context.Context, channel Channel) (ChannelRecord, error) {
	if s == nil {
		return ChannelRecord{}, ErrServiceNotConfigured
	}
	if !channel.Valid() {
		return ChannelRecord{}, ErrInvalidChannel
	}
	return s.repo.GetChannel(ctx, channel)
}

// ListChannels returns every configured channel. Secret values are never present.
func (s *Service) ListChannels(ctx context.Context) ([]ChannelRecord, error) {
	if s == nil {
		return nil, ErrServiceNotConfigured
	}
	return s.repo.ListChannels(ctx)
}

// GetSettings returns fallback, retry and deduplication settings.
func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	if s == nil {
		return Settings{}, ErrServiceNotConfigured
	}
	return s.repo.GetSettings(ctx)
}

// UpsertChannel replaces the configuration for one channel. The caller must hold
// the notification write permission; a denial is audited and never mutates data.
func (s *Service) UpsertChannel(ctx context.Context, subjectID, requestID string, channel Channel, input ChannelInput) (ChannelRecord, error) {
	if err := s.requireRequest(requestID); err != nil {
		return ChannelRecord{}, err
	}
	if !channel.Valid() {
		return ChannelRecord{}, ErrInvalidChannel
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "notification_channel_write"); err != nil {
		return ChannelRecord{}, err
	}
	if err := validateChannelInput(channel, input); err != nil {
		return ChannelRecord{}, err
	}
	now := s.now()
	var record ChannelRecord
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		previous := CredentialNotConfigured
		current, err := s.repo.getChannel(txCtx, tx, channel)
		switch {
		case err == nil:
			record.SecretRef = current.SecretRef
			previous = current.CredentialStatus
		case errors.Is(err, ErrChannelNotFound):
		default:
			return err
		}
		record.Channel = channel
		record.Enabled = input.Enabled
		record.SMTP = input.SMTP
		record.QQ = input.QQ
		record.UpdatedAt = now
		switch {
		case record.SecretRef == "":
			record.CredentialStatus = CredentialNotConfigured
		case previous == CredentialInvalid:
			record.CredentialStatus = CredentialInvalid
		default:
			record.CredentialStatus = CredentialConfigured
		}
		if err := s.repo.putChannel(txCtx, tx, record); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionChannel, "success", requestID, "channel_configured"))
	})
	if err != nil {
		return ChannelRecord{}, err
	}
	return record, nil
}

// SetSecretRef stores only an opaque CX secret-store identifier. The credential
// value itself has no field in this API and is never audited or persisted here.
func (s *Service) SetSecretRef(ctx context.Context, subjectID, requestID string, channel Channel, secretRef string) (ChannelRecord, error) {
	if err := s.requireRequest(requestID); err != nil {
		return ChannelRecord{}, err
	}
	if !channel.Valid() {
		return ChannelRecord{}, ErrInvalidChannel
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "notification_secret_write"); err != nil {
		return ChannelRecord{}, err
	}
	if err := validateSecretRef(secretRef); err != nil {
		return ChannelRecord{}, err
	}
	now := s.now()
	var record ChannelRecord
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := s.repo.getChannel(txCtx, tx, channel); err != nil {
			return err
		}
		if err := s.repo.updateCredential(txCtx, tx, channel, secretRef, CredentialConfigured, now); err != nil {
			return err
		}
		updated, err := s.repo.getChannel(txCtx, tx, channel)
		if err != nil {
			return err
		}
		record = updated
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionSecret, "success", requestID, "secret_reference_set"))
	})
	if err != nil {
		return ChannelRecord{}, err
	}
	return record, nil
}

// UpdateSettings changes fallback, retry and dedupe policy with authorization and
// an audit record in the same transaction.
func (s *Service) UpdateSettings(ctx context.Context, subjectID, requestID string, settings Settings) (Settings, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Settings{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "notification_settings_write"); err != nil {
		return Settings{}, err
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	now := s.now()
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.putSettings(txCtx, tx, settings, now); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionSettings, "success", requestID, "settings_updated"))
	})
	if err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// TestChannel sends one fixed test message through exactly the addressed channel.
// An optional idempotency key suppresses a repeated test inside the dedupe window.
// Upstream errors are reduced to exported sentinels.
func (s *Service) TestChannel(ctx context.Context, subjectID, requestID string, channel Channel, idempotencyKey string) (Delivery, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Delivery{}, err
	}
	if !channel.Valid() {
		return Delivery{}, ErrInvalidChannel
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "notification_test"); err != nil {
		return Delivery{}, err
	}
	if err := validateDedupeKey(idempotencyKey); err != nil {
		return Delivery{}, err
	}
	record, err := s.repo.GetChannel(ctx, channel)
	if err != nil {
		return Delivery{}, err
	}
	if !usableChannel(record) {
		return Delivery{}, ErrChannelNotConfigured
	}
	if err := s.requireSender(channel); err != nil {
		return Delivery{}, err
	}
	message := Message{
		Subject: "ServerFleet notification test",
		Body:    "This is a test notification from ServerFleet.",
	}
	dedupeKey := ""
	if idempotencyKey != "" {
		dedupeKey = "test:" + string(channel) + ":" + idempotencyKey
	}
	start, err := s.beginDelivery(ctx, channel, message, dedupeKey)
	if err != nil {
		return Delivery{}, err
	}
	if start.existing {
		return start.delivery, providerError(start.delivery.Status)
	}
	attempted, sendErr := s.attempt(ctx, record, message, start.delivery)
	final, err := s.finalizeDelivery(ctx, attempted, ActionTest, subjectID, requestID, "test_send", func(txCtx context.Context, tx storage.Tx) error {
		if attempted.ProviderCredentialInvalid {
			return s.repo.updateCredential(txCtx, tx, channel, record.SecretRef, CredentialInvalid, s.now())
		}
		if attempted.Status == DeliveryProviderAccepted || attempted.Status == DeliverySubmitted {
			if record.CredentialStatus != CredentialConfigured {
				return s.repo.updateCredential(txCtx, tx, channel, record.SecretRef, CredentialConfigured, s.now())
			}
		}
		return nil
	})
	if err != nil {
		return Delivery{}, err
	}
	if sendErr != nil {
		return final, sendErr
	}
	return final, nil
}

// SendAlert is the trusted monitoring entry point. It applies deduplication, then
// sends through QQ Bot when available and optionally falls back to SMTP exactly
// once on a definitive QQ failure. Unknown and rate-limited answers are terminal.
func (s *Service) SendAlert(ctx context.Context, alert Alert) (Delivery, error) {
	if s == nil {
		return Delivery{}, ErrServiceNotConfigured
	}
	if !alert.Channel.Valid() && alert.Channel != "" {
		return Delivery{}, ErrInvalidChannel
	}
	message := Message{Subject: alert.Subject, Body: alert.Body}
	if err := validateMessage(message); err != nil {
		return Delivery{}, err
	}
	if err := validateDedupeKey(alert.DedupeKey); err != nil {
		return Delivery{}, err
	}
	message = redactedMessage(message)
	primary, smtpFallback, err := s.resolveChannels(ctx, alert.Channel)
	if err != nil {
		if alert.DedupeKey != "" {
			existing, found, lookupErr := s.findDedupedDelivery(ctx, alert.DedupeKey)
			if lookupErr == nil && found {
				return existing, nil
			}
		}
		return Delivery{}, err
	}
	if err := s.requireSender(primary.Channel); err != nil {
		return Delivery{}, err
	}
	start, err := s.beginDelivery(ctx, primary.Channel, message, alert.DedupeKey)
	if err != nil {
		return Delivery{}, err
	}
	if start.existing {
		return start.delivery, nil
	}
	attempted, sendErr := s.attempt(ctx, primary, message, start.delivery)
	final, err := s.finalizeDelivery(ctx, attempted, ActionSend, systemAuditSubject, "alert", "alert", nil)
	if err != nil {
		return Delivery{}, err
	}
	if !shouldFallback(attempted.Status) || !start.settings.SMTPFallbackOnQQFailure || smtpFallback == nil {
		return final, sendErr
	}
	if err := s.requireSender(ChannelSMTP); err != nil {
		return final, sendErr
	}
	fallback, err := s.insertFallback(ctx, final)
	if err != nil {
		return final, err
	}
	fallbackAttempt, fallbackErr := s.attempt(ctx, *smtpFallback, message, fallback)
	fallbackFinal, err := s.finalizeDelivery(ctx, fallbackAttempt, ActionSend, systemAuditSubject, "alert-fallback", "alert_fallback", nil)
	if err != nil {
		return final, err
	}
	return fallbackFinal, fallbackErr
}

// RetryDelivery retries one definitively failed delivery. It honors the configured
// retry limit and the fixed minimum backoff; unknown, rate-limited and rejected
// answers are not retried, and no retry is attempted implicitly by SendAlert.
func (s *Service) RetryDelivery(ctx context.Context, subjectID, requestID string, deliveryID int64) (Delivery, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Delivery{}, err
	}
	if deliveryID <= 0 {
		return Delivery{}, ErrDeliveryNotFound
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "notification_retry"); err != nil {
		return Delivery{}, err
	}
	delivery, err := s.repo.GetDelivery(ctx, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	if delivery.Status != DeliveryFailed {
		return Delivery{}, ErrNotRetryable
	}
	accepted, err := s.repo.hasAcceptedSibling(ctx, delivery.DedupeKey, delivery.ID)
	if err != nil {
		return Delivery{}, err
	}
	if accepted {
		return Delivery{}, ErrNotRetryable
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return Delivery{}, err
	}
	if delivery.Attempts > settings.MaxRetryAttempts {
		return Delivery{}, ErrRetryLimit
	}
	now := s.now()
	if now.Sub(delivery.UpdatedAt) < MinRetryBackoff {
		return Delivery{}, ErrRetryBackoff
	}
	record, err := s.repo.GetChannel(ctx, delivery.Channel)
	if err != nil {
		return Delivery{}, err
	}
	if !usableChannel(record) {
		return Delivery{}, ErrChannelNotConfigured
	}
	if err := s.requireSender(delivery.Channel); err != nil {
		return Delivery{}, err
	}
	message := Message{Subject: "ServerFleet notification retry", Body: delivery.Message}
	attempted, sendErr := s.attempt(ctx, record, message, delivery)
	final, err := s.finalizeDelivery(ctx, attempted, ActionRetry, subjectID, requestID, "retry", nil)
	if err != nil {
		return Delivery{}, err
	}
	if sendErr != nil {
		return final, sendErr
	}
	return final, nil
}

// ListDeliveries returns redacted delivery attempts ordered newest first.
func (s *Service) ListDeliveries(ctx context.Context, filter DeliveryFilter) ([]Delivery, error) {
	if s == nil {
		return nil, ErrServiceNotConfigured
	}
	return s.repo.ListDeliveries(ctx, filter)
}

type deliveryStart struct {
	delivery Delivery
	existing bool
	settings Settings
}

func (s *Service) findDedupedDelivery(ctx context.Context, dedupeKey string) (Delivery, bool, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	if settings.DeduplicationWindowSeconds == 0 {
		return Delivery{}, false, nil
	}
	since := s.now().Add(-time.Duration(settings.DeduplicationWindowSeconds) * time.Second)
	delivery, err := s.repo.findDelivery(ctx, s.db, dedupeKey, since)
	if errors.Is(err, ErrDeliveryNotFound) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, err
	}
	return delivery, true, nil
}

func (s *Service) beginDelivery(ctx context.Context, channel Channel, message Message, dedupeKey string) (deliveryStart, error) {
	now := s.now()
	var start deliveryStart
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		settings, err := s.repo.getSettings(txCtx, tx)
		if err != nil {
			return err
		}
		start.settings = settings
		if dedupeKey != "" && settings.DeduplicationWindowSeconds > 0 {
			since := now.Add(-time.Duration(settings.DeduplicationWindowSeconds) * time.Second)
			existing, err := s.repo.findDelivery(txCtx, tx, dedupeKey, since)
			switch {
			case err == nil:
				start.delivery = existing
				start.existing = true
				return nil
			case errors.Is(err, ErrDeliveryNotFound):
			default:
				return err
			}
		}
		delivery := Delivery{
			Channel:   channel,
			Status:    DeliveryQueued,
			Message:   message.Body,
			DedupeKey: dedupeKey,
			Attempts:  0,
			CreatedAt: now,
			UpdatedAt: now,
		}
		id, err := s.repo.insertDelivery(txCtx, tx, delivery)
		if err != nil {
			return err
		}
		delivery.ID = id
		start.delivery = delivery
		return nil
	})
	if err != nil {
		return deliveryStart{}, err
	}
	return start, nil
}

func (s *Service) insertFallback(ctx context.Context, primary Delivery) (Delivery, error) {
	now := s.now()
	delivery := Delivery{
		Channel:   ChannelSMTP,
		Status:    DeliveryQueued,
		Message:   primary.Message,
		DedupeKey: primary.DedupeKey,
		CreatedAt: now,
		UpdatedAt: now,
	}
	var id int64
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.repo.insertDelivery(txCtx, tx, delivery)
		if err != nil {
			return err
		}
		id = created
		return nil
	})
	delivery.ID = id
	return delivery, err
}

// attemptedDelivery carries only non-secret classification data from a provider.
type attemptedDelivery struct {
	Delivery
	ProviderCredentialInvalid bool
}

func (s *Service) attempt(ctx context.Context, record ChannelRecord, message Message, delivery Delivery) (attemptedDelivery, error) {
	attempted := attemptedDelivery{Delivery: delivery}
	attempted.Attempts++
	attempted.UpdatedAt = s.now()
	var (
		result ProviderResult
		err    error
	)
	switch record.Channel {
	case ChannelSMTP:
		if s.smtp == nil || record.SMTP == nil {
			return attempted, ErrProviderUnavailable
		}
		result, err = s.smtp.Send(ctx, *record.SMTP, record.SecretRef, message)
	case ChannelQQBot:
		if s.qq == nil || record.QQ == nil {
			return attempted, ErrProviderUnavailable
		}
		result, err = s.qq.Send(ctx, *record.QQ, record.SecretRef, message)
	default:
		return attempted, ErrInvalidChannel
	}
	status, code := classifyProviderResult(result, err)
	attempted.Status = status
	attempted.ProviderCode = code
	attempted.ProviderCredentialInvalid = result.CredentialInvalid
	return attempted, providerError(status)
}

func classifyProviderResult(result ProviderResult, callErr error) (DeliveryStatus, string) {
	if callErr != nil {
		switch result.Outcome {
		case ProviderRateLimited:
			return DeliveryRateLimited, sanitizeProviderCode(result.Code)
		case ProviderUnknown:
			return DeliveryUnknown, sanitizeProviderCode(result.Code)
		case ProviderRejected:
			return DeliveryRejected, sanitizeProviderCode(result.Code)
		default:
			return DeliveryFailed, "provider_error"
		}
	}
	code := sanitizeProviderCode(result.Code)
	switch result.Outcome {
	case "", ProviderAccepted:
		return DeliveryProviderAccepted, code
	case ProviderSubmitted:
		return DeliverySubmitted, code
	case ProviderRejected:
		return DeliveryRejected, code
	case ProviderRateLimited:
		return DeliveryRateLimited, code
	case ProviderUnknown:
		return DeliveryUnknown, code
	default:
		return DeliveryUnknown, code
	}
}

func providerError(status DeliveryStatus) error {
	switch status {
	case DeliveryProviderAccepted, DeliverySubmitted:
		return nil
	case DeliveryRejected:
		return ErrProviderRejected
	case DeliveryRateLimited:
		return ErrProviderRateLimited
	case DeliveryUnknown:
		return ErrDeliveryUnknown
	default:
		return ErrProviderFailure
	}
}

func shouldFallback(status DeliveryStatus) bool {
	return status == DeliveryFailed || status == DeliveryRejected
}

func (s *Service) finalizeDelivery(ctx context.Context, attempted attemptedDelivery, action, subjectID, requestID, reason string, extra func(context.Context, storage.Tx) error) (Delivery, error) {
	delivery := attempted.Delivery
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if extra != nil {
			if err := extra(txCtx, tx); err != nil {
				return err
			}
		}
		if err := s.repo.updateDelivery(txCtx, tx, delivery); err != nil {
			return err
		}
		result := "success"
		if providerError(delivery.Status) != nil {
			result = "failure"
		}
		reasonCode := reason
		if reasonCode == "" {
			reasonCode = string(delivery.Status)
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, action, result, requestID, reasonCode))
	})
	if err != nil {
		return Delivery{}, err
	}
	return delivery, nil
}

func (s *Service) resolveChannels(ctx context.Context, requested Channel) (ChannelRecord, *ChannelRecord, error) {
	if requested != "" {
		record, err := s.repo.GetChannel(ctx, requested)
		if err != nil {
			return ChannelRecord{}, nil, err
		}
		if !usableChannel(record) {
			return ChannelRecord{}, nil, ErrChannelNotConfigured
		}
		if requested == ChannelQQBot {
			if smtp, err := s.repo.GetChannel(ctx, ChannelSMTP); err == nil && usableChannel(smtp) {
				return record, &smtp, nil
			} else if err != nil && !errors.Is(err, ErrChannelNotFound) {
				return ChannelRecord{}, nil, err
			}
		}
		return record, nil, nil
	}
	qq, qqErr := s.repo.GetChannel(ctx, ChannelQQBot)
	if qqErr != nil && !errors.Is(qqErr, ErrChannelNotFound) {
		return ChannelRecord{}, nil, qqErr
	}
	smtp, smtpErr := s.repo.GetChannel(ctx, ChannelSMTP)
	if smtpErr != nil && !errors.Is(smtpErr, ErrChannelNotFound) {
		return ChannelRecord{}, nil, smtpErr
	}
	if qqErr == nil && usableChannel(qq) {
		if smtpErr == nil && usableChannel(smtp) {
			return qq, &smtp, nil
		}
		return qq, nil, nil
	}
	if smtpErr == nil && usableChannel(smtp) {
		return smtp, nil, nil
	}
	return ChannelRecord{}, nil, ErrChannelNotConfigured
}

func (s *Service) requireSender(channel Channel) error {
	switch channel {
	case ChannelSMTP:
		if s.smtp == nil {
			return ErrProviderUnavailable
		}
	case ChannelQQBot:
		if s.qq == nil {
			return ErrProviderUnavailable
		}
	default:
		return ErrInvalidChannel
	}
	return nil
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return ErrServiceNotConfigured
	}
	if !validText(requestID, audit.DefaultLimits().MaxRequestIDBytes) {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID, reason string) error {
	check := auth.Check{SubjectID: subjectID, Action: action}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) {
		denial := s.event(subjectID, action, "denied", requestID, reason)
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("notifications: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID, reason string) audit.Event {
	origin := audit.SourceWeb
	if action == ActionSend {
		origin = audit.SourceSystem
	}
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		Source: origin, ReasonCode: reason, At: s.now(),
	}
}

func isPureDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
