package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
)

// GatewayArtifactObserver consumes the child file execution lifecycle. It the
// artifact upload child from Ack/Started/Finished and durably records its own
// state; it never records a parent task result, so a successful upload cannot
// finish the publication before the Nginx action runs.
type GatewayArtifactObserver struct {
	db    *storage.DB
	stage GatewayArtifactStageChecker
}

func NewGatewayArtifactObserver(db *storage.DB, stage GatewayArtifactStageChecker) (*GatewayArtifactObserver, error) {
	if db == nil || stage == nil {
		return nil, files.ErrChannelUnavailable
	}
	return &GatewayArtifactObserver{db: db, stage: stage}, nil
}

// Observe returns owned=false for any event that is not a known gateway child
// execution; the caller must continue with the CX file gate and task routing.
func (o *GatewayArtifactObserver) Observe(ctx context.Context, observation transport.LinkObservation, envelope *agentv1.Envelope) (bool, error) {
	if o == nil || o.db == nil || o.stage == nil || envelope == nil {
		return false, files.ErrChannelUnavailable
	}
	taskID, executionID, kind, result := gatewayChildEvent(envelope)
	if executionID == "" {
		return false, nil
	}
	binding, err := loadGatewayArtifactBinding(ctx, o.db, executionID)
	if errors.Is(err, errGatewayArtifactBindingNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if binding.TaskID != taskID || binding.NodeID != observation.Identity.NodeID {
		return true, files.ErrTaskBindingRequired
	}
	if err := o.stage.CheckArtifactChild(ctx, binding.TaskID, binding.NodeID, binding.PlanID,
		binding.TargetRevision, binding.GatewayID, binding.PlanDigest, binding.AbsolutePath,
		binding.SitePath, binding.SHA256); err != nil {
		return true, err
	}
	state, outcome, err := gatewayChildTransition(binding.State, binding.Result, kind, envelope, result)
	if err != nil {
		return true, err
	}
	if state == binding.State && outcome == binding.Result {
		return true, nil
	}
	return true, o.update(ctx, executionID, state, outcome)
}

func gatewayChildEvent(envelope *agentv1.Envelope) (taskID, executionID, kind string, result agentv1.ExecutionResult) {
	switch {
	case envelope.GetTaskAck() != nil:
		ack := envelope.GetTaskAck()
		return ack.GetTaskId(), ack.GetExecutionId(), "ack", agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN
	case envelope.GetTaskStarted() != nil:
		started := envelope.GetTaskStarted()
		return started.GetTaskId(), started.GetExecutionId(), "started", agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN
	case envelope.GetTaskFinished() != nil:
		finished := envelope.GetTaskFinished()
		return finished.GetTaskId(), finished.GetExecutionId(), "finished", finished.GetResult()
	default:
		return "", "", "", agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN
	}
}

// gatewayChildTransition enforces a monotonic child state machine. A duplicate
// event is idempotent, a conflicting terminal event is rejected, and a finished
// event that arrives before acceptance records UNKNOWN rather than success.
func gatewayChildTransition(state, outcome, kind string, envelope *agentv1.Envelope, result agentv1.ExecutionResult) (string, string, error) {
	switch kind {
	case "ack":
		ack := envelope.GetTaskAck()
		if !ack.GetAccepted() {
			if state == "finished" && outcome == agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN.String() {
				return state, outcome, nil
			}
			return "rejected", "rejected", nil
		}
		switch state {
		case "", "reserved", "accepted":
			return "accepted", outcome, nil
		case "started", "finished":
			return state, outcome, nil
		default:
			return "", "", files.ErrTransferExecution
		}
	case "started":
		switch state {
		case "", "reserved", "accepted", "started":
			return "started", outcome, nil
		case "finished":
			return state, outcome, nil
		default:
			return "", "", files.ErrTransferExecution
		}
	case "finished":
		nextResult := result.String()
		switch state {
		case "started", "accepted":
			return "finished", nextResult, nil
		case "finished":
			if outcome != nextResult && outcome != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN.String() {
				return "", "", files.ErrTransferIdentity
			}
			return state, outcome, nil
		default:
			return "finished", agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN.String(), nil
		}
	}
	return "", "", files.ErrTransferExecution
}

func (o *GatewayArtifactObserver) update(ctx context.Context, executionID, state, result string) error {
	_, err := o.db.Exec(ctx, `UPDATE gateway_artifact_bindings SET state=?, result=?, updated_at=? WHERE execution_id=?`,
		state, result, time.Now().UTC().Format(time.RFC3339), executionID)
	if err != nil {
		return fmt.Errorf("gateway: update artifact child state: %w", err)
	}
	return nil
}
