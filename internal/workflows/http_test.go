package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func httpEngineFixture(t *testing.T) (*Engine, *workflowTestDB) {
	t.Helper()
	db := &workflowTestDB{tasks: map[string]string{}, keys: map[string]string{}}
	engine, err := NewEngine(workflowTestBackend(db), []ActionDefinition{{
		ID: "fixed.read",
		Canonicalize: func(raw json.RawMessage) (json.RawMessage, error) {
			if string(raw) != `{}` {
				return nil, ErrWorkflowDenied
			}
			return bytes.Clone(raw), nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return engine, db
}

type httpFixture struct {
	handler    http.Handler
	authorized *[]string
	denyWrite  bool
	denyNodes  bool
}

func newHTTPFixture(t *testing.T, engine *Engine) *httpFixture {
	t.Helper()
	fixture := &httpFixture{authorized: &[]string{}}
	mux := http.NewServeMux()
	err := RegisterHTTP(mux, engine, HTTPConfig{
		Authenticate: func(r *http.Request) (Principal, error) {
			if r.Header.Get("X-Test-Subject") == "" {
				return Principal{}, auth.ErrDenied
			}
			return Principal{SubjectID: r.Header.Get("X-Test-Subject"), RequestID: "req-workflow-test"}, nil
		},
		AuthorizeAction: func(_ context.Context, _ string, action string) error {
			*fixture.authorized = append(*fixture.authorized, "action:"+action)
			if fixture.denyWrite && action == ActionWrite {
				return auth.ErrDenied
			}
			return nil
		},
		AuthorizeNode: func(_ context.Context, _ string, action string, _ []string) error {
			*fixture.authorized = append(*fixture.authorized, "node:"+action)
			if fixture.denyNodes {
				return auth.ErrDenied
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler = mux
	return fixture
}

func (f *httpFixture) do(t *testing.T, method, path, subject string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if subject != "" {
		request.Header.Set("X-Test-Subject", subject)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	response := map[string]any{}
	if recorder.Body.Len() > 0 {
		_ = json.Unmarshal(recorder.Body.Bytes(), &response)
	}
	return recorder.Code, response
}

func workflowBody() map[string]any {
	return map[string]any{
		"trigger": "manual",
		"match":   map[string]string{},
		"steps": []map[string]any{{
			"actionId":   "fixed.read",
			"nodeIds":    []string{"node-a"},
			"parameters": map[string]string{},
		}},
	}
}

func TestWorkflowHTTPCreateReadPatchAndOwnerRules(t *testing.T) {
	engine, _ := httpEngineFixture(t)
	fixture := newHTTPFixture(t, engine)

	if status, _ := fixture.do(t, "GET", "/api/v1/workflows", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", status)
	}
	status, created := fixture.do(t, "POST", "/api/v1/workflows", "owner", workflowBody())
	if status != http.StatusCreated {
		t.Fatalf("create = %d: %+v", status, created)
	}
	workflow := created
	if nested, ok := created["workflow"].(map[string]any); ok {
		workflow = nested
	}
	id, _ := workflow["id"].(string)
	if id == "" || workflow["revision"].(float64) != 1 || workflow["enabled"] != true {
		t.Fatalf("unexpected created workflow: %+v", created)
	}

	status, listed := fixture.do(t, "GET", "/api/v1/workflows", "owner", nil)
	if status != http.StatusOK {
		t.Fatalf("list = %d: %+v", status, listed)
	}
	items := listed["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != id {
		t.Fatalf("unexpected list: %+v", listed)
	}

	status, detail := fixture.do(t, "GET", "/api/v1/workflows/"+id, "owner", nil)
	if status != http.StatusOK || len(detail["runs"].([]any)) != 0 {
		t.Fatalf("detail = %d: %+v", status, detail)
	}

	status, patched := fixture.do(t, "PATCH", "/api/v1/workflows/"+id, "owner", map[string]any{
		"expectedRevision": 1, "enabled": false,
	})
	if status != http.StatusOK || patched["revision"].(float64) != 2 || patched["enabled"] != false {
		t.Fatalf("patch = %d: %+v", status, patched)
	}
	status, _ = fixture.do(t, "PATCH", "/api/v1/workflows/"+id, "owner", map[string]any{
		"expectedRevision": 1, "enabled": true,
	})
	if status != http.StatusConflict {
		t.Fatalf("stale patch = %d, want 409", status)
	}

	fixture.denyWrite = true
	if status, _ := fixture.do(t, "POST", "/api/v1/workflows", "owner", workflowBody()); status != http.StatusForbidden {
		t.Fatalf("read-only create = %d, want 403", status)
	}
	if status, _ := fixture.do(t, "PATCH", "/api/v1/workflows/"+id, "owner", map[string]any{"expectedRevision": 2}); status != http.StatusForbidden {
		t.Fatalf("read-only patch = %d, want 403", status)
	}
	fixture.denyWrite = false

	fixture.denyNodes = true
	if status, _ := fixture.do(t, "POST", "/api/v1/workflows", "owner", workflowBody()); status != http.StatusForbidden {
		t.Fatalf("ungranted node create = %d, want 403", status)
	}
	fixture.denyNodes = false

	status, _ = fixture.do(t, "PATCH", "/api/v1/workflows/"+id, "owner", map[string]any{
		"expectedRevision": 2, "enabled": true,
	})
	if status != http.StatusOK {
		t.Fatalf("re-enable = %d", status)
	}
}

func TestWorkflowHTTPTriggerEntersTaskServiceAtMostOnce(t *testing.T) {
	engine, db := httpEngineFixture(t)
	fixture := newHTTPFixture(t, engine)
	status, created := fixture.do(t, "POST", "/api/v1/workflows", "owner", workflowBody())
	if status != http.StatusCreated {
		t.Fatalf("create = %d: %+v", status, created)
	}
	workflow := created
	if nested, ok := created["workflow"].(map[string]any); ok {
		workflow = nested
	}
	id := workflow["id"].(string)

	status, _ = fixture.do(t, "POST", "/api/v1/workflows/"+id+"/runs", "owner", map[string]any{
		"eventId": "event-1", "kind": "schedule", "values": map[string]string{},
	})
	if status != http.StatusAccepted {
		t.Fatalf("unmatched trigger = %d", status)
	}
	if len(db.tasks) != 0 {
		t.Fatalf("unmatched trigger created tasks: %v", db.tasks)
	}

	status, triggered := fixture.do(t, "POST", "/api/v1/workflows/"+id+"/runs", "owner", map[string]any{
		"eventId": "event-1", "kind": "manual", "values": map[string]string{},
	})
	if status != http.StatusAccepted || triggered["matched"] != true {
		t.Fatalf("trigger = %d: %+v", status, triggered)
	}
	run := triggered["run"].(map[string]any)
	steps := run["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["taskId"] == "" || len(db.tasks) != 1 {
		t.Fatalf("trigger did not submit exactly one task: %+v tasks=%v", run, db.tasks)
	}
	taskID := steps[0].(map[string]any)["taskId"].(string)

	status, replayed := fixture.do(t, "POST", "/api/v1/workflows/"+id+"/runs", "owner", map[string]any{
		"eventId": "event-1", "kind": "manual", "values": map[string]string{},
	})
	if status != http.StatusAccepted || replayed["run"].(map[string]any)["runId"] != run["runId"] || len(db.tasks) != 1 {
		t.Fatalf("replayed trigger duplicated work: %+v tasks=%v", replayed, db.tasks)
	}
	if db.tasks[taskID] != "queued" {
		t.Fatalf("task state = %q", db.tasks[taskID])
	}

	status, _ = fixture.do(t, "PATCH", "/api/v1/workflows/"+id, "owner", map[string]any{
		"expectedRevision": 1, "enabled": false,
	})
	if status != http.StatusOK {
		t.Fatalf("disable = %d", status)
	}
	status, _ = fixture.do(t, "POST", "/api/v1/workflows/"+id+"/runs", "owner", map[string]any{
		"eventId": "event-2", "kind": "manual", "values": map[string]string{},
	})
	if status != http.StatusConflict {
		t.Fatalf("disabled trigger = %d, want 409", status)
	}

	status, detail := fixture.do(t, "GET", "/api/v1/workflows/"+id, "owner", nil)
	if status != http.StatusOK || len(detail["runs"].([]any)) != 1 {
		t.Fatalf("run history = %d: %+v", status, detail)
	}
}

func TestWorkflowHTTPRejectsMalformedAndForeignRequests(t *testing.T) {
	engine, _ := httpEngineFixture(t)
	fixture := newHTTPFixture(t, engine)

	bad := workflowBody()
	bad["steps"] = []map[string]any{{"actionId": "fixed.read", "nodeIds": []string{"node-a"}, "parameters": map[string]any{"nested": map[string]string{"x": "y"}}}}
	if status, _ := fixture.do(t, "POST", "/api/v1/workflows", "owner", bad); status != http.StatusBadRequest {
		t.Fatalf("non-string parameters = %d, want 400", status)
	}
	unknownAction := workflowBody()
	unknownAction["steps"] = []map[string]any{{"actionId": "unknown.action", "nodeIds": []string{"node-a"}, "parameters": map[string]string{}}}
	if status, _ := fixture.do(t, "POST", "/api/v1/workflows", "owner", unknownAction); status != http.StatusBadRequest {
		t.Fatalf("unknown action = %d, want 400", status)
	}
	if status, _ := fixture.do(t, "GET", "/api/v1/workflows/missing", "owner", nil); status != http.StatusNotFound {
		t.Fatalf("missing workflow = %d, want 404", status)
	}
	if status, _ := fixture.do(t, "POST", "/api/v1/workflows/missing/runs", "owner", map[string]any{"eventId": "e", "kind": "manual"}); status != http.StatusNotFound {
		t.Fatalf("missing trigger = %d, want 404", status)
	}
	raw := httptest.NewRequest("POST", "/api/v1/workflows", strings.NewReader(`{"trigger":"manual","steps":[]}`))
	raw.Header.Set("Content-Type", "application/json")
	raw.Header.Set("X-Test-Subject", "owner")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, raw)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty steps = %d, want 400", recorder.Code)
	}
}

func TestWorkflowReadOwnerIsolation(t *testing.T) {
	db := &workflowTestDB{tasks: map[string]string{}, keys: map[string]string{}}
	backend := workflowTestBackend(db)
	backend.View = func(ctx context.Context, fn func(WorkflowReadTx) error) error {
		return fn(WorkflowReadTx{
			Read: func(context.Context) ([]byte, error) { return bytes.Clone(db.payload), nil },
			Admit: func(context.Context, WorkflowOperation) error {
				return nil
			},
		})
	}
	engine, err := NewEngine(backend, []ActionDefinition{{ID: "fixed.read", Canonicalize: func(raw json.RawMessage) (json.RawMessage, error) {
		return bytes.Clone(raw), nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	op := WorkflowOperation{ActorID: "owner", Action: "workflows.save", WorkflowID: "wf-owner"}
	if _, err := engine.Save(context.Background(), op, Definition{ID: op.WorkflowID, Enabled: true, Trigger: "manual", Steps: []Step{{ActionID: "fixed.read", NodeIDs: []string{"node-a"}, Parameters: []byte(`{}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Get(context.Background(), WorkflowOperation{ActorID: "other", Action: "workflows.read"}, "wf-owner"); !errors.Is(err, ErrWorkflowDenied) {
		t.Fatalf("foreign read = %v", err)
	}
	definitions, err := engine.List(context.Background(), WorkflowOperation{ActorID: "other", Action: "workflows.read"})
	if err != nil || len(definitions) != 0 {
		t.Fatalf("foreign list = %v %+v", err, definitions)
	}
}
