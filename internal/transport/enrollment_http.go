package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// EnrollmentHTTPResult preserves existing wire states, distinct from the
// service's internal approved/rejected. The two metadata fields are PROPOSED
// extensions and require separate contract acceptance before mounting.
type EnrollmentHTTPResult struct {
	State                string    `json:"state"`
	NodeID               string    `json:"nodeId,omitempty"`
	CertificatePEM       string    `json:"certificatePem,omitempty"`
	IntermediateChainPEM string    `json:"intermediateChainPem,omitempty"`
	TrustGeneration      uint64    `json:"trustGeneration,omitempty"`
	ExpiresAt            time.Time `json:"expiresAt"`
}
type EnrollmentHTTPRejection struct{ RequestID, ReasonCode string }
type EnrollmentHTTPConfig struct {
	Authority, ServerName                                  string
	MaxConcurrent, MaxIPs, PerIPPerMinute, GlobalPerMinute int
	// Verify actual local listener profile/PKI authority/quarantine before use
	// and response. No authority is derived from request Host/body/claims.
	Current func(context.Context) error
	// Record only bounded codes/request references, never token/CSR/body/keys.
	// A durable bounded/aggregating adapter is required; no default fake audit.
	Rejected func(context.Context, EnrollmentHTTPRejection) error
}
type EnrollmentHTTP struct {
	service       *EnrollmentService
	cfg           EnrollmentHTTPConfig
	busy          chan struct{}
	mu            sync.Mutex
	minute, total int64
	ips           map[netip.Addr]int
	clock         time.Time
	now           func() time.Time
}

func NewEnrollmentHTTP(service *EnrollmentService, cfg EnrollmentHTTPConfig) (*EnrollmentHTTP, error) {
	u, err := url.Parse("https://" + cfg.Authority)
	if service == nil || err != nil || u.Host != cfg.Authority || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || cfg.ServerName == "" || cfg.ServerName != u.Hostname() || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 32 || cfg.MaxIPs < 1 || cfg.MaxIPs > 4096 || cfg.PerIPPerMinute < 1 || cfg.PerIPPerMinute > 600 || cfg.GlobalPerMinute < 1 || cfg.GlobalPerMinute > 6000 || cfg.Current == nil || cfg.Rejected == nil {
		return nil, ErrEnrollmentUnavailable
	}
	return &EnrollmentHTTP{service: service, cfg: cfg, busy: make(chan struct{}, cfg.MaxConcurrent), ips: map[netip.Addr]int{}, now: time.Now}, nil
}
func (h *EnrollmentHTTP) current(ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrEnrollmentUnavailable
		}
	}()
	if ctx.Err() != nil || h.cfg.Current(ctx) != nil {
		return ErrEnrollmentUnavailable
	}
	return nil
}
func (h *EnrollmentHTTP) reject(w http.ResponseWriter, r *http.Request, status int, code string) {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	func() {
		defer func() {
			if recover() != nil {
				status = http.StatusServiceUnavailable
				code = "ENROLLMENT_SOURCE_UNAVAILABLE"
			}
		}()
		if h.cfg.Rejected(auditCtx, EnrollmentHTTPRejection{w.Header().Get("X-Request-ID"), code}) != nil || auditCtx.Err() != nil {
			status = http.StatusServiceUnavailable
			code = "ENROLLMENT_SOURCE_UNAVAILABLE"
		}
	}()
	w.Header().Set("Content-Type", "application/problem+json")
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "60")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "requestId": w.Header().Get("X-Request-ID")})
}
func (h *EnrollmentHTTP) budget(r *http.Request) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ErrEnrollmentRejected
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return ErrEnrollmentRejected
	}
	ip = ip.Unmap()
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	if now.IsZero() || now.Before(h.clock) {
		return ErrEnrollmentUnavailable
	}
	h.clock = now
	minute := now.Unix() / 60
	if minute != h.minute {
		h.minute = minute
		h.total = 0
		h.ips = map[netip.Addr]int{}
	}
	if h.total >= int64(h.cfg.GlobalPerMinute) || h.ips[ip] >= h.cfg.PerIPPerMinute {
		return errEnrollmentRateLimited
	}
	if _, exists := h.ips[ip]; !exists && len(h.ips) >= h.cfg.MaxIPs {
		return errEnrollmentRateLimited
	}
	h.ips[ip]++
	h.total++
	return nil
}

var errEnrollmentRateLimited = errors.New("transport: enrollment request budget exceeded")

func decodeEnrollmentBody(w http.ResponseWriter, r *http.Request) (AgentEnrollmentInput, error) {
	var out AgentEnrollmentInput
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" {
		return out, ErrEnrollmentRejected
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return out, ErrEnrollmentRejected
	}
	for key := range params {
		if key != "charset" {
			return out, ErrEnrollmentRejected
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil || !utf8.Valid(raw) {
		return out, ErrEnrollmentRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return out, ErrEnrollmentRejected
	}
	values := map[string]string{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return out, ErrEnrollmentRejected
		}
		switch key {
		case "enrollmentToken", "requestId", "csrPem", "pinnedRootSpkiSha256", "agentVersion", "platform":
		default:
			return out, ErrEnrollmentRejected
		}
		if _, duplicate := values[key]; duplicate {
			return out, ErrEnrollmentRejected
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return out, ErrEnrollmentRejected
		}
		var text string
		if json.Unmarshal(value, &text) != nil || text == "" {
			return out, ErrEnrollmentRejected
		}
		values[key] = text
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(values) != 6 {
		return out, ErrEnrollmentRejected
	}
	if _, err = d.Token(); err != io.EOF {
		return out, ErrEnrollmentRejected
	}
	out = AgentEnrollmentInput{values["enrollmentToken"], values["requestId"], values["csrPem"], values["pinnedRootSpkiSha256"], values["agentVersion"], values["platform"]}
	return out, nil
}

func enrollmentHTTPResult(result AgentEnrollmentResult) (EnrollmentHTTPResult, error) {
	out := EnrollmentHTTPResult{State: result.State, ExpiresAt: result.ExpiresAt}
	switch result.State {
	case "pending":
		if result.NodeID != "" || result.CertificatePEM != "" || result.TrustGeneration != 0 {
			return out, ErrEnrollmentUnavailable
		}
	case "rejected", "expired":
		out.State = "denied"
	case "approved":
		if result.NodeID == "" || result.TrustGeneration == 0 {
			return out, ErrEnrollmentUnavailable
		}
		block, rest := pem.Decode([]byte(result.CertificatePEM))
		if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) == 0 {
			return out, ErrEnrollmentUnavailable
		}
		out.State = "issued"
		out.NodeID = result.NodeID
		out.CertificatePEM = string(pem.EncodeToMemory(block))
		out.IntermediateChainPEM = string(rest)
		out.TrustGeneration = result.TrustGeneration
	default:
		return out, ErrEnrollmentUnavailable
	}
	return out, nil
}

func (h *EnrollmentHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	requestID, err := NewConnectionID()
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("X-Request-ID", requestID)
	if r.URL.EscapedPath() != "/api/v1/agent/enroll" {
		h.reject(w, r, 404, "NOT_FOUND")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.reject(w, r, 405, "METHOD_NOT_ALLOWED")
		return
	}
	if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 || r.TLS.ServerName != h.cfg.ServerName || len(r.TLS.PeerCertificates) != 0 || r.Host != h.cfg.Authority {
		h.reject(w, r, 403, "ENROLLMENT_SOURCE_REJECTED")
		return
	}
	for _, name := range []string{"Origin", "Cookie", "Authorization", "Proxy-Authorization", "Referer", "Sec-Fetch-Site", "X-CSRF-Token"} {
		if len(r.Header.Values(name)) != 0 {
			h.reject(w, r, 403, "ENROLLMENT_SOURCE_REJECTED")
			return
		}
	}
	if r.URL.RawQuery != "" {
		h.reject(w, r, 400, "INVALID_REQUEST")
		return
	}
	bounded, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(bounded)
	if h.current(bounded) != nil {
		h.reject(w, r, 503, "ENROLLMENT_SOURCE_UNAVAILABLE")
		return
	}
	if err = h.budget(r); err != nil {
		status := http.StatusServiceUnavailable
		code := "ENROLLMENT_SOURCE_UNAVAILABLE"
		if err == errEnrollmentRateLimited {
			status = http.StatusTooManyRequests
			code = "RATE_LIMITED"
		} else if err == ErrEnrollmentRejected {
			status = http.StatusForbidden
			code = "ENROLLMENT_SOURCE_REJECTED"
		}
		h.reject(w, r, status, code)
		return
	}
	select {
	case h.busy <- struct{}{}:
		defer func() { <-h.busy }()
	default:
		h.reject(w, r, 429, "RATE_LIMITED")
		return
	}
	if http.NewResponseController(w).SetReadDeadline(time.Now().Add(10*time.Second)) != nil {
		h.reject(w, r, 503, "ENROLLMENT_SOURCE_UNAVAILABLE")
		return
	}
	input, err := decodeEnrollmentBody(w, r)
	if err != nil {
		h.reject(w, r, 400, "INVALID_REQUEST")
		return
	}
	result, err := h.service.Submit(bounded, input)
	if err != nil {
		status, code := 503, "ENROLLMENT_SOURCE_UNAVAILABLE"
		if err == ErrEnrollmentRejected {
			status, code = 403, "ENROLLMENT_REJECTED"
		}
		h.reject(w, r, status, code)
		return
	}
	wire, err := enrollmentHTTPResult(result)
	if err != nil {
		h.reject(w, r, 503, "ENROLLMENT_SOURCE_UNAVAILABLE")
		return
	}
	raw, err := json.Marshal(wire)
	if err != nil || len(raw) > 64<<10 || h.current(bounded) != nil {
		h.reject(w, r, 503, "ENROLLMENT_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
