package server

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func canonicalEmptyAIParameters(raw json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if len(raw) > 1024 || json.Unmarshal(raw, &object) != nil || object == nil || len(object) != 0 {
		return nil, ai.ErrToolDenied
	}
	return json.RawMessage(`{}`), nil
}
func canonicalAIProfile(raw json.RawMessage) (json.RawMessage, error) {
	b, e := canonicalToolParameters(raw)
	if e != nil {
		return nil, e
	}
	if _, e = transport.ParseCommandParameters(b); e != nil {
		return nil, ai.ErrToolDenied
	}
	return b, nil
}

func (s *Server) aiTaskCurrent(ctx context.Context, t tasks.Task) error {
	if !strings.HasPrefix(t.IdempotencyKey, "ai-agent-") {
		return nil
	}
	if t.Source != "ai" || s.AI == nil || t.PluginID != "" || t.ScopeRevision == 0 {
		return auth.ErrDenied
	}
	ids := make([]string, 0, len(t.Targets))
	for _, target := range t.Targets {
		ids = append(ids, target.NodeID)
	}
	if s.AI.CheckAgentTask(ctx, ai.AgentTaskBinding{Key: t.IdempotencyKey, TaskID: t.ID, SubjectID: t.SubmitterID, Action: t.ActionID, Parameters: t.Params, ScopeRevision: t.ScopeRevision, NodeIDs: ids}) != nil {
		return auth.ErrDenied
	}
	return nil
}
func (s *Server) aiAgentBoundTask(ctx context.Context, session string, b ai.AgentTaskBinding) (tasks.Task, error) {
	actor, _, ok := ai.ActorFromContext(ctx)
	if !ok || s.AI == nil || s.aiCommitGate(ctx) != nil {
		return tasks.Task{}, ai.ErrToolDenied
	}
	v, e := s.AI.Get(ctx, session)
	if e != nil || v.CreatedBy != actor.SubjectID || v.ScopeRevision != b.ScopeRevision {
		return tasks.Task{}, ai.ErrToolDenied
	}
	b.SubjectID = actor.SubjectID
	if s.AI.CheckAgentTask(ctx, b) != nil {
		return tasks.Task{}, ai.ErrToolDenied
	}
	t, e := s.Tasks.Get(ctx, b.TaskID)
	if e != nil || t.SubmitterID != actor.SubjectID || t.IdempotencyKey != b.Key || t.Source != "ai" || t.ActionID != b.Action || t.Params != b.Parameters || t.ScopeRevision != b.ScopeRevision {
		return tasks.Task{}, ai.ErrToolDenied
	}
	ids := []string{}
	for _, target := range t.Targets {
		ids = append(ids, target.NodeID)
	}
	sort.Strings(ids)
	nodes := append([]string{}, b.NodeIDs...)
	sort.Strings(nodes)
	left, _ := json.Marshal(ids)
	right, _ := json.Marshal(nodes)
	if !bytes.Equal(left, right) {
		return tasks.Task{}, ai.ErrToolDenied
	}
	if (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(ctx, actor.SubjectID, t.ID) != nil {
		return tasks.Task{}, ai.ErrToolDenied
	}
	return t, nil
}
func (s *Server) aiAgentReadTask(ctx context.Context, session string, b ai.AgentTaskBinding) (ai.AgentTaskSnapshot, error) {
	t, e := s.aiAgentBoundTask(ctx, session, b)
	if e != nil {
		return ai.AgentTaskSnapshot{}, e
	}
	state := string(t.Status)
	snapshot := ai.AgentTaskSnapshot{State: state}
	if !t.Status.Terminal() {
		return snapshot, nil
	}
	for _, target := range t.Targets {
		if string(target.State) == "unknown" {
			return ai.AgentTaskSnapshot{State: "unknown"}, nil
		}
	}
	targets := []map[string]string{}
	for _, v := range t.Targets {
		targets = append(targets, map[string]string{"nodeId": v.NodeID, "state": string(v.State), "message": v.Message})
	}
	logs, e := s.TasksRepo.Logs(ctx, t.ID, 0, 20)
	if e != nil {
		return snapshot, e
	}
	lines := []map[string]string{}
	for _, v := range logs {
		line := v.Message
		if len(line) > 512 {
			line = line[:512]
		}
		lines = append(lines, map[string]string{"nodeId": v.NodeID, "level": v.Level, "message": line})
	}
	data, e := json.Marshal(map[string]any{"taskId": t.ID, "state": state, "targets": targets, "logs": lines, "failureReason": t.FailureReason})
	if e != nil || len(data) > 32<<10 {
		return snapshot, ai.ErrExecutionUnavailable
	}
	if _, e = s.aiAgentBoundTask(ctx, session, b); e != nil {
		return snapshot, e
	}
	snapshot.Result = string(data)
	return snapshot, nil
}
func (s *Server) aiAgentCancelTask(ctx context.Context, session string, b ai.AgentTaskBinding) error {
	t, e := s.aiAgentBoundTask(ctx, session, b)
	if e != nil {
		return e
	}
	if t.Status.Terminal() {
		return nil
	}
	_, e = s.Tasks.Cancel(ctx, t.ID, t.SubmitterID, newRequestID(), t.Revision)
	return e
}
func (s *Server) aiInspectNodes(ctx context.Context, r ai.ToolExecutionRequest) (ai.ToolExecutionResult, error) {
	actor, _, ok := ai.ActorFromContext(ctx)
	if !ok || len(r.NodeIDs) == 0 || len(r.NodeIDs) > 50 || string(r.Parameters) != "{}" {
		return ai.ToolExecutionResult{}, ai.ErrToolDenied
	}
	items := []map[string]any{}
	for _, id := range r.NodeIDs {
		if s.Identity.Authorize(ctx, auth.Check{SubjectID: actor.SubjectID, Action: "nodes.read", NodeIDs: []string{id}}) != nil {
			return ai.ToolExecutionResult{}, ai.ErrToolDenied
		}
		n, e := s.NodesRepo.Get(ctx, id)
		if e != nil {
			return ai.ToolExecutionResult{}, e
		}
		item := map[string]any{"nodeId": n.ID, "name": n.Name, "status": n.Status, "region": n.Region, "lastSeenAt": n.LastSeenAt}
		if metrics, fresh := n.Usage(time.Now(), time.Minute); fresh {
			item["metrics"] = metrics
		} else {
			item["metrics"] = nil
		}
		items = append(items, item)
	}
	data, e := json.Marshal(map[string]any{"nodes": items})
	if e != nil || len(data) > 32<<10 || s.aiCommitGate(ctx) != nil {
		return ai.ToolExecutionResult{}, ai.ErrToolDenied
	}
	return ai.ToolExecutionResult{State: "completed", Observation: data}, nil
}

func (s *Server) commandTaskSourceCurrent(ctx context.Context, t tasks.Task) bool {
	if t.Source == "web" {
		return t.ScopeRevision == 0
	}
	return t.Source == "ai" && t.ScopeRevision > 0 && strings.HasPrefix(t.IdempotencyKey, "ai-agent-") && s.aiTaskCurrent(ctx, t) == nil
}
