package platform_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
)

func TestWave3PluginCenterRoutesAndXrayGate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	tlsServer := httptest.NewUnstartedServer(nil)
	portText := strconv.Itoa(tlsServer.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + portText
	composed, err := server.New(ctx, server.Config{
		DBPath:     filepath.Join(dir, "controller-plugins.db"),
		Origin:     origin,
		StagingDir: filepath.Join(dir, "staging"),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = composed.Close() }()
	tlsServer.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}}
	tlsServer.Config.Handler = composed
	tlsServer.StartTLS()
	defer tlsServer.Close()

	newClient := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), jar)
	}
	doReq := func(client *http.Client, method, path string, body any, csrf string) (int, []byte) {
		t.Helper()
		var reader io.Reader
		idempotencyKey := ""
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				sum := sha256.Sum256(append([]byte(path), raw...))
				idempotencyKey = hex.EncodeToString(sum[:16])
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	decode := func(t *testing.T, data []byte, value any) {
		t.Helper()
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}

	admin := newClient()
	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status, data := doReq(admin, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave3-plugin-admin-password",
	}, ""); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := doReq(admin, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wave3-plugin-admin-password"}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &login)

	if status, _ := doReq(newClient(), "GET", "/api/v1/plugins", nil, ""); status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Fatalf("anonymous plugin list = %d", status)
	}

	status, data = doReq(admin, "GET", "/api/v1/plugins", nil, "")
	if status != http.StatusOK {
		t.Fatalf("plugin list = %d: %s", status, data)
	}
	var installed struct {
		Items []any `json:"items"`
	}
	decode(t, data, &installed)
	if len(installed.Items) != 0 {
		t.Fatalf("expected no installed plugin: %s", data)
	}

	// The declarative Xray surface is closed while the package is not installed.
	for _, path := range []string{
		"/api/v1/plugins/xray-manager",
		"/api/v1/plugins/xray-manager/nodes",
		"/api/v1/plugins/xray-manager/templates",
		"/api/v1/plugins/xray-manager/users",
		"/api/v1/plugins/xray-manager/traffic",
	} {
		status, data = doReq(admin, "GET", path, nil, "")
		if status != http.StatusForbidden || !bytes.Contains(data, []byte("XRAY_PLUGIN_NOT_INSTALLED")) {
			t.Fatalf("%s = %d: %s", path, status, data)
		}
	}

	// Repository configuration is persistent declarative metadata only.
	status, data = doReq(admin, "GET", "/api/v1/plugins/repository", nil, "")
	if status != http.StatusOK {
		t.Fatalf("repository get = %d: %s", status, data)
	}
	var repository struct {
		Mode       string `json:"mode"`
		Configured bool   `json:"configured"`
		Path       string `json:"path"`
	}
	decode(t, data, &repository)
	if repository.Mode != "official" || repository.Configured || repository.Path != "Plugin" {
		t.Fatalf("unexpected initial repository: %s", data)
	}
	status, data = doReq(admin, "PUT", "/api/v1/plugins/repository", map[string]any{
		"mode": "custom", "url": "https://plugins.example.test/repo.git", "ref": "main", "path": "Plugin",
	}, login.CSRF)
	if status != http.StatusOK {
		t.Fatalf("repository put = %d: %s", status, data)
	}
	decode(t, data, &repository)
	if repository.Mode != "custom" || !repository.Configured {
		t.Fatalf("repository not persisted: %s", data)
	}
	status, data = doReq(admin, "PUT", "/api/v1/plugins/repository", map[string]any{
		"mode": "custom", "ref": "main", "path": "Plugin",
	}, login.CSRF)
	if status != http.StatusBadRequest {
		t.Fatalf("custom repository without url = %d: %s", status, data)
	}
	status, data = doReq(admin, "PUT", "/api/v1/plugins/repository", map[string]any{
		"mode": "official", "url": "https://plugins.example.test/repo.git", "ref": "main", "path": "Plugin",
	}, login.CSRF)
	if status != http.StatusBadRequest {
		t.Fatalf("official repository with url = %d: %s", status, data)
	}

	status, data = doReq(admin, "GET", "/api/v1/plugins/catalog", nil, "")
	if status != http.StatusOK {
		t.Fatalf("catalog = %d: %s", status, data)
	}
	var catalog struct {
		Items []any `json:"items"`
	}
	decode(t, data, &catalog)
	if len(catalog.Items) != 0 {
		t.Fatalf("expected empty catalog: %s", data)
	}
	status, data = doReq(admin, "POST", "/api/v1/plugins/catalog/refresh", nil, login.CSRF)
	if status != http.StatusNotImplemented || !bytes.Contains(data, []byte("PLUGIN_CATALOG_SOURCE_UNAVAILABLE")) {
		t.Fatalf("catalog refresh = %d: %s", status, data)
	}

	// Install/update have no package source adapter; activation requires install.
	status, data = doReq(admin, "POST", "/api/v1/plugins/xray-manager/install", map[string]any{
		"version": "1.0.0", "sourceCommit": "0123456789abcdef0123456789abcdef01234567",
		"expectedSha256":      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"acceptedPermissions": []string{"xray.read"},
	}, login.CSRF)
	if status != http.StatusNotImplemented || !bytes.Contains(data, []byte("PLUGIN_PACKAGE_SOURCE_UNAVAILABLE")) {
		t.Fatalf("install = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/plugins/xray-manager/update", map[string]any{
		"version": "1.0.1", "sourceCommit": "0123456789abcdef0123456789abcdef01234567",
		"expectedSha256":      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"acceptedPermissions": []string{"xray.read"},
	}, login.CSRF)
	if status != http.StatusNotImplemented {
		t.Fatalf("update = %d: %s", status, data)
	}
	status, data = doReq(admin, "PATCH", "/api/v1/plugins/xray-manager/activation", map[string]any{"enabled": true}, login.CSRF)
	if status != http.StatusConflict || !bytes.Contains(data, []byte("PLUGIN_NOT_INSTALLED")) {
		t.Fatalf("activation = %d: %s", status, data)
	}
	status, data = doReq(admin, "DELETE", "/api/v1/plugins/xray-manager", nil, login.CSRF)
	if status != http.StatusNotFound {
		t.Fatalf("uninstall = %d: %s", status, data)
	}
	status, data = doReq(admin, "GET", "/api/v1/plugins/xray-manager/unknown", nil, "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown plugin = %d: %s", status, data)
	}
	status, data = doReq(admin, "GET", "/api/v1/plugin-operations/0123456789abcdef0123456789abcdef", nil, "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown operation = %d: %s", status, data)
	}
}
