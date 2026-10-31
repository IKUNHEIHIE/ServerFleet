package transport_test

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func trustedBinding() transport.ConnectionBinding {
	return transport.ConnectionBinding{NodeID: "synthetic-node", ConnectionID: "synthetic-connection", Direction: transport.AgentToController}
}

func newGuard(t testing.TB, binding transport.ConnectionBinding) *transport.ConnectionGuard {
	t.Helper()
	g, err := transport.NewConnectionGuard(binding)
	if err != nil || g == nil {
		t.Fatalf("guard construction: %v", err)
	}
	return g
}

func assertConnectionError(t testing.TB, decoded *agentv1.Envelope, err error, code transport.ConnectionErrorCode) {
	t.Helper()
	var detail *transport.ConnectionError
	if decoded != nil || !errors.As(err, &detail) || detail.Code != code {
		t.Fatalf("got message=%v error=%v, want nil / %s", decoded, err, code)
	}
}

func receiveSequence(t testing.TB, g *transport.ConnectionGuard, sequence uint64) *agentv1.Envelope {
	t.Helper()
	sample := examples()[2]
	sample.e.Sequence = sequence
	raw := encodeFrame(t, sample.e)
	before := bytes.Clone(raw)
	decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
	if err != nil || decoded == nil {
		t.Fatalf("sequence %d failed: %v", sequence, err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("Receive mutated raw frame")
	}
	return decoded
}

// At the task baseline, bounded decode + semantic validation accepted this
// mismatching node. The same frame now invokes the actual connection guard.
func TestConnectionWrongNodeRejected(t *testing.T) {
	g := newGuard(t, trustedBinding())
	sample := examples()[2]
	sample.e.NodeId = "synthetic-other-node"
	raw := encodeFrame(t, sample.e)
	decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionNodeMismatch)
	if g.HighWatermark() != 0 {
		t.Fatal("mismatch consumed a sequence")
	}
	receiveSequence(t, g, 1)
}

func TestConnectionExplicitBindingAndUnboundDefaults(t *testing.T) {
	for _, mutate := range []func(*transport.ConnectionBinding){
		func(b *transport.ConnectionBinding) { b.NodeID = "" }, func(b *transport.ConnectionBinding) { b.NodeID = " " },
		func(b *transport.ConnectionBinding) { b.NodeID = "node\x00" }, func(b *transport.ConnectionBinding) { b.NodeID = string([]byte{0xff}) },
		func(b *transport.ConnectionBinding) { b.ConnectionID = "" }, func(b *transport.ConnectionBinding) { b.ConnectionID = " connection" },
		func(b *transport.ConnectionBinding) { b.Direction = 0 }, func(b *transport.ConnectionBinding) { b.Direction = 255 },
	} {
		binding := trustedBinding()
		mutate(&binding)
		g, err := transport.NewConnectionGuard(binding)
		if g != nil {
			t.Fatal("invalid binding returned a usable guard")
		}
		var detail *transport.ConnectionError
		if !errors.As(err, &detail) || detail.Code != transport.ConnectionInvalidBinding {
			t.Fatalf("invalid binding: %v", err)
		}
	}
	var nilGuard *transport.ConnectionGuard
	for _, g := range []*transport.ConnectionGuard{nilGuard, {}} {
		sample := examples()[2]
		raw := encodeFrame(t, sample.e)
		decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
		assertConnectionError(t, decoded, err, transport.ConnectionUnbound)
		g.Close()
		decoded, err = g.Receive(raw, sample.ctx, limits(), frameLimits())
		assertConnectionError(t, decoded, err, transport.ConnectionUnbound)
		if g.HighWatermark() != 0 {
			t.Fatal("unbound guard got a watermark")
		}
	}
}

func TestConnectionIncreasingJumpAndOutOfOrder(t *testing.T) {
	g := newGuard(t, trustedBinding())
	for _, sequence := range []uint64{1, 2, 9} {
		receiveSequence(t, g, sequence)
		if g.HighWatermark() != sequence {
			t.Fatal("successful frame did not commit sequence")
		}
	}
	for _, sequence := range []uint64{9, 8, 1} {
		sample := examples()[2]
		sample.e.Sequence = sequence
		sample.e.MessageId = "different-message-id"
		decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
		assertConnectionError(t, decoded, err, transport.ConnectionSequenceRejected)
		if g.HighWatermark() != 9 {
			t.Fatal("rejection changed watermark")
		}
	}
	sample := examples()[2]
	sample.e.Sequence = 0
	decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	if decoded != nil {
		t.Fatal("zero sequence exposed a message")
	}
	assertCode(t, err, transport.InvalidField)
	if g.HighWatermark() != 9 {
		t.Fatal("zero sequence reset watermark")
	}
	receiveSequence(t, g, 10)
}

func TestConnectionMaximumSequenceDoesNotWrap(t *testing.T) {
	g := newGuard(t, trustedBinding())
	maximum := ^uint64(0)
	receiveSequence(t, g, maximum-1)
	receiveSequence(t, g, maximum)
	for _, sequence := range []uint64{maximum, maximum - 1, 1, 0} {
		sample := examples()[2]
		sample.e.Sequence = sequence
		decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
		if sequence == 0 {
			if decoded != nil {
				t.Fatal("zero after max exposed message")
			}
			assertCode(t, err, transport.InvalidField)
		} else {
			assertConnectionError(t, decoded, err, transport.ConnectionSequenceRejected)
		}
		if g.HighWatermark() != maximum {
			t.Fatal("maximum sequence wrapped or decreased")
		}
	}
}

func TestConnectionAllKnownMessagesAndUnknownExtension(t *testing.T) {
	for _, sample := range examples() {
		t.Run(sample.name, func(t *testing.T) {
			binding := trustedBinding()
			binding.Direction = sample.ctx.Direction
			g := newGuard(t, binding)
			raw := append(encodeFrame(t, sample.e), bytesField(100, []byte{0xff, 0x00})...)
			before := bytes.Clone(raw)
			decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
			if decoded == nil || err != nil || g.HighWatermark() != 1 {
				t.Fatalf("known payload rejected: %v", err)
			}
			if !bytes.Equal(decoded.ProtoReflect().GetUnknown(), bytesField(100, []byte{0xff, 0x00})) || !bytes.Equal(raw, before) {
				t.Fatal("extension lost or input mutated")
			}
		})
	}
}

func TestConnectionFailuresDoNotConsumeSequence(t *testing.T) {
	type failureCase struct {
		name       string
		change     func(*example, *transport.ValidationLimits, *transport.FrameDecodeLimits)
		wire       func(testing.TB, example) []byte
		connection transport.ConnectionErrorCode
		frame      transport.FrameErrorCode
		semantic   transport.ValidationCode
	}
	cases := []failureCase{
		{name: "wrong_node", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			s.e.NodeId = "different-node"
		}, connection: transport.ConnectionNodeMismatch},
		{name: "wrong_connection", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			s.e.ConnectionId = "different-connection"
		}, connection: transport.ConnectionIDMismatch},
		{name: "opposite_bound_direction", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) { *s = examples()[4] }, connection: transport.ConnectionDirectionMismatch},
		{name: "wrong_payload_direction", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			s.ctx.Direction = transport.ControllerToAgent
		}, semantic: transport.InvalidDirection},
		{name: "wrong_phase", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			s.ctx.Phase = transport.AwaitAgentHello
		}, semantic: transport.InvalidHandshake},
		{name: "wrong_version", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			s.e.ProtocolMajor = 999
		}, semantic: transport.InvalidProtocol},
		{name: "missing_message_id", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) { s.e.MessageId = "" }, semantic: transport.MissingID},
		{name: "bad_privilege", change: func(s *example, _ *transport.ValidationLimits, _ *transport.FrameDecodeLimits) {
			*s = examples()[4]
			s.e.GetTaskOffer().Privilege = 999
		}, semantic: transport.UnknownEnum},
		{name: "invalid_semantic_limits", change: func(_ *example, l *transport.ValidationLimits, _ *transport.FrameDecodeLimits) { l.MaxMessageBytes = 0 }, semantic: transport.InvalidContext},
		{name: "invalid_frame_limits", change: func(_ *example, _ *transport.ValidationLimits, f *transport.FrameDecodeLimits) { f.MaxFrameBytes = 0 }, frame: transport.FrameInvalidLimits},
		{name: "field_limit", change: func(_ *example, _ *transport.ValidationLimits, f *transport.FrameDecodeLimits) { f.MaxFields = 1 }, frame: transport.FrameFieldLimit},
		{name: "depth_limit", change: func(_ *example, _ *transport.ValidationLimits, f *transport.FrameDecodeLimits) { f.MaxDepth = 1 }, frame: transport.FrameDepthLimit},
		{name: "too_large", change: func(_ *example, _ *transport.ValidationLimits, f *transport.FrameDecodeLimits) { f.MaxFrameBytes = 1 }, frame: transport.FrameTooLarge},
		{name: "malformed", wire: func(testing.TB, example) []byte { return []byte{0x80} }, frame: transport.FrameMalformed},
		{name: "duplicate_metadata", wire: func(t testing.TB, s example) []byte { return append(encodeFrame(t, s.e), varintField(1, 1)...) }, frame: transport.FrameDuplicateMetadata},
		{name: "invalid_utf8_decode", wire: func(t testing.TB, s example) []byte {
			return append(withoutPayload(t, s), bytesField(22, bytesField(4, []byte{0xff}))...)
		}, frame: transport.FrameDecodeFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGuard(t, trustedBinding())
			receiveSequence(t, g, 4)
			sample := examples()[2]
			l, f := limits(), frameLimits()
			if tc.change != nil {
				tc.change(&sample, &l, &f)
			}
			sample.e.Sequence = 5
			raw := encodeFrame(t, sample.e)
			if tc.wire != nil {
				raw = tc.wire(t, sample)
			}
			before := bytes.Clone(raw)
			decoded, err := g.Receive(raw, sample.ctx, l, f)
			if decoded != nil || err == nil || g.HighWatermark() != 4 || !bytes.Equal(raw, before) {
				t.Fatalf("rejected frame changed state or output: %v", err)
			}
			switch {
			case tc.connection != "":
				assertConnectionError(t, decoded, err, tc.connection)
			case tc.semantic != "":
				assertCode(t, err, tc.semantic)
			default:
				var detail *transport.FrameError
				if !errors.As(err, &detail) || detail.Code != tc.frame {
					t.Fatalf("wrong frame error: %v", err)
				}
			}
			// Correcting the rejected frame may reuse that unconsumed sequence.
			receiveSequence(t, g, 5)
			if g.HighWatermark() != 5 {
				t.Fatal("corrected sequence could not commit")
			}
		})
	}
}

func TestConnectionCloseIsTerminalAndNewConnectionExplicit(t *testing.T) {
	g := newGuard(t, trustedBinding())
	receiveSequence(t, g, 7)
	g.Close()
	g.Close()
	sample := examples()[2]
	sample.e.Sequence = 8
	decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionClosed)
	decoded, err = g.Receive([]byte{0}, transport.ValidationContext{}, transport.ValidationLimits{}, transport.FrameDecodeLimits{})
	assertConnectionError(t, decoded, err, transport.ConnectionClosed)
	if g.HighWatermark() != 7 {
		t.Fatal("close cleared or advanced watermark")
	}
	binding := trustedBinding()
	binding.ConnectionID = "synthetic-new-connection"
	newConnection := newGuard(t, binding)
	sample.e.Sequence = 1
	decoded, err = newConnection.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionIDMismatch)
	if newConnection.HighWatermark() != 0 {
		t.Fatal("old connection polluted new watermark")
	}
	sample.e.ConnectionId = binding.ConnectionID
	decoded, err = newConnection.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	if decoded == nil || err != nil || newConnection.HighWatermark() != 1 {
		t.Fatalf("explicit new connection failed: %v", err)
	}
	decoded, err = g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionClosed)
}

func TestConnectionBindingAndReturnedMessageCannotResetState(t *testing.T) {
	binding := trustedBinding()
	g := newGuard(t, binding)
	binding.NodeID = "changed-node"
	binding.ConnectionID = "changed-connection"
	binding.Direction = transport.ControllerToAgent
	decoded := receiveSequence(t, g, 1)
	decoded.NodeId = "changed-node"
	decoded.ConnectionId = "changed-connection"
	decoded.Sequence = ^uint64(0)
	if g.HighWatermark() != 1 {
		t.Fatal("returned message changed private state")
	}
	copyOfHandle := *g
	receiveSequence(t, &copyOfHandle, 2)
	if g.HighWatermark() != 2 {
		t.Fatal("copy reset the connection stream")
	}
	g.Close()
	sample := examples()[2]
	sample.e.Sequence = 3
	decoded, err := copyOfHandle.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionClosed)
}

type guardResult struct {
	message *agentv1.Envelope
	err     error
}

func TestConnectionConcurrentSameSequence(t *testing.T) {
	g := newGuard(t, trustedBinding())
	receiveSequence(t, g, 1)
	sample := examples()[2]
	sample.e.Sequence = 2
	raw := encodeFrame(t, sample.e)
	start := make(chan struct{})
	results := make(chan guardResult, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
			results <- guardResult{decoded, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result.err == nil {
			if result.message == nil {
				t.Fatal("success without message")
			}
			accepted++
		} else {
			assertConnectionError(t, result.message, result.err, transport.ConnectionSequenceRejected)
		}
	}
	if accepted != 1 || g.HighWatermark() != 2 {
		t.Fatalf("accepted=%d watermark=%d, want 1/2", accepted, g.HighWatermark())
	}
}

func TestConnectionConcurrentIncreasingSequences(t *testing.T) {
	g := newGuard(t, trustedBinding())
	receiveSequence(t, g, 1)
	start := make(chan struct{})
	results := make(chan guardResult, 2)
	for _, sequence := range []uint64{2, 3} {
		sample := examples()[2]
		sample.e.Sequence = sequence
		raw := encodeFrame(t, sample.e)
		go func() {
			<-start
			decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
			results <- guardResult{decoded, err}
		}()
	}
	close(start)
	accepted := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err == nil {
			accepted++
		} else {
			assertConnectionError(t, result.message, result.err, transport.ConnectionSequenceRejected)
		}
	}
	if accepted < 1 || accepted > 2 || g.HighWatermark() != 3 {
		t.Fatalf("concurrent state regressed: accepted=%d watermark=%d", accepted, g.HighWatermark())
	}
}

func TestConnectionConcurrentCloseAndReceive(t *testing.T) {
	g := newGuard(t, trustedBinding())
	receiveSequence(t, g, 1)
	sample := examples()[2]
	sample.e.Sequence = 2
	raw := encodeFrame(t, sample.e)
	start := make(chan struct{})
	results := make(chan guardResult, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			decoded, err := g.Receive(raw, sample.ctx, limits(), frameLimits())
			results <- guardResult{decoded, err}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); <-start; g.Close() }()
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result.err == nil {
			if result.message == nil {
				t.Fatal("success without message")
			}
			accepted++
			continue
		}
		var detail *transport.ConnectionError
		if result.message != nil || !errors.As(result.err, &detail) || (detail.Code != transport.ConnectionClosed && detail.Code != transport.ConnectionSequenceRejected) {
			t.Fatalf("unexpected race result: %v", result.err)
		}
	}
	if accepted > 1 || g.HighWatermark() != 1+uint64(accepted) {
		t.Fatal("close/receive atomicity violated")
	}
	sample.e.Sequence = 3
	decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionClosed)
}

func TestConnectionErrorsDoNotEchoBindingOrFrame(t *testing.T) {
	marker := "synthetic-sensitive-marker"
	binding := trustedBinding()
	binding.NodeID = marker
	g := newGuard(t, binding)
	sample := examples()[2]
	sample.e.MessageId = marker
	decoded, err := g.Receive(encodeFrame(t, sample.e), sample.ctx, limits(), frameLimits())
	assertConnectionError(t, decoded, err, transport.ConnectionNodeMismatch)
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "synthetic-node") {
		t.Fatal("binding/frame leaked into error")
	}
	binding.ConnectionID = marker + "\n"
	_, err = transport.NewConnectionGuard(binding)
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatal("invalid binding echoed input")
	}
}
