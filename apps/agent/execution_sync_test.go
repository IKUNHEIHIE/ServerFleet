package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func TestExecutionAckAndStartFollowDurableLedgerAndDoNotReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	calls := 0
	e, err := newAgentExecutor(dir, testActions(func(context.Context, []byte) executionResult {
		calls++
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	offer := testOffer()
	stages := []string{}
	observer := func(stage string, replay bool) error {
		if replay {
			t.Fatal("first execution marked replay")
		}
		stages = append(stages, stage)
		if _, err := os.Stat(filepath.Join(dir, offer.ExecutionId, "claim.json")); err != nil {
			t.Fatal("ack before durable claim", err)
		}
		if stage == "started" {
			if _, err := os.Stat(filepath.Join(dir, offer.ExecutionId, "started.json")); err != nil {
				t.Fatal("start event before durable marker", err)
			}
		}
		if calls != 0 {
			t.Fatal("effect preceded lifecycle event")
		}
		return nil
	}
	if result, seen, err := e.executeObserved(ctx, offer, observer); err != nil || seen || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED || calls != 1 {
		t.Fatal(result, seen, err)
	}
	if len(stages) != 2 || stages[0] != "accepted" || stages[1] != "started" {
		t.Fatal(stages)
	}
	if _, seen, err := e.executeObserved(ctx, offer, func(stage string, replay bool) error {
		if stage != "accepted" || !replay {
			t.Fatal("replay fabricated a new start", stage, replay)
		}
		return nil
	}); err != nil || !seen || calls != 1 {
		t.Fatal(seen, err, calls)
	}
	reply, err := e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 10})
	if err != nil || len(reply.Executions) != 1 || !reply.Executions[0].Started || reply.Executions[0].Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatal(reply, err)
	}
}

func TestExecutionFailedNotificationLeavesUnknownReservationAcrossRestart(t *testing.T) {
	ctx := context.Background()
	for _, stop := range []string{"accepted", "started"} {
		t.Run(stop, func(t *testing.T) {
			dir := t.TempDir()
			calls := 0
			actions := testActions(func(context.Context, []byte) executionResult {
				calls++
				return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
			})
			e, err := newAgentExecutor(dir, actions)
			if err != nil {
				t.Fatal(err)
			}
			offer := testOffer()
			if _, _, err = e.executeObserved(ctx, offer, func(stage string, _ bool) error {
				if stage == stop {
					return errors.New("notification outcome unknown")
				}
				return nil
			}); err == nil || calls != 0 {
				t.Fatal("failed event allowed effects", calls, err)
			}
			_ = e.Close()
			e, err = newAgentExecutor(dir, actions)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			if result, seen, err := e.execute(ctx, offer); err != nil || !seen || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN || calls != 0 {
				t.Fatal("restart replayed unconfirmed execution", result, seen, err, calls)
			}
			reply, err := e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 1})
			if err != nil || len(reply.Executions) != 1 || reply.Executions[0].Started != (stop == "started") || reply.Executions[0].Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
				t.Fatal(reply, err)
			}
		})
	}
}

func TestExecutionPanicAndCorruptSnapshotAreNeverReportedSuccessful(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	calls := 0
	e, err := newAgentExecutor(dir, testActions(func(context.Context, []byte) executionResult {
		calls++
		panic("registered capability crashed after possible effects")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	offer := testOffer()
	if result, _, err := e.execute(ctx, offer); err != nil || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
		t.Fatal(result, err)
	}
	if _, seen, err := e.execute(ctx, offer); err != nil || !seen || calls != 1 {
		t.Fatal("panic reran effect", seen, calls, err)
	}
	if err := os.WriteFile(filepath.Join(dir, offer.ExecutionId, "result.json"), []byte(`{"Result":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 1}); err != errExecutionRejected {
		t.Fatal("corrupt enum reported", err)
	}
	if _, _, err := e.execute(ctx, offer); err != errExecutionRejected {
		t.Fatal("corrupt stored result replayed", err)
	}
}

func TestExecutionSnapshotBoundsTruncationAndNoOutputDisclosure(t *testing.T) {
	ctx := context.Background()
	e, err := newAgentExecutor(t.TempDir(), testActions(func(context.Context, []byte) executionResult {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: []byte("private-capability-output")}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, id := range []string{"execution-a", "execution-b"} {
		offer := testOffer()
		offer.ExecutionId = id
		if _, _, err = e.execute(ctx, offer); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 1})
	if err != nil || len(reply.Executions) != 1 || !reply.Truncated || reply.Executions[0].ExecutionId != "execution-a" {
		t.Fatal(reply, err)
	}
	if _, err = e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 1001}); err != errExecutionRejected {
		t.Fatal("unbounded sync accepted", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = e.snapshots(canceled, &agentv1.StateSyncRequest{SyncId: "sync-one", MaxExecutions: 1}); err != errExecutionRejected {
		t.Fatal("canceled sync accepted", err)
	}
}
