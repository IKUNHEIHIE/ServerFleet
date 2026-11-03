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

// GatewayArtifactObserver consumes the child file execution lifecycle. It claims
// the artifact upload child from Ack/Started/Finished and durably records its own
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
// Every transition is a compare-and-set against the persisted (state,result).
// A lost race reloads and re-evaluates instead of writing a stale snapshot, so
// multiple observers, reconnects and duplicate frames cannot move a terminal
// record backwards.
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
	// The stage check reads immutable plan/parameter fields, so it runs once per
	// frame; only the mutable (state,result) snapshot is reloaded on a lost CAS.
	if err := o.stage.CheckArtifactChild(ctx, binding.TaskID, binding.NodeID, binding.PlanID,
		binding.TargetRevision, binding.GatewayID, binding.PlanDigest, binding.AbsolutePath,
		binding.SitePath, binding.SHA256); err != nil {
		return true, err
	}
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			reloaded, loadErr := loadGatewayArtifactBinding(ctx, o.db, executionID)
			if errors.Is(loadErr, errGatewayArtifactBindingNotFound) {
				return false, nil
			}
			if loadErr != nil {
				return true, loadErr
			}
			binding = reloaded
		}
		state, outcome, transitionErr := gatewayChildTransition(binding.State, binding.Result, kind, envelope, result)
		if transitionErr != nil {
			return true, transitionErr
		}
		if state == binding.State && outcome == binding.Result {
			return true, nil
		}
		swapped, casErr := o.compareAndSet(ctx, executionID, binding.State, binding.Result, state, outcome)
		if casErr != nil {
			return true, casErr
		}
		if swapped {
			return true, nil
		}
	}
	// A competing observer kept winning. Re-read the durable record: a terminal
	// or closed state already reflects the strongest event, so this frame is a
	// no-op rather than a reason to replay an effect.
	if _, err := loadGatewayArtifactBinding(ctx, o.db, executionID); err != nil {
		return true, err
	}
	return true, nil
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

// gatewayChildTransition enforces a monotonic child state machine. Terminal and
// rejected states are immutable: a late, duplicate or conflicting event cannot
// rewrite them. UNKNOWN is terminal too and is never upgraded to success.
func gatewayChildTransition(state, outcome, kind string, envelope *agentv1.Envelope, result agentv1.ExecutionResult) (string, string, error) {
	switch kind {
	case "ack":
		ack := envelope.GetTaskAck()
		if state == "finished" || state == "rejected" {
			return state, outcome, nil
		}
		if !ack.GetAccepted() {
			return "rejected", "rejected", nil
		}
		switch state {
		case "", "reserved", "accepted":
			return "accepted", outcome, nil
		case "started":
			return state, outcome, nil
		default:
			return "", "", files.ErrTransferExecution
		}
	case "started":
		switch state {
		case "finished", "rejected":
			return state, outcome, nil
		case "", "reserved", "accepted", "started":
			return "started", outcome, nil
		default:
			return "", "", files.ErrTransferExecution
		}
	case "finished":
		nextResult := result.String()
		switch state {
		case "rejected":
			return state, outcome, nil
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

// compareAndSet atomically advances the durable child record only when it still
// holds the exact state the transition was computed from.
func (o *GatewayArtifactObserver) compareAndSet(ctx context.Context, executionID, fromState, fromResult, toState, toResult string) (bool, error) {
	updated, err := o.db.Exec(ctx,
		`UPDATE gateway_artifact_bindings SET state=?, result=?, updated_at=? WHERE execution_id=? AND state=? AND result=?`,
		toState, toResult, time.Now().UTC().Format(time.RFC3339), executionID, fromState, fromResult)
	if err != nil {
		return false, fmt.Errorf("gateway: update artifact child state: %w", err)
	}
	rows, err := updated.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("gateway: inspect artifact child update: %w", err)
	}
	return rows == 1, nil
}
