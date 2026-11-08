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
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/tasks"
)

// This test-only checker validates the owned nginx configuration with its real
// binary. It is explicitly injected: the standalone Controller's missing route
// checker is still a production assembly gap, not a default-success fallback.
type cloudR3NginxCheck struct{ binary, prefix string }

func (c cloudR3NginxCheck) CheckRoute(ctx context.Context, s gateway.RouteSpec) (gateway.CheckResult, error) {
	if s.Domain != "app.example.com" || s.GatewayID != "gw-r3" {
		return gateway.CheckResult{}, fmt.Errorf("unexpected isolated route")
	}
	_, e := exec.CommandContext(ctx, c.binary, "-t", "-p", c.prefix+"/", "-c", filepath.Join(c.prefix, "nginx.conf")).CombinedOutput()
	return gateway.CheckResult{Passed: e == nil}, e
}

// Exercises the real central file gate, uploader, child observer, human task
// approval, resident Agent and nginx. DNS uses a LOCAL TLS protocol endpoint;
// this is not evidence for external providers or the standalone checker gap.
func TestCloudR3GatewayActualAgentPublicationAndReverseCompensation(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("actual gateway requires ordinary Linux UID and private owned directories")
	}
	nginx := os.Getenv("NGINX_BIN")
	if nginx == "" {
		var e error
		nginx, e = exec.LookPath("nginx")
		if e != nil {
			t.Fatal("actual nginx is required", e)
		}
	}
	_, agent, _, _ := buildActualProducts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	fileRoot := filepath.Join(dir, "files")
	if e := os.Mkdir(fileRoot, 0700); e != nil {
		t.Fatal(e)
	}
	prefix := filepath.Join(fileRoot, "gateway")
	siteDir := filepath.Join(prefix, "conf")
	artifactDir := filepath.Join(prefix, "artifacts")
	for _, d := range []string{prefix, siteDir, artifactDir} {
		if e := os.Mkdir(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	conf := "worker_processes 1;\npid " + filepath.Join(prefix, "nginx.pid") + ";\nerror_log " + filepath.Join(prefix, "error.log") + ";\nevents { worker_connections 64; }\nhttp { access_log off; include " + siteDir + "/*.conf; }\n"
	if e := os.WriteFile(filepath.Join(prefix, "nginx.conf"), []byte(conf), 0600); e != nil {
		t.Fatal(e)
	}
	command := exec.CommandContext(ctx, nginx, "-p", prefix+"/", "-c", filepath.Join(prefix, "nginx.conf"))
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("owned nginx start: %v %s", e, out)
	}
	t.Cleanup(func() {
		_ = exec.Command(nginx, "-s", "stop", "-p", prefix+"/", "-c", filepath.Join(prefix, "nginx.conf")).Run()
	})
	upstreamBytes := bytes.Repeat([]byte("cloud-r3-real-gateway-bytes\n"), 13)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(upstreamBytes) }))
	defer upstream.Close()
	_, upstreamPort, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	upPort, _ := strconv.Atoi(upstreamPort)
	free, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	listen := free.Addr().String()
	free.Close()
	var dnsMu sync.Mutex
	creates, deletes := 0, 0
	providerHost := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/user/tokens/verify":
			_, _ = io.WriteString(w, `{"success":true,"result":{"status":"active"}}`)
		case r.URL.Path == "/zones":
			_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"pz-r3","name":"example.com","status":"active"}],"result_info":{"page":1,"total_pages":1}}`)
		case r.Method == "POST" && r.URL.Path == "/zones/pz-r3/dns_records":
			dnsMu.Lock()
			creates++
			dnsMu.Unlock()
			_, _ = io.WriteString(w, `{"success":true,"result":{"id":"record-r3"}}`)
		case r.Method == "DELETE" && r.URL.Path == "/zones/pz-r3/dns_records/record-r3":
			dnsMu.Lock()
			deletes++
			dnsMu.Unlock()
			_, _ = io.WriteString(w, `{"success":true,"result":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerHost.Close()
	provider, e := dns.NewCloudflareProvider(dns.CloudflareConfig{BaseURL: providerHost.URL, Client: providerHost.Client(), Secrets: dns.SecretBorrowerFunc(func(ctx context.Context, _ dns.SecretUse, f func(context.Context, string) error) error {
		return f(ctx, "synthetic-local-token")
	})})
	if e != nil {
		t.Fatal(e)
	}
	pki := newTestPKI(t)
	ca := filepath.Join(dir, "ca.pem")
	if e = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pki.rootDER}), 0600); e != nil {
		t.Fatal(e)
	}
	host := httptest.NewUnstartedServer(nil)
	port := strconv.Itoa(host.Listener.Addr().(*net.TCPAddr).Port)
	authority := "localhost:" + port
	origin := "https://" + authority
	paths := &r2FilePaths{roots: map[string]string{}}
	composed, e := server.New(ctx, server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin, AgentAuthority: authority, AgentServerName: "localhost", EnrollmentIssuer: managerIssuer(t, pki), DNSProvider: provider, GatewayChecker: cloudR3NginxCheck{nginx, prefix}, FileExecution: &server.FileExecutionConfig{Paths: paths, SpoolDir: filepath.Join(dir, "spool")}, GatewayDeployment: &server.GatewayDeployment{Gateways: map[string]server.GatewayInstallPaths{"gw-r3": {SiteDir: siteDir, ArtifactDir: artifactDir, HTTPListen: listen}}}})
	if e != nil {
		t.Fatal(e)
	}
	host.Config.Handler = composed
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pki.roots}
	host.StartTLS()
	t.Cleanup(func() { host.Close(); composed.Close() })
	setup, e := composed.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = composed.Identity.Bootstrap(ctx, setup, "admin", "cloud-r3-private-password"); e != nil {
		t.Fatal(e)
	}
	jar, _ := cookiejar.New(nil)
	c := &r3Client{t: t, ctx: ctx, http: newTestHTTPClient(pki, "127.0.0.1:"+port, jar), origin: origin}
	type loginResult struct {
		CSRF     string `json:"csrfToken"`
		Identity struct {
			ID string `json:"userId"`
		} `json:"identity"`
	}
	var login loginResult
	json.Unmarshal(c.must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "cloud-r3-private-password"}, 200), &login)
	c.csrf = login.CSRF
	program, e := composed.Identity.Login(ctx, "admin", "cloud-r3-private-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = composed.Identity.CreateUser(ctx, program.Secret, program.CSRF, auth.UserCreation{Username: "gateway-reviewer", Password: "cloud-r3-reviewer-password", RoleIDs: []string{"administrator"}, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	otherJar, _ := cookiejar.New(nil)
	reviewer := &r3Client{t: t, ctx: ctx, http: newTestHTTPClient(pki, "127.0.0.1:"+port, otherJar), origin: origin}
	var other loginResult
	json.Unmarshal(reviewer.must("POST", "/api/v1/auth/login", map[string]string{"username": "gateway-reviewer", "password": "cloud-r3-reviewer-password"}, 200), &other)
	reviewer.csrf = other.CSRF
	var token struct {
		Token string `json:"token"`
	}
	json.Unmarshal(c.must("POST", "/api/v1/enrollment-tokens", map[string]any{"expiryMinutes": 10, "maxUses": 1}, 201), &token)
	state := filepath.Join(dir, "agent-state")
	enroll := []string{"-enroll-token", token.Token, "-state-dir", state, "-controller-url", origin, "-root-cert", ca}
	if _, out, code := runAgentCLI(t, ctx, agent, enroll...); code != 0 {
		t.Fatal(code, out)
	}
	var pending struct {
		Items []struct{ ID, NodeID, Status string }
	}
	json.Unmarshal(c.must("GET", "/api/v1/enrollments", nil, 200), &pending)
	if len(pending.Items) != 1 {
		t.Fatal("missing pending identity")
	}
	node := pending.Items[0].NodeID
	c.must("POST", "/api/v1/enrollments/"+pending.Items[0].ID+"/decision", map[string]string{"decision": "approve", "reason": "isolated actual gateway"}, 200)
	if _, out, code := runAgentCLI(t, ctx, agent, enroll...); code != 0 {
		t.Fatal(code, out)
	}
	permissions := []string{"nodes.read", "tasks.submit", "tasks.read", "tasks.approve", "tasks.cancel"}
	for _, p := range server.ExtraPermissions() {
		if p.ResourceType == "node" {
			permissions = append(permissions, p.Key)
		}
	}
	for _, subject := range []string{login.Identity.ID, other.Identity.ID} {
		c.must("POST", "/api/v1/resource-grants", map[string]any{"principalId": subject, "resourceType": "node", "resourceId": node, "permissions": permissions}, 200)
	}
	paths.mu.Lock()
	paths.roots[node] = fileRoot
	paths.mu.Unlock()
	if _, e = composed.DB.Exec(ctx, `UPDATE nodes SET address='127.0.0.1' WHERE id=?`, node); e != nil {
		t.Fatal(e)
	}
	process := startR3Process(t, agent, "-serve", "-state-dir", state, "-controller-url", origin, "-root-cert", ca, "-file-root", fileRoot, "-gateway-prefix", prefix, "-nginx-binary", nginx)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e = composed.Links.FileConnection(ctx, node); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual Agent failed to connect", readLogTail(process.log))
		}
		time.Sleep(25 * time.Millisecond)
	}
	group, e := composed.ServiceGroups.Create(ctx, login.Identity.ID, "r3-group", servicegroups.Input{ID: "group-r3", Name: "Owned upstream", Service: servicegroups.ServiceConfig{Port: upPort, Protocol: "http"}, MemberNodeIDs: []string{node}})
	if e != nil {
		t.Fatal(e)
	}
	gw, e := composed.Gateway.CreateGateway(ctx, login.Identity.ID, "r3-gateway", gateway.CreateGatewayInput{ID: "gw-r3", NodeID: node, Name: "Owned nginx", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = composed.Gateway.RecordChecks(ctx, login.Identity.ID, "r3-check", gw.ID, gw.Revision, gateway.CheckReport{NginxInstalled: true, ChecksPassed: true, NginxVersion: "actual-isolated"}); e != nil {
		t.Fatal(e)
	}
	account, e := composed.DNS.CreateAccount(ctx, login.Identity.ID, "r3-account", dns.CreateAccountInput{ID: "account-r3", DisplayName: "Local TLS protocol", Enabled: true, MakeDefault: true, Credential: dns.CredentialRef("secret/r3-local")})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = composed.DNS.TestAccount(ctx, login.Identity.ID, "r3-account-test", account.ID); e != nil {
		t.Fatal(e)
	}
	zone, e := composed.DNS.CreateZone(ctx, login.Identity.ID, "r3-zone", account.ID, dns.CreateZoneInput{ID: "zone-r3", ProviderZoneID: "pz-r3"})
	if e != nil {
		t.Fatal(e)
	}
	var publication struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.must("POST", "/api/v1/publications", map[string]any{"name": "Actual local gateway", "domain": "app.example.com", "serviceGroupId": group.ID, "expectedGroupRevision": group.Revision, "gatewayIds": []string{gw.ID}, "zoneId": zone.ID, "https": false, "targetRevision": 1}, 202), &publication)
	if publication.ID == "" {
		t.Fatal("missing publication id")
	}
	approved := map[string]bool{}
	runWithApproval := func(route string) {
		t.Helper()
		type response struct {
			code int
			body []byte
		}
		done := make(chan response, 1)
		go func() { code, body := c.request("POST", route, map[string]any{}); done <- response{code, body} }()
		timeout := time.NewTimer(40 * time.Second)
		defer timeout.Stop()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case r := <-done:
				if r.code != 200 {
					t.Fatalf("actual publication %s: %d %s agent=%s", route, r.code, r.body, readLogTail(process.log))
				}
				t.Logf("actual HTTP %s: %s", route, r.body)
				return
			case <-timeout.C:
				t.Fatal("actual publication timed out", readLogTail(process.log))
			case <-tick.C:
				list, e := composed.TasksRepo.List(ctx, 100, 0)
				if e != nil {
					t.Fatal(e)
				}
				for _, task := range list {
					if task.Status != tasks.StatusAwaitingApproval || approved[task.ID] {
						continue
					}
					if task.ActionID == gateway.NginxActionApply {
						entries, e := os.ReadDir(artifactDir)
						if e != nil || len(entries) != 0 {
							t.Fatal("artifact sent before independent approval", e)
						}
					}
					c.must("POST", "/api/v1/tasks/"+task.ID+"/approve", map[string]string{"planDigest": task.PlanHash, "decision": "approve", "reason": "self approval must fail"}, 409)
					reviewer.must("POST", "/api/v1/tasks/"+task.ID+"/approve", map[string]string{"planDigest": task.PlanHash, "decision": "approve", "reason": "owned real nginx and byte verification"}, 200)
					approved[task.ID] = true
				}
			}
		}
	}
	runWithApproval("/api/v1/publications/" + publication.ID + "/run")
	request, e := http.NewRequestWithContext(ctx, "GET", "http://"+listen+"/", nil)
	if e != nil {
		t.Fatal(e)
	}
	request.Host = "app.example.com"
	var received []byte
	deadline = time.Now().Add(5 * time.Second)
	for {
		resp, err := http.DefaultClient.Do(request)
		if err == nil {
			received, e = io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && bytes.Equal(received, upstreamBytes) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual nginx bytes differ: %v %q", err, received)
		}
		time.Sleep(25 * time.Millisecond)
	}
	var childState, childResult string
	if e = composed.DB.QueryRow(ctx, `SELECT state,result FROM gateway_artifact_bindings`).Scan(&childState, &childResult); e != nil || childState != "finished" || childResult != "EXECUTION_RESULT_SUCCEEDED" {
		t.Fatal("child not durably complete", childState, childResult, e)
	}
	runWithApproval("/api/v1/publications/" + publication.ID + "/rollback-all")
	routes, e := composed.Gateway.ListRoutes(ctx, login.Identity.ID, gw.ID)
	if e != nil || len(routes) != 0 {
		t.Fatal("compensation left route", routes, e)
	}
	entries, e := os.ReadDir(siteDir)
	if e != nil || len(entries) != 0 {
		t.Fatal("compensation left managed site", e)
	}
	dnsMu.Lock()
	defer dnsMu.Unlock()
	if creates != 1 || deletes != 1 {
		t.Fatal("DNS compensation count", creates, deletes)
	}
	if len(approved) != 2 {
		t.Fatal("apply and removal need separate human approvals", len(approved))
	}
	t.Logf("real WSS artifact, durable child, nginx -t/reload, HTTP bytes=%d, approved apply/remove, reverse DNS cleanup verified", len(received))
}
