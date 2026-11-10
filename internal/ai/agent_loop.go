package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type AgentLoop struct {
	sessions     *SessionService
	conversation *ConversationService
	config       AgentLoopConfig
	locks        sync.Map
	workerMu     sync.Mutex
	workers      map[string]context.CancelFunc
	closed       bool
	wg           sync.WaitGroup
}

func NewAgentLoop(s *SessionService, c *ConversationService, cfg AgentLoopConfig) (*AgentLoop, error) {
	if s == nil || c == nil || c.sessions != s || cfg.Execute == nil || cfg.ReadTask == nil || cfg.CancelTask == nil {
		return nil, ErrSessionUnavailable
	}
	return &AgentLoop{sessions: s, conversation: c, config: cfg, workers: map[string]context.CancelFunc{}}, nil
}

func (l *AgentLoop) read(ctx context.Context, session, id string) (agentRunRecord, error) {
	view, e := l.sessions.Get(ctx, session)
	if e != nil {
		return agentRunRecord{}, e
	}
	var out agentRunRecord
	e = l.sessions.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		r := st.Sessions[session]
		a, ok := r.AgentRuns[id]
		if !ok || a.View.ScopeRevision != view.ScopeRevision {
			return ErrScopeConflict
		}
		out = a
		return nil
	})
	if e != nil {
		return out, e
	}
	for _, step := range out.View.Steps {
		if len(step.NodeIDs) == 0 {
			continue
		}
		if _, e = l.sessions.AssessTool(ctx, session, ToolCall{ToolID: step.ToolName, NodeIDs: step.NodeIDs, Parameters: step.Parameters, ScopeRevision: view.ScopeRevision}); e != nil {
			return agentRunRecord{}, e
		}
	}
	return out, nil
}
func (l *AgentLoop) Get(ctx context.Context, session, id string) (AgentRun, error) {
	a, e := l.read(ctx, session, id)
	if e != nil {
		return AgentRun{}, e
	}
	v := a.View
	view, e := l.sessions.Get(ctx, session)
	if e != nil || view.ScopeRevision != v.ScopeRevision {
		return AgentRun{}, ErrScopeConflict
	}
	for i := range v.Steps {
		if v.Steps[i].Result != "" {
			text, e := l.conversation.filter(ctx, view, "assistant", v.Steps[i].Result)
			if e != nil {
				return AgentRun{}, e
			}
			v.Steps[i].Result = text
		}
	}
	v.Goal, e = l.conversation.filter(ctx, view, "user", v.Goal)
	if e != nil {
		return AgentRun{}, e
	}
	if v.FinalText != "" {
		v.FinalText, e = l.conversation.filter(ctx, view, "assistant", v.FinalText)
		if e != nil {
			return AgentRun{}, e
		}
	}
	current, e := l.sessions.Get(ctx, session)
	if e != nil || current.ScopeRevision != v.ScopeRevision || !slices.Equal(current.AllowedNodeIDs, view.AllowedNodeIDs) {
		return AgentRun{}, ErrScopeConflict
	}
	l.workerMu.Lock()
	_, v.DriverActive = l.workers[id]
	l.workerMu.Unlock()
	return v, nil
}
func (l *AgentLoop) List(ctx context.Context, session string) ([]AgentRun, error) {
	v, e := l.sessions.Get(ctx, session)
	if e != nil {
		return nil, e
	}
	out := []AgentRun{}
	e = l.sessions.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		for _, r := range st.Sessions[session].AgentRuns {
			if r.View.ScopeRevision == v.ScopeRevision {
				out = append(out, r.View)
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	visible := []AgentRun{}
	for _, r := range out {
		v, e := l.Get(ctx, session, r.ID)
		if e == nil {
			visible = append(visible, v)
		} else if !errors.Is(e, ErrToolDenied) && !errors.Is(e, ErrScopeConflict) {
			return nil, e
		}
	}
	return visible, nil
}
func (l *AgentLoop) Create(ctx context.Context, session string, in AgentRunInput) (AgentRun, error) {
	if in.MaxSteps == 0 {
		in.MaxSteps = 8
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 900
	}
	if !validMessageContent(in.Goal) || len(in.Goal) > 24000 || !exactID(in.ClientRunID) || in.ScopeRevision == 0 || in.MaxSteps < 1 || in.MaxSteps > 12 || in.TimeoutSeconds < 60 || in.TimeoutSeconds > 3600 {
		return AgentRun{}, ErrToolDenied
	}
	actor, e := l.sessions.actor(ctx, "ai.runs.create", session, true)
	if e != nil {
		return AgentRun{}, e
	}
	view, e := l.sessions.Get(ctx, session)
	if e != nil || view.Status != "active" || view.ScopeRevision != in.ScopeRevision {
		return AgentRun{}, ErrScopeConflict
	}
	text, e := l.conversation.filter(ctx, view, "user", in.Goal)
	if e != nil {
		return AgentRun{}, e
	}
	if len(text) > 24000 {
		return AgentRun{}, ErrToolDenied
	}
	in.Goal = text
	profile, e := l.conversation.profile(ctx, actor.SubjectID)
	if e != nil {
		return AgentRun{}, e
	}
	hash := agentHash(struct {
		Input   AgentRunInput
		Profile ModelProfile
	}{in, profile})
	var out AgentRun
	e = l.sessions.change(ctx, func(st *sessionState, now time.Time) error {
		if _, e := l.sessions.recheck(ctx, actor, "ai.runs.create", session, true); e != nil {
			return e
		}
		r := st.Sessions[session]
		if r.Closed || r.Scope.SubjectID != actor.SubjectID || r.Scope.Revision != in.ScopeRevision {
			return ErrScopeConflict
		}
		for _, a := range r.AgentRuns {
			if a.ClientID == in.ClientRunID {
				if a.InputHash != hash {
					return ErrScopeConflict
				}
				out = a.View
				return nil
			}
		}
		for key, a := range r.AgentRuns {
			if !agentTerminal(a.View.State) {
				if a.View.ScopeRevision == in.ScopeRevision {
					return ErrScopeConflict
				}
				a.View.State, a.View.StopReason = "unknown", "scope_changed_no_replay"
				a.View.Revision++
				r.AgentRuns[key] = a
			}
		}
		if len(r.AgentRuns) >= 16 {
			return ErrSessionUnavailable
		}
		id, e := sessionID()
		if e != nil {
			return e
		}
		out = AgentRun{ID: id, SessionID: session, Goal: text, State: "queued", Revision: 1, ScopeRevision: in.ScopeRevision, MaxSteps: in.MaxSteps, CreatedAt: now, ExpiresAt: now.Add(time.Duration(in.TimeoutSeconds) * time.Second), Steps: []AgentStep{}}
		if r.AgentRuns == nil {
			r.AgentRuns = map[string]agentRunRecord{}
		}
		r.AgentRuns[id] = agentRunRecord{View: out, ClientID: in.ClientRunID, InputHash: hash, Profile: profile, History: []json.RawMessage{}}
		st.Sessions[session] = r
		return l.sessions.audit(st, r, "ai.runs.create", now)
	})
	return out, e
}
func (l *AgentLoop) change(ctx context.Context, session, id string, expected uint64, fn func(*agentRunRecord, time.Time) error) (agentRunRecord, error) {
	actor, e := l.sessions.actor(ctx, "ai.runs.advance", session, true)
	if e != nil {
		return agentRunRecord{}, e
	}
	var out agentRunRecord
	e = l.sessions.change(ctx, func(st *sessionState, now time.Time) error {
		if _, e := l.sessions.recheck(ctx, actor, "ai.runs.advance", session, true); e != nil {
			return e
		}
		r := st.Sessions[session]
		a, ok := r.AgentRuns[id]
		if !ok || r.Closed || r.Scope.SubjectID != actor.SubjectID || r.Scope.Revision != a.View.ScopeRevision || a.View.Revision != expected {
			return ErrScopeConflict
		}
		if e := fn(&a, now); e != nil {
			return e
		}
		a.View.Revision++
		r.AgentRuns[id] = a
		st.Sessions[session] = r
		out = a
		return l.sessions.audit(st, r, "ai.runs.advance", now)
	})
	return out, e
}
func (l *AgentLoop) stop(ctx context.Context, a agentRunRecord, state, reason string) (AgentRun, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	b, e := l.change(cleanup, a.View.SessionID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		r.View.State = state
		r.View.StopReason = reason
		if len(r.View.Steps) > 0 {
			r.View.Steps[len(r.View.Steps)-1].State = state
		}
		return nil
	})
	if e != nil {
		return AgentRun{}, e
	}
	return b.View, nil
}
func (l *AgentLoop) catalog(ctx context.Context, v SessionView) ([]AgentTool, error) {
	var scope Scope
	if e := l.sessions.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		r, ok := st.Sessions[v.ID]
		if !ok || r.Closed || r.Scope.Revision != v.ScopeRevision || r.Scope.SubjectID != v.CreatedBy {
			return ErrScopeConflict
		}
		scope = recordScope(r)
		return nil
	}); e != nil {
		return nil, e
	}
	defs := []AgentTool{{Name: "inspect_nodes", Action: "nodes.read", Description: "Read scoped node inventory and latest reported metrics. Never discovers ungranted nodes."}, {Name: "self_check", Action: "tasks.command", Description: "Run the fixed Agent self-check (no arbitrary command)."}, {Name: "run_profile", Action: "tasks.exec.v1", Description: "Run a preconfigured ordinary-user executable profile. User must supply profileId and policySha256. arguments is a JSON string array; timeoutSeconds is a decimal string. Human approval required.", Profile: true}, {Name: "run_root_profile", Action: "host.root", Description: "Run an explicitly installed root-owned command profile through the approved helper. Never arbitrary shell. Exact profile/policy/arguments and human administrator approvals required.", Profile: true}}
	out := []AgentTool{}
	for _, d := range defs {
		tool, ok := l.sessions.tools[d.Action]
		if !ok {
			continue
		}
		params := json.RawMessage(`{}`)
		if d.Profile {
			params = json.RawMessage(`{"profileId":"catalog","arguments":"[]","timeoutSeconds":"1","policySha256":"` + strings.Repeat("0", 64) + `"}`)
		}
		params, e := tool.Canonicalize(params)
		if e != nil {
			return nil, e
		}
		authority, e := l.sessions.authority(ctx, v.CreatedBy, d.Action)
		if e != nil {
			return nil, ErrSessionUnavailable
		}
		for _, node := range v.AllowedNodeIDs[:min(200, len(v.AllowedNodeIDs))] {
			e := EvaluateTool(scope, ToolCall{ToolID: d.Action, NodeIDs: []string{node}, Parameters: params, ScopeRevision: v.ScopeRevision}, tool.Definition, authority, nil, l.sessions.now().UTC())
			if e == nil || errors.Is(e, ErrApprovalRequired) {
				d.NodeIDs = append(d.NodeIDs, node)
			}
		}
		if len(d.NodeIDs) > 0 {
			out = append(out, d)
		}
	}
	current, e := l.sessions.Get(ctx, v.ID)
	if e != nil || current.ScopeRevision != v.ScopeRevision {
		return nil, ErrScopeConflict
	}
	return out, nil
}
func (l *AgentLoop) Advance(ctx context.Context, session, id string, expected uint64) (AgentRun, error) {
	lock, _ := l.locks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	if !mu.TryLock() {
		return AgentRun{}, ErrScopeConflict
	}
	defer mu.Unlock()
	a, e := l.read(ctx, session, id)
	if e != nil {
		return AgentRun{}, e
	}
	if a.View.Revision != expected {
		return AgentRun{}, ErrScopeConflict
	}
	if agentTerminal(a.View.State) {
		return a.View, nil
	}
	if !l.sessions.now().Before(a.View.ExpiresAt) {
		state := "failed"
		if a.View.State == "waiting_task" || a.View.State == "canceling" || a.View.State == "thinking" || a.View.State == "submitting" {
			state = "unknown"
		}
		return l.stop(ctx, a, state, "run_deadline_expired")
	}
	if a.View.State == "queued" || a.View.State == "waiting_approval" {
		current, e := l.sessions.Get(ctx, session)
		if e != nil {
			return AgentRun{}, e
		}
		profile, e := l.conversation.profile(ctx, current.CreatedBy)
		if e != nil || profile != a.Profile {
			return l.stop(ctx, a, "failed", "model_profile_changed_or_unavailable")
		}
	}
	switch a.View.State {
	case "thinking", "submitting":
		return l.stop(ctx, a, "unknown", "interrupted_step_not_replayed")
	case "waiting_task", "canceling":
		return l.observeTask(ctx, a)
	case "waiting_approval":
		s := a.View.Steps[len(a.View.Steps)-1]
		var approval *ApprovalRequest
		e = l.sessions.store.View(ctx, func(raw []byte) error {
			st, e := decodeSessions(raw)
			if e != nil {
				return e
			}
			p, ok := st.Sessions[session].Approvals[s.ApprovalID]
			if ok {
				v := approvalRequestView(p, l.sessions.now().UTC())
				approval = &v
			}
			return nil
		})
		if e != nil {
			return AgentRun{}, e
		}
		if p := approval; p != nil {
			switch p.State {
			case "pending":
				return a.View, nil
			case "approved":
				return l.execute(ctx, a, p.ID)
			default:
				return l.stop(ctx, a, "failed", "approval_denied_expired_or_consumed")
			}
		}
		return l.stop(ctx, a, "failed", "approval_missing")
	case "queued":
		return l.think(ctx, a)
	default:
		return AgentRun{}, ErrScopeConflict
	}
}
func (l *AgentLoop) think(ctx context.Context, a agentRunRecord) (AgentRun, error) {
	if len(a.View.Steps) >= a.View.MaxSteps {
		return l.stop(ctx, a, "limit_reached", "maximum_model_steps_reached")
	}
	view, e := l.sessions.Get(ctx, a.View.SessionID)
	if e != nil {
		return AgentRun{}, e
	}
	tools, e := l.catalog(ctx, view)
	if e != nil {
		return AgentRun{}, e
	}
	p := &agentPrompt{RunID: a.View.ID, Step: len(a.View.Steps) + 1, Goal: a.View.Goal, Tools: tools, History: a.History, Instructions: agentInstructions}
	if validateAgentPrompt(p) != nil {
		return l.stop(ctx, a, "limit_reached", "context_size_limit")
	}
	a, e = l.change(ctx, view.ID, a.View.ID, a.View.Revision, func(r *agentRunRecord, now time.Time) error {
		r.View.State = "thinking"
		r.View.Steps = append(r.View.Steps, AgentStep{Number: p.Step, State: "thinking", NodeIDs: []string{}, StartedAt: now})
		r.PlanningSHA = agentHash(p)
		return nil
	})
	if e != nil {
		return AgentRun{}, e
	}
	input := "Agent run " + a.View.ID + " step " + jsonNumber(p.Step) + ": " + a.View.Goal
	modelCtx := context.WithValue(ctx, agentPromptKey{}, p)
	message, e := l.conversation.Submit(modelCtx, view.ID, "ag-"+a.View.ID+"-"+jsonNumber(p.Step), MessageInput{Content: input, ClientMessageID: "ag-" + a.View.ID + "-" + jsonNumber(p.Step), ScopeRevision: view.ScopeRevision})
	if e != nil {
		return l.stop(ctx, a, "unknown", "planning_message_not_confirmed")
	}
	a, e = l.change(ctx, view.ID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		r.View.Steps[p.Step-1].MessageID = message.MessageID
		return nil
	})
	if e != nil {
		return AgentRun{}, e
	}
	response, e := l.conversation.Generate(modelCtx, view.ID, message.MessageID)
	if e != nil {
		return l.stop(ctx, a, "unknown", "model_response_not_confirmed")
	}
	var decision agentDecision
	if decodeAgentJSON([]byte(response.RedactedContent), &decision) != nil {
		return l.stop(ctx, a, "unknown", "invalid_model_decision")
	}
	a, e = l.change(ctx, view.ID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		r.History = append(r.History, decision.Items...)
		b, _ := json.Marshal(r.History)
		if len(b) > 128<<10 {
			return ErrSessionUnavailable
		}
		if decision.Call == nil {
			if strings.TrimSpace(decision.Text) != "" {
				r.View.State = "completed"
				r.View.FinalText = decision.Text
				r.View.Steps[p.Step-1].State = "completed"
			} else {
				r.View.State = "queued"
				r.View.Steps[p.Step-1].State = "completed"
			}
			return nil
		}
		r.Call = decision.Call
		s := &r.View.Steps[p.Step-1]
		s.ToolName = decision.Call.ToolName
		s.NodeIDs = slices.Clone(decision.Call.NodeIDs)
		s.Parameters = bytes.Clone(decision.Call.Parameters)
		return nil
	})
	if e != nil {
		return AgentRun{}, e
	}
	if decision.Call == nil {
		return a.View, nil
	}
	// The provider is a proposal source. Independently canonicalize, recheck
	// scope and stop repeated side effects before creating any approval/task.
	call := a.Call
	assessment, e := l.sessions.AssessTool(ctx, view.ID, ToolCall{ToolID: call.ToolName, NodeIDs: call.NodeIDs, Parameters: call.Parameters, ScopeRevision: view.ScopeRevision})
	if e != nil {
		return l.stop(ctx, a, "failed", "tool_or_resource_denied")
	}
	sanitized, e := l.conversation.filter(ctx, view, "user", string(assessment.Call.Parameters))
	if e != nil || sanitized != string(assessment.Call.Parameters) {
		return l.stop(ctx, a, "failed", "secret_or_invalid_tool_parameters")
	}
	if !l.sessions.tools[call.ToolName].Definition.ReadOnly {
		for _, old := range a.View.Steps[:len(a.View.Steps)-1] {
			if old.ToolName == call.ToolName && sameAgentNodes(old.NodeIDs, call.NodeIDs) && bytes.Equal(old.Parameters, assessment.Call.Parameters) {
				return l.stop(ctx, a, "failed", "repeated_side_effect_requires_manual_review")
			}
		}
	}
	if assessment.RequiresApproval {
		approval, e := l.sessions.RequestApproval(ctx, view.ID, ApprovalInput{ToolName: call.ToolName, NodeIDs: call.NodeIDs, Parameters: assessment.Call.Parameters, ScopeRevision: view.ScopeRevision})
		if e != nil {
			return l.stop(ctx, a, "failed", "approval_request_denied")
		}
		a, e = l.change(ctx, view.ID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
			r.View.State = "waiting_approval"
			s := &r.View.Steps[p.Step-1]
			s.State = "waiting_approval"
			s.ApprovalID = approval.ID
			return nil
		})
		return a.View, e
	}
	return l.execute(ctx, a, "")
}
func (l *AgentLoop) execute(ctx context.Context, a agentRunRecord, approval string) (AgentRun, error) {
	call := a.Call
	if call == nil {
		return l.stop(ctx, a, "failed", "missing_tool_call")
	}
	var e error
	a, e = l.change(ctx, a.View.SessionID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		r.View.State = "submitting"
		r.View.Steps[len(r.View.Steps)-1].State = "submitting"
		return nil
	})
	if e != nil {
		return AgentRun{}, e
	}
	execCtx := context.WithValue(ctx, agentInvocationKey{}, agentKey(a.View.ID, len(a.View.Steps)))
	result, e := l.sessions.Execute(execCtx, a.View.SessionID, ToolExecutionInput{ToolName: call.ToolName, NodeIDs: call.NodeIDs, Parameters: call.Parameters, ScopeRevision: a.View.ScopeRevision}, approval, l.config.Execute)
	if e != nil {
		return l.stop(ctx, a, "unknown", "tool_submission_not_confirmed")
	}
	if result.TaskID == "" {
		return l.completeTool(ctx, a, string(result.Observation), "")
	}
	a, e = l.change(ctx, a.View.SessionID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		r.View.State = "waiting_task"
		s := &r.View.Steps[len(r.View.Steps)-1]
		s.State = "waiting_task"
		s.TaskID = result.TaskID
		s.TaskState = result.State
		return nil
	})
	return a.View, e
}
func (l *AgentLoop) binding(a agentRunRecord) AgentTaskBinding {
	s := a.View.Steps[len(a.View.Steps)-1]
	return AgentTaskBinding{Key: agentKey(a.View.ID, s.Number), TaskID: s.TaskID, Action: s.ToolName, Parameters: string(s.Parameters), ScopeRevision: a.View.ScopeRevision, NodeIDs: slices.Clone(s.NodeIDs), Cancellation: a.View.State == "canceling"}
}
func (l *AgentLoop) observeTask(ctx context.Context, a agentRunRecord) (AgentRun, error) {
	b := l.binding(a)
	snapshot, e := l.config.ReadTask(ctx, a.View.SessionID, b)
	if e != nil {
		return l.stop(ctx, a, "unknown", "task_observation_not_authorized_or_available")
	}
	if snapshot.State == "unknown" {
		return l.stop(ctx, a, "unknown", "task_outcome_unknown_no_replay")
	}
	if slices.Contains([]string{"awaiting_approval", "queued", "running", "cancelling"}, snapshot.State) {
		s := a.View.Steps[len(a.View.Steps)-1]
		if s.TaskState == snapshot.State {
			return a.View, nil
		}
		a, e = l.change(ctx, a.View.SessionID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
			r.View.Steps[len(r.View.Steps)-1].TaskState = snapshot.State
			return nil
		})
		return a.View, e
	}
	if !slices.Contains([]string{"completed", "partial_failure", "failed", "canceled", "expired"}, snapshot.State) {
		return l.stop(ctx, a, "unknown", "unrecognized_task_result")
	}
	if a.View.State == "canceling" {
		return l.stop(ctx, a, "canceled", "cancel_requested_task_terminal_"+snapshot.State)
	}
	return l.completeTool(ctx, a, snapshot.Result, snapshot.State)
}
func (l *AgentLoop) completeTool(ctx context.Context, a agentRunRecord, result, taskState string) (AgentRun, error) {
	if len(result) == 0 || len(result) > 32<<10 {
		return l.stop(ctx, a, "unknown", "unbounded_or_empty_tool_result")
	}
	view, e := l.sessions.Get(ctx, a.View.SessionID)
	if e != nil {
		return AgentRun{}, e
	}
	text, e := l.conversation.filter(ctx, view, "assistant", result)
	if e != nil {
		return l.stop(ctx, a, "failed", "tool_result_disclosure_denied")
	}
	a, e = l.change(ctx, a.View.SessionID, a.View.ID, a.View.Revision, func(r *agentRunRecord, _ time.Time) error {
		s := &r.View.Steps[len(r.View.Steps)-1]
		s.State = "completed"
		if s.TaskID != "" {
			s.TaskState = taskState
		}
		s.Result = text
		r.View.State = "queued"
		b, _ := json.Marshal(map[string]string{"type": "function_call_output", "call_id": r.Call.CallID, "output": text})
		r.History = append(r.History, b)
		r.Call = nil
		if b, _ := json.Marshal(r.History); len(b) > 128<<10 {
			return ErrSessionUnavailable
		}
		return nil
	})
	return a.View, e
}
func (l *AgentLoop) Cancel(ctx context.Context, session, id string, expected uint64) (AgentRun, error) {
	a, e := l.read(ctx, session, id)
	if e != nil {
		return AgentRun{}, e
	}
	if a.View.Revision != expected {
		return AgentRun{}, ErrScopeConflict
	}
	if agentTerminal(a.View.State) {
		return a.View, nil
	}
	if a.View.State == "submitting" {
		v, e := l.stop(ctx, a, "unknown", "canceled_during_unconfirmed_submission")
		if e == nil {
			l.interrupt(id)
		}
		return v, e
	}
	if a.View.State != "waiting_task" && a.View.State != "canceling" {
		v, e := l.stop(ctx, a, "canceled", "operator_canceled")
		if e == nil {
			l.interrupt(id)
		}
		return v, e
	}
	a, e = l.change(ctx, session, id, expected, func(r *agentRunRecord, _ time.Time) error { r.View.State = "canceling"; return nil })
	if e != nil {
		return AgentRun{}, e
	}
	if e = l.config.CancelTask(ctx, session, l.binding(a)); e != nil {
		return l.stop(ctx, a, "unknown", "task_cancellation_not_confirmed")
	}
	return a.View, nil
}

// CheckAgentTask is a read-only CURRENT gate for the unified scheduler and
// per-frame/privileged admission, including after the initiating HTTP ended.
// A consumed AI approval authorizes only its ONE immutable bound task.
func (s *SessionService) CheckAgentTask(ctx context.Context, b AgentTaskBinding) error {
	if !strings.HasPrefix(b.Key, "ai-agent-") {
		return ErrToolDenied
	}
	var record sessionRecord
	var run agentRunRecord
	found := false
	e := s.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		for _, r := range st.Sessions {
			for _, a := range r.AgentRuns {
				if len(a.View.Steps) > 0 && agentKey(a.View.ID, len(a.View.Steps)) == b.Key {
					record, run, found = r, a, true
					return nil
				}
			}
		}
		return ErrToolDenied
	})
	if e != nil || !found {
		return ErrToolDenied
	}
	if record.Closed || record.Scope.SubjectID != b.SubjectID || record.Scope.Revision != b.ScopeRevision || run.View.ScopeRevision != b.ScopeRevision || !s.now().Before(run.View.ExpiresAt) || run.View.State != "submitting" && run.View.State != "waiting_task" && run.View.State != "canceling" {
		return ErrToolDenied
	}
	if run.View.State == "canceling" && !b.Cancellation {
		return ErrToolDenied
	}
	step := run.View.Steps[len(run.View.Steps)-1]
	if step.ToolName != b.Action || string(step.Parameters) != b.Parameters || !sameAgentNodes(step.NodeIDs, b.NodeIDs) || step.TaskID != "" && step.TaskID != b.TaskID {
		return ErrToolDenied
	}
	tool, ok := s.tools[b.Action]
	if !ok {
		return ErrToolDenied
	}
	call := ToolCall{ToolID: b.Action, NodeIDs: b.NodeIDs, Parameters: step.Parameters, ScopeRevision: b.ScopeRevision}
	authority, e := s.authority(ctx, b.SubjectID, b.Action)
	if e != nil {
		return ErrToolDenied
	}
	scope := recordScope(record)
	decision := EvaluateTool(scope, call, tool.Definition, authority, nil, s.now().UTC())
	if decision == nil {
		return nil
	}
	if !errors.Is(decision, ErrApprovalRequired) {
		return ErrToolDenied
	}
	p, ok := record.Approvals[step.ApprovalID]
	digest, e := BindCall(scope, call)
	if !ok || e != nil || p.State != "consumed" || p.SubjectID != b.SubjectID || p.ToolName != b.Action || p.Digest != digest || p.ScopeRevision != b.ScopeRevision || !sameAgentNodes(p.NodeIDs, b.NodeIDs) || !p.HumanWeb || !s.now().Before(p.ExpiresAt) || tool.Definition.RootShell && !p.Administrator {
		return ErrToolDenied
	}
	return nil
}
func sameAgentNodes(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}
