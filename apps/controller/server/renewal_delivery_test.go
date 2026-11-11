package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
	"testing"
	"time"
)

func TestRenewalDeliveryHTTPRejectsUnreviewedUnknownAndNeverReplays(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewUnstartedServer(nil)
	origin := "https://" + ts.Listener.Addr().String()
	s, e := New(ctx, Config{DBPath: filepath.Join(t.TempDir(), "fleet.db"), Origin: origin, StagingDir: filepath.Join(t.TempDir(), "stage")})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// Stop owned background workers before replacing the configured CA seam.
	s.runCancel()
	<-s.runDone
	ts.Config.Handler = s
	ts.StartTLS()
	defer ts.Close()
	client := ts.Client()
	client.Jar, _ = cookiejar.New(nil)
	request := func(method, path string, body any, csrf string) (int, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(raw))
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if method == "POST" {
			req.Header.Set("Idempotency-Key", "test-"+strings.ReplaceAll(path, "/", "-")+"-"+csrf)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, b
	}
	if code, _ := request("GET", "/api/v1/installations/catalog", nil, ""); code != 403 {
		t.Fatal("anonymous install metadata", code)
	}
	token, e := s.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if code, b := request("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": token, "username": "admin", "password": "synthetic-r31-password"}, ""); code != 200 && code != 204 {
		t.Fatal(code, string(b))
	}
	code, raw := request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "synthetic-r31-password"}, "")
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var login struct {
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
		CSRF string `json:"csrfToken"`
	}
	if json.Unmarshal(raw, &login) != nil || login.CSRF == "" || login.Identity.UserID == "" {
		t.Fatal("missing authenticated identity")
	}
	owner := login.Identity.UserID
	now := time.Now().UTC()
	for _, id := range []string{"old-order", "old-install", "unknown-order"} {
		state := tasks.StatusCompleted
		target := tasks.TargetSucceeded
		if id == "unknown-order" {
			state = tasks.StatusUnknown
			target = tasks.TargetUnknown
		}
		task := tasks.Task{ID: id, Name: "private-sentinel", SubmitterID: owner, ActionID: actionPublicCertificateIssue, OperationClass: tasks.OperationClassModifying, Params: `{"private":"sentinel"}`, IdempotencyKey: id, Revision: 1, Status: state, PlanHash: strings.Repeat("a", 64), TargetType: tasks.TargetTypeController, TargetResourceID: "fixture", Source: "system", RequestID: id, CreatedAt: now, UpdatedAt: now, UnstartedDeadline: now.Add(time.Hour)}
		if e = s.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error {
			return s.TasksRepo.Create(c, tx, task, []tasks.Target{{NodeID: "node-1", State: target}})
		}); e != nil {
			t.Fatal(e)
		}
	}
	p := certificates.PublicRenewal{ID: "renew-one", OwnerID: owner, Revision: 1, Enabled: true, OrderTaskID: "old-order", InstallTaskID: "old-install", TaskID: "unknown-order", CSRTaskID: "csr-one", NodeID: "node-1", Domain: "example.com", KeyRef: "key-one", ZoneID: "zone-one", ProfileSHA256: strings.Repeat("a", 64), NotAfter: now.Add(60 * 24 * time.Hour), RenewBeforeDays: 30, UpdatedAt: now}
	if e = s.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error { return certificates.SavePublicRenewalTx(c, tx, p, 0) }); e != nil {
		t.Fatal(e)
	}
	s.PublicCertificates = &publicCertificateExecution{s: s}
	path := "/api/v1/certificates/public-renewals/renew-one/delivery"
	body := map[string]any{"expectedRevision": 0, "policyRevision": 1, "enabled": true, "publicationIds": []string{}}
	if code, _ = request("POST", path, body, ""); code != 403 {
		t.Fatal("CSRF bypass", code)
	}
	if code, b := request("POST", path, body, login.CSRF); code != 200 {
		t.Fatal(code, string(b))
	}
	s.driveRenewalDeliveries(ctx)
	s.driveRenewalDeliveries(ctx)
	d, e := certificates.ReadRenewalDelivery(ctx, s.DB, p.ID)
	if e != nil || d.Cycle == nil || d.Cycle.State != "blocked" {
		t.Fatal("unknown cycle did not stop", d, e)
	}
	if code, b := request("GET", path, nil, ""); code != 200 || bytes.Contains(b, []byte("private-sentinel")) || bytes.Contains(b, []byte(`"private"`)) {
		t.Fatal("metadata authorization/redaction", code, string(b))
	}
	recovery := map[string]any{"expectedRevision": d.Revision, "policyRevision": 1, "orderTaskId": "manual-order", "installTaskId": "manual-install", "publicationIds": []string{}, "incidentIds": []string{}, "note": "independent repair proof", "confirmed": true, "enabled": false}
	if code, b := request("POST", path+"/recover", recovery, login.CSRF); code != 409 {
		t.Fatal("unreviewed UNKNOWN adopted", code, string(b))
	}
	if code, _ = request("POST", path, body, login.CSRF); code != 409 {
		t.Fatal("stale revision accepted", code)
	}
	var count int
	if e = s.DB.QueryRow(ctx, `SELECT count(*) FROM tasks`).Scan(&count); e != nil || count != 3 {
		t.Fatal("unknown was resubmitted", count, e)
	}
	original, e := s.Tasks.Get(ctx, "unknown-order")
	if e != nil || original.Status != tasks.StatusUnknown {
		t.Fatal("source fact was rewritten", e)
	}
	if code, b := request("GET", "/api/v1/installations/catalog", nil, ""); code != 200 || !bytes.Contains(b, []byte(`"available":false`)) {
		t.Fatal(code, string(b))
	}
}
