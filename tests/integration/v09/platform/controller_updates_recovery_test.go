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
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
)

func TestWave3UpdatesAndRecoveryRoutes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	tlsServer := httptest.NewUnstartedServer(nil)
	portText := strconv.Itoa(tlsServer.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + portText
	composed, err := server.New(ctx, server.Config{
		DBPath:     filepath.Join(dir, "controller-updates.db"),
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
			if method == http.MethodPost {
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
		"setupToken": setupToken, "username": "admin", "password": "wave3-update-admin-password",
	}, ""); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := doReq(admin, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wave3-update-admin-password"}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &login)

	if status, _ := doReq(newClient(), "POST", "/api/v1/system/updates/check", map[string]any{"channel": "stable"}, ""); status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Fatalf("anonymous update check = %d", status)
	}

	// Update source, planner and installer are explicitly unavailable.
	status, data = doReq(admin, "POST", "/api/v1/system/updates/check", map[string]any{"channel": "stable"}, login.CSRF)
	if status != http.StatusServiceUnavailable || !bytes.Contains(data, []byte("UPDATE_SOURCE_UNAVAILABLE")) {
		t.Fatalf("update check = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/system/updates/check", map[string]any{"channel": "nightly"}, login.CSRF)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid channel = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/system/updates/plan", map[string]any{"targetVersion": "0.10.0", "dryRun": true}, login.CSRF)
	if status != http.StatusServiceUnavailable || !bytes.Contains(data, []byte("UPDATE_PLANNER_UNAVAILABLE")) {
		t.Fatalf("update plan = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/system/updates/apply", map[string]any{
		"planId": "plan-1", "confirmation": "0.10.0", "backupMode": "required",
	}, login.CSRF)
	if status != http.StatusNotImplemented || !bytes.Contains(data, []byte("UPDATE_INSTALLER_UNAVAILABLE")) {
		t.Fatalf("update apply = %d: %s", status, data)
	}
	status, data = doReq(admin, "GET", "/api/v1/system/updates/jobs", nil, "")
	if status != http.StatusOK {
		t.Fatalf("update jobs = %d: %s", status, data)
	}
	var jobs struct {
		Items []any `json:"items"`
	}
	decode(t, data, &jobs)
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no update jobs: %s", data)
	}
	if status, _ = doReq(admin, "GET", "/api/v1/system/updates/jobs/job-1", nil, ""); status != http.StatusNotFound {
		t.Fatalf("update job get = %d", status)
	}
	if status, _ = doReq(admin, "POST", "/api/v1/system/updates/jobs/job-1/rollback", nil, login.CSRF); status != http.StatusNotFound {
		t.Fatalf("update rollback = %d", status)
	}

	// A real SQLite backup is verified by quick_check and migrations ledger.
	backupPath := filepath.Join(dir, "backups", "backup-1.db")
	if err := composed.DB.BackupTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}
	status, data = doReq(admin, "POST", "/api/v1/backups/backup-1/verify", nil, login.CSRF)
	if status != http.StatusOK {
		t.Fatalf("backup verify = %d: %s", status, data)
	}
	var verified struct {
		BackupID string `json:"backupId"`
		Verified bool   `json:"verified"`
		Checks   []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
	}
	decode(t, data, &verified)
	if !verified.Verified || len(verified.Checks) != 1 || !verified.Checks[0].Passed || verified.Checks[0].Name != "backup_integrity" {
		t.Fatalf("unexpected backup verification: %s", data)
	}

	junkPath := filepath.Join(dir, "backups", "junk.db")
	if err := os.WriteFile(junkPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, data = doReq(admin, "POST", "/api/v1/backups/junk/verify", nil, login.CSRF)
	if status != http.StatusOK {
		t.Fatalf("junk verify = %d: %s", status, data)
	}
	verified.Verified = false
	verified.Checks = nil
	decode(t, data, &verified)
	if verified.Verified || len(verified.Checks) != 1 || verified.Checks[0].Passed {
		t.Fatalf("junk backup unexpectedly verified: %s", data)
	}
	if status, _ = doReq(admin, "POST", "/api/v1/backups/missing/verify", nil, login.CSRF); status != http.StatusNotFound {
		t.Fatalf("missing backup verify = %d", status)
	}

	plan := map[string]any{
		"backupId": "backup-1", "targetControllerId": "recovery-target",
		"oldControllerFenced": true, "recoveryKeyReference": "recovery-key-1",
	}
	status, data = doReq(admin, "POST", "/api/v1/disaster-recovery/preview", plan, login.CSRF)
	if status != http.StatusOK {
		t.Fatalf("recovery preview = %d: %s", status, data)
	}
	var preview struct {
		Eligible bool `json:"eligible"`
		Verified bool `json:"verified"`
		Checks   []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
			Reason string `json:"reason"`
		} `json:"checks"`
	}
	decode(t, data, &preview)
	if preview.Eligible || !preview.Verified || len(preview.Checks) < 4 {
		t.Fatalf("unexpected recovery preview: %s", data)
	}
	for _, check := range preview.Checks {
		if check.Name == "old_controller_fenced" && check.Passed {
			t.Fatalf("client fencing declaration was accepted as evidence: %s", data)
		}
	}

	status, data = doReq(admin, "POST", "/api/v1/disaster-recovery/jobs", plan, login.CSRF)
	if status != http.StatusNotImplemented || !bytes.Contains(data, []byte("RECOVERY_EXECUTOR_UNAVAILABLE")) {
		t.Fatalf("recovery job = %d: %s", status, data)
	}
	if status, _ = doReq(admin, "GET", "/api/v1/disaster-recovery/jobs/job-1", nil, ""); status != http.StatusNotFound {
		t.Fatalf("recovery job get = %d", status)
	}

	// A non-administrator is denied even with a valid session.
	status, data = doReq(admin, "POST", "/api/v1/users", map[string]any{
		"username": "operator", "displayName": "Operator", "password": "wave3-update-operator-password", "roleIds": []string{"operator"},
	}, login.CSRF)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create operator = %d: %s", status, data)
	}
	operator := newClient()
	status, data = doReq(operator, "POST", "/api/v1/auth/login", map[string]string{"username": "operator", "password": "wave3-update-operator-password"}, "")
	if status != http.StatusOK {
		t.Fatalf("operator login = %d: %s", status, data)
	}
	var operatorLogin struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &operatorLogin)
	status, data = doReq(operator, "POST", "/api/v1/system/updates/check", map[string]any{"channel": "stable"}, operatorLogin.CSRF)
	if status != http.StatusForbidden {
		t.Fatalf("operator update check = %d: %s", status, data)
	}
}
