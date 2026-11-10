package platform_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"strconv"
	"strings"
	"testing"
	"time"
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
			sum := sha256.Sum256([]byte(method + path + body))
			req.Header.Set("Idempotency-Key", hex.EncodeToString(sum[:16]))
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
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
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
	// R30: real Controller/SQLite/HTTPS metadata flow. Seed historical task facts;
	// this does not simulate Agent execution or grant any new execution capability.
	for _, id := range []string{"closure-node", "other-node"} {
		if _, e = composed.NodesRepo.Create(ctx, composed.DB, nodes.NodeCreateInput{ID: id, Name: id, Status: nodes.StatusOffline}); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"closure-node", "other-node"} {
		body, _ := json.Marshal(map[string]any{"principalId": login.Identity.UserID, "resourceType": "node", "resourceId": id, "permissions": []string{"tasks.read"}})
		if status, response := request("POST", "/api/v1/resource-grants", string(body), login.CSRF); status != 200 {
			t.Fatal("explicit task read grant", status, string(response))
		}
	}
	original := tasks.Task{ID: "closure-original", Name: "synthetic-private-task-name", SubmitterID: login.Identity.UserID, ActionID: "host.patch", OperationClass: tasks.OperationClassStandard, Params: `{"value":"synthetic-private-params"}`, Revision: 3, Status: tasks.StatusUnknown, PlanHash: strings.Repeat("a", 64), TargetType: "node_ids", RequestID: "closure-source", Source: "web", CreatedAt: time.Now().UTC().Add(-time.Minute), UpdatedAt: time.Now().UTC(), UnstartedDeadline: time.Now().UTC().Add(time.Hour)}
	if e = composed.TasksRepo.Create(ctx, composed.DB, original, []tasks.Target{{NodeID: "closure-node", State: tasks.TargetUnknown, Code: tasks.NodeResultUnknown, Message: "synthetic-private-task-output"}}); e != nil {
		t.Fatal(e)
	}
	if code, raw = request("POST", "/api/v1/operations/incidents/sync", `{}`, login.CSRF); code != 200 {
		t.Fatal(code, string(raw))
	}
	code, raw = request("GET", "/api/v1/operations/incidents?taskId=closure-original&limit=1", "", "")
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var page struct {
		Items []tasks.Incident `json:"items"`
	}
	if e = json.Unmarshal(raw, &page); e != nil || len(page.Items) != 1 {
		t.Fatal(e, string(raw))
	}
	v := page.Items[0]
	if bytes.Contains(raw, []byte("synthetic-private")) {
		t.Fatal("raw task text in operator facts")
	}
	sequence := 0
	apply := func(input tasks.IncidentDecision, want int) {
		t.Helper()
		sequence++
		input.ExpectedRevision = v.Revision
		input.RequestID = fmt.Sprintf("closure-request-%03d", sequence)
		if input.Note == "" {
			input.Note = "Synthetic operator investigation and review"
		}
		body, _ := json.Marshal(input)
		status, response := request("POST", "/api/v1/operations/incidents/"+v.ID+"/actions", string(body), login.CSRF)
		if status != want {
			t.Fatal(input.Action, status, string(response))
		}
		if status == 200 {
			if err := json.Unmarshal(response, &v); err != nil {
				t.Fatal(err)
			}
		}
	}
	apply(tasks.IncidentDecision{Action: "claim"}, 200)
	for _, secret := range []string{"HTTPS://example.invalid/log?%74oken=do-not-store", "https://user:pass@example.invalid/log", "https://example.invalid/log?X-Amz-Signature=do-not-store"} {
		apply(tasks.IncidentDecision{Action: "note", Note: secret}, 403)
	}
	if code, raw = request("GET", "/api/v1/operations/incidents?limit=101", "", ""); code != 400 {
		t.Fatal(code, string(raw))
	}
	if code, raw = request("GET", "/api/v1/operations/incidents?state=%xx", "", ""); code != 400 {
		t.Fatal(code, string(raw))
	}

	apply(tasks.IncidentDecision{Action: "propose", Outcome: "confirmed_succeeded"}, 409)
	for _, category := range []string{"actual_state", "execution_log", "impact_check"} {
		now := time.Now().UTC()
		apply(tasks.IncidentDecision{Action: "record_evidence", EvidenceCategory: category, EvidenceReference: "synthetic-journal-reference", ObservedAt: &now}, 200)
	}
	if code, _ = request("POST", "/api/v1/operations/incidents/"+v.ID+"/actions", `{"expectedRevision":1,"requestId":"no-csrf","action":"reconcile","note":"no csrf"}`, ""); code != 403 {
		t.Fatal("closure CSRF bypass", code)
	}
	follow := original
	follow.ID = "closure-follow"
	follow.Name = "Synthetic follow-up"
	follow.Params = "{}"
	follow.ActionID = "host.selfcheck"
	follow.Revision = 1
	follow.Status = tasks.StatusRunning
	follow.CreatedAt = time.Now().UTC()
	follow.PlanHash = strings.Repeat("b", 64)
	if e = composed.TasksRepo.Create(ctx, composed.DB, follow, []tasks.Target{{NodeID: "closure-node", State: tasks.TargetDispatched, Code: tasks.NodeResultUnknown}}); e != nil {
		t.Fatal(e)
	}
	unrelated := follow
	unrelated.ID = "closure-unrelated"
	unrelated.CreatedAt = time.Now().UTC()
	unrelated.PlanHash = strings.Repeat("c", 64)
	if e = composed.TasksRepo.Create(ctx, composed.DB, unrelated, []tasks.Target{{NodeID: "other-node", State: tasks.TargetDispatched, Code: tasks.NodeResultUnknown}}); e != nil {
		t.Fatal(e)
	}
	apply(tasks.IncidentDecision{Action: "link_task", FollowupTaskID: unrelated.ID}, 409)
	apply(tasks.IncidentDecision{Action: "link_task", FollowupTaskID: follow.ID}, 200)
	apply(tasks.IncidentDecision{Action: "reconcile"}, 200)
	checks := &tasks.IncidentChecks{ActualStateChecked: true, ExecutionLogsChecked: true, ImpactChecked: true}
	apply(tasks.IncidentDecision{Action: "propose", Outcome: "confirmed_succeeded", ObservationDigest: v.ObservationDigest, Checks: checks}, 409)
	if _, e = composed.DB.Exec(ctx, `UPDATE tasks SET status='completed',revision=revision+1,updated_at=? WHERE id=?`, storage.EncodeTime(time.Now().UTC()), follow.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = composed.DB.Exec(ctx, `UPDATE task_targets SET state='succeeded',result_code='succeeded' WHERE task_id=?`, follow.ID); e != nil {
		t.Fatal(e)
	}
	apply(tasks.IncidentDecision{Action: "reconcile"}, 200)
	apply(tasks.IncidentDecision{Action: "propose", Outcome: "confirmed_no_effect", ObservationDigest: v.ObservationDigest, Checks: checks}, 200)
	apply(tasks.IncidentDecision{Action: "approve", ObservationDigest: v.Proposal.ObservationDigest, ReviewAcknowledged: true}, 409)
	code, raw = request("GET", "/api/v1/operations/incidents/"+v.ID+"/evidence", "", "")
	if code != 200 || bytes.Contains(raw, []byte("synthetic-private")) {
		t.Fatal("evidence page", code, string(raw))
	}
	var ledger struct {
		Items []tasks.IncidentEvidence `json:"items"`
	}
	if e = json.Unmarshal(raw, &ledger); e != nil || len(ledger.Items) != 5 {
		t.Fatal(e, len(ledger.Items))
	}
	if code, _ = request("GET", "/api/v1/operations/incidents/"+v.ID+"/evidence?after=-1", "", ""); code != 400 {
		t.Fatal("negative evidence cursor", code)
	}
	if _, e = composed.DB.Exec(ctx, `UPDATE tasks SET status='failed',revision=revision+1 WHERE id=?`, original.ID); e != nil {
		t.Fatal(e)
	}
	code, raw = request("GET", "/api/v1/operations/incidents/"+v.ID, "", "")
	if code != 200 || json.Unmarshal(raw, &v) != nil || v.Source.State != "unknown" || v.Assessment == nil || !v.Assessment.SourceChanged {
		t.Fatal("source history disappeared", code, string(raw))
	}
	stored, e := composed.Tasks.Get(ctx, follow.ID)
	if e != nil || stored.Status != tasks.StatusCompleted {
		t.Fatal("case actions changed follow-up execution", stored, e)
	}

}
