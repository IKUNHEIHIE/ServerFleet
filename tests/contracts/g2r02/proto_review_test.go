package g2r02

import (
	"testing"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func TestAllEnvelopeVariantsRoundTrip(t *testing.T) {
	d := (&agentv1.Envelope{}).ProtoReflect().Descriptor()
	oneof := d.Oneofs().ByName("message")
	if oneof == nil || oneof.Fields().Len() != 13 {
		t.Fatal("reviewed 13 Envelope variants changed")
	}
	if d.ParentFile().Services().Len() != 0 {
		t.Fatal("WSS contract must not introduce a gRPC service")
	}
	for i := 0; i < oneof.Fields().Len(); i++ {
		field := oneof.Fields().Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			before := &agentv1.Envelope{ProtocolMajor: 1, MessageId: "synthetic-message", NodeId: "synthetic-node"}
			before.ProtoReflect().Mutable(field)
			wire, err := proto.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			after := new(agentv1.Envelope)
			if err := proto.Unmarshal(wire, after); err != nil || !proto.Equal(before, after) {
				t.Fatalf("variant roundtrip failed: %v", err)
			}
		})
	}
}

// This test proves a limitation: successful Protobuf decode is not authorization
// or semantic validation. A future transport gate must reject these for execution.
func TestProtoDecoderAllowsUnsafeSemanticDefaults(t *testing.T) {
	for _, value := range []agentv1.Privilege{agentv1.Privilege_PRIVILEGE_UNSPECIFIED, agentv1.Privilege(999)} {
		before := &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: &agentv1.TaskOffer{Privilege: value}}}
		wire, err := proto.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		after := new(agentv1.Envelope)
		if err := proto.Unmarshal(wire, after); err != nil {
			t.Fatal(err)
		}
		if after.GetProtocolMajor() != 0 || after.GetTaskOffer().GetExecutionId() != "" || after.GetTaskOffer().GetPrivilege() != value {
			t.Fatal("decoder behavior changed; re-review semantic gate")
		}
	}
	if err := proto.Unmarshal(nil, new(agentv1.Envelope)); err != nil {
		t.Fatal("empty frame is syntactically valid protobuf; transport must reject missing message")
	}
}

func TestMalformedWireRejected(t *testing.T) {
	for _, wire := range [][]byte{{0x0a, 0xff}, {0xc2, 0x01, 0x7f}} {
		if err := proto.Unmarshal(wire, new(agentv1.Envelope)); err == nil {
			t.Fatal("truncated payload accepted")
		}
	}
}
