package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const SessionCookieName = "__Host-sf_session"

// BrowserGuard is a candidate strict same-origin boundary. Configuration comes
// from the local trusted composition root, not Host/X-Forwarded-* headers.
// It does not authenticate and never infers identity from a browser header.
type BrowserGuard struct {
	origin string
	host   string
}

func NewBrowserGuard(origin string) (*BrowserGuard, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, ErrInvalidCheck
	}
	return &BrowserGuard{origin, u.Host}, nil
}

// Check rejects cross-origin requests for reads too, because responses can
// contain node data or CSRF material. Mutations require an exact Origin.
// TLS termination is outside this helper and needs its own reviewed proxy setup.
func (g *BrowserGuard) Check(r *http.Request, mutation bool) error {
	if g == nil || r == nil || r.Host != g.host {
		return ErrDenied
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || len(origins) == 1 && origins[0] != g.origin {
		return ErrDenied
	}
	if mutation && len(origins) != 1 {
		return ErrDenied
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return ErrDenied
	}
	return nil
}

// SessionCredential rejects ambiguous credentials BEFORE looking up identity.
// Restricted bearer is intentionally unavailable until its cap intersection and
// issue/revoke contract are accepted. An empty Authorization still counts.
func SessionCredential(r *http.Request) (string, error) {
	if r == nil {
		return "", ErrCredential
	}
	count := 0
	legacy := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
			if name == SessionCookieName {
				count++
				if !ok {
					return "", ErrCredential
				}
			}
			if name == "sf_session" {
				legacy++
			}
		}
	}
	if len(r.Header.Values("Authorization")) > 0 || legacy > 0 || count != 1 {
		return "", ErrCredential
	}
	cookies := r.CookiesNamed(SessionCookieName)
	if len(cookies) != 1 || !validSecret(cookies[0].Value) {
		return "", ErrCredential
	}
	return cookies[0].Value, nil
}

func SessionCookie(secret string) (*http.Cookie, error) {
	if !validSecret(secret) {
		return nil, ErrCredential
	}
	return &http.Cookie{Name: SessionCookieName, Value: secret, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}, nil
}

func ClearSessionCookie() *http.Cookie {
	return &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1}
}

// IdentityHTTPStatus maps only fixed auth categories; underlying source errors
// must be sanitized by IdentityService first. It never echoes an error message.
func IdentityHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrCredential):
		return http.StatusUnauthorized
	case errors.Is(err, ErrDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, ErrAlreadyInitialized):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidCheck):
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}
