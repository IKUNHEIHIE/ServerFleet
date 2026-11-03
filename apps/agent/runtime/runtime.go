// Package runtime owns the optional Agent CLI presentation for status self-check,
// one explicit enrollment attempt, a single WSS+mTLS connection or resident
// links with bounded reconnect. It never replaces an existing identity.
package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Scaffold is the unchanged M0-P1 no-argument output.
const Scaffold = "Server Fleet Agent — M0-P1 scaffold; registration and execution unavailable"

// DefaultVersion is the default Agent version reported during enrollment.
const DefaultVersion = "0.9.0-dev"

var errRuntime = errors.New("agent runtime: invalid or unavailable local state")

// Status is the locally verified Agent identity projection.
type Status struct {
	StateDir        string     `json:"stateDir"`
	Enrolled        bool       `json:"enrolled"`
	NodeID          string     `json:"nodeId,omitempty"`
	Fingerprint     string     `json:"fingerprint,omitempty"`
	TrustGeneration uint64     `json:"trustGeneration,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
}

// EnrollRequest is one explicit enrollment attempt with resolved inputs.
type EnrollRequest struct {
	ControllerURL   string
	EnrollToken     string
	StateDir        string
	RootPin         string
	RootCertificate string
	AgentVersion    string
}

// EnrollResult is the observed Controller enrollment outcome.
type EnrollResult struct {
	State     string    `json:"state"`
	NodeID    string    `json:"nodeId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ConnectRequest is one controlled single-shot link attempt with resolved inputs.
type ConnectRequest struct {
	ControllerURL   string
	StateDir        string
	RootPin         string
	RootCertificate string
	NodeName        string
	WaitTaskMillis  int
	AgentVersion    string
	Persistent      bool
	FileRoot        string
	GatewayPrefix   string
	NginxBinary     string
}

// ConnectResult is the observed single-shot connection outcome.
type ConnectResult struct {
	StateDir       string `json:"stateDir"`
	NodeName       string `json:"nodeName,omitempty"`
	NodeID         string `json:"nodeId"`
	ConnectionID   string `json:"connectionId"`
	HeartbeatSent  bool   `json:"heartbeatSent"`
	MetricsSent    bool   `json:"metricsSent"`
	WaitTaskMillis int    `json:"waitTaskMillis"`
	TasksExecuted  int    `json:"tasksExecuted"`
}

// Deps binds the reviewed local identity store, enrollment client and one-shot
// connection loop.
type Deps struct {
	Status  func(ctx context.Context, stateDir, rootPin string) (Status, error)
	Enroll  func(ctx context.Context, request EnrollRequest) (EnrollResult, error)
	Connect func(ctx context.Context, request ConnectRequest) (ConnectResult, error)
	Serve   func(ctx context.Context, request ConnectRequest) error
}

// Run executes the Agent CLI. No arguments keep the historical scaffold output.
func Run(ctx context.Context, args []string, deps Deps, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, Scaffold)
		return 0
	}
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	statusFlag := flags.Bool("status", false, "print local identity status and exit")
	stateDir := flags.String("state-dir", "", "local Agent identity directory (absolute)")
	rootPin := flags.String("root-pin", "", "trusted local Agent root SPKI SHA-256 (64 hex)")
	rootCert := flags.String("root-cert", "", "local Agent root certificate PEM path (also derives -root-pin)")
	controllerURL := flags.String("controller-url", "", "Controller https or wss origin, e.g. https://controller.example")
	enrollToken := flags.String("enroll-token", "", "one-time enrollment token")
	connectOnce := flags.Bool("connect-once", false, "open one controlled WSS+mTLS connection, sample once and exit")
	serve := flags.Bool("serve", false, "maintain WSS+mTLS with bounded reconnect; stop with SIGTERM")
	fileRoot := flags.String("file-root", "", "explicit private Linux file root (enables reviewed files.transfer.v1 profile)")
	gatewayPrefix := flags.String("gateway-prefix", "", "explicit private Linux nginx prefix (enables reviewed gateway.nginx.* profile)")
	nginxBinary := flags.String("nginx-binary", "", "nginx executable for the trusted gateway prefix (default: nginx on PATH)")
	nodeName := flags.String("node-name", "", "optional operator label reported in the one-shot result")
	waitTaskMS := flags.Int("wait-task-ms", 0, "one-shot wait window for one TaskOffer after sampling (0..600000)")
	agentVersion := flags.String("agent-version", DefaultVersion, "Agent version reported during enrollment and connection")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "agent: unexpected positional arguments")
		return 2
	}
	if *fileRoot != "" && (!filepath.IsAbs(*fileRoot) || !(*serve || *connectOnce)) {
		return 2
	}
	if *gatewayPrefix != "" && (!filepath.IsAbs(*gatewayPrefix) || !(*serve || *connectOnce)) {
		return 2
	}
	if *nginxBinary != "" && *gatewayPrefix == "" {
		return 2
	}

	resolvedPin := strings.TrimSpace(*rootPin)
	if resolvedPin == "" && strings.TrimSpace(*rootCert) != "" {
		derived, err := pinFromCertificateFile(*rootCert)
		if err != nil {
			fmt.Fprintln(stderr, "agent: -root-cert is unusable")
			return 2
		}
		resolvedPin = derived
	}
	if !validPin(resolvedPin) {
		fmt.Fprintln(stderr, "agent: -root-pin must be the 64-hex local root SPKI SHA-256")
		return 2
	}
	if !filepath.IsAbs(*stateDir) {
		fmt.Fprintln(stderr, "agent: -state-dir must be an absolute path")
		return 2
	}
	if (*connectOnce || *serve) && strings.TrimSpace(*rootCert) == "" {
		fmt.Fprintln(stderr, "agent: -connect-once requires -root-cert")
		return 2
	}

	modes := 0
	for _, active := range []bool{*statusFlag, *enrollToken != "", *connectOnce, *serve} {
		if active {
			modes++
		}
	}
	if modes > 1 {
		fmt.Fprintln(stderr, "agent: -status, -enroll-token, -connect-once and -serve are mutually exclusive")
		return 2
	}

	switch {
	case *serve:
		origin, ok := normalizeControllerURL(*controllerURL)
		if !ok || !validNodeName(strings.TrimSpace(*nodeName)) || *waitTaskMS != 0 {
			return 2
		}
		if deps.Serve == nil {
			fmt.Fprintln(stderr, "agent: persistent connection unavailable")
			return 1
		}
		if err := deps.Serve(ctx, ConnectRequest{ControllerURL: origin, StateDir: *stateDir, RootPin: resolvedPin, RootCertificate: *rootCert, NodeName: strings.TrimSpace(*nodeName), AgentVersion: *agentVersion, Persistent: true, FileRoot: *fileRoot, GatewayPrefix: *gatewayPrefix, NginxBinary: *nginxBinary}); err != nil {
			fmt.Fprintln(stderr, "agent: persistent connection stopped; reconnect budget exhausted or local state invalid")
			return 1
		}
		return 0
	case *statusFlag:
		if deps.Status == nil {
			fmt.Fprintln(stderr, "agent: status self-check is unavailable")
			return 1
		}
		status, err := deps.Status(ctx, *stateDir, resolvedPin)
		if err != nil {
			fmt.Fprintln(stderr, "agent: local identity is unavailable or invalid; no automatic replacement")
			return 1
		}
		writeJSON(stdout, status)
		return 0
	case *enrollToken != "":
		if deps.Enroll == nil {
			fmt.Fprintln(stderr, "agent: enrollment is unavailable")
			return 1
		}
		origin := strings.TrimSpace(*controllerURL)
		if !strings.HasPrefix(origin, "https://") || strings.TrimSpace(*rootCert) == "" {
			fmt.Fprintln(stderr, "agent: -enroll-token requires -controller-url https://... and -root-cert")
			return 2
		}
		result, err := deps.Enroll(ctx, EnrollRequest{
			ControllerURL:   origin,
			EnrollToken:     *enrollToken,
			StateDir:        *stateDir,
			RootPin:         resolvedPin,
			RootCertificate: *rootCert,
			AgentVersion:    *agentVersion,
		})
		if err != nil {
			fmt.Fprintln(stderr, "agent: enrollment failed or outcome is unconfirmed; no automatic retry")
			return 1
		}
		writeJSON(stdout, result)
		if result.State == "pending" {
			fmt.Fprintln(stderr, "agent: enrollment is pending administrator approval; re-run after approval")
		}
		return 0
	case *connectOnce:
		if deps.Connect == nil {
			fmt.Fprintln(stderr, "agent: one-shot connection is unavailable")
			return 1
		}
		origin, ok := normalizeControllerURL(*controllerURL)
		if !ok {
			fmt.Fprintln(stderr, "agent: -connect-once requires -controller-url https://... or wss://...")
			return 2
		}
		label := strings.TrimSpace(*nodeName)
		if !validNodeName(label) {
			fmt.Fprintln(stderr, "agent: -node-name must be a bounded UTF-8 label without control characters")
			return 2
		}
		if *waitTaskMS < 0 || *waitTaskMS > 600000 {
			fmt.Fprintln(stderr, "agent: -wait-task-ms must be between 0 and 600000")
			return 2
		}
		result, err := deps.Connect(ctx, ConnectRequest{
			FileRoot:        *fileRoot,
			ControllerURL:   origin,
			StateDir:        *stateDir,
			RootPin:         resolvedPin,
			RootCertificate: *rootCert,
			NodeName:        label,
			WaitTaskMillis:  *waitTaskMS,
			AgentVersion:    *agentVersion,
			GatewayPrefix:   *gatewayPrefix,
			NginxBinary:     *nginxBinary,
		})
		if err != nil {
			fmt.Fprintln(stderr, "agent: one-shot connection failed; no automatic retry")
			return 1
		}
		writeJSON(stdout, result)
		return 0
	default:
		fmt.Fprintln(stderr, "agent: specify -status, -enroll-token, -connect-once or -serve (see -h)")
		return 2
	}
}

// normalizeControllerURL accepts only an https/wss origin with a DNS hostname;
// the reviewed Agent client never trusts IP literals, credentials or paths.
func normalizeControllerURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" && u.Scheme != "wss" || u.User != nil || u.Host == "" || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" ||
		net.ParseIP(u.Hostname()) != nil || strings.ToLower(u.Hostname()) != u.Hostname() {
		return "", false
	}
	return "https://" + u.Host, true
}

func validNodeName(name string) bool {
	if name == "" {
		return true
	}
	if len(name) > 128 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func writeJSON(w io.Writer, value any) {
	encoder := json.NewEncoder(w)
	_ = encoder.Encode(value)
}

func validPin(pin string) bool {
	if len(pin) != 64 {
		return false
	}
	raw, err := hex.DecodeString(pin)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == pin
}

func pinFromCertificateFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 {
		return "", errRuntime
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 64<<10 {
		return "", errRuntime
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return "", errRuntime
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid {
		return "", errRuntime
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return "", errRuntime
	}
	sum := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}
