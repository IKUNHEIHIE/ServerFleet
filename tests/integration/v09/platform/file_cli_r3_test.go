package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/tasks"
)

type r3Process struct {
	command *exec.Cmd
	done    chan struct{}
	log     string
}

func startR3Process(t *testing.T, binary string, args ...string) *r3Process {
	t.Helper()
	logname := filepath.Join(t.TempDir(), "process.log")
	f, err := os.OpenFile(logname, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err = cmd.Start(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	p := &r3Process{cmd, make(chan struct{}), logname}
	go func() { cmd.Wait(); f.Close(); close(p.done) }()
	t.Cleanup(func() { p.stop(t) })
	return p
}
func (p *r3Process) stop(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		return
	default:
	}
	p.command.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(8 * time.Second):
		p.command.Process.Kill()
		<-p.done
		t.Error("owned CLI needed force stop", readLogTail(p.log))
	}
}

type r3Client struct {
	t            *testing.T
	ctx          context.Context
	http         *http.Client
	origin, csrf string
	sequence     atomic.Int32
}

func (c *r3Client) request(method, route string, body any) (int, []byte) {
	c.t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(c.ctx, method, c.origin+route, input)
	if err != nil {
		c.t.Fatal(err)
	}
	r.Header.Set("Origin", c.origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Requested-With", "ServerFleetWeb")
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("Idempotency-Key", fmt.Sprintf("r3-isolated-request-%06d", c.sequence.Add(1)))
	resp, err := c.http.Do(r)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, raw
}
func (c *r3Client) must(method, route string, body any, status int) []byte {
	c.t.Helper()
	got, raw := c.request(method, route, body)
	if got != status {
		c.t.Fatalf("%s %s %d want %d: %s", method, route, got, status, raw)
	}
	return raw
}
func (c *r3Client) waitHealthy(p *r3Process) {
	c.t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for {
		select {
		case <-p.done:
			c.t.Fatal("CLI exited", readLogTail(p.log))
		default:
		}
		r, err := c.http.Get(c.origin + "/healthz")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			c.t.Fatal("CLI did not listen", readLogTail(p.log))
		}
		time.Sleep(30 * time.Millisecond)
	}
}
func TestR3TrustedFileDeploymentActualControllerAndTwoAgentCLI(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("non-root local Linux CLI deployment")
	}
	controller, agent := os.Getenv("SF_R3_CONTROLLER_BINARY"), os.Getenv("SF_R3_AGENT_BINARY")
	if !filepath.IsAbs(controller) || !filepath.IsAbs(agent) {
		t.Skip("explicit prebuilt actual Controller/Agent required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	origin := "https://localhost:" + port
	options := bootstrapOptions(dir, port, origin)
	// Trusted local installer fixture prepares the initial identity and local CA;
	// all enrollment/grants/transfers below use the standalone CLI's HTTPS API.
	seed, material, err := composeController(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := seed.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = seed.Identity.Bootstrap(ctx, setup, "admin", "r3-synthetic-cli-password"); err != nil {
		t.Fatal(err)
	}
	writeLocalCAServerCertificate(t, material, options.TLSCert, options.TLSKey)
	seed.Close()
	jar, _ := cookiejar.New(nil)
	client := &r3Client{t: t, ctx: ctx, http: localCAHTTPClient(material.Roots, "127.0.0.1:"+port, jar), origin: origin}
	args := []string{"-listen", options.Listen, "-db", options.DBPath, "-origin", origin, "-tls-cert", options.TLSCert, "-tls-key", options.TLSKey}
	p := startR3Process(t, controller, args...)
	client.waitHealthy(p)
	raw := client.must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "r3-synthetic-cli-password"}, 200)
	var login struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	json.Unmarshal(raw, &login)
	client.csrf = login.CSRF
	var nodes, states, roots []string
	for i := 0; i < 2; i++ {
		raw = client.must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201)
		var token struct {
			Token string `json:"token"`
		}
		json.Unmarshal(raw, &token)
		state := filepath.Join(dir, fmt.Sprintf("identity-%d", i))
		enroll := []string{"-enroll-token", token.Token, "-state-dir", state, "-controller-url", origin, "-root-cert", material.CertificatePath}
		if _, stderr, code := runAgentCLI(t, ctx, agent, enroll...); code != 0 {
			t.Fatal(code, stderr)
		}
		raw = client.must("GET", "/api/v1/enrollments", nil, 200)
		var pending struct {
			Items []struct{ ID, NodeID, Status string } `json:"items"`
		}
		json.Unmarshal(raw, &pending)
		id, node := "", ""
		for _, e := range pending.Items {
			if e.Status == "pending" {
				id, node = e.ID, e.NodeID
			}
		}
		if id == "" {
			t.Fatal("pending identity missing")
		}
		client.must("POST", "/api/v1/enrollments/"+id+"/decision", map[string]string{"decision": "approve", "reason": "isolated R3 CLI"}, 200)
		if _, stderr, code := runAgentCLI(t, ctx, agent, enroll...); code != 0 {
			t.Fatal(code, stderr)
		}
		client.must("POST", "/api/v1/resource-grants", map[string]any{"principalId": login.Identity.ID, "resourceType": "node", "resourceId": node, "permissions": []string{"nodes.read", "tasks.submit", "tasks.read", "files.read", "files.write", "files.transfer"}}, 200)
		root := filepath.Join(dir, fmt.Sprintf("file-root-%d", i))
		if os.Mkdir(root, 0700) != nil {
			t.Fatal("private root")
		}
		nodes = append(nodes, node)
		states = append(states, state)
		roots = append(roots, root)
	}
	data := bytes.Repeat([]byte("actual-cli-file-byte-path"), 2600)
	os.WriteFile(filepath.Join(roots[0], "source.bin"), data, 0600)
	os.Mkdir(filepath.Join(roots[1], "out"), 0700)
	config := filepath.Join(dir, "files.json")
	document := map[string]any{"version": 1, "spoolDir": filepath.Join(dir, "spool"), "nodeRoots": []map[string]string{{"nodeId": nodes[0], "logicalRoot": "/source"}, {"nodeId": nodes[1], "logicalRoot": "/target"}}}
	raw, _ = json.Marshal(document)
	os.WriteFile(config, raw, 0600)
	p.stop(t)
	p = startR3Process(t, controller, append(args, "-file-deployment", config)...)
	client.waitHealthy(p)
	for i := range nodes {
		startR3Process(t, agent, "-serve", "-state-dir", states[i], "-controller-url", origin, "-root-cert", material.CertificatePath, "-file-root", roots[i])
	}
	for _, node := range nodes {
		deadline := time.Now().Add(15 * time.Second)
		for {
			raw = client.must("GET", "/api/v1/nodes/"+node+"/diagnostics", nil, 200)
			var item struct {
				Status string `json:"status"`
			}
			json.Unmarshal(raw, &item)
			if item.Status == "online" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("Agent not online", string(raw))
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	transferBody := map[string]any{"sourceNodeId": nodes[0], "destinationNodeId": nodes[1], "sourcePaths": []string{"/source/source.bin"}, "destinationDir": "/target/out", "conflictPolicy": "skip"}
	for run := 0; run < 2; run++ {
		raw = client.must("POST", "/api/v1/transfers", transferBody, 202)
		var created struct{ ID, TaskID string }
		json.Unmarshal(raw, &created)
		deadline := time.Now().Add(20 * time.Second)
		for {
			raw = client.must("GET", "/api/v1/tasks/"+created.TaskID, nil, 200)
			var task struct {
				State   tasks.TaskStatus `json:"state"`
				Targets []string         `json:"targetNodeIdsSnapshot"`
			}
			json.Unmarshal(raw, &task)
			if task.State.Terminal() {
				if task.State != tasks.StatusCompleted || len(task.Targets) != 2 {
					t.Fatal("actual parent did not complete", string(raw))
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("actual CLI transfer timed out", string(raw), readLogTail(p.log))
			}
			time.Sleep(30 * time.Millisecond)
		}
		raw = client.must("GET", "/api/v1/transfers/"+created.ID, nil, 200)
		var tr files.Transfer
		json.Unmarshal(raw, &tr)
		if tr.Status != files.TransferCompleted {
			t.Fatal(string(raw))
		}
		got, err := os.ReadFile(filepath.Join(roots[1], "out", "source.bin"))
		if err != nil || !bytes.Equal(got, data) || sha256Hex(got) != sha256Hex(data) {
			t.Fatal("target bytes/SHA differ", err)
		}
	}
	runTransfer := func(body map[string]any) files.Transfer {
		t.Helper()
		raw := client.must("POST", "/api/v1/transfers", body, 202)
		var created struct{ ID, TaskID string }
		json.Unmarshal(raw, &created)
		deadline := time.Now().Add(20 * time.Second)
		for {
			raw = client.must("GET", "/api/v1/tasks/"+created.TaskID, nil, 200)
			var task struct {
				State tasks.TaskStatus `json:"state"`
			}
			json.Unmarshal(raw, &task)
			if task.State.Terminal() {
				if task.State != tasks.StatusCompleted {
					t.Fatal("policy transfer did not complete", string(raw), readLogTail(p.log))
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("policy transfer timed out", string(raw))
			}
			time.Sleep(30 * time.Millisecond)
		}
		raw = client.must("GET", "/api/v1/transfers/"+created.ID, nil, 200)
		var tr files.Transfer
		json.Unmarshal(raw, &tr)
		if tr.Status != files.TransferCompleted {
			t.Fatal(string(raw))
		}
		return tr
	}
	// Overwrite replaces the existing target through the unified task/approval
	// path; rename publishes to a reported sibling and leaves the original.
	overwrite := bytes.Repeat([]byte("o"), 65000)
	if err = os.WriteFile(filepath.Join(roots[0], "source.bin"), overwrite, 0600); err != nil {
		t.Fatal(err)
	}
	policyBase := map[string]any{"sourceNodeId": nodes[0], "destinationNodeId": nodes[1], "sourcePaths": []string{"/source/source.bin"}, "destinationDir": "/target/out"}
	overwriteBody := map[string]any{}
	for key, value := range policyBase {
		overwriteBody[key] = value
	}
	overwriteBody["conflictPolicy"] = "overwrite"
	tr := runTransfer(overwriteBody)
	if len(tr.Items) != 1 || tr.Items[0].Status != files.ItemCopied || tr.Items[0].DestinationPath != "/target/out/source.bin" {
		t.Fatal("overwrite item", string(raw))
	}
	if got, e := os.ReadFile(filepath.Join(roots[1], "out", "source.bin")); e != nil || !bytes.Equal(got, overwrite) {
		t.Fatal("overwrite target bytes", e)
	}
	renameBody := map[string]any{}
	for key, value := range policyBase {
		renameBody[key] = value
	}
	renameBody["conflictPolicy"] = "rename"
	tr = runTransfer(renameBody)
	if len(tr.Items) != 1 || tr.Items[0].Status != files.ItemCopied || tr.Items[0].DestinationPath != "/target/out/source (1).bin" {
		t.Fatal("rename item", string(raw))
	}
	if got, e := os.ReadFile(filepath.Join(roots[1], "out", "source (1).bin")); e != nil || !bytes.Equal(got, overwrite) {
		t.Fatal("renamed target bytes", e)
	}
	if got, e := os.ReadFile(filepath.Join(roots[1], "out", "source.bin")); e != nil || !bytes.Equal(got, overwrite) {
		t.Fatal("rename replaced the original", e)
	}
	bad := map[string]any{}
	for key, value := range transferBody {
		bad[key] = value
	}
	bad["destinationDir"] = "/unregistered/out"
	client.must("POST", "/api/v1/transfers", bad, 400)
	// A corrupted/insecure startup file never falls back to a disabled worker.
	badConfig := filepath.Join(dir, "bad.json")
	os.WriteFile(badConfig, []byte(`{"version":1}`), 0644)
	badArgs := append(append([]string{}, args...), "-file-deployment", badConfig)
	badArgs[1] = "127.0.0.1:0"
	command := exec.CommandContext(ctx, controller, badArgs...)
	if err = command.Run(); err == nil {
		t.Fatal("invalid config started controller")
	}
	t.Logf("actual standalone Controller CLI + two resident Agent CLIs: %d bytes/SHA %s, task/transfer terminal, repeat skip, unregistered root and insecure startup rejected", len(data), sha256Hex(data))
}
