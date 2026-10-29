// Package workflows plans automation for the sole Controller task pipeline.
package workflows

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

var ErrWorkflowDenied = errors.New("workflows: plan rejected")

type Definition struct {
	ID        string
	Revision  uint64
	SubjectID string
	Enabled   bool
	Trigger   string
	Match     map[string]string
	Steps     []Step
}
type Step struct {
	ActionID      string
	NodeIDs       []string
	Parameters    json.RawMessage
	HumanApproval bool
}
type TriggerEvent struct {
	ID     string
	Kind   string
	Values map[string]string
}
type TaskIntent struct {
	SubjectID      string
	ActionID       string
	NodeIDs        []string
	Parameters     json.RawMessage
	IdempotencyKey string
	HumanApproval  bool
}
type Plan struct {
	WorkflowID string
	Revision   uint64
	EventID    string
	Digest     string
	Tasks      []TaskIntent
}

func validID(s string) bool {
	return s != "" && len(s) <= 128 && !strings.ContainsAny(s, "*?[] \t\r\n\x00")
}

// Prepare does not authorize or execute. The trusted trigger producer supplies
// event facts; arbitrary expression/script evaluation is deliberately absent.
// Unmatched conditions yield no plan, never a fabricated completed run. Each
// intent must pass current authorization/approval/audit in tasks at dispatch.
func Prepare(def Definition, event TriggerEvent, registeredActions []string) (*Plan, error) {
	if !def.Enabled || !validID(def.ID) || !validID(def.SubjectID) || def.Revision == 0 || !validID(event.ID) || len(def.Steps) == 0 || len(def.Steps) > 32 || len(def.Match) > 32 || len(event.Values) > 128 {
		return nil, ErrWorkflowDenied
	}
	switch def.Trigger {
	case "schedule", "monitor.alert", "task.completed", "manual":
	default:
		return nil, ErrWorkflowDenied
	}
	if event.Kind != def.Trigger {
		return nil, nil
	}
	for key, value := range def.Match {
		if !validID(key) || len(value) > 1024 {
			return nil, ErrWorkflowDenied
		}
		actual, exists := event.Values[key]
		if !exists || actual != value {
			return nil, nil
		}
	}
	plan := &Plan{WorkflowID: def.ID, Revision: def.Revision, EventID: event.ID}
	for i, step := range def.Steps {
		if !validID(step.ActionID) || !slices.Contains(registeredActions, step.ActionID) || len(step.NodeIDs) == 0 || len(step.NodeIDs) > 1000 || len(step.Parameters) > 64<<10 || !json.Valid(step.Parameters) {
			return nil, ErrWorkflowDenied
		}
		seen := map[string]bool{}
		for _, id := range step.NodeIDs {
			if !validID(id) || seen[id] {
				return nil, ErrWorkflowDenied
			}
			seen[id] = true
		}
		keyData, _ := json.Marshal(struct {
			ID, Event string
			Revision  uint64
			Step      int
		}{def.ID, event.ID, def.Revision, i})
		key := sha256.Sum256(keyData)
		plan.Tasks = append(plan.Tasks, TaskIntent{def.SubjectID, step.ActionID, slices.Clone(step.NodeIDs), slices.Clone(step.Parameters), "workflow-" + hex.EncodeToString(key[:]), step.HumanApproval})
	}
	data, _ := json.Marshal(plan)
	digest := sha256.Sum256(data)
	plan.Digest = hex.EncodeToString(digest[:])
	return plan, nil
}
