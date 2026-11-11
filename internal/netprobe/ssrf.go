package netprobe

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// TargetPolicy enforces the SSRF boundary for monitor targets. Only http/https
// URLs (for HTTP) and host:port pairs (for TCP/TLS) are accepted. Loopback,
// RFC1918, unique-local, link-local, unspecified and multicast addresses are
// rejected unless the exact host or CIDR is present in AllowedHosts.
//
// DNS is not resolved here: a public hostname can still resolve to a private
// address at probe time. The Prober must therefore dial only the validated target
// and should apply its own resolved-address policy. Cloud metadata endpoints and
// localhost names are always rejected, even when present in the allowlist.
type TargetPolicy struct {
	entries []allowEntry
}

type allowEntry struct {
	ip      net.IP
	network *net.IPNet
	host    string
}

var (
	cgnatNetwork = mustCIDR("100.64.0.0/10")
	metadataV4   = net.ParseIP("169.254.169.254")
	metadataV6   = net.ParseIP("fd00:ec2::254")
	metadataV4b  = net.ParseIP("169.254.170.2")
	metadataV4c  = net.ParseIP("100.100.100.200")
)

var metadataHosts = map[string]struct{}{
	"metadata":                   {},
	"metadata.google.internal":   {},
	"metadata.goog":              {},
	"instance-data":              {},
	"instance-data.ec2.internal": {},
}

var internalSuffixes = []string{".internal", ".local", ".home.arpa"}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic("monitoring: invalid built-in cidr " + value)
	}
	return network
}

// NewTargetPolicy compiles an explicit allowlist of IPs, CIDRs and exact
// hostnames. The returned policy is immutable and safe for concurrent use.
func NewTargetPolicy(allowedHosts []string) (TargetPolicy, error) {
	policy := TargetPolicy{entries: make([]allowEntry, 0, len(allowedHosts))}
	for _, raw := range allowedHosts {
		entry := strings.TrimSpace(raw)
		if entry == "" || len(entry) > 255 || !validText(entry, 255) || strings.Contains(entry, "://") ||
			strings.ContainsAny(entry, " \t\r\n") {
			return TargetPolicy{}, ErrInvalidAllowedHosts
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				return TargetPolicy{}, ErrInvalidAllowedHosts
			}
			policy.entries = append(policy.entries, allowEntry{network: network})
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			policy.entries = append(policy.entries, allowEntry{ip: ip})
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(entry), ".")
		if host == "" || strings.ContainsAny(host, "@?#\\/") {
			return TargetPolicy{}, ErrInvalidAllowedHosts
		}
		policy.entries = append(policy.entries, allowEntry{host: host})
	}
	return policy, nil
}

// Validate checks one target against the policy without performing DNS or network
// I/O. Errors never echo the target string, which may contain userinfo.
func (p TargetPolicy) Validate(kind Kind, target string) error {
	switch kind {
	case KindHTTP:
		return p.validateHTTP(target)
	case KindTCP, KindTLS:
		return p.validateHostPort(target)
	default:
		return ErrInvalidTarget
	}
}

func (p TargetPolicy) validateHTTP(target string) error {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" || parsed.Scheme == "" {
		return ErrInvalidTarget
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrInvalidTarget
	}
	if parsed.User != nil {
		return ErrTargetForbidden
	}
	host := parsed.Hostname()
	if host == "" {
		return ErrInvalidTarget
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return ErrInvalidTarget
		}
	}
	return p.validateHost(host)
}

func (p TargetPolicy) validateHostPort(target string) error {
	if strings.Contains(target, "://") {
		return ErrInvalidTarget
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return ErrInvalidTarget
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return ErrInvalidTarget
	}
	if strings.ContainsAny(host, "@?#/\\") {
		return ErrTargetForbidden
	}
	return p.validateHost(host)
}

func (p TargetPolicy) validateHost(host string) error {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if normalized == "" || strings.ContainsAny(normalized, "@?#/\\") {
		return ErrInvalidTarget
	}
	if strings.Contains(normalized, "%") {
		return ErrTargetForbidden
	}
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		return ErrTargetForbidden
	}
	if _, blocked := metadataHosts[normalized]; blocked {
		return ErrTargetForbidden
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		if p.hostnameAllowed(normalized) {
			return nil
		}
		if numericHost(normalized) {
			return ErrInvalidTarget
		}
		if hexNumericHost(normalized) {
			return ErrTargetForbidden
		}
		if !strings.Contains(normalized, ".") {
			return ErrTargetForbidden
		}
		for _, suffix := range internalSuffixes {
			if strings.HasSuffix(normalized, suffix) {
				return ErrTargetForbidden
			}
		}
		return nil
	}
	if metadataIP(ip) {
		return ErrTargetForbidden
	}
	if p.ipAllowed(ip) {
		return nil
	}
	if forbiddenIP(ip) {
		return ErrTargetForbidden
	}
	return nil
}

func (p TargetPolicy) ipAllowed(ip net.IP) bool {
	for _, entry := range p.entries {
		if entry.network != nil && entry.network.Contains(ip) {
			return true
		}
		if entry.ip != nil && entry.ip.Equal(ip) {
			return true
		}
	}
	return false
}

func (p TargetPolicy) hostnameAllowed(host string) bool {
	for _, entry := range p.entries {
		if entry.host != "" && entry.host == host {
			return true
		}
	}
	return false
}

func metadataIP(ip net.IP) bool {
	if embedded := embeddedIPv4(ip); embedded != nil {
		return embedded.Equal(metadataV4) || embedded.Equal(metadataV4b) || embedded.Equal(metadataV4c)
	}
	return ip.Equal(metadataV6)
}

// embeddedIPv4 recognizes IPv4-mapped, IPv4-compatible and NAT64 well-known
// addresses whose embedded IPv4 value must be checked too.
func embeddedIPv4(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	if len(ip) != net.IPv6len {
		return nil
	}
	allZero := true
	for _, b := range ip[:12] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return net.IP(ip[12:16])
	}
	if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
		for _, b := range ip[4:12] {
			if b != 0 {
				return nil
			}
		}
		return net.IP(ip[12:16])
	}
	return nil
}

// numericHost catches malformed integer/octal-looking IPv4 forms that a resolver
// might reinterpret differently from net.ParseIP.
func numericHost(host string) bool {
	if host == "" {
		return false
	}
	for _, r := range host {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// hexNumericHost catches legacy 0x-prefixed IPv4 labels that some resolvers
// interpret as an address.
func hexNumericHost(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if len(label) < 3 || (label[0] != '0') || (label[1] != 'x' && label[1] != 'X') {
			continue
		}
		digits := label[2:]
		allHex := true
		for _, r := range digits {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				allHex = false
				break
			}
		}
		if allHex {
			return true
		}
	}
	return false
}

func forbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		if v4.IsUnspecified() || v4.IsLoopback() || v4.IsPrivate() ||
			v4.IsLinkLocalUnicast() || v4.IsLinkLocalMulticast() || v4.IsMulticast() {
			return true
		}
		if v4[0] == 0 || v4.Equal(net.IPv4bcast) {
			return true
		}
		return cgnatNetwork.Contains(v4)
	}
	if embedded := embeddedIPv4(ip); embedded != nil {
		return forbiddenIP(embedded)
	}
	return ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast()
}
