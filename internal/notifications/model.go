package notifications

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Channel identifies one outbound provider. Both values are modeled explicitly;
// unknown values are rejected rather than becoming a generic sender.
type Channel string

const (
	ChannelSMTP  Channel = "smtp"
	ChannelQQBot Channel = "qqbot"
)

func (c Channel) Valid() bool {
	return c == ChannelSMTP || c == ChannelQQBot
}

// CredentialStatus reports whether an opaque secret reference is present and was
// last accepted. The status is metadata only; it never contains a credential.
type CredentialStatus string

const (
	CredentialNotConfigured CredentialStatus = "not_configured"
	CredentialConfigured    CredentialStatus = "configured"
	CredentialInvalid       CredentialStatus = "invalid"
)

func (c CredentialStatus) Valid() bool {
	switch c {
	case CredentialNotConfigured, CredentialConfigured, CredentialInvalid:
		return true
	default:
		return false
	}
}

// SMTPSecurity is the negotiated transport security for SMTP.
type SMTPSecurity string

const (
	SMTPSecurityTLS      SMTPSecurity = "tls"
	SMTPSecurityStartTLS SMTPSecurity = "starttls"
)

func (s SMTPSecurity) Valid() bool {
	return s == SMTPSecurityTLS || s == SMTPSecurityStartTLS
}

// QQTargetType is the officially supported QQ Bot delivery target shape.
type QQTargetType string

const (
	QQTargetPrivate QQTargetType = "private"
	QQTargetGroup   QQTargetType = "group"
)

func (t QQTargetType) Valid() bool {
	return t == QQTargetPrivate || t == QQTargetGroup
}

// SMTPSettings is the non-secret part of an SMTP channel. The password is never
// a field here; the sender receives the opaque SecretRef separately.
type SMTPSettings struct {
	Host      string       `json:"host"`
	Port      int          `json:"port"`
	Security  SMTPSecurity `json:"security"`
	Sender    string       `json:"sender"`
	Recipient string       `json:"recipient"`
}

// QQSettings is the non-secret part of a QQ Bot channel. The app secret is never
// a field here; the sender receives the opaque SecretRef separately.
type QQSettings struct {
	AppID      string       `json:"appId"`
	TargetType QQTargetType `json:"targetType"`
	TargetID   string       `json:"targetId"`
}

// ChannelInput is a configuration write. Only the block for the addressed channel
// is meaningful; the other block must be nil.
type ChannelInput struct {
	Enabled bool          `json:"enabled"`
	SMTP    *SMTPSettings `json:"smtp,omitempty"`
	QQ      *QQSettings   `json:"qqbot,omitempty"`
}

// ChannelRecord is the redacted stored channel view. SecretRef is an opaque
// identifier, not a credential value.
type ChannelRecord struct {
	AutomationOwnerID  string           `json:"automationOwnerId,omitempty"`
	CredentialRevision uint64           `json:"credentialRevision,omitempty"`
	BackgroundReady    bool             `json:"backgroundReady,omitempty"`
	Channel            Channel          `json:"channel"`
	Enabled            bool             `json:"enabled"`
	SMTP               *SMTPSettings    `json:"smtp,omitempty"`
	QQ                 *QQSettings      `json:"qqbot,omitempty"`
	SecretRef          string           `json:"secretRef,omitempty"`
	CredentialStatus   CredentialStatus `json:"credentialStatus"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

// Settings controls fallback, retry and notification deduplication behavior.
type Settings struct {
	SMTPFallbackOnQQFailure    bool `json:"smtpFallbackOnQQFailure"`
	MaxRetryAttempts           int  `json:"maxRetryAttempts"`
	DeduplicationWindowSeconds int  `json:"deduplicationWindowSeconds"`
}

// Validate bounds retry storms and prevents an unbounded dedupe window.
func (s Settings) Validate() error {
	if s.MaxRetryAttempts < 0 || s.MaxRetryAttempts > 5 {
		return ErrInvalidSettings
	}
	if s.DeduplicationWindowSeconds < 0 || s.DeduplicationWindowSeconds > 86400 {
		return ErrInvalidSettings
	}
	return nil
}

// Message is the redacted-or-redactable outbound content. Credentials must never
// be placed in Subject or Body.
type Message struct {
	Subject string
	Body    string
}

// Alert is the trusted monitoring-to-notification input. An empty Channel selects
// QQ Bot when usable, otherwise SMTP. DedupeKey may be empty to opt out of
// suppression; monitoring supplies a stable key.
type AlertSource struct {
	Kind     string `json:"kind,omitempty"`
	ID       string `json:"id,omitempty"`
	OwnerID  string `json:"ownerId,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}
type Alert struct {
	Source    AlertSource `json:"source,omitempty"`
	DedupeKey string
	Subject   string
	Body      string
	Channel   Channel
}

// DeliveryStatus is the persisted provider outcome. Queued and submitted are
// intermediate states; rate_limited and delivery_unknown are terminal here.
type DeliveryStatus string

const (
	DeliveryQueued           DeliveryStatus = "queued"
	DeliverySubmitted        DeliveryStatus = "submitted"
	DeliveryProviderAccepted DeliveryStatus = "provider_accepted"
	DeliveryRejected         DeliveryStatus = "rejected"
	DeliveryRateLimited      DeliveryStatus = "rate_limited"
	DeliveryFailed           DeliveryStatus = "failed"
	DeliveryUnknown          DeliveryStatus = "delivery_unknown"
)

func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryQueued, DeliverySubmitted, DeliveryProviderAccepted,
		DeliveryRejected, DeliveryRateLimited, DeliveryFailed, DeliveryUnknown:
		return true
	default:
		return false
	}
}

// Delivery is the redacted delivery record. It never contains a credential.
type Delivery struct {
	TaskID       string         `json:"taskId,omitempty"`
	ID           int64          `json:"id"`
	Channel      Channel        `json:"channel"`
	Status       DeliveryStatus `json:"status"`
	ProviderCode string         `json:"providerCode,omitempty"`
	Message      string         `json:"message"`
	DedupeKey    string         `json:"dedupeKey,omitempty"`
	Attempts     int            `json:"attempts"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// DeliveryFilter bounds ListDeliveries. Zero channel/status mean no filter; a
// zero limit selects the default page size.
type DeliveryFilter struct {
	Channel Channel
	Status  DeliveryStatus
	Offset  int
	Limit   int
}

const (
	DefaultDeliveryLimit = 50
	MaxDeliveryLimit     = 500
)

func validateChannelInput(channel Channel, input ChannelInput) error {
	if !channel.Valid() {
		return ErrInvalidChannel
	}
	switch channel {
	case ChannelSMTP:
		if input.SMTP == nil || input.QQ != nil {
			return ErrInvalidConfig
		}
		return validateSMTPSettings(*input.SMTP)
	case ChannelQQBot:
		if input.QQ == nil || input.SMTP != nil {
			return ErrInvalidConfig
		}
		return validateQQSettings(*input.QQ)
	default:
		return ErrInvalidChannel
	}
}

func validateSMTPSettings(settings SMTPSettings) error {
	if !validHost(settings.Host) {
		return ErrInvalidConfig
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return ErrInvalidConfig
	}
	if !settings.Security.Valid() {
		return ErrInvalidConfig
	}
	if !validEmail(settings.Sender) || !validEmail(settings.Recipient) {
		return ErrInvalidConfig
	}
	return nil
}

func validateQQSettings(settings QQSettings) error {
	if !validToken(settings.AppID, 64) || !settings.TargetType.Valid() || !validToken(settings.TargetID, 128) {
		return ErrInvalidConfig
	}
	return nil
}

func validateMessage(message Message) error {
	if !validText(message.Subject, 256) {
		return ErrInvalidMessage
	}
	if message.Body == "" || len(message.Body) > 8192 || !utf8.ValidString(message.Body) {
		return ErrInvalidMessage
	}
	return nil
}

func validateDedupeKey(key string) error {
	if key == "" {
		return nil
	}
	if len(key) > 200 || !utf8.ValidString(key) {
		return ErrInvalidDedupeKey
	}
	if strings.ContainsFunc(key, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	}) {
		return ErrInvalidDedupeKey
	}
	return nil
}

func validateSecretRef(ref string) error {
	if !validToken(ref, 128) {
		return ErrInvalidSecretRef
	}
	return nil
}

func validHost(value string) bool {
	if value == "" || len(value) > 255 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	if strings.ContainsAny(value, "@/?&#\\ \t\r\n") {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validEmail(value string) bool {
	if value == "" || len(value) > 254 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 || strings.Count(value, "@") != 1 {
		return false
	}
	if strings.ContainsAny(value, "<>\"\\ \t\r\n") {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	if !validText(value, max) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsSpace)
}

func usableChannel(record ChannelRecord) bool {
	if !record.Enabled || record.SecretRef == "" || record.CredentialStatus != CredentialConfigured {
		return false
	}
	switch record.Channel {
	case ChannelSMTP:
		return record.SMTP != nil && validateSMTPSettings(*record.SMTP) == nil
	case ChannelQQBot:
		return record.QQ != nil && validateQQSettings(*record.QQ) == nil
	default:
		return false
	}
}

func normalizedFilter(filter DeliveryFilter) (DeliveryFilter, error) {
	if filter.Channel != "" && !filter.Channel.Valid() {
		return DeliveryFilter{}, ErrInvalidQuery
	}
	if filter.Status != "" && !filter.Status.Valid() {
		return DeliveryFilter{}, ErrInvalidQuery
	}
	if filter.Offset < 0 {
		return DeliveryFilter{}, ErrInvalidQuery
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultDeliveryLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxDeliveryLimit {
		return DeliveryFilter{}, ErrInvalidQuery
	}
	return filter, nil
}

// Send ports are injected by composition. Implementations may perform network
// I/O; this package itself never dials. secretRef is an opaque CX secret-store ID,
// never the credential value.
type SMTPSender interface {
	Send(ctx context.Context, settings SMTPSettings, secretRef string, message Message) (ProviderResult, error)
}

type QQSender interface {
	Send(ctx context.Context, settings QQSettings, secretRef string, message Message) (ProviderResult, error)
}

// ProviderOutcome classifies a provider answer. An empty outcome with a nil error
// is treated as accepted for simple ports; unknown non-empty values become
// delivery_unknown and are not retried.
type ProviderOutcome string

const (
	ProviderAccepted    ProviderOutcome = "provider_accepted"
	ProviderSubmitted   ProviderOutcome = "submitted"
	ProviderRejected    ProviderOutcome = "rejected"
	ProviderRateLimited ProviderOutcome = "rate_limited"
	ProviderUnknown     ProviderOutcome = "delivery_unknown"
)

// ProviderResult is the sanitized provider answer. CredentialInvalid lets a test
// send mark the stored credential status without exposing upstream detail.
type ProviderResult struct {
	Outcome           ProviderOutcome
	Code              string
	CredentialInvalid bool
}
