package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/tasks"
)

type gatewayRouteReceiptKey struct{}

// This value is created only after the approved unified nginx task completed.
// It is never decoded from HTTP, a provider response, an Agent frame or cache.
type gatewayRouteReceipt struct {
	ExpectedLeafSHA256                                                       string
	SubjectID, RequestID, TaskID, PlanID, GatewayID, NodeID, Domain, GroupID string
	TargetRevision, GroupRevision                                            uint64
	HTTPS                                                                    bool
	Site                                                                     gateway.NginxSite
	HTTPStatus                                                               int
	ResponseSHA256                                                           string
	ResponseBytes                                                            int64
}

type gatewayRouteChecker struct {
	server     *Server
	tasks      *tasks.Service
	plans      *publishing.Repo
	nodes      *nodes.Repo
	groups     *servicegroups.Repo
	gateways   *gateway.Repo
	authorizer auth.Authorizer
	paths      map[string]GatewayInstallPaths
	roots      map[string]*x509.CertPool
}

func gatewayPublicationProbeToken(plan string, revision uint64, gatewayID string) string {
	h := sha256.Sum256([]byte("serverfleet.gateway.probe.v1\x00" + plan + "\x00" + strconv.FormatUint(revision, 10) + "\x00" + gatewayID))
	return hex.EncodeToString(h[:])
}
func gatewayTaskIdempotencyKey(planID, gatewayID, suffix string, multi bool) string {
	if !multi {
		return planID + suffix
	}
	h := sha256.Sum256([]byte(planID + "\x00" + gatewayID + "\x00" + suffix))
	return "gw-" + hex.EncodeToString(h[:])
}

func newGatewayRouteChecker(s *Server, t *tasks.Service, p *publishing.Repo, n *nodes.Repo, g *servicegroups.Repo, r *gateway.Repo, a auth.Authorizer, paths map[string]GatewayInstallPaths) (gateway.Checker, error) {
	if s == nil || s.Links == nil || s.runtimeGate == nil || t == nil || p == nil || n == nil || g == nil || r == nil || a == nil || len(paths) == 0 {
		return nil, gateway.ErrCheckerUnavailable
	}
	c := &gatewayRouteChecker{server: s, tasks: t, plans: p, nodes: n, groups: g, gateways: r, authorizer: a, paths: make(map[string]GatewayInstallPaths, len(paths)), roots: map[string]*x509.CertPool{}}
	for id, entry := range paths {
		if gateway.ValidateNginxListen(defaultListen(entry.HTTPListen, false)) != nil || gateway.ValidateNginxListen(defaultListen(entry.HTTPSListen, true)) != nil {
			return nil, ErrGatewayDeployment
		}
		c.paths[id] = entry
		if entry.ProbeCAFile != "" {
			raw, err := readPrivateDeployment(entry.ProbeCAFile)
			if err != nil {
				return nil, ErrGatewayDeployment
			}
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(raw) {
				return nil, ErrGatewayDeployment
			}
			c.roots[id] = pool
		}
	}
	return c, nil
}
func defaultListen(value string, https bool) string {
	if value != "" {
		return value
	}
	if https {
		return gateway.DefaultNginxHTTPSListen
	}
	return gateway.DefaultNginxHTTPListen
}

func (c *gatewayRouteChecker) current(ctx context.Context, p *gatewayRouteReceipt) (string, string, error) {
	if c == nil || p == nil || ctx == nil || ctx.Err() != nil || c.server.runtimeGate(ctx) != nil {
		return "", "", gateway.ErrRouteCheckFailed
	}
	if err := c.authorizer.Authorize(ctx, auth.Check{SubjectID: p.SubjectID, Action: gateway.ActionWrite, NodeIDs: []string{p.NodeID}}); err != nil {
		return "", "", err
	}
	plan, _, err := c.plans.Get(ctx, p.PlanID)
	if err != nil || !plan.TargetsPersisted || plan.TargetRevision != p.TargetRevision || plan.CreatedBy != p.SubjectID || plan.Domain != p.Domain || plan.ServiceGroupID != p.GroupID || plan.ExpectedGroupRevision != p.GroupRevision || plan.HTTPS != p.HTTPS {
		return "", "", gateway.ErrRouteCheckFailed
	}
	matches := 0
	for _, id := range plan.GatewayIDs {
		if id == p.GatewayID {
			matches++
		}
	}
	if matches != 1 {
		return "", "", gateway.ErrRouteCheckFailed
	}
	group, err := c.groups.Get(ctx, p.GroupID)
	if err != nil || group.Revision != p.GroupRevision {
		return "", "", gateway.ErrRouteCheckFailed
	}
	gw, err := c.gateways.GetGateway(ctx, c.server.DB, p.GatewayID)
	if err != nil || gw.NodeID != p.NodeID {
		return "", "", gateway.ErrRouteCheckFailed
	}
	node, err := c.nodes.Get(ctx, p.NodeID)
	if err != nil || node.Status != nodes.StatusOnline {
		return "", "", gateway.ErrRouteCheckFailed
	}
	ip := net.ParseIP(strings.TrimSpace(node.Address))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return "", "", gateway.ErrRouteCheckFailed
	}
	link, err := c.server.Links.CurrentConnection(ctx, p.NodeID)
	if err != nil || link.ConnectionID == "" {
		return "", "", gateway.ErrRouteCheckFailed
	}
	task, err := c.tasks.Get(ctx, p.TaskID)
	if err != nil || gatewayCompleted(task) != nil || !task.ApprovalRequired || task.SubmitterID != p.SubjectID || task.ActionID != gateway.NginxActionApply || len(task.Targets) != 1 || task.Targets[0].NodeID != p.NodeID || !validGatewayDigest(task.PlanHash) || task.IdempotencyKey != gatewayTaskIdempotencyKey(plan.ID, p.GatewayID, "-apply", len(plan.GatewayIDs) > 1) {
		return "", "", gateway.ErrRouteCheckFailed
	}
	var params map[string]string
	if json.Unmarshal([]byte(task.Params), &params) != nil || params["operation"] != "apply" || params["site_path"] != p.Site.Path || params["artifact_path"] != p.Site.ArtifactPath || params["config_sha256"] != p.Site.Digest || p.Site.GatewayID != p.GatewayID || p.Site.Domain != p.Domain || p.Site.ProbeToken != gatewayPublicationProbeToken(p.PlanID, p.TargetRevision, p.GatewayID) {
		return "", "", gateway.ErrRouteCheckFailed
	}
	return ip.String(), link.ConnectionID, nil
}

func (c *gatewayRouteChecker) CheckRoute(ctx context.Context, spec gateway.RouteSpec) (gateway.CheckResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return gateway.CheckResult{}, gateway.ErrCheckerUnavailable
	}
	receipt, ok := ctx.Value(gatewayRouteReceiptKey{}).(*gatewayRouteReceipt)
	if !ok || receipt == nil || receipt.GatewayID != spec.GatewayID || receipt.NodeID != spec.NodeID || receipt.Domain != spec.Domain || receipt.GroupID != spec.ServiceGroupID || receipt.GroupRevision != spec.ServiceGroupRevision || receipt.HTTPS != spec.HTTPS || spec.Upstream != "" {
		return gateway.CheckResult{}, gateway.ErrCheckerUnavailable
	}
	ip, connection, err := c.current(ctx, receipt)
	if err != nil {
		return gateway.CheckResult{}, err
	}
	entry, ok := c.paths[spec.GatewayID]
	if !ok {
		return gateway.CheckResult{}, gateway.ErrCheckerUnavailable
	}
	listen := defaultListen(entry.HTTPListen, false)
	scheme := "http"
	if spec.HTTPS {
		listen = defaultListen(entry.HTTPSListen, true)
		scheme = "https"
	}
	port := listen
	if _, e := strconv.Atoi(listen); e != nil {
		_, port, err = net.SplitHostPort(listen)
		if err != nil {
			return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
	}
	endpoint := net.JoinHostPort(ip, port)
	originHost := net.JoinHostPort(spec.Domain, port)
	// No DNS, environment proxy, redirect, cookie jar or arbitrary URL. The only
	// destination is the current approved gateway node and trusted nginx port.
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != originHost {
				return nil, gateway.ErrRouteCheckFailed
			}
			return dialer.DialContext(ctx, network, endpoint)
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: spec.Domain, RootCAs: c.roots[spec.GatewayID]},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+originHost+"/", nil)
	if err != nil {
		return gateway.CheckResult{}, err
	}
	request.Host = spec.Domain
	response, err := client.Do(request)
	if err != nil {
		return gateway.CheckResult{}, fmt.Errorf("%w: gateway HTTP probe", gateway.ErrRouteCheckFailed)
	}
	defer response.Body.Close()
	if receipt.ExpectedLeafSHA256 != "" {
		if !receipt.HTTPS || response.TLS == nil || len(response.TLS.PeerCertificates) == 0 {
			return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
		}
		leaf := sha256.Sum256(response.TLS.PeerCertificates[0].Raw)
		if hex.EncodeToString(leaf[:]) != receipt.ExpectedLeafSHA256 {
			return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
		}
	}

	bytes, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(bytes) > 64<<10 || response.StatusCode < 200 || response.StatusCode >= 300 || len(response.Header.Values("X-ServerFleet-Route")) != 1 || response.Header.Get("X-ServerFleet-Route") != receipt.Site.ProbeToken {
		return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
	}
	currentIP, currentConnection, err := c.current(ctx, receipt)
	if err != nil || currentIP != ip || currentConnection != connection {
		return gateway.CheckResult{}, gateway.ErrRouteCheckFailed
	}
	digest := sha256.Sum256(bytes)
	receipt.HTTPStatus = response.StatusCode
	receipt.ResponseBytes = int64(len(bytes))
	receipt.ResponseSHA256 = hex.EncodeToString(digest[:])
	return gateway.CheckResult{Passed: true}, nil
}
