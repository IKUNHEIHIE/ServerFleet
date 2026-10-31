package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/workflows"
)

// workflowActions is the fixed trusted action allowlist automation may request.
// Execution, authorization and approval stay in the sole task service.
func workflowActions() []workflows.ActionDefinition {
	stringParameters := func(raw json.RawMessage) (json.RawMessage, error) {
		var object map[string]string
		if len(raw) == 0 || len(raw) > 16<<10 || json.Unmarshal(raw, &object) != nil {
			return nil, workflows.ErrWorkflowDenied
		}
		return json.Marshal(object)
	}
	return []workflows.ActionDefinition{
		{ID: "tasks.command", Canonicalize: stringParameters},
		{ID: "host.patch", Canonicalize: stringParameters},
		{ID: "host.root", Canonicalize: stringParameters},
	}
}

// workflowRuntime adapts the SQLite state and the sole task service to the engine.
// Workflow state updates and task submission are separate durable transactions;
// the task idempotency key makes a retried advance reuse the same task ID.
type workflowRuntime struct {
	mu        sync.Mutex
	db        *storage.DB
	tasks     *tasks.Service
	tasksRepo *tasks.Repo
	audit     *audit.SQLRecorder
	identity  *auth.IdentityService
	now       func() time.Time
}

func (s *Server) setupWorkflows() error {
	runtime := &workflowRuntime{
		db: s.DB, tasks: s.Tasks, tasksRepo: s.TasksRepo,
		audit: s.Audit, identity: s.Identity, now: s.now,
	}
	backend, err := workflows.NewSQLWorkflowBackend(workflows.SQLWorkflowStore{
		InTx: func(ctx context.Context, fn func(workflows.SQLWorkflowTx) error) error {
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			return fn(workflows.SQLWorkflowTx{
				Read:       runtime.read,
				Exec:       runtime.exec,
				Admit:      runtime.admit,
				SubmitTask: runtime.submitTask,
				TaskState:  runtime.taskState,
				Audit:      runtime.recordAudit,
			})
		},
		View: func(ctx context.Context, fn func(workflows.SQLWorkflowReadTx) error) error {
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			return fn(workflows.SQLWorkflowReadTx{Read: runtime.read, Admit: runtime.admit})
		},
	})
	if err != nil {
		return err
	}
	engine, err := workflows.NewEngine(backend, workflowActions())
	if err != nil {
		return err
	}
	s.Workflows = engine
	s.workflowState = runtime
	return nil
}

func (w *workflowRuntime) read(ctx context.Context, query string, args ...any) ([]byte, error) {
	var payload []byte
	if err := w.db.QueryRow(ctx, query, args...).Scan(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (w *workflowRuntime) exec(ctx context.Context, query string, args ...any) (int64, error) {
	var affected int64
	err := w.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		result, err := tx.Exec(txCtx, query, args...)
		if err != nil {
			return err
		}
		affected, err = result.RowsAffected()
		return err
	})
	return affected, err
}

// admit binds every engine operation to the authenticated actor and the actor's
// current permissions. A revoked or downgraded subject cannot resume a frozen run.
func (w *workflowRuntime) admit(ctx context.Context, op workflows.WorkflowOperation) error {
	principal, ok := workflows.PrincipalFromContext(ctx)
	if !ok || principal.SubjectID != op.ActorID {
		return workflows.ErrWorkflowDenied
	}
	switch op.Action {
	case workflows.OperationRead:
		return w.identity.AuthorizeSubjectAction(ctx, op.ActorID, workflows.ActionRead)
	case workflows.OperationSave, workflows.OperationTrigger, workflows.OperationAdvance, "workflows.dispatch":
		return w.identity.AuthorizeSubjectAction(ctx, op.ActorID, workflows.ActionWrite)
	default:
		return workflows.ErrWorkflowDenied
	}
}

func (w *workflowRuntime) submitTask(ctx context.Context, intent workflows.TaskIntent, binding workflows.WorkflowBinding) (workflows.TaskReceipt, error) {
	class, err := resolveOperationClass(intent.ActionID)
	if err != nil {
		return workflows.TaskReceipt{}, err
	}
	name := "workflow step " + strconv.Itoa(binding.Step+1)
	task, err := w.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:       intent.SubjectID,
		RequestID:       newRequestID(),
		Name:            name,
		ActionID:        intent.ActionID,
		OperationClass:  class,
		TargetType:      "node_ids",
		TargetNodeIDs:   intent.NodeIDs,
		CanonicalParams: intent.Parameters,
		IdempotencyKey:  intent.IdempotencyKey,
		RequireApproval: intent.HumanApproval,
		Source:          audit.SourceSystem,
	})
	if err != nil {
		return workflows.TaskReceipt{}, err
	}
	return workflows.TaskReceipt{TaskID: task.ID}, nil
}

func (w *workflowRuntime) taskState(ctx context.Context, taskID string) (workflows.WorkflowTaskState, error) {
	task, err := w.tasksRepo.Get(ctx, taskID)
	if err != nil {
		return workflows.WorkflowTaskState{}, err
	}
	return workflows.WorkflowTaskState{TaskID: task.ID, State: workflowTaskState(task.Status)}, nil
}

func workflowTaskState(state tasks.TaskStatus) string {
	switch state {
	case tasks.StatusCompleted:
		return "succeeded"
	case tasks.StatusPartialFailure:
		return "failed"
	case tasks.StatusCancelling:
		return "running"
	default:
		return string(state)
	}
}

func (w *workflowRuntime) recordAudit(ctx context.Context, event workflows.WorkflowAudit) error {
	at := event.At
	if at.IsZero() {
		at = w.now().UTC()
	}
	return w.audit.Append(ctx, audit.Event{
		SubjectID: event.ActorID,
		Action:    event.Action,
		Result:    event.Result,
		RequestID: newRequestID(),
		TaskID:    event.TaskID,
		NodeIDs:   []string{},
		Source:    audit.SourceSystem,
		At:        at,
	})
}

func (s *Server) registerWorkflowRoutes() error {
	if s.Workflows == nil {
		return nil
	}
	return workflows.RegisterHTTP(s.mux, s.Workflows, workflows.HTTPConfig{
		Authenticate: func(r *http.Request) (workflows.Principal, error) {
			identity, requestID, err := s.authenticateIdentity(r)
			if err != nil {
				return workflows.Principal{}, err
			}
			return workflows.Principal{SubjectID: identity.SubjectID, RequestID: requestID}, nil
		},
		AuthorizeAction: func(ctx context.Context, subjectID, action string) error {
			return s.Identity.AuthorizeSubjectAction(ctx, subjectID, action)
		},
		AuthorizeNode: func(ctx context.Context, subjectID, action string, nodeIDs []string) error {
			return (&composedAuthorizer{identity: s.Identity}).Authorize(ctx, auth.Check{
				SubjectID: subjectID,
				Action:    action,
				NodeIDs:   nodeIDs,
			})
		},
	})
}
