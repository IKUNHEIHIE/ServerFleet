package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"strconv"
	"time"
)

type nodeProbeClient struct{ s *Server }
type nodeProbeRun struct {
	Key          string
	MonitorID    string
	Owner        string
	Actor        string
	Revision     uint64
	Observation  uint64
	Node         string
	Manual       bool
	Enabled      bool
	Params       string
	TaskID       string
	PlanHash     string
	Receipt      *transport.ProbeReceipt
	ConnectionID string
}

func (s *Server) validateProbeSource(ctx context.Context, actor, node string) error {
	if s.runtimeGate(ctx) != nil || s.Identity.AuthorizeSubjectAction(ctx, actor, monitoring.ActionWrite) != nil || s.Identity.Authorize(ctx, auth.Check{SubjectID: actor, Action: transport.ProbeCapability, NodeIDs: []string{node}}) != nil {
		return auth.ErrDenied
	}
	n, e := s.NodesRepo.Get(ctx, node)
	if e != nil || n.Status == nodes.StatusRevoked || n.Status == nodes.StatusSuspended {
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) readNodeProbe(ctx context.Context, key string) (nodeProbeRun, error) {
	var r nodeProbeRun
	var raw []byte
	var task string
	e := s.DB.QueryRow(ctx, `SELECT task_id,payload FROM monitor_probe_runs WHERE run_key=?`, key).Scan(&task, &raw)
	if e != nil {
		return r, e
	}
	if len(raw) > 16384 || json.Unmarshal(raw, &r) != nil || r.Key != key || r.TaskID != task {
		return r, auth.ErrDenied
	}
	return r, nil
}
func (s *Server) currentNodeProbe(ctx context.Context, r nodeProbeRun) error {
	if s.Monitoring == nil || s.validateProbeSource(ctx, r.Actor, r.Node) != nil {
		return auth.ErrDenied
	}
	m, e := s.Monitoring.ProbeSnapshot(ctx, r.MonitorID, r.Owner, r.Revision, r.Observation, r.Node)
	if e != nil || m.Enabled != r.Enabled || !m.Enabled && !r.Manual {
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) bindNodeProbe(ctx context.Context, r nodeProbeRun, t tasks.Task) error {
	if t.ID == "" {
		return nil
	}
	if r.TaskID != "" {
		if r.TaskID != t.ID || r.PlanHash != t.PlanHash {
			return auth.ErrDenied
		}
		return nil
	}
	r.TaskID = t.ID
	r.PlanHash = t.PlanHash
	raw, _ := json.Marshal(r)
	changed, e := s.DB.Exec(ctx, `UPDATE monitor_probe_runs SET task_id=?,payload=? WHERE run_key=? AND task_id=''`, t.ID, raw, r.Key)
	if e != nil {
		return e
	}
	n, e := changed.RowsAffected()
	if e != nil {
		return e
	}
	if n == 1 {
		return nil
	}
	current, e := s.readNodeProbe(ctx, r.Key)
	if e != nil || current.TaskID != t.ID || current.PlanHash != t.PlanHash {
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) nodeProbeTaskCurrent(ctx context.Context, t tasks.Task) error {
	p, e := transport.ParseProbeParameters([]byte(t.Params))
	if e != nil {
		return auth.ErrDenied
	}
	r, e := s.readNodeProbe(ctx, p.RequestKey)
	if e != nil || s.currentNodeProbe(ctx, r) != nil || r.Params != t.Params || r.Actor != t.SubmitterID || t.OperationClass != tasks.OperationClassStandard || t.ApprovalRequired || t.PluginID != "" || t.ScopeRevision != 0 || t.Source != "system" || t.TargetType != "node_ids" || t.TargetResourceID != "" || len(t.Targets) != 1 || t.Targets[0].NodeID != r.Node || t.IdempotencyKey != "node-probe-"+r.Key {
		return auth.ErrDenied
	}
	return s.bindNodeProbe(ctx, r, t)
}
func (s *Server) currentTask(ctx context.Context, t tasks.Task) error {
	if t.ActionID == transport.ProbeCapability {
		return s.nodeProbeTaskCurrent(ctx, t)
	}
	return s.aiTaskCurrent(ctx, t)
}
func (c nodeProbeClient) Probe(ctx context.Context, m monitoring.Monitor, actor string, manual bool) (monitoring.ProbeResult, error) {
	s := c.s
	unknown := func(task, reason string) monitoring.ProbeResult {
		return monitoring.ProbeResult{Unknown: true, TaskID: task, Reason: reason}
	}
	if s.validateProbeSource(ctx, actor, m.SourceNodeID) != nil {
		return unknown("", "probe_source_denied"), nil
	}
	if s.Links == nil {
		return unknown("", "probe_source_unavailable"), nil
	}
	if _, e := s.Links.CurrentConnection(ctx, m.SourceNodeID); e != nil {
		return unknown("", "probe_source_offline"), nil
	}
	seed, _ := json.Marshal([]any{m.ID, m.AutomationRevision, m.ObservationRevision, actor, manual})
	sum := sha256.Sum256(seed)
	key := hex.EncodeToString(sum[:])
	params, _ := json.Marshal(map[string]string{"requestKey": key, "kind": string(m.Kind), "target": m.Target, "timeoutSeconds": strconv.Itoa(m.TimeoutSeconds), "profile": transport.ProbeProfile})
	if _, e := transport.ParseProbeParameters(params); e != nil {
		return unknown("", "probe_target_rejected"), nil
	}
	run := nodeProbeRun{Key: key, MonitorID: m.ID, Owner: m.AutomationOwnerID, Actor: actor, Revision: m.AutomationRevision, Observation: m.ObservationRevision, Node: m.SourceNodeID, Manual: manual, Enabled: m.Enabled, Params: string(params)}
	raw, _ := json.Marshal(run)
	if _, e := s.DB.Exec(ctx, `INSERT OR IGNORE INTO monitor_probe_runs(run_key,task_id,payload) VALUES(?,'',?)`, key, raw); e != nil {
		return unknown("", "probe_storage_unavailable"), e
	}
	run, e := s.readNodeProbe(ctx, key)
	if e != nil || s.currentNodeProbe(ctx, run) != nil {
		return unknown("", "probe_configuration_changed"), nil
	}
	if run.TaskID == "" {
		t, e := s.Tasks.Submit(ctx, tasks.SubmitRequest{SubjectID: actor, RequestID: "node-probe-" + key, Name: "Read service availability from selected node", ActionID: transport.ProbeCapability, OperationClass: tasks.OperationClassStandard, Source: audit.SourceSystem, TargetNodeIDs: []string{run.Node}, Params: map[string]string{"requestKey": key, "kind": string(m.Kind), "target": m.Target, "timeoutSeconds": strconv.Itoa(m.TimeoutSeconds), "profile": transport.ProbeProfile}, IdempotencyKey: "node-probe-" + key})
		if e != nil {
			return unknown("", "probe_task_unavailable"), nil
		}
		if e = s.bindNodeProbe(ctx, run, t); e != nil {
			return unknown(t.ID, "probe_task_binding_failed"), nil
		}
		run.TaskID = t.ID
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.currentNodeProbe(ctx, run) != nil {
			return unknown(run.TaskID, "probe_configuration_changed"), nil
		}
		t, e := s.Tasks.Get(ctx, run.TaskID)
		if e != nil {
			return unknown(run.TaskID, "probe_task_unavailable"), nil
		}
		if t.Status.Terminal() {
			if t.Status != tasks.StatusCompleted || len(t.Targets) != 1 || t.Targets[0].State != tasks.TargetSucceeded {
				return unknown(t.ID, "probe_execution_unknown"), nil
			}
			saved, e := s.readNodeProbe(ctx, key)
			if e != nil || saved.Receipt == nil {
				return unknown(t.ID, "probe_receipt_missing"), nil
			}
			r := saved.Receipt
			result := monitoring.ProbeResult{Healthy: r.Outcome == "up", Unknown: r.Outcome == "unknown", TaskID: t.ID, Reason: r.Reason, StatusCode: r.StatusCode}
			if r.LatencyMs != nil {
				d := time.Duration(*r.LatencyMs) * time.Millisecond
				result.Latency = &d
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			_, _ = s.Tasks.Cancel(clean, actor, "probe-timeout-"+key, t.ID)
			cancel()
			return unknown(t.ID, "probe_source_timeout"), nil
		case <-ticker.C:
		}
	}
}
func (s *Server) recordNodeProbe(ctx context.Context, obs transport.LinkObservation, out *agentv1.TaskOutput) error {
	t, e := s.TasksRepo.Get(ctx, out.TaskId)
	if e != nil || t.ActionID != transport.ProbeCapability {
		return auth.ErrDenied
	}
	p, e := transport.ParseProbeParameters([]byte(t.Params))
	if e != nil {
		return e
	}
	r, e := s.readNodeProbe(ctx, p.RequestKey)
	if e != nil || r.TaskID != t.ID || r.PlanHash != t.PlanHash || r.Node != obs.Identity.NodeID || out.ExecutionId != executionIDFor(t.ID, r.Node, t.PlanHash) {
		return auth.ErrDenied
	}
	// Stale/terminal frames are ignored, never turned into a new observation.
	if t.Status.Terminal() || s.currentNodeProbe(ctx, r) != nil {
		return nil
	}
	if _, e = s.Tasks.CheckExecutionForNode(ctx, t.ID, t.PlanHash, r.Node); e != nil {
		return e
	}
	if out.Truncated || out.Stream != agentv1.TaskOutput_STREAM_STDOUT {
		return auth.ErrDenied
	}
	receipt, e := transport.ParseProbeReceipt(out.Chunk)
	if e != nil || receipt.RequestKey != r.Key {
		return auth.ErrDenied
	}
	return s.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		var raw []byte
		var task string
		if e := tx.QueryRow(c, `SELECT task_id,payload FROM monitor_probe_runs WHERE run_key=?`, r.Key).Scan(&task, &raw); e != nil {
			return e
		}
		var current nodeProbeRun
		if json.Unmarshal(raw, &current) != nil || task != t.ID || current.PlanHash != t.PlanHash {
			return auth.ErrDenied
		}
		if current.Receipt != nil {
			a, _ := json.Marshal(current.Receipt)
			b, _ := json.Marshal(receipt)
			if string(a) != string(b) {
				return auth.ErrDenied
			}
			return nil
		}
		current.Receipt = &receipt
		current.ConnectionID = obs.ConnectionID
		raw, _ = json.Marshal(current)
		_, e := tx.Exec(c, `UPDATE monitor_probe_runs SET payload=? WHERE run_key=? AND task_id=?`, raw, r.Key, t.ID)
		return e
	})
}

var _ monitoring.RemoteProber = nodeProbeClient{}
