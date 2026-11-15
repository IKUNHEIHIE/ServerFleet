package transport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func TestFileTaskParametersMatchStringMapTaskPortAndRejectAmbiguity(t *testing.T) {
	p := FileParameters{TransferID: "transfer", RelativePath: "data.bin", Direction: "write", SHA256: strings.Repeat("a", 64), Size: 42, ConflictPolicy: "skip"}
	raw, _ := json.Marshal(p)
	var taskParameters map[string]string
	if json.Unmarshal(raw, &taskParameters) != nil || taskParameters["size"] != "42" || taskParameters["createParents"] != "false" {
		t.Fatal("incompatible task parameters")
	}
	if got, err := ParseFileParameters(raw); err != nil || got != p {
		t.Fatal(err)
	}
	for _, policy := range []string{"overwrite", "rename"} {
		accepted := strings.Replace(string(raw), `"conflictPolicy":"skip"`, `"conflictPolicy":"`+policy+`"`, 1)
		if got, err := ParseFileParameters([]byte(accepted)); err != nil || got.ConflictPolicy != policy {
			t.Fatal("reviewed conflict policy rejected", policy, err)
		}
	}
	for _, input := range []string{strings.Replace(string(raw), `"size":"42"`, `"size":"042"`, 1), strings.Replace(string(raw), `"direction":"write"`, `"direction":"write","direction":"read"`, 1), strings.Replace(string(raw), `"relativePath":"data.bin"`, `"relativePath":"../escape"`, 1), strings.Replace(string(raw), `"conflictPolicy":"skip"`, `"conflictPolicy":"replace"`, 1), strings.Replace(string(raw), `"createParents":"false"`, `"createParents":"true"`, 1)} {
		if _, err := ParseFileParameters([]byte(input)); err == nil {
			t.Fatal("ambiguous or unsupported task plan admitted")
		}
	}
}
func TestFileFramesRequireExplicitMinorGrantBoundsAndDirection(t *testing.T) {
	l := ValidationLimits{AllowFileFrames: true, MaxMessageBytes: 64 << 10, MaxTextBytes: 256, MaxParametersBytes: 4096, MaxOutputBytes: 1024, MaxMetrics: 8, MaxSyncExecutions: 8, MaxCapabilities: 8, MaxTimeoutSeconds: 60, MaxOfferLifetime: time.Minute, AllowedActions: map[string]string{FileCapability: "1"}}
	ctx := ValidationContext{Direction: ControllerToAgent, Phase: Established, ProtocolMajor: 1, ProtocolMinor: 1, Now: time.Now()}
	r := &agentv1.FileChunkRequest{TaskId: "task", ExecutionId: "execution", TransferId: "transfer", ReadBytes: 16}
	e := &agentv1.Envelope{ProtocolMajor: 1, ProtocolMinor: 1, NodeId: "node", ConnectionId: "connection", MessageId: "message", Sequence: 1, SentAtUnixMs: time.Now().UnixMilli(), Message: &agentv1.Envelope_FileChunkRequest{FileChunkRequest: r}}
	if err := ValidateEnvelope(e, ctx, l); err != nil {
		t.Fatal(err)
	}
	l.AllowFileFrames = false
	if ValidateEnvelope(e, ctx, l) == nil {
		t.Fatal("default file profile opened")
	}
	l.AllowFileFrames = true
	ctx.Direction = AgentToController
	if ValidateEnvelope(e, ctx, l) == nil {
		t.Fatal("reversed file request")
	}
	ctx.Direction = ControllerToAgent
	r.Data = []byte("mixed direction")
	if ValidateEnvelope(e, ctx, l) == nil {
		t.Fatal("read/write mixed payload")
	}
	r.Data = nil
	r.Cancel = true
	if ValidateEnvelope(e, ctx, l) == nil {
		t.Fatal("cancel carried read bytes")
	}
	r.Cancel = false
	r.ReadBytes = FileChunkBytes + 1
	if ValidateEnvelope(e, ctx, l) == nil {
		t.Fatal("unbounded allocation")
	}
}

func TestFileOpFramesRequireExplicitGrantBoundsAndDirection(t *testing.T) {
	l := ValidationLimits{AllowFileFrames: true, MaxMessageBytes: 64 << 10, MaxTextBytes: 256, MaxParametersBytes: 4096, MaxOutputBytes: 1024, MaxMetrics: 8, MaxSyncExecutions: 8, MaxCapabilities: 8, MaxTimeoutSeconds: 60, MaxOfferLifetime: time.Minute, AllowedActions: map[string]string{FileCapability: "1"}}
	ctx := ValidationContext{Direction: ControllerToAgent, Phase: Established, ProtocolMajor: 1, ProtocolMinor: 1, Now: time.Now()}
	env := func(m *agentv1.Envelope) *agentv1.Envelope {
		m.ProtocolMajor, m.ProtocolMinor, m.NodeId, m.ConnectionId, m.MessageId, m.Sequence, m.SentAtUnixMs = 1, 1, "node", "connection", "message", 1, time.Now().UnixMilli()
		return m
	}
	req := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-1", NodeId: "node", Op: "list", Path: "data"}}})
	if err := ValidateEnvelope(req, ctx, l); err != nil {
		t.Fatal(err)
	}
	root := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-root", NodeId: "node", Op: "list", Path: "/"}}})
	if err := ValidateEnvelope(root, ctx, l); err != nil {
		t.Fatal("operator root path \"/\" must be accepted", err)
	}
	badPath := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-esc", NodeId: "node", Op: "list", Path: "/../etc"}}})
	if ValidateEnvelope(badPath, ctx, l) == nil {
		t.Fatal("escape path must be rejected")
	}
	ren := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-2", NodeId: "node", Op: "rename", Path: "data/a", NewName: "b"}}})
	if err := ValidateEnvelope(ren, ctx, l); err != nil {
		t.Fatal(err)
	}
	badOp := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-3", NodeId: "node", Op: "chmod", Path: "data"}}})
	if ValidateEnvelope(badOp, ctx, l) == nil {
		t.Fatal("unknown op accepted")
	}
	badName := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "req-4", NodeId: "node", Op: "rename", Path: "data/a", NewName: "../escape"}}})
	if ValidateEnvelope(badName, ctx, l) == nil {
		t.Fatal("unsafe rename target accepted")
	}
	rep := env(&agentv1.Envelope{Message: &agentv1.Envelope_FileOpReply{FileOpReply: &agentv1.FileOpReply{RequestId: "req-1", ResultCode: "ok", Path: "data", Entries: []*agentv1.FileEntry{{Name: "a", Type: "file", Size: 3, ModifiedUnixMs: 1}}}}})
	if err := ValidateEnvelope(rep, ValidationContext{Direction: AgentToController, Phase: Established, ProtocolMajor: 1, ProtocolMinor: 1, Now: time.Now()}, l); err != nil {
		t.Fatal(err)
	}
	l.AllowedActions = map[string]string{}
	if ValidateEnvelope(req, ctx, l) == nil {
		t.Fatal("schema presence granted a file op capability")
	}
}
