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
	"serverfleet.local/serverfleet/internal/nodes"
)

type workflowHTTPClient struct {
	origin string
	client *http.Client
	ctx    context.Context
	t      *testing.T
}

func (c *workflowHTTPClient) do(method, path string, body any, csrf string) (int, []byte) {
	c.t.Helper()
	var request io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		request = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.origin+path, request)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", c.origin)
	req.Header.Set("X-Requested-With", "ServerFleetWeb")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if body != nil && method == http.MethodPost {
		raw, _ := json.Marshal(body)
		sum := sha256.Sum256(append([]byte(path), raw...))
		req.Header.Set("Idempotency-Key", hex.EncodeToString(sum[:16]))
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, data
}

func TestWave4WorkflowHTTPManagementApprovalAndAudit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	tlsServer := httptest.NewUnstartedServer(nil)
	portText := strconv.Itoa(tlsServer.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + portText
	composed, err := server.New(ctx, server.Config{
		DBPath:     filepath.Join(dir, "controller-workflows.db"),
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

	newClient := func() *workflowHTTPClient {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return &workflowHTTPClient{
			origin: origin,
			client: newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", portText), jar),
			ctx:    ctx, t: t,
		}
	}
	decode := func(t *testing.T, data []byte, value any) {
		t.Helper()
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}

	const grantedNode, ungrantedNode = "workflow-node-granted", "workflow-node-ungranted"
	for _, id := range []string{grantedNode, ungrantedNode} {
		if _, err := composed.NodesRepo.Create(ctx, composed.DB, nodes.NodeCreateInput{ID: id, Name: "Workflow " + id, Status: nodes.StatusOffline}); err != nil {
			t.Fatalf("seed node %s: %v", id, err)
		}
	}

	admin := newClient()
	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status, data := admin.do("POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave4-workflow-admin-password",
	}, ""); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := admin.do("POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wave4-workflow-admin-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login = %d: %s", status, data)
	}
	var login struct {
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &login)
	adminID, adminCSRF := login.Identity.UserID, login.CSRF
	if adminID == "" || adminCSRF == "" {
		t.Fatalf("missing login material: %s", data)
	}
	for _, permission := range []string{"nodes.read", "tasks.command", "tasks.submit", "tasks.approve", "tasks.read", "host.patch"} {
		if status, data := admin.do("POST", "/api/v1/resource-grants", map[string]any{
			"principalId": adminID, "resourceType": "node", "resourceId": grantedNode,
			"permissions": []string{permission},
		}, adminCSRF); status != http.StatusOK {
			t.Fatalf("grant %s = %d: %s", permission, status, data)
		}
	}

	if status, _ := newClient().do("GET", "/api/v1/workflows", nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("anonymous workflow list = %d, want 401", status)
	}

	// Untargeted node is outside the actor's grant snapshot: creation must fail closed.
	if status, data := admin.do("POST", "/api/v1/workflows", map[string]any{
		"trigger": "manual",
		"steps": []map[string]any{{
			"actionId": "host.patch", "nodeIds": []string{ungrantedNode}, "parameters": map[string]string{"command": "uptime"},
		}},
	}, adminCSRF); status != http.StatusForbidden {
		t.Fatalf("ungranted target create = %d: %s", status, data)
	}

	createBody := map[string]any{
		"trigger": "manual",
		"match":   map[string]string{},
		"steps": []map[string]any{{
			"actionId": "host.patch", "nodeIds": []string{grantedNode}, "parameters": map[string]string{"command": "uptime"},
		}},
	}
	status, data = admin.do("POST", "/api/v1/workflows", createBody, adminCSRF)
	if status != http.StatusCreated {
		t.Fatalf("create workflow = %d: %s", status, data)
	}
	var created struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
		Enabled  bool   `json:"enabled"`
		Steps    []struct {
			ActionID string   `json:"actionId"`
			NodeIDs  []string `json:"nodeIds"`
		} `json:"steps"`
	}
	decode(t, data, &created)
	if created.ID == "" || created.Revision != 1 || !created.Enabled || len(created.Steps) != 1 || created.Steps[0].NodeIDs[0] != grantedNode {
		t.Fatalf("unexpected workflow: %s", data)
	}
	workflowID := created.ID

	status, data = admin.do("GET", "/api/v1/workflows", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list workflows = %d: %s", status, data)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, data, &list)
	if len(list.Items) != 1 || list.Items[0].ID != workflowID {
		t.Fatalf("unexpected list: %s", data)
	}

	status, data = admin.do("PATCH", "/api/v1/workflows/"+workflowID, map[string]any{
		"expectedRevision": 1, "enabled": false,
	}, adminCSRF)
	if status != http.StatusOK {
		t.Fatalf("disable = %d: %s", status, data)
	}
	if status, _ := admin.do("POST", "/api/v1/workflows/"+workflowID+"/runs", map[string]any{
		"eventId": "wave4-event-disabled", "kind": "manual", "values": map[string]string{},
	}, adminCSRF); status != http.StatusConflict {
		t.Fatalf("disabled trigger = %d, want 409", status)
	}
	if status, _ := admin.do("PATCH", "/api/v1/workflows/"+workflowID, map[string]any{
		"expectedRevision": 1, "enabled": true,
	}, adminCSRF); status != http.StatusConflict {
		t.Fatalf("stale patch = %d, want 409", status)
	}
	status, data = admin.do("PATCH", "/api/v1/workflows/"+workflowID, map[string]any{
		"expectedRevision": 2, "enabled": true,
	}, adminCSRF)
	if status != http.StatusOK {
		t.Fatalf("re-enable = %d: %s", status, data)
	}

	// Read-only role: can list (empty own scope), cannot create, patch or run.
	status, data = admin.do("POST", "/api/v1/users", map[string]any{
		"username": "workflow-reader", "displayName": "Workflow Reader",
		"password": "wave4-workflow-reader-password", "roleIds": []string{"reader"},
	}, adminCSRF)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create reader = %d: %s", status, data)
	}
	reader := newClient()
	status, data = reader.do("POST", "/api/v1/auth/login", map[string]string{
		"username": "workflow-reader", "password": "wave4-workflow-reader-password",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("reader login = %d: %s", status, data)
	}
	var readerLogin struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &readerLogin)
	if status, data := reader.do("GET", "/api/v1/workflows", nil, ""); status != http.StatusOK {
		t.Fatalf("reader list = %d: %s", status, data)
	}
	if status, _ := reader.do("POST", "/api/v1/workflows", createBody, readerLogin.CSRF); status != http.StatusForbidden {
		t.Fatalf("reader create = %d, want 403", status)
	}
	if status, _ := reader.do("PATCH", "/api/v1/workflows/"+workflowID, map[string]any{
		"expectedRevision": 3, "enabled": false,
	}, readerLogin.CSRF); status != http.StatusForbidden {
		t.Fatalf("reader patch = %d, want 403", status)
	}
	if status, _ := reader.do("POST", "/api/v1/workflows/"+workflowID+"/runs", map[string]any{
		"eventId": "wave4-event-reader", "kind": "manual", "values": map[string]string{},
	}, readerLogin.CSRF); status != http.StatusForbidden {
		t.Fatalf("reader trigger = %d, want 403", status)
	}
	if status, _ := reader.do("GET", "/api/v1/workflows/"+workflowID, nil, ""); status != http.StatusNotFound {
		t.Fatalf("foreign workflow detail = %d, want 404", status)
	}

	// Triggering records a frozen plan and submits the step into the sole task
	// pipeline. host.patch is modifying, so the task must await human approval.
	status, data = admin.do("POST", "/api/v1/workflows/"+workflowID+"/runs", map[string]any{
		"eventId": "wave4-event-1", "kind": "manual", "values": map[string]string{"requested_by": "admin"},
	}, adminCSRF)
	if status != http.StatusAccepted {
		t.Fatalf("trigger = %d: %s", status, data)
	}
	var triggered struct {
		Matched bool `json:"matched"`
		Run     struct {
			ID    string `json:"runId"`
			State string `json:"state"`
			Steps []struct {
				TaskID string `json:"taskId"`
				State  string `json:"state"`
			} `json:"steps"`
		} `json:"run"`
	}
	decode(t, data, &triggered)
	if !triggered.Matched || triggered.Run.ID == "" || len(triggered.Run.Steps) != 1 || triggered.Run.Steps[0].TaskID == "" {
		t.Fatalf("unexpected trigger response: %s", data)
	}
	taskID := triggered.Run.Steps[0].TaskID

	status, data = admin.do("GET", "/api/v1/tasks/"+taskID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("get workflow task = %d: %s", status, data)
	}
	var task struct {
		JobID   string `json:"jobId"`
		Name    string `json:"name"`
		State   string `json:"state"`
		Results []struct {
			NodeID string `json:"nodeId"`
		} `json:"results"`
		Revision  uint64   `json:"revision"`
		PlanHash  string   `json:"planDigest"`
		TargetIDs []string `json:"targetNodeIdsSnapshot"`
	}
	decode(t, data, &task)
	if task.State != "awaiting_approval" || len(task.TargetIDs) != 1 || task.TargetIDs[0] != grantedNode {
		t.Fatalf("workflow task did not enter approval: %s", data)
	}

	// The submitter cannot approve their own automating task either.
	if status, data := admin.do("POST", "/api/v1/tasks/"+taskID+"/approve", map[string]any{
		"planDigest": task.PlanHash, "decision": "approve", "reason": "self approval attempt",
	}, adminCSRF); status != http.StatusConflict {
		t.Fatalf("self approval = %d: %s", status, data)
	}

	// Replaying the same trigger event cannot duplicate side effects.
	if status, data := admin.do("POST", "/api/v1/workflows/"+workflowID+"/runs", map[string]any{
		"eventId": "wave4-event-1", "kind": "manual", "values": map[string]string{"requested_by": "admin"},
	}, adminCSRF); status != http.StatusAccepted {
		t.Fatalf("replayed trigger = %d: %s", status, data)
	}
	status, data = admin.do("GET", "/api/v1/tasks", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list tasks = %d: %s", status, data)
	}
	var tasksPage struct {
		Items []struct {
			TaskID string `json:"taskId"`
			State  string `json:"state"`
		} `json:"items"`
	}
	decode(t, data, &tasksPage)
	matching := 0
	for _, item := range tasksPage.Items {
		if item.TaskID == taskID {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("workflow task duplicated: %s", data)
	}

	status, data = admin.do("GET", "/api/v1/workflows/"+workflowID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("detail = %d: %s", status, data)
	}
	var detail struct {
		Runs []struct {
			ID string `json:"runId"`
		} `json:"runs"`
	}
	decode(t, data, &detail)
	if len(detail.Runs) != 1 || detail.Runs[0].ID != triggered.Run.ID {
		t.Fatalf("run detail = %s", data)
	}

	for _, action := range []string{"workflows.save", "workflows.trigger", "workflows.task.submit"} {
		status, data = admin.do("GET", "/api/v1/audit/events?action="+action+"&limit=10", nil, "")
		if status != http.StatusOK {
			t.Fatalf("audit %s = %d: %s", action, status, data)
		}
		var page struct {
			Items []struct {
				Action string `json:"action"`
				Actor  string `json:"actorId"`
				TaskID string `json:"taskId"`
			} `json:"items"`
		}
		decode(t, data, &page)
		found := false
		for _, item := range page.Items {
			if item.Action != action {
				continue
			}
			found = true
			if item.Actor != adminID && item.Actor != "workflow-reader" {
				t.Fatalf("unexpected audit actor for %s: %s", action, data)
			}
		}
		if !found {
			t.Fatalf("missing durable audit action %s: %s", action, data)
		}
	}
}
