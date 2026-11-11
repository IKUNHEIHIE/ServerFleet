package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"testing"
	"time"
)

type probeTestDispatcher struct{ calls int }

func (p *probeTestDispatcher) Dispatch(context.Context, tasks.Command) (tasks.DispatchResult, error) {
	p.calls++
	return tasks.DispatchResult{Accepted: true}, nil
}
func (p *probeTestDispatcher) Cancel(context.Context, tasks.CancelCommand) error { return nil }
func TestRemoteProbeTaskBindingReceiptAndRevocation(t *testing.T) {
	ctx := context.Background()
	dispatcher := &probeTestDispatcher{}
	s, e := New(ctx, Config{DBPath: filepath.Join(t.TempDir(), "probe.db"), Origin: "https://controller.example.invalid", StagingDir: filepath.Join(t.TempDir(), "stage"), Dispatcher: dispatcher})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.runCancel()
	<-s.runDone
	setup, e := s.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Identity.Bootstrap(ctx, setup, "admin", "synthetic-node-probe-password"); e != nil {
		t.Fatal(e)
	}
	login, e := s.Identity.Login(ctx, "admin", "synthetic-node-probe-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	owner := login.Identity.SubjectID
	if _, e = s.NodesRepo.Create(ctx, s.DB, nodes.NodeCreateInput{ID: "source-node", Name: "Source", Status: nodes.StatusOnline}); e != nil {
		t.Fatal(e)
	}
	if s.validateProbeSource(ctx, owner, "source-node") == nil {
		t.Fatal("administrator implicitly received a node probe grant")
	}
	if _, e = s.Identity.CreateResourceGrant(ctx, login.Secret, login.CSRF, auth.GrantRequest{PrincipalID: owner, ResourceType: "node", ResourceID: "source-node", Permissions: []string{transport.ProbeCapability, tasks.ActionCancel}}); e != nil {
		t.Fatal(e)
	}
	input := monitoring.MonitorInput{ID: "remote", Name: "Remote public service", Kind: monitoring.KindHTTP, Target: "https://example.com/health", IntervalSeconds: 60, TimeoutSeconds: 5, Enabled: true, SourceNodeID: "source-node"}
	m, e := s.Monitoring.Create(ctx, owner, "create-remote", input)
	if e != nil {
		t.Fatal(e)
	}
	// A missing live source is an unknown observation and creates no queued
	// late-running node task, even though inventory and authorization exist.
	check, _, e := s.Monitoring.RunCheckNow(ctx, owner, "offline-source", m.ID)
	if e != nil || check.Status != monitoring.StateUnknown || check.TaskID != "" || dispatcher.calls != 0 {
		t.Fatal("offline source fabricated execution or business failure", check, e)
	}
	m, e = s.Monitoring.Get(ctx, owner, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	keyHash := sha256.Sum256([]byte("synthetic remote claim"))
	key := hex.EncodeToString(keyHash[:])
	params := map[string]string{"requestKey": key, "kind": "http", "target": m.Target, "timeoutSeconds": "5", "profile": transport.ProbeProfile}
	encoded, _ := json.Marshal(params)
	run := nodeProbeRun{Deadline: time.Now().UTC().Add(time.Minute), Key: key, MonitorID: m.ID, Owner: owner, Actor: owner, Revision: m.AutomationRevision, Observation: m.ObservationRevision, Node: m.SourceNodeID, Manual: true, Enabled: true, Params: string(encoded)}
	payload, _ := json.Marshal(run)
	if _, e = s.DB.Exec(ctx, `INSERT INTO monitor_probe_runs VALUES(?,'',?)`, key, payload); e != nil {
		t.Fatal(e)
	}
	task, e := s.Tasks.Submit(ctx, tasks.SubmitRequest{SubjectID: owner, RequestID: "node-probe-" + key, Name: "Synthetic probe task", ActionID: transport.ProbeCapability, OperationClass: tasks.OperationClassStandard, Source: audit.SourceSystem, TargetNodeIDs: []string{run.Node}, Params: params, IdempotencyKey: "node-probe-" + key})
	if e != nil {
		t.Fatal(e)
	}
	// TaskCurrent binds the private immutable intent before real node dispatch.
	if e = s.Tasks.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if dispatcher.calls != 1 {
		t.Fatal("not dispatched", dispatcher.calls)
	}
	task, e = s.Tasks.Get(ctx, task.ID)
	if e != nil || task.Status != tasks.StatusRunning {
		t.Fatal(task, e)
	}
	saved, e := s.readNodeProbe(ctx, key)
	if e != nil || saved.TaskID != task.ID || saved.PlanHash != task.PlanHash {
		t.Fatal("bind race not recovered", saved, e)
	}
	altered := task
	var bad map[string]string
	json.Unmarshal([]byte(task.Params), &bad)
	bad["target"] = "https://other.example.com/"
	raw, _ := json.Marshal(bad)
	altered.Params = string(raw)
	if s.nodeProbeTaskCurrent(ctx, altered) == nil {
		t.Fatal("mutated target admitted")
	}
	if s.validateProbeSource(ctx, "not-a-user", run.Node) == nil {
		t.Fatal("ungranted actor admitted")
	}
	receipt := transport.ProbeReceipt{Version: 1, RequestKey: key, Outcome: "down", Reason: "http_status"}
	code := 503
	receipt.StatusCode = &code
	wire, _ := json.Marshal(receipt)
	output := &agentv1.TaskOutput{TaskId: task.ID, ExecutionId: executionIDFor(task.ID, run.Node, task.PlanHash), Stream: agentv1.TaskOutput_STREAM_STDOUT, Chunk: wire}
	wrong := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: "wrong-node"}, ConnectionID: "other"}
	if s.recordNodeProbe(ctx, wrong, output) == nil {
		t.Fatal("wrong peer receipt admitted")
	}
	peer := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: run.Node}, ConnectionID: "synthetic-verified-link"}
	if e = s.recordNodeProbe(ctx, peer, output); e != nil {
		t.Fatal(e)
	}
	if e = s.recordNodeProbe(ctx, peer, output); e != nil {
		t.Fatal("duplicate receipt not idempotent", e)
	}
	receipt.Outcome = "up"
	receipt.Reason = ""
	wire, _ = json.Marshal(receipt)
	output.Chunk = wire
	if s.recordNodeProbe(ctx, peer, output) == nil {
		t.Fatal("conflicting receipt overwrote immutable result")
	}
	expired := saved
	expired.Deadline = time.Now().UTC().Add(-time.Second)
	if s.currentNodeProbe(ctx, expired) == nil {
		t.Fatal("expired producer intent remained executable")
	}
	stopped := saved
	stopped.Stopped = true
	if s.currentNodeProbe(ctx, stopped) == nil {
		t.Fatal("abandoned producer intent remained executable")
	}
	input.Target = "https://example.com/other"
	if _, e = s.Monitoring.Update(ctx, owner, "replace-source-revision", m.ID, input); e != nil {
		t.Fatal(e)
	}
	if s.nodeProbeTaskCurrent(ctx, task) == nil {
		t.Fatal("old source revision still current")
	}
	// A result already in flight is ignored after the source revision changes.
	if e = s.recordNodeProbe(ctx, peer, output); e != nil {
		t.Fatal("late stale frame disconnected peer", e)
	}
	saved, e = s.readNodeProbe(ctx, key)
	if e != nil || saved.Receipt == nil || saved.Receipt.Outcome != "down" {
		t.Fatal("late result changed observation", saved, e)
	}
	s.stopNodeProbe(ctx, key)
	saved, e = s.readNodeProbe(ctx, key)
	if e != nil || !saved.Stopped || saved.Receipt == nil || saved.Receipt.Outcome != "down" {
		t.Fatal("abandoned intent did not retain immutable receipt", saved, e)
	}
}
