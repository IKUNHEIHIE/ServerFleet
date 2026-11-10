package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
)

var errInstallation = errors.New("installation incomplete: existing identity/runtime retained; inspect install-state.json and resume the SAME configuration")

type installationSource struct {
	Origin          string   `json:"origin"`
	Addresses       []string `json:"addresses"`
	RootCertificate string   `json:"rootCertificate,omitempty"`
	RootPEM         string   `json:"rootPem,omitempty"`
	Version         string   `json:"version"`
	KeyID           string   `json:"keyId"`
	PublicKey       string   `json:"publicKey"`
	MinimumSequence uint64   `json:"minimumSequence"`
}
type installationDocument struct {
	Format              int                `json:"format"`
	Directory           string             `json:"directory"`
	Source              installationSource `json:"source"`
	Provision           provisionDocument  `json:"provision"`
	HTTPSRootPEM        string             `json:"httpsRootPem,omitempty"`
	IdentityRootPEM     string             `json:"identityRootPem,omitempty"`
	EnrollmentTokenFile string             `json:"enrollmentTokenFile,omitempty"`
	ServiceMode         string             `json:"serviceMode"`
	WaitSeconds         int                `json:"waitSeconds"`
}
type installationState struct {
	Format           int       `json:"format"`
	ConfigSHA256     string    `json:"configSha256"`
	Phase            string    `json:"phase"`
	ManifestSHA256   string    `json:"manifestSha256,omitempty"`
	NodeID           string    `json:"nodeId,omitempty"`
	ServiceInstalled bool      `json:"serviceInstalled"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func installationDirExists(root *os.Root, name string) bool {
	i, e := root.Lstat(name)
	return e == nil && i.IsDir() && i.Mode().Perm() == 0700 && i.Mode()&os.ModeSymlink == 0
}
func installationWrite(root *os.Root, name string, raw []byte) error {
	if _, e := root.Lstat(name + ".next"); e == nil {
		return errInstallation
	}
	if e := provisionWrite(root, name+".next", raw, 0600); e != nil {
		return e
	}
	if e := root.Rename(name+".next", name); e != nil {
		return e
	}
	return syncProvisionDirectory(root, ".")
}
func installState(root *os.Root, state *installationState, phase string) error {
	state.Phase = phase
	state.UpdatedAt = time.Now().UTC()
	b, _ := json.Marshal(state)
	return installationWrite(root, "install-state.json", b)
}
func installationReadConfig(filename string) (installationDocument, []byte, error) {
	var d installationDocument
	raw, e := recovery.ReadPrivateFile(filename, 128<<10)
	if e != nil || strictProvisionJSON(raw, &d) != nil || d.Format != 1 || !filepath.IsAbs(d.Directory) || filepath.Clean(d.Directory) != d.Directory || !regexpProvisionPath(d.Directory) || d.Provision.Component != "controller" && d.Provision.Component != "agent" || d.ServiceMode != "user" && d.ServiceMode != "external" || d.WaitSeconds < 10 || d.WaitSeconds > 600 || !transport.FileDigest(d.Provision.RootPin) {
		return d, nil, errInstallation
	}
	if d.Source.MinimumSequence == 0 || !transport.FileID(d.Source.KeyID) || len(d.Source.Addresses) < 1 || len(d.Source.Addresses) > 8 {
		return d, nil, errInstallation
	}
	u, e := url.Parse(d.Source.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || d.Source.Origin != "https://"+u.Host {
		return d, nil, errInstallation
	}
	key, e := base64.StdEncoding.Strict().DecodeString(d.Source.PublicKey)
	if e != nil || len(key) != ed25519.PublicKeySize || !transport.FileID(d.Source.Version) {
		return d, nil, errInstallation
	}
	if d.Provision.Directory != "" || d.Provision.Artifact != "" || d.Provision.Manifest != "" || d.Provision.SupervisorExecutable != "" || d.Provision.Feed != nil {
		return d, nil, errInstallation
	}
	if d.Provision.Component == "agent" && (d.Provision.Agent == nil || d.Provision.Agent.IdentityDir != "" || d.Provision.Agent.NodeID != "") {
		return d, nil, errInstallation
	}
	return d, raw, nil
}
func regexpProvisionPath(p string) bool {
	if len(p) > 512 || p == "/" {
		return false
	}
	for _, r := range p {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_.-", r)) {
			return false
		}
	}
	return !strings.Contains(p, "/../")
}

// RunInstallCommand is a resumable ordinary-user installer. Its root wrapper
// only creates a dedicated account and systemd unit; all product state is owned
// by that ordinary user. No root helper/sudoers/firewall/production API is added.
func RunInstallCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controller install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "private installation JSON")
	systemUser := flags.String("system-user", "", "explicit root-only dedicated service account wrapper")
	verifyHealth := flags.Bool("verify-health", false, "finish an externally started service using fresh product readiness")
	checkOnly := flags.Bool("check", false, "preflight inputs and environment without download or installation")
	if flags.Parse(args) != nil || flags.NArg() != 0 || ctx == nil || ctx.Err() != nil {
		return errInstallation
	}
	d, raw, e := installationReadConfig(*config)
	if e != nil {
		return e
	}
	if *systemUser != "" {
		return runSystemInstallation(ctx, *config, *systemUser, *checkOnly, stdout, stderr)
	}
	if !provisionOwnerSupported() || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return fmt.Errorf("installation requires Linux amd64/arm64 and an ordinary user (root: use -system-user serverfleet)")
	}
	if d.ServiceMode == "user" {
		if e = installationServicePreflight(ctx); e != nil {
			return e
		}
	}
	if *checkOnly {
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "PREFLIGHT_OK", "architecture": runtime.GOARCH, "component": d.Provision.Component, "changesMade": false})
	}
	root, e := openProvisionPrivateDirectory(d.Directory)
	if os.IsNotExist(e) {
		root, e = newProvisionDirectory(d.Directory)
	}
	if e != nil {
		return e
	}
	defer root.Close()
	unlock, e := installationLock(root)
	if e != nil {
		return e
	}
	defer unlock()
	state := installationState{Format: 1, ConfigSHA256: recoveryDigest(raw), Phase: "new"}
	if _, e := root.Lstat("install-state.json"); e == nil {
		b, e := recovery.ReadPrivateFile(filepath.Join(d.Directory, "install-state.json"), 8192)
		if e != nil || strictProvisionJSON(b, &state) != nil || state.ConfigSHA256 != recoveryDigest(raw) || state.Format != 1 {
			return errInstallation
		}
	} else if !os.IsNotExist(e) {
		return e
	}

	if *verifyHealth {
		if d.ServiceMode != "external" || state.ManifestSHA256 == "" {
			return errInstallation
		}
		if e = installationSystemctl(ctx, false, "is-active", "serverfleet-"+d.Provision.Component+".service"); e != nil {
			return e
		}
		if e = installationWaitHealth(ctx, filepath.Join(d.Directory, "runtime"), d.Source.Version, state.NodeID, time.Duration(d.WaitSeconds)*time.Second); e != nil {
			return e
		}
		state.ServiceInstalled = true
		if e = installState(root, &state, "healthy"); e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(installationResult(d, state, d.Source.Version))
	}
	if e = installState(root, &state, state.Phase); e != nil {
		return e
	}
	provision := d.Provision
	provision.Format = 1
	provision.Directory = filepath.Join(d.Directory, "runtime")
	if d.HTTPSRootPEM != "" {
		if e = installationEnsureFile(root, "https-ca.pem", []byte(d.HTTPSRootPEM)); e != nil {
			return e
		}
		provision.RootCertificate = filepath.Join(d.Directory, "https-ca.pem")
	}
	if d.IdentityRootPEM != "" {
		if e = installationEnsureFile(root, "identity-root.pem", []byte(d.IdentityRootPEM)); e != nil {
			return e
		}
	}
	sourceCA := []byte(d.Source.RootPEM)
	if len(sourceCA) == 0 {
		sourceCA, e = recovery.ReadPrivateFile(d.Source.RootCertificate, 16384)
		if e != nil {
			return e
		}
	}
	client, e := updates.PinnedHTTPSClient(updates.HTTPReleaseFeed{Origin: d.Source.Origin, Addresses: d.Source.Addresses, RootPEM: sourceCA}, nil)
	if e != nil {
		return e
	}
	defer client.CloseIdleConnections()
	prefix := "/" + d.Source.Version + "/" + runtime.GOARCH + "/"
	get := func(name string, max int64) ([]byte, error) {
		return installationDownload(ctx, client, d.Source.Origin+prefix+name, max)
	}
	manifestRaw, e := get("installation.manifest.json", 256<<10)
	if e != nil {
		return e
	}
	signature, e := get("installation.signature", ed25519.SignatureSize)
	if e != nil {
		return e
	}
	key, _ := base64.StdEncoding.Strict().DecodeString(d.Source.PublicKey)
	var manifest installationManifest
	if strictProvisionJSON(manifestRaw, &manifest) != nil || !validInstallationManifest(manifest, runtime.GOARCH) || manifest.Version != d.Source.Version || manifest.Sequence < d.Source.MinimumSequence || !ed25519.Verify(ed25519.PublicKey(key), append([]byte(installationSignatureDomain), manifestRaw...), signature) {
		return errors.New("installation signature/version/platform rejected; no executable was run")
	}
	digest := recoveryDigest(manifestRaw)
	if state.ManifestSHA256 != "" && state.ManifestSHA256 != digest {
		return errInstallation
	}
	state.ManifestSHA256 = digest
	if e = installationEnsureFile(root, "installation.manifest.json", manifestRaw); e != nil {
		return e
	}
	if e = root.Mkdir("release", 0700); e != nil && !installationDirExists(root, "release") {
		return e
	}
	release, e := root.OpenRoot("release")
	if e != nil {
		return e
	}
	defer release.Close()
	for _, file := range manifest.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if strings.Contains(file.Path, "/") {
			parts := strings.Split(path.Dir(file.Path), "/")
			acc := ""
			for _, part := range parts {
				acc = path.Join(acc, part)
				if e = release.Mkdir(acc, 0700); e != nil && !installationDirExists(release, acc) {
					return e
				}
			}
		}
		filename := filepath.Join(d.Directory, "release", file.Path)
		if info, err := release.Lstat(file.Path); err == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != file.Bytes {
				return errInstallation
			}
			if e = installationVerifyFile(filename, file); e != nil {
				return e
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if e = installationDownloadFile(ctx, client, d.Source.Origin+prefix+file.Path, release, file); e != nil {
			return e
		}
	}
	provision.Artifact = filepath.Join(d.Directory, "release", provision.Component)
	provision.Manifest = provision.Artifact + ".manifest.json"
	provision.SupervisorExecutable = filepath.Join(d.Directory, "release/controller")
	provision.ReleaseKeys = map[string]string{d.Source.KeyID: d.Source.PublicKey}
	if provision.Component == "agent" {
		identity := filepath.Join(d.Directory, "identity")
		provision.Agent.IdentityDir = identity
		if e = root.Mkdir("identity", 0700); e != nil && !installationDirExists(root, "identity") {
			return e
		}
		node, e := installationEnroll(ctx, d, provision, root, &state, stderr)
		if e != nil {
			return e
		}
		if node == "" {
			return json.NewEncoder(stdout).Encode(map[string]any{"state": "WAITING_FOR_NODE_APPROVAL", "directory": d.Directory, "resumeCommand": "controller install -config " + *config})
		}
		provision.Agent.NodeID = node
		state.NodeID = node
		if provision.Agent.FileRoot == "" {
			if e = root.Mkdir("files", 0700); e != nil && !installationDirExists(root, "files") {
				return e
			}
			provision.Agent.FileRoot = filepath.Join(d.Directory, "files")
		}
		if provision.Agent.CertificateRoot == "" {
			if e = root.Mkdir("website-keys", 0700); e != nil && !installationDirExists(root, "website-keys") {
				return e
			}
			provision.Agent.CertificateRoot = filepath.Join(d.Directory, "website-keys")
		}
	} else {
		// The web directory is always from the signed bundle, never a config override.
		for _, arg := range provision.ControllerArguments {
			if arg == "-web-dir" || arg == "--web-dir" || strings.HasPrefix(arg, "-web-dir=") {
				return errInstallation
			}
		}
		provision.ControllerArguments = append(provision.ControllerArguments, "-web-dir", filepath.Join(d.Directory, "release/web"))
	}
	provisionRaw, _ := json.Marshal(provision)
	if e = installationEnsureFile(root, "provision.json", provisionRaw); e != nil {
		return e
	}
	prepared := filepath.Join(provision.Directory, "prepared.json")
	if _, e = os.Lstat(prepared); os.IsNotExist(e) {
		if _, e = root.Lstat("runtime"); e == nil {
			return errors.New("partial runtime retained: inspect provision-attempt.json; installer refuses to overwrite it")
		}
		if e = installState(root, &state, "provisioning"); e != nil {
			return e
		}
		if e = RunProvisionCommand(ctx, []string{"-config", filepath.Join(d.Directory, "provision.json")}, io.Discard, stderr); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	if e = installState(root, &state, "prepared"); e != nil {
		return e
	}
	if d.ServiceMode == "external" {
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "PREPARED_SYSTEM_SERVICE_REQUIRED", "component": provision.Component, "directory": d.Directory, "runtime": provision.Directory})
	}
	if e = installationUserService(ctx, provision.Directory, provision.Component); e != nil {
		return e
	}
	state.ServiceInstalled = true
	if e = installState(root, &state, "checking_health"); e != nil {
		return e
	}
	if e = installationWaitHealth(ctx, provision.Directory, manifest.Version, state.NodeID, time.Duration(d.WaitSeconds)*time.Second); e != nil {
		return e
	}
	if e = installState(root, &state, "healthy"); e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(installationResult(d, state, manifest.Version))
}
func installationResult(d installationDocument, s installationState, version string) map[string]any {
	out := map[string]any{"state": "INSTALLED_HEALTHY", "component": d.Provision.Component, "directory": d.Directory, "version": version, "nodeId": s.NodeID, "serviceInstalled": true}
	if d.Provision.Component == "controller" {
		out["console"] = d.Provision.ControllerOrigin
		out["setupTokenFile"] = filepath.Join(d.Directory, "runtime/bootstrap-token.txt")
		out["setupTokenExpiresInSeconds"] = 600
	}
	return out
}
func installationEnsureFile(root *os.Root, name string, want []byte) error {
	i, e := root.Lstat(name)
	if e == nil {
		if !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || i.Size() != int64(len(want)) {
			return errInstallation
		}
		b, e := root.ReadFile(name)
		if e != nil || !bytes.Equal(b, want) {
			return errInstallation
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	return provisionWrite(root, name, want, 0600)
}

func installationDownload(ctx context.Context, client *http.Client, url string, max int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.GetBody = nil
	response, e := client.Do(req)
	if e != nil {
		return nil, errInstallation
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength > max || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Set-Cookie") != "" {
		return nil, errInstallation
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, errInstallation
	}
	return b, nil
}
func installationEnroll(ctx context.Context, d installationDocument, p provisionDocument, root *os.Root, state *installationState, stderr io.Writer) (string, error) {
	if d.EnrollmentTokenFile == "" {
		d.EnrollmentTokenFile = filepath.Join(d.Directory, "enroll-token.txt")
		if _, e := root.Lstat("enroll-token.txt"); os.IsNotExist(e) {
			token, e := installationReadToken()
			if e != nil {
				return "", e
			}
			defer clear(token)
			if e = provisionWrite(root, "enroll-token.txt", token, 0600); e != nil {
				return "", e
			}
		}
	}
	if d.IdentityRootPEM == "" || d.EnrollmentTokenFile == "" {
		return "", errors.New("agent installation requires pinned identity Root PEM and a private enrollment token file")
	}
	binary := p.Artifact
	// The Agent owns the persistent CSR/request ID. All approval polls refer to
	// that SAME request; no second identity or new token is generated on resume.
	call := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = installationEnvironment()
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		e := cmd.Run()
		if out.Len() > 16384 {
			return nil, errInstallation
		}
		return out.Bytes(), e
	}
	statusArgs := []string{"-status", "-state-dir", p.Agent.IdentityDir, "-root-pin", p.RootPin}
	if b, e := call(statusArgs...); e == nil {
		var status struct {
			Enrolled bool
			NodeID   string
		}
		if json.Unmarshal(b, &status) == nil && status.Enrolled && transport.FileID(status.NodeID) {
			return status.NodeID, nil
		}
	}
	if e := installState(root, state, "enrollment_pending"); e != nil {
		return "", e
	}
	until := time.Now().Add(time.Duration(d.WaitSeconds) * time.Second)
	for {
		b, e := call("-state-dir", p.Agent.IdentityDir, "-root-pin", p.RootPin, "-root-cert", filepath.Join(d.Directory, "identity-root.pem"), "-controller-ca", p.RootCertificate, "-controller-url", p.ControllerOrigin, "-enroll-token-file", d.EnrollmentTokenFile)
		if e != nil {
			return "", errors.New("enrollment outcome unconfirmed or denied: retain identity and token; inspect the controller before resuming")
		}
		var result struct {
			State     string    `json:"state"`
			NodeID    string    `json:"nodeId"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if json.Unmarshal(b, &result) != nil {
			return "", errInstallation
		}
		if result.State == "issued" && transport.FileID(result.NodeID) {
			return result.NodeID, nil
		}
		if result.State != "pending" || time.Now().After(result.ExpiresAt) {
			return "", errInstallation
		}
		if time.Now().After(until) {
			return "", nil
		}
		fmt.Fprintln(stderr, "Waiting for node approval in the Controller; the same enrollment request is retained.")
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
func installationWaitHealth(ctx context.Context, directory, version, node string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		raw, e := recovery.ReadPrivateFile(filepath.Join(directory, "health/ready.json"), 8192)
		var v struct {
			Version                     string
			PID                         int
			Nonce, NodeID, ConnectionID string
			ObservedAt                  time.Time
		}
		if e == nil && json.Unmarshal(raw, &v) == nil && v.Version == version && v.PID > 1 && len(v.Nonce) == 32 && !v.ObservedAt.After(time.Now()) && time.Since(v.ObservedAt) < 3*time.Second && (node == "" || v.NodeID == node && v.ConnectionID != "") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("service has not provided fresh product readiness; keep installation and inspect systemd logs; no reinstall performed")
}
