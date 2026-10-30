package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

type executionStarted struct{ At time.Time }

func validExecutionResult(result executionResult) bool {
	if len(result.Output) > 64<<10 {
		return false
	}
	switch result.Result {
	case agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, agentv1.ExecutionResult_EXECUTION_RESULT_FAILED, agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED, agentv1.ExecutionResult_EXECUTION_RESULT_TIMED_OUT, agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN:
		return true
	default:
		return false
	}
}

func runRegisteredAction(ctx context.Context, action agentAction, parameters []byte) (result executionResult) {
	defer func() {
		if recover() != nil {
			// A registered capability may already have had effects before a panic.
			result = executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
	}()
	return action.Run(ctx, parameters)
}

// snapshots reads only the local private execution ledger, never Controller
// tables. Missing completion is UNKNOWN, not queued/retryable. `started` means
// durable intent-to-start, not proof that an external effect actually occurred.
// Live progress may only be overlaid by the active runtime's own running map.
func (e *agentExecutor) snapshots(ctx context.Context, request *agentv1.StateSyncRequest) (*agentv1.StateSyncReply, error) {
	if e == nil || e.ledger == nil || ctx == nil || ctx.Err() != nil || request == nil || !executionIDPattern.MatchString(request.SyncId) || request.MaxExecutions == 0 || request.MaxExecutions > 1000 || request.SinceUnixMs < 0 {
		return nil, errExecutionRejected
	}
	dir, err := e.ledger.Open(".")
	if err != nil {
		return nil, errExecutionRejected
	}
	entries, readErr := dir.ReadDir(10001)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) > 10000 {
		return nil, errExecutionRejected
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})
	reply := &agentv1.StateSyncReply{SyncId: request.SyncId}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !executionIDPattern.MatchString(entry.Name()) {
			return nil, errExecutionRejected
		}
		name := entry.Name()
		data, err := e.readRecord(name+"/claim.json", 1024)
		var claim executionClaim
		if err != nil || json.Unmarshal(data, &claim) != nil || claim.ExecutionID != name || !executionIDPattern.MatchString(claim.TaskID) {
			return nil, errExecutionRejected
		}
		digest, err := hex.DecodeString(claim.OfferDigest)
		if err != nil || len(digest) != 32 {
			return nil, errExecutionRejected
		}
		info, err := e.ledger.Stat(name + "/claim.json")
		if err != nil {
			return nil, errExecutionRejected
		}
		snapshot := &agentv1.ExecutionSnapshot{TaskId: claim.TaskID, ExecutionId: claim.ExecutionID, Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN, UpdatedAtUnixMs: info.ModTime().UnixMilli()}
		data, err = e.readRecord(name+"/started.json", 256)
		if err == nil {
			var started executionStarted
			if json.Unmarshal(data, &started) != nil || started.At.IsZero() {
				return nil, errExecutionRejected
			}
			snapshot.Started = true
			snapshot.UpdatedAtUnixMs = max(snapshot.UpdatedAtUnixMs, started.At.UnixMilli())
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errExecutionRejected
		}
		data, err = e.readRecord(name+"/result.json", 128<<10)
		if err == nil {
			var result executionResult
			if json.Unmarshal(data, &result) != nil || !validExecutionResult(result) {
				return nil, errExecutionRejected
			}
			// Legacy terminal records were written only after a capability returned.
			// They attest invocation even without the newer pre-start marker. This
			// is not proof of an external effect beyond that recorded outcome.
			snapshot.Started = true
			snapshot.Result = result.Result
			info, err := e.ledger.Stat(name + "/result.json")
			if err != nil {
				return nil, errExecutionRejected
			}
			snapshot.UpdatedAtUnixMs = max(snapshot.UpdatedAtUnixMs, info.ModTime().UnixMilli())
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errExecutionRejected
		}
		if snapshot.UpdatedAtUnixMs <= 0 {
			return nil, errExecutionRejected
		}
		if snapshot.UpdatedAtUnixMs < request.SinceUnixMs {
			continue
		}
		if len(reply.Executions) == int(request.MaxExecutions) {
			reply.Truncated = true
			break
		}
		// No output bytes or offer parameters are exposed by reconciliation.
		reply.Executions = append(reply.Executions, snapshot)
	}
	return reply, nil
}
