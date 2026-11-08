// Package bootstrap owns the Controller CLI option parsing and the default
// self-contained composition used by apps/controller/main.go.
package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/updates"
)

// Options are the Controller CLI flags.
type Options struct {
	ShowVersion           bool
	WebDirectory          string
	Listen                string
	DBPath                string
	Origin                string
	TLSCert               string
	TLSKey                string
	AgentPKIDir           string
	AgentPKIMode          string
	AgentRootPin          string
	AgentAuthority        string
	AgentServerName       string
	FileDeployment        string
	GatewayDeployment     string
	UpdateDeployment      string
	CredentialKeyDir      string
	DisableCleanupRetries bool
}

// ParseOptions parses Controller flags. It returns flag.ErrHelp for -h.
func ParseOptions(args []string, output io.Writer) (Options, error) {
	flags := flag.NewFlagSet("controller", flag.ContinueOnError)
	flags.SetOutput(output)
	var options Options
	flags.BoolVar(&options.ShowVersion, "version", false, "print the build version and exit")
	flags.StringVar(&options.Listen, "listen", "127.0.0.1:8080", "local listen address")
	flags.StringVar(&options.DBPath, "db", "", "SQLite WAL database path (enables management API)")
	flags.StringVar(&options.Origin, "origin", "", "trusted https:// browser Origin (required with -db)")
	flags.StringVar(&options.TLSCert, "tls-cert", "", "TLS server certificate path (required with -db)")
	flags.StringVar(&options.TLSKey, "tls-key", "", "TLS server private key path (required with -db)")
	flags.StringVar(&options.AgentPKIMode, "agent-pki-mode", "provisioned", "provisioned offline-Root/online-Intermediate PKI, or explicit local-development")
	flags.StringVar(&options.AgentRootPin, "agent-root-pin", "", "offline-approved Agent Root SPKI SHA-256 (required in provisioned mode)")
	flags.StringVar(&options.AgentPKIDir, "agent-pki-dir", "", "local Agent CA directory (default: pki beside -db)")
	flags.StringVar(&options.AgentAuthority, "agent-authority", "", "Agent-facing authority host:port (default: derived from -listen)")
	flags.StringVar(&options.AgentServerName, "agent-server-name", "", "Agent TLS server name (default: host of -agent-authority)")
	flags.StringVar(&options.CredentialKeyDir, "credential-key-dir", "", "private credential key directory (default: credential-keys beside -db)")
	flags.StringVar(&options.WebDirectory, "web-dir", "", "private built console directory (requires management HTTPS mode)")
	flags.StringVar(&options.FileDeployment, "file-deployment", "", "private startup node-root registry (Linux; requires management TLS mode)")
	flags.StringVar(&options.GatewayDeployment, "gateway-deployment", "", "private startup gateway nginx/certificate path registry (requires -file-deployment)")
	flags.StringVar(&options.UpdateDeployment, "update-deployment", "", "private signed local Agent update configuration (Linux; requires management TLS mode)")
	flags.BoolVar(&options.DisableCleanupRetries, "disable-cleanup-retries", false, "disable automatic retries of owned ACME cleanup records")
	if err := flags.Parse(args); err != nil {
		return Options{}, err
	}
	if flags.NArg() != 0 {
		return Options{}, errors.New("controller: unexpected positional arguments")
	}
	return options, nil
}

// Scaffold reports whether the M0-P1 health scaffold mode was requested.
func (o Options) Scaffold() bool { return o.DBPath == "" }

// Validate enforces the management-mode prerequisites without opening storage.
func (o Options) Validate() error {
	if o.WebDirectory != "" && o.Scaffold() {
		return errors.New("controller: -web-dir requires -db and direct HTTPS")
	}
	if o.UpdateDeployment != "" && o.Scaffold() {
		return errors.New("controller: -update-deployment requires -db and direct HTTPS")
	}
	if o.FileDeployment != "" && o.Scaffold() {
		return errors.New("controller: -file-deployment requires -db and direct HTTPS")
	}
	if o.GatewayDeployment != "" && o.FileDeployment == "" {
		return errors.New("controller: -gateway-deployment requires -file-deployment")
	}
	if o.Scaffold() {
		return nil
	}
	mode := o.AgentPKIMode
	if mode == "" {
		mode = "provisioned"
	}
	if mode != "provisioned" && mode != "local-development" {
		return errors.New("controller: unsupported Agent PKI mode")
	}
	if mode == "provisioned" && len(o.AgentRootPin) != 64 {
		return errors.New("controller: provisioned PKI requires an independently verified -agent-root-pin; local development must be explicit")
	}
	if o.Origin == "" || o.TLSCert == "" || o.TLSKey == "" {
		return errors.New("controller: -db requires -origin, -tls-cert and -tls-key (direct HTTPS is mandatory)")
	}
	return nil
}

// LocalPKIDirectory resolves the default local CA directory beside the database.
func (o Options) LocalPKIDirectory() (string, error) {
	if strings.TrimSpace(o.AgentPKIDir) != "" {
		return o.AgentPKIDir, nil
	}
	if o.DBPath == "" {
		return "", errors.New("controller: local PKI requires -db")
	}
	absolute, err := filepath.Abs(o.DBPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(absolute), "pki"), nil
}

// DefaultAgentAuthority derives the Agent-facing authority from the listen
// address. Loopback IP listeners map to localhost so the reviewed Agent client,
// which rejects IP literals, can use the local default.
func DefaultAgentAuthority(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "", errors.New("controller: -listen must be host:port")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		name, err := os.Hostname()
		if err != nil || name == "" {
			return "", errors.New("controller: cannot derive agent authority; set -agent-authority")
		}
		return net.JoinHostPort(name, port), nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return net.JoinHostPort("localhost", port), nil
	}
	return net.JoinHostPort(host, port), nil
}

// Compose opens the management server with the default local Agent PKI. A
// scaffold request returns (nil, nil, nil).
func Compose(ctx context.Context, options Options) (*server.Server, *localpki.Material, error) {
	if err := options.Validate(); err != nil {
		return nil, nil, err
	}
	if options.Scaffold() {
		return nil, nil, nil
	}
	var fileConfig *server.FileExecutionConfig
	if options.FileDeployment != "" {
		var err error
		fileConfig, err = server.LoadFileExecutionConfig(options.FileDeployment)
		if err != nil {
			return nil, nil, err
		}
	}
	var gatewayDeployment *server.GatewayDeployment
	if options.GatewayDeployment != "" {
		var err error
		gatewayDeployment, err = server.LoadGatewayDeployment(options.GatewayDeployment)
		if err != nil {
			return nil, nil, err
		}
	}
	var updateDeployment *updates.ProductDeploymentOptions
	if options.UpdateDeployment != "" {
		var err error
		updateDeployment, err = server.LoadUpdateDeployment(options.UpdateDeployment)
		if err != nil {
			return nil, nil, err
		}
	}
	directory, err := options.LocalPKIDirectory()
	if err != nil {
		return nil, nil, err
	}
	var material *localpki.Material
	if options.AgentPKIMode == "local-development" {
		material, err = localpki.Ensure(directory)
	} else {
		material, err = localpki.LoadProvisioned(directory, options.AgentRootPin)
	}
	if err != nil {
		return nil, nil, err
	}
	authority := options.AgentAuthority
	if strings.TrimSpace(authority) == "" {
		authority, err = DefaultAgentAuthority(options.Listen)
		if err != nil {
			return nil, nil, err
		}
	}
	serverName := options.AgentServerName
	if strings.TrimSpace(serverName) == "" {
		host, _, splitErr := net.SplitHostPort(authority)
		if splitErr != nil || host == "" {
			return nil, nil, errors.New("controller: -agent-authority must be host:port")
		}
		serverName = host
	}
	composed, err := server.New(ctx, server.Config{
		FileExecution:         fileConfig,
		WebDirectory:          options.WebDirectory,
		GatewayDeployment:     gatewayDeployment,
		UpdateProduct:         updateDeployment,
		DisableCleanupRetries: options.DisableCleanupRetries,
		DBPath:                options.DBPath,
		CredentialKeyDir:      options.CredentialKeyDir,
		Origin:                options.Origin,
		EnrollmentIssuer:      material.Issuer,
		AgentAuthority:        authority,
		AgentServerName:       serverName,
	})
	if err != nil {
		return nil, nil, err
	}
	return composed, material, nil
}

// TLSConfig loads the management TLS identity and trusts the local Agent CA for
// optional client certificates. Browser sessions remain certificate-free.
func TLSConfig(options Options, material *localpki.Material) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(options.TLSCert, options.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("controller: load TLS certificate: %w", err)
	}
	config := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	}
	if material != nil {
		config.ClientCAs = material.Roots
		config.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return config, nil
}
