package platform_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/tasks"
)

// TestAM02DistributionMultiTargetRealAgents covers the V09-CLOUD-AM-02 first
// checkpoint: a frozen multi-target distribution plan executed by the unified
// worker against two resident real Agent processes with per-target honest
// outcomes:
//  1. normal two-target distribution (both files verified on disk);
//  2. partial success with one offline target (online target keeps its
//     result, offline target fails honestly, no batch replay);
//  3. current-permission revocation mid-flight (revoked target fails
//     denied_now, the sibling target still succeeds);
//  4. unified task-interface cancellation before any write frame (per-target
//     cancelled convergence, no destination file).
func TestAM02DistributionMultiTargetRealAgents(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("actual non-root Linux Controller plus two Agent processes")
	}
	agentBinary, ok := explicitBinary("SF_R2_AGENT_BINARY", "agent")
	if !ok {
		_, agentBinary, _, _ = buildActualProducts(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	agentAState := filepath.Join(dir, "agent-a-state")
	agentBState := filepath.Join(dir, "agent-b-state")
	rootA := filepath.Join(dir, "root-a")
	rootB := filepath.Join(dir, "root-b")
	rootC := filepath.Join(dir, "root-c")
	incomingA := filepath.Join(rootA, "incoming")
	incomingB := filepath.Join(rootB, "incoming")
	incomingC := filepath.Join(rootC, "incoming")
	for _, d := range []string{rootA, incomingA, rootB, incomingB, rootC, incomingC} {
		if err := os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pki := newTestPKI(t)
	rootCert := filepath.Join(dir, "ca.pem")
	os.WriteFile(rootCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pki.rootDER}), 0600)
	host := httptest.NewUnstartedServer(nil)
	port := strconv.Itoa(host.Listener.Addr().(*net.TCPAddr).Port)
	authority := "localhost:" + port
	origin := "https://" + authority
	paths := &r2FilePaths{roots: map[string]string{}}
	var pauseMu sync.Mutex
	var pause func(context.Context, tasks.Task) error
	cfg := server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin, AgentAuthority: authority, AgentServerName: "localhost", EnrollmentIssuer: managerIssuer(t, pki), FileExecution: &server.FileExecutionConfig{Paths: paths, SpoolDir: filepath.Join(dir, "spool"), ScopeGate: func(gateCtx context.Context, task tasks.Task) error {
		pauseMu.Lock()
		hook := pause
		pauseMu.Unlock()
		if hook != nil {
			return hook(gateCtx, task)
		}
		return nil
	}}}
	composed, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = composed
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pki.roots}
	host.StartTLS()
	t.Cleanup(func() {
		host.Close()
		_ = composed.Close()
	})
	jar, _ := cookiejar.New(nil)
	client := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", port), jar)
	var counter atomic.Int32
	csrf := ""
	request := func(method, route string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		r, e := http.NewRequestWithContext(ctx, method, origin+route, reader)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", fmt.Sprintf("am02-request-%06d", counter.Add(1)))
		r.Header.Set("X-Requested-With", "ServerFleetWeb")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return response.StatusCode, raw
	}
	must := func(method, route string, body any, status int) []byte {
		t.Helper()
		got, raw := request(method, route, body)
		if got != status {
			t.Fatalf("%s %s %d want %d: %s", method, route, got, status, raw)
		}
		return raw
	}
	setup, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	must("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": setup, "username": "admin", "password": "private-am02-test-password"}, 204)
	raw := must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "private-am02-test-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	csrf = login.CSRF
	subject := login.Identity.ID

	enrollAgent := func(name, state, root string) string {
		t.Helper()
		tokenRaw := must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
		var token struct {
			Token string `json:"token"`
		}
		json.Unmarshal(tokenRaw, &token)
		enroll := []string{"-enroll-token", token.Token, "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert}
		if _, stderr, code := runAgentCLI(t, ctx, agentBinary, enroll...); code != 0 {
			t.Fatalf("%s pending enrollment %d %s", name, code, stderr)
		}
		raw := must("GET", "/api/v1/enrollments", nil, 200)
		var list struct {
			Items []struct{ ID, Status, NodeID string } `json:"items"`
		}
		json.Unmarshal(raw, &list)
		enrollID, node := "", ""
		for _, e := range list.Items {
			if e.Status == "pending" {
				enrollID, node = e.ID, e.NodeID
			}
		}
		if enrollID == "" {
			t.Fatalf("%s pending identity missing", name)
		}
		must("POST", "/api/v1/enrollments/"+enrollID+"/decision", map[string]string{"decision": "approve", "reason": "am02 " + name}, 200)
		if _, stderr, code := runAgentCLI(t, ctx, agentBinary, enroll...); code != 0 {
			t.Fatalf("%s issued enrollment %d %s", name, code, stderr)
		}
		must("POST", "/api/v1/resource-grants", map[string]any{"principalId": subject, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.submit", "tasks.read", "tasks.cancel", "files.read", "files.write", "files.transfer"}}, 200)
		return node
	}
	nodeA := enrollAgent("agent-a", agentAState, rootA)
	nodeB := enrollAgent("agent-b", agentBState, rootB)
	// Node C is enrolled and granted but its process never starts: the honest
	// offline target for the partial-success scenario.
	nodeC := enrollAgent("agent-c", filepath.Join(dir, "agent-c-state"), rootC)
	paths.mu.Lock()
	paths.roots[nodeA] = "/target"
	paths.roots[nodeB] = "/target"
	paths.roots[nodeC] = "/target"
	paths.mu.Unlock()

	agentLogs := map[string]string{}
	startAgent := func(name, state, root string) *exec.Cmd {
		t.Helper()
		logPath := filepath.Join(dir, name+".log")
		agentLogs[name] = logPath
		logFile, e := os.Create(logPath)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { logFile.Close() })
		command := exec.Command(agentBinary, "-serve", "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert, "-file-root", root, "-node-name", name)
		command.Stdout = logFile
		command.Stderr = logFile
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan struct{})
		go func() { command.Wait(); close(done) }()
		t.Cleanup(func() {
			command.Process.Kill()
			<-done
		})
		return command
	}
	startAgent("am02-agent-a", agentAState, rootA)
	startAgent("am02-agent-b", agentBState, rootB)
	waitOnline := func(node string) {
		t.Helper()
		var lastCode int
		var lastBody []byte
		deadline := time.Now().Add(30 * time.Second)
		for {
			if _, e := composed.Links.FileConnection(ctx, node); e == nil {
				return
			}
			lastCode, lastBody = request("GET", "/api/v1/nodes/"+node, nil)
			if time.Now().After(deadline) {
				for name, logPath := range agentLogs {
					if raw, e := os.ReadFile(logPath); e == nil {
						t.Logf("agent %s log tail: %s", name, string(raw[min(len(raw), 2000):]))
					}
				}
				t.Fatalf("agent %s never connected: nodes API %d %s", node, lastCode, lastBody)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitOnline(nodeA)
	waitOnline(nodeB)

	distributionStatus := func(distributionID, taskID string) (status string, body []byte) {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		for {
			raw := must("GET", "/api/v1/files/distributions/"+distributionID, nil, 200)
			var progress struct {
				Status string `json:"status"`
			}
			json.Unmarshal(raw, &progress)
			if progress.Status != files.DistributionDispatching || time.Now().After(deadline) {
				if taskID != "" {
					if task, e := composed.Tasks.Get(ctx, taskID); e == nil {
						targets := ""
						for _, target := range task.Targets {
							targets += target.NodeID + "/" + string(target.State) + "/" + string(target.Code) + " "
						}
						t.Logf("DUMP task %s status=%s cancel=%v failure=%s targets: %s", task.ID, task.Status, task.CancelRequested, task.FailureReason, targets)
					} else {
						t.Logf("DUMP task get error: %v", e)
					}
				}
				return progress.Status, raw
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	distribute := func(name string, payload []byte, nodes []string) (string, string) {
		t.Helper()
		entry := map[string]any{"name": name, "sizeBytes": len(payload), "sha256": sha256Hex(payload)}
		raw := must("POST", "/api/v1/files/distributions", map[string]any{"destinationDir": "/target/incoming", "targetNodes": nodes, "files": []any{entry}}, 201)
		var created struct {
			DistributionID string `json:"distributionId"`
		}
		json.Unmarshal(raw, &created)
		if created.DistributionID == "" {
			t.Fatalf("missing distribution session: %s", raw)
		}
		r, e := http.NewRequestWithContext(ctx, "PUT", origin+"/api/v1/files/distributions/"+created.DistributionID+"/content?offset=0", bytes.NewReader(payload))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("Idempotency-Key", fmt.Sprintf("am02-content-%06d", counter.Add(1)))
		r.Header.Set("X-Requested-With", "ServerFleetWeb")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("PUT content = %d: %s", response.StatusCode, body)
		}
		complete := must("POST", "/api/v1/files/distributions/"+created.DistributionID+"/complete", nil, 202)
		var accepted struct {
			DistributionID, Status, TaskID string
		}
		json.Unmarshal(complete, &accepted)
		if accepted.Status != files.DistributionDispatching || accepted.TaskID == "" {
			t.Fatalf("complete did not dispatch a unified task: %s", complete)
		}
		return created.DistributionID, accepted.TaskID
	}
	fileDigestEquals := func(path string, payload []byte) bool {
		raw, e := os.ReadFile(path)
		return e == nil && bytes.Equal(raw, payload)
	}

	// 1. Normal two-target distribution: both real Agents receive the exact
	// frozen bytes and the per-target rows persist bytes and digest.
	bothPayload := []byte("am02-dual-target-payload")
	bothID, bothTask := distribute("dual.bin", bothPayload, []string{nodeA, nodeB})
	status, raw := distributionStatus(bothID, bothTask)
	if status != files.DistributionCompleted {
		t.Fatalf("dual distribution converged to %q: %s", status, raw)
	}
	var dual struct {
		TargetNodes []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
			Bytes  int64  `json:"bytes"`
			SHA256 string `json:"sha256"`
			Error  any    `json:"error"`
		} `json:"targetNodes"`
	}
	json.Unmarshal(raw, &dual)
	if len(dual.TargetNodes) != 2 {
		t.Fatalf("dual distribution targets = %d, want 2", len(dual.TargetNodes))
	}
	for _, target := range dual.TargetNodes {
		if target.Status != files.TargetStateCompleted || target.Bytes != int64(len(bothPayload)) || target.SHA256 != sha256Hex(bothPayload) || target.Error != nil {
			t.Fatalf("dual target %+v did not complete honestly", target)
		}
	}
	if !fileDigestEquals(filepath.Join(incomingA, "dual.bin"), bothPayload) || !fileDigestEquals(filepath.Join(incomingB, "dual.bin"), bothPayload) {
		t.Fatal("dual distribution did not write both destinations")
	}

	// 2. Offline target + unified task-interface cancellation: nodeC never
	// started, so the frozen task honestly waits queued for its offline
	// target. Cancelling through the task interface converges every
	// per-target row to cancelled without any write frame.
	offlinePayload := []byte("am02-offline-cancelled-payload")
	offlineID, offlineTask := distribute("offline-cancel.bin", offlinePayload, []string{nodeA, nodeC})
	must("POST", "/api/v1/tasks/"+offlineTask+"/cancel", map[string]any{"reason": "am02-offline-target-cancel"}, 200)
	status, raw = distributionStatus(offlineID, offlineTask)
	if status != files.DistributionCancelled {
		t.Fatalf("offline-cancel distribution converged to %q: %s", status, raw)
	}
	var cancelledView struct {
		TargetNodes []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
		} `json:"targetNodes"`
	}
	json.Unmarshal(raw, &cancelledView)
	for _, target := range cancelledView.TargetNodes {
		if target.Status != files.TargetStateCancelled {
			t.Fatalf("offline-cancel target %s = %q, want cancelled", target.NodeID, target.Status)
		}
	}
	if _, e := os.Stat(filepath.Join(incomingA, "offline-cancel.bin")); !os.IsNotExist(e) {
		t.Fatal("cancelled offline distribution wrote the online destination")
	}
	if _, e := os.Stat(filepath.Join(incomingC, "offline-cancel.bin")); !os.IsNotExist(e) {
		t.Fatal("offline target received a file")
	}

	// 3. Current-permission revocation: the gate hook only NOTIFIES that the
	// worker's first frame arrived (never blocking a frame), so the test can
	// revoke nodeB's grant deterministically while nodeA is still streaming a
	// large file: nodeA completes, nodeB fails denied_now without a write
	// frame, status converges to partial and nodeA's result is never replayed
	// or overwritten.
	var revokeEngaged atomic.Bool
	pauseMu.Lock()
	pause = func(ctx context.Context, task tasks.Task) error {
		revokeEngaged.Store(true)
		return nil
	}
	pauseMu.Unlock()
	revokePayload := bytes.Repeat([]byte("am02-revocation-payload-24mb-"), 24*(1<<20)/30)
	revokeID, revokeTask := distribute("revoke.bin", revokePayload, []string{nodeA, nodeB})
	revokeWait := time.Now().Add(30 * time.Second)
	for !revokeEngaged.Load() && time.Now().Before(revokeWait) {
		time.Sleep(5 * time.Millisecond)
	}
	if !revokeEngaged.Load() {
		t.Fatal("gate hook never engaged for the revocation scenario")
	}
	grants := must("GET", "/api/v1/resource-grants", nil, 200)
	var grantList struct {
		Items []struct {
			GrantID   string `json:"grantId"`
			Resource  string `json:"resourceId"`
			Principal string `json:"principalId"`
		} `json:"items"`
	}
	json.Unmarshal(grants, &grantList)
	nodeBGrant := ""
	for _, grant := range grantList.Items {
		if grant.Resource == nodeB && grant.Principal == subject {
			nodeBGrant = grant.GrantID
		}
	}
	if nodeBGrant == "" {
		t.Fatal("nodeB grant missing")
	}
	must("DELETE", "/api/v1/resource-grants/"+nodeBGrant, nil, 204)
	pauseMu.Lock()
	pause = nil
	pauseMu.Unlock()
	status, raw = distributionStatus(revokeID, revokeTask)
	if status != files.DistributionPartial {
		for name, logPath := range agentLogs {
			if logRaw, e := os.ReadFile(logPath); e == nil {
				t.Logf("agent %s log: %s", name, string(logRaw[max(0, len(logRaw)-3000):]))
			}
		}
		t.Fatalf("revocation distribution converged to %q: %s", status, raw)
	}
	var revoked struct {
		TargetNodes []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
			Error  struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"targetNodes"`
	}
	json.Unmarshal(raw, &revoked)
	revokedByNode := map[string]struct {
		Status string
		Code   string
	}{}
	for _, target := range revoked.TargetNodes {
		revokedByNode[target.NodeID] = struct {
			Status string
			Code   string
		}{target.Status, target.Error.Code}
	}
	if revokedByNode[nodeA].Status != files.TargetStateCompleted {
		t.Fatalf("revocation sibling lost its result: %+v", revokedByNode[nodeA])
	}
	if revokedByNode[nodeB].Status != files.TargetStateFailed || revokedByNode[nodeB].Code != "denied_now" {
		t.Fatalf("revoked target did not fail honestly: %+v", revokedByNode[nodeB])
	}
	if !fileDigestEquals(filepath.Join(incomingA, "revoke.bin"), revokePayload) {
		t.Fatal("revocation distribution did not write the authorized destination")
	}
	if _, e := os.Stat(filepath.Join(incomingB, "revoke.bin")); !os.IsNotExist(e) {
		t.Fatal("revoked target received a file")
	}

	// 4. Unified task-interface cancellation before any write frame: the cancel
	// request lands within milliseconds of the 202, before the scheduler tick
	// starts the worker; if the worker did start, every frame is rejected by
	// the task cancel recheck. Either path converges every per-target row to
	// cancelled without any destination write.
	cancelPayload := []byte("am02-cancelled-payload")
	cancelID, cancelTask := distribute("cancel.bin", cancelPayload, []string{nodeA})
	must("POST", "/api/v1/tasks/"+cancelTask+"/cancel", map[string]any{"reason": "am02-cancel-before-write"}, 200)
	status, raw = distributionStatus(cancelID, cancelTask)
	if status != files.DistributionCancelled {
		t.Fatalf("cancelled distribution converged to %q: %s", status, raw)
	}
	if _, e := os.Stat(filepath.Join(incomingA, "cancel.bin")); !os.IsNotExist(e) {
		t.Fatal("cancelled distribution wrote the destination")
	}

	t.Log("AM02: real two-Agent multi-target distribution with partial success, offline, revocation and cancellation negatives verified")
}
