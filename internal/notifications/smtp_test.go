package notifications_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/notifications"
)

type smtpServer struct {
	t             *testing.T
	listener      net.Listener
	tlsConfig     *tls.Config
	implicit      bool
	advertiseSTLS bool
	rejectAuth    bool
	user          string
	password      string

	mu       sync.Mutex
	messages []string
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, pool
}

func newSMTPServer(t *testing.T, implicit bool) (*smtpServer, *x509.CertPool) {
	t.Helper()
	certificate, pool := testCertificate(t)
	server := &smtpServer{
		t: t, implicit: implicit, advertiseSTLS: !implicit,
		user: "mailer", password: "smtp-secret",
		tlsConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
	}
	var listener net.Listener
	var err error
	if implicit {
		listener, err = tls.Listen("tcp", "127.0.0.1:0", server.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	server.listener = listener
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server, pool
}

func (s *smtpServer) hostPort() (string, int) {
	address := s.listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port
}

func (s *smtpServer) storedMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}

func (s *smtpServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpServer) handle(conn net.Conn) {
	defer conn.Close()
	if s.implicit {
		if tlsConn, ok := conn.(*tls.Conn); ok {
			if err := tlsConn.Handshake(); err != nil {
				return
			}
		}
	}
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !write("220 test ESMTP") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			if !write("250-test") {
				return
			}
			if s.advertiseSTLS {
				if !write("250-STARTTLS") {
					return
				}
			}
			if !write("250-AUTH PLAIN") || !write("250 HELP") {
				return
			}
		case upper == "STARTTLS":
			if !s.advertiseSTLS {
				write("454 TLS not available")
				continue
			}
			if !write("220 ready for TLS") {
				return
			}
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)
			s.advertiseSTLS = false
		case strings.HasPrefix(upper, "AUTH PLAIN "):
			encoded := strings.TrimSpace(line[len("AUTH PLAIN "):])
			raw, _ := base64.StdEncoding.DecodeString(encoded)
			parts := strings.Split(string(raw), "\x00")
			if s.rejectAuth || len(parts) != 3 || parts[1] != s.user || parts[2] != s.password {
				if !write("535 authentication failed") {
					return
				}
				continue
			}
			if !write("235 authentication successful") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"):
			write("250 ok")
		case upper == "DATA":
			if !write("354 end with .") {
				return
			}
			var data strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				trimmed := strings.TrimRight(dataLine, "\r\n")
				if trimmed == "." {
					break
				}
				data.WriteString(trimmed + "\n")
			}
			s.mu.Lock()
			s.messages = append(s.messages, data.String())
			s.mu.Unlock()
			write("250 queued")
		case upper == "QUIT":
			write("221 bye")
			return
		case upper == "RSET", upper == "NOOP":
			write("250 ok")
		default:
			write("500 unsupported")
		}
	}
}

type mailerOptions struct {
	secret     string
	rootCAs    *x509.CertPool
	maxMessage int
	rejectAuth bool
}

func newTestMailer(t *testing.T, server *smtpServer, pool *x509.CertPool, options mailerOptions) *notifications.SMTPMailer {
	t.Helper()
	mailer, err := notifications.NewSMTPMailer(notifications.SMTPConfig{
		Secrets: notifications.SecretResolverFunc(func(context.Context, string) (string, error) {
			return options.secret, nil
		}),
		RootCAs: pool, Timeout: 5 * time.Second, MaxMessageBytes: options.maxMessage,
	})
	if err != nil {
		t.Fatal(err)
	}
	return mailer
}

func smtpSettings(server *smtpServer, security notifications.SMTPSecurity) notifications.SMTPSettings {
	host, port := server.hostPort()
	return notifications.SMTPSettings{
		Host: host, Port: port, Security: security,
		Sender: "fleet@example.com", Recipient: "ops@example.com",
	}
}

func decodeSMTPBody(t *testing.T, raw string) (map[string]string, string) {
	t.Helper()
	parts := strings.SplitN(raw, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatalf("message body = %q", raw)
	}
	headers := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		index := strings.IndexByte(line, ':')
		if index <= 0 {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(line[:index]))] = strings.TrimSpace(line[index+1:])
	}
	encoded := strings.Join(strings.Fields(parts[1]), "")
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return headers, string(body)
}

func TestSMTPMailerImplicitTLS(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	result, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityTLS),
		"secret/smtp", notifications.Message{Subject: "磁盘告警", Body: "disk > 90%"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != notifications.ProviderAccepted {
		t.Fatalf("result = %+v", result)
	}
	messages := server.storedMessages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	headers, body := decodeSMTPBody(t, messages[0])
	if headers["to"] != "ops@example.com" || headers["content-type"] != "text/plain; charset=UTF-8" {
		t.Fatalf("headers = %+v", headers)
	}
	if body != "disk > 90%" {
		t.Fatalf("body = %q", body)
	}
}

func TestSMTPMailerSTARTTLS(t *testing.T) {
	server, pool := newSMTPServer(t, false)
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	result, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityStartTLS),
		"secret/smtp", notifications.Message{Subject: "alert", Body: "starttls"})
	if err != nil || result.Outcome != notifications.ProviderAccepted {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(server.storedMessages()) != 1 {
		t.Fatal("message not stored")
	}
}

func TestSMTPMailerRejectsAuthFailure(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	server.rejectAuth = true
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	result, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityTLS),
		"secret/smtp", notifications.Message{Subject: "alert", Body: "auth"})
	if err == nil || !errors.Is(err, notifications.ErrSMTPSecret) {
		t.Fatalf("err = %v", err)
	}
	if result.Outcome != notifications.ProviderRejected || !result.CredentialInvalid {
		t.Fatalf("result = %+v", result)
	}
}

func TestSMTPMailerRequiresSTARTTLS(t *testing.T) {
	server, pool := newSMTPServer(t, false)
	server.advertiseSTLS = false
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	_, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityStartTLS),
		"secret/smtp", notifications.Message{Subject: "alert", Body: "no starttls"})
	if !errors.Is(err, notifications.ErrSMTPStartTLS) {
		t.Fatalf("err = %v", err)
	}
}

func TestSMTPMailerConnectionFailureIsNotAccepted(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	settings := smtpSettings(server, notifications.SMTPSecurityTLS)
	_ = server.listener.Close()
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	result, err := mailer.Send(context.Background(), settings, "secret/smtp", notifications.Message{Subject: "alert", Body: "down"})
	if err == nil || result.Outcome == notifications.ProviderAccepted {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestSMTPMailerRejectsOversizeMessage(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret", maxMessage: 200})
	result, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityTLS),
		"secret/smtp", notifications.Message{Subject: "alert", Body: strings.Repeat("x", 400)})
	if err == nil || result.Outcome != notifications.ProviderRejected {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(server.storedMessages()) != 0 {
		t.Fatal("oversize message reached the server")
	}
}

func TestSMTPMailerSecretResolverFailure(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	mailer, err := notifications.NewSMTPMailer(notifications.SMTPConfig{
		Secrets: notifications.SecretResolverFunc(func(context.Context, string) (string, error) {
			return "", io.EOF
		}),
		RootCAs: pool, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityTLS), "secret/smtp", notifications.Message{Subject: "alert", Body: "x"}); !errors.Is(err, notifications.ErrSecretUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestSMTPAuthHeaderEncodingRoundTrip(t *testing.T) {
	server, pool := newSMTPServer(t, true)
	mailer := newTestMailer(t, server, pool, mailerOptions{secret: "mailer:smtp-secret"})
	if _, err := mailer.Send(context.Background(), smtpSettings(server, notifications.SMTPSecurityTLS), "secret/smtp", notifications.Message{Subject: "hello", Body: "world"}); err != nil {
		t.Fatal(err)
	}
	raw := server.storedMessages()[0]
	if strings.Contains(raw, "smtp-secret") {
		t.Fatalf("secret leaked into message: %s", raw)
	}
	_ = fmt.Sprintf("%v", raw)
}
