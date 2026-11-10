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
	"strings"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/nodes"
)

func TestWave3AIManagementScopeApprovalsAndRootShellGate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pki := newTestPKI(t)

	tlsServer := httptest.NewUnstartedServer(nil)
	portText := strconv.Itoa(tlsServer.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + portText
	composed, err := server.New(ctx, server.Config{
		DBPath:     filepath.Join(dir, "controller-ai.db"),
		Origin:     origin,
		StagingDir: filepath.Join(dir, "staging"),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = composed.Close() }()
	tlsServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pki.serverCert},
	}
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
		if method == http.MethodPost && idempotencyKey != "" {
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

	nodeID := "ai-node-1"
	if _, err := composed.NodesRepo.Create(ctx, composed.DB, nodes.NodeCreateInput{ID: nodeID, Name: "AI Target", Status: nodes.StatusOffline}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	admin := newClient()
	setupToken, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status, data := doReq(admin, "POST", "/api/v1/auth/bootstrap", map[string]string{
		"setupToken": setupToken, "username": "admin", "password": "wave3-ai-admin-password",
	}, ""); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", status, data)
	}
	status, data := doReq(admin, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wave3-ai-admin-password"}, "")
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
	csrf := login.CSRF

	if status, _ := doReq(newClient(), "GET", "/api/v1/ai/security-policy", nil, ""); status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Fatalf("anonymous AI route = %d, want 401/403", status)
	}

	// Explicit role and node grants are still required for the AI actor.
	for _, permission := range []string{"tasks.command", "tasks.submit", "tasks.read", "nodes.read", "host.root"} {
		status, data = doReq(admin, "POST", "/api/v1/resource-grants", map[string]any{
			"principalId": login.Identity.UserID, "resourceType": "node", "resourceId": nodeID,
			"permissions": []string{permission},
		}, csrf)
		if status != http.StatusOK {
			t.Fatalf("grant %s = %d: %s", permission, status, data)
		}
	}

	// 1. New session starts with zero resource grants and zero allowed nodes.
	status, data = doReq(admin, "POST", "/api/v1/ai/sessions", map[string]any{}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("create AI session = %d: %s", status, data)
	}
	var session struct {
		ID             string            `json:"id"`
		ScopeRevision  uint64            `json:"scopeRevision"`
		ResourceGrants []json.RawMessage `json:"resourceGrants"`
		AllowedNodeIDs []string          `json:"allowedNodeIds"`
		Status         string            `json:"status"`
	}
	decode(t, data, &session)
	if session.ID == "" || session.ScopeRevision != 1 || len(session.ResourceGrants) != 0 || len(session.AllowedNodeIDs) != 0 || session.Status != "active" {
		t.Fatalf("unexpected new session: %s", data)
	}

	status, data = doReq(admin, "GET", "/api/v1/ai/security-policy", nil, "")
	if status != http.StatusOK {
		t.Fatalf("security policy = %d: %s", status, data)
	}
	var policy struct {
		DefaultDeny            bool `json:"defaultDeny"`
		ServerSideEnforced     bool `json:"serverSideEnforced"`
		AllowUnscopedDiscovery bool `json:"allowUnscopedDiscovery"`
	}
	decode(t, data, &policy)
	if !policy.DefaultDeny || !policy.ServerSideEnforced || policy.AllowUnscopedDiscovery {
		t.Fatalf("unexpected security policy: %s", data)
	}

	// 2. Fixed self-check takes no arbitrary command; a valid call still needs approval.
	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/grants", map[string]any{
		"resourceType": "node", "resourceId": nodeID, "allowedActions": []string{"tasks.command"},
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("add grant = %d: %s", status, data)
	}
	var session2 struct {
		ScopeRevision  uint64   `json:"scopeRevision"`
		AllowedNodeIDs []string `json:"allowedNodeIds"`
	}
	decode(t, data, &session2)
	if session2.ScopeRevision != 2 || len(session2.AllowedNodeIDs) != 1 || session2.AllowedNodeIDs[0] != nodeID {
		t.Fatalf("unexpected granted session: %s", data)
	}
	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": session2.ScopeRevision, "toolName": "tasks.command",
		"targetNodeIds": []string{nodeID}, "arguments": map[string]string{"command": "uptime"},
	}, csrf)
	if status != http.StatusForbidden || !bytes.Contains(data, []byte("NOT_AUTHORIZED")) {
		t.Fatalf("arbitrary self-check command accepted = %d: %s", status, data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": session2.ScopeRevision, "toolName": "tasks.command",
		"targetNodeIds": []string{nodeID}, "arguments": map[string]string{},
	}, csrf)
	if status != http.StatusForbidden {
		t.Fatalf("execute without approval = %d: %s", status, data)
	}
	if !bytes.Contains(data, []byte("APPROVAL_REQUIRED")) {
		t.Fatalf("expected APPROVAL_REQUIRED: %s", data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/approvals", map[string]any{
		"toolName": "tasks.command", "arguments": map[string]string{},
		"targetNodeIds": []string{nodeID}, "scopeRevision": session2.ScopeRevision,
	}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("request approval = %d: %s", status, data)
	}
	var approval struct {
		ID                     string   `json:"id"`
		State                  string   `json:"state"`
		CanonicalArgumentsHash string   `json:"canonicalArgumentsHash"`
		TargetNodeIDsSnapshot  []string `json:"targetNodeIdsSnapshot"`
		ScopeRevision          uint64   `json:"scopeRevision"`
	}
	decode(t, data, &approval)
	if approval.ID == "" || approval.State != "pending" || len(approval.CanonicalArgumentsHash) != 64 || approval.ScopeRevision != session2.ScopeRevision {
		t.Fatalf("unexpected approval: %s", data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/approvals/"+approval.ID+"/decision", map[string]any{
		"decision": "approve", "note": "reviewed by admin",
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("decide approval = %d: %s", status, data)
	}
	var decided struct {
		State string `json:"state"`
	}
	decode(t, data, &decided)
	if decided.State != "approved" {
		t.Fatalf("approval state = %q: %s", decided.State, data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": session2.ScopeRevision, "toolName": "tasks.command",
		"targetNodeIds": []string{nodeID}, "arguments": map[string]string{},
		"approvalId": approval.ID,
	}, csrf)
	if status != http.StatusAccepted {
		t.Fatalf("execute with approval = %d: %s", status, data)
	}
	var execution struct {
		TaskID   string `json:"taskId"`
		Accepted bool   `json:"accepted"`
	}
	decode(t, data, &execution)
	if !execution.Accepted || execution.TaskID == "" {
		t.Fatalf("unexpected execution result: %s", data)
	}

	// One-time approval: replaying the same call must fail.
	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": session2.ScopeRevision, "toolName": "tasks.command",
		"targetNodeIds": []string{nodeID}, "arguments": map[string]string{},
		"approvalId": approval.ID,
	}, csrf)
	if status != http.StatusConflict {
		t.Fatalf("approval replay = %d: %s", status, data)
	}

	// 3. Full access changes approval mode, never resource scope or root gating.
	status, data = doReq(admin, "PATCH", "/api/v1/ai/sessions/"+session.ID+"/approval-policy", map[string]any{
		"mode": "full_access", "durationSeconds": 900, "userConfirmedFullAccess": false,
	}, csrf)
	if status != http.StatusBadRequest {
		t.Fatalf("full access without confirmation = %d: %s", status, data)
	}
	status, data = doReq(admin, "PATCH", "/api/v1/ai/sessions/"+session.ID+"/approval-policy", map[string]any{
		"mode": "full_access", "durationSeconds": 900, "userConfirmedFullAccess": true,
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("full access policy = %d: %s", status, data)
	}
	var fullPolicy struct {
		Mode          string `json:"mode"`
		EffectiveMode string `json:"effectiveMode"`
		ScopeRevision uint64 `json:"scopeRevision"`
	}
	decode(t, data, &fullPolicy)
	if fullPolicy.Mode != "full_access" || fullPolicy.EffectiveMode != "full_access" {
		t.Fatalf("unexpected full access policy: %s", data)
	}

	// 4. An installed Root profile still requires a fresh human admin approval.
	rootParameters := map[string]string{"profileId": "approved-status", "policySha256": strings.Repeat("a", 64), "arguments": "[]", "timeoutSeconds": "10"}
	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/grants", map[string]any{
		"resourceType": "node", "resourceId": nodeID, "allowedActions": []string{"host.root"},
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("add root grant = %d: %s", status, data)
	}
	var rootSession struct {
		ScopeRevision uint64 `json:"scopeRevision"`
	}
	decode(t, data, &rootSession)
	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": rootSession.ScopeRevision, "toolName": "host.root",
		"targetNodeIds": []string{nodeID}, "arguments": map[string]string{"command": "systemctl restart fleet-agent"},
	}, csrf)
	if status != http.StatusForbidden || !bytes.Contains(data, []byte("NOT_AUTHORIZED")) {
		t.Fatalf("arbitrary Root shell accepted = %d: %s", status, data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": rootSession.ScopeRevision, "toolName": "host.root",
		"targetNodeIds": []string{nodeID}, "arguments": rootParameters,
	}, csrf)
	if status != http.StatusForbidden {
		t.Fatalf("root execute under full access without approval = %d: %s", status, data)
	}
	if !bytes.Contains(data, []byte("APPROVAL_REQUIRED")) {
		t.Fatalf("Root profile bypassed human approval gate: %s", data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/approvals", map[string]any{
		"toolName": "host.root", "arguments": rootParameters,
		"targetNodeIds": []string{nodeID}, "scopeRevision": rootSession.ScopeRevision,
	}, csrf)
	if status != http.StatusCreated {
		t.Fatalf("root approval = %d: %s", status, data)
	}
	var rootApproval struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	decode(t, data, &rootApproval)

	// A non-owner operator cannot decide the approval.
	status, data = doReq(admin, "POST", "/api/v1/users", map[string]any{
		"username": "operator", "displayName": "Operator", "password": "wave3-ai-operator-password", "roleIds": []string{"operator"},
	}, csrf)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create operator = %d: %s", status, data)
	}
	operator := newClient()
	status, data = doReq(operator, "POST", "/api/v1/auth/login", map[string]string{"username": "operator", "password": "wave3-ai-operator-password"}, "")
	if status != http.StatusOK {
		t.Fatalf("operator login = %d: %s", status, data)
	}
	var operatorLogin struct {
		CSRF string `json:"csrfToken"`
	}
	decode(t, data, &operatorLogin)
	status, data = doReq(operator, "POST", "/api/v1/ai/sessions/"+session.ID+"/approvals/"+rootApproval.ID+"/decision", map[string]any{
		"decision": "approve", "note": "operator attempt",
	}, operatorLogin.CSRF)
	if status != http.StatusForbidden {
		t.Fatalf("operator root decision = %d: %s", status, data)
	}

	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/approvals/"+rootApproval.ID+"/decision", map[string]any{
		"decision": "approve", "note": "admin root review",
	}, csrf)
	if status != http.StatusOK {
		t.Fatalf("admin root decision = %d: %s", status, data)
	}
	status, data = doReq(admin, "POST", "/api/v1/ai/tools/execute", map[string]any{
		"sessionId": session.ID, "scopeRevision": rootSession.ScopeRevision, "toolName": "host.root",
		"targetNodeIds": []string{nodeID}, "arguments": rootParameters,
		"approvalId": rootApproval.ID,
	}, csrf)
	if status != http.StatusAccepted {
		t.Fatalf("root execute with approval = %d: %s", status, data)
	}
	var rootExecution struct {
		TaskID string `json:"taskId"`
	}
	decode(t, data, &rootExecution)
	status, data = doReq(admin, "GET", "/api/v1/tasks/"+rootExecution.TaskID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("root task read = %d: %s", status, data)
	}
	var rootTask struct {
		State string `json:"state"`
	}
	decode(t, data, &rootTask)
	if rootTask.State != "awaiting_approval" {
		t.Fatalf("root task state = %q, want awaiting_approval (task approval still applies)", rootTask.State)
	}

	// 5. Messages route exists and honestly reports no configured model.
	status, data = doReq(admin, "POST", "/api/v1/ai/sessions/"+session.ID+"/messages", map[string]any{
		"content": "hello", "clientMessageId": "client-message-0001", "scopeRevision": rootSession.ScopeRevision,
	}, csrf)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("messages without model = %d: %s", status, data)
	}

	// 6. Revoking the grant immediately shrinks the computed allowed node set.
	status, data = doReq(admin, "GET", "/api/v1/ai/sessions/"+session.ID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("get session = %d: %s", status, data)
	}
	var current struct {
		ScopeRevision  uint64 `json:"scopeRevision"`
		ResourceGrants []struct {
			GrantID string `json:"grantId"`
		} `json:"resourceGrants"`
	}
	decode(t, data, &current)
	if len(current.ResourceGrants) != 2 {
		t.Fatalf("expected two grants: %s", data)
	}
	status, data = doReq(admin, "DELETE", "/api/v1/ai/sessions/"+session.ID+"/grants/"+current.ResourceGrants[0].GrantID, nil, csrf)
	if status != http.StatusOK {
		t.Fatalf("revoke grant = %d: %s", status, data)
	}
	var afterRevoke struct {
		ScopeRevision  uint64   `json:"scopeRevision"`
		AllowedNodeIDs []string `json:"allowedNodeIds"`
	}
	decode(t, data, &afterRevoke)
	if afterRevoke.ScopeRevision != current.ScopeRevision+1 || len(afterRevoke.AllowedNodeIDs) != 1 {
		t.Fatalf("revocation snapshot mismatch: %s", data)
	}
}
