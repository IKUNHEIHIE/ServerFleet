package server

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
)

type fakeGatewayStage struct {
	mu        sync.Mutex
	calls     int
	failAfter int
}

func (s *fakeGatewayStage) CheckArtifactStage(context.Context, string, string, string, uint64, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.failAfter > 0 && s.calls > s.failAfter {
		return files.ErrTaskBindingRequired
	}
	return nil
}

func (s *fakeGatewayStage) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeGatewayChannel struct {
	mu     sync.Mutex
	opened []files.FileBinding
	data   []byte
}

func (c *fakeGatewayChannel) OpenFileChannel(ctx context.Context, binding files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, files.FileOpenResult{}, err
	}
	c.mu.Lock()
	c.opened = append(c.opened, binding)
	c.mu.Unlock()
	return &fakeGatewayChannelSession{channel: c, binding: binding}, files.FileOpenResult{
		TaskID: binding.TaskID, ExecutionID: binding.ExecutionID, TransferID: binding.TransferID,
		PlanDigestSHA256: binding.PlanDigestSHA256, RelativePath: binding.RelativePath,
		Size: binding.Size, SHA256: binding.SHA256, ResultCode: "ok",
	}, nil
}

func (c *fakeGatewayChannel) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...)
}

func (c *fakeGatewayChannel) bindings() []files.FileBinding {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]files.FileBinding(nil), c.opened...)
}

type fakeGatewayChannelSession struct {
	channel *fakeGatewayChannel
	binding files.FileBinding
	buffer  []byte
}

func (s *fakeGatewayChannelSession) ReadChunk(context.Context, files.FileChunkOp) (files.FileChunkResult, error) {
	return files.FileChunkResult{}, files.ErrChannelProtocol
}

func (s *fakeGatewayChannelSession) WriteChunk(_ context.Context, op files.FileChunkOp) (files.FileChunkResult, error) {
	if op.Cancel {
		return files.FileChunkResult{TaskID: s.binding.TaskID, ExecutionID: s.binding.ExecutionID, TransferID: s.binding.TransferID, ResultCode: "canceled"}, nil
	}
	if op.Offset != uint64(len(s.buffer)) {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	s.buffer = append(s.buffer, op.Data...)
	s.channel.mu.Lock()
	s.channel.data = append([]byte(nil), s.buffer...)
	s.channel.mu.Unlock()
	next := op.Offset + uint64(len(op.Data))
	return files.FileChunkResult{
		TaskID: s.binding.TaskID, ExecutionID: s.binding.ExecutionID, TransferID: s.binding.TransferID,
		Offset: op.Offset, NextOffset: next,
		Complete: op.Finish && int64(next) == s.binding.Size, ResultCode: "ok",
	}, nil
}

func (s *fakeGatewayChannelSession) Cancel(context.Context, string) error { return nil }
func (s *fakeGatewayChannelSession) Close() error                         { return nil }

type gatewayWSSFixture struct {
	db      *storage.DB
	stage   *fakeGatewayStage
	channel *fakeGatewayChannel
	bound   GatewayBoundArtifactUploader
	root    string
}

func newGatewayWSSFixture(t *testing.T) *gatewayWSSFixture {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "gateway.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureGatewayArtifactBindings(ctx, db); err != nil {
		t.Fatal(err)
	}
	stage := &fakeGatewayStage{}
	channel := &fakeGatewayChannel{}
	root := "/srv/gateway"
	uploader, err := NewGatewayChannelUploader(channel, map[string]string{"node-1": root}, db, stage)
	if err != nil {
		t.Fatal(err)
	}
	bound, ok := uploader.(GatewayBoundArtifactUploader)
	if !ok {
		t.Fatal("uploader does not implement GatewayBoundArtifactUploader")
	}
	return &gatewayWSSFixture{db: db, stage: stage, channel: channel, bound: bound, root: root}
}

func (f *gatewayWSSFixture) binding(content []byte) GatewayArtifactBinding {
	return GatewayArtifactBinding{
		TaskID: "task-1", NodeID: "node-1", ExecutionID: "exec-1", TransferID: "gwa-1",
		PlanDigest: gatewayContentDigest([]byte("plan")), GatewayID: "gw-1", PlanID: "plan-1",
		TargetRevision: 2, AbsolutePath: f.root + "/artifacts/site.conf",
		Content: content, SHA256: gatewayContentDigest(content),
	}
}

func TestGatewayChannelUploaderBindsScopeAndPersists(t *testing.T) {
	fixture := newGatewayWSSFixture(t)
	content := []byte(strings.Repeat("server { }\n", 2000))
	binding := fixture.binding(content)
	if err := fixture.bound.UploadBound(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	if got := fixture.channel.bytes(); string(got) != string(content) {
		t.Fatalf("uploaded bytes = %d, want %d", len(got), len(content))
	}
	opened := fixture.channel.bindings()
	if len(opened) != 1 {
		t.Fatalf("open calls = %d, want 1", len(opened))
	}
	if opened[0].TaskID != binding.TaskID || opened[0].ExecutionID != binding.ExecutionID ||
		opened[0].TransferID != binding.TransferID || opened[0].PlanDigestSHA256 != binding.PlanDigest ||
		opened[0].RelativePath != "artifacts/site.conf" || opened[0].NodeID != "node-1" {
		t.Fatalf("open binding = %+v", opened[0])
	}
	if fixture.stage.count() < 3 {
		t.Fatalf("stage checks = %d, want at least 3", fixture.stage.count())
	}
	stored, err := loadGatewayArtifactBinding(context.Background(), fixture.db, binding.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RelativePath != "artifacts/site.conf" || stored.Size != int64(len(content)) || stored.SHA256 != binding.SHA256 {
		t.Fatalf("stored binding = %+v", stored)
	}
}

func TestGatewayChannelUploaderStopsWhenStageRevoked(t *testing.T) {
	fixture := newGatewayWSSFixture(t)
	fixture.stage.failAfter = 2
	content := []byte(strings.Repeat("x", 20000))
	if err := fixture.bound.UploadBound(context.Background(), fixture.binding(content)); err == nil {
		t.Fatal("upload succeeded after the stage was revoked")
	}
	if len(fixture.channel.bytes()) != 0 {
		t.Fatal("bytes were written after the per-chunk stage check failed")
	}
}

func TestGatewayChannelUploaderRejectsPathOutsideRoot(t *testing.T) {
	fixture := newGatewayWSSFixture(t)
	binding := fixture.binding([]byte("x"))
	binding.AbsolutePath = "/etc/passwd"
	if err := fixture.bound.UploadBound(context.Background(), binding); !errors.Is(err, files.ErrPathMappingUnavailable) {
		t.Fatalf("err = %v, want ErrPathMappingUnavailable", err)
	}
}

func TestGatewayChannelUploaderLegacyUploadFailsClosed(t *testing.T) {
	fixture := newGatewayWSSFixture(t)
	uploader, err := NewGatewayChannelUploader(fixture.channel, map[string]string{"node-1": fixture.root}, fixture.db, fixture.stage)
	if err != nil {
		t.Fatal(err)
	}
	if err := uploader.Upload(context.Background(), "task-1", "node-1", fixture.root+"/artifacts/site.conf", []byte("x"), gatewayContentDigest([]byte("x"))); !errors.Is(err, files.ErrTaskBindingRequired) {
		t.Fatalf("err = %v, want ErrTaskBindingRequired", err)
	}
}

func TestGatewayFileOfferMatchesFileCapability(t *testing.T) {
	binding := files.FileBinding{
		TaskID: "task-1", ExecutionID: "exec-1", TransferID: "gwa-1", NodeID: "node-1",
		RelativePath: "artifacts/site.conf", Direction: files.ChannelWrite,
		Size: 12, SHA256: gatewayContentDigest([]byte("server{}\n")), ConflictPolicy: files.ConflictOverwrite,
		PlanDigestSHA256: gatewayContentDigest([]byte("plan")),
	}
	offer, err := gatewayFileOffer(binding, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	params, err := transport.FileOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	if params.TransferID != binding.TransferID || params.RelativePath != binding.RelativePath ||
		params.Direction != "write" || params.SHA256 != binding.SHA256 || offer.PlanDigestSha256 != binding.PlanDigestSHA256 {
		t.Fatalf("offer = %+v params = %+v", offer, params)
	}
}

func TestGatewayArtifactGateAuthorizesOnlyStoredBinding(t *testing.T) {
	fixture := newGatewayWSSFixture(t)
	content := []byte("server { }\n")
	binding := fixture.binding(content)
	if err := fixture.bound.UploadBound(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	gate, err := NewGatewayArtifactGate(fixture.db, fixture.stage)
	if err != nil {
		t.Fatal(err)
	}
	fileBinding := files.FileBinding{
		TaskID: binding.TaskID, ExecutionID: binding.ExecutionID, TransferID: binding.TransferID,
		NodeID: binding.NodeID, RelativePath: "artifacts/site.conf", Direction: files.ChannelWrite,
		Size: int64(len(content)), SHA256: binding.SHA256, ConflictPolicy: files.ConflictOverwrite,
		PlanDigestSHA256: binding.PlanDigest,
	}
	offer, err := gatewayFileOffer(fileBinding, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	observation := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: binding.NodeID}}
	envelope := &agentv1.Envelope{Message: &agentv1.Envelope_FileOpenRequest{FileOpenRequest: &agentv1.FileOpenRequest{
		TaskId: binding.TaskID, ExecutionId: binding.ExecutionID, TransferId: binding.TransferID,
	}}}
	if err := gate.Authorize(context.Background(), observation, offer, envelope); err != nil {
		t.Fatalf("stored binding was rejected: %v", err)
	}
	forged := proto.Clone(offer).(*agentv1.TaskOffer)
	forged.TaskId = "task-2"
	if err := gate.Authorize(context.Background(), observation, forged, envelope); !errors.Is(err, files.ErrTaskBindingRequired) {
		t.Fatalf("forged task err = %v, want ErrTaskBindingRequired", err)
	}
	if ok, err := gate.Matches(context.Background(), binding.ExecutionID); err != nil || !ok {
		t.Fatalf("Matches = %v, %v", ok, err)
	}
	if ok, err := gate.Matches(context.Background(), "exec-unknown"); err != nil || ok {
		t.Fatalf("Matches unknown = %v, %v", ok, err)
	}
}
