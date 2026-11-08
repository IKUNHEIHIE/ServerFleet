package files_test

import (
	"bytes"
	"context"
	"serverfleet.local/serverfleet/internal/files"
	"testing"
)

type lifetimeChannel struct {
	*fakeAgent
	t              *testing.T
	transfer, open context.Context
	cancel         context.CancelFunc
}

func (c *lifetimeChannel) OpenFileChannelForTransfer(transfer, open context.Context, b files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
	c.transfer, c.open = transfer, open
	if _, ok := open.Deadline(); !ok {
		c.t.Fatal("open handshake lost its frame deadline")
	}
	s, r, e := c.fakeAgent.OpenFileChannel(open, b)
	return &lifetimeSession{AgentFileSession: s, channel: c}, r, e
}

type lifetimeSession struct {
	files.AgentFileSession
	channel *lifetimeChannel
}

func (s *lifetimeSession) WriteChunk(ctx context.Context, op files.FileChunkOp) (files.FileChunkResult, error) {
	c := s.channel
	if c.open.Err() == nil {
		c.t.Fatal("open-call timeout was retained past the handshake")
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if e := c.transfer.Err(); e != nil {
		return files.FileChunkResult{}, e
	}
	if _, ok := ctx.Deadline(); !ok {
		c.t.Fatal("chunk lost its own frame deadline")
	}
	return s.AgentFileSession.WriteChunk(ctx, op)
}
func TestChannelTransferLifetimeOutlivesOpenCall(t *testing.T) {
	c := &lifetimeChannel{fakeAgent: newFakeAgent(), t: t}
	tr, e := files.NewChannelTransport(files.ChannelTransportConfig{Channel: c, Paths: slashPathMapper{}})
	if e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("whole-task-owned-session"), 3000)
	r, e := tr.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/lifetime.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if e != nil || r.Bytes != int64(len(data)) {
		t.Fatalf("transfer died with its open call: %+v %v", r, e)
	}
	stored, ok := c.file("node-a", "/srv/lifetime.bin")
	if !ok || !bytes.Equal(stored, data) {
		t.Fatal("actual session bytes differ")
	}
}
func TestChannelTransferStillHonorsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(testChannelContext())
	defer cancel()
	c := &lifetimeChannel{fakeAgent: newFakeAgent(), t: t, cancel: cancel}
	tr, e := files.NewChannelTransport(files.ChannelTransportConfig{Channel: c, Paths: slashPathMapper{}})
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("must-not-write-after-cancel")
	e = tr.PushFile(ctx, "node-a", "/srv/cancel.bin", bytes.NewReader(data), int64(len(data)), digest(data))
	if e == nil {
		t.Fatal("parent cancellation was detached")
	}
	if _, ok := c.file("node-a", "/srv/cancel.bin"); ok {
		t.Fatal("cancelled transfer published bytes")
	}
}
