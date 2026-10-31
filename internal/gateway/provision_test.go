package gateway_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
)

func TestProvisionRejectsExistingSiteWithoutApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", true)
	cases := []gateway.ProvisionInput{
		{ExistingSite: true},
		{ExistingSite: true, TakeoverApproved: true},
		{ExistingSite: false, TakeoverApproved: true},
	}
	for index, input := range cases {
		if _, err := f.service.Provision(ctx, "admin", "req-takeover", "gw1", input); !errors.Is(err, gateway.ErrTakeoverApprovalRequired) {
			t.Fatalf("case %d err = %v", index, err)
		}
	}
	if got := f.ports.tasks.callCount(); got != 0 {
		t.Fatalf("rejected takeover submitted %d tasks", got)
	}
	if provisions := f.provisions(t, "gw1"); len(provisions) != 0 {
		t.Fatalf("rejected takeover persisted provisions: %+v", provisions)
	}
	stored := f.gateway(t, "gw1")
	if stored.Status != gateway.StatusPending || stored.Revision != 1 || !stored.Enabled {
		t.Fatalf("rejected takeover changed gateway: %+v", stored)
	}
	if got := f.auditCount(t, gateway.ActionProvision, "success"); got != 0 {
		t.Fatalf("provision audit = %d", got)
	}
}

func TestProvisionSubmitsOneTaskAndReconciles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", true)
	provision, err := f.service.Provision(ctx, "admin", "req-prov", "gw1", gateway.ProvisionInput{
		ExistingSite: true, TakeoverApproved: true, ApprovalRef: "approval-123",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provision.Status != gateway.ProvisionQueued || provision.TaskID != "task-1" || provision.ID == "" {
		t.Fatalf("provision = %+v", provision)
	}
	if got := f.ports.tasks.callCount(); got != 1 {
		t.Fatalf("task submits = %d", got)
	}
	request := f.ports.tasks.lastRequest()
	if request.SubjectID != "admin" || request.RequestID != "req-prov" || request.ActionID != gateway.ActionProvision {
		t.Fatalf("task request = %+v", request)
	}
	if request.ResourceID != "gw1" || len(request.NodeIDs) != 1 || request.NodeIDs[0] != "n1" {
		t.Fatalf("task request target = %+v", request)
	}
	if request.Params[gateway.ParamExistingSite] != "true" ||
		request.Params[gateway.ParamTakeoverApproved] != "true" ||
		request.Params[gateway.ParamApprovalRef] != "approval-123" {
		t.Fatalf("task params = %+v", request.Params)
	}
	if request.IdempotencyKey != "req-prov" {
		t.Fatalf("idempotency key = %q", request.IdempotencyKey)
	}
	stored := f.gateway(t, "gw1")
	if stored.Status != gateway.StatusInstalling || stored.Revision != 2 || stored.ChecksPassed {
		t.Fatalf("gateway after provision = %+v", stored)
	}
	if _, err := f.service.Provision(ctx, "admin", "req-prov-2", "gw1", gateway.ProvisionInput{}); !errors.Is(err, gateway.ErrProvisionInProgress) {
		t.Fatalf("second provision err = %v", err)
	}
	if got := f.ports.tasks.callCount(); got != 1 {
		t.Fatalf("second provision submitted another task: %d", got)
	}
	page, err := f.recorder.Query(ctx, audit.Query{Action: gateway.ActionProvision, TaskID: "task-1", Limit: 10})
	if err != nil || page.Total != 1 {
		t.Fatalf("provision audit by task = %+v, %v", page, err)
	}

	running, err := f.service.ApplyProvisionUpdate(ctx, "task-1", "running", []gateway.ProvisionStep{
		{Name: "install", Status: gateway.StepRunning, Message: "installing token=hidden"},
	}, "")
	if err != nil {
		t.Fatalf("Apply running: %v", err)
	}
	if running.Status != gateway.ProvisionRunning || running.StartedAt.IsZero() || len(running.Steps) != 1 {
		t.Fatalf("running provision = %+v", running)
	}
	if strings.Contains(running.Steps[0].Message, "hidden") || !strings.Contains(running.Steps[0].Message, "[redacted]") {
		t.Fatalf("step message = %q", running.Steps[0].Message)
	}
	stored = f.gateway(t, "gw1")
	if stored.Status != gateway.StatusInstalling || stored.Revision != 3 {
		t.Fatalf("gateway after running = %+v", stored)
	}

	completed, err := f.service.ApplyProvisionUpdate(ctx, "task-1", "completed", []gateway.ProvisionStep{
		{Name: "install", Status: gateway.StepSucceeded},
	}, "")
	if err != nil {
		t.Fatalf("Apply completed: %v", err)
	}
	if completed.Status != gateway.ProvisionCompleted || completed.FinishedAt.IsZero() {
		t.Fatalf("completed provision = %+v", completed)
	}
	stored = f.gateway(t, "gw1")
	if stored.Status != gateway.StatusVerifying || stored.Revision != 4 || stored.ChecksPassed || !stored.Enabled {
		t.Fatalf("gateway after completion = %+v", stored)
	}
	eligible, err := f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || eligible {
		t.Fatalf("completed without checks eligible = %v, %v", eligible, err)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, "task-1", "completed", nil, ""); err != nil {
		t.Fatalf("idempotent completed: %v", err)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, "task-1", "running", nil, ""); !errors.Is(err, gateway.ErrProvisionTerminal) {
		t.Fatalf("regression err = %v", err)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, "task-1", "bogus", nil, ""); !errors.Is(err, gateway.ErrInvalidProvision) {
		t.Fatalf("unknown status err = %v", err)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, "missing-task", "completed", nil, ""); !errors.Is(err, gateway.ErrProvisionNotFound) {
		t.Fatalf("unknown task err = %v", err)
	}

	fetched, err := f.service.GetProvision(ctx, "admin", provision.ID)
	if err != nil || fetched.Status != gateway.ProvisionCompleted {
		t.Fatalf("GetProvision = %+v, %v", fetched, err)
	}
	checked, err := f.service.RecordChecks(ctx, "admin", "req-checks", "gw1", stored.Revision, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.25.4",
	})
	if err != nil {
		t.Fatalf("RecordChecks: %v", err)
	}
	if checked.Status != gateway.StatusReady {
		t.Fatalf("checked gateway = %+v", checked)
	}
	eligible, err = f.service.SchedulingEligible(ctx, "admin", "gw1")
	if err != nil || !eligible {
		t.Fatalf("eligible after checks = %v, %v", eligible, err)
	}
	if got := f.auditCount(t, gateway.ActionProvision, "success"); got != 1 {
		t.Fatalf("provision audit = %d", got)
	}
	if got := f.auditCount(t, gateway.ActionProvisionUpdate, "success"); got != 2 {
		t.Fatalf("provision update audit = %d", got)
	}
}

func TestApplyProvisionFailureRedactsAndDisables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", true)
	provision, err := f.service.Provision(ctx, "admin", "req-prov", "gw1", gateway.ProvisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, provision.TaskID, "running", nil, ""); err != nil {
		t.Fatal(err)
	}
	failed, err := f.service.ApplyProvisionUpdate(ctx, provision.TaskID, "failed", []gateway.ProvisionStep{
		{Name: "install", Status: gateway.StepFailed, Message: "token=stepsecret"},
	}, "install failed token=supersecret at https://user:pass@example.com")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if failed.Status != gateway.ProvisionFailed || failed.FinishedAt.IsZero() {
		t.Fatalf("failed provision = %+v", failed)
	}
	if strings.Contains(failed.Error, "supersecret") || strings.Contains(failed.Error, "user:pass") {
		t.Fatalf("provision error not redacted: %q", failed.Error)
	}
	if strings.Contains(failed.Steps[0].Message, "stepsecret") {
		t.Fatalf("step message not redacted: %q", failed.Steps[0].Message)
	}
	stored := f.gateway(t, "gw1")
	if stored.Status != gateway.StatusFailed || stored.Enabled {
		t.Fatalf("gateway after failure = %+v", stored)
	}
	if strings.Contains(stored.LastError, "supersecret") || strings.Contains(stored.LastError, "user:pass") {
		t.Fatalf("gateway error not redacted: %q", stored.LastError)
	}
	if got := f.auditCount(t, gateway.ActionProvisionUpdate, "failure"); got != 1 {
		t.Fatalf("failure audit = %d", got)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, provision.TaskID, "failed", nil, "again"); err != nil {
		t.Fatalf("idempotent failed: %v", err)
	}
	if got := f.auditCount(t, gateway.ActionProvisionUpdate, "failure"); got != 1 {
		t.Fatalf("idempotent failure audit = %d", got)
	}
	if _, err := f.service.ApplyProvisionUpdate(ctx, provision.TaskID, "completed", nil, ""); !errors.Is(err, gateway.ErrProvisionTerminal) {
		t.Fatalf("completed after failure err = %v", err)
	}
}

func TestProvisionAuthDenialDoesNotSubmitTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newGatewayFixture(t)
	f.createGateway(t, "gw1", "edge", false)
	denied := newGatewayService(t, f, &switchAuthorizer{deny: true})
	if _, err := denied.Provision(ctx, "admin", "req-denied", "gw1", gateway.ProvisionInput{}); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied provision err = %v", err)
	}
	if got := f.ports.tasks.callCount(); got != 0 {
		t.Fatalf("denied provision submitted %d tasks", got)
	}
	if provisions := f.provisions(t, "gw1"); len(provisions) != 0 {
		t.Fatalf("denied provision persisted: %+v", provisions)
	}
	if stored := f.gateway(t, "gw1"); stored.Status != gateway.StatusPending {
		t.Fatalf("denied provision changed gateway: %+v", stored)
	}
	if got := f.auditCount(t, gateway.ActionWrite, "denied"); got != 1 {
		t.Fatalf("denial audit = %d", got)
	}
}

func TestProvisionPersistenceRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "provision-restart.db")
	ports := defaultGatewayPorts()
	first := newGatewayFixtureAt(t, path, ports, nil)
	first.createGateway(t, "gw1", "edge", true)
	provision, err := first.service.Provision(ctx, "admin", "req-prov", "gw1", gateway.ProvisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.service.ApplyProvisionUpdate(ctx, provision.TaskID, "running", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := first.db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newGatewayFixtureAt(t, path, ports, nil)
	reloaded, err := second.repo.GetProvisionByTaskID(ctx, second.db, provision.TaskID)
	if err != nil || reloaded.Status != gateway.ProvisionRunning || reloaded.StartedAt.IsZero() {
		t.Fatalf("reloaded provision = %+v, %v", reloaded, err)
	}
	stored := second.gateway(t, "gw1")
	if stored.Status != gateway.StatusInstalling || stored.Revision != 3 {
		t.Fatalf("gateway after restart = %+v", stored)
	}
	if _, err := second.service.ApplyProvisionUpdate(ctx, provision.TaskID, "completed", nil, ""); err != nil {
		t.Fatalf("resume completed: %v", err)
	}
	checked, err := second.service.RecordChecks(ctx, "admin", "req-checks", "gw1", stored.Revision+1, gateway.CheckReport{
		NginxInstalled: true, ChecksPassed: true,
	})
	if err != nil || checked.Status != gateway.StatusReady {
		t.Fatalf("checks after restart = %+v, %v", checked, err)
	}
	if got := ports.tasks.callCount(); got != 1 {
		t.Fatalf("task submits after restart = %d", got)
	}
}
