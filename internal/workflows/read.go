package workflows

import (
	"bytes"
	"context"
	"errors"
	"sort"
)

// Read operations never write persistence. They still call the composition Admit
// port so a revoked or downgraded subject cannot keep reading frozen definitions.

func copyDefinition(def Definition) Definition {
	out := def
	out.Match = map[string]string{}
	for key, value := range def.Match {
		out.Match[key] = value
	}
	out.Steps = make([]Step, len(def.Steps))
	for i, step := range def.Steps {
		out.Steps[i] = step
		out.Steps[i].NodeIDs = append([]string(nil), step.NodeIDs...)
		out.Steps[i].Parameters = bytes.Clone(step.Parameters)
	}
	return out
}

func (e *Engine) readState(ctx context.Context, op WorkflowOperation) (*workflowState, error) {
	if e == nil || ctx == nil || ctx.Err() != nil || op.Action != "workflows.read" || !validID(op.ActorID) {
		return nil, ErrWorkflowDenied
	}
	var state *workflowState
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
		state, err = decodeWorkflow(raw)
		return err
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrWorkflowDenied):
			return nil, ErrWorkflowDenied
		case errors.Is(err, ErrWorkflowConflict):
			return nil, ErrWorkflowConflict
		default:
			return nil, ErrWorkflowUnavailable
		}
	}
	if ctx.Err() != nil {
		return nil, ErrWorkflowUnavailable
	}
	return state, nil
}

// List returns only definitions owned by op.ActorID. Definitions are owner-scoped;
// an administrator reads their own automation too, never another subject's.
func (e *Engine) List(ctx context.Context, op WorkflowOperation) ([]Definition, error) {
	state, err := e.readState(ctx, op)
	if err != nil {
		return nil, err
	}
	out := make([]Definition, 0, len(state.Definitions))
	for _, def := range state.Definitions {
		if def.SubjectID == op.ActorID {
			out = append(out, copyDefinition(def))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns one owner-scoped definition. Other owners observe not-found.
func (e *Engine) Get(ctx context.Context, op WorkflowOperation, workflowID string) (Definition, error) {
	if !validID(workflowID) || op.WorkflowID != "" && op.WorkflowID != workflowID {
		return Definition{}, ErrWorkflowDenied
	}
	op.WorkflowID = workflowID
	state, err := e.readState(ctx, op)
	if err != nil {
		return Definition{}, err
	}
	def, exists := state.Definitions[workflowID]
	if !exists || def.SubjectID != op.ActorID {
		return Definition{}, ErrWorkflowDenied
	}
	return copyDefinition(def), nil
}

// Runs lists the newest bounded run history for one owner-scoped workflow.
func (e *Engine) Runs(ctx context.Context, op WorkflowOperation, workflowID string) ([]RunView, error) {
	if !validID(workflowID) {
		return nil, ErrWorkflowDenied
	}
	op.WorkflowID = workflowID
	state, err := e.readState(ctx, op)
	if err != nil {
		return nil, err
	}
	def, exists := state.Definitions[workflowID]
	if !exists || def.SubjectID != op.ActorID {
		return nil, ErrWorkflowDenied
	}
	out := make([]RunView, 0, len(state.Runs))
	for _, run := range state.Runs {
		if run.View.WorkflowID != workflowID {
			continue
		}
		view := run.View
		view.Steps = append([]RunStep{}, view.Steps...)
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
