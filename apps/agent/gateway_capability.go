package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

const (
	gatewayApplyAction         = "gateway.nginx.site_apply"
	gatewayRemoveAction        = "gateway.nginx.site_remove"
	gatewayActionSchemaVersion = "1"
	gatewayMaxArtifactBytes    = 64 << 10
	gatewayMaxSiteBytes        = 64 << 10
)

var (
	errGatewayCapability = errors.New("agent: gateway capability rejected")
	errGatewaySiteExists = errors.New("agent: existing site requires approved takeover")
	errGatewayDigest     = errors.New("agent: artifact or site digest mismatch")
)

// gatewayCommandRunner is injectable for tests. Production uses the fixed argv
// below; no caller-supplied command text is accepted.
type gatewayCommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execGatewayRunner struct{}

func (execGatewayRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type gatewayCapabilityConfig struct {
	Prefix      string // private nginx prefix root
	SiteDir     string // absolute directory containing managed site files
	ArtifactDir string // absolute staging directory for artifact files
	NginxBinary string // defaults to nginx
	MainConfig  string // absolute main nginx.conf under Prefix
}

type gatewayCapability struct {
	prefix      string
	siteDir     string
	artifactDir string
	nginxBinary string
	mainConfig  string
	runner      gatewayCommandRunner
}

type gatewayParams struct {
	Operation        string
	SitePath         string
	ArtifactPath     string
	ConfigSHA256     string
	TakeoverApproved bool
	ApprovalRef      string
}

// newGatewayCapability validates the operator-owned roots. A missing/invalid
// prefix keeps the capability closed; no default /etc/nginx path is assumed.
func newGatewayCapability(config gatewayCapabilityConfig, runner gatewayCommandRunner) (*gatewayCapability, error) {
	if runner == nil {
		return nil, errGatewayCapability
	}
	for _, path := range []string{config.Prefix, config.SiteDir, config.ArtifactDir, config.MainConfig} {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errGatewayCapability
		}
	}
	if !withinRoot(config.Prefix, config.SiteDir) || !withinRoot(config.Prefix, config.ArtifactDir) || !withinRoot(config.Prefix, config.MainConfig) {
		return nil, errGatewayCapability
	}
	if err := rejectSymlinkPath(config.Prefix); err != nil {
		return nil, errGatewayCapability
	}
	if info, err := os.Lstat(config.SiteDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errGatewayCapability
	}
	if info, err := os.Lstat(config.ArtifactDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errGatewayCapability
	}
	binary := config.NginxBinary
	if binary == "" {
		binary = "nginx"
	}
	if strings.ContainsAny(binary, " \t\r\n;|&") {
		return nil, errGatewayCapability
	}
	return &gatewayCapability{
		prefix: config.Prefix, siteDir: config.SiteDir, artifactDir: config.ArtifactDir,
		nginxBinary: binary, mainConfig: config.MainConfig, runner: runner,
	}, nil
}

// Actions returns the two typed agent actions for registration by the Agent
// composition. The action IDs are fixed; the schema version is fixed.
func (c *gatewayCapability) Actions() map[string]agentAction {
	if c == nil {
		return nil
	}
	return map[string]agentAction{
		gatewayApplyAction: {Validate: func(raw []byte) error {
			p, err := c.parse(raw, gatewayApplyAction)
			if err == nil && p.Operation != "apply" {
				return errGatewayCapability
			}
			return err
		}, Run: c.runApply},
		gatewayRemoveAction: {Validate: func(raw []byte) error {
			p, err := c.parse(raw, gatewayRemoveAction)
			if err == nil && p.Operation != "remove" {
				return errGatewayCapability
			}
			return err
		}, Run: c.runRemove},
	}
}

func (c *gatewayCapability) parse(raw []byte, action string) (gatewayParams, error) {
	if c == nil || len(raw) == 0 || len(raw) > 4096 {
		return gatewayParams{}, errGatewayCapability
	}
	var values map[string]string
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(&values); err != nil || values == nil {
		return gatewayParams{}, errGatewayCapability
	}
	if _, err := decoder.Token(); err != io.EOF {
		return gatewayParams{}, errGatewayCapability
	}
	allowed := map[string]bool{"operation": true, "site_path": true, "config_sha256": true}
	if action == gatewayApplyAction {
		allowed["artifact_path"] = true
		allowed["takeover_approved"] = true
		allowed["approval_ref"] = true
	}
	for key := range values {
		if !allowed[key] {
			return gatewayParams{}, errGatewayCapability
		}
	}
	p := gatewayParams{
		Operation: values["operation"], SitePath: values["site_path"],
		ArtifactPath: values["artifact_path"], ConfigSHA256: values["config_sha256"],
		ApprovalRef: values["approval_ref"],
	}
	if action == gatewayApplyAction {
		parsed, err := strconv.ParseBool(values["takeover_approved"])
		if err != nil || (values["takeover_approved"] != "true" && values["takeover_approved"] != "false") {
			return gatewayParams{}, errGatewayCapability
		}
		p.TakeoverApproved = parsed
	}
	if (action == gatewayApplyAction && p.Operation != "apply") || (action == gatewayRemoveAction && p.Operation != "remove") {
		return gatewayParams{}, errGatewayCapability
	}
	if !filepath.IsAbs(p.SitePath) || !withinRoot(c.siteDir, p.SitePath) || filepath.Clean(p.SitePath) != p.SitePath {
		return gatewayParams{}, errGatewayCapability
	}
	if action == gatewayApplyAction {
		if !filepath.IsAbs(p.ArtifactPath) || !withinRoot(c.artifactDir, p.ArtifactPath) || filepath.Clean(p.ArtifactPath) != p.ArtifactPath {
			return gatewayParams{}, errGatewayCapability
		}
	} else if p.ArtifactPath != "" {
		return gatewayParams{}, errGatewayCapability
	}
	if !validLowerHex(p.ConfigSHA256, 64) {
		return gatewayParams{}, errGatewayCapability
	}
	if p.ApprovalRef != "" && (len(p.ApprovalRef) > 128 || strings.TrimSpace(p.ApprovalRef) != p.ApprovalRef) {
		return gatewayParams{}, errGatewayCapability
	}
	return p, nil
}

func (c *gatewayCapability) runApply(ctx context.Context, raw []byte) executionResult {
	p, err := c.parse(raw, gatewayApplyAction)
	if err != nil || p.Operation != "apply" {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	}
	if err := c.apply(ctx, p); err != nil {
		if ctx.Err() != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	}
	return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
}

func (c *gatewayCapability) runRemove(ctx context.Context, raw []byte) executionResult {
	p, err := c.parse(raw, gatewayRemoveAction)
	if err != nil || p.Operation != "remove" {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	}
	if err := c.remove(ctx, p); err != nil {
		if ctx.Err() != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	}
	return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
}

func (c *gatewayCapability) apply(ctx context.Context, p gatewayParams) error {
	artifact, err := readBounded(p.ArtifactPath, gatewayMaxArtifactBytes)
	if err != nil {
		return errGatewayCapability
	}
	digest := sha256.Sum256(artifact)
	if hex.EncodeToString(digest[:]) != p.ConfigSHA256 {
		return errGatewayDigest
	}
	if err := rejectSymlinkPath(filepath.Dir(p.SitePath)); err != nil {
		return errGatewayCapability
	}
	existing, existingErr := readBounded(p.SitePath, gatewayMaxSiteBytes)
	switch {
	case existingErr == nil:
		existingDigest := sha256.Sum256(existing)
		if hex.EncodeToString(existingDigest[:]) == p.ConfigSHA256 {
			return nil
		}
		if !p.TakeoverApproved || p.ApprovalRef == "" {
			return errGatewaySiteExists
		}
	case os.IsNotExist(existingErr):
		existing = nil
	default:
		return errGatewayCapability
	}
	if err := writeAtomic(p.SitePath, artifact); err != nil {
		return err
	}
	if err := c.validateAndReload(ctx); err != nil {
		restoreErr := c.restore(ctx, p.SitePath, existing)
		if restoreErr != nil {
			return errGatewayCapability
		}
		return err
	}
	return nil
}

func (c *gatewayCapability) remove(ctx context.Context, p gatewayParams) error {
	if err := rejectSymlinkPath(filepath.Dir(p.SitePath)); err != nil {
		return errGatewayCapability
	}
	existing, err := readBounded(p.SitePath, gatewayMaxSiteBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errGatewayCapability
	}
	digest := sha256.Sum256(existing)
	if hex.EncodeToString(digest[:]) != p.ConfigSHA256 {
		return errGatewayDigest
	}
	if err := os.Remove(p.SitePath); err != nil {
		return errGatewayCapability
	}
	if err := c.validateAndReload(ctx); err != nil {
		if restoreErr := c.restore(ctx, p.SitePath, existing); restoreErr != nil {
			return errGatewayCapability
		}
		return err
	}
	return nil
}

func (c *gatewayCapability) validateAndReload(ctx context.Context) error {
	if output, err := c.runner.Run(ctx, c.nginxBinary, "-t", "-p", c.prefix, "-c", c.mainConfig); err != nil {
		_ = output
		return errGatewayCapability
	}
	if output, err := c.runner.Run(ctx, c.nginxBinary, "-s", "reload", "-p", c.prefix, "-c", c.mainConfig); err != nil {
		_ = output
		return errGatewayCapability
	}
	return nil
}

func (c *gatewayCapability) restore(ctx context.Context, sitePath string, previous []byte) error {
	if previous == nil {
		if err := os.Remove(sitePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := writeAtomic(sitePath, previous); err != nil {
		return err
	}
	return c.validateAndReload(ctx)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".gateway-*")
	if err != nil {
		return errGatewayCapability
	}
	name := temp.Name()
	discard := func() {
		_ = temp.Close()
		_ = os.Remove(name)
	}
	if err := temp.Chmod(0o600); err != nil {
		discard()
		return errGatewayCapability
	}
	if _, err := temp.Write(data); err != nil || temp.Sync() != nil {
		discard()
		return errGatewayCapability
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return errGatewayCapability
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return errGatewayCapability
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return errGatewayCapability
	}
	defer directory.Close()
	if directory.Sync() != nil {
		return errGatewayCapability
	}
	return nil
}

func readBounded(path string, max int) ([]byte, error) {
	if link, err := os.Lstat(path); err != nil {
		return nil, err
	} else if link.Mode()&os.ModeSymlink != 0 {
		return nil, errGatewayCapability
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(max) {
		return nil, errGatewayCapability
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil || len(data) > max {
		return nil, errGatewayCapability
	}
	return data, nil
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	return true
}

func rejectSymlinkPath(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errGatewayCapability
			}
		} else if !os.IsNotExist(err) {
			return errGatewayCapability
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
