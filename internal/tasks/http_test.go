package tasks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
)

func httpFixture(t *testing.T, class tasks.OperationClass, resolver tasks.GroupResolver) (*fixture, *httptest.Server) {
	t.Helper()
	f := newFixture(t, "n1", "n2")
	mux := http.NewServeMux()
	err := tasks.Register(mux, f.service,
		func(r *http.Request) (tasks.Principal, error) {
			subject := r.Header.Get("X-Test-Subject")
			if subject == "" {
				subject = "user-1"
			}
			return tasks.Principal{SubjectID: subject, RequestID: "req-http"}, nil
		}, resolver, func(string) (tasks.OperationClass, error) { return class, nil }, readGateFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return f, httptest.NewServer(mux)
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	return postJSONAs(t, url, body, "")
}

func postJSONAs(t *testing.T, url string, body any, subject string) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if subject != "" {
		request.Header.Set("X-Test-Subject", subject)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestHTTPCreateGetListAndDryRun(t *testing.T) {
	t.Parallel()
	f, server := httpFixture(t, tasks.OperationClassStandard, nil)
	defer server.Close()
	response := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "patch fleet", "command": "host.patch",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1", "n2"}},
		"dryRun": false, "dispatchMode": "batch",
	})
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%s", response.StatusCode, body)
	}
	var job struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if job.JobID == "" || (job.State != "queued" && job.State != "awaiting_approval") {
		t.Fatalf("job = %+v", job)
	}
	get, err := http.Get(server.URL + "/api/v1/tasks/" + job.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", get.StatusCode)
	}
	get.Body.Close()
	list, err := http.Get(server.URL + "/api/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			TaskID     string `json:"taskId"`
			PlanDigest string `json:"planDigest"`
		} `json:"items"`
	}
	if err := json.NewDecoder(list.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	list.Body.Close()
	if len(page.Items) != 1 || page.Items[0].TaskID != job.JobID || page.Items[0].PlanDigest == "" {
		t.Fatalf("page = %+v", page)
	}

	dry := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "dry", "command": "host.patch",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
		"dryRun": true,
	})
	if dry.StatusCode != http.StatusOK {
		t.Fatalf("dry status = %d", dry.StatusCode)
	}
	var preview struct {
		Snapshot []string `json:"targetNodeIdsSnapshot"`
		Eligible []string `json:"eligibleNodeIds"`
		Denied   []string `json:"deniedNodeIds"`
	}
	if err := json.NewDecoder(dry.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	dry.Body.Close()
	if len(preview.Snapshot) != 1 || len(preview.Eligible) != 1 || len(preview.Denied) != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if count, _ := f.repo.List(context.Background(), 10, 0); len(count) != 1 {
		t.Fatalf("dry run persisted a task: %d", len(count))
	}
}

func TestHTTPApprovalRequiresPlanDigest(t *testing.T) {
	t.Parallel()
	f, server := httpFixture(t, tasks.OperationClassRoot, nil)
	defer server.Close()
	response := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "root task", "command": "host.root",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
		"dryRun": false,
	})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var job struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if job.State != "awaiting_approval" {
		t.Fatalf("state = %s", job.State)
	}
	list, _ := http.Get(server.URL + "/api/v1/tasks")
	var page struct {
		Items []struct {
			PlanDigest string `json:"planDigest"`
		} `json:"items"`
	}
	json.NewDecoder(list.Body).Decode(&page)
	list.Body.Close()
	if len(page.Items) != 1 {
		t.Fatal("task missing from list")
	}
	wrong := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/approve", map[string]any{
		"planDigest": "deadbeef", "decision": "approve",
	})
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong digest status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()
	self := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/approve", map[string]any{
		"planDigest": page.Items[0].PlanDigest, "decision": "approve",
	})
	if self.StatusCode != http.StatusConflict {
		t.Fatalf("self approval status = %d", self.StatusCode)
	}
	self.Body.Close()
	approve := postJSONAs(t, server.URL+"/api/v1/tasks/"+job.JobID+"/approve", map[string]any{
		"planDigest": page.Items[0].PlanDigest, "decision": "approve", "reason": "reviewed",
	}, "admin-2")
	if approve.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(approve.Body)
		t.Fatalf("approve status = %d body=%s", approve.StatusCode, body)
	}
	approve.Body.Close()
	cancel := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/cancel", map[string]any{
		"reason": "stop", "expectedRevision": 1,
	})
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d", cancel.StatusCode)
	}
	cancel.Body.Close()
	task, err := f.service.Get(context.Background(), job.JobID)
	if err != nil || task.Status != tasks.StatusCanceled {
		t.Fatalf("task = %+v, %v", task, err)
	}
}

func TestHTTPUnauthorizedAndDenied(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	mux := http.NewServeMux()
	if err := tasks.Register(mux, f.service,
		func(*http.Request) (tasks.Principal, error) { return tasks.Principal{}, context.Canceled },
		nil, nil, readGateFunc(func(context.Context, string, string) error { return nil })); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	response := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}
	response.Body.Close()

	denied, deniedServer := httpFixture(t, tasks.OperationClassStandard, nil)
	defer deniedServer.Close()
	denied.authorizer.setDeny(true)
	response = postJSON(t, deniedServer.URL+"/api/v1/tasks", map[string]any{
		"name": "denied", "command": "host.patch",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied status = %d", response.StatusCode)
	}
	response.Body.Close()
	page, err := denied.audit.Query(context.Background(), audit.Query{Action: "host.patch", Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestHTTPRejectsUnknownFieldsAndUnresolvedGroups(t *testing.T) {
	t.Parallel()
	_, server := httpFixture(t, tasks.OperationClassStandard, nil)
	defer server.Close()
	response := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "x", "command": "host.patch", "unexpected": true,
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", response.StatusCode)
	}
	response.Body.Close()
	group := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "x", "command": "host.patch",
		"target": map[string]any{"type": "service_group", "groupId": "sg1"},
	})
	if group.StatusCode != http.StatusBadRequest {
		t.Fatalf("group status = %d", group.StatusCode)
	}
	group.Body.Close()
}

func TestHTTPGroupResolverAndSSE(t *testing.T) {
	t.Parallel()
	resolver := groupResolverFunc(func(context.Context, string, string) ([]string, error) {
		return []string{"n1", "n2"}, nil
	})
	f, server := httpFixture(t, tasks.OperationClassStandard, resolver)
	defer server.Close()
	response := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "group task", "command": "host.patch",
		"target": map[string]any{"type": "service_group", "groupId": "sg1"},
	})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var job struct {
		JobID string `json:"jobId"`
	}
	json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if job.JobID == "" {
		t.Fatal("missing job id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/api/v1/tasks/"+job.JobID+"/events", nil)
	stream, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if !strings.Contains(string(body), "event: created") {
		t.Fatalf("SSE body = %q", string(body))
	}
	task, err := f.service.Get(context.Background(), job.JobID)
	if err != nil || len(task.Targets) != 2 {
		t.Fatalf("task = %+v, %v", task, err)
	}
}

type readGateFunc func(context.Context, string, string) error

func (f readGateFunc) AuthorizeTaskRead(ctx context.Context, subjectID, taskID string) error {
	return f(ctx, subjectID, taskID)
}

type groupResolverFunc func(context.Context, string, string) ([]string, error)

func (f groupResolverFunc) ResolveTargets(ctx context.Context, targetType, groupID string) ([]string, error) {
	return f(ctx, targetType, groupID)
}
