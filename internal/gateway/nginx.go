package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultNginxSiteDir     = "/etc/nginx/conf.d"
	DefaultNginxArtifactDir = "/var/lib/serverfleet/staging/nginx"
	DefaultNginxACMERoot    = "/var/lib/serverfleet/acme"
	DefaultNginxHTTPListen  = "80"
	DefaultNginxHTTPSListen = "443"
	maxNginxConfigBytes     = 32 << 10
	maxNginxUpstreamHosts   = 64
	NginxActionApply        = "gateway.nginx.site_apply"
	NginxActionRemove       = "gateway.nginx.site_remove"
)

var (
	ErrInvalidNginx      = errors.New("gateway: invalid nginx site request")
	ErrNginxTakeover     = errors.New("gateway: existing site requires an approved takeover")
	ErrNginxConfigTooBig = errors.New("gateway: nginx site config exceeds the limit")
)

// NginxSiteRequest is the validated input for one managed site file. Certificate
// and key values are absolute file paths; private key material is never accepted.
type NginxSiteRequest struct {
	GatewayID            string
	Domain               string
	Upstream             string
	ServiceGroupID       string
	ServiceGroupRevision uint64
	UpstreamHosts        []string
	HTTPS                bool
	CertificatePath      string
	PrivateKeyPath       string
	ExistingSite         bool
	TakeoverApproved     bool
	ApprovalRef          string
	SiteDir              string
	ArtifactDir          string
	HTTPListen           string
	HTTPSListen          string
}

// NginxSite is the deterministic rendered file and its digest.
type NginxSite struct {
	GatewayID    string
	Domain       string
	Path         string
	ArtifactPath string
	Config       []byte
	Digest       string
}

// RenderNginxSite validates a route and renders one managed site file. It refuses
// to touch an existing site without both the takeover flag and an approval
// reference, and it validates every value that could inject configuration.
func RenderNginxSite(request NginxSiteRequest) (NginxSite, error) {
	if !validID(request.GatewayID) {
		return NginxSite{}, fmt.Errorf("%w: gateway id", ErrInvalidNginx)
	}
	domain, err := normalizeDomain(request.Domain)
	if err != nil {
		return NginxSite{}, err
	}
	hosts, err := nginxUpstreamHosts(request)
	if err != nil {
		return NginxSite{}, err
	}
	if request.ExistingSite && (!request.TakeoverApproved || !validToken(request.ApprovalRef, 128)) {
		return NginxSite{}, ErrNginxTakeover
	}
	if request.TakeoverApproved && !validToken(request.ApprovalRef, 128) {
		return NginxSite{}, ErrNginxTakeover
	}
	siteDir := request.SiteDir
	if siteDir == "" {
		siteDir = DefaultNginxSiteDir
	}
	if err := validateNginxPath(siteDir, false); err != nil {
		return NginxSite{}, err
	}
	artifactDir := request.ArtifactDir
	if artifactDir == "" {
		artifactDir = DefaultNginxArtifactDir
	}
	if err := validateNginxPath(artifactDir, false); err != nil {
		return NginxSite{}, err
	}
	httpListen, err := normalizeNginxListen(request.HTTPListen, DefaultNginxHTTPListen)
	if err != nil {
		return NginxSite{}, err
	}
	httpsListen, err := normalizeNginxListen(request.HTTPSListen, DefaultNginxHTTPSListen)
	if err != nil {
		return NginxSite{}, err
	}
	certificatePath, keyPath := request.CertificatePath, request.PrivateKeyPath
	if request.HTTPS {
		if err := validateNginxPath(certificatePath, true); err != nil {
			return NginxSite{}, err
		}
		if err := validateNginxPath(keyPath, true); err != nil {
			return NginxSite{}, err
		}
		if certificatePath == keyPath {
			return NginxSite{}, fmt.Errorf("%w: certificate and key paths must differ", ErrInvalidNginx)
		}
	} else if certificatePath != "" || keyPath != "" {
		return NginxSite{}, fmt.Errorf("%w: certificate paths require https", ErrInvalidNginx)
	}
	name := nginxSiteFileName(request.GatewayID, domain)
	sitePath := path.Join(siteDir, name)
	var config strings.Builder
	config.WriteString("# Managed by ServerFleet; do not edit.\n")
	config.WriteString("# gateway: " + request.GatewayID + "\n")
	config.WriteString("# route: " + domain + "\n")
	config.WriteString("# https: " + strconv.FormatBool(request.HTTPS) + "\n")
	if request.ServiceGroupID != "" {
		config.WriteString("# service_group: " + request.ServiceGroupID + " revision " + strconv.FormatUint(request.ServiceGroupRevision, 10) + "\n")
	}
	if len(hosts) > 1 || request.ServiceGroupID != "" {
		upstreamName := nginxUpstreamName(request.GatewayID, domain)
		config.WriteString("upstream " + upstreamName + " {\n")
		config.WriteString("    least_conn;\n")
		for _, host := range hosts {
			config.WriteString("    server " + host + ";\n")
		}
		config.WriteString("}\n\n")
	}
	proxyTarget := hosts[0]
	if len(hosts) > 1 || request.ServiceGroupID != "" {
		proxyTarget = nginxUpstreamName(request.GatewayID, domain)
	}
	if request.HTTPS {
		config.WriteString("server {\n")
		config.WriteString("    listen " + httpListen + ";\n")
		config.WriteString("    server_name " + domain + ";\n")
		config.WriteString("    location /.well-known/acme-challenge/ {\n")
		config.WriteString("        root " + DefaultNginxACMERoot + ";\n")
		config.WriteString("    }\n")
		config.WriteString("    location / {\n")
		config.WriteString("        return 301 https://$host$request_uri;\n")
		config.WriteString("    }\n")
		config.WriteString("}\n\n")
		config.WriteString("server {\n")
		config.WriteString("    listen " + httpsListen + " ssl;\n")
		config.WriteString("    server_name " + domain + ";\n")
		config.WriteString("    ssl_certificate " + certificatePath + ";\n")
		config.WriteString("    ssl_certificate_key " + keyPath + ";\n")
		config.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
		config.WriteString("    ssl_session_cache shared:ServerFleet:10m;\n")
		config.WriteString("    ssl_session_timeout 10m;\n")
		config.WriteString("    add_header Strict-Transport-Security \"max-age=31536000\" always;\n")
		config.WriteString(nginxProxyLocation(proxyTarget))
		config.WriteString("}\n")
	} else {
		config.WriteString("server {\n")
		config.WriteString("    listen " + httpListen + ";\n")
		config.WriteString("    server_name " + domain + ";\n")
		config.WriteString("    location /.well-known/acme-challenge/ {\n")
		config.WriteString("        root " + DefaultNginxACMERoot + ";\n")
		config.WriteString("    }\n")
		config.WriteString(nginxProxyLocation(proxyTarget))
		config.WriteString("}\n")
	}
	if config.Len() > maxNginxConfigBytes {
		return NginxSite{}, ErrNginxConfigTooBig
	}
	payload := []byte(config.String())
	digest := sha256.Sum256(payload)
	digestHex := hex.EncodeToString(digest[:])
	return NginxSite{
		GatewayID: request.GatewayID, Domain: domain, Path: sitePath,
		ArtifactPath: path.Join(artifactDir, request.GatewayID, digestHex+".conf"),
		Config:       payload, Digest: digestHex,
	}, nil
}

func validateNginxTaskParams(params map[string]string) error {
	if len(params) > 32 {
		return fmt.Errorf("%w: task params", ErrInvalidNginx)
	}
	for key, value := range params {
		if !validToken(key, 64) || len(value) > 1024 ||
			strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("%w: task param", ErrInvalidNginx)
		}
	}
	return nil
}

func nginxProxyLocation(upstream string) string {
	var builder strings.Builder
	builder.WriteString("    location / {\n")
	builder.WriteString("        proxy_pass http://" + upstream + ";\n")
	builder.WriteString("        proxy_http_version 1.1;\n")
	builder.WriteString("        proxy_set_header Host $host;\n")
	builder.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
	builder.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	builder.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
	builder.WriteString("        proxy_connect_timeout 10s;\n")
	builder.WriteString("        proxy_read_timeout 60s;\n")
	builder.WriteString("        proxy_send_timeout 60s;\n")
	builder.WriteString("    }\n")
	return builder.String()
}

func nginxUpstreamHosts(request NginxSiteRequest) ([]string, error) {
	if request.Upstream == "" && request.ServiceGroupID == "" {
		return nil, fmt.Errorf("%w: exactly one of upstream or service group is required", ErrInvalidNginx)
	}
	if request.Upstream != "" && request.ServiceGroupID != "" {
		return nil, fmt.Errorf("%w: upstream and service group are mutually exclusive", ErrInvalidNginx)
	}
	if request.Upstream != "" {
		upstream, err := normalizeUpstream(request.Upstream)
		if err != nil {
			return nil, err
		}
		if len(request.UpstreamHosts) != 0 {
			return nil, fmt.Errorf("%w: direct upstream cannot carry group hosts", ErrInvalidNginx)
		}
		return []string{upstream}, nil
	}
	if !validID(request.ServiceGroupID) || request.ServiceGroupRevision == 0 {
		return nil, fmt.Errorf("%w: service group revision", ErrInvalidNginx)
	}
	if len(request.UpstreamHosts) == 0 || len(request.UpstreamHosts) > maxNginxUpstreamHosts {
		return nil, fmt.Errorf("%w: service group hosts", ErrInvalidNginx)
	}
	seen := make(map[string]struct{}, len(request.UpstreamHosts))
	hosts := make([]string, 0, len(request.UpstreamHosts))
	for _, raw := range request.UpstreamHosts {
		host, err := normalizeUpstream(raw)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[host]; duplicate {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("%w: service group hosts", ErrInvalidNginx)
	}
	sort.Strings(hosts)
	return hosts, nil
}

func normalizeNginxListen(value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	if len(value) > 128 || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, " \t\r\n;{}\\") ||
		strings.ContainsFunc(value, unicode.IsControl) {
		return "", fmt.Errorf("%w: listen", ErrInvalidNginx)
	}
	if port, err := strconv.Atoi(value); err == nil {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("%w: listen port", ErrInvalidNginx)
		}
		return strconv.Itoa(port), nil
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("%w: listen", ErrInvalidNginx)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: listen port", ErrInvalidNginx)
	}
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(host, strconv.Itoa(port)), nil
	}
	labels := strings.Split(strings.ToLower(host), ".")
	for _, label := range labels {
		if !validHostLabel(label) {
			return "", fmt.Errorf("%w: listen host", ErrInvalidNginx)
		}
	}
	return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port)), nil
}

func validateNginxPath(value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%w: path is required", ErrInvalidNginx)
		}
		return nil
	}
	if len(value) > 4096 || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") ||
		strings.ContainsAny(value, "\\\x00") || strings.Contains(value, "//") {
		return fmt.Errorf("%w: path", ErrInvalidNginx)
	}
	if strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	}) {
		return fmt.Errorf("%w: path", ErrInvalidNginx)
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "/" || strings.Contains(cleaned, "..") {
		return fmt.Errorf("%w: path", ErrInvalidNginx)
	}
	return nil
}

func nginxSiteFileName(gatewayID, domain string) string {
	safe := strings.ReplaceAll(domain, "*.", "wildcard.")
	safe = strings.ReplaceAll(safe, "*", "wildcard")
	return "serverfleet-" + gatewayID + "-" + safe + ".conf"
}

func nginxUpstreamName(gatewayID, domain string) string {
	sum := sha256.Sum256([]byte(gatewayID + "|" + domain))
	return "sf_" + hex.EncodeToString(sum[:6])
}

// NginxTaskInput binds a rendered site to the unified task pipeline. NodeID is
// the owning gateway node; no shell string is derived from user data.
type NginxTaskInput struct {
	SubjectID        string
	RequestID        string
	GatewayID        string
	NodeID           string
	IdempotencyKey   string
	ExistingSite     bool
	TakeoverApproved bool
	ApprovalRef      string
}

// BuildNginxApplyTask returns the exact unified task request for writing,
// validating and reloading one rendered site. The agent action must itself verify
// the content digest before writing and run its fixed validate/reload commands.
func BuildNginxApplyTask(site NginxSite, input NginxTaskInput) (TaskRequest, error) {
	if !validToken(input.SubjectID, 128) || !validText(input.RequestID, 128) {
		return TaskRequest{}, fmt.Errorf("%w: task subject/request", ErrInvalidNginx)
	}
	if !validID(input.GatewayID) || input.GatewayID != site.GatewayID || !validID(input.NodeID) {
		return TaskRequest{}, fmt.Errorf("%w: task identity", ErrInvalidNginx)
	}
	if len(site.Config) == 0 || !validToken(site.Digest, 64) {
		return TaskRequest{}, fmt.Errorf("%w: rendered site", ErrInvalidNginx)
	}
	params := map[string]string{
		"operation":           "apply",
		"site_path":           site.Path,
		"artifact_path":       site.ArtifactPath,
		"config_sha256":       site.Digest,
		"validate_command":    "nginx -t",
		"reload_command":      "reload",
		ParamExistingSite:     strconv.FormatBool(input.ExistingSite),
		ParamTakeoverApproved: strconv.FormatBool(input.TakeoverApproved),
	}
	if input.ApprovalRef != "" {
		params[ParamApprovalRef] = input.ApprovalRef
	}
	if err := validateNginxTaskParams(params); err != nil {
		return TaskRequest{}, err
	}
	return TaskRequest{
		SubjectID: input.SubjectID, RequestID: input.RequestID, ActionID: NginxActionApply,
		ResourceID: input.GatewayID, NodeIDs: []string{input.NodeID},
		Params: params, IdempotencyKey: input.IdempotencyKey,
	}, nil
}

// BuildNginxRemoveTask removes only when the on-disk digest still matches the
// site that ServerFleet wrote.
func BuildNginxRemoveTask(site NginxSite, input NginxTaskInput) (TaskRequest, error) {
	if !validToken(input.SubjectID, 128) || !validText(input.RequestID, 128) {
		return TaskRequest{}, fmt.Errorf("%w: task subject/request", ErrInvalidNginx)
	}
	if !validID(input.GatewayID) || input.GatewayID != site.GatewayID || !validID(input.NodeID) {
		return TaskRequest{}, fmt.Errorf("%w: task identity", ErrInvalidNginx)
	}
	params := map[string]string{
		"operation":        "remove",
		"site_path":        site.Path,
		"config_sha256":    site.Digest,
		"validate_command": "nginx -t",
		"reload_command":   "reload",
	}
	if err := validateNginxTaskParams(params); err != nil {
		return TaskRequest{}, err
	}
	return TaskRequest{
		SubjectID: input.SubjectID, RequestID: input.RequestID, ActionID: NginxActionRemove,
		ResourceID: input.GatewayID, NodeIDs: []string{input.NodeID},
		Params: params, IdempotencyKey: input.IdempotencyKey,
	}, nil
}
