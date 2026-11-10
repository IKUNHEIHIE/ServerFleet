package transport

import (
	"context"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

// FileFrameGate is mandatory trusted composition. It must check the CURRENT
// unified task, actor, approval, node, execution, immutable plan and cancel state
// for BOTH requests and replies. No module import, second scheduler or fallback.
type FileFrameGate func(context.Context, LinkObservation, *agentv1.TaskOffer, *agentv1.Envelope) error
type FileClient struct {
	links    *ControllerLinks
	gate     FileFrameGate
	mu       sync.Mutex
	sessions map[string]*FileSession
	max      int
}
type FileSession struct {
	client       *FileClient
	observation  LinkObservation
	offer        *agentv1.TaskOffer
	parameters   FileParameters
	ctx          context.Context
	cancel       context.CancelFunc
	opMu         sync.Mutex
	mu           sync.Mutex
	waiting      string
	responses    chan *agentv1.Envelope
	closed       bool
	opened       bool
	started      bool
	finished     bool
	size, offset uint64
	digest       string
}

func NewFileClient(links *ControllerLinks, gate FileFrameGate, max int) (*FileClient, error) {
	if links == nil || gate == nil || max < 1 || max > 64 {
		return nil, ErrLinkRejected
	}
	return &FileClient{links: links, gate: gate, sessions: map[string]*FileSession{}, max: max}, nil
}
func fileSessionKey(node, execution string) string { return node + "/" + execution }
func (c *FileClient) NewSession(ctx context.Context, node string, offer *agentv1.TaskOffer) (*FileSession, error) {
	p, err := FileOffer(offer)
	if err != nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrLinkRejected
	}
	o, err := c.links.FileConnection(ctx, node)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, min(time.Duration(offer.TimeoutSeconds)*time.Second, 10*time.Minute))
	s := &FileSession{client: c, observation: o, offer: proto.Clone(offer).(*agentv1.TaskOffer), parameters: p, ctx: bounded, cancel: cancel, responses: make(chan *agentv1.Envelope, 1)}
	c.mu.Lock()
	key := fileSessionKey(node, offer.ExecutionId)
	if c.sessions[key] != nil || len(c.sessions) >= c.max {
		c.mu.Unlock()
		cancel()
		return nil, ErrLinkRejected
	}
	c.sessions[key] = s
	c.mu.Unlock()
	return s, nil
}
func (s *FileSession) check(ctx context.Context, e *agentv1.Envelope) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrLinkRejected
		}
	}()
	if ctx == nil || ctx.Err() != nil || s.ctx.Err() != nil {
		return ErrLinkRejected
	}
	o, err := s.client.links.FileConnection(ctx, s.observation.Identity.NodeID)
	if err != nil || o != s.observation {
		return ErrLinkRejected
	}
	if s.client.gate(ctx, o, proto.Clone(s.offer).(*agentv1.TaskOffer), proto.Clone(e).(*agentv1.Envelope)) != nil || ctx.Err() != nil {
		return ErrLinkRejected
	}
	return nil
}
func (s *FileSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.client.mu.Lock()
	key := fileSessionKey(s.observation.Identity.NodeID, s.offer.ExecutionId)
	if s.client.sessions[key] == s {
		delete(s.client.sessions, key)
	}
	s.client.mu.Unlock()
	return nil
}
func (s *FileSession) exchange(ctx context.Context, e *agentv1.Envelope, kind string) (result *agentv1.Envelope, err error) {
	// Any uncertain frame closes this execution's local session. Reconciliation
	// belongs to the unified task worker, never a second write attempt here.
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	s.mu.Lock()
	if s.closed || s.waiting != "" {
		s.mu.Unlock()
		return nil, ErrLinkRejected
	}
	s.waiting = kind
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.waiting = ""; s.mu.Unlock() }()
	if s.check(ctx, e) != nil {
		return nil, ErrLinkRejected
	}
	if s.client.links.Send(ctx, s.observation.Identity.NodeID, e) != nil {
		return nil, ErrDeliveryUnknown
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case reply := <-s.responses:
		if s.check(ctx, reply) != nil {
			return nil, ErrLinkRejected
		}
		return reply, nil
	case <-ctx.Done():
		return nil, ErrDeliveryUnknown
	case <-s.ctx.Done():
		return nil, ErrDeliveryUnknown
	case <-timer.C:
		return nil, ErrDeliveryUnknown
	}
}

// Open first waits for the durable TaskAck. PreviouslySeen cannot authorize new
// effects, even after reconnect; query execution state instead of auto-retrying.
func (s *FileSession) Open(ctx context.Context) (*agentv1.FileOpenReply, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.started {
		return nil, ErrLinkRejected
	}
	s.started = true
	e, err := s.exchange(ctx, &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: proto.Clone(s.offer).(*agentv1.TaskOffer)}}, "ack")
	if err != nil {
		return nil, err
	}
	ack := e.GetTaskAck()
	if ack == nil || !ack.Accepted || ack.PreviouslySeen {
		return nil, ErrDeliveryUnknown
	}
	p := s.parameters
	r := &agentv1.FileOpenRequest{TaskId: s.offer.TaskId, ExecutionId: s.offer.ExecutionId, TransferId: p.TransferID, RelativePath: p.RelativePath, Direction: p.Direction, Size: p.Size, Sha256: p.SHA256, ConflictPolicy: p.ConflictPolicy, CreateParents: p.CreateParents, ResumeOffset: FileResumeOffset(p), PlanDigestSha256: s.offer.PlanDigestSha256}
	e, err = s.exchange(ctx, &agentv1.Envelope{Message: &agentv1.Envelope_FileOpenRequest{FileOpenRequest: r}}, "open")
	if err != nil {
		return nil, err
	}
	reply := e.GetFileOpenReply()
	if reply == nil {
		return nil, ErrLinkRejected
	}
	// Usually the agent echoes the requested path. A rename policy may return a
	// free sibling chosen under the same explicit directory; never another tree.
	pathOK := reply.RelativePath == p.RelativePath
	if p.ConflictPolicy == "rename" {
		pathOK = FilePath(reply.RelativePath) && fileDirectory(reply.RelativePath) == fileDirectory(p.RelativePath)
	}
	if reply.ResultCode != "ok" || !pathOK || reply.PlanDigestSha256 != s.offer.PlanDigestSha256 || reply.ResumeOffset != FileResumeOffset(p) || reply.Size > FileMaxBytes || !FileDigest(reply.Sha256) || p.SHA256 != "" && (reply.Size != p.Size || reply.Sha256 != p.SHA256) {
		return nil, ErrLinkRejected
	}
	s.offset = reply.ResumeOffset
	s.opened = true
	s.finished = reply.Complete
	s.size = reply.Size
	s.digest = reply.Sha256
	return reply, nil
}
func (s *FileSession) Chunk(ctx context.Context, r *agentv1.FileChunkRequest) (*agentv1.FileChunkReply, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if !s.opened || s.finished || r == nil || r.TaskId != s.offer.TaskId || r.ExecutionId != s.offer.ExecutionId || r.TransferId != s.parameters.TransferID || r.Offset != s.offset {
		return nil, ErrLinkRejected
	}
	if !r.Cancel && (s.parameters.Direction == "read" && (r.ReadBytes == 0 || len(r.Data) > 0 || r.Finish) || s.parameters.Direction == "write" && r.ReadBytes > 0) {
		return nil, ErrLinkRejected
	}
	next := r.Offset + uint64(len(r.Data))
	if s.parameters.Direction == "read" {
		next = r.Offset + uint64(r.ReadBytes)
	}
	if !r.Cancel && next > s.size {
		return nil, ErrLinkRejected
	}
	e, err := s.exchange(ctx, &agentv1.Envelope{Message: &agentv1.Envelope_FileChunkRequest{FileChunkRequest: proto.Clone(r).(*agentv1.FileChunkRequest)}}, "chunk")
	if err != nil {
		return nil, err
	}
	reply := e.GetFileChunkReply()
	if reply == nil || reply.Offset != r.Offset {
		return nil, ErrLinkRejected
	}
	if r.Cancel {
		if reply.ResultCode != "canceled" || len(reply.Data) != 0 {
			return nil, ErrLinkRejected
		}
		s.finished = true
		return reply, nil
	}
	if reply.ResultCode != "ok" || reply.NextOffset != next || s.parameters.Direction == "read" && uint32(len(reply.Data)) != r.ReadBytes || s.parameters.Direction == "write" && len(reply.Data) != 0 || reply.Complete != (next == s.size && (s.parameters.Direction == "read" || r.Finish)) {
		return nil, ErrLinkRejected
	}
	s.offset = next
	s.finished = reply.Complete
	return reply, nil
}

// Deliver is called only from the authenticated ControllerLinks Event adapter.
// It consumes correlated replies/acks; normal task events stay with tasks.
func (c *FileClient) Deliver(ctx context.Context, o LinkObservation, e *agentv1.Envelope) (bool, error) {
	if e == nil {
		return false, ErrLinkRejected
	}
	task, execution, transfer := FileFrameIDs(e)
	kind := ""
	if e.GetFileOpenReply() != nil {
		kind = "open"
	}
	if e.GetFileChunkReply() != nil {
		kind = "chunk"
	}
	if a := e.GetTaskAck(); a != nil {
		task, execution = a.TaskId, a.ExecutionId
		kind = "ack"
	}
	if kind == "" {
		return false, nil
	}
	c.mu.Lock()
	s := c.sessions[fileSessionKey(o.Identity.NodeID, execution)]
	c.mu.Unlock()
	if s == nil {
		if FileEnvelope(e) {
			return true, ErrLinkRejected
		}
		return false, nil
	}
	if task != s.offer.TaskId || kind != "ack" && transfer != s.parameters.TransferID || o != s.observation || s.check(s.ctx, e) != nil {
		return true, ErrLinkRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.waiting != kind {
		return true, ErrLinkRejected
	}
	select {
	case s.responses <- proto.Clone(e).(*agentv1.Envelope):
		return true, nil
	default:
		return true, ErrLinkRejected
	}
}

// AuthorizeFrame is the ControllerLinks.Admit composition seam. Only registered
// active sessions may send/receive file frames; an offer must be the exact one
// registered by the unified task worker. The mandatory live gate runs again.
func (c *FileClient) AuthorizeFrame(ctx context.Context, o LinkObservation, e *agentv1.Envelope) error {
	if c == nil || e == nil {
		return ErrLinkRejected
	}
	task, execution, transfer := FileFrameIDs(e)
	if offer := e.GetTaskOffer(); offer != nil {
		task, execution = offer.TaskId, offer.ExecutionId
	}
	c.mu.Lock()
	s := c.sessions[fileSessionKey(o.Identity.NodeID, execution)]
	c.mu.Unlock()
	if s == nil || task != s.offer.TaskId || o != s.observation {
		return ErrLinkRejected
	}
	if offer := e.GetTaskOffer(); offer != nil {
		if !proto.Equal(offer, s.offer) {
			return ErrLinkRejected
		}
	} else if transfer != s.parameters.TransferID {
		return ErrLinkRejected
	}
	return s.check(ctx, e)
}
