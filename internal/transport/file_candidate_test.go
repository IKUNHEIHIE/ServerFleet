package transport_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func TestR6FileCandidateRoundTripsButHasNoDefaultRuntimeGrant(t *testing.T) {
	request := envelope()
	request.Message = &agentv1.Envelope_FileOpenRequest{FileOpenRequest: &agentv1.FileOpenRequest{TaskId: "task", ExecutionId: "exec", TransferId: "file", RelativePath: "data/blob", Direction: "read"}}
	reply := envelope()
	reply.Message = &agentv1.Envelope_FileOpenReply{FileOpenReply: &agentv1.FileOpenReply{TaskId: "task", ExecutionId: "exec", TransferId: "file", ResultCode: "ok"}}
	chunk := envelope()
	chunk.Message = &agentv1.Envelope_FileChunkRequest{FileChunkRequest: &agentv1.FileChunkRequest{TaskId: "task", ExecutionId: "exec", TransferId: "file", Data: []byte("synthetic")}}
	chunkReply := envelope()
	chunkReply.Message = &agentv1.Envelope_FileChunkReply{FileChunkReply: &agentv1.FileChunkReply{TaskId: "task", ExecutionId: "exec", TransferId: "file", Data: []byte("synthetic")}}
	for _, before := range []*agentv1.Envelope{request, reply, chunk, chunkReply} {
		wire, err := proto.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		after := new(agentv1.Envelope)
		if err = proto.Unmarshal(wire, after); err != nil || !proto.Equal(before, after) {
			t.Fatal("file candidate lost its binding or bytes")
		}
		for _, direction := range []transport.Direction{transport.AgentToController, transport.ControllerToAgent} {
			if err = transport.ValidateEnvelope(after, contextFor(direction, transport.Established), limits()); err == nil {
				t.Fatal("schema presence granted a file runtime capability")
			}
		}
	}
}
