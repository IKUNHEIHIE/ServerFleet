package monitoring

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stable probe failure classifications. The persisted error text never contains
// a response body, response headers or target userinfo.
const (
	ProbeCodeInvalidTarget   = "invalid_target"
	ProbeCodeTargetForbidden = "target_forbidden"
	ProbeCodeDNSFailure      = "dns_failure"
	ProbeCodeDialFailed      = "dial_failed"
	ProbeCodeTLSFailed       = "tls_failed"
	ProbeCodeRequestFailed   = "request_failed"
	ProbeCodeTimeout         = "timeout"
)

const (
	maxProbeRedirects = 5
	probeUserAgent    = "ServerFleet-Probe/1.0"
)

// ProbeError is one classified probe failure. Code is stable; Summary is a
// fixed description, never raw upstream text.
type ProbeError struct {
	Code    string
	Summary string
	Cause   error
}

func (e *ProbeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Summary == "" {
		return e.Code
	}
	return e.Code + ": " + e.Summary
}

func (e *ProbeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// HostResolver resolves a hostname to addresses. It is satisfied by
// *net.Resolver and exists so the rebinding boundary can be tested.
type HostResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// NetProberConfig configures NetProber. AllowedHosts is the same explicit
// override list used by TargetPolicy: only entries listed there may resolve to a
// loopback, private, link-local or otherwise forbidden address. RootCAs replaces
// the system trust store for HTTPS/TLS probes (for internal CAs).
type NetProberConfig struct {
	AllowedHosts []string
	Resolver     HostResolver
	RootCAs      *x509.CertPool
}

// NetProber performs real HTTP/TCP/TLS probes with stdlib transports. Every
// connection is dialed against an address resolved and re-validated inside this
// prober, so a DNS answer that changes between the policy check and the dial
// (DNS rebinding) cannot reach a forbidden address.
type NetProber struct {
	policy   TargetPolicy
	resolver HostResolver
	rootCAs  *x509.CertPool
}

func NewNetProber(cfg NetProberConfig) (*NetProber, error) {
	policy, err := NewTargetPolicy(cfg.AllowedHosts)
	if err != nil {
		return nil, err
	}
	resolver := cfg.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &NetProber{policy: policy, resolver: resolver, rootCAs: cfg.RootCAs}, nil
}

// HTTP probes one http/https URL. A response with a 2xx or 3xx status is
// healthy; any other status records the code and a fixed reason without reading
// the body or echoing headers.
func (p *NetProber) HTTP(ctx context.Context, spec TargetSpec) (ProbeResult, error) {
	target, err := p.parseHTTPTarget(spec.Target)
	if err != nil {
		return ProbeResult{}, err
	}
	probeCtx, cancel := probeContext(ctx, spec.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ProbeResult{}, probeInvalidTarget()
	}
	request.Header.Set("User-Agent", probeUserAgent)
	request.Header.Set("Accept", "*/*")
	start := time.Now()
	response, err := p.httpClient(spec.Timeout).Do(request)
	latency := time.Since(start)
	if err != nil {
		return ProbeResult{Latency: &latency}, classifyRequestError(probeCtx, err)
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status >= 200 && status < 400 {
		return ProbeResult{Healthy: true, StatusCode: &status, Latency: &latency}, nil
	}
	return ProbeResult{
		StatusCode: &status, Latency: &latency, Reason: fmt.Sprintf("http_status_%d", status),
	}, nil
}

// TCP probes a host:port by completing a TCP handshake against a validated
// resolved address.
func (p *NetProber) TCP(ctx context.Context, spec TargetSpec) (ProbeResult, error) {
	host, port, err := parseTargetHostPort(spec.Target)
	if err != nil {
		return ProbeResult{}, err
	}
	if err := p.checkTargetName(host); err != nil {
		return ProbeResult{}, err
	}
	probeCtx, cancel := probeContext(ctx, spec.Timeout)
	defer cancel()
	start := time.Now()
	conn, err := p.dialTCP(probeCtx, host, port)
	latency := time.Since(start)
	if err != nil {
		return ProbeResult{Latency: &latency}, err
	}
	_ = conn.Close()
	return ProbeResult{Healthy: true, Latency: &latency}, nil
}

// TLS probes a host:port by completing a verified TLS handshake against a
// validated resolved address. The certificate chain is always verified.
func (p *NetProber) TLS(ctx context.Context, spec TargetSpec) (ProbeResult, error) {
	host, port, err := parseTargetHostPort(spec.Target)
	if err != nil {
		return ProbeResult{}, err
	}
	if err := p.checkTargetName(host); err != nil {
		return ProbeResult{}, err
	}
	probeCtx, cancel := probeContext(ctx, spec.Timeout)
	defer cancel()
	start := time.Now()
	conn, err := p.dialTCP(probeCtx, host, port)
	latency := time.Since(start)
	if err != nil {
		return ProbeResult{Latency: &latency}, err
	}
	tlsConn := tls.Client(conn, p.tlsConfig(host))
	if err := tlsConn.HandshakeContext(probeCtx); err != nil {
		_ = conn.Close()
		latency = time.Since(start)
		return ProbeResult{Latency: &latency}, classifyTLSFailure(probeCtx, err)
	}
	_ = tlsConn.Close()
	latency = time.Since(start)
	return ProbeResult{Healthy: true, Latency: &latency}, nil
}

func (p *NetProber) tlsConfig(serverName string) *tls.Config {
	config := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	if p.rootCAs != nil {
		config.RootCAs = p.rootCAs
	}
	return config
}

func (p *NetProber) httpClient(timeout time.Duration) *http.Client {
	// Proxy is disabled on purpose: an environment proxy would replace the dialed
	// address and bypass the resolved-address policy check.
	transport := &http.Transport{
		Proxy:             nil,
		DialContext:       p.dialContext,
		TLSClientConfig:   p.tlsConfig(""),
		DisableKeepAlives: true,
	}
	if timeout > 0 {
		transport.TLSHandshakeTimeout = timeout
		transport.ResponseHeaderTimeout = timeout
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxProbeRedirects {
				return &ProbeError{Code: ProbeCodeRequestFailed, Summary: "too many redirects"}
			}
			if request.URL.User != nil {
				return probeForbiddenTarget()
			}
			scheme := strings.ToLower(request.URL.Scheme)
			if scheme != "http" && scheme != "https" {
				return probeInvalidTarget()
			}
			return p.checkTargetName(request.URL.Hostname())
		},
	}
}

// dialContext re-resolves the requested host and validates every answer before
// any connection is attempted. It is the DNS rebinding boundary.
func (p *NetProber) dialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return nil, probeInvalidTarget()
	}
	if _, err := parsePort(port); err != nil {
		return nil, probeInvalidTarget()
	}
	if err := p.checkTargetName(host); err != nil {
		return nil, err
	}
	return p.dialTCP(ctx, host, port)
}

func (p *NetProber) dialTCP(ctx context.Context, host, port string) (net.Conn, error) {
	addresses, err := p.resolveHost(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if err := p.checkResolvedAddress(host, address); err != nil {
			return nil, err
		}
	}
	dialer := &net.Dialer{}
	var lastErr error
	for _, address := range addresses {
		conn, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, classifyDialFailure(ctx, lastErr)
}

func (p *NetProber) resolveHost(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	answers, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		if ctx.Err() != nil {
			return nil, classifyContextFailure(ctx, err)
		}
		if timeoutError(err) {
			return nil, probeTimeout(err)
		}
		return nil, &ProbeError{Code: ProbeCodeDNSFailure, Summary: "dns resolution failed", Cause: err}
	}
	addresses := make([]net.IP, 0, len(answers))
	seen := make(map[string]struct{}, len(answers))
	for _, answer := range answers {
		if answer.IP == nil {
			continue
		}
		key := answer.IP.String()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		addresses = append(addresses, answer.IP)
	}
	if len(addresses) == 0 {
		return nil, &ProbeError{Code: ProbeCodeDNSFailure, Summary: "dns returned no address"}
	}
	return addresses, nil
}

// checkResolvedAddress re-applies the address policy to one resolved IP.
// Forbidden ranges pass only for an explicit allowlist entry: the exact hostname
// for a resolved name, or the exact IP/CIDR for a literal IP target. A hostname
// therefore cannot reach a loopback or private address merely because that IP is
// allowlisted. Metadata endpoints are rejected even when allowlisted.
func (p *NetProber) checkResolvedAddress(host string, ip net.IP) error {
	if ip == nil || metadataIP(ip) {
		return probeForbiddenTarget()
	}
	if !forbiddenIP(ip) {
		return nil
	}
	normalized := normalizeHost(host)
	if net.ParseIP(normalized) != nil {
		if p.policy.ipAllowed(ip) {
			return nil
		}
		return probeForbiddenTarget()
	}
	if p.policy.hostnameAllowed(normalized) {
		return nil
	}
	return probeForbiddenTarget()
}

func (p *NetProber) checkTargetName(host string) error {
	normalized := normalizeHost(host)
	if normalized == "" || strings.ContainsAny(normalized, "@?#/\\") {
		return probeInvalidTarget()
	}
	if strings.Contains(normalized, "%") {
		return probeForbiddenTarget()
	}
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		return probeForbiddenTarget()
	}
	if _, blocked := metadataHosts[normalized]; blocked {
		return probeForbiddenTarget()
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		if p.policy.hostnameAllowed(normalized) {
			return nil
		}
		if numericHost(normalized) {
			return probeInvalidTarget()
		}
		if hexNumericHost(normalized) {
			return probeForbiddenTarget()
		}
		if !strings.Contains(normalized, ".") {
			return probeForbiddenTarget()
		}
		for _, suffix := range internalSuffixes {
			if strings.HasSuffix(normalized, suffix) {
				return probeForbiddenTarget()
			}
		}
		return nil
	}
	if metadataIP(ip) {
		return probeForbiddenTarget()
	}
	if p.policy.ipAllowed(ip) {
		return nil
	}
	if forbiddenIP(ip) {
		return probeForbiddenTarget()
	}
	return nil
}

func (p *NetProber) parseHTTPTarget(target string) (*url.URL, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return nil, probeInvalidTarget()
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, probeInvalidTarget()
	}
	if parsed.User != nil {
		return nil, probeForbiddenTarget()
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, probeInvalidTarget()
	}
	if port := parsed.Port(); port != "" {
		if _, err := parsePort(port); err != nil {
			return nil, probeInvalidTarget()
		}
	}
	if err := p.checkTargetName(host); err != nil {
		return nil, err
	}
	return parsed, nil
}

func parseTargetHostPort(target string) (string, string, error) {
	if strings.Contains(target, "://") {
		return "", "", probeInvalidTarget()
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return "", "", probeInvalidTarget()
	}
	if _, err := parsePort(port); err != nil {
		return "", "", probeInvalidTarget()
	}
	if strings.ContainsAny(host, "@?#/\\") {
		return "", "", probeForbiddenTarget()
	}
	return host, port, nil
}

func parsePort(raw string) (int, error) {
	number, err := strconv.Atoi(raw)
	if err != nil || number < 1 || number > 65535 {
		return 0, errors.New("invalid port")
	}
	return number, nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func probeContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func classifyDialFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return classifyContextFailure(ctx, err)
	}
	if timeoutError(err) {
		return probeTimeout(err)
	}
	if err == nil {
		err = errors.New("no address")
	}
	return &ProbeError{Code: ProbeCodeDialFailed, Summary: "connection failed", Cause: err}
}

func classifyRequestError(probeCtx context.Context, err error) error {
	var classified *ProbeError
	if errors.As(err, &classified) {
		return classified
	}
	if probeCtx.Err() != nil {
		return classifyContextFailure(probeCtx, err)
	}
	if timeoutError(err) {
		return probeTimeout(err)
	}
	return &ProbeError{Code: ProbeCodeRequestFailed, Summary: "http request failed", Cause: err}
}

func classifyTLSFailure(probeCtx context.Context, err error) error {
	if probeCtx.Err() != nil {
		return classifyContextFailure(probeCtx, err)
	}
	if timeoutError(err) {
		return probeTimeout(err)
	}
	return &ProbeError{Code: ProbeCodeTLSFailed, Summary: "tls handshake failed", Cause: err}
}

// classifyContextFailure distinguishes caller cancellation (propagated as-is)
// from the probe's own timeout.
func classifyContextFailure(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || timeoutError(err) {
		return probeTimeout(err)
	}
	if err == nil {
		err = errors.New("probe failed")
	}
	return &ProbeError{Code: ProbeCodeRequestFailed, Summary: "probe failed", Cause: err}
}

func probeTimeout(cause error) error {
	return &ProbeError{Code: ProbeCodeTimeout, Summary: "probe timed out", Cause: cause}
}

func probeInvalidTarget() error {
	return &ProbeError{Code: ProbeCodeInvalidTarget, Summary: "target is invalid"}
}

func probeForbiddenTarget() error {
	return &ProbeError{Code: ProbeCodeTargetForbidden, Summary: "target address is forbidden"}
}

func timeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
