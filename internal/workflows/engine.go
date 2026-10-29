package workflows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrWorkflowUnavailable = errors.New("workflows: persistence or task service unavailable")
var ErrWorkflowConflict = errors.New("workflows: revision conflict")

type ActionDefinition struct {
	ID           string
	Canonicalize func(json.RawMessage) (json.RawMessage, error)
}

// WorkflowOperation is from trusted Web/trigger composition. Admit must check
// the current authenticated actor for edits and the trusted producer for events.
// The stored owner is checked again for each task; IDs alone are not identity.
type WorkflowOperation struct {
	ActorID, Action, WorkflowID, EventID string
	ExpectedRevision                     uint64
}
type WorkflowBinding struct {
	WorkflowID, RunID string
	Revision          uint64
	Step              int
}
type TaskReceipt struct{ TaskID string }
type WorkflowTaskState struct{ TaskID, State string }
type WorkflowAudit struct {
	ActorID, Action, WorkflowID, RunID, TaskID, Result string
	At                                                 time.Time
}

// All functions in WorkflowTx must bind the SAME DS transaction. SubmitTask
// calls the sole task service and consumes required approval there atomically.
// Nil functions fail closed; there is no local execution or queue fallback.
type WorkflowTx struct {
	Read       func(context.Context) ([]byte, error)
	Write      func(context.Context, []byte) error
	Admit      func(context.Context, WorkflowOperation) error
	SubmitTask func(context.Context, TaskIntent, WorkflowBinding) (TaskReceipt, error)
	TaskState  func(context.Context, string) (WorkflowTaskState, error)
	Audit      func(context.Context, WorkflowAudit) error
}
type WorkflowReadTx struct {
	Read  func(context.Context) ([]byte, error)
	Admit func(context.Context, WorkflowOperation) error
}
type WorkflowBackend struct {
	InTx func(context.Context, func(WorkflowTx) error) error
	View func(context.Context, func(WorkflowReadTx) error) error
}
type RunStep struct {
	TaskID string `json:"taskId,omitempty"`
	State  string `json:"state"`
}
type RunView struct {
	ID         string    `json:"runId"`
	WorkflowID string    `json:"workflowId"`
	Revision   uint64    `json:"workflowRevision"`
	EventID    string    `json:"eventId"`
	State      string    `json:"state"`
	Steps      []RunStep `json:"steps"`
	CreatedAt  time.Time `json:"createdAt"`
}
type workflowRun struct {
	View        RunView
	Plan        Plan
	EventDigest string
}
type workflowState struct {
	Version     int
	ClockFloor  time.Time
	Definitions map[string]Definition
	Runs        map[string]workflowRun
}
type Engine struct {
	backend WorkflowBackend
	actions map[string]ActionDefinition
	now     func() time.Time
}

func NewEngine(backend WorkflowBackend, actions []ActionDefinition) (*Engine, error) {
	if backend.InTx == nil || backend.View == nil || len(actions) == 0 || len(actions) > 256 {
		return nil, ErrWorkflowUnavailable
	}
	registry := map[string]ActionDefinition{}
	for _, a := range actions {
		if !validID(a.ID) || a.Canonicalize == nil || registry[a.ID].ID != "" {
			return nil, ErrWorkflowDenied
		}
		registry[a.ID] = a
	}
	return &Engine{backend, registry, time.Now}, nil
}
func decodeWorkflow(raw []byte) (*workflowState, error) {
	st := &workflowState{}
	if len(raw) > 8<<20 || len(raw) > 0 && (json.Unmarshal(raw, st) != nil || st.Version != 1) {
		return nil, ErrWorkflowUnavailable
	}
	st.Version = 1
	if st.Definitions == nil {
		st.Definitions = map[string]Definition{}
	}
	if st.Runs == nil {
		st.Runs = map[string]workflowRun{}
	}
	if len(st.Definitions) > 1000 || len(st.Runs) > 10000 {
		return nil, ErrWorkflowUnavailable
	}
	for id, def := range st.Definitions {
		if id != def.ID || !validID(id) || !validID(def.SubjectID) || def.Revision == 0 {
			return nil, ErrWorkflowUnavailable
		}
		copy := def
		copy.Enabled = true
		actions := []string{}
		for _, step := range def.Steps {
			actions = append(actions, step.ActionID)
		}
		if _, err := Prepare(copy, TriggerEvent{ID: "validate", Kind: def.Trigger, Values: def.Match}, actions); err != nil {
			return nil, ErrWorkflowUnavailable
		}
	}
	for id, run := range st.Runs {
		v := run.View
		p := run.Plan
		if id != v.ID || id != workflowRunID(v.WorkflowID, v.EventID) || st.Definitions[v.WorkflowID].ID == "" || v.Revision == 0 || p.WorkflowID != v.WorkflowID || p.Revision != v.Revision || p.EventID != v.EventID || len(v.Steps) == 0 || len(v.Steps) != len(p.Tasks) || len(v.Steps) > 32 || v.CreatedAt.IsZero() {
			return nil, ErrWorkflowUnavailable
		}
		hash, err := hex.DecodeString(run.EventDigest)
		if err != nil || len(hash) != 32 {
			return nil, ErrWorkflowUnavailable
		}
		copy := p
		copy.Digest = ""
		raw, _ := json.Marshal(copy)
		digest := sha256.Sum256(raw)
		if p.Digest != hex.EncodeToString(digest[:]) {
			return nil, ErrWorkflowUnavailable
		}
		switch v.State {
		case "planned", "running", "succeeded", "failed", "canceled", "paused_unknown":
		default:
			return nil, ErrWorkflowUnavailable
		}
		for _, step := range v.Steps {
			switch step.State {
			case "pending":
				if step.TaskID != "" {
					return nil, ErrWorkflowUnavailable
				}
			case "queued", "awaiting_approval", "running", "paused", "waiting_node", "succeeded", "unknown", "failed", "canceled", "expired", "timed_out":
				if !validID(step.TaskID) {
					return nil, ErrWorkflowUnavailable
				}
			default:
				return nil, ErrWorkflowUnavailable
			}
		}
	}
	return st, nil
}
func (e *Engine) transaction(ctx context.Context, op WorkflowOperation, fn func(WorkflowTx, *workflowState, time.Time) error) error {
	if e == nil || ctx == nil || ctx.Err() != nil || !validID(op.ActorID) || !validID(op.Action) {
		return ErrWorkflowDenied
	}
	err := e.backend.InTx(ctx, func(tx WorkflowTx) error {
		if tx.Read == nil || tx.Write == nil || tx.Admit == nil || tx.SubmitTask == nil || tx.TaskState == nil || tx.Audit == nil {
			return ErrWorkflowUnavailable
		}
		if tx.Admit(ctx, op) != nil {
			return ErrWorkflowDenied
		}
		raw, err := tx.Read(ctx)
		if err != nil {
			return ErrWorkflowUnavailable
		}
		st, err := decodeWorkflow(raw)
		if err != nil {
			return err
		}
		now := e.now().UTC()
		if now.IsZero() || now.Before(st.ClockFloor) {
			return ErrWorkflowUnavailable
		}
		st.ClockFloor = now
		if err = fn(tx, st, now); err != nil {
			return err
		}
		raw, err = json.Marshal(st)
		if err != nil || len(raw) > 8<<20 || ctx.Err() != nil {
			return ErrWorkflowUnavailable
		}
		return tx.Write(ctx, raw)
	})
	for _, kind := range []error{ErrWorkflowDenied, ErrWorkflowUnavailable, ErrWorkflowConflict} {
		if errors.Is(err, kind) {
			return kind
		}
	}
	if err != nil || ctx.Err() != nil {
		return ErrWorkflowUnavailable
	}
	return nil
}
func (e *Engine) canonicalDefinition(def Definition) (Definition, error) {
	raw, err := json.Marshal(def)
	if err != nil || len(raw) > 256<<10 {
		return Definition{}, ErrWorkflowDenied
	}
	var out Definition
	if json.Unmarshal(raw, &out) != nil {
		return Definition{}, ErrWorkflowDenied
	}
	if !validID(out.ID) || !validID(out.SubjectID) || out.Revision == 0 {
		return Definition{}, ErrWorkflowDenied
	}
	for i, step := range out.Steps {
		action, ok := e.actions[step.ActionID]
		if !ok {
			return Definition{}, ErrWorkflowDenied
		}
		canonical, err := action.Canonicalize(bytes.Clone(step.Parameters))
		if err != nil || len(canonical) > 64<<10 || !json.Valid(canonical) {
			return Definition{}, ErrWorkflowDenied
		}
		out.Steps[i].Parameters = bytes.Clone(canonical)
	}
	// Validate the complete shape even for disabled definitions. No action runs.
	copy := out
	copy.Enabled = true
	_, err = Prepare(copy, TriggerEvent{ID: "validation", Kind: out.Trigger, Values: out.Match}, e.actionIDs())
	if err != nil {
		return Definition{}, err
	}
	return out, nil
}
func (e *Engine) actionIDs() []string {
	out := make([]string, 0, len(e.actions))
	for id := range e.actions {
		out = append(out, id)
	}
	return out
}

// Save uses authenticated op.ActorID as owner; client SubjectID is rejected.
// expected revision zero means create. Updates keep all old run references so
// new revisions cannot make an old event/approval/task resumable.
func (e *Engine) Save(ctx context.Context, op WorkflowOperation, def Definition) (Definition, error) {
	if op.Action != "workflows.save" || op.WorkflowID != def.ID || def.SubjectID != "" && def.SubjectID != op.ActorID {
		return Definition{}, ErrWorkflowDenied
	}
	def.SubjectID = op.ActorID
	def.Revision = op.ExpectedRevision + 1
	if def.Revision == 0 {
		return Definition{}, ErrWorkflowConflict
	}
	def, err := e.canonicalDefinition(def)
	if err != nil {
		return Definition{}, err
	}
	err = e.transaction(ctx, op, func(tx WorkflowTx, st *workflowState, now time.Time) error {
		old, exists := st.Definitions[def.ID]
		if exists && (old.SubjectID != op.ActorID || old.Revision != op.ExpectedRevision) || !exists && op.ExpectedRevision != 0 {
			return ErrWorkflowConflict
		}
		if !exists && len(st.Definitions) >= 1000 {
			return ErrWorkflowUnavailable
		}
		st.Definitions[def.ID] = def
		return tx.Audit(ctx, WorkflowAudit{ActorID: op.ActorID, Action: "workflows.save", WorkflowID: def.ID, Result: "success", At: now})
	})
	if err != nil {
		return Definition{}, err
	}
	return def, nil
}
func workflowRunID(workflow, event string) string { return workflow + ":" + event }

// Trigger records a frozen plan only. Advance submits into the sole tasks store;
// recording a plan is never shown as a completed workflow or an execution.
func (e *Engine) Trigger(ctx context.Context, op WorkflowOperation, event TriggerEvent) (*RunView, error) {
	if op.Action != "workflows.trigger" || op.EventID != event.ID || !validID(event.ID) {
		return nil, ErrWorkflowDenied
	}
	eventRaw, err := json.Marshal(event)
	if err != nil || len(eventRaw) > 128<<10 {
		return nil, ErrWorkflowDenied
	}
	if json.Unmarshal(eventRaw, &event) != nil {
		return nil, ErrWorkflowDenied
	}
	var result *RunView
	eventHash := sha256.Sum256(eventRaw)
	eventDigest := hex.EncodeToString(eventHash[:])
	err = e.transaction(ctx, op, func(tx WorkflowTx, st *workflowState, now time.Time) error {
		def, exists := st.Definitions[op.WorkflowID]
		if !exists || !def.Enabled {
			return ErrWorkflowDenied
		}
		runID := workflowRunID(def.ID, event.ID)
		if old, exists := st.Runs[runID]; exists {
			if old.EventDigest != eventDigest {
				return ErrWorkflowConflict
			}
			v := old.View
			v.Steps = append([]RunStep{}, v.Steps...)
			result = &v
			return nil
		}
		if len(st.Runs) >= 10000 {
			return ErrWorkflowUnavailable
		}
		plan, err := Prepare(def, event, e.actionIDs())
		if err != nil {
			return err
		}
		if plan == nil {
			return nil
		}
		view := RunView{ID: runID, WorkflowID: def.ID, Revision: def.Revision, EventID: event.ID, State: "planned", CreatedAt: now, Steps: make([]RunStep, len(plan.Tasks))}
		for i := range view.Steps {
			view.Steps[i].State = "pending"
		}
		st.Runs[runID] = workflowRun{view, *plan, eventDigest}
		copy := view
		copy.Steps = append([]RunStep{}, view.Steps...)
		result = &copy
		return tx.Audit(ctx, WorkflowAudit{ActorID: def.SubjectID, Action: "workflows.trigger", WorkflowID: def.ID, RunID: runID, Result: "success", At: now})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (e *Engine) Advance(ctx context.Context, op WorkflowOperation) (RunView, error) {
	if op.Action != "workflows.advance" {
		return RunView{}, ErrWorkflowDenied
	}
	var result RunView
	err := e.transaction(ctx, op, func(tx WorkflowTx, st *workflowState, now time.Time) error {
		runID := workflowRunID(op.WorkflowID, op.EventID)
		run, exists := st.Runs[runID]
		if !exists {
			return ErrWorkflowDenied
		}
		def, exists := st.Definitions[op.WorkflowID]
		if !exists || !def.Enabled || def.Revision != run.View.Revision {
			run.View.State = "canceled"
			st.Runs[runID] = run
			result = run.View
			return tx.Audit(ctx, WorkflowAudit{ActorID: op.ActorID, Action: "workflows.invalidated", WorkflowID: op.WorkflowID, RunID: runID, Result: "denied", At: now})
		}
		if run.View.State == "succeeded" || run.View.State == "failed" || run.View.State == "canceled" || run.View.State == "paused_unknown" {
			result = run.View
			return nil
		}
		for i, step := range run.View.Steps {
			if step.State == "succeeded" {
				continue
			}
			if step.TaskID != "" {
				current, err := tx.TaskState(ctx, step.TaskID)
				if err != nil || current.TaskID != step.TaskID {
					return ErrWorkflowUnavailable
				}
				switch current.State {
				case "succeeded":
					step.State = "succeeded"
					run.View.Steps[i] = step
					continue
				case "queued", "awaiting_approval", "running", "paused", "waiting_node":
					step.State = current.State
					run.View.State = "running"
				case "unknown":
					step.State = "unknown"
					run.View.State = "paused_unknown"
					if err := tx.Audit(ctx, WorkflowAudit{ActorID: def.SubjectID, Action: "workflows.paused_unknown", WorkflowID: def.ID, RunID: runID, TaskID: step.TaskID, Result: "failure", At: now}); err != nil {
						return err
					}
				case "failed", "canceled", "expired", "timed_out":
					step.State = current.State
					run.View.State = "failed"
					if err := tx.Audit(ctx, WorkflowAudit{ActorID: def.SubjectID, Action: "workflows.failed", WorkflowID: def.ID, RunID: runID, TaskID: step.TaskID, Result: "failure", At: now}); err != nil {
						return err
					}
				default:
					return ErrWorkflowUnavailable
				}
				run.View.Steps[i] = step
				break
			}
			intent := run.Plan.Tasks[i]
			ownerOp := WorkflowOperation{ActorID: def.SubjectID, Action: "workflows.dispatch", WorkflowID: def.ID, EventID: run.View.EventID, ExpectedRevision: def.Revision}
			if tx.Admit(ctx, ownerOp) != nil {
				return ErrWorkflowDenied
			}
			receipt, err := tx.SubmitTask(ctx, intent, WorkflowBinding{def.ID, runID, def.Revision, i})
			if err != nil || !validID(receipt.TaskID) {
				return ErrWorkflowUnavailable
			}
			run.View.Steps[i] = RunStep{receipt.TaskID, "queued"}
			run.View.State = "running"
			if err = tx.Audit(ctx, WorkflowAudit{ActorID: def.SubjectID, Action: "workflows.task.submit", WorkflowID: def.ID, RunID: runID, TaskID: receipt.TaskID, Result: "success", At: now}); err != nil {
				return err
			}
			break
		}
		completed := true
		for _, step := range run.View.Steps {
			if step.State != "succeeded" {
				completed = false
			}
		}
		if completed {
			run.View.State = "succeeded"
			if err := tx.Audit(ctx, WorkflowAudit{ActorID: def.SubjectID, Action: "workflows.complete", WorkflowID: def.ID, RunID: runID, Result: "success", At: now}); err != nil {
				return err
			}
		}
		st.Runs[runID] = run
		result = run.View
		return nil
	})
	if err != nil {
		return RunView{}, err
	}
	result.Steps = append([]RunStep{}, result.Steps...)
	return result, nil
}
func (e *Engine) GetRun(ctx context.Context, op WorkflowOperation) (RunView, error) {
	if op.Action != "workflows.read" {
		return RunView{}, ErrWorkflowDenied
	}
	var out RunView
	err := e.transaction(ctx, op, func(_ WorkflowTx, st *workflowState, _ time.Time) error {
		run, ok := st.Runs[workflowRunID(op.WorkflowID, op.EventID)]
		if !ok {
			return ErrWorkflowDenied
		}
		out = run.View
		out.Steps = append([]RunStep{}, out.Steps...)
		return nil
	})
	if err != nil {
		return RunView{}, err
	}
	return out, nil
}

// CheckDispatch is called at every sole-task dispatch/stage, including retries,
// to prevent old frozen workflows from executing after edit/disable/revocation.
func (e *Engine) CheckDispatch(ctx context.Context, op WorkflowOperation, binding WorkflowBinding, taskID string) error {
	if op.Action != "workflows.dispatch" || op.WorkflowID != binding.WorkflowID || op.ExpectedRevision != binding.Revision || binding.Step < 0 {
		return ErrWorkflowDenied
	}
	if e == nil || ctx == nil || ctx.Err() != nil {
		return ErrWorkflowDenied
	}
	err := e.backend.View(ctx, func(tx WorkflowReadTx) error {
		if tx.Read == nil || tx.Admit == nil {
			return ErrWorkflowUnavailable
		}
		if tx.Admit(ctx, op) != nil {
			return ErrWorkflowDenied
		}
		raw, err := tx.Read(ctx)
		if err != nil {
			return ErrWorkflowUnavailable
		}
		return CheckDispatchState(raw, op, binding, taskID)
	})
	if errors.Is(err, ErrWorkflowDenied) {
		return ErrWorkflowDenied
	}
	if err != nil || ctx.Err() != nil {
		return ErrWorkflowUnavailable
	}
	return nil
}

// CheckDispatchState supports a task-owner adapter already holding the same DS
// transaction: read only this domain's payload through its approved repository,
// then call this pure validator. Never nest a second writer in task admission.
func CheckDispatchState(raw []byte, op WorkflowOperation, binding WorkflowBinding, taskID string) error {
	st, err := decodeWorkflow(raw)
	if err != nil {
		return err
	}
	def, ok := st.Definitions[binding.WorkflowID]
	run, exists := st.Runs[binding.RunID]
	if op.Action != "workflows.dispatch" || op.WorkflowID != binding.WorkflowID || op.ExpectedRevision != binding.Revision || binding.Step < 0 || !validID(taskID) || !ok || !exists || !def.Enabled || def.SubjectID != op.ActorID || def.Revision != binding.Revision || run.View.Revision != binding.Revision || run.View.State != "running" || binding.Step >= len(run.View.Steps) || run.View.Steps[binding.Step].TaskID != taskID {
		return ErrWorkflowDenied
	}
	return nil
}
