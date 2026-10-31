package transport_test

import (
	"strings"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

var fixedNow = time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

func limits() transport.ValidationLimits {
	return transport.ValidationLimits{
		MaxMessageBytes: 16384, MaxTextBytes: 256, MaxParametersBytes: 512, MaxOutputBytes: 1024,
		MaxMetrics: 4, MaxSyncExecutions: 4, MaxCapabilities: 4, MaxTimeoutSeconds: 60,
		MaxOfferLifetime: 15 * time.Minute, AllowedActions: map[string]string{"synthetic.read": "v1"},
	}
}

func contextFor(direction transport.Direction, phase transport.HandshakePhase) transport.ValidationContext {
	return transport.ValidationContext{Direction: direction, Phase: phase, ProtocolMajor: 1, ProtocolMinor: 2, Now: fixedNow}
}

func envelope() *agentv1.Envelope {
	return &agentv1.Envelope{ProtocolMajor: 1, ProtocolMinor: 2, MessageId: "synthetic-message", ConnectionId: "synthetic-connection",
		NodeId: "synthetic-node", Sequence: 1, SentAtUnixMs: fixedNow.UnixMilli()}
}

type example struct {
	name string
	e    *agentv1.Envelope
	ctx  transport.ValidationContext
}

func examples() []example {
	frames := make([]*agentv1.Envelope, 13)
	for i := range frames {
		frames[i] = envelope()
	}
	ms := fixedNow.UnixMilli()
	frames[0].Message = &agentv1.Envelope_AgentHello{AgentHello: &agentv1.AgentHello{AgentVersion: "synthetic-v1", Platform: "linux", Architecture: "amd64", CapabilityIds: []string{"synthetic.read"}, MinProtocolMajor: 1, MaxProtocolMajor: 1, BootId: "synthetic-boot"}}
	frames[1].Message = &agentv1.Envelope_ControllerHello{ControllerHello: &agentv1.ControllerHello{Accepted: true, SelectedProtocolMajor: 1, SelectedProtocolMinor: 2, GrantedCapabilityIds: []string{"synthetic.read"}, HeartbeatIntervalSeconds: 10, MaxFrameBytes: 16384, ServerTimeUnixMs: ms}}
	frames[2].Message = &agentv1.Envelope_Heartbeat{Heartbeat: &agentv1.Heartbeat{ConnectivityStatus: "online"}}
	frames[3].Message = &agentv1.Envelope_Metrics{Metrics: &agentv1.MetricsBatch{BatchId: "synthetic-batch", Points: []*agentv1.MetricPoint{{Name: "synthetic.cpu", Value: 0, Unit: "percent", ObservedAtUnixMs: ms}}}}
	frames[4].Message = &agentv1.Envelope_TaskOffer{TaskOffer: &agentv1.TaskOffer{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", PlanDigestSha256: strings.Repeat("a", 64), ActionId: "synthetic.read", CanonicalParametersJson: []byte(`{"path":"synthetic"}`), Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, ExpiresAtUnixMs: ms + 900000, TimeoutSeconds: 60, ActionSchemaVersion: "v1"}}
	frames[5].Message = &agentv1.Envelope_TaskAck{TaskAck: &agentv1.TaskAck{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", Accepted: true}}
	frames[6].Message = &agentv1.Envelope_TaskStarted{TaskStarted: &agentv1.TaskStarted{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", StartedAtUnixMs: ms}}
	frames[7].Message = &agentv1.Envelope_TaskOutput{TaskOutput: &agentv1.TaskOutput{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", StreamSequence: 1, Stream: agentv1.TaskOutput_STREAM_STDOUT, Chunk: []byte("synthetic-output")}}
	frames[8].Message = &agentv1.Envelope_TaskFinished{TaskFinished: &agentv1.TaskFinished{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, FinishedAtUnixMs: ms}}
	frames[9].Message = &agentv1.Envelope_TaskCancel{TaskCancel: &agentv1.TaskCancel{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", ReasonCode: "USER_REQUEST", RequestedAtUnixMs: ms}}
	frames[10].Message = &agentv1.Envelope_StateSyncRequest{StateSyncRequest: &agentv1.StateSyncRequest{SyncId: "synthetic-sync", MaxExecutions: 4}}
	frames[11].Message = &agentv1.Envelope_StateSyncReply{StateSyncReply: &agentv1.StateSyncReply{SyncId: "synthetic-sync", Executions: []*agentv1.ExecutionSnapshot{{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Started: true, UpdatedAtUnixMs: ms}}}}
	frames[12].Message = &agentv1.Envelope_ControlError{ControlError: &agentv1.ControlError{Code: "SYNTHETIC_FAILURE", Message: "redacted"}}
	return []example{
		{"agent_hello", frames[0], contextFor(transport.AgentToController, transport.AwaitAgentHello)},
		{"controller_hello", frames[1], contextFor(transport.ControllerToAgent, transport.AwaitControllerHello)},
		{"heartbeat", frames[2], contextFor(transport.AgentToController, transport.Established)},
		{"metrics", frames[3], contextFor(transport.AgentToController, transport.Established)},
		{"task_offer", frames[4], contextFor(transport.ControllerToAgent, transport.Established)},
		{"task_ack", frames[5], contextFor(transport.AgentToController, transport.Established)},
		{"task_started", frames[6], contextFor(transport.AgentToController, transport.Established)},
		{"task_output", frames[7], contextFor(transport.AgentToController, transport.Established)},
		{"task_finished", frames[8], contextFor(transport.AgentToController, transport.Established)},
		{"task_cancel", frames[9], contextFor(transport.ControllerToAgent, transport.Established)},
		{"sync_request", frames[10], contextFor(transport.ControllerToAgent, transport.Established)},
		{"sync_reply", frames[11], contextFor(transport.AgentToController, transport.Established)},
		{"control_error", frames[12], contextFor(transport.ControllerToAgent, transport.Established)},
	}
}
