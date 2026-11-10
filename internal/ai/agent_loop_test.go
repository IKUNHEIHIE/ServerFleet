package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// These doubles exercise the durable loop and trusted bindings; they do not
// claim a real model, Agent connection, root execution or supplier acceptance.
func agentLoopFixture(t *testing.T, approve bool, unknownTask bool) (*AgentLoop, SessionView, *int, *int) {
	t.Helper()
	ctx := context.Background()
	s, _, actor, authority, _ := sessionFixture(t)
	actor.Administrator = true
	s.tools["tasks.command"] = RegisteredTool{Definition: ToolDefinition{ID: "tasks.command", ReadOnly: true, AutoAllowed: true}, Canonicalize: s.tools["nodes.read"].Canonicalize}
	authority.AllowedTools = append(authority.AllowedTools, "tasks.command")
	v, e := s.Create(ctx)
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.AddGrant(ctx, v.ID, v.ScopeRevision, ResourceGrantInput{"node", "node-a", []string{"nodes.read", "tasks.command"}})
	if e != nil {
		t.Fatal(e)
	}
	if !approve {
		v, e = s.SetPolicy(ctx, v.ID, v.ScopeRevision, LowRisk, 0)
		if e != nil {
			t.Fatal(e)
		}
	}
	modelCalls, executions := 0, 0
	c, _ := conversationFixture(t, s, func(_ context.Context, r GenerationRequest, emit func(string) error) error {
		modelCalls++
		if r.Agent == nil || len(r.Messages) != 1 {
			t.Fatal("agent mixed with chat or missing private prompt")
		}
		if modelCalls == 1 {
			decision := agentDecision{Call: &AgentCall{ToolName: "tasks.command", NodeIDs: []string{"node-a"}, Parameters: json.RawMessage(`{}`), CallID: "call-one"}, Items: []json.RawMessage{json.RawMessage(`{"type":"function_call","name":"self_check","call_id":"call-one","arguments":"{\"nodeIds\":[\"node-a\"]}"}`)}}
			b, _ := json.Marshal(decision)
			return emit(string(b))
		}
		if len(r.Agent.History) != 2 || !strings.Contains(string(r.Agent.History[1]), "function_call_output") || !strings.Contains(string(r.Agent.History[1]), "actual-result") {
			t.Fatal("real result absent from next model turn")
		}
		return emit(`{"text":"Self-check completed based on actual-result.","items":[]}`)
	})
	l, e := NewAgentLoop(s, c, AgentLoopConfig{
		Execute: func(ctx context.Context, r ToolExecutionRequest) (ToolExecutionResult, error) {
			executions++
			b := AgentTaskBinding{Key: r.InvocationID, SubjectID: "owner", Action: r.ToolID, NodeIDs: r.NodeIDs, Parameters: string(r.Parameters), ScopeRevision: r.ScopeRevision}
			if r.InvocationID == "" || s.CheckAgentTask(ctx, b) != nil {
				t.Fatal("missing immutable execution binding")
			}
			return ToolExecutionResult{TaskID: "actual-task", State: "queued"}, nil
		},
		ReadTask: func(ctx context.Context, _ string, b AgentTaskBinding) (AgentTaskSnapshot, error) {
			b.SubjectID = "owner"
			if b.TaskID != "actual-task" || s.CheckAgentTask(ctx, b) != nil {
				t.Fatal("task result not bound")
			}
			forged := b
			forged.TaskID = "other-task"
			if s.CheckAgentTask(ctx, forged) == nil {
				t.Fatal("cross task result admitted")
			}
			if unknownTask {
				return AgentTaskSnapshot{State: "unknown"}, nil
			}
			return AgentTaskSnapshot{State: "completed", Result: `{"state":"completed","message":"actual-result"}`}, nil
		},
		CancelTask: func(context.Context, string, AgentTaskBinding) error { return nil },
	})
	if e != nil {
		t.Fatal(e)
	}
	return l, v, &modelCalls, &executions
}

func TestAgentLoopTaskResultContinuationAndChatIsolation(t *testing.T) {
	ctx := context.Background()
	l, v, models, executions := agentLoopFixture(t, false, false)
	// A queued ordinary chat does not become model context or block the run.
	if _, e := l.conversation.Submit(ctx, v.ID, "ordinary-pending", MessageInput{Content: "ordinary chat", ClientMessageID: "ordinary-pending", ScopeRevision: v.ScopeRevision}); e != nil {
		t.Fatal(e)
	}
	in := AgentRunInput{Goal: "inspect and self check", ClientRunID: "client-run", ScopeRevision: v.ScopeRevision, MaxSteps: 4, TimeoutSeconds: 900}
	r, e := l.Create(ctx, v.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := l.Create(ctx, v.ID, in)
	if e != nil || replay.ID != r.ID {
		t.Fatal("create replay duplicated", e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "waiting_task" || *executions != 1 {
		t.Fatal(r, e)
	}
	if _, e = l.conversation.Generate(ctx, v.ID, r.Steps[0].MessageID); !errors.Is(e, ErrToolDenied) {
		t.Fatal("ordinary generate admitted internal plan", e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "queued" || r.Steps[0].TaskState != "completed" {
		t.Fatal(r, e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "completed" || r.FinalText == "" || *models != 2 || *executions != 1 {
		t.Fatal(r, e)
	}
	if _, e = l.Advance(ctx, v.ID, r.ID, r.Revision); e != nil || *models != 2 || *executions != 1 {
		t.Fatal("terminal run replayed", e)
	}
	messages, e := l.conversation.Messages(ctx, v.ID, 0, 100)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range messages {
		if m.AgentRunID != "" && strings.Contains(m.RedactedContent, "function_call") {
			t.Fatal("internal provider plan disclosed")
		}
	}
}

func TestAgentLoopApprovalAndUnknownNeverReplay(t *testing.T) {
	ctx := context.Background()
	l, v, models, executions := agentLoopFixture(t, true, true)
	r, e := l.Create(ctx, v.ID, AgentRunInput{Goal: "self check", ClientRunID: "approval-run", ScopeRevision: v.ScopeRevision})
	if e != nil {
		t.Fatal(e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "waiting_approval" || *executions != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Advance(ctx, v.ID, r.ID, r.Revision); e != nil || *executions != 0 {
		t.Fatal("pending approval executed", e)
	}
	if _, e = l.sessions.DecideApproval(ctx, v.ID, r.Steps[0].ApprovalID, "approve", ""); e != nil {
		t.Fatal(e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "waiting_task" || *executions != 1 {
		t.Fatal(r, e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, r.Revision)
	if e != nil || r.State != "unknown" {
		t.Fatal(r, e)
	}
	if _, e = l.Advance(ctx, v.ID, r.ID, r.Revision); e != nil || *executions != 1 || *models != 1 {
		t.Fatal("unknown replayed", e)
	}
}

func TestAgentLoopInterruptedPlanAndScopeChange(t *testing.T) {
	ctx := context.Background()
	l, v, models, executions := agentLoopFixture(t, false, false)
	r, e := l.Create(ctx, v.ID, AgentRunInput{Goal: "self check", ClientRunID: "interrupted", ScopeRevision: v.ScopeRevision})
	if e != nil {
		t.Fatal(e)
	}
	_, e = l.change(ctx, v.ID, r.ID, r.Revision, func(a *agentRunRecord, _ time.Time) error { a.View.State = "thinking"; return nil })
	if e != nil {
		t.Fatal(e)
	}
	current, e := l.Get(ctx, v.ID, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e = l.Advance(ctx, v.ID, r.ID, current.Revision)
	if e != nil || r.State != "unknown" || *models != 0 || *executions != 0 {
		t.Fatal("restart replayed", r, e)
	}
	second, e := l.Create(ctx, v.ID, AgentRunInput{Goal: "second goal", ClientRunID: "second", ScopeRevision: v.ScopeRevision})
	if e != nil {
		t.Fatal(e)
	}
	v, e = l.sessions.SetPolicy(ctx, v.ID, v.ScopeRevision, EveryCall, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = l.Get(ctx, v.ID, second.ID); !errors.Is(e, ErrScopeConflict) {
		t.Fatal("stale scope read", e)
	}
	if _, e = l.Create(ctx, v.ID, AgentRunInput{Goal: "new scope", ClientRunID: "third", ScopeRevision: v.ScopeRevision}); e != nil {
		t.Fatal("stale run permanently blocked session", e)
	}
}

func TestAgentNativeFunctionCallsRejectUnscopedAndMultiple(t *testing.T) {
	p := &agentPrompt{RunID: "run", Step: 1, Goal: "inspect", Instructions: agentInstructions, Tools: []AgentTool{{Name: "inspect_nodes", Action: "nodes.read", NodeIDs: []string{"node-a"}}}}
	item := map[string]any{"type": "function_call", "name": "inspect_nodes", "id": "fc-one", "call_id": "call-one", "status": "completed", "arguments": `{"nodeIds":["node-a"]}`}
	response := map[string]any{"id": "response-one", "object": "response", "status": "completed", "model": "test-model", "store": false, "output": []any{item}}
	raw, _ := json.Marshal(response)
	text, e := decodeAgentDecision(raw, "test-model", p)
	if e != nil || !strings.Contains(text, "nodes.read") {
		t.Fatal(text, e)
	}
	item["arguments"] = `{"nodeIds":["node-b"]}`
	raw, _ = json.Marshal(response)
	if _, e = decodeAgentDecision(raw, "test-model", p); e == nil {
		t.Fatal("unscoped model tool call admitted")
	}
	item["arguments"] = `{"nodeIds":["node-a"]}`
	response["output"] = []any{item, item}
	raw, _ = json.Marshal(response)
	if _, e = decodeAgentDecision(raw, "test-model", p); e == nil {
		t.Fatal("multiple parallel calls admitted")
	}
}
