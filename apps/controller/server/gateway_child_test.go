package server

import (
	"context"
	"errors"
	"testing"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/transport"
)

func gatewayChildAck(taskID, executionID string, accepted bool) *agentv1.Envelope {
	return &agentv1.Envelope{Message: &agentv1.Envelope_TaskAck{TaskAck: &agentv1.TaskAck{
		TaskId: taskID, ExecutionId: executionID, Accepted: accepted,
	}}}
}

func gatewayChildStarted(taskID, executionID string) *agentv1.Envelope {
	return &agentv1.Envelope{Message: &agentv1.Envelope_TaskStarted{TaskStarted: &agentv1.TaskStarted{
		TaskId: taskID, ExecutionId: executionID,
	}}}
}

func gatewayChildFinished(taskID, executionID string, result agentv1.ExecutionResult) *agentv1.Envelope {
	return &agentv1.Envelope{Message: &agentv1.Envelope_TaskFinished{TaskFinished: &agentv1.TaskFinished{
		TaskId: taskID, ExecutionId: executionID, Result: result,
	}}}
}

func newGatewayChildFixture(t *testing.T) (*gatewayWSSFixture, *GatewayArtifactObserver, GatewayArtifactBinding, transport.LinkObservation) {
	t.Helper()
	fixture := newGatewayWSSFixture(t)
	binding := fixture.binding([]byte("server { }\n"))
	if err := saveGatewayArtifactBinding(context.Background(), fixture.db, binding); err != nil {
		t.Fatal(err)
	}
	observer, err := NewGatewayArtifactObserver(fixture.db, fixture.stage)
	if err != nil {
		t.Fatal(err)
	}
	observation := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: binding.NodeID}, ConnectionID: "connection-one"}
	return fixture, observer, binding, observation
}

func TestGatewayArtifactObserverRoutesChildLifecycleWithoutParentResult(t *testing.T) {
	fixture, observer, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	for _, step := range []struct {
		name     string
		envelope *agentv1.Envelope
		state    string
		result   string
	}{
		{"ack", gatewayChildAck(binding.TaskID, binding.ExecutionID, true), "accepted", ""},
		{"started", gatewayChildStarted(binding.TaskID, binding.ExecutionID), "started", ""},
		{"finished", gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED), "finished", "EXECUTION_RESULT_SUCCEEDED"},
	} {
		owned, err := observer.Observe(ctx, observation, step.envelope)
		if err != nil || !owned {
			t.Fatalf("%s owned=%v err=%v", step.name, owned, err)
		}
		stored, err := loadGatewayArtifactBinding(ctx, fixture.db, binding.ExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != step.state || stored.Result != step.result {
			t.Fatalf("%s state=%q result=%q, want %q/%q", step.name, stored.State, stored.Result, step.state, step.result)
		}
	}
}

func TestGatewayArtifactObserverIsIdempotentAndRejectsConflicts(t *testing.T) {
	fixture, observer, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := observer.Observe(ctx, observation, gatewayChildAck(binding.TaskID, binding.ExecutionID, true)); err != nil {
			t.Fatal(err)
		}
		if _, err := observer.Observe(ctx, observation, gatewayChildStarted(binding.TaskID, binding.ExecutionID)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)); err != nil {
		t.Fatalf("duplicate terminal event was rejected: %v", err)
	}
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_FAILED)); !errors.Is(err, files.ErrTransferIdentity) {
		t.Fatalf("conflicting terminal err=%v, want ErrTransferIdentity", err)
	}
	stored, err := loadGatewayArtifactBinding(ctx, fixture.db, binding.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result != "EXECUTION_RESULT_SUCCEEDED" {
		t.Fatalf("terminal result was overwritten: %q", stored.Result)
	}
}

func TestGatewayArtifactObserverUnknownBeforeStartStaysUnknown(t *testing.T) {
	fixture, observer, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)); err != nil {
		t.Fatal(err)
	}
	stored, err := loadGatewayArtifactBinding(ctx, fixture.db, binding.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result != "EXECUTION_RESULT_UNKNOWN" {
		t.Fatalf("premature success was recorded as %q", stored.Result)
	}
}

func TestGatewayArtifactObserverAcceptsReconnectAndRejectsForeignNode(t *testing.T) {
	_, observer, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	reconnected := transport.LinkObservation{Identity: observation.Identity, ConnectionID: "connection-two"}
	if owned, err := observer.Observe(ctx, reconnected, gatewayChildAck(binding.TaskID, binding.ExecutionID, true)); err != nil || !owned {
		t.Fatalf("reconnect owned=%v err=%v", owned, err)
	}
	foreign := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: "node-two"}}
	if owned, err := observer.Observe(ctx, foreign, gatewayChildAck(binding.TaskID, binding.ExecutionID, true)); !owned || !errors.Is(err, files.ErrTaskBindingRequired) {
		t.Fatalf("foreign node owned=%v err=%v", owned, err)
	}
}

func TestGatewayArtifactObserverIgnoresUnrelatedExecutions(t *testing.T) {
	_, observer, binding, observation := newGatewayChildFixture(t)
	if owned, err := observer.Observe(context.Background(), observation, gatewayChildAck(binding.TaskID, "exec-unknown", true)); owned || err != nil {
		t.Fatalf("unrelated execution owned=%v err=%v", owned, err)
	}
}

func TestGatewayArtifactObserverMultipleObserversKeepTerminal(t *testing.T) {
	fixture, first, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	second, err := NewGatewayArtifactObserver(fixture.db, fixture.stage)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		observer *GatewayArtifactObserver
		envelope *agentv1.Envelope
	}{
		{first, gatewayChildAck(binding.TaskID, binding.ExecutionID, true)},
		{second, gatewayChildStarted(binding.TaskID, binding.ExecutionID)},
		{first, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)},
	}
	for _, step := range steps {
		if owned, err := step.observer.Observe(ctx, observation, step.envelope); err != nil || !owned {
			t.Fatalf("step owned=%v err=%v", owned, err)
		}
	}
	late := []*agentv1.Envelope{
		gatewayChildAck(binding.TaskID, binding.ExecutionID, false),
		gatewayChildStarted(binding.TaskID, binding.ExecutionID),
		gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN),
	}
	for _, envelope := range late {
		if _, err := second.Observe(ctx, observation, envelope); err != nil && !errors.Is(err, files.ErrTransferIdentity) {
			t.Fatalf("late event err=%v", err)
		}
	}
	stored, err := loadGatewayArtifactBinding(ctx, fixture.db, binding.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "finished" || stored.Result != "EXECUTION_RESULT_SUCCEEDED" {
		t.Fatalf("multiple observers moved terminal: state=%q result=%q", stored.State, stored.Result)
	}
}

func TestGatewayArtifactObserverUnknownNeverUpgradesToSuccess(t *testing.T) {
	fixture, observer, binding, observation := newGatewayChildFixture(t)
	ctx := context.Background()
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN)); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Observe(ctx, observation, gatewayChildFinished(binding.TaskID, binding.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)); err != nil && !errors.Is(err, files.ErrTransferIdentity) {
		t.Fatalf("unknown upgrade err=%v", err)
	}
	stored, err := loadGatewayArtifactBinding(ctx, fixture.db, binding.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result != "EXECUTION_RESULT_UNKNOWN" {
		t.Fatalf("UNKNOWN upgraded to %q", stored.Result)
	}
}
