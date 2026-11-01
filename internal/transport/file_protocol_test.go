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
	for _, input := range []string{strings.Replace(string(raw), `"size":"42"`, `"size":"042"`, 1), strings.Replace(string(raw), `"direction":"write"`, `"direction":"write","direction":"read"`, 1), strings.Replace(string(raw), `"relativePath":"data.bin"`, `"relativePath":"../escape"`, 1), strings.Replace(string(raw), `"conflictPolicy":"skip"`, `"conflictPolicy":"overwrite"`, 1), strings.Replace(string(raw), `"createParents":"false"`, `"createParents":"true"`, 1)} {
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
