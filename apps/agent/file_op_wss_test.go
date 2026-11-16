package main

import (
	"context"

	"os"
	"path/filepath"
	"runtime"
	"serverfleet.local/serverfleet/internal/transport"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func diropSeed(t *testing.T) (root, ledger, node string) {
	t.Helper()
	root = privateAgentDirectory(t)
	ledger = privateAgentDirectory(t)
	node = "node-dirop"
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root, ledger, node
}

// Direct handler check (no WSS): isolates the agent FileOp handler itself.
func TestFileOpHandleOpDirect(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root")
	}
	root, _, node := diropSeed(t)
	files, err := newAgentFileSessions(root, node, false, func(context.Context, *agentv1.TaskOffer) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	env := &agentv1.Envelope{Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "d1", NodeId: node, Op: "list", Path: "/"}}}
	reply, err := files.HandleOp(context.Background(), env)
	if err != nil {
		t.Fatal("HandleOp error", err)
	}
	r := reply.GetFileOpReply()
	if r == nil || r.ResultCode != "ok" {
		t.Fatal("direct list not ok", r)
	}
	got := map[string]string{}
	for _, e := range r.Entries {
		got[e.Name] = e.Type
	}
	if got["hello.txt"] != "file" || got["sub"] != "directory" {
		t.Fatal("direct entries wrong", got)
	}
}

// End-to-end over a real WSS+mTLS link: the exact path the console browse uses.
func TestFileOpDirectoryRoundTripOverWSS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root actual WSS")
	}
	root, ledger, node := diropSeed(t)
	f := newFileWSSFixture(t, node, root, ledger)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	op := func(req *agentv1.FileOpRequest) *agentv1.FileOpReply {
		t.Helper()
		req.NodeId = node
		reply, err := f.client.DirectoryOp(ctx, node, req)
		if err != nil {
			t.Fatalf("directory op %s %q: %v (agent_frames_seen=%d)", req.Op, req.Path, err, f.frames.Load())
		}
		return reply
	}

	list := op(&agentv1.FileOpRequest{RequestId: "dirop-list", Op: "list", Path: "/"})
	if list.ResultCode != "ok" {
		t.Fatal("list not ok:", list.ResultCode)
	}
	names := map[string]string{}
	for _, e := range list.Entries {
		names[e.Name] = e.Type
	}
	if names["hello.txt"] != "file" || names["sub"] != "directory" {
		t.Fatal("list entries wrong", names)
	}

	st := op(&agentv1.FileOpRequest{RequestId: "dirop-stat", Op: "stat", Path: "/hello.txt"})
	if st.ResultCode != "ok" || st.Size != 11 {
		t.Fatal("stat wrong", st.ResultCode, st.Size)
	}

	mk := op(&agentv1.FileOpRequest{RequestId: "dirop-mkdir", Op: "mkdir", Path: "/newdir"})
	if mk.ResultCode != "ok" {
		t.Fatal("mkdir not ok", mk.ResultCode)
	}
	if info, err := os.Stat(filepath.Join(root, "newdir")); err != nil || !info.IsDir() {
		t.Fatal("mkdir did not create dir", err)
	}

	rn := op(&agentv1.FileOpRequest{RequestId: "dirop-rename", Op: "rename", Path: "/hello.txt", NewName: "renamed.txt"})
	if rn.ResultCode != "ok" {
		t.Fatal("rename not ok", rn.ResultCode)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal("rename target missing", err)
	}

	rm := op(&agentv1.FileOpRequest{RequestId: "dirop-remove", Op: "remove", Path: "/sub"})
	if rm.ResultCode != "ok" {
		t.Fatal("remove not ok", rm.ResultCode)
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Fatal("remove did not delete", err)
	}

	escReply, escErr := f.client.DirectoryOp(ctx, node, &agentv1.FileOpRequest{RequestId: "dirop-esc", NodeId: node, Op: "list", Path: "/../etc"})
	if escErr == nil && escReply.ResultCode == "ok" {
		t.Fatal("escape path must not succeed")
	}
}

func TestFileOpIncomingValidates(t *testing.T) {
	semantic, _ := agentSessionLimits()
	semantic.AllowFileFrames = true
	semantic.AllowedActions = map[string]string{transport.FileCapability: "1"}
	env := &agentv1.Envelope{ProtocolMajor: 1, ProtocolMinor: 1, MessageId: "m", ConnectionId: "c", NodeId: "node", Sequence: 1, SentAtUnixMs: time.Now().UnixMilli(), Message: &agentv1.Envelope_FileOpRequest{FileOpRequest: &agentv1.FileOpRequest{RequestId: "r", NodeId: "node", Op: "list", Path: "/"}}}
	ctx := transport.ValidationContext{Direction: transport.ControllerToAgent, Phase: transport.Established, ProtocolMajor: 1, ProtocolMinor: 1, Now: time.Now()}
	if err := transport.ValidateEnvelope(env, ctx, semantic); err != nil {
		t.Fatal("FileOpRequest should validate incoming", err)
	}
}
