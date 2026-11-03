package server

import (
	"context"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"testing"
	"time"
)

func TestReviewR3GatewayLateRejectedAckCannotOverwriteSuccess(t *testing.T) {
	f, o, b, obs := newGatewayChildFixture(t)
	ctx := context.Background()
	for _, e := range []*agentv1.Envelope{
		gatewayChildAck(b.TaskID, b.ExecutionID, true),
		gatewayChildStarted(b.TaskID, b.ExecutionID),
		gatewayChildFinished(b.TaskID, b.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED),
	} {
		if owned, err := o.Observe(ctx, obs, e); !owned || err != nil {
			t.Fatalf("setup: owned=%v err=%v", owned, err)
		}
	}
	_, _ = o.Observe(ctx, obs, gatewayChildAck(b.TaskID, b.ExecutionID, false))
	got, err := loadGatewayArtifactBinding(ctx, f.db, b.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "finished" || got.Result != "EXECUTION_RESULT_SUCCEEDED" {
		t.Fatalf("late rejected Ack overwrote durable terminal: state=%q result=%q", got.State, got.Result)
	}
}

type reviewR3LateEventKey struct{}
type reviewR3StageBarrier struct {
	GatewayArtifactStageChecker
	entered chan struct{}
	release chan struct{}
}

func (s *reviewR3StageBarrier) CheckArtifactChild(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error {
	if ctx.Value(reviewR3LateEventKey{}) != nil {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.GatewayArtifactStageChecker.CheckArtifactChild(ctx, taskID, nodeID, planID, targetRevision, gatewayID, planDigest, artifactPath, sitePath, configDigest)
}
func TestReviewR3GatewayConcurrentStartedCannotOverwriteFinished(t *testing.T) {
	f, o, b, obs := newGatewayChildFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := o.Observe(ctx, obs, gatewayChildAck(b.TaskID, b.ExecutionID, true)); err != nil {
		t.Fatal(err)
	}
	barrier := &reviewR3StageBarrier{GatewayArtifactStageChecker: o.stage, entered: make(chan struct{}), release: make(chan struct{})}
	o.stage = barrier
	done := make(chan error, 1)
	go func() {
		_, err := o.Observe(context.WithValue(ctx, reviewR3LateEventKey{}, true), obs, gatewayChildStarted(b.TaskID, b.ExecutionID))
		done <- err
	}()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, finishErr := o.Observe(ctx, obs, gatewayChildFinished(b.TaskID, b.ExecutionID, agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED))
	close(barrier.release)
	if finishErr != nil {
		t.Fatal(finishErr)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	got, err := loadGatewayArtifactBinding(ctx, f.db, b.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "finished" || got.Result != "EXECUTION_RESULT_SUCCEEDED" {
		t.Fatalf("stale concurrent Started overwrote durable terminal: state=%q result=%q", got.State, got.Result)
	}
}
