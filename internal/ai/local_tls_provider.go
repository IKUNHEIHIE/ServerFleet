package ai

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"strings"

	"serverfleet.local/serverfleet/internal/auth"
)

// Explicit composition-only protocol test configuration. It cannot be decoded
// from owner model settings, select an external endpoint or read environment keys.
type LocalTLSProviderConfig struct {
	Origin string
	Roots  *x509.CertPool
}

func NewLocalTLSOpenAITextProvider(credentials *auth.CredentialService, resolve CredentialResolver, cfg LocalTLSProviderConfig) (*OpenAITextProvider, error) {
	u, err := url.Parse(cfg.Origin)
	if err != nil || cfg.Roots == nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.Port() == "" || strings.Contains(u.Hostname(), "%") {
		return nil, ErrModelUnavailable
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, ErrModelUnavailable
	}
	p, err := NewOpenAIStreamingTextProvider(credentials, resolve)
	if err != nil {
		return nil, err
	}
	t := p.client.Transport.(*http.Transport)
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: cfg.Roots.Clone()}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		h, port, e := net.SplitHostPort(address)
		if e != nil || h != host || port != u.Port() {
			return nil, ErrModelUnavailable
		}
		addresses, e := net.DefaultResolver.LookupIPAddr(ctx, h)
		if e != nil || len(addresses) == 0 {
			return nil, ErrModelUnavailable
		}
		for _, a := range addresses {
			if !a.IP.IsLoopback() || a.Zone != "" {
				return nil, ErrModelUnavailable
			}
		}
		var last error
		for _, a := range addresses {
			conn, e := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	p.endpoint = cfg.Origin + "/v1/responses"
	return p, nil
}
func NewLocalTLSBYOKRuntime(sessions *SessionService, credentials *auth.CredentialService, filter func(context.Context, SessionView, string, string) (string, error), quota GenerationQuota, cfg LocalTLSProviderConfig) (*BYOKRuntime, error) {
	return newBYOKRuntime(sessions, credentials, filter, quota, func(v *auth.CredentialService, r CredentialResolver) (*OpenAITextProvider, error) {
		return NewLocalTLSOpenAITextProvider(v, r, cfg)
	})
}
