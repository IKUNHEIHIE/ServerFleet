//go:build cx_r2_controller

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

// Trusted fixture registry is populated only after actual enrollment identities
// exist. No HTTP path/URL can register a root or authorize an execution.
type r2FilePaths struct {
	mu    sync.RWMutex
	roots map[string]string
}

func (p *r2FilePaths) RelativePath(ctx context.Context, node, absolute string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, err := files.NewRootPathMapper(p.roots)
	if err != nil {
		return "", err
	}
	return m.RelativePath(ctx, node, absolute)
}
func (p *r2FilePaths) AbsolutePath(ctx context.Context, node, relative string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, err := files.NewRootPathMapper(p.roots)
	if err != nil {
		return "", err
	}
	return m.AbsolutePath(ctx, node, relative)
}
func TestR2HTTPUnifiedWorkerTwoActualAgentsMultiFileAndUnknown(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("actual non-root Linux Controller plus two Agent processes")
	}
	agentBinary := os.Getenv("SF_R2_AGENT_BINARY")
	if !filepath.IsAbs(agentBinary) {
		t.Fatal("explicit prebuilt actual Agent binary is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	sourceRoot := filepath.Join(dir, "source")
	targetRoot := filepath.Join(dir, "target")
	for _, d := range []string{sourceRoot, targetRoot, filepath.Join(targetRoot, "out")} {
		if err := os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	data := map[string][]byte{"first.bin": bytes.Repeat([]byte("first-data-"), 9000), "second.bin": bytes.Repeat([]byte("second-data-"), 4000), "empty.bin": {}}
	checksums := map[string]string{}
	sourcePaths := []string{}
	for _, name := range []string{"first.bin", "second.bin", "empty.bin"} {
		if err := os.WriteFile(filepath.Join(sourceRoot, name), data[name], 0600); err != nil {
			t.Fatal(err)
		}
		sourcePaths = append(sourcePaths, "/source/"+name)
		checksums["/source/"+name] = sha256Hex(data[name])
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
	cfg := server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin, AgentAuthority: authority, AgentServerName: "localhost", EnrollmentIssuer: managerIssuer(t, pki), FileExecution: &server.FileExecutionConfig{Paths: paths, SpoolDir: filepath.Join(dir, "spool"), ScopeGate: func(context.Context, tasks.Task) error {
		if denyPublished.Load() {
			if _, err := os.Stat(filepath.Join(targetRoot, "out", "uncertain.bin")); err == nil {
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
		r.Header.Set("Idempotency-Key", fmt.Sprintf("r2-fixture-request-%06d", counter.Add(1)))
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
	must("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": setup, "username": "admin", "password": "private-r2-test-password"}, 204)
	raw := must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "private-r2-test-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	csrf = login.CSRF
	type process struct {
		command     *exec.Cmd
		done        chan struct{}
		state, node string
	}
	processes := []process{}
	t.Cleanup(func() {
		for _, p := range processes {
			_ = p.command.Process.Signal(os.Interrupt)
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				p.command.Process.Kill()
				<-p.done
			}
		}
	})
	for i, root := range []string{sourceRoot, targetRoot} {
		raw = must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
		var token struct {
			Token string `json:"token"`
		}
		json.Unmarshal(raw, &token)
		state := filepath.Join(dir, fmt.Sprintf("agent-state-%d", i))
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
		id, node := "", ""
		for _, e := range list.Items {
			if e.Status == "pending" {
				id, node = e.ID, e.NodeID
			}
		}
		if id == "" {
			t.Fatal("missing actual enrollment")
		}
		must("POST", "/api/v1/enrollments/"+id+"/decision", map[string]string{"decision": "approve", "reason": "isolated r2 byte path"}, 200)
		_, stderr, code = runAgentCLI(t, ctx, agentBinary, args...)
		if code != 0 {
			t.Fatalf("issued enrollment %d %s", code, stderr)
		}
		logical := "/source"
		if i == 1 {
			logical = "/target"
		}
		paths.mu.Lock()
		paths.roots[node] = logical
		paths.mu.Unlock()
		must("POST", "/api/v1/resource-grants", map[string]any{"principalId": login.Identity.ID, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.submit", "tasks.read", "tasks.cancel", "files.read", "files.write", "files.transfer"}}, 200)
		logFile, e := os.Create(filepath.Join(dir, fmt.Sprintf("agent-%d.log", i)))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { logFile.Close() })
		command := exec.Command(agentBinary, "-serve", "-state-dir", state, "-controller-url", origin, "-root-cert", rootCert, "-file-root", root, "-node-name", fmt.Sprintf("file-node-%d", i))
		command.Stdout = logFile
		command.Stderr = logFile
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan struct{})
		go func() { command.Wait(); close(done) }()
		processes = append(processes, process{command, done, state, node})
	}
	for _, p := range processes {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, e := composed.Links.FileConnection(ctx, p.node); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("actual Agent did not negotiate file capability")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	body := func(paths []string) map[string]any {
		return map[string]any{"sourceNodeId": processes[0].node, "destinationNodeId": processes[1].node, "sourcePaths": paths, "destinationDir": "/target/out", "conflictPolicy": "skip", "checksums": checksums}
	}
	create := func(input map[string]any) (string, string) {
		t.Helper()
		raw := must("POST", "/api/v1/transfers", input, 202)
		var ids struct{ ID, TaskID string }
		json.Unmarshal(raw, &ids)
		if ids.ID == "" || ids.TaskID == "" {
			t.Fatalf("HTTP omitted task binding %s", raw)
		}
		return ids.ID, ids.TaskID
	}
	wait := func(id, taskID string, expected tasks.TaskStatus) files.Transfer {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		for {
			task, e := composed.Tasks.Get(ctx, taskID)
			if e != nil {
				t.Fatal(e)
			}
			if task.Status.Terminal() {
				if task.Status != expected {
					tr, _ := composed.Files.GetTransfer(ctx, login.Identity.ID, id)
					t.Fatalf("parent terminal %s want %s; transfer %+v", task.Status, expected, tr)
				}
				tr, e := composed.Files.GetTransfer(ctx, login.Identity.ID, id)
				if e != nil {
					t.Fatal(e)
				}
				must("GET", "/api/v1/transfers/"+id, nil, 200)
				must("GET", "/api/v1/tasks/"+taskID, nil, 200)
				return tr
			}
			if time.Now().After(deadline) {
				tr, _ := composed.Files.GetTransfer(ctx, login.Identity.ID, id)
				events, _ := composed.Tasks.Events(ctx, taskID, 0, 100)
				t.Fatalf("parent task timed out: task=%+v transfer=%+v events=%+v scheduler=%v agent0=%s agent1=%s", task, tr, events, composed.Tasks.RunOnce(ctx), readLogTail(filepath.Join(dir, "agent-0.log")), readLogTail(filepath.Join(dir, "agent-1.log")))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	id, taskID := create(body(sourcePaths))
	transfer := wait(id, taskID, tasks.StatusCompleted)
	t.Log("normal multi-file parent completed")
	if transfer.Status != files.TransferCompleted || len(transfer.Items) != 3 {
		t.Fatal(transfer)
	}
	for name, expected := range data {
		actual, e := os.ReadFile(filepath.Join(targetRoot, "out", name))
		if e != nil || !bytes.Equal(actual, expected) || sha256Hex(actual) != sha256Hex(expected) {
			t.Fatal("actual destination bytes/SHA mismatch", name, e)
		}
	}
	var count int
	if err = composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&count); err != nil || count != 6 {
		t.Fatal("not one execution per node/item", count, err)
	}
	// Existing regular destination skip is explicit and does not rewrite bytes.
	id, taskID = create(body(sourcePaths))
	transfer = wait(id, taskID, tasks.StatusCompleted)
	t.Log("explicit skip parent completed")
	for _, item := range transfer.Items {
		if item.Status != files.ItemSkipped {
			t.Fatal("skip silently degraded", item)
		}
	}
	invalid := body(sourcePaths)
	invalid["conflictPolicy"] = "overwrite"
	must("POST", "/api/v1/transfers", invalid, 400)
	// SQLite rejects durable acceptance: no Open and no destination effects.
	os.WriteFile(filepath.Join(sourceRoot, "audit.bin"), []byte("acceptance must persist first"), 0600)
	_, err = composed.DB.Exec(ctx, `CREATE TRIGGER reject_file_accept BEFORE UPDATE ON cx_file_execution_bindings WHEN json_extract(NEW.payload,'$.State')='accepted' BEGIN SELECT RAISE(ABORT,'forced acceptance audit boundary'); END`)
	if err != nil {
		t.Fatal(err)
	}
	input := body([]string{"/source/audit.bin"})
	delete(input, "checksums")
	id, taskID = create(input)
	wait(id, taskID, tasks.StatusUnknown)
	if _, e := os.Stat(filepath.Join(targetRoot, "out", "audit.bin")); !os.IsNotExist(e) {
		t.Fatal("acceptance failure emitted file effects")
	}
	composed.DB.Exec(ctx, `DROP TRIGGER reject_file_accept`)
	// Published bytes followed by current authority loss cannot be reported as
	// parent success or replayed after a real destination reconnect.
	payload := bytes.Repeat([]byte("actual-uncertain-write"), 7000)
	os.WriteFile(filepath.Join(sourceRoot, "uncertain.bin"), payload, 0600)
	denyPublished.Store(true)
	input = body([]string{"/source/uncertain.bin"})
	delete(input, "checksums")
	id, taskID = create(input)
	wait(id, taskID, tasks.StatusUnknown)
	actual, e := os.ReadFile(filepath.Join(targetRoot, "out", "uncertain.bin"))
	if e != nil || !bytes.Equal(actual, payload) {
		t.Fatal("uncertain write fixture did not actually publish bytes", e)
	}
	info, _ := os.Stat(filepath.Join(targetRoot, "out", "uncertain.bin"))
	var before int
	composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&before)
	denyPublished.Store(false)
	// Close/reopen the Controller DB and its real listener; resident Agents use
	// their existing private identity/ledger and reconnect without replay.
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
	for _, p := range processes {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, e := composed.Links.FileConnection(ctx, p.node); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("actual resident Agent did not reconnect")
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	for i := 0; i < 3; i++ {
		if err = composed.Tasks.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var after int
	composed.DB.QueryRow(ctx, `SELECT count(*) FROM cx_file_execution_bindings WHERE task_id=?`, taskID).Scan(&after)
	fresh, _ := os.Stat(filepath.Join(targetRoot, "out", "uncertain.bin"))
	task, _ := composed.Tasks.Get(ctx, taskID)
	if before != after || task.Status != tasks.StatusUnknown || !fresh.ModTime().Equal(info.ModTime()) {
		t.Fatal("restart/reconnect replayed uncertain write")
	}
	t.Log("actual Controller HTTP + unified worker + two resident Agent processes: three items, six child executions, SHA, task terminal, durable acceptance failure and published UNKNOWN restart fence verified")
}
