package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func filterExecutionOutput(ctx context.Context, action agentAction, result executionResult) executionResult {
	if len(result.Output) == 0 {
		return result
	}
	borrowed := bytes.Clone(result.Output)
	// Registered Run transfers ownership of returned output; best-effort clearing
	// is not a promise to erase all copies that a program callback may retain.
	clear(result.Output)
	result.Output = nil
	defer clear(borrowed)
	if action.FilterOutput == nil {
		result.Truncated = true
		return result
	}
	filtered, err := callOutputFilter(ctx, action.FilterOutput, borrowed)
	if err != nil {
		result.Truncated = true
		return result
	}
	if len(filtered) == 0 {
		result.Truncated = true
	}
	if len(filtered) > 64<<10 {
		filtered = filtered[:64<<10]
		result.Truncated = true
	}
	result.Output = bytes.Clone(filtered)
	return result
}
func callOutputFilter(ctx context.Context, filter func(context.Context, []byte) ([]byte, error), raw []byte) (out []byte, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = errExecutionRejected
		}
	}()
	return filter(ctx, raw)
}

type executionOutputPlan struct {
	Version                                        int
	TaskID, ExecutionID, OfferDigest, ResultSHA256 string
	ChunkBytes, Count, Bytes                       int
	Truncated                                      bool
}

func outputPlanFor(claim executionClaim, raw []byte, result executionResult, chunkBytes int) executionOutputPlan {
	hash := sha256.Sum256(raw)
	total := min(len(result.Output), chunkBytes*64)
	count := (total + chunkBytes - 1) / chunkBytes
	truncated := result.Truncated || total < len(result.Output)
	if count == 0 && truncated {
		count = 1
	}
	return executionOutputPlan{1, claim.TaskID, claim.ExecutionID, claim.OfferDigest, hex.EncodeToString(hash[:]), chunkBytes, count, total, truncated}
}
func (e *agentExecutor) loadOutputPlan(ctx context.Context, name string, claim executionClaim) (executionOutputPlan, executionResult, bool, error) {
	if ctx == nil || ctx.Err() != nil {
		return executionOutputPlan{}, executionResult{}, false, errExecutionRejected
	}
	data, err := e.readRecord(name+"/output.json", 2048)
	if errors.Is(err, os.ErrNotExist) {
		return executionOutputPlan{}, executionResult{}, false, nil
	}
	var plan executionOutputPlan
	if err != nil || json.Unmarshal(data, &plan) != nil || plan.Version != 1 || plan.TaskID != claim.TaskID || plan.ExecutionID != claim.ExecutionID || plan.OfferDigest != claim.OfferDigest || plan.ChunkBytes < 1 || plan.ChunkBytes > 64<<10 || plan.Count < 1 || plan.Count > 64 || plan.Bytes < 0 || plan.Bytes > 64<<10 {
		return executionOutputPlan{}, executionResult{}, false, errExecutionRejected
	}
	canonical, err := json.Marshal(plan)
	if err != nil || !bytes.Equal(canonical, data) {
		return executionOutputPlan{}, executionResult{}, false, errExecutionRejected
	}
	raw, err := e.readRecord(name+"/result.json", 128<<10)
	var result executionResult
	if err != nil || json.Unmarshal(raw, &result) != nil || !validExecutionResult(result) || plan != outputPlanFor(claim, raw, result, plan.ChunkBytes) || ctx.Err() != nil {
		return executionOutputPlan{}, executionResult{}, false, errExecutionRejected
	}
	return plan, result, true, nil
}

// prepareOutput publishes a durable production cursor BEFORE network writes.
// It binds the exact sanitized terminal record and fixed chunk partition. This
// is not a Controller receipt/ACK; no resend or execution is triggered by sync.
func (e *agentExecutor) prepareOutput(ctx context.Context, offer *agentv1.TaskOffer, chunkBytes int) (executionOutputPlan, executionResult, error) {
	if e == nil || e.ledger == nil || ctx == nil || ctx.Err() != nil || offer == nil || !executionIDPattern.MatchString(offer.ExecutionId) || chunkBytes < 1 || chunkBytes > 64<<10 {
		return executionOutputPlan{}, executionResult{}, errExecutionRejected
	}
	name := offer.ExecutionId
	claim := executionClaim{offer.TaskId, offer.ExecutionId, offerDigest(offer)}
	data, err := e.readRecord(name+"/claim.json", 1024)
	var stored executionClaim
	if err != nil || json.Unmarshal(data, &stored) != nil || stored != claim {
		return executionOutputPlan{}, executionResult{}, errExecutionRejected
	}
	if plan, result, exists, err := e.loadOutputPlan(ctx, name, claim); err != nil {
		return executionOutputPlan{}, executionResult{}, err
	} else if exists {
		return plan, result, nil
	}
	raw, err := e.readRecord(name+"/result.json", 128<<10)
	var result executionResult
	if err != nil || json.Unmarshal(raw, &result) != nil || !validExecutionResult(result) {
		return executionOutputPlan{}, executionResult{}, errExecutionRejected
	}
	plan := outputPlanFor(claim, raw, result, chunkBytes)
	if plan.Count == 0 {
		return plan, result, nil
	}
	if err = e.writeRecord(name+"/output.json", plan); err != nil || e.syncDirectory(name) != nil || ctx.Err() != nil {
		return executionOutputPlan{}, executionResult{}, errExecutionRejected
	}
	return plan, result, nil
}
