package workflows

import (
	"bytes"
	"testing"
)

func TestWorkflowFrozenInputsStableIdempotencyAndConditions(t *testing.T) {
	def := Definition{"wf-1", 1, "owner", true, "monitor.alert", map[string]string{"severity": "high"}, []Step{{"service.restart", []string{"node-a"}, []byte(`{"unit":"test"}`), true}}}
	event := TriggerEvent{"event-1", "monitor.alert", map[string]string{"severity": "high"}}
	plan, e := Prepare(def, event, []string{"service.restart"})
	if e != nil || plan == nil {
		t.Fatal(e)
	}
	replay, e := Prepare(def, event, []string{"service.restart"})
	if e != nil || replay.Digest != plan.Digest || replay.Tasks[0].IdempotencyKey != plan.Tasks[0].IdempotencyKey {
		t.Fatal("unstable replay", e)
	}
	def.Steps[0].NodeIDs[0] = "new-node"
	def.Steps[0].Parameters[2] = 'X'
	if plan.Tasks[0].NodeIDs[0] != "node-a" || !bytes.Equal(plan.Tasks[0].Parameters, []byte(`{"unit":"test"}`)) || !plan.Tasks[0].HumanApproval {
		t.Fatal("plan mutated")
	}
	event.Values["severity"] = "low"
	if p, e := Prepare(def, event, []string{"service.restart"}); p != nil || e != nil {
		t.Fatal(p, e)
	}
}
func TestWorkflowUnknownActionWildcardAndDisabledDenied(t *testing.T) {
	def := Definition{ID: "wf", Revision: 1, SubjectID: "u", Enabled: true, Trigger: "manual", Steps: []Step{{ActionID: "shell", NodeIDs: []string{"n"}, Parameters: []byte(`{}`)}}}
	event := TriggerEvent{ID: "event", Kind: "manual"}
	if _, e := Prepare(def, event, nil); e != ErrWorkflowDenied {
		t.Fatal(e)
	}
	def.Steps[0].NodeIDs = []string{"*"}
	if _, e := Prepare(def, event, []string{"shell"}); e != ErrWorkflowDenied {
		t.Fatal(e)
	}
	def.Enabled = false
	if _, e := Prepare(def, event, []string{"shell"}); e != ErrWorkflowDenied {
		t.Fatal(e)
	}
}
