package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type reviewNodeLookup struct{}

func (reviewNodeLookup) Online(context.Context, string) (bool, error) { return true, nil }

type reviewGatewayAuth bool

func (a reviewGatewayAuth) Authorize(context.Context, auth.Check) error {
	if a {
		return nil
	}
	return auth.ErrDenied
}

type reviewUpload struct {
	taskID, node, path, digest string
	content                    []byte
}

type reviewGatewayUploads struct {
	mu       sync.Mutex
	uploads  []reviewUpload
	err      error
	onUpload func()
}

func (u *reviewGatewayUploads) Upload(_ context.Context, taskID, nodeID, absolutePath string, content []byte, sha256Hex string) error {
	u.mu.Lock()
	u.uploads = append(u.uploads, reviewUpload{taskID, nodeID, absolutePath, sha256Hex, append([]byte(nil), content...)})
	err, hook := u.err, u.onUpload
	u.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func (u *reviewGatewayUploads) snapshot() []reviewUpload {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]reviewUpload(nil), u.uploads...)
}

func (u *reviewGatewayUploads) has(node, path, digest string) bool {
	for _, upload := range u.snapshot() {
		if upload.node == node && upload.path == path && upload.digest == digest {
			return true
		}
	}
	return false
}

type reviewGatewayAuthority struct {
	mu       sync.Mutex
	calls    int
	failFrom int
}

func (a *reviewGatewayAuthority) Current(context.Context, string, uint64, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.failFrom > 0 && a.calls >= a.failFrom {
		return publishing.ErrProductionTarget
	}
	return nil
}

func (a *reviewGatewayAuthority) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

type reviewGatewayAgent struct {
	mu         sync.Mutex
	tasks      *tasks.Service
	uploader   *reviewGatewayUploads
	result     tasks.NodeResultCode
	calls      int
	sawUpload  bool
	artifactOK bool
}

func (a *reviewGatewayAgent) Dispatch(ctx context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	params, err := gatewayCommandParams(command.Params)
	if err != nil {
		return tasks.DispatchResult{Accepted: false, Reason: "bad_params"}, nil
	}
	a.mu.Lock()
	a.calls++
	service, result := a.tasks, a.result
	if command.ActionID == gateway.NginxActionApply {
		a.artifactOK = a.uploader.has(command.NodeID, params["artifact_path"], params["config_sha256"])
		a.sawUpload = a.artifactOK
	}
	artifactOK := a.artifactOK
	a.mu.Unlock()
	if command.ActionID == gateway.NginxActionApply && !artifactOK {
		return tasks.DispatchResult{Accepted: false, Reason: "artifact_missing"}, nil
	}
	go func() {
		waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			task, err := service.Get(waitCtx, command.TaskID)
			if err != nil {
				return
			}
			for _, target := range task.Targets {
				if target.NodeID != command.NodeID || target.State != tasks.TargetDispatched {
					continue
				}
				_, _ = service.RecordResult(waitCtx, command.TaskID, command.NodeID, result, "fake agent result")
				return
			}
			select {
			case <-waitCtx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	return tasks.DispatchResult{Accepted: true}, nil
}

func (a *reviewGatewayAgent) Cancel(context.Context, tasks.CancelCommand) error { return nil }

func (a *reviewGatewayAgent) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

type gatewayHarnessOptions struct {
	SubmitAllow   bool
	DispatchAllow bool
	Approve       bool
	AgentResult   tasks.NodeResultCode
	UploadError   error
	ApprovalWait  time.Duration
	ExecuteWait   time.Duration
	AuthorityFrom int
	OnUpload      func()
}

type gatewayHarness struct {
	service   *tasks.Service
	repo      *tasks.Repo
	db        *storage.DB
	host      *gatewayTaskHost
	uploader  *reviewGatewayUploads
	agent     *reviewGatewayAgent
	authority *reviewGatewayAuthority
	approveMu sync.Mutex
	approve   error
	scheduler context.CancelFunc
}

func newGatewayHarness(t *testing.T, options gatewayHarnessOptions) *gatewayHarness {
	t.Helper()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations(), publishing.Migrations(), publishing.TargetMigrations())
	repo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := audit.NewValidator(AuditActions(), audit.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	registry := newGatewayArtifactRegistry()
	uploader := &reviewGatewayUploads{err: options.UploadError, onUpload: options.OnUpload}
	authority := &reviewGatewayAuthority{failFrom: options.AuthorityFrom}
	agent := &reviewGatewayAgent{uploader: uploader, result: options.AgentResult}
	if agent.result == "" {
		agent.result = tasks.NodeResultSucceeded
	}
	dispatcher, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
		Repo: repo, DB: db, Artifacts: uploader, Next: agent,
		Authorizer: reviewGatewayAuth(options.SubmitAllow), Registry: registry,
		planAuthority: authority,
		Authority: func(context.Context, tasks.Task, string) error {
			if options.DispatchAllow {
				return nil
			}
			return auth.ErrDenied
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := tasks.NewService(tasks.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: reviewGatewayAuth(options.SubmitAllow),
		Dispatcher: dispatcher, Interval: 10 * time.Millisecond, Nodes: reviewNodeLookup{},
	})
	if err != nil {
		t.Fatal(err)
	}
	agent.tasks = service
	harness := &gatewayHarness{
		service: service, repo: repo, db: db, uploader: uploader, agent: agent, authority: authority,
	}
	approvalWait := options.ApprovalWait
	if approvalWait <= 0 {
		approvalWait = 100 * time.Millisecond
	}
	executeWait := options.ExecuteWait
	if executeWait <= 0 {
		executeWait = 3 * time.Second
	}
	host, err := newGatewayTaskHost(service, gatewayHostConfig{
		Registry: registry, Authority: authority, Poll: 5 * time.Millisecond,
		ApprovalWait: approvalWait, ExecuteWait: executeWait,
		OnSubmitted: func(taskID string) {
			if !options.Approve {
				return
			}
			_, err := service.Decide(context.Background(), taskID, 1, tasks.Decision{
				ApproverID: "reviewer", RequestID: "approve-" + taskID, Approve: true,
			})
			harness.approveMu.Lock()
			if err != nil && !errors.Is(err, tasks.ErrInvalidStatus) {
				harness.approve = err
			}
			harness.approveMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.host = host
	runCtx, cancel := context.WithCancel(context.Background())
	harness.scheduler = cancel
	t.Cleanup(cancel)
	go func() { _ = service.Run(runCtx) }()
	return harness
}

func reviewGatewayPlan() publishing.Plan {
	return publishing.Plan{
		ID: "plan-one", Domain: "app.example.com", ServiceGroupID: "group-one",
		ExpectedGroupRevision: 1, GatewayIDs: []string{"gw-one"},
		TargetRevision: 2, TargetsPersisted: true, CreatedBy: "owner",
	}
}

func reviewGatewaySite(t *testing.T) gateway.NginxSite {
	t.Helper()
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: "gw-one", Domain: "app.example.com", ServiceGroupID: "group-one",
		ServiceGroupRevision: 1, UpstreamHosts: []string{"127.0.0.1:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func reviewGatewayInput() gateway.NginxTaskInput {
	return gateway.NginxTaskInput{
		SubjectID: "owner", RequestID: "req-gateway", GatewayID: "gw-one", NodeID: "node-one",
	}
}

func TestGatewayTaskPublishesOnlyAfterApprovalAndTerminal(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{SubmitAllow: true, DispatchAllow: true, Approve: true})
	site := reviewGatewaySite(t)
	taskID, err := h.host.apply(context.Background(), reviewGatewayPlan(), site, reviewGatewayInput())
	if err != nil {
		list, _ := h.repo.List(context.Background(), 10, 0)
		for _, task := range list {
			logs, _ := h.service.Logs(context.Background(), task.ID, 0, 20)
			events, _ := h.service.Events(context.Background(), task.ID, 0, 20)
			t.Logf("task=%s status=%s targets=%+v logs=%+v events=%+v", task.ID, task.Status, task.Targets, logs, events)
		}
		t.Logf("approval err = %v", h.approve)
		t.Fatal(err)
	}
	if taskID == "" {
		t.Fatal("missing task id")
	}
	uploads := h.uploader.snapshot()
	if len(uploads) != 1 || uploads[0].taskID != taskID || uploads[0].node != "node-one" || uploads[0].path != site.ArtifactPath ||
		uploads[0].digest != site.Digest || string(uploads[0].content) != string(site.Config) {
		t.Fatalf("uploads = %+v", uploads)
	}
	if h.agent.count() != 1 || !h.agent.sawUpload {
		t.Fatalf("agent calls = %d, saw upload = %v", h.agent.count(), h.agent.sawUpload)
	}
	if h.authority.count() < 3 {
		t.Fatalf("authority rechecks = %d", h.authority.count())
	}
	task, err := h.service.Get(context.Background(), taskID)
	if err != nil || task.Status != tasks.StatusCompleted {
		t.Fatalf("task = %+v, %v", task, err)
	}
	for _, target := range task.Targets {
		if target.State != tasks.TargetSucceeded {
			t.Fatalf("target = %+v", target)
		}
	}
	h.approveMu.Lock()
	defer h.approveMu.Unlock()
	if h.approve != nil {
		t.Fatal(h.approve)
	}
}

func TestGatewayTaskAwaitingApprovalNeverUploads(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: false,
		ApprovalWait: 80 * time.Millisecond,
	})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if !errors.Is(err, ErrGatewayAwaitingApproval) {
		t.Fatalf("err = %v", err)
	}
	if len(h.uploader.snapshot()) != 0 || h.agent.count() != 0 {
		t.Fatalf("pending approval uploaded=%d dispatched=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskDeniedSubmitStopsBeforeTask(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{SubmitAllow: false, DispatchAllow: true, Approve: true})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if len(h.uploader.snapshot()) != 0 || h.agent.count() != 0 {
		t.Fatalf("denied submit uploaded=%d dispatched=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskRevokedAtDispatchStopsUpload(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{SubmitAllow: true, DispatchAllow: false, Approve: true})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if err == nil {
		t.Fatal("revoked dispatch reported success")
	}
	if len(h.uploader.snapshot()) != 0 || h.agent.count() != 0 {
		t.Fatalf("revoked dispatch uploaded=%d dispatched=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskUploadFailureNeverReachesAgent(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: true,
		UploadError: errors.New("private staging unavailable"),
	})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if err == nil {
		t.Fatal("upload failure reported success")
	}
	if len(h.uploader.snapshot()) != 1 || h.agent.count() != 0 {
		t.Fatalf("uploads=%d agent=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskUnknownResultIsNotSuccess(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: true, AgentResult: tasks.NodeResultUnknown,
	})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if !errors.Is(err, ErrGatewayTaskUncertain) {
		t.Fatalf("err = %v", err)
	}
	if len(h.uploader.snapshot()) != 1 || h.agent.count() != 1 {
		t.Fatalf("uploads=%d agent=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskTargetRevisionChangedBeforeUpload(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: true, AuthorityFrom: 2,
	})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if err == nil {
		t.Fatal("stale target revision reported success")
	}
	if len(h.uploader.snapshot()) != 0 || h.agent.count() != 0 {
		t.Fatalf("uploads=%d agent=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskTargetRevisionChangedAfterUpload(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{
		SubmitAllow: true, DispatchAllow: true, Approve: true, AuthorityFrom: 3,
	})
	_, err := h.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if err == nil {
		t.Fatal("stale target revision after upload reported success")
	}
	if len(h.uploader.snapshot()) != 1 || h.agent.count() != 0 {
		t.Fatalf("uploads=%d agent=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}

func TestGatewayTaskCancelDuringUploadStopsAgent(t *testing.T) {
	var harness *gatewayHarness
	options := gatewayHarnessOptions{SubmitAllow: true, DispatchAllow: true, Approve: true}
	options.OnUpload = func() {
		if harness == nil {
			return
		}
		ids, err := harness.repo.ListActive(context.Background(), 10)
		if err != nil || len(ids) == 0 {
			return
		}
		_ = harness.repo.SetCancelRequested(context.Background(), harness.db, ids[0])
	}
	harness = newGatewayHarness(t, options)
	_, err := harness.host.apply(context.Background(), reviewGatewayPlan(), reviewGatewaySite(t), reviewGatewayInput())
	if err == nil {
		t.Fatal("canceled task reported success")
	}
	if len(harness.uploader.snapshot()) != 1 || harness.agent.count() != 0 {
		t.Fatalf("uploads=%d agent=%d", len(harness.uploader.snapshot()), harness.agent.count())
	}
}

func TestGatewayTaskSamePlanIsIdempotent(t *testing.T) {
	h := newGatewayHarness(t, gatewayHarnessOptions{SubmitAllow: true, DispatchAllow: true, Approve: true})
	plan := reviewGatewayPlan()
	site := reviewGatewaySite(t)
	first, err := h.host.apply(context.Background(), plan, site, reviewGatewayInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.host.apply(context.Background(), plan, site, reviewGatewayInput())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("task ids differ: %s/%s", first, second)
	}
	if len(h.uploader.snapshot()) != 1 || h.agent.count() != 1 {
		t.Fatalf("uploads=%d agent=%d", len(h.uploader.snapshot()), h.agent.count())
	}
}
