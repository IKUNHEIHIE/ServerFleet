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
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/tasks"
)

// TestAM01UploadHTTPUnifiedTaskActualAgentShaVerified verifies the first upload
// taskification checkpoint with the real products: HTTP upload bytes -> verified
// staging -> unified task dispatch (202) -> frame-authorized push -> actual Agent
// destination file with SHA-256 verification, plus honest cancellation and the
// published-bytes UNKNOWN restart fence.
func TestAM01UploadHTTPUnifiedTaskActualAgentShaVerified(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("actual non-root Linux Controller plus one Agent process")
	}
	agentBinary, ok := explicitBinary("SF_R2_AGENT_BINARY", "agent")
	if !ok {
		_, agentBinary, _, _ = buildActualProducts(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	targetRoot := filepath.Join(dir, "target")
	incoming := filepath.Join(targetRoot, "incoming")
	for _, d := range []string{targetRoot, incoming} {
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
	var denyPublished atomic.Bool
	var pauseMu sync.Mutex
	var pause func(context.Context, tasks.Task) error
	cfg := server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin, AgentAuthority: authority, AgentServerName: "localhost", EnrollmentIssuer: managerIssuer(t, pki), FileExecution: &server.FileExecutionConfig{Paths: paths, SpoolDir: filepath.Join(dir, "spool"), ScopeGate: func(gateCtx context.Context, task tasks.Task) error {
		pauseMu.Lock()
		hook := pause
		pauseMu.Unlock()
		if hook != nil {
			if err := hook(gateCtx, task); err != nil {
				return err
			}
		}
		if denyPublished.Load() {
			if _, err := os.Stat(filepath.Join(incoming, "uncertain.bin")); err == nil {
				return auth.ErrDenied
			}
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
	t.Cleanup(func() { host.Close(); composed.Close() })
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
		r.Header.Set("Idempotency-Key", fmt.Sprintf("am01-fixture-request-%06d", counter.Add(1)))
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
		actual, raw := request(method, route, body)
		if actual != status {
			t.Fatalf("%s %s = %d: %s", method, route, actual, raw)
		}
		return raw
	}
	setup, err := composed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	must("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": setup, "username": "admin", "password": "private-am01-test-password"}, 204)
	raw := must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "private-am01-test-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	csrf = login.CSRF
	raw = must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
	var token struct {
		Token string `json:"token"`
	}
	json.Unmarshal(raw, &token)
	state := filepath.Join(dir, "agent-state")
	args := []string{"-enroll-token", token.Token, "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert}
	_, stderr, code := runAgentCLI(t, ctx, agentBinary, args...)
	if code != 0 {
		t.Fatalf("pending enrollment %d %s", code, stderr)
	}
	raw = must("GET", "/api/v1/enrollments", nil, 200)
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
		t.Fatal("missing actual enrollment")
	}
	must("POST", "/api/v1/enrollments/"+enrollID+"/decision", map[string]string{"decision": "approve", "reason": "isolated am01 upload byte path"}, 200)
	_, stderr, code = runAgentCLI(t, ctx, agentBinary, args...)
	if code != 0 {
		t.Fatalf("issued enrollment %d %s", code, stderr)
	}
	paths.mu.Lock()
	paths.roots[node] = "/target"
	paths.mu.Unlock()
	must("POST", "/api/v1/resource-grants", map[string]any{"principalId": login.Identity.ID, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.submit", "tasks.read", "tasks.cancel", "files.read", "files.write", "files.transfer"}}, 200)
	logFile, e := os.Create(filepath.Join(dir, "agent-0.log"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { logFile.Close() })
	command := exec.Command(agentBinary, "-serve", "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert, "-file-root", targetRoot, "-node-name", "upload-node-am01")
	command.Stdout = logFile
	command.Stderr = logFile
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			command.Process.Kill()
			<-done
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, e := composed.Links.FileConnection(ctx, node); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual Agent did not negotiate file capability", readLogTail(filepath.Join(dir, "agent-0.log")))
		}
		time.Sleep(20 * time.Millisecond)
	}
	repo, err := files.NewRepo(composed.DB)
	if err != nil {
		t.Fatal(err)
	}
	// dispatch opens a resumable staging upload, streams the real bytes and
	// completes it; the unified path must answer 202 with the task binding and
	// never claim a transport result.
	dispatch := func(name string, payload []byte, checksum bool) (string, string) {
		t.Helper()
		uploadBody := map[string]any{"destinationDir": "/target/incoming", "conflictPolicy": "overwrite", "files": []map[string]any{{"name": name, "sizeBytes": len(payload)}}}
		if checksum {
			uploadBody["files"].([]map[string]any)[0]["sha256"] = sha256Hex(payload)
		}
		raw := must("POST", "/api/v1/nodes/"+node+"/files/uploads", uploadBody, 201)
		var opened struct{ UploadID string }
		json.Unmarshal(raw, &opened)
		if opened.UploadID == "" {
			t.Fatalf("missing upload id %s", raw)
		}
		content, e := http.NewRequestWithContext(ctx, "PUT", origin+"/api/v1/nodes/"+node+"/files/uploads/"+opened.UploadID+"/content", bytes.NewReader(payload))
		if e != nil {
			t.Fatal(e)
		}
		content.Header.Set("Origin", origin)
		content.Header.Set("Idempotency-Key", fmt.Sprintf("am01-content-%06d", counter.Add(1)))
		content.Header.Set("X-Requested-With", "ServerFleetWeb")
		if csrf != "" {
			content.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(content)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ = io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("PUT content = %d: %s", response.StatusCode, raw)
		}
		raw = must("POST", "/api/v1/nodes/"+node+"/files/uploads/"+opened.UploadID+"/complete", nil, 202)
		var accepted struct {
			UploadID, Status, TaskID string
		}
		json.Unmarshal(raw, &accepted)
		if accepted.UploadID != opened.UploadID || accepted.Status != files.UploadDispatching || accepted.TaskID == "" {
			t.Fatalf("complete response did not dispatch a unified task: %s", raw)
		}
		return accepted.UploadID, accepted.TaskID
	}
	waitUpload := func(uploadID, taskID string, want tasks.TaskStatus, wantUpload string) {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		for {
			task, e := composed.Tasks.Get(ctx, taskID)
			if e != nil {
				t.Fatal(e)
			}
			upload, _ := repo.GetUpload(ctx, uploadID)
			if task.Status.Terminal() && upload.Status == wantUpload {
				if task.Status != want {
					t.Fatalf("upload %s with task %s want task %s", upload.Status, task.Status, want)
				}
				must("GET", "/api/v1/tasks/"+taskID, nil, 200)
				return
			}
			if task.Status.Terminal() && upload.Status != wantUpload {
				events, _ := composed.Tasks.Events(ctx, taskID, 0, 100)
				t.Fatalf("task %s but upload %s want %s: %+v events=%+v agent=%s", task.Status, upload.Status, wantUpload, upload, events, readLogTail(filepath.Join(dir, "agent-0.log")))
			}
			if time.Now().After(deadline) {
				events, _ := composed.Tasks.Events(ctx, taskID, 0, 100)
				t.Fatalf("unified upload task timed out: task=%+v upload=%+v events=%+v agent=%s", task, upload, events, readLogTail(filepath.Join(dir, "agent-0.log")))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// 1. Happy path: real HTTP bytes reach the actual Agent under the frozen
	// plan, the agent verifies SHA-256 and the upload terminal state follows
	// the observed task result.
	payload := bytes.Repeat([]byte("am01-upload-bytes-"), 4000)
	uploadID, taskID := dispatch("verified.bin", payload, true)
	waitUpload(uploadID, taskID, tasks.StatusCompleted, files.UploadCompleted)
	actual, e := os.ReadFile(filepath.Join(incoming, "verified.bin"))
	if e != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("destination bytes mismatch: %v len=%d", e, len(actual))
	}
	if hex := sha256Hex(actual); hex != sha256Hex(payload) {
		t.Fatal("destination SHA-256 mismatch")
	}
	upload, e := repo.GetUpload(ctx, uploadID)
	if e != nil || upload.SHA256Actual != sha256Hex(payload) || upload.ErrorCode != "" {
		t.Fatalf("upload record terminal evidence wrong: %+v", upload)
	}
	var bindings int
	composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&bindings)
	if bindings != 1 {
		t.Fatal("unified upload must hold exactly one destination reservation", bindings)
	}
	if _, e := os.Stat(filepath.Join(dir, "spool")); e == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "spool"))
		if len(entries) != 0 {
			t.Fatal("completed upload left execution spool residue")
		}
	}
	// 2. Cancellation before the destination write keeps the upload honest:
	// the task cancels and the upload records cancelled, never completed.
	paused := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	pauseMu.Lock()
	pause = func(gateCtx context.Context, task tasks.Task) error {
		once.Do(func() { close(paused) })
		select {
		case <-release:
		case <-gateCtx.Done():
			return gateCtx.Err()
		}
		return nil
	}
	pauseMu.Unlock()
	uploadID, taskID = dispatch("cancelled.bin", []byte("cancelled upload payload"), true)
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("scope gate pause not reached")
	}
	must("POST", "/api/v1/tasks/"+taskID+"/cancel", map[string]any{"reason": "am01-upload-cancel-check"}, 200)
	close(release)
	pauseMu.Lock()
	pause = nil
	pauseMu.Unlock()
	waitUpload(uploadID, taskID, tasks.StatusCanceled, files.UploadCancelled)
	if _, e := os.Stat(filepath.Join(incoming, "cancelled.bin")); !os.IsNotExist(e) {
		t.Fatal("cancelled upload wrote the destination", e)
	}
	// 3. A frame rejection after the agent accepted the session stays UNKNOWN
	// at the task level, the upload records agent_unknown and the restart
	// fence forbids any replay of published bytes.
	os.WriteFile(filepath.Join(incoming, "uncertain.bin"), []byte("stale"), 0600)
	os.Remove(filepath.Join(incoming, "uncertain.bin"))
	payload = bytes.Repeat([]byte("actual-uncertain-upload"), 5000)
	denyPublished.Store(true)
	uploadID, taskID = dispatch("uncertain.bin", payload, true)
	waitUpload(uploadID, taskID, tasks.StatusUnknown, files.UploadFailed)
	upload, _ = repo.GetUpload(ctx, uploadID)
	if upload.ErrorCode != files.UploadErrorAgentUnknown {
		t.Fatalf("uncertain upload must record agent_unknown, got %q", upload.ErrorCode)
	}
	if written, e := os.ReadFile(filepath.Join(incoming, "uncertain.bin")); e != nil {
		t.Fatalf("denial fence did not let the agent publish bytes: %v", e)
	} else if !bytes.HasPrefix(written, payload) && len(written) == 0 {
		t.Fatal("fixture did not actually publish bytes")
	}
	info, _ := os.Stat(filepath.Join(incoming, "uncertain.bin"))
	var before int
	composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&before)
	denyPublished.Store(false)
	// Close/reopen the Controller DB and its real listener; the resident Agent
	// reconnects without replay.
	host.Close()
	if err = composed.Close(); err != nil {
		t.Fatal(err)
	}
	listener, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if e != nil {
		t.Fatal(e)
	}
	host = httptest.NewUnstartedServer(nil)
	host.Listener.Close()
	host.Listener = listener
	composed, err = server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = composed
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pki.roots}
	host.StartTLS()
	reconnectDeadline := time.Now().Add(45 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("resident Agent exited before reconnect: %s", readLogTail(filepath.Join(dir, "agent-0.log")))
		default:
		}
		if _, e := composed.Links.FileConnection(ctx, node); e == nil {
			break
		}
		if time.Now().After(reconnectDeadline) {
			t.Fatalf("actual resident Agent did not reconnect within bounded backoff/handshake: %s", readLogTail(filepath.Join(dir, "agent-0.log")))
		}
		time.Sleep(30 * time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		if err = composed.Tasks.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var after int
	composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&after)
	fresh, _ := os.Stat(filepath.Join(incoming, "uncertain.bin"))
	task, _ := composed.Tasks.Get(ctx, taskID)
	if before != after || task.Status != tasks.StatusUnknown || !fresh.ModTime().Equal(info.ModTime()) {
		t.Fatal("restart/reconnect replayed uncertain upload")
	}
	t.Log("actual Controller HTTP upload -> unified task (202) -> frame-authorized push -> real Agent destination with SHA-256; honest cancel, agent_unknown terminal and published UNKNOWN restart fence verified")
}
