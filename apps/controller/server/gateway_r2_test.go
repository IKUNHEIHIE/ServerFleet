package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

type gatewayR2Fixture struct {
	db         *storage.DB
	repo       *tasks.Repo
	service    *tasks.Service
	dispatcher *gatewayTaskDispatcher
	uploads    *reviewerBoundCapture
	next       *reviewerNextCapture
	task       tasks.Task
	content    []byte
	digest     string
	params     map[string]string
}

func newGatewayR2Fixture(t *testing.T) *gatewayR2Fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations())
	if err := EnsureGatewayArtifactBindings(ctx, db); err != nil {
		t.Fatal(err)
	}
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
	uploads := &reviewerBoundCapture{}
	next := &reviewerNextCapture{}
	dispatcher, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
		Repo: repo, DB: db, Artifacts: uploads, Next: next, Authorizer: reviewGatewayAuth(true),
		Registry: registry, planAuthority: &reviewGatewayAuthority{},
		Authority: func(context.Context, tasks.Task, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: recorder, Authorizer: reviewGatewayAuth(true), Dispatcher: dispatcher, Nodes: reviewNodeLookup{}})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("server { listen 18080; }\n")
	digest := gatewayContentDigest(content)
	params := map[string]string{
		"operation": "apply", "site_path": "/srv/gw/conf/site.conf",
		"artifact_path": "/srv/gw/artifacts/site.conf", "config_sha256": digest,
	}
	task, err := service.Submit(ctx, tasks.SubmitRequest{
		SubjectID: "owner", RequestID: "r2-submit", Name: "r2 gateway",
		ActionID: gateway.NginxActionApply, OperationClass: tasks.OperationClassModifying,
		TargetType: "node_ids", TargetNodeIDs: []string{"node-one"}, Params: params,
		IdempotencyKey: "r2-gateway-unique",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.register(gatewayPlannedTask{
		TaskID: task.ID, ActionID: gateway.NginxActionApply, GatewayID: "gw-one", NodeID: "node-one",
		SitePath: params["site_path"], ArtifactPath: params["artifact_path"], Digest: digest,
		Content: content, PlanID: "plan-one", TargetRevision: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(ctx, task.ID, task.Revision, tasks.Decision{ApproverID: "reviewer", RequestID: "r2-approve", Approve: true}); err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &gatewayR2Fixture{db: db, repo: repo, service: service, dispatcher: dispatcher,
		uploads: uploads, next: next, task: task, content: content, digest: digest, params: params}
}

func (f *gatewayR2Fixture) checkStage(planDigest, artifactPath, sitePath, configDigest string) error {
	return f.dispatcher.CheckArtifactStage(context.Background(), f.task.ID, "node-one", "plan-one", 2,
		"gw-one", planDigest, artifactPath, sitePath, configDigest)
}

func TestGatewayArtifactStageRejectsStalePlanAndParameters(t *testing.T) {
	fixture := newGatewayR2Fixture(t)
	if err := fixture.checkStage(fixture.task.PlanHash, fixture.params["artifact_path"], fixture.params["site_path"], fixture.digest); err != nil {
		t.Fatalf("current stage rejected: %v", err)
	}
	cases := []struct{ name, plan, artifact, site, digest string }{
		{"stale plan", gatewayContentDigest([]byte("old-plan")), fixture.params["artifact_path"], fixture.params["site_path"], fixture.digest},
		{"changed artifact", fixture.task.PlanHash, "/srv/gw/artifacts/other.conf", fixture.params["site_path"], fixture.digest},
		{"changed site", fixture.task.PlanHash, fixture.params["artifact_path"], "/srv/gw/conf/other.conf", fixture.digest},
		{"changed bytes", fixture.task.PlanHash, fixture.params["artifact_path"], fixture.params["site_path"], gatewayContentDigest([]byte("other"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := fixture.checkStage(tc.plan, tc.artifact, tc.site, tc.digest); !errors.Is(err, tasks.ErrApprovalInvalid) {
				t.Fatalf("err=%v, want ErrApprovalInvalid", err)
			}
		})
	}
}

func TestGatewayArtifactStageRejectsOldBindingAfterRevisionAndNewApproval(t *testing.T) {
	fixture := newGatewayR2Fixture(t)
	ctx := context.Background()
	newContent := []byte("server { listen 18081; }\n")
	newDigest := gatewayContentDigest(newContent)
	newParams := map[string]string{
		"operation": "apply", "site_path": fixture.params["site_path"],
		"artifact_path": fixture.params["artifact_path"], "config_sha256": newDigest,
	}
	revised, err := fixture.service.Revise(ctx, fixture.task.ID, tasks.ReviseRequest{
		SubjectID: "owner", RequestID: "r2-revise", ExpectedRevision: fixture.task.Revision,
		TargetNodeIDs: []string{"node-one"}, Params: newParams,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Decide(ctx, revised.ID, revised.Revision, tasks.Decision{ApproverID: "reviewer", RequestID: "r2-approve-two", Approve: true}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.checkStage(fixture.task.PlanHash, fixture.params["artifact_path"], fixture.params["site_path"], fixture.digest); !errors.Is(err, tasks.ErrApprovalInvalid) {
		t.Fatalf("old binding borrowed the new approval: err=%v", err)
	}
}

func TestGatewayChildLifecycleNeverCompletesParentTask(t *testing.T) {
	fixture := newGatewayR2Fixture(t)
	ctx := context.Background()
	result, err := fixture.dispatcher.Dispatch(ctx, tasks.Command{
		TaskID: fixture.task.ID, NodeID: "node-one", ActionID: gateway.NginxActionApply,
		Params: json.RawMessage(fixture.task.Params),
	})
	if err != nil || !result.Accepted || fixture.next.calls != 1 {
		t.Fatalf("dispatch=%+v err=%v next=%d", result, err, fixture.next.calls)
	}
	binding := fixture.uploads.binding
	if binding.ExecutionID == executionIDFor(fixture.task.ID, "node-one", fixture.task.PlanHash) {
		t.Fatal("artifact child reused the Nginx execution id")
	}
	if err := saveGatewayArtifactBinding(ctx, fixture.db, binding); err != nil {
		t.Fatal(err)
	}
	observer, err := NewGatewayArtifactObserver(fixture.db, fixture.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	observation := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: binding.NodeID}, ConnectionID: "r2-link"}
	for _, envelope := range []*agentv1.Envelope{
		gatewayChildAck(binding.TaskID, binding.ExecutionID, true),
		gatewayChildStarted(binding.TaskID, binding.ExecutionID),
		gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED),
	} {
		if owned, err := observer.Observe(ctx, observation, envelope); err != nil || !owned {
			t.Fatalf("child event owned=%v err=%v", owned, err)
		}
	}
	parent, err := fixture.service.Get(ctx, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != tasks.StatusQueued {
		t.Fatalf("child upload changed parent status to %s", parent.Status)
	}
	for _, target := range parent.Targets {
		if target.State == tasks.TargetSucceeded {
			t.Fatalf("child upload marked parent target %s succeeded", target.NodeID)
		}
	}
}
