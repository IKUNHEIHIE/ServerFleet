package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func gatewayLedgerOffer(taskID, executionID, actionID string) *agentv1.TaskOffer {
	return &agentv1.TaskOffer{
		TaskId: taskID, ExecutionId: executionID,
		PlanDigestSha256:        strings.Repeat("a", 64),
		ActionId:                actionID,
		CanonicalParametersJson: []byte(`{"operation":"apply"}`),
		Privilege:               agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs:         time.Now().Add(5 * time.Minute).UnixMilli(),
		TimeoutSeconds:          30,
		ActionSchemaVersion:     "1",
	}
}

func TestGatewayArtifactChildAndApplyUseDistinctAgentLedgerExecutions(t *testing.T) {
	success := func(context.Context, []byte) executionResult {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	}
	accept := func([]byte) error { return nil }
	ledger := privateAgentDirectory(t)
	executor, err := newAgentExecutor(ledger, map[string]agentAction{
		transport.FileCapability: {Validate: accept, Run: success},
		gatewayApplyAction:       {Validate: accept, Run: success},
		gatewayRemoveAction:      {Validate: accept, Run: success},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	ctx := context.Background()

	child := gatewayLedgerOffer("task-one", "gwa-child", transport.FileCapability)
	apply := gatewayLedgerOffer("task-one", "exec-parent", gatewayApplyAction)
	if result, seen, err := executor.execute(ctx, child); err != nil || seen || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("artifact child result=%+v seen=%v err=%v", result, seen, err)
	}
	if result, seen, err := executor.execute(ctx, apply); err != nil || seen || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatalf("apply result=%+v seen=%v err=%v", result, seen, err)
	}

	// The same Agent ledger execution cannot carry a different action: this is
	// exactly why the artifact child must not reuse the parent execution id.
	conflict := proto.Clone(apply).(*agentv1.TaskOffer)
	conflict.ExecutionId = child.ExecutionId
	if _, seen, err := executor.execute(ctx, conflict); !errors.Is(err, errExecutionRejected) || !seen {
		t.Fatalf("conflicting ledger claim seen=%v err=%v, want errExecutionRejected", seen, err)
	}
}
