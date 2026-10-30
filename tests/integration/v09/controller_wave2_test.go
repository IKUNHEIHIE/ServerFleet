package v09_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

type wave2Dispatcher struct {
	mu       sync.Mutex
	commands []tasks.Command
}

func (d *wave2Dispatcher) Dispatch(_ context.Context, cmd tasks.Command) (tasks.DispatchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, cmd)
	return tasks.DispatchResult{Accepted: true}, nil
}

func (d *wave2Dispatcher) Cancel(context.Context, tasks.CancelCommand) error { return nil }

func TestWave2ControllerEndToEndHTTPSComposition(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "controller-wave2.db")
	dispatcher := &wave2Dispatcher{}

	tlsServer := httptest.NewUnstartedServer(nil)
	origin := "https://" + tlsServer.Listener.Addr().String()
	composed, err := server.New(ctx, server.Config{
		DBPath:     dbPath,
		Origin:     origin,
		StagingDir: filepath.Join(dir, "staging"),
		Dispatcher: dispatcher,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = composed.Close() }()

	tlsServer.Config.Handler = composed
	tlsServer.StartTLS()
	defer tlsServer.Close()

	versions, err := storage.AppliedVersions(ctx, composed.DB)
	if err != nil {
		t.Fatalf("AppliedVersions: %v", err)
	}
	if len(versions) != 19 {
		t.Fatalf("applied migrations = %d (%v), want 19", len(versions), versions)
	}

	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatalf("PrepareSetup: %v", err)
	}

	newClient := func() *http.Client {
		c := tlsServer.Client()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		c.Jar = jar
		return c
	}
	adminClient := newClient()

	doReq := func(client *http.Client, method, path string, body any, csrf, idemKey string) (*http.Response, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, origin+path, reader)
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
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s missing Cache-Control: no-store", method, path)
		}
		return resp, data
	}

	// 1. Bootstrap status -> Bootstrap -> Login -> /auth/me CSRF recovery
	resp, data := doReq(adminClient, "GET", "/api/v1/auth/bootstrap-status", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap-status = %d: %s", resp.StatusCode, data)
	}
	var bootState struct {
		Initialized bool `json:"initialized"`
		Available   bool `json:"bootstrapAvailable"`
	}
	if err := json.Unmarshal(data, &bootState); err != nil || !bootState.Available || bootState.Initialized {
		t.Fatalf("unexpected bootstrap state: %+v (%v)", bootState, err)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken,
		"username":   "admin",
		"password":   "wave2-admin-strong-password",
	}, "", "wave2-bootstrap-0001")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bootstrap = %d: %s", resp.StatusCode, data)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "wave2-admin-strong-password",
	}, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", resp.StatusCode, data)
	}
	var adminLogin struct {
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(data, &adminLogin); err != nil || adminLogin.CSRF == "" {
		t.Fatalf("decode admin login: %v (%s)", err, data)
	}

	resp, data = doReq(adminClient, "GET", "/api/v1/auth/me", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me = %d: %s", resp.StatusCode, data)
	}
	var mePayload struct {
		UserID    string `json:"userId"`
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(data, &mePayload); err != nil || mePayload.CSRFToken != adminLogin.CSRF {
		t.Fatalf("me csrf mismatch: %+v want %s", mePayload, adminLogin.CSRF)
	}

	// 2. Seed two nodes in SQLite and verify zero-grant default even for bootstrap admin
	for _, n := range []nodes.NodeCreateInput{
		{ID: "node-tokyo", Name: "Tokyo Edge", Region: "ap-northeast", Status: nodes.StatusOnline},
		{ID: "node-frankfurt", Name: "Frankfurt Core", Region: "eu-central", Status: nodes.StatusOnline},
	} {
		if _, err := composed.NodesRepo.Create(ctx, composed.DB, n); err != nil {
			t.Fatalf("seed node %s: %v", n.ID, err)
		}
		if _, applied, err := composed.NodesRepo.ApplyAgentUpdate(ctx, composed.DB, nodes.AgentUpdate{
			NodeID:       n.ID,
			Status:       nodes.StatusOnline,
			Sequence:     1,
			ObservedAt:   time.Now().UTC(),
			SampleSource: nodes.SampleSourceAgent,
		}); err != nil || !applied {
			t.Fatalf("agent update %s: %v", n.ID, err)
		}
	}

	resp, data = doReq(adminClient, "GET", "/api/v1/nodes", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes (0 grants) = %d: %s", resp.StatusCode, data)
	}
	var nodeList struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &nodeList); err != nil || len(nodeList.Items) != 0 {
		t.Fatalf("admin got implicit nodes without grant: %+v (%v)", nodeList.Items, err)
	}

	// 3. Grant node-tokyo permissions to admin and verify scoped visibility + group/service-group creation
	resp, data = doReq(adminClient, "POST", "/api/v1/resource-grants", map[string]any{
		"principalId":  adminLogin.Identity.UserID,
		"resourceType": "node",
		"resourceId":   "node-tokyo",
		"permissions": []string{
			"nodes.read",
			"nodes.write",
			"groups.write",
			"servicegroups.read",
			"servicegroups.write",
			"tasks.submit",
			"tasks.read",
		},
	}, adminLogin.CSRF, "wave2-grant-admin-0001")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create grant = %d: %s", resp.StatusCode, data)
	}

	resp, data = doReq(adminClient, "GET", "/api/v1/nodes", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes (with grant) = %d: %s", resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, &nodeList); err != nil || len(nodeList.Items) != 1 || nodeList.Items[0].ID != "node-tokyo" {
		t.Fatalf("expected only [node-tokyo], got %+v (%v)", nodeList.Items, err)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/groups", map[string]any{
		"name":        "APAC Edge",
		"description": "Tokyo nodes",
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create group = %d: %s", resp.StatusCode, data)
	}
	var createdGroup struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}
	if err := json.Unmarshal(data, &createdGroup); err != nil || createdGroup.ID == "" {
		t.Fatalf("decode group: %v (%s)", err, data)
	}
	resp, data = doReq(adminClient, "PUT", "/api/v1/groups/"+createdGroup.ID+"/members", map[string]any{
		"memberNodeIds":    []string{"node-tokyo"},
		"expectedRevision": createdGroup.Revision,
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace group members = %d: %s", resp.StatusCode, data)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/service-groups", map[string]any{
		"name":          "Edge Web",
		"description":   "Tokyo HTTP service",
		"memberNodeIds": []string{"node-tokyo"},
		"service": map[string]any{
			"port":     8080,
			"protocol": "http",
		},
		"loadBalancing": map[string]any{
			"enabled":  true,
			"strategy": "round_robin",
		},
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create service group = %d: %s", resp.StatusCode, data)
	}

	// 4. Create second operator for two-person task approval, preview & submit task, reject self-approval, approve & execute
	resp, data = doReq(adminClient, "POST", "/api/v1/users", map[string]any{
		"username":    "approver",
		"displayName": "Approver Two",
		"password":    "wave2-approver-strong-password",
		"roleIds":     []string{"operator"},
	}, adminLogin.CSRF, "wave2-user-approver-0001")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create approver = %d: %s", resp.StatusCode, data)
	}
	var approverUser struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(data, &approverUser); err != nil || approverUser.UserID == "" {
		t.Fatalf("decode approver: %v (%s)", err, data)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/resource-grants", map[string]any{
		"principalId":  approverUser.UserID,
		"resourceType": "node",
		"resourceId":   "node-tokyo",
		"permissions":  []string{"tasks.approve", "tasks.read"},
	}, adminLogin.CSRF, "wave2-grant-approver-0001")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant approver = %d: %s", resp.StatusCode, data)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/tasks/preview", map[string]any{
		"name":    "patch tokyo",
		"command": "host.patch",
		"target": map[string]any{
			"type":    "node_ids",
			"nodeIds": []string{"node-tokyo"},
		},
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview task = %d: %s", resp.StatusCode, data)
	}

	resp, data = doReq(adminClient, "POST", "/api/v1/tasks", map[string]any{
		"name":    "patch tokyo",
		"command": "host.patch",
		"target": map[string]any{
			"type":    "node_ids",
			"nodeIds": []string{"node-tokyo"},
		},
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit task = %d: %s", resp.StatusCode, data)
	}
	var createdJob struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &createdJob); err != nil || createdJob.State != "awaiting_approval" {
		t.Fatalf("unexpected created job: %+v (%v)", createdJob, err)
	}

	// Fetch task summary to get planDigest
	resp, data = doReq(adminClient, "GET", "/api/v1/tasks", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tasks = %d: %s", resp.StatusCode, data)
	}
	var taskPage struct {
		Items []struct {
			TaskID     string `json:"taskId"`
			PlanDigest string `json:"planDigest"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &taskPage); err != nil || len(taskPage.Items) != 1 {
		t.Fatalf("unexpected task page: %+v (%v)", taskPage, err)
	}
	planDigest := taskPage.Items[0].PlanDigest

	// Self-approval by admin must be rejected with 409 Conflict
	resp, _ = doReq(adminClient, "POST", "/api/v1/tasks/"+createdJob.JobID+"/approve", map[string]any{
		"planDigest": planDigest,
		"decision":   "approve",
		"reason":     "self approve attempt",
	}, adminLogin.CSRF, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("self-approval status = %d, want 409", resp.StatusCode)
	}

	// Second user logs in and approves
	approverClient := newClient()
	resp, data = doReq(approverClient, "POST", "/api/v1/auth/login", map[string]string{
		"username": "approver",
		"password": "wave2-approver-strong-password",
	}, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approver login = %d: %s", resp.StatusCode, data)
	}
	var approverLogin struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(data, &approverLogin); err != nil {
		t.Fatal(err)
	}

	resp, data = doReq(approverClient, "POST", "/api/v1/tasks/"+createdJob.JobID+"/approve", map[string]any{
		"planDigest": planDigest,
		"decision":   "approve",
		"reason":     "peer reviewed",
	}, approverLogin.CSRF, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("peer approve = %d: %s", resp.StatusCode, data)
	}

	if err := composed.Tasks.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(dispatcher.commands) != 1 || dispatcher.commands[0].NodeID != "node-tokyo" {
		t.Fatalf("dispatched commands = %+v", dispatcher.commands)
	}
	if _, err := composed.Tasks.RecordResult(ctx, createdJob.JobID, "node-tokyo", tasks.NodeResultSucceeded, "applied"); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}

	// Verify SSE events projection with a short-lived stream context
	sseCtx, sseCancel := context.WithTimeout(ctx, 2*time.Second)
	sseReq, err := http.NewRequestWithContext(sseCtx, "GET", origin+"/api/v1/tasks/"+createdJob.JobID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	sseReq.Header.Set("Origin", origin)
	sseReq.Header.Set("X-Requested-With", "ServerFleetWeb")
	sseResp, err := adminClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := sseResp.Body.Read(buf)
	sseCancel()
	_ = sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK || !strings.Contains(string(buf[:n]), "event:") {
		t.Fatalf("SSE events = %d: %s", sseResp.StatusCode, string(buf[:n]))
	}

	// 5. Verify durable audit query over HTTPS
	resp, data = doReq(adminClient, "GET", "/api/v1/audit/events?limit=50", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit events = %d: %s", resp.StatusCode, data)
	}
	var auditList struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &auditList); err != nil || len(auditList.Items) < 6 {
		t.Fatalf("expected >= 6 audit events, got %d (%v)", len(auditList.Items), err)
	}
}
