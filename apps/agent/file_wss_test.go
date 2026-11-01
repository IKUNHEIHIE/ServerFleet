package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type fileWSSFixture struct {
	client             *transport.FileClient
	node, root, ledger string
	gate               *atomic.Bool
	finished           chan *agentv1.TaskFinished
	frames             *atomic.Int32
	stop               func()
}

func newFileWSSFixture(t *testing.T, node, root, ledger string, dropFinal ...bool) *fileWSSFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	serverCert, clientCert, roots, pin := sessionPKI(t)
	fingerprint := sha256.Sum256(clientCert.Certificate[0])
	gate := new(atomic.Bool)
	gate.Store(true)
	verifier, _ := transport.NewPeerVerifier(func(context.Context, string) (transport.AgentIdentity, error) {
		return transport.AgentIdentity{NodeID: node, FingerprintSHA256: hex.EncodeToString(fingerprint[:]), Serial: "3", Enabled: true, TrustGeneration: 1}, nil
	})
	semantic, frames := agentSessionLimits()
	semantic.AllowFileFrames = true
	semantic.AllowedActions = map[string]string{transport.FileCapability: "1"}
	finished := make(chan *agentv1.TaskFinished, 16)
	hello := make(chan struct{}, 1)
	frameCount := new(atomic.Int32)
	var client *transport.FileClient
	hub, err := transport.NewControllerLinks(verifier, transport.ControllerLinkConfig{ProtocolMajor: 1, ProtocolMinor: 1, Semantic: semantic, Frames: frames, HeartbeatInterval: time.Second, MaxConnections: 1, Handshake: func(context.Context, transport.LinkObservation, *agentv1.AgentHello) ([]string, error) {
		return []string{transport.FileCapability}, nil
	}, Admit: func(context.Context, transport.LinkObservation, *agentv1.Envelope) error {
		if !gate.Load() {
			return transport.ErrLinkRejected
		}
		return nil
	}, Event: func(ctx context.Context, o transport.LinkObservation, e *agentv1.Envelope) error {
		if e.GetAgentHello() != nil {
			hello <- struct{}{}
		}
		if a := e.GetTaskAck(); a != nil && !a.Accepted {
			t.Log("rejected ack", a.ReasonCode, a.PreviouslySeen)
		}
		if transport.FileEnvelope(e) {
			frameCount.Add(1)
		}
		if _, err := client.Deliver(ctx, o, e); err != nil {
			return err
		}
		if e.GetTaskOutput() != nil {
			t.Error("file bytes reached ordinary task output")
		}
		if f := e.GetTaskFinished(); f != nil {
			finished <- proto.Clone(f).(*agentv1.TaskFinished)
		}
		return nil
	}, Disconnected: func(context.Context, transport.LinkObservation) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	client, err = transport.NewFileClient(hub, func(_ context.Context, _ transport.LinkObservation, o *agentv1.TaskOffer, e *agentv1.Envelope) error {
		if !gate.Load() {
			return transport.ErrLinkRejected
		}
		if _, err := transport.FileOffer(o); err != nil {
			return err
		}
		return nil
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := verifier.ControllerTLSConfig(serverCert, roots)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, id, _, err := transport.AcceptWebSocket(w, r, transport.WebSocketConfig{MaxFrameBytes: 64 << 10})
		if err != nil {
			return
		}
		_ = hub.Serve(ctx, socket, *r.TLS, id)
	}))
	server.TLS = tlsConfig
	server.StartTLS()
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "https://")
	connection, err := tls.Dial("tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientCert}, RootCAs: roots, ServerName: "fleet.test"})
	if err != nil {
		t.Fatal(err)
	}
	socket, id, err := transport.ClientWebSocket(ctx, connection, address, "/link", transport.WebSocketConfig{MaxFrameBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	current := func(context.Context, *agentv1.TaskOffer) error { return nil } // synthetic LOCAL path/capability authority; Controller gate is separate
	files, err := newAgentFileSessions(root, node, current)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := newAgentExecutor(ledger, map[string]agentAction{transport.FileCapability: files.Action()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := agentSessionConfig{NodeID: node, BootID: "boot-test", Version: "0.9.0-test", ServerName: "fleet.test", RootSPKISHA256: pin, ProtocolMajor: 1, ProtocolMinor: 1, MaxConcurrent: 1, Semantic: semantic, Frames: frames, Files: files, Current: func(context.Context, tls.ConnectionState) error { return nil }, Admit: current, Probe: socket.Ping}
	done := make(chan error, 1)
	var frameSocket transport.FrameSocket = socket
	if len(dropFinal) > 0 && dropFinal[0] {
		frameSocket = dropFileCompletion{socket}
	}
	go func() { done <- runAgentSession(ctx, frameSocket, connection.ConnectionState(), id, cfg, executor) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			socket.Close()
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Error("file worker did not stop")
			}
			files.Close()
			executor.Close()
		})
	}
	t.Cleanup(stop)
	select {
	case <-hello:
	case <-ctx.Done():
		t.Fatal("WSS handshake did not complete")
	}
	return &fileWSSFixture{client, node, root, ledger, gate, finished, frameCount, stop}
}

type dropFileCompletion struct{ transport.FrameSocket }

func (s dropFileCompletion) Write(ctx context.Context, raw []byte) error {
	var e agentv1.Envelope
	if proto.Unmarshal(raw, &e) == nil && e.GetFileChunkReply() != nil && e.GetFileChunkReply().Complete {
		s.Close()
		return io.ErrClosedPipe
	}
	return s.FrameSocket.Write(ctx, raw)
}

func TestActualWSSCompletionLossAndReconnectCannotRepeatWrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root actual WSS")
	}
	root, ledger := privateAgentDirectory(t), privateAgentDirectory(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data := []byte("synthetic complete bytes before lost reply")
	hash := sha256.Sum256(data)
	o := fileTestOffer(transport.FileParameters{TransferID: "lost-reply", RelativePath: "once.bin", Direction: "write", Size: uint64(len(data)), SHA256: hex.EncodeToString(hash[:]), ConflictPolicy: "skip"}, "lost-execution")
	first := newFileWSSFixture(t, "node-lost", root, ledger, true)
	s, err := first.client.NewSession(ctx, first.node, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 500*time.Millisecond)
	_, err = s.Chunk(short, &agentv1.FileChunkRequest{TaskId: o.TaskId, ExecutionId: o.ExecutionId, TransferId: "lost-reply", Data: data, Finish: true})
	stop()
	if err == nil {
		t.Fatal("lost completion was reported successful")
	}
	s.Close()
	first.stop()
	got, err := os.ReadFile(filepath.Join(root, "once.bin"))
	if err != nil || string(got) != string(data) {
		t.Fatal("no actual committed bytes before lost reply", err)
	}
	second := newFileWSSFixture(t, "node-lost", root, ledger)
	again, err := second.client.NewSession(ctx, second.node, o)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, err = again.Open(ctx); err == nil {
		t.Fatal("reconnect repeated a persisted unknown write")
	}
	raw, err := os.ReadFile(filepath.Join(ledger, o.ExecutionId, "result.json"))
	if err != nil || !strings.Contains(string(raw), `"Result":5`) {
		t.Fatal("missing durable UNKNOWN", err, string(raw))
	}
}
func fileTestOffer(p transport.FileParameters, execution string) *agentv1.TaskOffer {
	return &agentv1.TaskOffer{TaskId: "task-" + execution, ExecutionId: execution, ActionId: transport.FileCapability, ActionSchemaVersion: "1", Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, CanonicalParametersJson: canonicalFileParameters(p), PlanDigestSha256: strings.Repeat("a", 64), TimeoutSeconds: 15, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()}
}
func TestActualWSSFileTwoAgentsBytesCancelRevokeAndUnknown(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root real file sessions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srcRoot, dstRoot := privateAgentDirectory(t), privateAgentDirectory(t)
	data := []byte(strings.Repeat("synthetic-wss-file-bytes-", 3500))
	os.WriteFile(filepath.Join(srcRoot, "source.bin"), data, 0600)
	source := newFileWSSFixture(t, "node-source", srcRoot, privateAgentDirectory(t))
	destination := newFileWSSFixture(t, "node-target", dstRoot, privateAgentDirectory(t))
	readOffer := fileTestOffer(transport.FileParameters{TransferID: "transfer-read", RelativePath: "source.bin", Direction: "read", ConflictPolicy: "skip"}, "exec-read")
	read, err := source.client.NewSession(ctx, source.node, readOffer)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	meta, err := read.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writeOffer := fileTestOffer(transport.FileParameters{TransferID: "transfer-write", RelativePath: "target.bin", Direction: "write", Size: meta.Size, SHA256: meta.Sha256, ConflictPolicy: "skip"}, "exec-write")
	write, err := destination.client.NewSession(ctx, destination.node, writeOffer)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	if _, err = write.Open(ctx); err != nil {
		t.Fatal(err)
	}
	// Snapshot is frozen: later replacement does not alter the streamed bytes.
	os.WriteFile(filepath.Join(srcRoot, "source.bin"), []byte("changed after stat"), 0600)
	for offset := uint64(0); offset < meta.Size; {
		length := min(uint64(16<<10), meta.Size-offset)
		chunk, err := read.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: readOffer.TaskId, ExecutionId: readOffer.ExecutionId, TransferId: "transfer-read", Offset: offset, ReadBytes: uint32(length)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = write.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: writeOffer.TaskId, ExecutionId: writeOffer.ExecutionId, TransferId: "transfer-write", Offset: offset, Data: chunk.Data, Finish: offset+length == meta.Size})
		if err != nil {
			t.Fatal(err)
		}
		offset += length
	}
	got, err := os.ReadFile(filepath.Join(dstRoot, "target.bin"))
	if err != nil || string(got) != string(data) {
		t.Fatal("WSS byte mismatch", err)
	}
	for _, fixture := range []*fileWSSFixture{source, destination} {
		select {
		case result := <-fixture.finished:
			if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
				t.Fatal(result)
			}
		case <-ctx.Done():
			t.Fatal("missing actual task finish")
		}
	}
	// Reopen the same execution: durable previously-seen reservation cannot
	// create another write, even if its final WSS receipt was lost.
	write.Close()
	again, err := destination.client.NewSession(ctx, destination.node, writeOffer)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, err = again.Open(ctx); err == nil {
		t.Fatal("execution replay reopened file")
	}
	for i, mode := range []string{"cancel", "revoke"} {
		p := transport.FileParameters{TransferID: fmt.Sprintf("negative-%d", i), RelativePath: mode + ".bin", Direction: "write", Size: meta.Size, SHA256: meta.Sha256, ConflictPolicy: "skip"}
		o := fileTestOffer(p, "exec-"+mode)
		s, err := destination.client.NewSession(ctx, destination.node, o)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Open(ctx); err != nil {
			t.Fatal(err)
		}
		if mode == "revoke" {
			destination.gate.Store(false)
		}
		_, err = s.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: o.TaskId, ExecutionId: o.ExecutionId, TransferId: p.TransferID, Cancel: true})
		if mode == "cancel" && err != nil || mode == "revoke" && err == nil {
			t.Fatal(mode, err)
		}
		if mode == "cancel" {
			select {
			case result := <-destination.finished:
				if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED {
					t.Fatal("cancel lacked terminal evidence", result)
				}
			case <-ctx.Done():
				t.Fatal("cancel terminal timed out")
			}
			frames := destination.frames.Load()
			if _, err = s.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: o.TaskId, ExecutionId: o.ExecutionId, TransferId: p.TransferID, Cancel: true}); err == nil || destination.frames.Load() != frames {
				t.Fatal("canceled session emitted another frame")
			}
		}
		s.Close()
		if _, e := os.Stat(filepath.Join(dstRoot, p.RelativePath)); !os.IsNotExist(e) {
			t.Fatal("negative transfer published bytes")
		}
	}
	if source.frames.Load() < 2 || destination.frames.Load() < 2 {
		t.Fatal("no actual file frames observed")
	}
}
