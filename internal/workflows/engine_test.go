package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// A transaction/task double ONLY. Real SQLite tests separately verify the
// persistence adapter; no fake task is used in a production composition.
type workflowTestDB struct {
	mu        sync.Mutex
	payload   []byte
	tasks     map[string]string
	keys      map[string]string
	audits    []WorkflowAudit
	fail      bool
	denyOwner bool
}

func workflowTestBackend(db *workflowTestDB) WorkflowBackend {
	admit := func(_ context.Context, op WorkflowOperation) error {
		if op.ActorID != "owner" && op.ActorID != "trigger-service" || db.denyOwner && op.Action == "workflows.dispatch" {
			return ErrWorkflowDenied
		}
		return nil
	}
	return WorkflowBackend{
		View: func(ctx context.Context, fn func(WorkflowReadTx) error) error {
			db.mu.Lock()
			defer db.mu.Unlock()
			return fn(WorkflowReadTx{func(context.Context) ([]byte, error) { return bytes.Clone(db.payload), nil }, admit})
		},
		InTx: func(ctx context.Context, fn func(WorkflowTx) error) error {
			db.mu.Lock()
			defer db.mu.Unlock()
			tasks := map[string]string{}
			for k, v := range db.tasks {
				tasks[k] = v
			}
			keys := map[string]string{}
			for k, v := range db.keys {
				keys[k] = v
			}
			payload := bytes.Clone(db.payload)
			audits := append([]WorkflowAudit{}, db.audits...)
			err := fn(WorkflowTx{Read: func(context.Context) ([]byte, error) { return bytes.Clone(payload), nil }, Write: func(_ context.Context, b []byte) error { payload = bytes.Clone(b); return nil }, Admit: admit, SubmitTask: func(_ context.Context, intent TaskIntent, _ WorkflowBinding) (TaskReceipt, error) {
				if id := keys[intent.IdempotencyKey]; id != "" {
					return TaskReceipt{id}, nil
				}
				id := fmt.Sprintf("task-%d", len(tasks)+1)
				tasks[id] = "queued"
				keys[intent.IdempotencyKey] = id
				return TaskReceipt{id}, nil
			}, TaskState: func(_ context.Context, id string) (WorkflowTaskState, error) {
				state, ok := tasks[id]
				if !ok {
					return WorkflowTaskState{}, ErrWorkflowUnavailable
				}
				return WorkflowTaskState{id, state}, nil
			}, Audit: func(_ context.Context, e WorkflowAudit) error { audits = append(audits, e); return nil }})
			if err != nil {
				return err
			}
			if db.fail {
				return errors.New("failed commit")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			db.payload = payload
			db.tasks = tasks
			db.keys = keys
			db.audits = audits
			return nil
		}}
}
func workflowFixture(t *testing.T) (*Engine, *workflowTestDB, WorkflowOperation, TriggerEvent) {
	t.Helper()
	db := &workflowTestDB{tasks: map[string]string{}, keys: map[string]string{}}
	engine, err := NewEngine(workflowTestBackend(db), []ActionDefinition{{"fixed.read", func(raw json.RawMessage) (json.RawMessage, error) {
		if string(raw) != `{}` {
			return nil, ErrWorkflowDenied
		}
		return bytes.Clone(raw), nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	op := WorkflowOperation{ActorID: "owner", Action: "workflows.save", WorkflowID: "workflow-1"}
	_, err = engine.Save(context.Background(), op, Definition{ID: op.WorkflowID, Enabled: true, Trigger: "manual", Steps: []Step{{ActionID: "fixed.read", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`)}, {ActionID: "fixed.read", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`), HumanApproval: true}}})
	if err != nil {
		t.Fatal(err)
	}
	event := TriggerEvent{ID: "event-1", Kind: "manual"}
	op.Action = "workflows.trigger"
	op.EventID = event.ID
	return engine, db, op, event
}
func TestWorkflowSequentialTasksAtMostOnceAndUnknownPauses(t *testing.T) {
	ctx := context.Background()
	engine, db, op, event := workflowFixture(t)
	run, err := engine.Trigger(ctx, op, event)
	if err != nil || run.State != "planned" || len(db.tasks) != 0 {
		t.Fatal(run, err)
	}
	again, err := engine.Trigger(ctx, op, event)
	if err != nil || again.ID != run.ID {
		t.Fatal(again, err)
	}
	changed := event
	changed.Values = map[string]string{"changed": "true"}
	if _, err = engine.Trigger(ctx, op, changed); err != ErrWorkflowConflict {
		t.Fatal("event ID reused with changed facts", err)
	}
	op.Action = "workflows.advance"
	db.fail = true
	if _, err = engine.Advance(ctx, op); err != ErrWorkflowUnavailable || len(db.tasks) != 0 {
		t.Fatal("failed transaction left a task", err, len(db.tasks))
	}
	db.fail = false
	view, err := engine.Advance(ctx, op)
	if err != nil || view.Steps[0].TaskID == "" || view.Steps[1].TaskID != "" || len(db.tasks) != 1 {
		t.Fatal(view, err)
	}
	if _, err = engine.Advance(ctx, op); err != nil || len(db.tasks) != 1 {
		t.Fatal("poll duplicated a task", err)
	}
	dispatch := op
	dispatch.Action = "workflows.dispatch"
	dispatch.ExpectedRevision = 1
	if err = engine.CheckDispatch(ctx, dispatch, WorkflowBinding{op.WorkflowID, view.ID, 1, 0}, view.Steps[0].TaskID); err != nil {
		t.Fatal(err)
	}
	db.tasks[view.Steps[0].TaskID] = "unknown"
	view, err = engine.Advance(ctx, op)
	if err != nil || view.State != "paused_unknown" {
		t.Fatal(view, err)
	}
	db.tasks[view.Steps[0].TaskID] = "succeeded"
	for range 2 {
		if view, err = engine.Advance(ctx, op); err != nil || view.State != "paused_unknown" || len(db.tasks) != 1 {
			t.Fatal("unknown was auto resumed", view, err)
		}
	}
	if err = engine.CheckDispatch(ctx, dispatch, WorkflowBinding{op.WorkflowID, view.ID, 1, 0}, view.Steps[0].TaskID); err != ErrWorkflowDenied {
		t.Fatal(err)
	}
}
func TestWorkflowSuccessDisableAndCurrentOwnerDenial(t *testing.T) {
	ctx := context.Background()
	engine, db, op, event := workflowFixture(t)
	_, err := engine.Trigger(ctx, op, event)
	if err != nil {
		t.Fatal(err)
	}
	op.Action = "workflows.advance"
	db.denyOwner = true
	if _, err = engine.Advance(ctx, op); err != ErrWorkflowDenied || len(db.tasks) != 0 {
		t.Fatal("revoked owner dispatched", err)
	}
	db.denyOwner = false
	view, err := engine.Advance(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	db.tasks[view.Steps[0].TaskID] = "succeeded"
	view, err = engine.Advance(ctx, op)
	if err != nil || len(db.tasks) != 2 || view.Steps[1].TaskID == "" {
		t.Fatal(view, err)
	}
	db.tasks[view.Steps[1].TaskID] = "awaiting_approval"
	view, err = engine.Advance(ctx, op)
	if err != nil || view.Steps[1].State != "awaiting_approval" {
		t.Fatal(view, err)
	}
	db.tasks[view.Steps[1].TaskID] = "succeeded"
	view, err = engine.Advance(ctx, op)
	if err != nil || view.State != "succeeded" {
		t.Fatal(view, err)
	}
	audits := len(db.audits)
	if _, err = engine.Advance(ctx, op); err != nil || len(db.audits) != audits {
		t.Fatal("terminal poll reaudited/reexecuted", err)
	}
	op.EventID = "event-2"
	op.Action = "workflows.trigger"
	event.ID = op.EventID
	if _, err = engine.Trigger(ctx, op, event); err != nil {
		t.Fatal(err)
	}
	op.Action = "workflows.advance"
	view, err = engine.Advance(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	save := WorkflowOperation{ActorID: "owner", Action: "workflows.save", WorkflowID: op.WorkflowID, ExpectedRevision: 1}
	def := Definition{ID: op.WorkflowID, Enabled: false, Trigger: "manual", Steps: []Step{{ActionID: "fixed.read", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`)}}}
	if _, err = engine.Save(ctx, save, def); err != nil {
		t.Fatal(err)
	}
	dispatch := op
	dispatch.Action = "workflows.dispatch"
	dispatch.ExpectedRevision = 1
	if err = engine.CheckDispatch(ctx, dispatch, WorkflowBinding{op.WorkflowID, view.ID, 1, 0}, view.Steps[0].TaskID); err != ErrWorkflowDenied {
		t.Fatal("disabled workflow dispatched", err)
	}
	view, err = engine.Advance(ctx, op)
	if err != nil || view.State != "canceled" {
		t.Fatal(view, err)
	}
}
func TestWorkflowCorruptPersistenceAndClockFailClosed(t *testing.T) {
	ctx := context.Background()
	engine, db, op, event := workflowFixture(t)
	if _, err := engine.Trigger(ctx, op, event); err != nil {
		t.Fatal(err)
	}
	var st workflowState
	_ = json.Unmarshal(db.payload, &st)
	r := st.Runs[workflowRunID(op.WorkflowID, op.EventID)]
	r.Plan.Tasks = nil
	st.Runs[r.View.ID] = r
	db.payload, _ = json.Marshal(st)
	op.Action = "workflows.advance"
	if _, err := engine.Advance(ctx, op); err != ErrWorkflowUnavailable {
		t.Fatal("corrupt plan accepted", err)
	}
	engine, _, op, event = workflowFixture(t)
	old := time.Now().Add(-time.Hour)
	engine.now = func() time.Time { return old }
	if _, err := engine.Trigger(ctx, op, event); err != ErrWorkflowUnavailable {
		t.Fatal("clock rollback", err)
	}
}
