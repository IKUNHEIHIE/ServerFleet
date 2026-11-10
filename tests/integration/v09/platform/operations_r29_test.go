package platform_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/server"
	"strconv"
	"testing"
)

func TestR29OperationsHTTPAdministratorCSRFAndCredentialMetadata(t *testing.T) {
	ctx := context.Background()
	pki := newTestPKI(t)
	ts := httptest.NewUnstartedServer(nil)
	port := strconv.Itoa(ts.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + port
	composed, e := server.New(ctx, server.Config{DBPath: filepath.Join(t.TempDir(), "r29.db"), Origin: origin, StagingDir: filepath.Join(t.TempDir(), "stage")})
	if e != nil {
		t.Fatal(e)
	}
	defer composed.Close()
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}}
	ts.Config.Handler = composed
	ts.StartTLS()
	defer ts.Close()
	jar, e := cookiejar.New(nil)
	if e != nil {
		t.Fatal(e)
	}
	client := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", port), jar)
	request := func(method, path, body, csrf string) (int, []byte) {
		t.Helper()
		var in io.Reader
		if body != "" {
			in = bytes.NewBufferString(body)
		}
		req, e := http.NewRequestWithContext(ctx, method, origin+path, in)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		if method == "POST" {
			req.Header.Set("Idempotency-Key", "r29-synthetic-key-0001")
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		r, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		return r.StatusCode, raw
	}
	if code, _ := request("GET", "/api/v1/operations/incidents", "", ""); code != 403 {
		t.Fatal("anonymous incident read", code)
	}
	token, e := composed.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"setupToken": token, "username": "admin", "password": "synthetic-r29-password"})
	if code, b := request("POST", "/api/v1/auth/bootstrap", string(body), ""); code != 200 && code != 204 {
		t.Fatal(code, string(b))
	}
	code, raw := request("POST", "/api/v1/auth/login", `{"username":"admin","password":"synthetic-r29-password"}`, "")
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	if e = json.Unmarshal(raw, &login); e != nil || login.CSRF == "" {
		t.Fatal(e)
	}
	if code, raw = request("GET", "/api/v1/operations/incidents", "", ""); code != 200 || !bytes.Contains(raw, []byte(`"items":[]`)) {
		t.Fatal(code, string(raw))
	}
	if code, _ = request("POST", "/api/v1/operations/incidents/sync", `{}`, ""); code != 403 {
		t.Fatal("CSRF bypass", code)
	}
	if code, raw = request("POST", "/api/v1/operations/incidents/sync", `{}`, login.CSRF); code != 200 {
		t.Fatal(code, string(raw))
	}
	if code, _ = request("POST", "/api/v1/operations/incidents/sync", `{"extra":true}`, login.CSRF); code != 400 {
		t.Fatal("unknown field accepted", code)
	}
	if code, _ = request("POST", "/api/v1/operations/incidents/missing/actions", `{"requestId":"one","requestId":"two"}`, login.CSRF); code != 400 {
		t.Fatal("duplicate accepted", code)
	}
	if code, _ = request("GET", "/api/v1/operations/incidents?all=true", "", ""); code != 400 {
		t.Fatal("unknown query accepted", code)
	}
	if code, raw = request("GET", "/api/v1/backups/maintenance", "", ""); code != 503 || !bytes.Contains(raw, []byte("BACKUP_DEPLOYMENT_REQUIRED")) {
		t.Fatal(code, string(raw))
	}
	if code, raw = request("POST", "/api/v1/credentials", `{"name":"Backup synthetic","kind":"backup_storage_secret","usageScope":["backups"],"secret":"{\"username\":\"synthetic\",\"password\":\"synthetic-r29-secret\"}"}`, login.CSRF); code != 201 {
		t.Fatal(code, string(raw))
	}
	if code, raw = request("GET", "/api/v1/credentials", "", ""); code != 200 || bytes.Contains(raw, []byte("synthetic-r29-secret")) || !bytes.Contains(raw, []byte("backup_storage_secret")) {
		t.Fatal(code, string(raw))
	}
}
