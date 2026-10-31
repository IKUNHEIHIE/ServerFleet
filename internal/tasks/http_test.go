package tasks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
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

func TestHTTPTaskExecutionsAndResume(t *testing.T) {
	t.Parallel()
	_, server := httpFixture(t, tasks.OperationClassStandard, nil)
	defer server.Close()

	create := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "offline wait", "command": "host.patch",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1", "n2"}},
		"dryRun": false,
	})
	if create.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(create.Body)
		t.Fatalf("create status = %d body=%s", create.StatusCode, body)
	}
	var job struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(create.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	create.Body.Close()
	if job.JobID == "" || job.State != "queued" {
		t.Fatalf("job = %+v", job)
	}
	detail, err := http.Get(server.URL + "/api/v1/tasks/" + job.JobID)
	if err != nil || detail.StatusCode != http.StatusOK {
		t.Fatalf("detail = %v %d", err, detail.StatusCode)
	}
	var detailBody struct {
		Revision uint64 `json:"revision"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&detailBody); err != nil {
		t.Fatal(err)
	}
	detail.Body.Close()
	if detailBody.Revision != 1 {
		t.Fatalf("detail revision = %d", detailBody.Revision)
	}

	executions, err := http.Get(server.URL + "/api/v1/tasks/" + job.JobID + "/executions")
	if err != nil || executions.StatusCode != http.StatusOK {
		t.Fatalf("executions = %v %d", err, executions.StatusCode)
	}
	var page struct {
		Items []struct {
			NodeID string `json:"nodeId"`
			State  string `json:"state"`
		} `json:"items"`
	}
	if err := json.NewDecoder(executions.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	executions.Body.Close()
	if len(page.Items) != 2 || page.Items[0].State != "queued" || page.Items[0].NodeID == "" {
		t.Fatalf("executions = %+v", page)
	}

	resume := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/resume", map[string]any{
		"reason": "extend offline wait", "expectedRevision": 1,
	})
	if resume.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resume.Body)
		t.Fatalf("resume status = %d body=%s", resume.StatusCode, body)
	}
	var resumed struct {
		State    string `json:"state"`
		Revision uint64 `json:"revision"`
	}
	if err := json.NewDecoder(resume.Body).Decode(&resumed); err != nil {
		t.Fatal(err)
	}
	resume.Body.Close()
	if resumed.State != "queued" || resumed.Revision != 2 {
		t.Fatalf("resumed = %+v", resumed)
	}

	stale := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/resume", map[string]any{
		"reason": "again", "expectedRevision": 1,
	})
	if stale.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(stale.Body)
		t.Fatalf("stale resume status = %d body=%s", stale.StatusCode, body)
	}
	stale.Body.Close()

	cancel := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/cancel", map[string]any{
		"reason": "operator stop", "expectedRevision": 2,
	})
	if cancel.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancel.Body)
		t.Fatalf("cancel status = %d body=%s", cancel.StatusCode, body)
	}
	cancel.Body.Close()
	terminal := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/resume", map[string]any{
		"reason": "too late",
	})
	if terminal.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(terminal.Body)
		t.Fatalf("terminal resume status = %d body=%s", terminal.StatusCode, body)
	}
	terminal.Body.Close()
}

func TestHTTPTaskResumeRejectsApprovalAndDenied(t *testing.T) {
	t.Parallel()
	_, server := httpFixture(t, tasks.OperationClassRoot, nil)
	defer server.Close()
	create := postJSON(t, server.URL+"/api/v1/tasks", map[string]any{
		"name": "root patch", "command": "host.root",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
		"dryRun": false,
	})
	var job struct {
		JobID string `json:"jobId"`
		State string `json:"state"`
	}
	if create.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(create.Body)
		t.Fatalf("create status = %d body=%s", create.StatusCode, body)
	}
	if err := json.NewDecoder(create.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	create.Body.Close()
	if job.State != "awaiting_approval" {
		t.Fatalf("job = %+v", job)
	}
	pending := postJSON(t, server.URL+"/api/v1/tasks/"+job.JobID+"/resume", map[string]any{
		"reason": "resume", "expectedRevision": 1,
	})
	if pending.StatusCode != http.StatusConflict {
		t.Fatalf("awaiting approval resume status = %d", pending.StatusCode)
	}
	pending.Body.Close()

	deniedFixture, deniedServer := httpFixture(t, tasks.OperationClassStandard, nil)
	defer deniedServer.Close()
	standalone := postJSON(t, deniedServer.URL+"/api/v1/tasks", map[string]any{
		"name": "standard", "command": "host.patch",
		"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
		"dryRun": false,
	})
	var deniedJob struct {
		JobID string `json:"jobId"`
	}
	if err := json.NewDecoder(standalone.Body).Decode(&deniedJob); err != nil {
		t.Fatal(err)
	}
	standalone.Body.Close()
	deniedFixture.authorizer.setDeny(true)
	denied := postJSON(t, deniedServer.URL+"/api/v1/tasks/"+deniedJob.JobID+"/resume", map[string]any{
		"reason": "resume", "expectedRevision": 1,
	})
	if denied.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(denied.Body)
		t.Fatalf("denied resume status = %d body=%s", denied.StatusCode, body)
	}
	denied.Body.Close()
	foundResume := false
	for _, action := range tasks.ServiceActions() {
		if action == tasks.ActionResume {
			foundResume = true
		}
	}
	if !foundResume {
		t.Fatalf("tasks.ServiceActions missing %q", tasks.ActionResume)
	}
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

func TestSSEStopsWhenReadAuthorizationIsRevoked(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "n1")
	var calls int32
	gate := readGateFunc(func(context.Context, string, string) error {
		if atomic.AddInt32(&calls, 1) <= 1 {
			return nil
		}
		return auth.ErrDenied
	})
	mux := http.NewServeMux()
	if err := tasks.Register(mux, f.service,
		func(*http.Request) (tasks.Principal, error) {
			return tasks.Principal{SubjectID: "user-1", RequestID: "req-http"}, nil
		}, nil, nil, gate); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	task := f.submit(t, tasks.SubmitRequest{TargetNodeIDs: []string{"n1"}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/tasks/"+task.ID+"/events", nil)
	stream, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if !strings.Contains(string(body), "authorization_revoked") {
		t.Fatalf("SSE body = %q", string(body))
	}
}
