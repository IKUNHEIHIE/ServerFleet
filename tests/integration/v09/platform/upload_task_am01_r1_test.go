package platform_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
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

// TestAM01R1UploadTaskConvergenceAndCrashRecovery covers the R1 review
// scenarios: convergence of a queued/unstarted upload cancelled through the
// unified task interface, crash of the Controller while the worker is
// streaming, a crash between task submission and upload binding, and the
// actual digest evidence when the client supplies no optional checksum.
func TestAM01R1UploadTaskConvergenceAndCrashRecovery(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("actual non-root Linux Controller plus one Agent process")
	}
	agentBinary, ok := explicitBinary("SF_R2_AGENT_BINARY", "agent")
	if !ok {
		_, agentBinary, _, _ = buildActualProducts(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
		r.Header.Set("Idempotency-Key", fmt.Sprintf("am01r1-request-%06d", counter.Add(1)))
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
	must("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": setup, "username": "admin", "password": "private-am01-r1-test-password"}, 204)
	raw := must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "private-am01-r1-test-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	csrf = login.CSRF
	subject := login.Identity.ID
	// Enroll the real Agent identity first; the resident process starts only
	// when the queued-cancel scenario needs actual execution.
	tokenRaw := must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
	var token struct {
		Token string `json:"token"`
	}
	json.Unmarshal(tokenRaw, &token)
	if token.Token == "" {
		t.Fatal("no enrollment token")
	}
	state := filepath.Join(dir, "agent-state")
	enroll := []string{"-enroll-token", token.Token, "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert}
	if _, stderr, code := runAgentCLI(t, ctx, agentBinary, enroll...); code != 0 {
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
		t.Fatal("pending identity missing")
	}
	must("POST", "/api/v1/enrollments/"+enrollID+"/decision", map[string]string{"decision": "approve", "reason": "am01 r1"}, 200)
	// The issuing run polls the approved enrollment and stores the identity.
	if _, stderr, code := runAgentCLI(t, ctx, agentBinary, enroll...); code != 0 {
		t.Fatalf("issued enrollment %d %s", code, stderr)
	}
	must("POST", "/api/v1/resource-grants", map[string]any{"principalId": subject, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.submit", "tasks.read", "tasks.cancel", "files.read", "files.write", "files.transfer"}}, 200)
	paths.mu.Lock()
	paths.roots[node] = "/target"
	paths.mu.Unlock()

	uploadStatus := func(uploadID string) string {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			body := must("GET", "/api/v1/nodes/"+node+"/files/uploads/"+uploadID, nil, 200)
			var progress struct {
				Status string `json:"status"`
			}
			json.Unmarshal(body, &progress)
			if progress.Status != files.UploadDispatching || time.Now().After(deadline) {
				return progress.Status
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	dispatch := func(name string, payload []byte, withSHA bool) (string, string) {
		t.Helper()
		entry := map[string]any{"name": name, "sizeBytes": len(payload)}
		if withSHA {
			entry["sha256"] = sha256Hex(payload)
		}
		raw := must("POST", "/api/v1/nodes/"+node+"/files/uploads", map[string]any{"destinationDir": "/target/incoming", "conflictPolicy": "overwrite", "files": []any{entry}}, 201)
		var created struct {
			UploadID string `json:"uploadId"`
		}
		json.Unmarshal(raw, &created)
		if created.UploadID == "" {
			t.Fatal("missing upload session")
		}
		r, e := http.NewRequestWithContext(ctx, "PUT", origin+"/api/v1/nodes/"+node+"/files/uploads/"+created.UploadID+"/content?offset=0", bytes.NewReader(payload))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("Idempotency-Key", fmt.Sprintf("am01r1-content-%06d", counter.Add(1)))
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
		complete := must("POST", "/api/v1/nodes/"+node+"/files/uploads/"+created.UploadID+"/complete", nil, 202)
		var accepted struct {
			UploadID, Status, TaskID string
		}
		json.Unmarshal(complete, &accepted)
		if accepted.UploadID != created.UploadID || accepted.Status != files.UploadDispatching || accepted.TaskID == "" {
			t.Fatalf("complete did not dispatch a unified task: %s", complete)
		}
		return created.UploadID, accepted.TaskID
	}

	// 1. A queued upload whose node is offline converges through the unified
	// task interface: HTTP task cancellation finalizes the never-started task
	// and the upload status read converges to cancelled without any write.
	offlinePayload := []byte("queued-until-cancelled")
	offlineUpload, offlineTask := dispatch("offline-cancel.bin", offlinePayload, true)
	must("POST", "/api/v1/tasks/"+offlineTask+"/cancel", map[string]any{"reason": "am01r1-queued-cancel"}, 200)
	if status := uploadStatus(offlineUpload); status != files.UploadCancelled {
		t.Fatalf("queued cancel converged to %q, want cancelled", status)
	}
	if _, e := os.Stat(filepath.Join(incoming, "offline-cancel.bin")); !os.IsNotExist(e) {
		t.Fatal("cancelled queued upload wrote the destination")
	}
	task, e := composed.Tasks.Get(ctx, offlineTask)
	if e != nil || task.Status != tasks.StatusCanceled {
		t.Fatalf("queued task = %v err=%v, want canceled", task.Status, e)
	}

	// The resident Agent process starts; execution paths are live from here.
	logFile, e := os.Create(filepath.Join(dir, "agent-r1.log"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { logFile.Close() })
	command := exec.Command(agentBinary, "-serve", "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert, "-file-root", targetRoot, "-node-name", "upload-node-am01-r1")
	command.Stdout = logFile
	command.Stderr = logFile
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	agentDone := make(chan struct{})
	go func() { command.Wait(); close(agentDone) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-agentDone:
		case <-time.After(5 * time.Second):
			command.Process.Kill()
			<-agentDone
		}
	})
	for deadline := time.Now().Add(20 * time.Second); ; {
		if _, e := composed.Links.FileConnection(ctx, node); e == nil {
			break
		}
		select {
		case <-agentDone:
			raw, _ := os.ReadFile(logFile.Name())
			t.Fatalf("resident Agent exited: %s", raw)
		default:
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(logFile.Name())
			t.Fatalf("resident Agent did not connect: log=%q node=%s", raw, node)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 2. A client that supplies no optional checksum still gets the actual
	// verified digest: the 202 response and the completed record carry it.
	plainPayload := bytes.Repeat([]byte("no-client-sha-"), 900)
	plainUpload, plainTask := dispatch("plain.bin", plainPayload, false)
	computed := sha256Hex(plainPayload)
	uploadStatus(plainUpload)
	// The file worker persists its upload result before returning the result
	// to the unified task worker. Require both durable terminal records, while
	// allowing the bounded asynchronous task-result handoff to complete.
	for deadline := time.Now().Add(10 * time.Second); ; {
		task, e = composed.Tasks.Get(ctx, plainTask)
		if e != nil {
			t.Fatalf("plain upload task read: %v", e)
		}
		if task.Status.Terminal() || time.Now().After(deadline) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if task.Status != tasks.StatusCompleted {
		t.Fatalf("plain upload task = %v", task.Status)
	}
	upload, e := composed.Files.GetUpload(ctx, subject, fmt.Sprintf("am01r1-plain-%d", counter.Add(1)), plainUpload)
	if e != nil || upload.SHA256Actual != computed || upload.Status != files.UploadCompleted {
		t.Fatalf("digest evidence lost: %+v err=%v", upload, e)
	}
	written, e := os.ReadFile(filepath.Join(incoming, "plain.bin"))
	if e != nil || sha256Hex(written) != computed {
		t.Fatalf("plain destination mismatch: %v", e)
	}

	// 3. Controller crash while the worker streams: the frozen first
	// Controller never applies an upload terminal state; the recomposed
	// Controller fences the task UNKNOWN and the upload converges to the
	// honest agent_unknown terminal without any replay.
	crashPayload := bytes.Repeat([]byte("crash-mid-stream-"), 3000)
	crashFile := filepath.Join(incoming, "crash.bin")
	crashEntered := make(chan struct{})
	crashRelease := make(chan struct{})
	var crashOnce sync.Once
	pauseMu.Lock()
	pause = func(gateCtx context.Context, task tasks.Task) error {
		if _, e := os.Stat(crashFile); e == nil {
			crashOnce.Do(func() { close(crashEntered) })
			// Block like a Controller that crashed mid-stream. The release
			// path exists only so the abandoned composition can be closed
			// deterministically after its fence assertions: releasing denies
			// the frame, the paused worker unwinds with an error and every
			// terminal write it may still attempt is already fenced by the
			// exactly-once guard.
			select {
			case <-crashRelease:
				return errors.New("crash simulation released with denial")
			case <-gateCtx.Done():
				return gateCtx.Err()
			}
		}
		return nil
	}
	pauseMu.Unlock()
	crashUpload, crashTask := dispatch("crash.bin", crashPayload, true)
	select {
	case <-crashEntered:
	case <-ctx.Done():
		t.Fatal("mid-stream crash point not reached")
	}
	// Hard-cut the listener only; the in-memory Controller is abandoned like a
	// crashed process instead of being closed gracefully.
	host.Close()
	listener, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if e != nil {
		t.Fatal(e)
	}
	host = httptest.NewUnstartedServer(nil)
	host.Listener.Close()
	host.Listener = listener
	composed2, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = composed2
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pki.roots}
	host.StartTLS()
	t.Cleanup(func() {
		host.Close()
		_ = composed2.Close()
	})
	if status := uploadStatus2(request, t, node, crashUpload); status != files.UploadFailed {
		t.Fatalf("crash-converged upload status = %q, want failed", status)
	}
	upload, e = composed2.Files.GetUpload(ctx, subject, fmt.Sprintf("am01r1-crash-%d", counter.Add(1)), crashUpload)
	if e != nil || upload.ErrorCode != files.UploadErrorAgentUnknown {
		t.Fatalf("crash upload must record agent_unknown: %+v err=%v", upload, e)
	}
	task, e = composed2.Tasks.Get(ctx, crashTask)
	if e != nil || task.Status != tasks.StatusUnknown {
		t.Fatalf("crash task = %v err=%v, want unknown", task.Status, e)
	}
	crashInfo, _ := os.Stat(crashFile)
	var crashBindings int
	composed2.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, crashTask).Scan(&crashBindings)
	for i := 0; i < 3; i++ {
		if err = composed2.Tasks.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var after int
	composed2.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, crashTask).Scan(&after)
	fresh, _ := os.Stat(crashFile)
	task, _ = composed2.Tasks.Get(ctx, crashTask)
	if crashBindings != after || task.Status != tasks.StatusUnknown || !fresh.ModTime().Equal(crashInfo.ModTime()) {
		t.Fatal("recomposition replayed the crashed upload")
	}

	// The frozen first Controller is now closed for good: its paused worker
	// unwinds through the cancelled context (its terminal write is already
	// fenced by the exactly-once guard) and its scheduler stops, so the
	// remaining scenarios run under the recomposed Controller only.
	pauseMu.Lock()
	pause = nil
	pauseMu.Unlock()
	close(crashRelease)
	if e := composed.Close(); e != nil {
		t.Fatal(e)
	}

	// 4. Crash between task submission and upload binding: the orphan task
	// fences UNKNOWN through the linkage timeout, the retry complete binds the
	// same task and converges to agent_unknown; no write ever happens.
	orphanPayload := []byte("orphan-upload-body")
	orphanFile := filepath.Join(incoming, "orphan.bin")
	entry := map[string]any{"name": "orphan.bin", "sizeBytes": len(orphanPayload), "sha256": sha256Hex(orphanPayload)}
	raw = must("POST", "/api/v1/nodes/"+node+"/files/uploads", map[string]any{"destinationDir": "/target/incoming", "conflictPolicy": "overwrite", "files": []any{entry}}, 201)
	var orphan struct {
		UploadID string `json:"uploadId"`
	}
	json.Unmarshal(raw, &orphan)
	r, e := http.NewRequestWithContext(ctx, "PUT", origin+"/api/v1/nodes/"+node+"/files/uploads/"+orphan.UploadID+"/content?offset=0", bytes.NewReader(orphanPayload))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Idempotency-Key", fmt.Sprintf("am01r1-orphan-%06d", counter.Add(1)))
	r.Header.Set("X-Requested-With", "ServerFleetWeb")
	r.Header.Set("X-CSRF-Token", csrf)
	response, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	// Submit the unified task WITHOUT binding the upload, exactly the state a
	// Controller crash leaves between submission and the binding transaction.
	orphanTaskID, e := composed2.FileWorker.SubmitUpload(ctx, files.UploadCommand{
		SubjectID: subject, RequestID: fmt.Sprintf("am01r1-orphan-submit-%d", counter.Add(1)),
		UploadID: orphan.UploadID, NodeID: node, DestinationPath: "/target/incoming/orphan.bin",
		SHA256Expected: sha256Hex(orphanPayload), Size: int64(len(orphanPayload)),
	})
	if e != nil {
		t.Fatal(e)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		task, e = composed2.Tasks.Get(ctx, orphanTaskID)
		if e == nil && task.Status.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphan task did not fence: %v err=%v", task.Status, e)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if task.Status != tasks.StatusUnknown {
		t.Fatalf("orphan task = %v, want unknown", task.Status)
	}
	if _, e := os.Stat(orphanFile); !os.IsNotExist(e) {
		t.Fatal("orphan task wrote the destination before binding")
	}
	// The retry complete returns the same idempotent task and binds the
	// upload; the terminal task then converges the upload without replay.
	complete := must("POST", "/api/v1/nodes/"+node+"/files/uploads/"+orphan.UploadID+"/complete", nil, 202)
	var accepted struct {
		UploadID, Status, TaskID string
	}
	json.Unmarshal(complete, &accepted)
	if accepted.TaskID != orphanTaskID || accepted.Status != files.UploadDispatching {
		t.Fatalf("orphan retry did not bind the same task: %s", complete)
	}
	if status := uploadStatus2(request, t, node, orphan.UploadID); status != files.UploadFailed {
		t.Fatalf("orphan upload converged to %q, want failed", status)
	}
	upload, e = composed2.Files.GetUpload(ctx, subject, fmt.Sprintf("am01r1-orphan2-%d", counter.Add(1)), orphan.UploadID)
	if e != nil || upload.ErrorCode != files.UploadErrorAgentUnknown || upload.TaskID != orphanTaskID {
		t.Fatalf("orphan upload evidence: %+v err=%v", upload, e)
	}
	if _, e := os.Stat(orphanFile); !os.IsNotExist(e) {
		t.Fatal("orphan replay wrote the destination")
	}
	var orphanBindings int
	composed2.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, orphanTaskID).Scan(&orphanBindings)
	if orphanBindings != 0 {
		t.Fatal("orphan task must not reserve a binding", orphanBindings)
	}
	t.Log("R1: queued task-interface cancel convergence, crash-mid-stream UNKNOWN reconciliation without replay, submit/bind interruption fence and checksum-less actual digest evidence verified")
}

func uploadStatus2(request func(string, string, any) (int, []byte), t *testing.T, node, uploadID string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, body := request("GET", "/api/v1/nodes/"+node+"/files/uploads/"+uploadID, nil)
		var progress struct {
			Status string `json:"status"`
		}
		json.Unmarshal(body, &progress)
		if progress.Status != files.UploadDispatching || time.Now().After(deadline) {
			return progress.Status
		}
		time.Sleep(30 * time.Millisecond)
	}
}
