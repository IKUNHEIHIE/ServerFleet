package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func TestPinnedCommandProcessChild(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) != 2 {
		t.Skip("private pinned executable subprocess only")
	}
	if os.WriteFile(args[0], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		os.Exit(71)
	}
	if args[1] == "wait" {
		for {
			time.Sleep(time.Second)
		}
	}
}

func pinnedCommandFixture(t *testing.T) (agentAction, string, string, func(string) []byte) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires genuine non-root command owner")
	}
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	program := filepath.Join(dir, "command")
	if e = os.WriteFile(program, body, 0700); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(body)
	policy := commandProfiles{Version: 1, NodeID: "node", Profiles: []commandProfile{{ID: "private-test", Executable: program, SHA256: hex.EncodeToString(sum[:]), Directory: dir, PrefixArguments: []string{"-test.run=^TestPinnedCommandProcessChild$", "--"}, MaximumArguments: 2, MaximumSeconds: 10}}}
	raw, _ := json.Marshal(policy)
	filename := filepath.Join(dir, "policy.json")
	if e = os.WriteFile(filename, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_, digest, e := loadCommandProfiles(filename, "node", false)
	if e != nil {
		t.Fatal(e)
	}
	action, e := registeredCommands(filename, "node")
	if e != nil {
		t.Fatal(e)
	}
	parameters := func(mode string) []byte {
		args, _ := json.Marshal([]string{filepath.Join(dir, "effect"), mode})
		p, _ := json.Marshal(map[string]string{"profileId": "private-test", "arguments": string(args), "policySha256": digest, "timeoutSeconds": "10"})
		return p
	}
	return action, dir, filename, parameters
}

func TestPinnedOrdinaryCommandActualEffectAndChangedPolicyDenial(t *testing.T) {
	action, dir, filename, parameters := pinnedCommandFixture(t)
	raw := parameters("finish")
	if e := action.Validate(raw); e != nil {
		t.Fatal(e)
	}
	if result := action.Run(context.Background(), raw); result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatal("actual pinned process failed", result)
	}
	if pid, e := os.ReadFile(filepath.Join(dir, "effect")); e != nil || len(pid) == 0 {
		t.Fatal("pinned process effect missing", e)
	}
	if e := os.Remove(filepath.Join(dir, "effect")); e != nil {
		t.Fatal(e)
	}
	previous, e := os.ReadFile(filename)
	if e != nil {
		t.Fatal(e)
	}
	var policy commandProfiles
	if json.Unmarshal(previous, &policy) != nil {
		t.Fatal("fixture policy")
	}
	policy.Profiles[0].MaximumSeconds = 9
	changed, _ := json.Marshal(policy)
	if e = os.WriteFile(filename, changed, 0600); e != nil {
		t.Fatal(e)
	}
	if e = action.Validate(raw); e == nil {
		t.Fatal("changed policy accepted before execution")
	}
	if result := action.Run(context.Background(), raw); result.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatal("changed policy executed")
	}
	if _, e = os.Stat(filepath.Join(dir, "effect")); !os.IsNotExist(e) {
		t.Fatal("denied policy produced an effect", e)
	}
	if e = os.WriteFile(filename, previous, 0600); e != nil {
		t.Fatal(e)
	}
	// Replace the pathname with a distinct inode: Linux may retain ETXTBSY on
	// the previously executed image. The pinned action must reject the new bytes.
	replacement := filepath.Join(dir, "replacement")
	if e = os.WriteFile(replacement, []byte("changed image"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(replacement, filepath.Join(dir, "command")); e != nil {
		t.Fatal(e)
	}
	if result := action.Run(context.Background(), raw); result.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatal("unpinned executable ran")
	}
}

func TestGuardedCommandMTLSRevocationKillsActualOwnedProcess(t *testing.T) {
	action, dir, _, parameters := pinnedCommandFixture(t)
	serverCert, clientCert, roots, _ := sessionPKI(t)
	allowed := new(atomic.Bool)
	allowed.Store(true)
	requests := new(atomic.Int32)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || !bytes.Equal(r.TLS.PeerCertificates[0].Raw, clientCert.Certificate[0]) || r.URL.Path != "/api/v1/agent/command-admission" || !allowed.Load() {
			w.WriteHeader(403)
			return
		}
		var body struct {
			NodeID string             `json:"nodeId"`
			Nonce  string             `json:"nonce"`
			Phase  string             `json:"phase"`
			Offer  *agentv1.TaskOffer `json:"offer"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.NodeID != "node" || body.Offer == nil || body.Offer.TaskId != "command-task" {
			w.WriteHeader(403)
			return
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"taskId": body.Offer.TaskId, "executionId": body.Offer.ExecutionId, "planHash": body.Offer.PlanDigestSha256, "nonce": body.Nonce, "phase": body.Phase, "timeoutSeconds": strconv.Itoa(int(body.Offer.TimeoutSeconds))})
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "https://")
	_, port, e := net.SplitHostPort(address)
	if e != nil {
		t.Fatal(e)
	}
	guarded := guardedCommandAction(action, "node", agentLinkEndpoint{serverName: "fleet.test", address: address, authority: net.JoinHostPort("fleet.test", port)}, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientCert}, RootCAs: roots, ServerName: "fleet.test"})
	offer := &agentv1.TaskOffer{TaskId: "command-task", ExecutionId: "command-execution", ActionId: transport.CommandCapability, PlanDigestSha256: strings.Repeat("a", 64), TimeoutSeconds: 10}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, agentOfferKey{}, offer)
	done := make(chan executionResult, 1)
	go func() { done <- guarded.Run(ctx, parameters("wait")) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	pid := 0
	for pid == 0 {
		b, err := os.ReadFile(filepath.Join(dir, "effect"))
		if err == nil {
			pid, _ = strconv.Atoi(string(b))
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual command never started", ctx.Err())
		case <-ticker.C:
		}
	}
	allowed.Store(false)
	select {
	case result := <-done:
		if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
			t.Fatal("revocation turned into trusted success", result)
		}
	case <-ctx.Done():
		t.Fatal("live revocation did not stop actual process", ctx.Err())
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("revoked command child survived")
	}
	if requests.Load() < 2 {
		t.Fatal("command ran without both admission and start observations")
	}
}
