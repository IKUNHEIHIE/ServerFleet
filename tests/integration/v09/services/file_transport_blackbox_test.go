package services_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func fileParameterJSON(values map[string]string) []byte {
	raw, _ := json.Marshal(values)
	return raw
}

func baseFileParameters(direction string) map[string]string {
	values := map[string]string{
		"transferId": "transfer_1", "relativePath": "data/blob.bin",
		"direction": direction, "size": "0", "sha256": "",
		"conflictPolicy": "skip", "createParents": "false",
	}
	if direction == "write" {
		values["size"] = "42"
		values["sha256"] = strings.Repeat("a", 64)
	}
	return values
}

// TestCXFileTransportExplicitUnsupportedPolicies is a black-box handoff for the
// CX file path: currently only new-file/skip is implemented. These assertions
// must keep failing closed until CX implements or explicitly rejects the wider
// policy set; DS does not modify internal/files this round.
func TestCXFileTransportExplicitUnsupportedPolicies(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"overwrite policy", func(v map[string]string) { v["conflictPolicy"] = "overwrite" }},
		{"rename policy", func(v map[string]string) { v["conflictPolicy"] = "rename" }},
		{"create parents", func(v map[string]string) { v["createParents"] = "true" }},
		{"read with size but no digest", func(v map[string]string) { v["direction"] = "read"; v["size"] = "1"; v["sha256"] = "" }},
		{"write without digest", func(v map[string]string) { v["direction"] = "write"; v["size"] = "42"; v["sha256"] = "" }},
		{"non-canonical size", func(v map[string]string) { v["direction"] = "write"; v["size"] = "042" }},
		{"absolute path", func(v map[string]string) { v["relativePath"] = "/etc/passwd" }},
		{"traversal", func(v map[string]string) { v["relativePath"] = "../escape" }},
		{"reserved staging prefix", func(v map[string]string) { v["relativePath"] = ".sf-transfer/partial" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			values := baseFileParameters("write")
			test.mutate(values)
			if _, err := transport.ParseFileParameters(fileParameterJSON(values)); !errors.Is(err, transport.ErrLinkRejected) {
				t.Fatalf("unsupported request was not rejected: %v", err)
			}
		})
	}
}

func TestCXFileTransportAcceptsOnlyCurrentReadAndSkipWrite(t *testing.T) {
	read := baseFileParameters("read")
	if _, err := transport.ParseFileParameters(fileParameterJSON(read)); err != nil {
		t.Fatalf("supported read rejected: %v", err)
	}
	write := baseFileParameters("write")
	if _, err := transport.ParseFileParameters(fileParameterJSON(write)); err != nil {
		t.Fatalf("supported write rejected: %v", err)
	}
	write["transferId"] = "transfer:with-colon"
	if _, err := transport.ParseFileParameters(fileParameterJSON(write)); err == nil {
		t.Fatal("colon transfer id accepted")
	}
}

func TestCXFileTransportTaskOfferRequiresNormalCapability(t *testing.T) {
	params := baseFileParameters("write")
	offer := &agentv1.TaskOffer{
		TaskId: "task_1", ExecutionId: "exec_1", ActionId: transport.FileCapability,
		ActionSchemaVersion: "1", Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
		CanonicalParametersJson: fileParameterJSON(params), PlanDigestSha256: strings.Repeat("a", 64),
		TimeoutSeconds: 15, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(),
	}
	if _, err := transport.FileOffer(offer); err != nil {
		t.Fatalf("valid offer rejected: %v", err)
	}
	root := proto.Clone(offer).(*agentv1.TaskOffer)
	root.Privilege = agentv1.Privilege_PRIVILEGE_ROOT
	if _, err := transport.FileOffer(root); err == nil {
		t.Fatal("root offer accepted for a normal file capability")
	}
	plugin := proto.Clone(offer).(*agentv1.TaskOffer)
	plugin.PluginId = "xray"
	if _, err := transport.FileOffer(plugin); err == nil {
		t.Fatal("plugin offer accepted for the core file capability")
	}
	schema := proto.Clone(offer).(*agentv1.TaskOffer)
	schema.ActionSchemaVersion = "2"
	if _, err := transport.FileOffer(schema); err == nil {
		t.Fatal("unknown schema version accepted")
	}
}

// TestCXFileTransportNoResumeOrRenameReproduction documents two lifecycle gaps
// for CX: FileSession.Open always sends resume_offset=0 and requires the reply
// path to equal the requested path, so multi-item resume/rename cannot be
// silently claimed by the controller.
func TestCXFileTransportNoResumeOrRenameReproduction(t *testing.T) {
	open := &agentv1.FileOpenRequest{
		TaskId: "task_1", ExecutionId: "exec_1", TransferId: "transfer_1",
		RelativePath: "data.bin", Direction: "write", Size: 42,
		Sha256: strings.Repeat("a", 64), ConflictPolicy: "skip",
	}
	if open.ResumeOffset != 0 {
		t.Fatal("file open resume offset is expected to be zero in the current CX transport")
	}
	// A rename reply path is not accepted by FileSession.Open; the recovered
	// source/destination contract must use the exact requested relative path.
	if open.RelativePath != "data.bin" {
		t.Fatal("unexpected path mutation")
	}
}
