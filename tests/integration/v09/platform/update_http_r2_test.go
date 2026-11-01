package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/updates"
)

func TestR2UpdateHTTPAwaitsHumanAndProjectsCurrentOwnerTask(t *testing.T) {
	ctx := context.Background()
	slot := t.TempDir()
	os.Chmod(slot, 0700)
	installer, err := updates.NewSlotInstaller(slot)
	if err != nil {
		t.Fatal(err)
	}
	defer installer.Close()
	var plan updates.FrozenInstallPlan
	var resolves atomic.Int32
	cfg := server.Config{DBPath: filepath.Join(t.TempDir(), "update-http.db"), Origin: "https://localhost", UpdateExecution: &updates.TaskInstallWorkerOptions{Installer: installer, Resolve: func(context.Context, updates.FrozenInstallPlan, updates.InstallIntent) (updates.PreparedInstall, error) {
		resolves.Add(1)
		return updates.PreparedInstall{}, updates.ErrArtifactRejected
	}, BackupCurrent: func(context.Context, updates.FrozenInstallPlan) error { return updates.ErrArtifactRejected }}, UpdateResolvePlan: func(context.Context, updates.ApplyRequest) (updates.FrozenInstallPlan, error) { return plan, nil }}
	composed, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer composed.Close()
	setup, _ := composed.Identity.PrepareSetup(ctx)
	if err = composed.Identity.Bootstrap(ctx, setup, "admin", "synthetic-update-http-password"); err != nil {
		t.Fatal(err)
	}
	login, err := composed.Identity.Login(ctx, "admin", "synthetic-update-http-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// Explicit trusted OFFLINE inventory fixture: no actual product release or
	// Agent process is executed in this HTTP admission/projection test.
	if _, err = composed.NodesRepo.Create(ctx, composed.DB, nodes.NodeCreateInput{ID: "owned-update-target", Name: "isolated update target", Address: "127.0.0.1", Status: nodes.StatusOffline}); err != nil {
		t.Fatal(err)
	}
	grant, err := composed.Identity.CreateResourceGrant(ctx, login.Secret, login.CSRF, auth.GrantRequest{PrincipalID: login.Identity.SubjectID, ResourceType: "node", ResourceID: "owned-update-target", Permissions: []string{"tasks.submit", "tasks.read", updates.ActionInstall}})
	if err != nil {
		t.Fatal(err)
	}
	previous := updates.ReleaseManifest{Format: 1, Component: "agent", Version: "1.0.0", Sequence: 1, OS: "linux", Architecture: "amd64", SHA256: strings.Repeat("a", 64), Bytes: 100}
	next := previous
	next.Version = "2.0.0"
	next.Sequence = 2
	plan = updates.FrozenInstallPlan{ID: "saved-update-plan", SubjectID: login.Identity.SubjectID, NodeID: "owned-update-target", BackupSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), Previous: previous, Attempted: next, AllowRollback: true}
	request := func(method, path string, body any, secret, csrf string) (int, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, cfg.Origin+path, bytes.NewReader(raw))
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("X-Requested-With", "ServerFleetWeb")
		r.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: secret})
		w := httptest.NewRecorder()
		composed.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	body := map[string]string{"planId": plan.ID, "confirmation": next.Version, "backupMode": "required"}
	if status, _ := request("POST", "/api/v1/system/updates/apply", body, login.Secret, ""); status != 403 {
		t.Fatal("apply bypassed CSRF", status)
	}
	status, raw := request("POST", "/api/v1/system/updates/apply", body, login.Secret, login.CSRF)
	var job updates.ApplyResult
	json.Unmarshal(raw, &job)
	if status != 202 || job.Status != string(tasks.StatusAwaitingApproval) || job.ID != job.TaskID {
		t.Fatal("persisted approval became HTTP failure", status, string(raw))
	}
	status, raw = request("POST", "/api/v1/system/updates/apply", body, login.Secret, login.CSRF)
	var repeated updates.ApplyResult
	json.Unmarshal(raw, &repeated)
	if status != 202 || repeated.TaskID != job.TaskID {
		t.Fatal("apply duplicated parent task", status, string(raw))
	}
	path := "/api/v1/system/updates/jobs/" + job.ID
	if status, raw = request("GET", path, nil, login.Secret, login.CSRF); status != 200 || !bytes.Contains(raw, []byte("awaiting_approval")) {
		t.Fatal(status, string(raw))
	}
	if status, raw = request("GET", "/api/v1/system/updates/jobs", nil, login.Secret, login.CSRF); status != 200 || !bytes.Contains(raw, []byte(job.ID)) {
		t.Fatal("empty jobs hid actual task", status, string(raw))
	}
	if _, err = composed.Identity.CreateUser(ctx, login.Secret, login.CSRF, auth.UserCreation{Username: "other-admin", Password: "synthetic-other-admin-password", RoleIDs: []string{"administrator"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	other, err := composed.Identity.Login(ctx, "other-admin", "synthetic-other-admin-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if status, _ = request("GET", path, nil, other.Secret, other.CSRF); status != 404 {
		t.Fatal("other owner read update job", status)
	}
	if status, raw = request("GET", "/api/v1/system/updates/jobs", nil, other.Secret, other.CSRF); status != 200 || bytes.Contains(raw, []byte(job.ID)) {
		t.Fatal("other owner listed update job", status, string(raw))
	}
	if err = composed.Identity.RevokeResourceGrant(ctx, login.Secret, login.CSRF, grant.GrantID); err != nil {
		t.Fatal(err)
	}
	if status, _ = request("GET", path, nil, login.Secret, login.CSRF); status != 403 {
		t.Fatal("revoked task read allowed", status)
	}
	if status, _ = request("GET", "/api/v1/system/updates/jobs", nil, login.Secret, login.CSRF); status != 403 {
		t.Fatal("revoked list returned partial success", status)
	}
	if resolves.Load() != 0 {
		t.Fatal("worker executed without human approval")
	}
}
