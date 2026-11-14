package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
)

var errProvision = errors.New("provision: invalid input or incomplete private installation; existing state is retained")

type provisionDocument struct {
	Format               int               `json:"format"`
	Component            string            `json:"component"`
	Directory            string            `json:"directory"`
	Artifact             string            `json:"artifact"`
	Manifest             string            `json:"manifest"`
	SupervisorExecutable string            `json:"supervisorExecutable"`
	RootCertificate      string            `json:"rootCertificate"`
	RootPin              string            `json:"rootPin"`
	ControllerOrigin     string            `json:"controllerOrigin"`
	ControllerAddresses  []string          `json:"controllerAddresses"`
	ReleaseKeys          map[string]string `json:"releaseKeys"`
	ControllerArguments  []string          `json:"controllerArguments,omitempty"`
	Agent                *provisionAgent   `json:"agent,omitempty"`
	Feed                 *provisionFeed    `json:"feed,omitempty"`
	FenceConfiguration   string            `json:"fenceConfiguration,omitempty"`
}
type provisionAgent struct {
	NodeID          string `json:"nodeId"`
	IdentityDir     string `json:"identityDir"`
	Name            string `json:"name,omitempty"`
	FileRoot        string `json:"fileRoot,omitempty"`
	DisableFiles    bool   `json:"disableFiles,omitempty"`
	CertificateRoot string `json:"certificateRoot,omitempty"`
	CertificateCA   string `json:"certificateCa,omitempty"`
	CommandProfiles string `json:"commandProfiles,omitempty"`
	RootHelper      string `json:"rootHelper,omitempty"`
	GatewayPrefix   string `json:"gatewayPrefix,omitempty"`
	NginxBinary     string `json:"nginxBinary,omitempty"`
}
type provisionFeed struct {
	Origin          string   `json:"origin"`
	Addresses       []string `json:"addresses"`
	RootCertificate string   `json:"rootCertificate"`
}

// RunProvisionCommand prepares a NEW ordinary-user installation only. It does
// not start artifacts, contact a network, sign a release, install a service,
// replace a database, grant execution or release any recovery quarantine.
func RunProvisionCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controller provision", flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "", "absolute private provisioning JSON; creates one new installation")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || !provisionOwnerSupported() {
		return errProvision
	}
	raw, e := recovery.ReadPrivateFile(*filename, 32<<10)
	var d provisionDocument
	if e != nil || strictProvisionJSON(raw, &d) != nil || d.Format != 1 || (d.Component != "agent" && d.Component != "controller") || !transport.FileDigest(d.RootPin) || !filepath.IsAbs(d.Directory) || filepath.Clean(d.Directory) != d.Directory || len(d.Directory) > 512 || !regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`).MatchString(d.Directory) || len(d.ReleaseKeys) < 1 || len(d.ReleaseKeys) > 16 {
		return errProvision
	}
	keys := map[string]ed25519.PublicKey{}
	for id, value := range d.ReleaseKeys {
		key, e := base64.StdEncoding.Strict().DecodeString(value)
		if !transport.FileID(id) || e != nil || len(key) != ed25519.PublicKeySize {
			return errProvision
		}
		keys[id] = key
	}
	ca, e := recovery.ReadPrivateFile(d.RootCertificate, 16<<10)
	if e != nil {
		return errProvision
	}
	block, rest := pem.Decode(bytes.TrimSpace(ca))
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return errProvision
	}
	anchor, e := x509.ParseCertificate(block.Bytes)
	if e != nil || !anchor.IsCA || !anchor.BasicConstraintsValid || anchor.KeyUsage&x509.KeyUsageCertSign == 0 || anchor.CheckSignatureFrom(anchor) != nil || time.Now().Before(anchor.NotBefore) || !time.Now().Before(anchor.NotAfter) {
		return errProvision
	}
	client, e := updates.PinnedHTTPSClient(updates.HTTPReleaseFeed{Origin: d.ControllerOrigin, Addresses: d.ControllerAddresses, RootPEM: ca}, nil)
	if e != nil {
		return errProvision
	}
	client.CloseIdleConnections() // validates configuration without I/O
	u, e := url.Parse(d.ControllerOrigin)
	if e != nil || net.ParseIP(u.Hostname()) != nil || u.Hostname() != strings.ToLower(u.Hostname()) {
		return errProvision
	}
	d.ControllerOrigin = strings.TrimSuffix(d.ControllerOrigin, "/")
	manifestRaw, e := recovery.ReadPrivateFile(d.Manifest, 16<<10)
	var initial updates.ReleaseManifest
	if e != nil || strictProvisionJSON(manifestRaw, &initial) != nil || initial.Format != 1 || initial.Component != d.Component || initial.OS != "linux" || initial.Architecture != runtime.GOARCH || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(initial.Version) || initial.Sequence == 0 || initial.Sequence > 1<<63-1 || !transport.FileDigest(initial.SHA256) || initial.Bytes < 1 || initial.Bytes > 256<<20 {
		return errProvision
	}
	artifact, e := openProvisionProgram(d.Artifact)
	if e != nil {
		return e
	}
	defer artifact.Close()
	supervisor, e := openProvisionProgram(d.SupervisorExecutable)
	if e != nil {
		return e
	}
	defer supervisor.Close()
	var options Options
	var material *localpki.Material
	generatedFiles := false
	if d.Component == "controller" {
		if d.Agent != nil || len(d.ControllerArguments) > 128 {
			return errProvision
		}
		for _, arg := range d.ControllerArguments {
			if arg == "--" {
				return errProvision
			}
		}
		options, e = ParseOptions(d.ControllerArguments, io.Discard)
		if e != nil || options.ShowVersion || options.DBPath != "" || options.ResidentSupervisor != "" || options.UpdateDeployment != "" || options.ReleaseSource != "" || options.AgentPKIMode != "provisioned" || options.WebDirectory == "" || options.CredentialKeyDir != "" {
			return errProvision
		}
		options.DBPath = filepath.Join(d.Directory, "controller.db")
		options.CredentialKeyDir = filepath.Join(d.Directory, "credential-keys")
		if options.FileDeployment == "" {
			generatedFiles = true
			options.FileDeployment = filepath.Join(d.Directory, "file-deployment.json")
		}
		if options.Origin != "" && options.Origin != d.ControllerOrigin || options.AgentRootPin != "" && options.AgentRootPin != d.RootPin {
			return errProvision
		}
		options.Origin, options.AgentRootPin = d.ControllerOrigin, d.RootPin
		if options.AgentAuthority == "" {
			port := u.Port()
			if port == "" {
				port = "443"
			}
			options.AgentAuthority = net.JoinHostPort(u.Hostname(), port)
		}
		if options.AgentServerName == "" {
			options.AgentServerName = u.Hostname()
		}
		if e = options.Validate(); e != nil {
			return e
		}
		pkiDir, e := options.LocalPKIDirectory()
		if e != nil {
			return e
		}
		material, e = localpki.LoadProvisioned(pkiDir, d.RootPin)
		if e != nil {
			return e
		}
		if _, e = TLSConfig(options, material); e != nil {
			return e
		}
	} else {
		a := d.Agent
		if len(d.ControllerArguments) != 0 || d.FenceConfiguration != "" || a == nil || !transport.FileID(a.NodeID) || !filepath.IsAbs(a.IdentityDir) || len(a.Name) > 128 || !utf8.ValidString(a.Name) {
			return errProvision
		}
		for _, r := range a.Name {
			if r < 0x20 || r == 0x7f {
				return errProvision
			}
		}
		for _, p := range []string{a.FileRoot, a.CertificateRoot, a.CertificateCA, a.CommandProfiles, a.RootHelper, a.GatewayPrefix, a.NginxBinary} {
			if p != "" && (!filepath.IsAbs(p) || filepath.Clean(p) != p) {
				return errProvision
			}
		}
		if a.DisableFiles && a.FileRoot != "" || a.CertificateCA != "" && a.CertificateRoot == "" || a.NginxBinary != "" && a.GatewayPrefix == "" || a.GatewayPrefix != "" && (a.FileRoot == "" || !provisionWithin(a.FileRoot, a.GatewayPrefix)) || a.CertificateRoot != "" && a.FileRoot != "" && (provisionWithin(a.FileRoot, a.CertificateRoot) || provisionWithin(a.CertificateRoot, a.FileRoot)) {
			return errProvision
		}
	}
	// Every failure after directory creation remains explicitly incomplete.
	// No cleanup or replay can overwrite old authority or uncertain effects.
	root, e := newProvisionDirectory(d.Directory)
	if e != nil {
		return e
	}
	defer root.Close()
	write := func(name string, v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return provisionWrite(root, name, b, 0600)
	}
	if e = write("provision-attempt.json", map[string]string{"state": "INCOMPLETE", "component": d.Component, "initialSha256": initial.SHA256}); e != nil {
		return e
	}
	for _, name := range []string{"slot", "mailbox", "inbox", "stage", "health"} {
		if e = root.Mkdir(name, 0700); e != nil {
			return e
		}
	}
	if e = copyProvisionProgram(root, "slot/active", artifact, initial.SHA256, initial.Bytes); e != nil {
		return e
	}
	if e = copyProvisionProgram(root, "supervisor", supervisor, "", 0); e != nil {
		return e
	}
	if e = provisionWrite(root, "https-ca.pem", ca, 0600); e != nil {
		return e
	}
	if generatedFiles {
		if e = root.Mkdir("file-spool", 0700); e != nil {
			return e
		}
		if e = write("file-deployment.json", map[string]any{"version": 1, "allowEmpty": true, "spoolDir": filepath.Join(d.Directory, "file-spool"), "nodeRoots": []map[string]string{}}); e != nil {
			return e
		}
	}
	c := updates.ResidentSupervisorConfig{Format: 1, Component: d.Component, Directory: filepath.Join(d.Directory, "slot"), Mailbox: filepath.Join(d.Directory, "mailbox"), Inbox: filepath.Join(d.Directory, "inbox"), StageDir: filepath.Join(d.Directory, "stage"), HealthFile: filepath.Join(d.Directory, "health/ready.json"), Initial: initial, Policy: updates.ReleasePolicy{Component: d.Component, Architecture: runtime.GOARCH, MinimumSequence: initial.Sequence, MaxBytes: 256 << 20, TrustedKeys: keys}, ControllerOrigin: d.ControllerOrigin, ControllerAddresses: d.ControllerAddresses, RootCertificate: filepath.Join(d.Directory, "https-ca.pem"), RootCertificateSHA256: recoveryDigest(ca), RootPin: d.RootPin}
	if d.FenceConfiguration != "" {
		if _, e = loadFenceServer(d.FenceConfiguration); e != nil {
			return e
		}
		b, e := recovery.ReadPrivateFile(d.FenceConfiguration, 32<<10)
		if e != nil {
			return e
		}
		c.FenceConfiguration, c.FenceConfigurationSHA256 = d.FenceConfiguration, recoveryDigest(b)
	}
	if d.Feed != nil {
		b, e := recovery.ReadPrivateFile(d.Feed.RootCertificate, 128<<10)
		if e != nil {
			return e
		}
		feed := updates.HTTPReleaseFeed{Origin: strings.TrimSuffix(d.Feed.Origin, "/"), Addresses: d.Feed.Addresses, RootPEM: b}
		client, e := updates.PinnedHTTPSClient(feed, nil)
		if e != nil {
			return e
		}
		client.CloseIdleConnections()
		if e = provisionWrite(root, "feed-ca.pem", b, 0600); e != nil {
			return e
		}
		c.Feed, c.FeedRootCertificate, c.FeedRootSHA256 = &feed, filepath.Join(d.Directory, "feed-ca.pem"), recoveryDigest(b)
	}
	configFile := filepath.Join(d.Directory, "supervisor.json")
	if d.Component == "controller" {
		options.BuildVersion = initial.Version
		composed, _, e := Compose(ctx, options)
		if e != nil {
			return e
		}
		token, tokenErr := composed.Identity.PrepareSetup(ctx)
		if tokenErr == nil {
			tokenErr = provisionWrite(root, "bootstrap-token.txt", []byte(token+"\n"), 0600)
		}
		if e = composed.Close(); e != nil {
			return e
		}
		if tokenErr != nil {
			return tokenErr
		}
		db, e := storage.Open(ctx, storage.Config{Path: options.DBPath, ReadOnly: true})
		if e != nil {
			return e
		}
		c.SchemaSHA256, e = updates.SQLiteSchemaFingerprint(ctx, db)
		ce := db.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		c.NodeID, c.Database = material.AuthorityID, options.DBPath
		c.Arguments = append(append([]string{}, d.ControllerArguments...), "-db", options.DBPath, "-credential-key-dir", options.CredentialKeyDir, "-origin", options.Origin, "-agent-root-pin", options.AgentRootPin, "-agent-authority", options.AgentAuthority, "-agent-server-name", options.AgentServerName, "-file-deployment", options.FileDeployment, "-resident-supervisor", configFile)
	} else {
		a := d.Agent
		c.NodeID, c.IdentityDir = a.NodeID, a.IdentityDir
		identity, e := supervisorNodeCertificate(c)
		if e != nil {
			return e
		}
		if e = provisionWrite(root, "agent-root.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: identity.Certificate[len(identity.Certificate)-1]}), 0600); e != nil {
			return e
		}
		mailboxFile := filepath.Join(d.Directory, "agent-mailbox.json")
		// Version comes from each signed image, not a frozen old CLI override.
		c.Arguments = []string{"-serve", "-state-dir", a.IdentityDir, "-root-pin", d.RootPin, "-root-cert", filepath.Join(d.Directory, "agent-root.pem"), "-controller-ca", c.RootCertificate, "-controller-url", d.ControllerOrigin, "-update-mailbox", mailboxFile}
		for _, p := range [][2]string{{"-node-name", a.Name}, {"-file-root", a.FileRoot}, {"-certificate-root", a.CertificateRoot}, {"-certificate-ca", a.CertificateCA}, {"-command-profiles", a.CommandProfiles}, {"-root-helper", a.RootHelper}, {"-gateway-prefix", a.GatewayPrefix}, {"-nginx-binary", a.NginxBinary}} {
			if p[1] != "" {
				c.Arguments = append(c.Arguments, p[0], p[1])
			}
		}
		if e = write("agent-mailbox.json", map[string]any{"format": 1, "nodeId": c.NodeID, "directory": c.Mailbox, "policySha256": updates.ResidentPolicySHA(c)}); e != nil {
			return e
		}
	}
	if e = write("supervisor.json", c); e != nil {
		return e
	}
	if _, e = updates.LoadResidentSupervisor(configFile); e != nil {
		return e
	}
	launch := "#!/bin/sh\nset -eu\numask 077\nexec " + filepath.Join(d.Directory, "supervisor") + " supervise -config " + configFile + "\n"
	if e = provisionWrite(root, "start", []byte(launch), 0700); e != nil {
		return e
	}
	unit := "[Unit]\nDescription=Server Fleet " + d.Component + " owned supervisor\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nWorkingDirectory=" + d.Directory + "\nExecStart=" + filepath.Join(d.Directory, "start") + "\nUMask=0077\nRestart=no\nTimeoutStopSec=15\n\n[Install]\nWantedBy=default.target\n"
	if e = provisionWrite(root, "serverfleet-"+d.Component+".service", []byte(unit), 0600); e != nil {
		return e
	}
	result := map[string]any{"state": "PREPARED_NOT_STARTED", "component": d.Component, "nodeId": c.NodeID, "directory": d.Directory, "policySha256": updates.ResidentPolicySHA(c), "version": initial.Version, "sequence": initial.Sequence, "start": filepath.Join(d.Directory, "start"), "serviceInstalled": false, "releaseSigned": false}
	if d.Component == "controller" {
		result["setupTokenFile"] = filepath.Join(d.Directory, "bootstrap-token.txt")
		result["setupTokenExpiresInSeconds"] = 600
	}
	if e = write("prepared.json", result); e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(result)
}

func provisionWithin(parent, child string) bool {
	r, e := filepath.Rel(parent, child)
	return e == nil && !filepath.IsAbs(r) && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
func strictProvisionJSON(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var unique func() error
	unique = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim != '{' && delim != '[' {
			return errProvision
		}
		keys := map[string]bool{}
		for d.More() {
			if delim == '{' {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok {
					return errProvision
				}
				s = strings.ToLower(s)
				if keys[s] {
					return errProvision
				}
				keys[s] = true
			}
			if e := unique(); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	if unique() != nil {
		return errProvision
	}
	if _, e := d.Token(); e != io.EOF {
		return errProvision
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(&struct{}{}) != io.EOF {
		return errProvision
	}
	return nil
}
