package transport

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

// Direction is supplied by the caller, never inferred from peer-controlled IDs.
type Direction uint8

const (
	AgentToController Direction = iota + 1
	ControllerToAgent
)

// HandshakePhase describes caller-owned context; validation never advances it.
type HandshakePhase uint8

const (
	AwaitAgentHello HandshakePhase = iota + 1
	AwaitControllerHello
	Established
)

// ValidationContext contains explicit offline inputs, not authentication proofs.
// ProtocolMajor/Minor are the version the caller expects for this message.
type ValidationContext struct {
	Direction     Direction
	Phase         HandshakePhase
	ProtocolMajor uint32
	ProtocolMinor uint32
	Now           time.Time
}

// ValidationLimits has no production defaults. Positive limits are required.
// AllowedActions maps an action ID to its exact schema version; empty denies all
// offers. It is only a local structural allowlist, not resource authorization.
// The caller must not mutate this map or the message during validation.
type ValidationLimits struct {
	MaxMessageBytes    int
	MaxTextBytes       int
	MaxParametersBytes int
	MaxOutputBytes     int
	MaxMetrics         int
	MaxSyncExecutions  int
	MaxCapabilities    int
	MaxTimeoutSeconds  uint32
	MaxOfferLifetime   time.Duration
	AllowedActions     map[string]string
}

type ValidationCode string

const (
	InvalidContext   ValidationCode = "INVALID_CONTEXT"
	MissingPayload   ValidationCode = "MISSING_PAYLOAD"
	InvalidProtocol  ValidationCode = "INVALID_PROTOCOL"
	InvalidDirection ValidationCode = "INVALID_DIRECTION"
	InvalidHandshake ValidationCode = "INVALID_HANDSHAKE"
	MissingID        ValidationCode = "MISSING_ID"
	InvalidField     ValidationCode = "INVALID_FIELD"
	UnknownEnum      ValidationCode = "UNKNOWN_ENUM"
	LimitExceeded    ValidationCode = "LIMIT_EXCEEDED"
	ExpiredOffer     ValidationCode = "EXPIRED_OFFER"
	ActionNotAllowed ValidationCode = "ACTION_NOT_ALLOWED"
)

// ValidationError deliberately contains no peer-supplied values or raw payloads.
// Callers can use errors.As to inspect Code and Field without parsing Error().
type ValidationError struct {
	Code  ValidationCode
	Field string
}

func (e *ValidationError) Error() string {
	return "protobuf semantics: " + string(e.Code) + " (" + e.Field + ")"
}

func invalid(code ValidationCode, field string) error {
	return &ValidationError{Code: code, Field: field}
}

// ValidateEnvelope checks an already decoded Envelope without I/O, mutation,
// hidden clocks, authentication, scheduling, execution or persistence.
// A nil error is NOT permission to execute. See SEMANTIC-VALIDATION.md.
func ValidateEnvelope(e *agentv1.Envelope, ctx ValidationContext, limits ValidationLimits) error {
	if ctx.Direction != AgentToController && ctx.Direction != ControllerToAgent {
		return invalid(InvalidContext, "context.direction")
	}
	if ctx.Phase != AwaitAgentHello && ctx.Phase != AwaitControllerHello && ctx.Phase != Established {
		return invalid(InvalidContext, "context.phase")
	}
	if ctx.ProtocolMajor == 0 || ctx.Now.IsZero() || ctx.Now.UnixMilli() <= 0 {
		return invalid(InvalidContext, "context.version_or_time")
	}
	if limits.MaxMessageBytes <= 0 || limits.MaxTextBytes <= 0 || limits.MaxParametersBytes <= 0 ||
		limits.MaxOutputBytes <= 0 || limits.MaxMetrics <= 0 || limits.MaxSyncExecutions <= 0 ||
		limits.MaxCapabilities <= 0 || limits.MaxTimeoutSeconds == 0 || limits.MaxOfferLifetime.Milliseconds() <= 0 {
		return invalid(InvalidContext, "limits")
	}
	if e == nil {
		return invalid(MissingPayload, "envelope.message")
	}
	payload, direction, phase := knownPayload(e)
	if payload == nil || !payload.ProtoReflect().IsValid() {
		return invalid(MissingPayload, "envelope.message")
	}
	if direction != 0 && direction != ctx.Direction {
		return invalid(InvalidDirection, "envelope.message")
	}
	if phase != 0 && phase != ctx.Phase || phase == 0 && ctx.Phase == AwaitAgentHello {
		return invalid(InvalidHandshake, "envelope.message")
	}
	if e.ProtocolMajor != ctx.ProtocolMajor || e.ProtocolMinor != ctx.ProtocolMinor {
		return invalid(InvalidProtocol, "envelope.protocol_version")
	}
	// This bounds re-encoded size only, NOT the original wire frame/allocation.
	if proto.Size(e) > limits.MaxMessageBytes {
		return invalid(LimitExceeded, "envelope.encoded_size")
	}
	v := semanticValidator{ctx: ctx, limits: limits}
	for _, id := range []struct{ field, value string }{
		{"envelope.message_id", e.MessageId}, {"envelope.connection_id", e.ConnectionId}, {"envelope.node_id", e.NodeId},
	} {
		if err := v.text(id.field, id.value, true); err != nil {
			return err
		}
	}
	if e.Sequence == 0 || e.SentAtUnixMs <= 0 {
		return invalid(InvalidField, "envelope.sequence_or_sent_at")
	}
	return v.payload(payload)
}

// Explicit cases also handle typed-nil oneof wrappers (direct Go callers can
// construct them). Unknown future oneof tags decode without a known payload.
func knownPayload(e *agentv1.Envelope) (proto.Message, Direction, HandshakePhase) {
	switch m := e.Message.(type) {
	case *agentv1.Envelope_AgentHello:
		if m != nil {
			return m.AgentHello, AgentToController, AwaitAgentHello
		}
	case *agentv1.Envelope_ControllerHello:
		if m != nil {
			return m.ControllerHello, ControllerToAgent, AwaitControllerHello
		}
	case *agentv1.Envelope_Heartbeat:
		if m != nil {
			return m.Heartbeat, AgentToController, Established
		}
	case *agentv1.Envelope_Metrics:
		if m != nil {
			return m.Metrics, AgentToController, Established
		}
	case *agentv1.Envelope_TaskOffer:
		if m != nil {
			return m.TaskOffer, ControllerToAgent, Established
		}
	case *agentv1.Envelope_TaskAck:
		if m != nil {
			return m.TaskAck, AgentToController, Established
		}
	case *agentv1.Envelope_TaskStarted:
		if m != nil {
			return m.TaskStarted, AgentToController, Established
		}
	case *agentv1.Envelope_TaskOutput:
		if m != nil {
			return m.TaskOutput, AgentToController, Established
		}
	case *agentv1.Envelope_TaskFinished:
		if m != nil {
			return m.TaskFinished, AgentToController, Established
		}
	case *agentv1.Envelope_TaskCancel:
		if m != nil {
			return m.TaskCancel, ControllerToAgent, Established
		}
	case *agentv1.Envelope_StateSyncRequest:
		if m != nil {
			return m.StateSyncRequest, ControllerToAgent, Established
		}
	case *agentv1.Envelope_StateSyncReply:
		if m != nil {
			return m.StateSyncReply, AgentToController, Established
		}
	case *agentv1.Envelope_ControlError:
		if m != nil {
			return m.ControlError, 0, 0
		}
	}
	return nil, 0, 0
}

type semanticValidator struct {
	ctx    ValidationContext
	limits ValidationLimits
}

func (v semanticValidator) text(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return invalid(MissingID, field)
	}
	if !utf8.ValidString(value) {
		return invalid(InvalidField, field)
	}
	if len(value) > v.limits.MaxTextBytes {
		return invalid(LimitExceeded, field)
	}
	return nil
}

func (v semanticValidator) execution(taskID, executionID string) error {
	if err := v.text("execution.task_id", taskID, true); err != nil {
		return err
	}
	return v.text("execution.execution_id", executionID, true)
}

func (v semanticValidator) capabilities(ids []string) error {
	if len(ids) > v.limits.MaxCapabilities {
		return invalid(LimitExceeded, "hello.capability_ids")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if err := v.text("hello.capability_ids", id, true); err != nil {
			return err
		}
		if seen[id] {
			return invalid(InvalidField, "hello.capability_ids")
		}
		seen[id] = true
	}
	return nil
}

func validResult(result agentv1.ExecutionResult, allowUnspecified bool) bool {
	switch result {
	case agentv1.ExecutionResult_EXECUTION_RESULT_UNSPECIFIED:
		return allowUnspecified // StateSync snapshots may describe unfinished work.
	case agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, agentv1.ExecutionResult_EXECUTION_RESULT_FAILED,
		agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED, agentv1.ExecutionResult_EXECUTION_RESULT_TIMED_OUT,
		agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN:
		return true
	default:
		return false
	}
}

func (v semanticValidator) offer(m *agentv1.TaskOffer) error {
	if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
		return err
	}
	for _, id := range []struct{ field, value string }{
		{"offer.plan_digest_sha256", m.PlanDigestSha256}, {"offer.action_id", m.ActionId}, {"offer.action_schema_version", m.ActionSchemaVersion},
	} {
		if err := v.text(id.field, id.value, true); err != nil {
			return err
		}
	}
	if len(m.PlanDigestSha256) != 64 {
		return invalid(InvalidField, "offer.plan_digest_sha256")
	}
	if _, err := hex.DecodeString(m.PlanDigestSha256); err != nil {
		return invalid(InvalidField, "offer.plan_digest_sha256")
	}
	if m.Privilege != agentv1.Privilege_PRIVILEGE_NORMAL && m.Privilege != agentv1.Privilege_PRIVILEGE_ROOT {
		return invalid(UnknownEnum, "offer.privilege")
	}
	if version, ok := v.limits.AllowedActions[m.ActionId]; !ok || version != m.ActionSchemaVersion {
		return invalid(ActionNotAllowed, "offer.action_id_or_schema_version")
	}
	if len(m.CanonicalParametersJson) > v.limits.MaxParametersBytes {
		return invalid(LimitExceeded, "offer.parameters")
	}
	parameters := strings.TrimSpace(string(m.CanonicalParametersJson))
	if !utf8.Valid(m.CanonicalParametersJson) || !json.Valid(m.CanonicalParametersJson) || len(parameters) == 0 || parameters[0] != '{' {
		return invalid(InvalidField, "offer.parameters")
	}
	if m.ExpiresAtUnixMs <= v.ctx.Now.UnixMilli() {
		return invalid(ExpiredOffer, "offer.expires_at")
	}
	if m.ExpiresAtUnixMs-v.ctx.Now.UnixMilli() > v.limits.MaxOfferLifetime.Milliseconds() {
		return invalid(LimitExceeded, "offer.expires_at")
	}
	if m.TimeoutSeconds == 0 {
		return invalid(InvalidField, "offer.timeout_seconds")
	}
	if m.TimeoutSeconds > v.limits.MaxTimeoutSeconds {
		return invalid(LimitExceeded, "offer.timeout_seconds")
	}
	return v.text("offer.plugin_id", m.PluginId, false)
}

func (v semanticValidator) payload(payload proto.Message) error {
	switch m := payload.(type) {
	case *agentv1.AgentHello:
		for _, id := range []struct{ field, value string }{
			{"agent_hello.agent_version", m.AgentVersion}, {"agent_hello.platform", m.Platform},
			{"agent_hello.architecture", m.Architecture}, {"agent_hello.boot_id", m.BootId},
		} {
			if err := v.text(id.field, id.value, true); err != nil {
				return err
			}
		}
		if m.MinProtocolMajor == 0 || m.MinProtocolMajor > m.MaxProtocolMajor ||
			v.ctx.ProtocolMajor < m.MinProtocolMajor || v.ctx.ProtocolMajor > m.MaxProtocolMajor {
			return invalid(InvalidProtocol, "agent_hello.major_range")
		}
		return v.capabilities(m.CapabilityIds)
	case *agentv1.ControllerHello:
		if err := v.text("controller_hello.reason_code", m.ReasonCode, !m.Accepted); err != nil {
			return err
		}
		if !m.Accepted {
			return nil
		} // Valid rejection, never an established session.
		if m.SelectedProtocolMajor != v.ctx.ProtocolMajor || m.SelectedProtocolMinor != v.ctx.ProtocolMinor {
			return invalid(InvalidProtocol, "controller_hello.selected_version")
		}
		if m.HeartbeatIntervalSeconds == 0 || m.MaxFrameBytes == 0 || m.ServerTimeUnixMs <= 0 {
			return invalid(InvalidField, "controller_hello.limits_or_time")
		}
		if uint64(m.MaxFrameBytes) > uint64(v.limits.MaxMessageBytes) {
			return invalid(LimitExceeded, "controller_hello.max_frame_bytes")
		}
		return v.capabilities(m.GrantedCapabilityIds)
	case *agentv1.Heartbeat:
		return v.text("heartbeat.connectivity_status", m.ConnectivityStatus, true)
	case *agentv1.MetricsBatch:
		if err := v.text("metrics.batch_id", m.BatchId, true); err != nil {
			return err
		}
		if len(m.Points) == 0 {
			return invalid(InvalidField, "metrics.points")
		}
		if len(m.Points) > v.limits.MaxMetrics {
			return invalid(LimitExceeded, "metrics.points")
		}
		for _, point := range m.Points {
			if point == nil {
				return invalid(InvalidField, "metrics.points")
			}
			if err := v.text("metrics.point.name", point.Name, true); err != nil {
				return err
			}
			if err := v.text("metrics.point.unit", point.Unit, true); err != nil {
				return err
			}
			if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) || point.ObservedAtUnixMs <= 0 {
				return invalid(InvalidField, "metrics.point.value_or_time")
			}
		}
		return nil
	case *agentv1.TaskOffer:
		return v.offer(m)
	case *agentv1.TaskAck:
		if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
			return err
		}
		return v.text("ack.reason_code", m.ReasonCode, !m.Accepted)
	case *agentv1.TaskStarted:
		if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
			return err
		}
		if m.StartedAtUnixMs <= 0 {
			return invalid(InvalidField, "started.started_at")
		}
		return nil
	case *agentv1.TaskOutput:
		if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
			return err
		}
		if m.Stream != agentv1.TaskOutput_STREAM_STDOUT && m.Stream != agentv1.TaskOutput_STREAM_STDERR {
			return invalid(UnknownEnum, "output.stream")
		}
		if m.StreamSequence == 0 || len(m.Chunk) == 0 && !m.Truncated {
			return invalid(InvalidField, "output.sequence_or_chunk")
		}
		if len(m.Chunk) > v.limits.MaxOutputBytes {
			return invalid(LimitExceeded, "output.chunk")
		}
		return nil
	case *agentv1.TaskFinished:
		if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
			return err
		}
		if !validResult(m.Result, false) {
			return invalid(UnknownEnum, "finished.result")
		}
		if m.FinishedAtUnixMs <= 0 {
			return invalid(InvalidField, "finished.finished_at")
		}
		return v.text("finished.error_code", m.ErrorCode, false)
	case *agentv1.TaskCancel:
		if err := v.execution(m.TaskId, m.ExecutionId); err != nil {
			return err
		}
		if m.RequestedAtUnixMs <= 0 {
			return invalid(InvalidField, "cancel.requested_at")
		}
		return v.text("cancel.reason_code", m.ReasonCode, true)
	case *agentv1.StateSyncRequest:
		if err := v.text("sync.sync_id", m.SyncId, true); err != nil {
			return err
		}
		if m.MaxExecutions == 0 || m.SinceUnixMs < 0 {
			return invalid(InvalidField, "sync.bounds")
		}
		if uint64(m.MaxExecutions) > uint64(v.limits.MaxSyncExecutions) {
			return invalid(LimitExceeded, "sync.max_executions")
		}
		return nil
	case *agentv1.StateSyncReply:
		if err := v.text("sync.sync_id", m.SyncId, true); err != nil {
			return err
		}
		if len(m.Executions) > v.limits.MaxSyncExecutions {
			return invalid(LimitExceeded, "sync.executions")
		}
		seen := make(map[string]bool, len(m.Executions))
		for _, execution := range m.Executions {
			if execution == nil {
				return invalid(InvalidField, "sync.executions")
			}
			if err := v.execution(execution.TaskId, execution.ExecutionId); err != nil {
				return err
			}
			if seen[execution.ExecutionId] {
				return invalid(InvalidField, "sync.duplicate_execution_id")
			}
			seen[execution.ExecutionId] = true
			if !validResult(execution.Result, true) {
				return invalid(UnknownEnum, "sync.result")
			}
			if execution.UpdatedAtUnixMs <= 0 {
				return invalid(InvalidField, "sync.updated_at")
			}
			if execution.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED && !execution.Started {
				return invalid(InvalidField, "sync.succeeded_without_start")
			}
		}
		return nil
	case *agentv1.ControlError:
		if err := v.text("control_error.code", m.Code, true); err != nil {
			return err
		}
		if err := v.text("control_error.message", m.Message, false); err != nil {
			return err
		}
		return v.text("control_error.related_message_id", m.RelatedMessageId, false)
	default:
		return invalid(MissingPayload, "envelope.message")
	}
}
