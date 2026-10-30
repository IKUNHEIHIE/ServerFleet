package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func TestAgentOutputFilteredBeforeDiskAndProductionCursorSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := privateAgentDirectory(t)
	actions := testActions(func(context.Context, []byte) executionResult {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: []byte("line with synthetic-secret\nsecond line")}
	})
	action := actions["test.write-marker"]
	action.FilterOutput = func(_ context.Context, raw []byte) ([]byte, error) {
		return bytes.ReplaceAll(raw, []byte("synthetic-secret"), []byte("[redacted]")), nil
	}
	actions["test.write-marker"] = action
	e, err := newAgentExecutor(dir, actions)
	if err != nil {
		t.Fatal(err)
	}
	offer := testOffer()
	result, _, err := e.execute(ctx, offer)
	if err != nil || bytes.Contains(result.Output, []byte("synthetic-secret")) {
		t.Fatal(result, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, offer.ExecutionId, "result.json"))
	if err != nil || bytes.Contains(raw, []byte("synthetic-secret")) {
		t.Fatal("unfiltered output reached disk", err)
	}
	plan, saved, err := e.prepareOutput(ctx, offer, 8)
	if err != nil || plan.Count < 2 || plan.Truncated || !bytes.Equal(result.Output, saved.Output) {
		t.Fatal(plan, err)
	}
	_ = e.Close()
	e, err = newAgentExecutor(dir, actions)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	reply, err := e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-logs", MaxExecutions: 1})
	if err != nil || len(reply.Executions) != 1 || reply.Executions[0].LastOutputSequence != uint64(plan.Count) {
		t.Fatal(reply, err)
	}
	// A changed partition may not reinterpret an already produced stream.
	prior, _, err := e.prepareOutput(ctx, offer, 16)
	if err != nil || prior != plan {
		t.Fatal("stream identity changed on replay", prior, err)
	}
	if err = os.WriteFile(filepath.Join(dir, offer.ExecutionId, "result.json"), []byte(`{"Result":1,"Output":"bXV0YXRlZA=="}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.snapshots(ctx, &agentv1.StateSyncRequest{SyncId: "sync-logs", MaxExecutions: 1}); err != errExecutionRejected {
		t.Fatal("mutated output reused old sequence/hash", err)
	}
}

func TestAgentMissingOrFailingOutputPolicyNeverPersistsRawLogs(t *testing.T) {
	for _, mode := range []string{"missing", "error", "panic"} {
		dir := privateAgentDirectory(t)
		actions := testActions(func(context.Context, []byte) executionResult {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: []byte("synthetic-secret")}
		})
		action := actions["test.write-marker"]
		switch mode {
		case "missing":
			action.FilterOutput = nil
		case "error":
			action.FilterOutput = func(context.Context, []byte) ([]byte, error) { return nil, errors.New("policy unavailable") }
		case "panic":
			action.FilterOutput = func(context.Context, []byte) ([]byte, error) { panic("must not leak diagnostics") }
		}
		actions["test.write-marker"] = action
		e, err := newAgentExecutor(dir, actions)
		if err != nil {
			t.Fatal(err)
		}
		offer := testOffer()
		result, _, err := e.execute(context.Background(), offer)
		if err != nil || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED || len(result.Output) != 0 || !result.Truncated {
			t.Fatal(mode, result, err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, offer.ExecutionId, "result.json"))
		if err != nil || bytes.Contains(raw, []byte("synthetic-secret")) {
			t.Fatal(mode, err)
		}
		plan, _, err := e.prepareOutput(context.Background(), offer, 1024)
		if err != nil || plan.Count != 1 || !plan.Truncated || plan.Bytes != 0 {
			t.Fatal(mode, plan, err)
		}
		_ = e.Close()
	}
}

func TestAgentOutputChunkCountIsBoundedAndTruncationExplicit(t *testing.T) {
	e, err := newAgentExecutor(privateAgentDirectory(t), testActions(func(context.Context, []byte) executionResult {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: bytes.Repeat([]byte("a"), 1000)}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	offer := testOffer()
	if _, _, err = e.execute(context.Background(), offer); err != nil {
		t.Fatal(err)
	}
	plan, _, err := e.prepareOutput(context.Background(), offer, 1)
	if err != nil || plan.Count != 64 || plan.Bytes != 64 || !plan.Truncated {
		t.Fatal(plan, err)
	}
}
