package tasks

import (
	"context"
	"errors"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
)

const TargetTypeController = "controller"

type ControllerAction struct {
	AllowSystem     bool
	RequireApproval bool
}

// ControllerExecution describes the REAL signed Controller authority. It does
// not create an Agent/node record, heartbeat, WSS link, or second scheduler.
// Only explicitly registered local workers can receive this target kind.
type ControllerExecution struct {
	AuthorityID string
	Actions     map[string]ControllerAction
	Authorize   func(context.Context, string, string, string) error
}
type controllerAdmissionKey struct{}

func cloneControllerExecution(value *ControllerExecution, workers map[string]Worker) (*ControllerExecution, error) {
	if value == nil {
		return nil, nil
	}
	if !validTargetID(value.AuthorityID) || value.Authorize == nil || len(value.Actions) == 0 {
		return nil, ErrInvalidTask
	}
	out := &ControllerExecution{AuthorityID: value.AuthorityID, Authorize: value.Authorize, Actions: map[string]ControllerAction{}}
	for action, rule := range value.Actions {
		if !validToken(action, 128) || workers[action] == nil {
			return nil, ErrInvalidTask
		}
		out.Actions[action] = rule
	}
	return out, nil
}

func (s *Service) SubmitController(ctx context.Context, request SubmitRequest) (Task, error) {
	if s == nil || s.controller == nil || ctx == nil {
		return Task{}, ErrInvalidTask
	}
	if _, ok := s.controller.Actions[request.ActionID]; !ok {
		return Task{}, ErrInvalidTask
	}
	request.TargetType = TargetTypeController
	request.TargetResourceID = s.controller.AuthorityID
	request.TargetNodeIDs = []string{s.controller.AuthorityID}
	return s.Submit(context.WithValue(ctx, controllerAdmissionKey{}, s), request)
}
func (s *Service) controllerRule(t Task) (ControllerAction, error) {
	if s == nil || s.controller == nil || t.TargetType != TargetTypeController || t.TargetResourceID != s.controller.AuthorityID || len(t.Targets) != 1 || t.Targets[0].NodeID != s.controller.AuthorityID || t.ScopeRevision != 0 || t.PluginID != "" || t.OperationClass == OperationClassRoot || s.workers[t.ActionID] == nil {
		return ControllerAction{}, ErrInvalidTask
	}
	rule, ok := s.controller.Actions[t.ActionID]
	if !ok {
		return rule, ErrInvalidTask
	}
	if t.Source != string(audit.SourceWeb) && (t.Source != string(audit.SourceSystem) || !rule.AllowSystem) {
		return rule, ErrInvalidTask
	}
	return rule, nil
}
func (s *Service) authorizeForTask(ctx context.Context, t Task, subjectID, action, requestID string, ids []string, reason string) error {
	if s.taskCurrent != nil && reason != "task_cancel" {
		if err := s.taskCurrent(ctx, t); err != nil {
			return err
		}
	}
	if t.TargetType != TargetTypeController {
		return s.authorize(ctx, subjectID, action, requestID, ids, reason)
	}
	if _, err := s.controllerRule(t); err != nil {
		return err
	}
	if s.runtimeGate != nil {
		if err := s.runtimeGate(ctx); err != nil {
			return err
		}
	}
	if err := s.controller.Authorize(ctx, subjectID, t.ActionID, action); err != nil {
		if errors.Is(err, auth.ErrDenied) {
			auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.audit.Append(auditCtx, audit.Event{SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID, TaskID: t.ID, Source: audit.SourceWeb, ReasonCode: reason, At: s.now()})
		}
		return err
	}
	return nil
}
func taskAuditIDs(t Task) []string {
	if t.TargetType == TargetTypeController {
		return nil
	}
	return taskTargetIDs(t)
}
func auditSnapshotIDs(kind string, ids []string) []string {
	if kind == TargetTypeController {
		return nil
	}
	return ids
}
