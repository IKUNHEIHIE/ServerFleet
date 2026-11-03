package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

var errAgentConnect = errors.New("agent: controlled connection unavailable; no automatic retry")

const (
	controlledCommandAction        = "tasks.command"
	controlledActionSchemaVersion  = "1"
	agentLinkFrameBytes            = 64 << 10
	agentLinkPath                  = "/api/v1/agent/link"
	agentLinkHandshakeTimeout      = 15 * time.Second
	agentLinkWaitCompletionTimeout = 10 * time.Second
)

type agentLinkEndpoint struct {
	serverName, address, authority string
}

// agentConnectOnce opens exactly one Agent-initiated WSS+mTLS connection. It
// loads only an already issued local identity, samples once, optionally waits
// for one controlled task offer and then closes; there is no retry or re-enroll.
func agentConnectOnce(ctx context.Context, request agentruntime.ConnectRequest) (agentruntime.ConnectResult, error) {
	var result agentruntime.ConnectResult
	if ctx == nil || ctx.Err() != nil || request.StateDir == "" || !filepath.IsAbs(request.StateDir) ||
		!validAgentVersion(request.AgentVersion) || request.WaitTaskMillis < 0 || request.WaitTaskMillis > 600000 {
		return result, errAgentConnect
	}
	rootPEM, err := readAgentFile(request.RootCertificate, 16<<10)
	if err != nil {
		return result, errAgentConnect
	}
	roots, pin, err := parseAgentConnectionRoot(rootPEM)
	if err != nil || pin != request.RootPin {
		return result, errAgentConnect
	}
	endpoint, err := parseAgentLinkEndpoint(request.ControllerURL)
	if err != nil {
		return result, errAgentConnect
	}
	store, err := openAgentIdentity(request.StateDir, pin, 1)
	if err != nil {
		return result, errAgentConnect
	}
	defer store.Close()
	result.StateDir = request.StateDir
	result.NodeName = request.NodeName
	result.WaitTaskMillis = request.WaitTaskMillis
	var facts agentIdentityFacts
	var linkErr error
	err = store.WithCertificate(ctx,
		func(ctx context.Context, observed agentIdentityFacts) error {
			if !executionIDPattern.MatchString(observed.NodeID) || !time.Now().Add(time.Minute).Before(observed.ExpiresAt) {
				return errAgentConnect
			}
			facts = observed
			return nil
		},
		func(ctx context.Context, certificate tls.Certificate) error {
			linkErr = runOneShotLink(ctx, request, endpoint, roots, pin, certificate, facts, &result)
			return linkErr
		})
	if err != nil {
		if errors.Is(linkErr, errAgentWorkersUnknown) {
			return result, errAgentWorkersUnknown
		}
		return result, errAgentConnect
	}
	if result.NodeID == "" || result.ConnectionID == "" || !result.HeartbeatSent || !result.MetricsSent {
		return result, errAgentConnect
	}
	return result, nil
}

func runOneShotLink(ctx context.Context, request agentruntime.ConnectRequest, endpoint agentLinkEndpoint, roots *x509.CertPool, pin string, certificate tls.Certificate, facts agentIdentityFacts, result *agentruntime.ConnectResult) error {
	ledger := filepath.Join(request.StateDir, "executions")
	if err := os.MkdirAll(ledger, 0o700); err != nil {
		return errAgentConnect
	}
	executor, err := newAgentExecutor(ledger, controlledCommandActions())
	if err != nil {
		return errAgentConnect
	}
	defer executor.Close()

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	tlsConfig := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		RootCAs:                roots,
		ServerName:             endpoint.serverName,
		Certificates:           []tls.Certificate{certificate},
		SessionTicketsDisabled: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			return verifyAgentLinkTrust(state, endpoint.serverName, pin)
		},
	}
	rawConnection, err := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}).DialContext(dialCtx, "tcp", endpoint.address)
	if err != nil {
		return errAgentConnect
	}
	connection := tls.Client(rawConnection, tlsConfig)
	if err := connection.HandshakeContext(dialCtx); err != nil || dialCtx.Err() != nil {
		_ = rawConnection.Close()
		return errAgentConnect
	}
	state := connection.ConnectionState()
	if err := verifyAgentLinkTrust(state, endpoint.serverName, pin); err != nil {
		_ = connection.Close()
		return errAgentConnect
	}
	upgradeCtx, upgradeCancel := context.WithTimeout(ctx, 10*time.Second)
	socket, connectionID, err := transport.ClientWebSocket(upgradeCtx, connection, endpoint.authority, agentLinkPath, transport.WebSocketConfig{MaxFrameBytes: agentLinkFrameBytes})
	upgradeCancel()
	if err != nil {
		_ = connection.Close()
		return errAgentConnect
	}
	defer socket.Close()
	bootID, err := transport.NewConnectionID()
	if err != nil {
		_ = socket.Close()
		return errAgentConnect
	}
	semantic, frames := agentSessionLimits()
	var files *agentFileSessions
	minor := uint32(0)
	admit := func(ctx context.Context, offer *agentv1.TaskOffer) error {
		if ctx.Err() != nil {
			return errAgentSession
		}
		if offer == nil {
			return errExecutionRejected
		}
		if offer.ActionId == transport.FileCapability && request.FileRoot != "" {
			_, err := transport.FileOffer(offer)
			return err
		}
		action, ok := executor.actions[offer.ActionId]
		if !ok || offer.Privilege != agentv1.Privilege_PRIVILEGE_NORMAL ||
			offer.PluginId != "" || offer.ActionSchemaVersion != semantic.AllowedActions[offer.ActionId] {
			return errExecutionRejected
		}
		return action.Validate(offer.CanonicalParametersJson)
	}
	if request.FileRoot != "" {
		files, err = newAgentFileSessions(request.FileRoot, facts.NodeID, func(ctx context.Context, o *agentv1.TaskOffer) error {
			if verifyAgentLinkTrust(state, endpoint.serverName, pin) != nil {
				return errAgentSession
			}
			return admit(ctx, o)
		})
		if err != nil {
			return err
		}
		defer files.Close()
		executor.actions[transport.FileCapability] = files.Action()
		semantic.AllowedActions[transport.FileCapability] = "1"
		semantic.AllowFileFrames = true
		minor = 1
	}
	if request.GatewayPrefix != "" {
		if !agentPathWithinRoot(request.FileRoot, request.GatewayPrefix) {
			return errAgentConnect
		}
		actions, err := newGatewayRegistration(gatewayRegistrationConfig{
			Prefix: request.GatewayPrefix, NginxBinary: request.NginxBinary,
		})
		if err != nil {
			return err
		}
		for id, action := range actions {
			executor.actions[id] = action
			semantic.AllowedActions[id] = gatewayActionSchemaVersion
		}
	}
	established := make(chan struct{})
	executed := make(chan struct{}, 4)
	var establishedOnce sync.Once
	cfg := agentSessionConfig{
		NodeID: facts.NodeID, BootID: bootID, Version: request.AgentVersion, ServerName: endpoint.serverName,
		RootSPKISHA256: pin, ProtocolMajor: 1, ProtocolMinor: minor, MaxConcurrent: 1, Semantic: semantic, Frames: frames, Files: files,
		Current: func(_ context.Context, state tls.ConnectionState) error {
			return verifyAgentLinkTrust(state, endpoint.serverName, pin)
		},
		Admit:         admit,
		Probe:         socket.Ping,
		InitialSample: sampleAgentMetrics,
		Established:   func() { establishedOnce.Do(func() { close(established) }) },
		ExecutionFinished: func(string, string) {
			select {
			case executed <- struct{}{}:
			default:
			}
		},
	}
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	defer sessionCancel()
	done := make(chan error, 1)
	go func() { done <- runAgentSession(sessionCtx, socket, state, connectionID, cfg, executor) }()
	select {
	case <-established:
	case <-done:
		return errAgentConnect
	case <-ctx.Done():
		return errAgentConnect
	case <-time.After(agentLinkHandshakeTimeout):
		sessionCancel()
		_ = socket.Close()
		return errAgentConnect
	}
	result.NodeID = facts.NodeID
	result.ConnectionID = connectionID
	result.HeartbeatSent = true
	result.MetricsSent = true
	stopHealth, healthErr := startAgentReleaseHealth(sessionCtx, facts, connectionID)
	if healthErr != nil {
		return healthErr
	}
	defer stopHealth()
	if request.Persistent {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			sessionCancel()
			_ = socket.Close()
			select {
			case err := <-done:
				if errors.Is(err, errAgentWorkersUnknown) {
					return err
				}
				return nil
			case <-time.After(agentLinkWaitCompletionTimeout):
				return errAgentWorkersUnknown
			}
		}
	}
	if wait := time.Duration(request.WaitTaskMillis) * time.Millisecond; wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-executed:
			result.TasksExecuted = 1
		case <-timer.C:
		case <-done:
			return errAgentConnect
		case <-ctx.Done():
			return errAgentConnect
		}
	} else {
		select {
		case <-executed:
			result.TasksExecuted = 1
		default:
		}
	}
	sessionCancel()
	select {
	case <-done:
	case <-time.After(agentLinkWaitCompletionTimeout):
		_ = socket.Close()
		return errAgentConnect
	}
	return nil
}

func controlledCommandActions() map[string]agentAction {
	return map[string]agentAction{
		controlledCommandAction: {
			Validate:     validateControlledCommand,
			Run:          runControlledSelfCheck,
			FilterOutput: sanitizeControlledOutput,
		},
	}
}

// validateControlledCommand accepts only the fixed self-check schema. The CLI
// does not expose a shell or arbitrary local command execution.
func validateControlledCommand(params []byte) error {
	if len(params) == 0 || len(params) > 1024 || !utf8.Valid(params) {
		return errExecutionRejected
	}
	var decoded map[string]string
	if err := json.Unmarshal(params, &decoded); err != nil || decoded == nil || len(decoded) != 0 {
		return errExecutionRejected
	}
	return nil
}

func runControlledSelfCheck(ctx context.Context, _ []byte) executionResult {
	if ctx == nil || ctx.Err() != nil {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if len(host) > 128 {
		host = host[:128]
	}
	output := fmt.Sprintf("server-fleet agent self-check ok host=%s platform=%s arch=%s", host, runtime.GOOS, runtime.GOARCH)
	return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, ExitCode: 0, Output: []byte(output)}
}

func sanitizeControlledOutput(ctx context.Context, raw []byte) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || !utf8.Valid(raw) {
		return nil, errExecutionRejected
	}
	out := make([]rune, 0, len(raw))
	for _, r := range string(raw) {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	text := []byte(string(out))
	if len(text) > 512 {
		text = text[:512]
	}
	return text, nil
}

func admitControlledOffer(offer *agentv1.TaskOffer) error {
	if offer == nil || offer.GetActionId() != controlledCommandAction ||
		offer.GetPrivilege() != agentv1.Privilege_PRIVILEGE_NORMAL || offer.GetPluginId() != "" ||
		offer.GetActionSchemaVersion() != controlledActionSchemaVersion ||
		len(offer.GetCanonicalParametersJson()) > 1024 {
		return errExecutionRejected
	}
	return validateControlledCommand(offer.GetCanonicalParametersJson())
}

func agentSessionLimits() (transport.ValidationLimits, transport.FrameDecodeLimits) {
	semantic := transport.ValidationLimits{
		MaxMessageBytes:    agentLinkFrameBytes,
		MaxTextBytes:       256,
		MaxParametersBytes: 16 << 10,
		MaxOutputBytes:     32 << 10,
		MaxMetrics:         128,
		MaxSyncExecutions:  128,
		MaxCapabilities:    32,
		MaxTimeoutSeconds:  600,
		MaxOfferLifetime:   10 * time.Minute,
		AllowedActions:     map[string]string{controlledCommandAction: controlledActionSchemaVersion},
	}
	return semantic, transport.FrameDecodeLimits{MaxFrameBytes: agentLinkFrameBytes, MaxFields: 64, MaxDepth: 16}
}

func parseAgentConnectionRoot(raw []byte) (*x509.CertPool, string, error) {
	trimmed := bytes.TrimSpace(raw)
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, "", errAgentConnect
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !root.IsCA || !root.BasicConstraintsValid || root.KeyUsage&x509.KeyUsageCertSign == 0 ||
		root.CheckSignatureFrom(root) != nil || time.Now().Before(root.NotBefore) || !time.Now().Before(root.NotAfter) {
		return nil, "", errAgentConnect
	}
	sum := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	pool := x509.NewCertPool()
	pool.AddCert(root)
	return pool, hex.EncodeToString(sum[:]), nil
}

func parseAgentLinkEndpoint(origin string) (agentLinkEndpoint, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" ||
		net.ParseIP(u.Hostname()) != nil || strings.ToLower(u.Hostname()) != u.Hostname() {
		return agentLinkEndpoint{}, errAgentConnect
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	authority := net.JoinHostPort(u.Hostname(), port)
	if len(authority) > 255 {
		return agentLinkEndpoint{}, errAgentConnect
	}
	return agentLinkEndpoint{serverName: u.Hostname(), address: authority, authority: authority}, nil
}

// verifyAgentLinkTrust is also called from VerifyConnection, where
// HandshakeComplete is still false by contract; the post-dial caller repeats it.
func verifyAgentLinkTrust(state tls.ConnectionState, serverName, pin string) error {
	if state.Version < tls.VersionTLS13 || state.ServerName != serverName ||
		len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		return errAgentConnect
	}
	now := time.Now()
	if now.Before(state.PeerCertificates[0].NotBefore) || !now.Before(state.PeerCertificates[0].NotAfter) {
		return errAgentConnect
	}
	for _, chain := range state.VerifiedChains {
		if len(chain) == 0 {
			return errAgentConnect
		}
		sum := sha256.Sum256(chain[len(chain)-1].RawSubjectPublicKeyInfo)
		if hex.EncodeToString(sum[:]) != pin {
			return errAgentConnect
		}
	}
	return nil
}

func readAgentFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errAgentConnect
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) > limit {
		return nil, errAgentConnect
	}
	return raw, nil
}

func agentPathWithinRoot(root, path string) bool {
	if root == "" || path == "" || !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
