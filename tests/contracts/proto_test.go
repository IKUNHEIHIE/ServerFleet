package contracts

import (
	"testing"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	before := &agentv1.Envelope{ProtocolMajor: 1, MessageId: "synthetic-message", NodeId: "synthetic-node",
		Message: &agentv1.Envelope_TaskOffer{TaskOffer: &agentv1.TaskOffer{TaskId: "synthetic-task", ExecutionId: "synthetic-execution", Privilege: agentv1.Privilege_PRIVILEGE_NORMAL}}}
	wire, err := proto.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	after := new(agentv1.Envelope)
	if err := proto.Unmarshal(wire, after); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(before, after) {
		t.Fatalf("roundtrip mismatch: %v", after)
	}
	if after.GetTaskOffer().GetExecutionId() != "synthetic-execution" {
		t.Fatal("oneof payload lost")
	}
}

func TestTruncatedEnvelopeRejected(t *testing.T) {
	if err := proto.Unmarshal([]byte{0x0a, 0xff}, new(agentv1.Envelope)); err == nil {
		t.Fatal("invalid protobuf frame accepted")
	}
}
