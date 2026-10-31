// Package runtime owns the optional Agent CLI presentation for status self-check
// and one explicit enrollment attempt. It never loops, retries, executes tasks or
// replaces an existing identity.
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
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Deps binds the reviewed local identity store and enrollment client.
type Deps struct {
	Status func(ctx context.Context, stateDir, rootPin string) (Status, error)
	Enroll func(ctx context.Context, request EnrollRequest) (EnrollResult, error)
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
	controllerURL := flags.String("controller-url", "", "Controller https origin, e.g. https://controller.example")
	enrollToken := flags.String("enroll-token", "", "one-time enrollment token")
	agentVersion := flags.String("agent-version", DefaultVersion, "Agent version reported during enrollment")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "agent: unexpected positional arguments")
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

	switch {
	case *statusFlag && *enrollToken != "":
		fmt.Fprintln(stderr, "agent: -status and -enroll-token are mutually exclusive")
		return 2
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
	default:
		fmt.Fprintln(stderr, "agent: specify -status or -enroll-token (see -h)")
		return 2
	}
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
