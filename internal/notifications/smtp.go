package notifications

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

const (
	DefaultSMTPTimeout    = 20 * time.Second
	DefaultMaxSMTPMessage = 1 << 20
)

var (
	ErrSMTPConfig    = errors.New("notifications: smtp sender configuration is invalid")
	ErrSMTPSecret    = errors.New("notifications: smtp secret is invalid")
	ErrSMTPMessage   = errors.New("notifications: smtp message is invalid or too large")
	ErrSMTPStartTLS  = errors.New("notifications: smtp server did not offer starttls")
	ErrSMTPTransport = errors.New("notifications: smtp transport failed")
)

// SMTPConfig configures the standard-library SMTP sender. Dialer and RootCAs are
// injectable for a local TLS test server; production uses the system roots.
type SMTPConfig struct {
	Secrets         SecretBorrower
	Dialer          *net.Dialer
	RootCAs         *x509.CertPool
	Timeout         time.Duration
	MaxMessageBytes int
	Now             func() time.Time
}

// SMTPMailer is the real network implementation of SMTPSender. It refuses to send
// credentials over a plaintext connection and never retries by itself.
type SMTPMailer struct {
	secrets  SecretBorrower
	dialer   *net.Dialer
	rootCAs  *x509.CertPool
	timeout  time.Duration
	maxBytes int
	now      func() time.Time
}

var _ SMTPSender = (*SMTPMailer)(nil)

func NewSMTPMailer(config SMTPConfig) (*SMTPMailer, error) {
	if config.Secrets == nil {
		return nil, ErrSMTPConfig
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultSMTPTimeout
	}
	maxBytes := config.MaxMessageBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxSMTPMessage
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &SMTPMailer{
		secrets: config.Secrets, dialer: dialer, rootCAs: config.RootCAs,
		timeout: timeout, maxBytes: maxBytes, now: now,
	}, nil
}

// Send delivers one text message. The provider I/O runs inside the CX borrow
// callback; a connection error after the terminating dot is delivery_unknown.
func (s *SMTPMailer) Send(ctx context.Context, settings SMTPSettings, secretRef string, message Message) (ProviderResult, error) {
	if s == nil || s.secrets == nil {
		return ProviderResult{}, ErrSMTPConfig
	}
	if err := validateSMTPSettings(settings); err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "invalid_config"}, err
	}
	if err := validateMessage(message); err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "invalid_message"}, err
	}
	payload, err := s.buildMessage(settings, message)
	if err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "message_too_large"}, err
	}
	use := SecretUse{Ref: secretRef, Operation: "smtp.send"}
	scope := secretScopeFrom(ctx)
	use.SubjectID, use.RequestID, use.PlanDigest = scope.SubjectID, scope.RequestID, scope.PlanDigest
	ran := false
	var result ProviderResult
	var sendErr error
	err = s.secrets.WithSecret(ctx, use, func(borrowCtx context.Context, credential string) error {
		ran = true
		result, sendErr = s.sendWithCredential(borrowCtx, settings, credential, payload)
		return sendErr
	})
	if err != nil && !ran {
		return ProviderResult{}, fmt.Errorf("%w: smtp", ErrSecretUnavailable)
	}
	if err != nil {
		if sendErr == nil {
			result = ProviderResult{Outcome: ProviderUnknown, Code: "smtp_use_unknown"}
		}
		return result, err
	}
	return result, sendErr
}

func (s *SMTPMailer) sendWithCredential(ctx context.Context, settings SMTPSettings, credential string, payload []byte) (ProviderResult, error) {
	if credential == "" {
		return ProviderResult{}, ErrSMTPSecret
	}
	username, password, err := parseSMTPCredential(credential, settings.Sender)
	if err != nil {
		return ProviderResult{}, err
	}
	client, conn, stop, err := s.connect(ctx, settings)
	if err != nil {
		return classifySMTPError("connect", err)
	}
	defer func() {
		stop()
		_ = client.Close()
		_ = conn.Close()
	}()

	auth := &smtpPlainAuth{username: username, password: password}
	if err := client.Auth(auth); err != nil {
		return classifySMTPError("auth", err)
	}
	if err := client.Mail(settings.Sender); err != nil {
		return classifySMTPError("mail_from", err)
	}
	if err := client.Rcpt(settings.Recipient); err != nil {
		return classifySMTPError("rcpt_to", err)
	}
	writer, err := client.Data()
	if err != nil {
		return classifySMTPError("data_open", err)
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return ProviderResult{Outcome: ProviderUnknown, Code: "smtp_data_unknown"}, fmt.Errorf("%w: data write", ErrSMTPTransport)
	}
	if err := writer.Close(); err != nil {
		return classifySMTPError("data_close", err)
	}
	return ProviderResult{Outcome: ProviderAccepted, Code: "smtp_250"}, nil
}

func (s *SMTPMailer) connect(ctx context.Context, settings SMTPSettings) (*smtp.Client, net.Conn, func(), error) {
	address := net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port))
	conn, err := s.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	release := func() { stop() }
	deadline := s.now().Add(s.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: settings.Host,
		RootCAs:    s.rootCAs,
	}
	switch settings.Security {
	case SMTPSecurityTLS:
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, nil, nil, err
		}
		client, err := smtp.NewClient(tlsConn, settings.Host)
		if err != nil {
			_ = tlsConn.Close()
			return nil, nil, nil, err
		}
		return client, tlsConn, release, nil
	case SMTPSecurityStartTLS:
		client, err := smtp.NewClient(conn, settings.Host)
		if err != nil {
			_ = conn.Close()
			return nil, nil, nil, err
		}
		if err := client.Hello("serverfleet.local"); err != nil {
			_ = client.Close()
			return nil, nil, nil, err
		}
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, nil, nil, ErrSMTPStartTLS
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, nil, nil, err
		}
		return client, conn, release, nil
	default:
		_ = conn.Close()
		return nil, nil, nil, ErrSMTPConfig
	}
}

func (s *SMTPMailer) buildMessage(settings SMTPSettings, message Message) ([]byte, error) {
	if len(message.Body) > s.maxBytes || len(message.Subject) > 256 {
		return nil, ErrSMTPMessage
	}
	encodedBody := base64.StdEncoding.EncodeToString([]byte(message.Body))
	var body strings.Builder
	for len(encodedBody) > 76 {
		body.WriteString(encodedBody[:76])
		body.WriteString("\r\n")
		encodedBody = encodedBody[76:]
	}
	body.WriteString(encodedBody)
	subject := mime.QEncoding.Encode("utf-8", message.Subject)
	if strings.ContainsAny(subject, "\r\n") {
		return nil, ErrSMTPMessage
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	var payload strings.Builder
	payload.WriteString("From: " + settings.Sender + "\r\n")
	payload.WriteString("To: " + settings.Recipient + "\r\n")
	payload.WriteString("Subject: " + subject + "\r\n")
	payload.WriteString("Date: " + s.now().UTC().Format(time.RFC1123Z) + "\r\n")
	payload.WriteString("Message-ID: <" + hex.EncodeToString(nonce) + "@serverfleet.local>\r\n")
	payload.WriteString("MIME-Version: 1.0\r\n")
	payload.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	payload.WriteString("Content-Transfer-Encoding: base64\r\n")
	payload.WriteString("\r\n")
	payload.WriteString(body.String())
	payload.WriteString("\r\n")
	if payload.Len() > s.maxBytes {
		return nil, ErrSMTPMessage
	}
	return []byte(payload.String()), nil
}

// smtpPlainAuth sends AUTH PLAIN only after this sender has established TLS. The
// standard smtp.PlainAuth refuses implicit-TLS connections because net/smtp only
// marks TLS after STARTTLS, so a separate implementation keeps port 465 working
// without weakening the plaintext check.
type smtpPlainAuth struct {
	username string
	password string
}

func (a *smtpPlainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a *smtpPlainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, ErrSMTPTransport
	}
	return nil, nil
}

// parseSMTPCredential accepts either a JSON object or "username:password". A bare
// value is treated as a password for the configured sender address.
func parseSMTPCredential(credential, sender string) (string, string, error) {
	if len(credential) > 4096 || strings.ContainsAny(credential, "\x00\r\n") {
		return "", "", ErrSMTPSecret
	}
	if strings.HasPrefix(strings.TrimSpace(credential), "{") {
		var parsed struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal([]byte(credential), &parsed); err != nil {
			return "", "", ErrSMTPSecret
		}
		username := parsed.Username
		if username == "" {
			username = sender
		}
		if parsed.Password == "" || !validToken(username, 320) {
			return "", "", ErrSMTPSecret
		}
		return username, parsed.Password, nil
	}
	if index := strings.IndexByte(credential, ':'); index > 0 {
		username := credential[:index]
		password := credential[index+1:]
		if password == "" || !validToken(username, 320) {
			return "", "", ErrSMTPSecret
		}
		return username, password, nil
	}
	if !validToken(sender, 320) {
		return "", "", ErrSMTPSecret
	}
	return sender, credential, nil
}

func classifySMTPError(stage string, err error) (ProviderResult, error) {
	var protocolErr *textproto.Error
	if errors.As(err, &protocolErr) {
		code := fmt.Sprintf("smtp_%d", protocolErr.Code)
		if stage == "auth" && (protocolErr.Code == 530 || protocolErr.Code == 534 || protocolErr.Code == 535) {
			return ProviderResult{Outcome: ProviderRejected, Code: "smtp_auth_failed", CredentialInvalid: true},
				fmt.Errorf("%w: auth", ErrSMTPSecret)
		}
		switch {
		case protocolErr.Code == 429 || protocolErr.Code == 421:
			return ProviderResult{Outcome: ProviderRateLimited, Code: code}, fmt.Errorf("%w: rate limited", ErrSMTPTransport)
		case protocolErr.Code >= 400 && protocolErr.Code < 500:
			return ProviderResult{Outcome: ProviderRejected, Code: code}, fmt.Errorf("%w: temporary rejection", ErrSMTPTransport)
		case stage == "data_close":
			// The terminating dot was rejected explicitly, so the message was not
			// accepted.
			return ProviderResult{Outcome: ProviderRejected, Code: code}, fmt.Errorf("%w: message rejected", ErrSMTPTransport)
		default:
			return ProviderResult{Outcome: ProviderRejected, Code: code}, fmt.Errorf("%w: rejected", ErrSMTPTransport)
		}
	}
	if stage == "data_close" || stage == "data_write" {
		return ProviderResult{Outcome: ProviderUnknown, Code: "smtp_data_unknown"}, fmt.Errorf("%w: connection lost after data", ErrSMTPTransport)
	}
	if errors.Is(err, ErrSMTPStartTLS) || errors.Is(err, ErrSMTPConfig) {
		return ProviderResult{}, err
	}
	if stage == "connect" {
		return ProviderResult{}, fmt.Errorf("%w: connect", ErrSMTPTransport)
	}
	if stage == "auth" || stage == "mail_from" || stage == "rcpt_to" || stage == "data_open" {
		// The message was not submitted; this is a known failure, not unknown.
		return ProviderResult{}, fmt.Errorf("%w: %s", ErrSMTPTransport, stage)
	}
	return ProviderResult{Outcome: ProviderUnknown, Code: "smtp_unknown"}, fmt.Errorf("%w: connection lost", ErrSMTPTransport)
}
