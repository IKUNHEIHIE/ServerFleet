package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

type fakeGatewayRunner struct {
	mu                sync.Mutex
	calls             [][]string
	failValidate      bool
	failReload        bool
	failValidateFirst bool
	failReloadFirst   bool
	validates         int
	reloads           int
}

func (r *fakeGatewayRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	switch {
	case len(args) > 0 && args[0] == "-t":
		r.validates++
		if r.failValidate || (r.failValidateFirst && r.validates == 1) {
			return []byte("syntax error"), os.ErrInvalid
		}
	case len(args) > 0 && args[0] == "-s":
		r.reloads++
		if r.failReload || (r.failReloadFirst && r.reloads == 1) {
			return []byte("reload failed"), os.ErrInvalid
		}
	}
	return []byte("ok"), nil
}

func (r *fakeGatewayRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([][]string, len(r.calls))
	for i := range r.calls {
		copied[i] = append([]string(nil), r.calls[i]...)
	}
	return copied
}

type gatewayTestEnv struct {
	prefix, siteDir, artifactDir, mainConfig string
	capability                               *gatewayCapability
	runner                                   *fakeGatewayRunner
}

func newGatewayTestEnv(t *testing.T) *gatewayTestEnv {
	t.Helper()
	return newGatewayTestEnvAt(t, t.TempDir())
}

func newGatewayTestEnvAt(t *testing.T, prefix string) *gatewayTestEnv {
	t.Helper()
	// The capability requires a private operator-owned root on Linux too.
	if err := os.Chmod(prefix, 0o700); err != nil {
		t.Fatal(err)
	}
	siteDir := filepath.Join(prefix, "conf")
	artifactDir := filepath.Join(prefix, "artifacts")
	mainConfig := filepath.Join(prefix, "nginx.conf")
	for _, dir := range []string{siteDir, artifactDir, filepath.Join(prefix, "logs"), filepath.Join(prefix, "temp")} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainConfig, []byte("events {}\nhttp { include conf/*.conf; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeGatewayRunner{}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capability, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix: prefix, SiteDir: siteDir, ArtifactDir: artifactDir, MainConfig: mainConfig,
		NginxBinary: binary,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	return &gatewayTestEnv{prefix: prefix, siteDir: siteDir, artifactDir: artifactDir, mainConfig: mainConfig, capability: capability, runner: runner}
}

func (e *gatewayTestEnv) artifact(t *testing.T, name string, content []byte) (string, string) {
	t.Helper()
	path := filepath.Join(e.artifactDir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:])
}

func (e *gatewayTestEnv) params(t *testing.T, operation, siteName, artifactPath, digest string, takeover bool, approval string) []byte {
	t.Helper()
	values := map[string]string{
		"operation": operation, "site_path": filepath.Join(e.siteDir, siteName),
		"config_sha256": digest,
	}
	if operation == "apply" {
		values["artifact_path"] = artifactPath
		values["takeover_approved"] = strconv.FormatBool(takeover)
		if approval != "" {
			values["approval_ref"] = approval
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (e *gatewayTestEnv) site(name string) string { return filepath.Join(e.siteDir, name) }

func TestGatewayCapabilityApplyAndIdempotent(t *testing.T) {
	env := newGatewayTestEnv(t)
	content := []byte("server { listen 127.0.0.1:18080; }\n")
	artifact, digest := env.artifact(t, "site.conf", content)
	action := env.capability.Actions()[gatewayApplyAction]
	params := env.params(t, "apply", "site.conf", artifact, digest, false, "")
	if err := action.Validate(params); err != nil {
		t.Fatal(err)
	}
	if result := action.Run(context.Background(), params); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("apply result = %+v", result)
	}
	stored, err := os.ReadFile(env.site("site.conf"))
	if err != nil || string(stored) != string(content) {
		t.Fatalf("stored = %q, err=%v", stored, err)
	}
	calls := env.runner.snapshot()
	if len(calls) != 2 || calls[0][1] != "-t" || calls[1][1] != "-s" || calls[1][2] != "reload" {
		t.Fatalf("calls = %+v", calls)
	}
	before := len(env.runner.snapshot())
	if result := action.Run(context.Background(), params); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("idempotent result = %+v", result)
	}
	if len(env.runner.snapshot()) != before {
		t.Fatal("idempotent apply reloaded nginx again")
	}
}

func TestGatewayCapabilityExistingSiteRequiresTakeover(t *testing.T) {
	env := newGatewayTestEnv(t)
	old := []byte("old site")
	if err := os.WriteFile(env.site("site.conf"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, digest := env.artifact(t, "site.conf", []byte("new site"))
	action := env.capability.Actions()[gatewayApplyAction]
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, digest, false, "")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("unapproved takeover result = %+v", result)
	}
	stored, _ := os.ReadFile(env.site("site.conf"))
	if string(stored) != string(old) {
		t.Fatalf("existing site changed: %q", stored)
	}
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, digest, true, "approval-1")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("approved takeover result = %+v", result)
	}
}

func TestGatewayCapabilityValidateFailureRestoresOldFile(t *testing.T) {
	env := newGatewayTestEnv(t)
	old := []byte("old site")
	if err := os.WriteFile(env.site("site.conf"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, digest := env.artifact(t, "new.conf", []byte("invalid site"))
	env.runner.failValidateFirst = true
	action := env.capability.Actions()[gatewayApplyAction]
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, digest, true, "approval-1")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("validate failure result = %+v", result)
	}
	stored, _ := os.ReadFile(env.site("site.conf"))
	if string(stored) != string(old) {
		t.Fatalf("old file was not restored: %q", stored)
	}
}

func TestGatewayCapabilityRestoreFailureIsUnknown(t *testing.T) {
	env := newGatewayTestEnv(t)
	old := []byte("old site")
	if err := os.WriteFile(env.site("site.conf"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, digest := env.artifact(t, "new.conf", []byte("invalid site"))
	env.runner.failValidate = true
	action := env.capability.Actions()[gatewayApplyAction]
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, digest, true, "approval-1")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
		t.Fatalf("restore failure result = %+v", result)
	}
}

func TestGatewayCapabilityReloadFailureRestoresOldFile(t *testing.T) {
	env := newGatewayTestEnv(t)
	old := []byte("old site")
	if err := os.WriteFile(env.site("site.conf"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, digest := env.artifact(t, "new.conf", []byte("new site"))
	env.runner.failReloadFirst = true
	action := env.capability.Actions()[gatewayApplyAction]
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, digest, true, "approval-1")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("reload failure result = %+v", result)
	}
	stored, _ := os.ReadFile(env.site("site.conf"))
	if string(stored) != string(old) {
		t.Fatalf("old file was not restored after reload failure: %q", stored)
	}
}

func TestGatewayCapabilityRemoveIsDigestBound(t *testing.T) {
	env := newGatewayTestEnv(t)
	content := []byte("managed site")
	if err := os.WriteFile(env.site("site.conf"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	wrong := "0" + digest[1:]
	if wrong == digest {
		wrong = "1" + digest[1:]
	}
	action := env.capability.Actions()[gatewayRemoveAction]
	if result := action.Run(context.Background(), env.params(t, "remove", "site.conf", "", wrong, false, "")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("wrong digest remove = %+v", result)
	}
	if _, err := os.Stat(env.site("site.conf")); err != nil {
		t.Fatal("wrong digest removed the site")
	}
	if result := action.Run(context.Background(), env.params(t, "remove", "site.conf", "", digest, false, "")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("remove result = %+v", result)
	}
	if _, err := os.Stat(env.site("site.conf")); !os.IsNotExist(err) {
		t.Fatal("site still exists after remove")
	}
}

func TestGatewayCapabilityPathAndDigestRejections(t *testing.T) {
	env := newGatewayTestEnv(t)
	artifact, digest := env.artifact(t, "site.conf", []byte("server {}"))
	action := env.capability.Actions()[gatewayApplyAction]
	outside := filepath.Join(env.prefix, "..", "escape.conf")
	raw, _ := json.Marshal(map[string]any{
		"operation": "apply", "site_path": outside, "artifact_path": artifact,
		"config_sha256": digest, "takeover_approved": false, "approval_ref": "",
	})
	if result := action.Run(context.Background(), raw); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("path escape result = %+v", result)
	}
	if result := action.Run(context.Background(), env.params(t, "apply", "site.conf", artifact, "a"+digest[1:], false, "")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("digest mismatch result = %+v", result)
	}
}

func TestGatewayCapabilityRejectsSymlinkedArtifactDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	env := newGatewayTestEnv(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(env.artifactDir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix: env.prefix, SiteDir: env.siteDir, ArtifactDir: filepath.Join(env.artifactDir, "link"), MainConfig: env.mainConfig,
	}, env.runner); err == nil {
		t.Fatal("capability accepted a symlinked artifact root")
	}
}

func TestGatewayCapabilityFixedArgvHasNoCallerCommands(t *testing.T) {
	env := newGatewayTestEnv(t)
	artifact, digest := env.artifact(t, "site.conf", []byte("server {}"))
	action := env.capability.Actions()[gatewayApplyAction]
	raw, _ := json.Marshal(map[string]any{
		"operation": "apply", "site_path": env.site("site.conf"), "artifact_path": artifact,
		"config_sha256": digest, "takeover_approved": false, "approval_ref": "",
		"validate_command": "sh -c evil", "reload_command": "rm -rf /",
	})
	if err := action.Validate(raw); err == nil {
		t.Fatal("unknown command fields were accepted")
	}
}

func asciiTestRoot(t *testing.T) string {
	t.Helper()
	// Drive-letter prefixes are only absolute on Windows. On Unix they would
	// resolve to a relative "C:/temp" under the package directory, and every
	// capability root check would reject the fixture as non-absolute.
	if runtime.GOOS == "windows" {
		candidates := []string{"C:/temp", "D:/temp"}
		for _, candidate := range candidates {
			if err := os.MkdirAll(candidate, 0o700); err == nil {
				if probe, err := os.CreateTemp(candidate, "probe-*"); err == nil {
					name := probe.Name()
					_ = probe.Close()
					_ = os.Remove(name)
					return candidate
				}
			}
		}
	}
	return os.TempDir()
}

// waitForGatewayProcess waits until nginx has written its pid file. Without the
// wait an immediate reload can race a master that has not finished starting and
// the capability correctly reports an unknown rollback outcome.
func waitForGatewayProcess(t *testing.T, pidPath string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if info, err := os.Stat(pidPath); err == nil && info.Mode().IsRegular() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nginx pid file %s did not appear", pidPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGatewayCapabilityRealNginxIsolated(t *testing.T) {
	nginxPath := os.Getenv("NGINX_BIN")
	if nginxPath == "" {
		if resolved, err := exec.LookPath("nginx"); err == nil {
			nginxPath = resolved
		}
	}
	if nginxPath == "" {
		t.Skip("NGINX_BIN not set and nginx is not installed")
	}
	prefix, err := os.MkdirTemp(asciiTestRoot(t), "sf-nginx-")
	if err != nil {
		t.Fatal(err)
	}
	prefix = filepath.Clean(prefix)
	t.Cleanup(func() { _ = os.RemoveAll(prefix) })
	env := newGatewayTestEnvAt(t, prefix)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("gateway-ok"))
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	listenAddr := freeLoopbackAddress(t)
	mainConfig := filepath.Join(env.prefix, "nginx.conf")
	main := "worker_processes 1;\n" +
		"pid " + filepath.ToSlash(filepath.Join(env.prefix, "nginx.pid")) + ";\n" +
		"error_log " + filepath.ToSlash(filepath.Join(env.prefix, "error.log")) + ";\n" +
		"events { worker_connections 64; }\n" +
		"http { access_log off; include " + filepath.ToSlash(filepath.Join(env.siteDir, "*.conf")) + "; }\n"
	if err := os.WriteFile(mainConfig, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.siteDir, "placeholder.conf"), []byte("# placeholder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capability, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix: env.prefix, SiteDir: env.siteDir, ArtifactDir: env.artifactDir,
		MainConfig: mainConfig, NginxBinary: nginxPath,
	}, execGatewayRunner{})
	if err != nil {
		t.Fatal(err)
	}
	start := exec.Command(nginxPath, "-p", filepath.ToSlash(env.prefix)+"/", "-c", filepath.ToSlash(mainConfig))
	var startOutput bytes.Buffer
	start.Stdout, start.Stderr = &startOutput, &startOutput
	if err := start.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = start.Wait() }()
	waitForGatewayProcess(t, filepath.Join(env.prefix, "nginx.pid"))
	t.Cleanup(func() {
		_ = exec.Command(nginxPath, "-s", "stop", "-p", filepath.ToSlash(env.prefix)+"/", "-c", filepath.ToSlash(mainConfig)).Run()
		_ = start.Process.Kill()
	})
	config := []byte(`server {
    listen ` + listenAddr + `;
    location / {
        proxy_pass http://` + upstreamURL.Host + `;
        proxy_set_header Host $host;
    }
}
`)
	sum := sha256.Sum256(config)
	digest := hex.EncodeToString(sum[:])
	artifact := filepath.Join(env.artifactDir, "site.conf")
	if err := os.WriteFile(artifact, config, 0o600); err != nil {
		t.Fatal(err)
	}
	apply := capability.Actions()[gatewayApplyAction]
	params := env.params(t, "apply", "site.conf", artifact, digest, false, "")
	if result := apply.Run(context.Background(), params); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("apply = %+v", result)
	}
	waitHTTP(t, "http://"+listenAddr+"/", http.StatusOK)
	if result := apply.Run(context.Background(), params); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("idempotent apply = %+v", result)
	}
	invalid := []byte("server { listen 127.0.0.1:1; this_is_not_valid; }\n")
	badSum := sha256.Sum256(invalid)
	badArtifact := filepath.Join(env.artifactDir, "bad.conf")
	if err := os.WriteFile(badArtifact, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	badParams := env.params(t, "apply", "site.conf", badArtifact, hex.EncodeToString(badSum[:]), true, "approval-1")
	if result := apply.Run(context.Background(), badParams); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED {
		t.Fatalf("invalid apply = %+v", result)
	}
	waitHTTP(t, "http://"+listenAddr+"/", http.StatusOK)
	remove := capability.Actions()[gatewayRemoveAction]
	if result := remove.Run(context.Background(), env.params(t, "remove", "site.conf", "", digest, false, "")); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("remove = %+v", result)
	}
	waitNotOK(t, "http://"+listenAddr+"/")
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func localHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
}

func waitNotOK(t *testing.T, target string) {
	t.Helper()
	client := localHTTPClient()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(target)
		if err != nil {
			return
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("HTTP %s still serves 200 after removal", target)
}

func waitHTTP(t *testing.T, target string, want int) {
	t.Helper()
	client := localHTTPClient()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(target)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("HTTP %s did not reach status %d", target, want)
}
