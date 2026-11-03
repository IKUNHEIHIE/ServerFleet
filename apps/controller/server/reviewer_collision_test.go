package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type reviewerBoundCapture struct{ binding GatewayArtifactBinding }

func (c *reviewerBoundCapture) Upload(context.Context, string, string, string, []byte, string) error {
	return nil
}
func (c *reviewerBoundCapture) UploadBound(_ context.Context, b GatewayArtifactBinding) error {
	c.binding = b
	return nil
}

type reviewerNextCapture struct{ calls int }

func (c *reviewerNextCapture) Dispatch(context.Context, tasks.Command) (tasks.DispatchResult, error) {
	c.calls++
	return tasks.DispatchResult{Accepted: true}, nil
}
func (c *reviewerNextCapture) Cancel(context.Context, tasks.CancelCommand) error { return nil }

func TestReviewerGatewayArtifactAndApplyNeedDistinctExecutions(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations())
	repo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	v, err := audit.NewValidator(AuditActions(), audit.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	a, err := audit.NewSQLRecorder(db, v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	registry := newGatewayArtifactRegistry()
	u := &reviewerBoundCapture{}
	next := &reviewerNextCapture{}
	d, err := newGatewayTaskDispatcher(gatewayExecutionConfig{Repo: repo, DB: db, Artifacts: u, Next: next, Authorizer: reviewGatewayAuth(true), Registry: registry, planAuthority: &reviewGatewayAuthority{}, Authority: func(context.Context, tasks.Task, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	s, err := tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: a, Authorizer: reviewGatewayAuth(true), Dispatcher: d, Nodes: reviewNodeLookup{}})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("server {}\n")
	digest := gatewayContentDigest(content)
	params := map[string]string{"operation": "apply", "site_path": "/srv/gw/conf/site.conf", "artifact_path": "/srv/gw/artifacts/site.conf", "config_sha256": digest}
	task, err := s.Submit(ctx, tasks.SubmitRequest{SubjectID: "owner", RequestID: "review-submit", Name: "review gateway", ActionID: gateway.NginxActionApply, OperationClass: tasks.OperationClassModifying, TargetType: "node_ids", TargetNodeIDs: []string{"node-one"}, Params: params, IdempotencyKey: "review-gateway-unique"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.register(gatewayPlannedTask{TaskID: task.ID, ActionID: gateway.NginxActionApply, GatewayID: "gw-one", NodeID: "node-one", SitePath: params["site_path"], ArtifactPath: params["artifact_path"], Digest: digest, Content: content, PlanID: "plan-one", TargetRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, task.ID, task.Revision, tasks.Decision{ApproverID: "reviewer", RequestID: "review-approve", Approve: true}); err != nil {
		t.Fatal(err)
	}
	task, err = s.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Dispatch(ctx, tasks.Command{TaskID: task.ID, NodeID: "node-one", ActionID: gateway.NginxActionApply, Params: json.RawMessage(task.Params)})
	if err != nil || !result.Accepted || next.calls != 1 {
		t.Fatalf("dispatch=%+v err=%v next=%d", result, err, next.calls)
	}
	applyExecution := executionIDFor(task.ID, "node-one", task.PlanHash)
	if u.binding.ExecutionID == applyExecution {
		t.Fatalf("artifact child and Nginx apply reuse the same Agent ledger execution ID: %s", applyExecution)
	}
}
