//go:build cx_r3_product

package platform_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/updates"
)

func TestR3ActualProductAgentHTTPApprovedSignedInstallAndCompatibleRollback(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("non-root actual Agent product")
	}
	oldBinary, nextBinary := os.Getenv("SF_R3_AGENT_V1"), os.Getenv("SF_R3_AGENT_V2")
	if !filepath.IsAbs(oldBinary) || !filepath.IsAbs(nextBinary) {
		t.Fatal("actual release builds required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	pki := newTestPKI(t)
	host := httptest.NewUnstartedServer(nil)
	port := fmt.Sprint(host.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + port
	var active atomic.Pointer[server.Server]
	host.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Load().ServeHTTP(w, r) })
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pki.roots}
	cfg := server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin, AgentAuthority: "localhost:" + port, AgentServerName: "localhost", EnrollmentIssuer: managerIssuer(t, pki)}
	composed, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	active.Store(composed)
	host.StartTLS()
	defer host.Close()
	defer func() { composed.Close() }()
	setup, _ := composed.Identity.PrepareSetup(ctx)
	if err = composed.Identity.Bootstrap(ctx, setup, "admin", "product-r3-synthetic-password"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &r3Client{t: t, ctx: ctx, http: newTestHTTPClient(pki, "127.0.0.1:"+port, jar), origin: origin}
	raw := c.must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "product-r3-synthetic-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	c.csrf = login.CSRF
	programLogin, e := composed.Identity.Login(ctx, "admin", "product-r3-synthetic-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = composed.Identity.CreateUser(ctx, programLogin.Secret, programLogin.CSRF, auth.UserCreation{Username: "release-approver", Password: "product-r3-approver-password", RoleIDs: []string{"administrator"}, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	otherJar, _ := cookiejar.New(nil)
	approver := &r3Client{t: t, ctx: ctx, http: newTestHTTPClient(pki, "127.0.0.1:"+port, otherJar), origin: origin}
	raw = approver.must("POST", "/api/v1/auth/login", map[string]string{"username": "release-approver", "password": "product-r3-approver-password"}, 200)
	var other struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &other)
	approver.csrf = other.CSRF
	rootCert := filepath.Join(dir, "ca.pem")
	if os.WriteFile(rootCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pki.rootDER}), 0600) != nil {
		t.Fatal("private CA certificate")
	}
	raw = c.must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
	var token struct{ Token string }
	json.Unmarshal(raw, &token)
	identity := filepath.Join(dir, "identity")
	enroll := []string{"-enroll-token", token.Token, "-state-dir", identity, "-controller-url", origin, "-root-cert", rootCert}
	if _, stderr, code := runAgentCLI(t, ctx, oldBinary, enroll...); code != 0 {
		t.Fatal(code, stderr)
	}
	raw = c.must("GET", "/api/v1/enrollments", nil, 200)
	var pending struct{ Items []struct{ ID, NodeID string } }
	json.Unmarshal(raw, &pending)
	if len(pending.Items) != 1 {
		t.Fatal(string(raw))
	}
	node := pending.Items[0].NodeID
	c.must("POST", "/api/v1/enrollments/"+pending.Items[0].ID+"/decision", map[string]string{"decision": "approve", "reason": "actual owned product update"}, 200)
	if _, stderr, code := runAgentCLI(t, ctx, oldBinary, enroll...); code != 0 {
		t.Fatal(code, stderr)
	}
	for _, user := range []string{login.Identity.ID, other.Identity.ID} {
		c.must("POST", "/api/v1/resource-grants", map[string]any{"principalId": user, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.read", "tasks.submit", "tasks.approve", updates.ActionInstall}}, 200)
	}
	slot, inbox, stage := filepath.Join(dir, "slot"), filepath.Join(dir, "inbox"), filepath.Join(dir, "stage")
	for _, d := range []string{slot, inbox, stage} {
		if os.Mkdir(d, 0700) != nil {
			t.Fatal("private directory")
		}
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	writeRelease := func(version string, seq uint64, binary string) updates.ReleaseManifest {
		t.Helper()
		data, e := os.ReadFile(binary)
		if e != nil {
			t.Fatal(e)
		}
		m := updates.ReleaseManifest{Format: 1, Component: "agent", Version: version, Sequence: seq, OS: "linux", Architecture: runtime.GOARCH, SHA256: sha256Hex(data), Bytes: int64(len(data))}
		directory := filepath.Join(inbox, version)
		os.Mkdir(directory, 0700)
		raw, _ := json.Marshal(m)
		os.WriteFile(filepath.Join(directory, "manifest.json"), raw, 0600)
		os.WriteFile(filepath.Join(directory, "signature.bin"), ed25519.Sign(key, append([]byte("serverfleet.release.v1\x00"), raw...)), 0600)
		os.WriteFile(filepath.Join(directory, "artifact"), data, 0600)
		return m
	}
	initial := writeRelease("1.0.0", 1, oldBinary)
	next := writeRelease("1.0.1", 2, nextBinary)
	writeRelease("1.0.2", 3, nextBinary)
	writeRelease("1.0.3", 4, nextBinary)
	oldBytes, _ := os.ReadFile(oldBinary)
	os.WriteFile(filepath.Join(slot, "active"), oldBytes, 0700)
	schemaFingerprint, e := updates.SQLiteSchemaFingerprint(ctx, composed.DB)
	if e != nil {
		t.Fatal(e)
	}
	if err = composed.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.UpdateProduct = &updates.ProductDeploymentOptions{Directory: slot, Inbox: inbox, StageDir: stage, KeyID: "synthetic-release", NodeID: node, IdentityDir: identity, ControllerOrigin: origin, RootCertificate: rootCert, Policy: updates.ReleasePolicy{Component: "agent", Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"synthetic-release": pub}}, Initial: initial, SchemaVersion: 0, SchemaFingerprint: schemaFingerprint, CandidateVersion: next.Version}
	composed, err = server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	active.Store(composed)
	health := func(version string) map[string]any {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for {
			raw, e := os.ReadFile(filepath.Join(slot, "health.json"))
			var v map[string]any
			if e == nil && json.Unmarshal(raw, &v) == nil && v["version"] == version {
				return v
			}
			if time.Now().After(deadline) {
				t.Fatal("actual product did not establish WSS readiness", string(raw))
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	before := health(initial.Version)
	oldPID := before["pid"]
	check := c.must("POST", "/api/v1/system/updates/check", map[string]string{"channel": "stable"}, 200)
	if !bytes.Contains(check, []byte(`"updateAvailable":true`)) {
		t.Fatal(string(check))
	}
	for _, version := range []string{"1.0.1", "1.0.2"} {
		raw = c.must("POST", "/api/v1/system/updates/plan", map[string]any{"targetVersion": version, "dryRun": true}, 200)
		var plan updates.Plan
		json.Unmarshal(raw, &plan)
		if !plan.Eligible {
			t.Fatal(string(raw))
		}
		raw = c.must("POST", "/api/v1/system/updates/apply", map[string]string{"planId": plan.PlanID, "confirmation": version, "backupMode": "required"}, 202)
		var job updates.ApplyResult
		json.Unmarshal(raw, &job)
		if job.Status != "awaiting_approval" {
			t.Fatal(string(raw))
		}
		beforeVersion := initial.Version
		if version != "1.0.1" {
			beforeVersion = next.Version
		}
		if v := health(beforeVersion); v["pid"] != oldPID {
			t.Fatal("product changed before human approval")
		}
		raw = c.must("GET", "/api/v1/tasks/"+job.TaskID, nil, 200)
		var task struct {
			Digest string `json:"planDigest"`
		}
		json.Unmarshal(raw, &task)
		approver.must("POST", "/api/v1/tasks/"+job.TaskID+"/approve", map[string]string{"planDigest": task.Digest, "decision": "approve", "reason": "verify signed isolated actual Agent release"}, 200)
		want := "completed"
		if version == "1.0.2" {
			want = "failed"
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			raw = c.must("GET", "/api/v1/system/updates/jobs/"+job.ID, nil, 200)
			var state updates.ApplyResult
			json.Unmarshal(raw, &state)
			if state.Status == want {
				break
			}
			if state.Status == "unknown" || time.Now().After(deadline) {
				events, _ := composed.Tasks.Events(ctx, job.TaskID, 0, 100)
				t.Fatal("product update terminal", string(raw), events)
			}
			time.Sleep(30 * time.Millisecond)
		}
		v := health(next.Version)
		if v["pid"] == oldPID {
			t.Fatal("actual process was not replaced")
		}
		oldPID = v["pid"]
		var installedRaw []byte
		var floor uint64
		composed.DB.QueryRow(ctx, `SELECT payload,sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&installedRaw, &floor)
		var installed updates.ReleaseManifest
		json.Unmarshal(installedRaw, &installed)
		if installed != next || floor != map[string]uint64{"1.0.1": 2, "1.0.2": 3}[version] {
			t.Fatal("journal provenance/floor", string(installedRaw), floor)
		}
		t.Logf("actual Agent product %s -> parent %s; current build %v PID %v; journal floor %d", version, want, v["version"], v["pid"], floor)
	}
	// Schema drift after human approval must end UNKNOWN before any product
	// change: approval is not a bypass of the current schema gate, and no
	// provenance/floor is committed.
	raw = c.must("POST", "/api/v1/system/updates/plan", map[string]any{"targetVersion": "1.0.3", "dryRun": true}, 200)
	var schemaPlan updates.Plan
	json.Unmarshal(raw, &schemaPlan)
	if !schemaPlan.Eligible {
		t.Fatal(string(raw))
	}
	raw = c.must("POST", "/api/v1/system/updates/apply", map[string]string{"planId": schemaPlan.PlanID, "confirmation": "1.0.3", "backupMode": "required"}, 202)
	var schemaJob updates.ApplyResult
	json.Unmarshal(raw, &schemaJob)
	if schemaJob.Status != "awaiting_approval" {
		t.Fatal(string(raw))
	}
	if _, err = composed.DB.Exec(ctx, `PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	raw = c.must("GET", "/api/v1/tasks/"+schemaJob.TaskID, nil, 200)
	var schemaTask struct {
		Digest string `json:"planDigest"`
	}
	json.Unmarshal(raw, &schemaTask)
	approver.must("POST", "/api/v1/tasks/"+schemaJob.TaskID+"/approve", map[string]string{"planDigest": schemaTask.Digest, "decision": "approve", "reason": "schema drift must stay unknown"}, 200)
	deadline := time.Now().Add(20 * time.Second)
	for {
		raw = c.must("GET", "/api/v1/system/updates/jobs/"+schemaJob.ID, nil, 200)
		var state updates.ApplyResult
		json.Unmarshal(raw, &state)
		if state.Status == "unknown" {
			break
		}
		if state.Status == "completed" || state.Status == "installed" || state.Status == "rolled_back" || time.Now().After(deadline) {
			t.Fatal("schema drift was not reported UNKNOWN", string(raw))
		}
		time.Sleep(30 * time.Millisecond)
	}
	if v := health(next.Version); v["pid"] != oldPID {
		t.Fatal("product was replaced despite schema drift")
	}
	var afterFloor uint64
	composed.DB.QueryRow(ctx, `SELECT sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&afterFloor)
	if afterFloor != 3 {
		t.Fatal("provenance advanced under schema drift", afterFloor)
	}
	if _, err = composed.DB.Exec(ctx, `PRAGMA user_version=0`); err != nil {
		t.Fatal(err)
	}
}
